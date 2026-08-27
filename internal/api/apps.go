package api

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/lamun-my-id/lamund/internal/gitdeploy"
	"github.com/lamun-my-id/lamund/internal/quota"
	"github.com/lamun-my-id/lamund/internal/runner"
	"github.com/lamun-my-id/lamund/internal/site"
	"github.com/lamun-my-id/lamund/internal/store"
)

func (s *server) registerApps(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/apps", s.requireAuth(s.handleListApps))
	mux.HandleFunc("POST /api/v1/apps", s.requireAuth(s.handleCreateApp))
	mux.HandleFunc("GET /api/v1/apps/{domain}", s.requireAuth(s.handleGetApp))
	mux.HandleFunc("DELETE /api/v1/apps/{domain}", s.requireAuth(s.handleDeleteApp))
	mux.HandleFunc("POST /api/v1/apps/{domain}/start", s.requireAuth(s.appAction(s.doStart)))
	mux.HandleFunc("POST /api/v1/apps/{domain}/stop", s.requireAuth(s.appAction(s.doStop)))
	mux.HandleFunc("POST /api/v1/apps/{domain}/restart", s.requireAuth(s.appAction(s.doRestart)))
	mux.HandleFunc("GET /api/v1/apps/{domain}/logs", s.requireAuth(s.handleAppLogs))
	mux.HandleFunc("POST /api/v1/apps/{domain}/build", s.requireAuth(s.handleBuild))
	mux.HandleFunc("GET /api/v1/apps/{domain}/build", s.requireAuth(s.handleBuildStatus))
	mux.HandleFunc("GET /api/v1/apps/{domain}/build/logs", s.requireAuth(s.handleBuildLogs))
	mux.HandleFunc("GET /api/v1/apps/{domain}/env", s.requireAuth(s.handleGetEnv))
	mux.HandleFunc("PUT /api/v1/apps/{domain}/env", s.requireAuth(s.handlePutEnv))
	mux.HandleFunc("POST /api/v1/apps/{domain}/deploy-git", s.requireAuth(s.handleDeployGit))
	// Webhook GitHub: PUBLIK (GitHub tak bisa kirim JWT) — diamankan HMAC.
	mux.HandleFunc("POST /api/v1/hooks/git/{domain}", s.handleGitHook)
}

// deployGit menjalankan git fetch → build → restart (dipakai manual & webhook).
func (s *server) deployGit(app store.App) {
	if s.d.Builder == nil {
		return
	}
	repoURL := s.authedRepoURL(app.UserID, app.RepoURL)
	pre := func(logw io.Writer) error {
		return gitdeploy.Fetch(repoURL, app.Branch, app.WorkDir, logw)
	}
	s.d.Builder.Deploy(app.Domain, app.WorkDir, pre, app.Env, func() {
		s.zeroDowntimeSwap(app)
	})
}

// authedRepoURL menyisipkan token GitHub milik userID (bila terhubung) ke
// URL clone https github.com, agar repo privat bisa di-deploy. Token tak
// pernah ditulis ke log (gitdeploy mencetak repoURL asli, bukan ini).
func (s *server) authedRepoURL(userID int64, repoURL string) string {
	const prefix = "https://github.com/"
	if !strings.HasPrefix(repoURL, prefix) {
		return repoURL
	}
	conn, err := s.d.Store.GetConnection(userID, "github")
	if err != nil || conn == nil || conn.Token == "" {
		return repoURL
	}
	return "https://x-access-token:" + conn.Token + "@github.com/" + strings.TrimPrefix(repoURL, prefix)
}

func (s *server) handleDeployGit(w http.ResponseWriter, r *http.Request) {
	a, _, ok := s.ownedApp(w, r)
	if !ok {
		return
	}
	if a.RepoURL == "" {
		writeErr(w, http.StatusBadRequest, "app ini tidak terhubung ke repositori Git")
		return
	}
	s.deployGit(*a)
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "deploying"})
}

