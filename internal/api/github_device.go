package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Device Flow "satu-klik" bersifat OPSIONAL & TAK diaktifkan default. Untuk
// produk self-hosted open-source, kita TIDAK menyematkan client_id apa pun —
// tiap operator yang mau one-click mendaftar GitHub OAuth App-nya SENDIRI lalu
// set env LAMUND_GITHUB_CLIENT_ID. Bila kosong, "Hubungkan GitHub" nonaktif dan
// pengguna memakai Personal Access Token (jalur tanpa konfigurasi, universal).
func (s *server) githubClientID() string {
	return s.d.GitHubClientID // kosong = device flow nonaktif
}

// deviceFlowEnabled true bila operator sudah menyetel client_id OAuth App-nya.
func (s *server) deviceFlowEnabled() bool {
	return s.githubClientID() != ""
}

// Base URL GitHub — var agar bisa dioverride di test.
var (
	ghLoginBase = "https://github.com"
	ghAPIBase   = "https://api.github.com"
)

type devicePending struct {
	deviceCode string
	interval   int
	expiresAt  time.Time
}

// ghDeviceCode memulai Device Flow: minta device_code + user_code ke GitHub.
func ghDeviceCode(clientID string) (deviceCode, userCode, verifyURI string, interval int, err error) {
	form := url.Values{"client_id": {clientID}, "scope": {"repo"}}
	req, _ := http.NewRequest("POST", ghLoginBase+"/login/device/code", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := githubClient.Do(req)
	if err != nil {
		return "", "", "", 0, err
	}
	defer resp.Body.Close()
	var out struct {
		DeviceCode      string `json:"device_code"`
		UserCode        string `json:"user_code"`
		VerificationURI string `json:"verification_uri"`
		Interval        int    `json:"interval"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", "", "", 0, err
	}
	if out.Interval < 1 {
		out.Interval = 5
	}
	return out.DeviceCode, out.UserCode, out.VerificationURI, out.Interval, nil
}

func (s *server) handleDeviceStart(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r.Context())
	if !s.deviceFlowEnabled() {
		writeErr(w, http.StatusNotImplemented, "GitHub satu-klik belum diaktifkan di server ini — pakai personal access token")
		return
	}
	deviceCode, userCode, verifyURI, interval, err := ghDeviceCode(s.githubClientID())
	if err != nil || deviceCode == "" {
		writeErr(w, http.StatusBadGateway, "gagal memulai koneksi GitHub")
		return
	}
	s.devMu.Lock()
	s.devPending[u.ID] = &devicePending{deviceCode: deviceCode, interval: interval, expiresAt: s.d.Now().Add(15 * time.Minute)}
	s.devMu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{
		"user_code": userCode, "verification_uri": verifyURI, "interval": interval,
	})
}

// ghDeviceToken menukar device_code jadi access token. Bila belum diauthorize,
// GitHub balas error "authorization_pending"/"slow_down" (bukan kegagalan).
func ghDeviceToken(clientID, deviceCode string) (accessToken, apiErr string, err error) {
	form := url.Values{
		"client_id":   {clientID},
		"device_code": {deviceCode},
		"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
	}
	req, _ := http.NewRequest("POST", ghLoginBase+"/login/oauth/access_token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := githubClient.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	var out struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", "", err
	}
	return out.AccessToken, out.Error, nil
}

func (s *server) handleDevicePoll(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r.Context())
	s.devMu.Lock()
	p := s.devPending[u.ID]
	var pend devicePending
	if p != nil {
		pend = *p
	}
	s.devMu.Unlock()
	if p == nil || s.d.Now().After(pend.expiresAt) {
		s.clearPending(u.ID)
		writeJSON(w, http.StatusOK, map[string]string{"status": "error", "error": "expired"})
		return
	}
	token, apiErr, err := ghDeviceToken(s.githubClientID(), pend.deviceCode)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]string{"status": "pending"})
		return
	}
	if apiErr == "authorization_pending" || apiErr == "slow_down" {
		writeJSON(w, http.StatusOK, map[string]string{"status": "pending"})
		return
	}
	if apiErr != "" || token == "" {
		s.clearPending(u.ID)
		writeJSON(w, http.StatusOK, map[string]string{"status": "error", "error": apiErr})
		return
	}
	login, err := githubLogin(token)
	if err != nil {
		s.clearPending(u.ID)
		writeErr(w, http.StatusBadGateway, "token GitHub tak bisa diverifikasi")
		return
	}
	meta := fmt.Sprintf(`{"login":%q}`, login)
	if err := s.d.Store.SetConnection(u.ID, "github", token, meta); err != nil {
		s.clearPending(u.ID)
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.clearPending(u.ID)
	writeJSON(w, http.StatusOK, map[string]string{"status": "connected", "login": login})
}

func (s *server) clearPending(userID int64) {
	s.devMu.Lock()
	delete(s.devPending, userID)
	s.devMu.Unlock()
}
