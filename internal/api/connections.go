package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// providerAllow adalah daftar provider yang didukung. GitHub dipakai untuk
// deploy repo privat + repo picker; sisanya token AI (fitur menyusul).
var providerAllow = map[string]bool{
	"github": true, "google": true, "openai": true, "anthropic": true, "gemini": true,
}

func (s *server) registerConnections(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/connections", s.requireAuth(s.handleListConnections))
	mux.HandleFunc("PUT /api/v1/connections/{provider}", s.requireAuth(s.handleSetConnection))
	mux.HandleFunc("DELETE /api/v1/connections/{provider}", s.requireAuth(s.handleDeleteConnection))
	mux.HandleFunc("GET /api/v1/connections/github/repos", s.requireAuth(s.handleGitHubRepos))
	mux.HandleFunc("GET /api/v1/connections/github/branches", s.requireAuth(s.handleGitHubBranches))
	mux.HandleFunc("POST /api/v1/connections/github/device/start", s.requireAuth(s.handleDeviceStart))
	mux.HandleFunc("POST /api/v1/connections/github/device/poll", s.requireAuth(s.handleDevicePoll))
}

type connectionJSON struct {
	Provider  string          `json:"provider"`
	Connected bool            `json:"connected"`
	Meta      json.RawMessage `json:"meta,omitempty"`
}

func (s *server) handleListConnections(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r.Context())
	conns, err := s.d.Store.ListConnections(u.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "gagal membaca koneksi")
		return
	}
	have := map[string]connectionJSON{}
	for _, c := range conns {
		meta := json.RawMessage(c.Meta)
		if c.Meta == "" {
			meta = nil
		}
		have[c.Provider] = connectionJSON{Provider: c.Provider, Connected: true, Meta: meta}
	}
	// Kembalikan status untuk semua provider yang didukung (connected true/false).
	out := []connectionJSON{}
	for _, p := range []string{"github", "google", "openai", "anthropic", "gemini"} {
		if c, ok := have[p]; ok {
			out = append(out, c)
		} else {
			out = append(out, connectionJSON{Provider: p, Connected: false})
		}
	}
	// github_device: apakah "Hubungkan GitHub" satu-klik aktif (operator sudah
	// menyetel client_id OAuth App). Bila false, UI menampilkan PAT sebagai
	// jalur utama (tanpa konfigurasi).
	writeJSON(w, http.StatusOK, map[string]any{"connections": out, "github_device": s.deviceFlowEnabled()})
}

type setConnReq struct {
	Token string `json:"token"`
}

func (s *server) handleSetConnection(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r.Context())
	provider := r.PathValue("provider")
	if !providerAllow[provider] {
		writeErr(w, http.StatusBadRequest, "provider tidak didukung")
		return
	}
	var req setConnReq
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "body tidak valid")
		return
	}
	if len(req.Token) < 8 {
		writeErr(w, http.StatusBadRequest, "token tidak valid")
		return
	}
	meta := ""
	// GitHub: validasi token dengan memanggil /user, simpan login sebagai meta.
	if provider == "github" {
		login, err := githubLogin(req.Token)
		if err != nil {
			writeErr(w, http.StatusBadGateway, "token GitHub ditolak: "+err.Error())
			return
		}
		meta = fmt.Sprintf(`{"login":%q}`, login)
	}
	if err := s.d.Store.SetConnection(u.ID, provider, req.Token, meta); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	m := json.RawMessage(meta)
	if meta == "" {
		m = nil
	}
	writeJSON(w, http.StatusOK, connectionJSON{Provider: provider, Connected: true, Meta: m})
}

func (s *server) handleDeleteConnection(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r.Context())
	provider := r.PathValue("provider")
	if err := s.d.Store.DeleteConnection(u.ID, provider); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "disconnected"})
}

type repoJSON struct {
	FullName string `json:"full_name"`
	Private  bool   `json:"private"`
	CloneURL string `json:"clone_url"`
}

// handleGitHubRepos memakai token tersimpan caller untuk memuat daftar repo
// (untuk repo picker di wizard). Token TIDAK pernah dikirim ke klien.
func (s *server) handleGitHubRepos(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r.Context())
	conn, err := s.d.Store.GetConnection(u.ID, "github")
	if err != nil || conn == nil {
		writeErr(w, http.StatusNotFound, "GitHub belum terhubung")
		return
	}
	repos, err := githubRepos(conn.Token)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "gagal memuat repo: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"repos": repos})
}

// ---- klien GitHub minimal (net/http, timeout ketat) ----

var githubClient = &http.Client{Timeout: 10 * time.Second}