// handleGitHook menerima push GitHub, memverifikasi HMAC, lalu deploy.
// Menangani DUA jenis target di URL yang sama: app (runtime) ATAU situs
// git-static — dibedakan lewat domain (unik). Coba app dulu, lalu situs.
func (s *server) handleGitHook(w http.ResponseWriter, r *http.Request) {
	domain := r.PathValue("domain")
	body, _ := io.ReadAll(io.LimitReader(r.Body, 5<<20))
	sig := r.Header.Get("X-Hub-Signature-256")
	ping := r.Header.Get("X-GitHub-Event") == "ping"

	// 1) App (runtime).
	if app, _ := s.d.Store.GetAppByDomain(domain); app != nil && app.WebhookSecret != "" {
		if !validGitHubSig(app.WebhookSecret, body, sig) {
			writeErr(w, http.StatusUnauthorized, "signature tidak valid")
			return
		}
		if ping {
			writeJSON(w, http.StatusOK, map[string]string{"status": "pong"})
			return
		}
		s.deployGit(*app)
		writeJSON(w, http.StatusAccepted, map[string]string{"status": "deploying"})
		return
	}

	// 2) Situs git-static.
	secret, _ := s.d.Store.GetSiteWebhookSecret(domain)
	site, _ := s.d.Store.GetSiteByDomain(domain)
	if secret == "" || site == nil || site.RepoURL == "" {
		writeErr(w, http.StatusNotFound, "webhook tidak ditemukan")
		return
	}
	if !validGitHubSig(secret, body, sig) {
		writeErr(w, http.StatusUnauthorized, "signature tidak valid")
		return
	}
	if ping {
		writeJSON(w, http.StatusOK, map[string]string{"status": "pong"})
		return
	}
	go s.deploySiteGit(*site, "webhook")
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "deploying"})
}

func validGitHubSig(secret string, body []byte, header string) bool {
	if !strings.HasPrefix(header, "sha256=") {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	want := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(want), []byte(header))
}

func validRepoURL(u string) bool {
	return strings.HasPrefix(u, "https://") && !strings.ContainsAny(u, " \t\n\r\"'`$;|&<>()") && len(u) < 512
}

// validBranch menolak nama branch yang bisa jadi flag git (argument injection).
func validBranch(b string) bool {
	if b == "" {
		return true // kosong → default "main" di gitdeploy
	}
	if strings.HasPrefix(b, "-") || len(b) > 200 {
		return false
	}
	for _, r := range b {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '.', r == '_', r == '/', r == '-':
		default:
			return false
		}
	}
	return true
}

func randomHex(nbytes int) string {
	b := make([]byte, nbytes)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// handleBuild memicu build asinkron (deteksi + install/build) di folder app,
// lalu restart app bila sukses.
func (s *server) handleBuild(w http.ResponseWriter, r *http.Request) {
	a, _, ok := s.ownedApp(w, r)
	if !ok {
		return
	}
	if s.d.Builder == nil {
		writeErr(w, http.StatusServiceUnavailable, "builder tidak tersedia")
		return
	}
	app := *a
	s.d.Builder.Build(app.Domain, app.WorkDir, app.Env, func() {
		s.zeroDowntimeSwap(app)
	})
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "building"})
}

func (s *server) handleBuildStatus(w http.ResponseWriter, r *http.Request) {
	a, _, ok := s.ownedApp(w, r)
	if !ok {
		return
	}
	if s.d.Builder == nil {
		writeJSON(w, http.StatusOK, map[string]string{"status": "idle"})
		return
	}
	writeJSON(w, http.StatusOK, s.d.Builder.Status(a.Domain))
}

func (s *server) handleBuildLogs(w http.ResponseWriter, r *http.Request) {
	a, _, ok := s.ownedApp(w, r)
	if !ok {
		return
	}
	if s.d.Builder == nil {
		writeJSON(w, http.StatusOK, map[string]any{"lines": []string{}})
		return
	}
	lines, err := s.d.Builder.Logs(a.Domain, 300)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "gagal membaca log build")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"lines": lines})
}

type appJSON struct {
	Domain        string `json:"domain"`
	Command       string `json:"command"`
	Port          int    `json:"port"`
	Autostart     bool   `json:"autostart"`
	State         string `json:"state"`
	PID           int    `json:"pid"`
	Restarts      int    `json:"restarts"`
	RepoURL       string `json:"repo_url,omitempty"`
	Branch        string `json:"branch,omitempty"`
	WebhookPath   string `json:"webhook_path,omitempty"`
	WebhookSecret string `json:"webhook_secret,omitempty"`
	OwnerType     string `json:"owner_type"`
	OwnerID       int64  `json:"owner_id"`
}

