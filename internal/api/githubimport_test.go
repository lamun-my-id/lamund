package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lamun-my-id/lamund/internal/store"
)

// mockGitHubCreate memasang mock GitHub API untuk create-repo + hooks.
// createStatus mengatur status balasan POST /user/repos.
func mockGitHubCreate(t *testing.T, createStatus int) *httptest.Server {
	t.Helper()
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "POST" && r.URL.Path == "/user/repos":
			w.WriteHeader(createStatus)
			if createStatus == http.StatusCreated {
				json.NewEncoder(w).Encode(map[string]any{
					"full_name":      "octocat/mysite",
					"clone_url":      "https://github.com/octocat/mysite.git",
					"default_branch": "main",
				})
			}
		case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/hooks"):
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]any{"id": 1})
		case r.URL.Path == "/user": // dipakai githubUser saat seed (Sites nil → tak kepakai)
			json.NewEncoder(w).Encode(map[string]any{"login": "octocat", "id": 1})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(mock.Close)
	oldBase := ghAPIBase
	ghAPIBase = mock.URL
	t.Cleanup(func() { ghAPIBase = oldBase })
	return mock
}

func adminToken(t *testing.T, h http.Handler) string {
	t.Helper()
	rec := login(t, h, "admin", "rahasia123")
	var lr loginResp
	json.Unmarshal(rec.Body.Bytes(), &lr)
	if lr.Token == "" {
		t.Fatal("token admin kosong")
	}
	return lr.Token
}

// TestCreateRepoForSite: happy path — repo dibuat, site tersambung, 202.
func TestCreateRepoForSite(t *testing.T) {
	h, st := harness(t) // admin id=1
	mockGitHubCreate(t, http.StatusCreated)
	st.CreateSite(store.Site{Domain: "lp.test", Type: "static", UserID: 1, OwnerType: "user", OwnerID: 1})
	st.SetConnection(1, "github", "gho_tok", "")
	tok := adminToken(t, h)

	rec := do(t, h, "POST", "/api/v1/sites/lp.test/create-repo", tok,
		map[string]any{"name": "mysite", "private": true})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("harus 202, dapat %d: %s", rec.Code, rec.Body)
	}
	site, _ := st.GetSiteByDomain("lp.test")
	if site == nil || site.RepoURL != "https://github.com/octocat/mysite.git" {
		t.Fatalf("RepoURL tak tersimpan, site=%+v", site)
	}
	if site.Branch != "main" {
		t.Fatalf("branch harus main, dapat %q", site.Branch)
	}
}

// TestCreateRepoConflictAlreadyConnected: site sudah punya repo → 409.
func TestCreateRepoConflictAlreadyConnected(t *testing.T) {
	h, st := harness(t)
	mockGitHubCreate(t, http.StatusCreated)
	st.CreateSite(store.Site{Domain: "lp.test", Type: "static", UserID: 1, OwnerType: "user", OwnerID: 1})
	st.SetSiteGit("lp.test", "https://github.com/x/y.git", "main", "", ".")
	st.SetConnection(1, "github", "gho_tok", "")
	tok := adminToken(t, h)

	rec := do(t, h, "POST", "/api/v1/sites/lp.test/create-repo", tok, map[string]any{"name": "z"})
	if rec.Code != http.StatusConflict {
		t.Fatalf("site sudah tersambung harus 409, dapat %d", rec.Code)
	}
}

// TestCreateRepoNameTaken: GitHub balas 422 → 409.
func TestCreateRepoNameTaken(t *testing.T) {
	h, st := harness(t)
	mockGitHubCreate(t, http.StatusUnprocessableEntity)
	st.CreateSite(store.Site{Domain: "lp.test", Type: "static", UserID: 1, OwnerType: "user", OwnerID: 1})
	st.SetConnection(1, "github", "gho_tok", "")
	tok := adminToken(t, h)

	rec := do(t, h, "POST", "/api/v1/sites/lp.test/create-repo", tok, map[string]any{"name": "dupe"})
	if rec.Code != http.StatusConflict {
		t.Fatalf("nama repo bentrok harus 409, dapat %d: %s", rec.Code, rec.Body)
	}
}

// TestCreateRepoNoConnection: belum connect GitHub → 400.
func TestCreateRepoNoConnection(t *testing.T) {
	h, st := harness(t)
	st.CreateSite(store.Site{Domain: "lp.test", Type: "static", UserID: 1, OwnerType: "user", OwnerID: 1})
	tok := adminToken(t, h)

	rec := do(t, h, "POST", "/api/v1/sites/lp.test/create-repo", tok, map[string]any{"name": "x"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("tanpa koneksi GitHub harus 400, dapat %d", rec.Code)
	}
}