// githubAPI melakukan request ke GitHub API. Bila body != nil, di-marshal jadi
// JSON (dipakai POST create-repo & pasang webhook). Body respons dibatasi 2MB.
func githubAPI(method, token, path string, body any) ([]byte, int, error) {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, 0, err
		}
		rdr = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, ghAPIBase+path, rdr)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "lamund")
	if body != nil {
		req.Header.Set("Content-Type", "application/vnd.github+json")
	}
	resp, err := githubClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	rb, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	return rb, resp.StatusCode, nil
}

func githubReq(token, path string) ([]byte, int, error) {
	return githubAPI("GET", token, path, nil)
}

func githubLogin(token string) (string, error) {
	login, _, err := githubUser(token)
	return login, err
}

// githubPrimaryEmail mengambil email primary+verified user (best-effort).
// Dipakai githubimport saat seed commit awal (butuh email author).
func githubPrimaryEmail(token string) string {
	body, code, err := githubReq(token, "/user/emails")
	if err != nil || code != 200 {
		return ""
	}
	var emails []struct {
		Email    string `json:"email"`
		Primary  bool   `json:"primary"`
		Verified bool   `json:"verified"`
	}
	_ = json.Unmarshal(body, &emails)
	for _, e := range emails {
		if e.Primary && e.Verified {
			return strings.ToLower(e.Email)
		}
	}
	return ""
}

// githubUser mengembalikan login + ID NUMERIK GitHub (immutable — login bisa
// di-rename, jadi identitas dipetakan lewat id, bukan login/email).
func githubUser(token string) (login string, id int64, err error) {
	body, code, err := githubReq(token, "/user")
	if err != nil {
		return "", 0, err
	}
	if code != 200 {
		return "", 0, fmt.Errorf("status %d", code)
	}
	var u struct {
		Login string `json:"login"`
		ID    int64  `json:"id"`
	}
	if err := json.Unmarshal(body, &u); err != nil || u.ID == 0 {
		return "", 0, fmt.Errorf("respons tak terduga")
	}
	return u.Login, u.ID, nil
}

// githubRepos memuat repo user (owned+collaborator+org) untuk repo picker.
// Paginate hingga beberapa halaman agar user dgn banyak repo tetap terjaring
// (sebelumnya hanya 100 → repo ke-101+ tak pernah muncul/terjaring search).
// Dedup by full_name (aman bila ada repo bergeser antar-halaman saat sort=updated).
func githubRepos(token string) ([]repoJSON, error) {
	const maxPages = 10 // s.d. ~1000 repo
	var all []repoJSON
	seen := map[string]bool{}
	for page := 1; page <= maxPages; page++ {
		path := fmt.Sprintf("/user/repos?per_page=100&affiliation=owner,collaborator,organization_member&sort=updated&page=%d", page)
		body, code, err := githubReq(token, path)
		if err != nil {
			return nil, err
		}
		if code != 200 {
			return nil, fmt.Errorf("status %d", code)
		}
		var raw []repoJSON
		if err := json.Unmarshal(body, &raw); err != nil {
			return nil, err
		}
		for _, r := range raw {
			if !seen[r.FullName] {
				seen[r.FullName] = true
				all = append(all, r)
			}
		}
		if len(raw) < 100 {
			break // halaman terakhir
		}
	}
	return all, nil
}

// githubBranches mengembalikan daftar nama branch repo {owner}/{repo}.
func githubBranches(token, owner, repo string) ([]string, error) {
	path := fmt.Sprintf("/repos/%s/%s/branches?per_page=100", url.PathEscape(owner), url.PathEscape(repo))
	body, code, err := githubReq(token, path)
	if err != nil {
		return nil, err
	}
	if code != 200 {
		return nil, fmt.Errorf("status %d", code)
	}
	var raw []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(raw))
	for _, b := range raw {
		names = append(names, b.Name)
	}
	return names, nil
}

type branchJSON struct {
	Name string `json:"name"`
}

// handleGitHubBranches memakai token tersimpan caller untuk memuat daftar branch
// repo (untuk branch picker di wizard). Token TIDAK pernah dikirim ke klien.
func (s *server) handleGitHubBranches(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r.Context())
	owner := r.URL.Query().Get("owner")
	repo := r.URL.Query().Get("repo")
	if owner == "" || repo == "" {
		writeErr(w, http.StatusBadRequest, "owner dan repo wajib diisi")
		return
	}
	conn, err := s.d.Store.GetConnection(u.ID, "github")
	if err != nil || conn == nil {
		writeErr(w, http.StatusNotFound, "GitHub belum terhubung")
		return
	}
	names, err := githubBranches(conn.Token, owner, repo)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "gagal memuat branch: "+err.Error())
		return
	}
	branches := make([]branchJSON, 0, len(names))
	for _, n := range names {
		branches = append(branches, branchJSON{Name: n})
	}
	writeJSON(w, http.StatusOK, map[string]any{"branches": branches})
}