func (s *server) appToJSON(a store.App) appJSON {
	j := appJSON{Domain: a.Domain, Command: a.Command, Port: a.Port, Autostart: a.Autostart,
		RepoURL: a.RepoURL, Branch: a.Branch, OwnerType: a.OwnerType, OwnerID: a.OwnerID}
	if a.WebhookSecret != "" {
		j.WebhookPath = "/api/v1/hooks/git/" + a.Domain
		j.WebhookSecret = a.WebhookSecret
	}
	if s.d.Runner != nil {
		st := s.d.Runner.Status(a.Domain)
		j.State, j.PID, j.Restarts = st.State, st.PID, st.Restarts
	}
	if j.State == "" {
		j.State = runner.StateStopped
	}
	return j
}

// ownedApp memuat app milik caller (admin: apa saja); 404 bila bukan miliknya.
func (s *server) ownedApp(w http.ResponseWriter, r *http.Request) (*store.App, *authUser, bool) {
	u, _ := userFrom(r.Context())
	a, err := s.d.Store.GetAppByDomain(r.PathValue("domain"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "gagal membaca app")
		return nil, nil, false
	}
	if a == nil || !s.canAccessOwner(u, a.OwnerType, a.OwnerID) {
		writeErr(w, http.StatusNotFound, "app tidak ditemukan")
		return nil, nil, false
	}
	return a, u, true
}

func (s *server) handleListApps(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r.Context())
	var (
		apps []store.App
		err  error
	)
	if u.Role == "superadmin" {
		apps, err = s.d.Store.ListApps()
	} else {
		apps, err = s.d.Store.ListAppsVisibleTo(u.ID)
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "gagal membaca apps")
		return
	}
	out := make([]appJSON, 0, len(apps))
	for _, a := range apps {
		out = append(out, s.appToJSON(a))
	}
	writeJSON(w, http.StatusOK, map[string]any{"apps": out})
}

type createAppReq struct {
	Domain    string            `json:"domain"`
	Command   string            `json:"command"`
	Autostart *bool             `json:"autostart"`
	RepoURL   string            `json:"repo_url"`
	Branch    string            `json:"branch"`
	Env       map[string]string `json:"env"`
	OwnerType string            `json:"owner_type"` // R4: 'user' (default) | 'team'
	OwnerID   int64             `json:"owner_id"`
	DNSAuto   bool              `json:"dns_auto"`
}

