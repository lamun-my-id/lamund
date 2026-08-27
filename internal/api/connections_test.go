package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestConnectionsLifecycle(t *testing.T) {
	h, st := harness(t)
	tok := loginToken(t, h)

	// awal: semua provider disconnected
	rec := do(t, h, "GET", "/api/v1/connections", tok, nil)
	if rec.Code != 200 {
		t.Fatalf("list connections harus 200, dapat %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), `"connected":true`) {
		t.Fatal("belum ada koneksi, tak boleh ada connected=true")
	}

	// hubungkan provider AI (tanpa panggilan jaringan)
	rec = do(t, h, "PUT", "/api/v1/connections/openai", tok, map[string]string{"token": "sk-test-abcdefgh"})
	if rec.Code != 200 {
		t.Fatalf("set connection harus 200, dapat %d: %s", rec.Code, rec.Body)
	}

	// list kini menandai openai connected — TANPA membocorkan token
	rec = do(t, h, "GET", "/api/v1/connections", tok, nil)
	body := rec.Body.String()
	if !strings.Contains(body, `"provider":"openai"`) || !strings.Contains(body, `"connected":true`) {
		t.Fatalf("openai harus connected: %s", body)
	}
	if strings.Contains(body, "sk-test-abcdefgh") {
		t.Fatal("token TIDAK boleh muncul di respons list")
	}

	// token benar-benar tersimpan (server-side)
	u, _ := st.GetUserByUsername("admin")
	conn, _ := st.GetConnection(u.ID, "openai")
	if conn == nil || conn.Token != "sk-test-abcdefgh" {
		t.Fatalf("token harus tersimpan server-side: %+v", conn)
	}

	// provider tak dikenal ditolak
	rec = do(t, h, "PUT", "/api/v1/connections/bogus", tok, map[string]string{"token": "xxxxxxxx"})
	if rec.Code != 400 {
		t.Fatalf("provider tak dikenal harus 400, dapat %d", rec.Code)
	}

	// putuskan
	rec = do(t, h, "DELETE", "/api/v1/connections/openai", tok, nil)
	if rec.Code != 200 {
		t.Fatalf("disconnect harus 200, dapat %d", rec.Code)
	}
	if c, _ := st.GetConnection(u.ID, "openai"); c != nil {
		t.Fatal("koneksi harus terhapus")
	}
}

func TestConnectionsIsolatedPerUser(t *testing.T) {
	h, st := harness(t)
	_, tokA := newMember(t, h, st, "connuser")
	do(t, h, "PUT", "/api/v1/connections/gemini", tokA, map[string]string{"token": "aizzzzzzzz"})

	// admin (user beda) tak melihat koneksi connuser
	adminTok := loginToken(t, h)
	rec := do(t, h, "GET", "/api/v1/connections", adminTok, nil)
	var resp struct {
		Connections []connectionJSON `json:"connections"`
	}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	for _, c := range resp.Connections {
		if c.Provider == "gemini" && c.Connected {
			t.Fatal("koneksi user lain tak boleh terlihat")
		}
	}
}

// TestGitHubBranches menguji githubBranches (unit) dan endpoint handleGitHubBranches.
func TestGitHubBranches(t *testing.T) {
	// Mock server GitHub yang menjawab /repos/{owner}/{repo}/branches.
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/branches"):
			json.NewEncoder(w).Encode([]map[string]string{
				{"name": "main"}, {"name": "develop"}, {"name": "feature-x"},
			})
		case r.URL.Path == "/user":
			json.NewEncoder(w).Encode(map[string]any{"login": "octocat", "id": 583231})
		default:
			http.NotFound(w, r)
		}
	}))
	defer mock.Close()

	// Override base URL GitHub ke mock.
	oldBase := ghAPIBase
	ghAPIBase = mock.URL
	t.Cleanup(func() { ghAPIBase = oldBase })

	// Uji unit githubBranches.
	names, err := githubBranches("tok-test", "octocat", "myrepo")
	if err != nil {
		t.Fatalf("githubBranches error: %v", err)
	}
	if len(names) != 3 || names[0] != "main" || names[1] != "develop" || names[2] != "feature-x" {
		t.Fatalf("branch tidak sesuai: %v", names)
	}

	// Uji endpoint GET /api/v1/connections/github/branches.
	h, st := harness(t)
	tok := loginToken(t, h)

	// Tanpa koneksi GitHub → 404.
	rec := do(t, h, "GET", "/api/v1/connections/github/branches?owner=octocat&repo=myrepo", tok, nil)
	if rec.Code != 404 {
		t.Fatalf("tanpa koneksi GitHub harus 404, dapat %d", rec.Code)
	}

	// Daftarkan koneksi GitHub (simpan langsung ke store agar lewati validasi /user).
	u, _ := st.GetUserByUsername("admin")
	st.SetConnection(u.ID, "github", "gho_test-token", `{"login":"octocat"}`)

	// Tanpa owner/repo → 400.
	rec = do(t, h, "GET", "/api/v1/connections/github/branches", tok, nil)
	if rec.Code != 400 {
		t.Fatalf("tanpa owner/repo harus 400, dapat %d", rec.Code)
	}

	// Dengan owner+repo → 200 + daftar branch.
	rec = do(t, h, "GET", "/api/v1/connections/github/branches?owner=octocat&repo=myrepo", tok, nil)
	if rec.Code != 200 {
		t.Fatalf("branches harus 200, dapat %d: %s", rec.Code, rec.Body)
	}
	var out struct {
		Branches []branchJSON `json:"branches"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(out.Branches) != 3 {
		t.Fatalf("mau 3 branch, dapat %d: %v", len(out.Branches), out.Branches)
	}
	if out.Branches[0].Name != "main" {
		t.Fatalf("branch pertama harus 'main', dapat %q", out.Branches[0].Name)
	}
}
