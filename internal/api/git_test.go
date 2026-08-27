package api

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/lamun-my-id/lamund/internal/auth"
	"github.com/lamun-my-id/lamund/internal/builder"
	"github.com/lamun-my-id/lamund/internal/runner"
	"github.com/lamun-my-id/lamund/internal/store"
)

// harnessWithRuntime menyetel Runner + Builder + Sites agar endpoint app bekerja.
func harnessWithRuntime(t *testing.T) (http.Handler, *store.Store, string, string) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	hash, _ := auth.HashPassword("rahasia123")
	st.CreateUser(store.User{Username: "admin", PasswordHash: hash, Role: "superadmin"})
	secret, _ := auth.GenerateSecret()
	logDir := t.TempDir()
	h := New(Deps{
		Store: st, Issuer: auth.NewIssuer(secret, time.Hour),
		Sites:   store.NewSiteFS(t.TempDir()),
		Runner:  runner.NewSupervisor(logDir),
		Builder: builder.NewManager(logDir),
	})
	return h, st, logDir, ""
}

func TestValidGitHubSig(t *testing.T) {
	body := []byte(`{"ref":"refs/heads/main"}`)
	mac := hmac.New(sha256.New, []byte("s3cr3t"))
	mac.Write(body)
	good := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if !validGitHubSig("s3cr3t", body, good) {
		t.Fatal("signature benar harus lolos")
	}
	if validGitHubSig("s3cr3t", body, "sha256=deadbeef") {
		t.Fatal("signature salah harus ditolak")
	}
	if validGitHubSig("s3cr3t", body, "") {
		t.Fatal("tanpa header harus ditolak")
	}
}

// sign menghitung header signature GitHub untuk body.
func sign(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func hook(t *testing.T, h http.Handler, domain, event, sig string, body []byte) int {
	t.Helper()
	req := httptest.NewRequest("POST", "/api/v1/hooks/git/"+domain, bytes.NewReader(body))
	if event != "" {
		req.Header.Set("X-GitHub-Event", event)
	}
	if sig != "" {
		req.Header.Set("X-Hub-Signature-256", sig)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code
}

func TestGitWebhook(t *testing.T) {
	h, st, _, _ := harnessDirs(t)
	// app dgn secret webhook (dibuat langsung di store — Builder nil di harness,
	// deploy jadi no-op; kita hanya menguji auth webhook & routing).
	st.CreateApp(store.App{
		Domain: "gitapp.com", UserID: 1, Command: "node .", Port: 40000,
		RepoURL: "https://example.com/r.git", Branch: "main", WebhookSecret: "topsecret",
	})
	body := []byte(`{"ref":"refs/heads/main"}`)

	// signature salah → 401
	if c := hook(t, h, "gitapp.com", "push", "sha256=bad", body); c != 401 {
		t.Fatalf("sig salah harus 401, dapat %d", c)
	}
	// tanpa app → 404
	if c := hook(t, h, "tidakada.com", "push", sign("x", body), body); c != 404 {
		t.Fatalf("app tak ada harus 404, dapat %d", c)
	}
	// ping dgn sig benar → 200 pong
	if c := hook(t, h, "gitapp.com", "ping", sign("topsecret", body), body); c != 200 {
		t.Fatalf("ping harus 200, dapat %d", c)
	}
	// push dgn sig benar → 202 deploying
	if c := hook(t, h, "gitapp.com", "push", sign("topsecret", body), body); c != 202 {
		t.Fatalf("push valid harus 202, dapat %d", c)
	}
}

func TestCreateAppWithRepoGeneratesWebhook(t *testing.T) {
	h, st, _, _ := harnessWithRuntime(t)
	tok := loginToken(t, h)
	rec := do(t, h, "POST", "/api/v1/apps", tok, map[string]any{
		"domain": "repo.com", "command": "npm start",
		"repo_url": "https://github.com/acme/app.git", "branch": "main",
	})
	if rec.Code != 201 {
		t.Fatalf("create app+repo mau 201, dapat %d: %s", rec.Code, rec.Body)
	}
	var out appJSON
	json.Unmarshal(rec.Body.Bytes(), &out)
	if out.WebhookSecret == "" || out.WebhookPath != "/api/v1/hooks/git/repo.com" {
		t.Fatalf("webhook harus tergenerate: %+v", out)
	}
	// URL non-https ditolak
	if c := do(t, h, "POST", "/api/v1/apps", tok, map[string]any{
		"domain": "bad.com", "command": "npm start", "repo_url": "git@github.com:x/y.git",
	}).Code; c != 400 {
		t.Fatalf("repo non-https harus 400, dapat %d", c)
	}
	_ = st
}

func localRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	run := func(a ...string) {
		c := exec.Command("git", a...)
		c.Dir = repo
		c.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", a, err, out)
		}
	}
	run("init", "-q", "-b", "main")
	os.WriteFile(filepath.Join(repo, "index.html"), []byte("HELLO-FROM-GIT"), 0o644)
	run("add", ".")
	run("commit", "-qm", "v1")
	return repo
}