func (s *server) handleCreateApp(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r.Context())
	if s.d.Runner == nil || s.d.Sites == nil {
		writeErr(w, http.StatusServiceUnavailable, "runtime app tidak tersedia")
		return
	}
	var req createAppReq
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "body tidak valid")
		return
	}
	if err := store.ValidateDomain(req.Domain); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(req.Command) < 2 {
		writeErr(w, http.StatusBadRequest, "command wajib diisi (mis. 'npm start')")
		return
	}
	// Enforce kuota jumlah app per user (default free-tier; admin bisa naikin).
	if okq, reason, err := quota.CanCreateApp(s.d.Store, u.ID, u.Role); err != nil {
		writeErr(w, http.StatusInternalServerError, "gagal cek kuota")
		return
	} else if !okq {
		writeErr(w, http.StatusForbidden, reason)
		return
	}
	// Folder kerja app = folder terkelola tenant (tempat file di-deploy via .zip).
	if err := s.d.Sites.EnsureSiteDir(u.ID, req.Domain); err != nil {
		writeErr(w, http.StatusInternalServerError, "gagal menyiapkan folder app")
		return
	}
	ownerType, ownerID, okOwner := s.resolveOwner(u, req.OwnerType, req.OwnerID)
	if !okOwner {
		writeErr(w, http.StatusForbidden, "kamu bukan anggota tim tujuan")
		return
	}
	autostart := true
	if req.Autostart != nil {
		autostart = *req.Autostart
	}
	for k := range req.Env {
		if !validEnvKey(k) {
			writeErr(w, http.StatusBadRequest, "nama variabel env tidak valid: "+k)
			return
		}
	}
	newApp := store.App{
		Domain: req.Domain, UserID: u.ID, Command: req.Command,
		WorkDir: s.d.Sites.SiteRoot(u.ID, req.Domain), Autostart: autostart,
		Env: envMapToSlice(req.Env), OwnerType: ownerType, OwnerID: ownerID,
	}
	// Terhubung ke Git → validasi URL & terbitkan secret webhook.
	if req.RepoURL != "" {
		if !validRepoURL(req.RepoURL) {
			writeErr(w, http.StatusBadRequest, "repo_url harus URL https:// yang valid")
			return
		}
		if !validBranch(req.Branch) {
			writeErr(w, http.StatusBadRequest, "nama branch tidak valid")
			return
		}
		newApp.RepoURL = req.RepoURL
		newApp.Branch = req.Branch
		newApp.WebhookSecret = randomHex(20)
	}
	app, err := s.d.Store.CreateApp(newApp)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	// Site proxy → port app, sehingga routing + HTTPS reuse.
	if _, err := s.d.Store.CreateSite(store.Site{
		Domain: app.Domain, Type: "proxy", ProxyTarget: fmt.Sprintf("http://127.0.0.1:%d", app.Port),
		Status: "active", UserID: u.ID, Config: appProxyConfig(app.Port),
		OwnerType: ownerType, OwnerID: ownerID,
	}); err != nil {
		s.d.Store.DeleteApp(app.Domain) // rollback
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if app.Autostart {
		s.d.Runner.Start(s.runnerApp(app))
	}
	// Auto-provision record DNS bila zona tercakup & dns_auto=true (best-effort).
	if req.DNSAuto {
		s.autoProvisionDNS(u, req.Domain)
	}
	s.notifyReload()
	writeJSON(w, http.StatusCreated, s.appToJSON(app))
}

func (s *server) handleGetApp(w http.ResponseWriter, r *http.Request) {
	a, _, ok := s.ownedApp(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, s.appToJSON(*a))
}

func (s *server) handleDeleteApp(w http.ResponseWriter, r *http.Request) {
	a, _, ok := s.ownedApp(w, r)
	if !ok {
		return
	}
	if s.d.Runner != nil {
		s.d.Runner.Stop(a.Domain)
	}
	s.d.Store.DeleteApp(a.Domain)
	s.d.Store.DeleteSite(a.Domain)
	s.notifyReload()
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// appAction membungkus aksi start/stop/restart dengan pemeriksaan kepemilikan.
func (s *server) appAction(fn func(store.App) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		a, _, ok := s.ownedApp(w, r)
		if !ok {
			return
		}
		if s.d.Runner == nil {
			writeErr(w, http.StatusServiceUnavailable, "runtime app tidak tersedia")
			return
		}
		if err := fn(*a); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, s.appToJSON(*a))
	}
}

func (s *server) doStart(a store.App) error   { return s.d.Runner.Start(s.runnerApp(a)) }
func (s *server) doStop(a store.App) error    { return s.d.Runner.Stop(a.Domain) }
func (s *server) doRestart(a store.App) error { return s.d.Runner.Restart(s.runnerApp(a)) }

func (s *server) handleAppLogs(w http.ResponseWriter, r *http.Request) {
	a, _, ok := s.ownedApp(w, r)
	if !ok {
		return
	}
	n := 100
	if q := r.URL.Query().Get("n"); q != "" {
		if v, err := strconv.Atoi(q); err == nil && v > 0 && v <= 1000 {
			n = v
		}
	}
	lines, err := s.d.Runner.Logs(a.Domain, n)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "gagal membaca log")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"lines": lines})
}

// runnerApp mengonversi store.App → runner.App, sekaligus menempelkan batas
// resource efektif dari kuota pemilik (default free-tier / override admin).
func (s *server) runnerApp(a store.App) runner.App {
	mem, cpu, pids := quota.AppLimits(s.d.Store, a.UserID)
	return runner.App{
		Name: a.Domain, LogKey: a.Domain, Dir: a.WorkDir, Command: a.Command, Env: a.Env, Port: a.Port,
		MemoryMB: mem, CPUPercent: cpu, Pids: pids,
	}
}

