package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGithubClientIDUnsetDisablesDeviceFlow(t *testing.T) {
	s := &server{d: Deps{}} // kosong → device flow nonaktif (self-hosted default)
	if s.githubClientID() != "" || s.deviceFlowEnabled() {
		t.Fatal("tanpa client_id, device flow harus nonaktif")
	}
	s2 := &server{d: Deps{GitHubClientID: "Iv1.custom"}}
	if s2.githubClientID() != "Iv1.custom" || !s2.deviceFlowEnabled() {
		t.Fatal("dengan client_id, device flow harus aktif")
	}
}

// mockGitHub membuat server tiruan untuk endpoint device flow + /user.
// counter access_token: panggilan ke-1 pending, ke-2 sukses.
func mockGitHub(t *testing.T) (*httptest.Server, *int) {
	t.Helper()
	tokenCalls := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/login/device/code", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"device_code": "DEV123", "user_code": "WXYZ-1234",
			"verification_uri": "https://github.com/login/device", "expires_in": 900, "interval": 1,
		})
	})
	mux.HandleFunc("/login/oauth/access_token", func(w http.ResponseWriter, _ *http.Request) {
		tokenCalls++
		w.Header().Set("Content-Type", "application/json")
		if tokenCalls < 2 {
			json.NewEncoder(w).Encode(map[string]string{"error": "authorization_pending"})
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"access_token": "gho_secret"})
	})
	mux.HandleFunc("/user", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"login": "octocat", "id": 583231})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, &tokenCalls
}

func TestDeviceStart(t *testing.T) {
	srv, _ := mockGitHub(t)
	oldL, oldA := ghLoginBase, ghAPIBase
	ghLoginBase, ghAPIBase = srv.URL, srv.URL
	t.Cleanup(func() { ghLoginBase, ghAPIBase = oldL, oldA })

	h, _ := harness(t)
	tok := loginToken(t, h)
	rec := do(t, h, "POST", "/api/v1/connections/github/device/start", tok, nil)
	if rec.Code != 200 {
		t.Fatalf("start harus 200, dapat %d: %s", rec.Code, rec.Body)
	}
	var resp struct {
		UserCode        string `json:"user_code"`
		VerificationURI string `json:"verification_uri"`
		Interval        int    `json:"interval"`
	}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.UserCode != "WXYZ-1234" || resp.VerificationURI == "" || resp.Interval < 1 {
		t.Fatalf("respons start salah: %+v", resp)
	}
}

func TestDevicePollPendingThenConnected(t *testing.T) {
	srv, calls := mockGitHub(t)
	oldL, oldA := ghLoginBase, ghAPIBase
	ghLoginBase, ghAPIBase = srv.URL, srv.URL
	t.Cleanup(func() { ghLoginBase, ghAPIBase = oldL, oldA })

	h, st := harness(t)
	tok := loginToken(t, h)
	// mulai device flow supaya ada pending
	do(t, h, "POST", "/api/v1/connections/github/device/start", tok, nil)

	// poll #1 → pending (mock token call ke-1)
	rec := do(t, h, "POST", "/api/v1/connections/github/device/poll", tok, nil)
	var p1 struct{ Status string `json:"status"` }
	json.Unmarshal(rec.Body.Bytes(), &p1)
	if p1.Status != "pending" {
		t.Fatalf("poll pertama harus pending, dapat %q (%s)", p1.Status, rec.Body)
	}
	// poll #2 → connected (mock token call ke-2 sukses)
	rec = do(t, h, "POST", "/api/v1/connections/github/device/poll", tok, nil)
	var p2 struct{ Status, Login string }
	json.Unmarshal(rec.Body.Bytes(), &p2)
	if p2.Status != "connected" || p2.Login != "octocat" {
		t.Fatalf("poll kedua harus connected octocat, dapat %+v (%s)", p2, rec.Body)
	}
	// token tersimpan server-side, tak bocor
	u, _ := st.GetUserByUsername("admin")
	conn, _ := st.GetConnection(u.ID, "github")
	if conn == nil || conn.Token != "gho_secret" {
		t.Fatalf("token GitHub harus tersimpan: %+v", conn)
	}
	if *calls < 2 {
		t.Fatalf("mock token harus dipanggil >=2, dapat %d", *calls)
	}
}