// TestGitDeployIntegration: webhook push → clone repo lokal → build (static,
// tanpa langkah) → sukses. Membuktikan rantai Fetch→build via webhook.
func TestGitDeployIntegration(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git tak tersedia")
	}
	h, st, _, _ := harnessWithRuntime(t)
	tok := loginToken(t, h)
	work := t.TempDir()
	st.CreateApp(store.App{
		Domain: "g.com", UserID: 1, Command: "sh -c 'sleep 30'", Port: 40077,
		WorkDir: work, RepoURL: localRepo(t), Branch: "main", WebhookSecret: "sec",
	})
	body := []byte(`{"ref":"refs/heads/main"}`)
	if c := hook(t, h, "g.com", "push", sign("sec", body), body); c != 202 {
		t.Fatalf("webhook push mau 202, dapat %d", c)
	}
	// tunggu build sukses (async), poll via API
	deadline := time.Now().Add(10 * time.Second)
	var status string
	for time.Now().Before(deadline) {
		rec := do(t, h, "GET", "/api/v1/apps/g.com/build", tok, nil)
		var s struct{ Status string }
		json.Unmarshal(rec.Body.Bytes(), &s)
		status = s.Status
		if status == "success" || status == "failed" {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if status != "success" {
		t.Fatalf("build git harus success, dapat %q", status)
	}
	// repo ter-clone ke workdir
	if b, _ := os.ReadFile(filepath.Join(work, "index.html")); string(b) != "HELLO-FROM-GIT" {
		t.Fatalf("repo harus ter-clone ke workdir, dapat %q", b)
	}
	_ = st
}

func TestAppEnv(t *testing.T) {
	h, _, _, _ := harnessWithRuntime(t)
	tok := loginToken(t, h)
	// create dgn env
	if c := do(t, h, "POST", "/api/v1/apps", tok, map[string]any{
		"domain": "envapp.com", "command": "node .", "autostart": false,
		"env": map[string]string{"NODE_ENV": "production", "API_KEY": "x=y=z"},
	}).Code; c != 201 {
		t.Fatalf("create+env mau 201, dapat %d", c)
	}
	// GET env
	rec := do(t, h, "GET", "/api/v1/apps/envapp.com/env", tok, nil)
	var g struct{ Env map[string]string }
	json.Unmarshal(rec.Body.Bytes(), &g)
	if g.Env["NODE_ENV"] != "production" || g.Env["API_KEY"] != "x=y=z" {
		t.Fatalf("GET env: %+v", g.Env)
	}
	// PUT env (ganti total)
	if c := do(t, h, "PUT", "/api/v1/apps/envapp.com/env", tok, map[string]any{
		"env": map[string]string{"ONLY": "1"},
	}).Code; c != 200 {
		t.Fatalf("PUT env mau 200, dapat %d", c)
	}
	rec = do(t, h, "GET", "/api/v1/apps/envapp.com/env", tok, nil)
	var g2 struct{ Env map[string]string }
	json.Unmarshal(rec.Body.Bytes(), &g2)
	if len(g2.Env) != 1 || g2.Env["ONLY"] != "1" {
		t.Fatalf("env setelah PUT: %+v", g2.Env)
	}
	// key tak valid ditolak
	if c := do(t, h, "PUT", "/api/v1/apps/envapp.com/env", tok, map[string]any{
		"env": map[string]string{"bad key!": "1"},
	}).Code; c != 400 {
		t.Fatalf("key tak valid harus 400, dapat %d", c)
	}
}

func TestAltPort(t *testing.T) {
	if altPort(40000) != 50000 || altPort(50000) != 40000 || altPort(40007) != 50007 {
		t.Fatal("altPort toggle base↔base+10000 salah")
	}
}

func TestWaitListening(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port
	if !waitListening(port, 2*time.Second) {
		t.Fatal("port yang listen harus terdeteksi siap")
	}
	// port bebas → tidak siap (timeout cepat)
	free, _ := net.Listen("tcp", "127.0.0.1:0")
	fp := free.Addr().(*net.TCPAddr).Port
	free.Close()
	if waitListening(fp, 500*time.Millisecond) {
		t.Fatal("port tanpa listener harus timeout")
	}
}