// appProxyConfig menghasilkan config situs (routing) yang mem-proxy ke port app.
func appProxyConfig(port int) string {
	cfg, _ := json.Marshal(site.Config{Version: site.ConfigVersion, Routes: []site.RouteSpec{{
		Match:   site.MatchSpec{PathPrefix: "/"},
		Handler: site.HandlerSpec{Proxy: &site.ProxySpec{Upstream: fmt.Sprintf("http://127.0.0.1:%d", port)}},
		Use:     []site.MiddlewareSpec{{Headers: &site.HeadersSpec{Security: true}}},
	}}})
	return string(cfg)
}

// altPort mengembalikan port pasangan (blue/green) untuk app: base ↔ base+10000.
func altPort(p int) int {
	if p >= 50000 {
		return p - 10000
	}
	return p + 10000
}

// waitListening menunggu port menerima koneksi (app siap) hingga timeout.
func waitListening(port int, timeout time.Duration) bool {
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if c, err := net.DialTimeout("tcp", addr, time.Second); err == nil {
			c.Close()
			return true
		}
		time.Sleep(300 * time.Millisecond)
	}
	return false
}

// zeroDowntimeSwap menjalankan versi baru di port pasangan, menunggu siap,
// mengalihkan route ke port baru, lalu mematikan versi lama. Bila versi baru
// tak kunjung siap, rollback: buang yang baru & pulihkan yang lama.
func (s *server) zeroDowntimeSwap(app store.App) {
	if s.d.Runner == nil {
		return
	}
	name, oldPort := app.Domain, app.Port
	newPort := altPort(oldPort)

	s.d.Runner.Rename(name, name+"#old") // instance lama (bila ada) minggir
	na := app
	na.Port = newPort
	s.d.Runner.Start(s.runnerApp(na)) // versi baru pada port pasangan, nama kanonik

	if !waitListening(newPort, 45*time.Second) {
		// gagal siap → rollback ke versi lama
		s.d.Runner.Stop(name)
		s.d.Runner.Delete(name)
		s.d.Runner.Rename(name+"#old", name)
		return
	}
	// sehat → alihkan lalu lintas ke port baru, matikan yang lama
	s.d.Store.SetSiteConfig(name, appProxyConfig(newPort))
	s.notifyReload()
	s.d.Store.SetAppPort(name, newPort)
	s.d.Runner.Stop(name + "#old")
	s.d.Runner.Delete(name + "#old")
}

// ---- environment variables ----

func (s *server) handleGetEnv(w http.ResponseWriter, r *http.Request) {
	a, _, ok := s.ownedApp(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"env": envSliceToMap(a.Env)})
}

func (s *server) handlePutEnv(w http.ResponseWriter, r *http.Request) {
	a, _, ok := s.ownedApp(w, r)
	if !ok {
		return
	}
	var req struct {
		Env map[string]string `json:"env"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "body tidak valid")
		return
	}
	for k := range req.Env {
		if !validEnvKey(k) {
			writeErr(w, http.StatusBadRequest, "nama variabel tidak valid: "+k)
			return
		}
	}
	env := envMapToSlice(req.Env)
	if err := s.d.Store.SetAppEnv(a.Domain, env); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Terapkan: restart app dengan env baru bila sedang berjalan.
	if s.d.Runner != nil && s.d.Runner.Status(a.Domain).State == runner.StateRunning {
		a.Env = env
		s.d.Runner.Restart(s.runnerApp(*a))
	}
	writeJSON(w, http.StatusOK, map[string]any{"env": req.Env})
}

func validEnvKey(k string) bool {
	if k == "" || len(k) > 128 {
		return false
	}
	for i, r := range k {
		first := i == 0
		switch {
		case r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z', r == '_':
		case !first && r >= '0' && r <= '9':
		default:
			return false
		}
	}
	return true
}

func envMapToSlice(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k, v := range m {
		out = append(out, k+"="+v)
	}
	sort.Strings(out) // deterministik
	return out
}

func envSliceToMap(env []string) map[string]string {
	m := make(map[string]string, len(env))
	for _, kv := range env {
		if i := strings.IndexByte(kv, '='); i > 0 {
			m[kv[:i]] = kv[i+1:]
		}
	}
	return m
}
