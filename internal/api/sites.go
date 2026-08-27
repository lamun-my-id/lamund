package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lamun-my-id/lamund/internal/builder"
	"github.com/lamun-my-id/lamund/internal/gitdeploy"
	"github.com/lamun-my-id/lamund/internal/quota"
	"github.com/lamun-my-id/lamund/internal/site"
	"github.com/lamun-my-id/lamund/internal/store"
)

// notifyReload memberi tahu data plane agar memuat ulang tabel routing.
func (s *server) notifyReload() {
	if s.d.OnSiteChange != nil {
		s.d.OnSiteChange()
	}
}

func (s *server) registerSites(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/sites", s.requireAuth(s.handleListSites))
	mux.HandleFunc("POST /api/v1/sites", s.requireAuth(s.handleCreateSite))
	mux.HandleFunc("GET /api/v1/sites/{domain}", s.requireAuth(s.handleGetSite))
	mux.HandleFunc("PATCH /api/v1/sites/{domain}", s.requireAuth(s.handlePatchSite))
	mux.HandleFunc("DELETE /api/v1/sites/{domain}", s.requireAuth(s.handleDeleteSite))
	mux.HandleFunc("POST /api/v1/sites/{domain}/deploy-git", s.requireAuth(s.handleSiteDeployGit))
	mux.HandleFunc("POST /api/v1/sites/{domain}/connect-git", s.requireAuth(s.handleSiteConnectGit))
	mux.HandleFunc("POST /api/v1/sites/{domain}/create-repo", s.requireAuth(s.handleCreateRepoForSite))
	mux.HandleFunc("POST /api/v1/sites/{domain}/disconnect-git", s.requireAuth(s.handleSiteDisconnectGit))
	mux.HandleFunc("GET /api/v1/sites/{domain}/webhook", s.requireAuth(s.handleSiteWebhook))
	mux.HandleFunc("POST /api/v1/sites/{domain}/webhook/regenerate", s.requireAuth(s.handleSiteWebhookRegen))
	mux.HandleFunc("GET /api/v1/sites/{domain}/deploy-log", s.requireAuth(s.handleSiteDeployLog))
	mux.HandleFunc("GET /api/v1/sites/{domain}/deploys", s.requireAuth(s.handleSiteDeploys))
	mux.HandleFunc("GET /api/v1/sites/{domain}/file", s.requireAuth(s.handleSiteReadFile))
	mux.HandleFunc("PUT /api/v1/sites/{domain}/file", s.requireAuth(s.handleSiteWriteFile))
	mux.HandleFunc("DELETE /api/v1/sites/{domain}/file", s.requireAuth(s.handleSiteDeleteFile))
	mux.HandleFunc("POST /api/v1/sites/{domain}/folder", s.requireAuth(s.handleSiteMkdir))
	mux.HandleFunc("GET /api/v1/sites/{domain}/domains", s.requireAuth(s.handleListSiteDomains))
	mux.HandleFunc("GET /api/v1/sites/{domain}/domains/status", s.requireAuth(s.handleSiteDomainStatus))
	mux.HandleFunc("POST /api/v1/sites/{domain}/domains", s.requireAuth(s.handleAddSiteDomain))
	mux.HandleFunc("DELETE /api/v1/sites/{domain}/domains/{alias}", s.requireAuth(s.handleDeleteSiteDomain))
}

type siteJSON struct {
	Domain      string `json:"domain"`
	Type        string `json:"type"`
	ProxyTarget string `json:"proxy_target,omitempty"`
	RootPath    string `json:"root_path,omitempty"`
	Status      string `json:"status"`
	CreatedAt   string `json:"created_at,omitempty"`
	OwnerType   string `json:"owner_type"`
	OwnerID     int64  `json:"owner_id"`
	// Field git (kosong = situs non-git). Dipakai panel untuk memutuskan
	// tampilan tab Git: form Connect (kosong) vs riwayat deploy (terisi).
	RepoURL   string `json:"repo_url,omitempty"`
	Branch    string `json:"branch,omitempty"`
	BuildCmd  string `json:"build_cmd,omitempty"`
	OutputDir string `json:"output_dir,omitempty"`
}

func toJSON(st store.Site) siteJSON {
	return siteJSON{st.Domain, st.Type, st.ProxyTarget, st.RootPath, st.Status, st.CreatedAt,
		st.OwnerType, st.OwnerID, st.RepoURL, st.Branch, st.BuildCmd, st.OutputDir}
}

func (s *server) handleListSites(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r.Context())
	var (
		sites []store.Site
		err   error
	)
	// Superadmin lihat semua; selain itu owner-scoped (personal + tim anggota).
	if u.Role == "superadmin" {
		sites, err = s.d.Store.ListSites()
	} else {
		sites, err = s.d.Store.ListSitesVisibleTo(u.ID)
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "gagal membaca situs")
		return
	}
	out := make([]siteJSON, 0, len(sites))
	for _, st := range sites {
		out = append(out, toJSON(st))
	}
	writeJSON(w, http.StatusOK, map[string]any{"sites": out})
}

type createSiteReq struct {
	Domain      string `json:"domain"`
	Type        string `json:"type"`
	ProxyTarget string `json:"proxy_target"`
	RootPath    string `json:"root_path"`
	OwnerType   string `json:"owner_type"` // R4: 'user' (default) | 'team'
	OwnerID     int64  `json:"owner_id"`
	RepoURL     string `json:"repo_url"`
	Branch      string `json:"branch"`
	BuildCmd    string `json:"build_cmd"`
	OutputDir   string `json:"output_dir"`
	DNSAuto     bool   `json:"dns_auto"`
}

func (s *server) handleCreateSite(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r.Context())
	var req createSiteReq
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "body tidak valid")
		return
	}
	if req.Type != "static" && req.Type != "proxy" {
		writeErr(w, http.StatusBadRequest, "type harus static atau proxy")
		return
	}
	// Reverse-proxy ke upstream custom HANYA operator/superadmin. Tenant bikin
	// site static + deploy app + route type 'app' (proxy ke port yang KITA sediakan).
	if req.Type == "proxy" && u.Role != "superadmin" {
		writeErr(w, http.StatusForbidden, "site proxy ke upstream custom tidak diizinkan — deploy app lalu route ke app-nya")
		return
	}
	// Validasi domain lebih awal — sebelum operasi FS (defense-in-depth).
	// store.CreateSite juga memvalidasi, tapi ini memastikan WriteSiteFile/SiteRoot
	// tidak dipanggil dengan domain tak tervalidasi.
	if err := store.ValidateDomain(req.Domain); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Type == "proxy" {
		norm, err := store.ValidateProxyTarget(req.ProxyTarget, false)
		if err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		req.ProxyTarget = norm
	}
	ownerType, ownerID, okOwner := s.resolveOwner(u, req.OwnerType, req.OwnerID)
	if !okOwner {
		writeErr(w, http.StatusForbidden, "kamu bukan anggota tim tujuan")
		return
	}
	ok, reason, err := quota.CanCreateSite(s.d.Store, u.ID, u.Role)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "gagal cek kuota")
		return
	}
	if !ok {
		writeErr(w, http.StatusForbidden, reason)
		return
	}
	site := store.Site{
		Domain:      req.Domain,
		Type:        req.Type,
		ProxyTarget: req.ProxyTarget,
		RootPath:    req.RootPath,
		Status:      "active",
		UserID:      u.ID,
		OwnerType:   ownerType,
		OwnerID:     ownerID,
	}
	// Situs statis lewat panel memakai folder terkelola per-tenant (user tak
	// menunjuk path server sembarang). File di-deploy lewat endpoint /deploy.
	if req.Type == "static" {
		// Validasi & simpan field git bila ada repo_url.
		if req.RepoURL != "" {
			if !validRepoURL(req.RepoURL) {
				writeErr(w, http.StatusBadRequest, "repo_url harus URL https:// yang valid")
				return
			}
			if !validBranch(req.Branch) {
				writeErr(w, http.StatusBadRequest, "nama branch tidak valid")
				return
			}
			if req.OutputDir != "" && (filepath.IsAbs(req.OutputDir) || strings.Contains(req.OutputDir, "..")) {
				writeErr(w, http.StatusBadRequest, "output_dir tidak valid")
				return
			}
			site.RepoURL = req.RepoURL
			site.Branch = req.Branch
			site.BuildCmd = req.BuildCmd
			// Default output_dir: "dist" bila ada build_cmd, "." untuk static murni.
			site.OutputDir = req.OutputDir
			if site.OutputDir == "" {
				if req.BuildCmd != "" {
					site.OutputDir = "dist"
				} else {
					site.OutputDir = "."
				}
			}
		}
		if s.d.Sites != nil {
			site.RootPath = s.d.Sites.SiteRoot(u.ID, req.Domain)
			// Placeholder hanya untuk situs tanpa repo (di-deploy manual lewat zip).
			if req.RepoURL == "" {
				if err := s.d.Sites.WriteSiteFile(u.ID, req.Domain, "index.html", placeholderHTML(req.Domain)); err != nil {
					writeErr(w, http.StatusInternalServerError, "gagal menyiapkan folder situs")
					return
				}
			}
		}
	}
	if _, err := s.d.Store.CreateSite(site); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	// Git-deploy async: clone → build → set root_path → reload.
	if req.Type == "static" && site.RepoURL != "" {
		go s.deploySiteGit(site, "create")
	}
	// Auto-provision DNS record A/AAAA bila zona tercakup dan dns_auto=true.
	// Best-effort: tidak menggagalkan pembuatan site.
	if req.DNSAuto {
		s.autoProvisionDNS(u, req.Domain)
	}
	s.notifyReload()
	writeJSON(w, http.StatusCreated, toJSON(site))
}

// ownedSite memuat site milik caller; admin boleh apa saja. Bila bukan milik
// caller (atau tak ada), balas 404 — jangan bocorkan keberadaan situs orang lain.
func (s *server) ownedSite(w http.ResponseWriter, r *http.Request) (*store.Site, *authUser, bool) {
	u, _ := userFrom(r.Context())
	domain := r.PathValue("domain")
	site, err := s.d.Store.GetSiteByDomain(domain)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "gagal membaca situs")
		return nil, nil, false
	}
	if site == nil || !s.canAccessOwner(u, site.OwnerType, site.OwnerID) {
		writeErr(w, http.StatusNotFound, "situs tidak ditemukan")
		return nil, nil, false
	}
	return site, u, true
}

func (s *server) handleGetSite(w http.ResponseWriter, r *http.Request) {
	site, _, ok := s.ownedSite(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, toJSON(*site))
}

type patchSiteReq struct {
	Type        *string `json:"type"`
	ProxyTarget *string `json:"proxy_target"`
	RootPath    *string `json:"root_path"`
	Status      *string `json:"status"`
}

func (s *server) handlePatchSite(w http.ResponseWriter, r *http.Request) {
	site, _, ok := s.ownedSite(w, r)
	if !ok {
		return
	}
	var req patchSiteReq
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "body tidak valid")
		return
	}
	if req.Status != nil {
		if *req.Status != "active" && *req.Status != "disabled" {
			writeErr(w, http.StatusBadRequest, "status harus active atau disabled")
			return
		}
		if err := s.d.Store.SetSiteStatus(site.Domain, *req.Status); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if req.Type != nil {
		switch *req.Type {
		case "proxy":
			target, err := store.ValidateProxyTarget(deref(req.ProxyTarget), false)
			if err != nil {
				writeErr(w, http.StatusBadRequest, err.Error())
				return
			}
			if err := s.d.Store.SetSiteProxy(site.Domain, target); err != nil {
				writeErr(w, http.StatusInternalServerError, err.Error())
				return
			}
		case "static":
			root := deref(req.RootPath)
			// Situs statis terkelola: abaikan path dari klien, pakai folder tenant.
			if s.d.Sites != nil {
				root = s.d.Sites.SiteRoot(site.UserID, site.Domain)
				if _, err := s.d.Sites.ListFiles(site.UserID, site.Domain); err != nil {
					_ = s.d.Sites.WriteSiteFile(site.UserID, site.Domain, "index.html", placeholderHTML(site.Domain))
				}
			}
			if err := s.d.Store.SetSiteStatic(site.Domain, root); err != nil {
				writeErr(w, http.StatusInternalServerError, err.Error())
				return
			}
		default:
			writeErr(w, http.StatusBadRequest, "type harus static atau proxy")
			return
		}
	}
	s.notifyReload()
	updated, _ := s.d.Store.GetSiteByDomain(site.Domain)
	writeJSON(w, http.StatusOK, toJSON(*updated))
}

func (s *server) handleDeleteSite(w http.ResponseWriter, r *http.Request) {
	site, _, ok := s.ownedSite(w, r)
	if !ok {
		return
	}
	if err := s.d.Store.DeleteSite(site.Domain); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.notifyReload()
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// deploySafeName mencegah nama domain dipakai untuk keluar direktori log.
func deploySafeName(domain string) string {
	d := strings.ToLower(strings.TrimSpace(domain))
	d = strings.ReplaceAll(d, "/", "_")
	d = strings.ReplaceAll(d, "..", "_")
	if d == "" {
		return "_unknown"
	}
	return d
}

// deployLogPath mengembalikan path file log deploy untuk domain.
func (s *server) deployLogPath(domain string) string {
	return filepath.Join(s.d.LogDir, "deploy", deploySafeName(domain)+".log")
}

// tailDeployLog membaca hingga n baris terakhir dari file log deploy domain.
func tailDeployLog(path string, n int) []string {
	f, err := os.Open(path)
	if err != nil {
		return []string{}
	}
	defer f.Close()
	ring := make([]string, 0, n)
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		if len(ring) == n {
			ring = ring[1:]
		}
		ring = append(ring, sc.Text())
	}
	return ring
}

// deploySiteGit mengklone/memperbarui repo Git ke dir situs, menjalankan build
// (bila ada build_cmd atau auto-detect), lalu memperbarui root_path dan memicu
// reload. Dijalankan di goroutine (async) — createSite sudah membalas 201.
func (s *server) deploySiteGit(site store.Site, trigger string) {
	if s.d.Sites == nil {
		return
	}
	dir := s.d.Sites.SiteRoot(site.UserID, site.Domain)
	repoURL := s.authedRepoURL(site.UserID, site.RepoURL)

	// Catat riwayat deploy (ala Vercel). Status akhir & commit diisi lewat defer
	// agar SEMUA jalur keluar (fetch/build gagal, containment tolak, sukses)
	// tertutup. commit di-capture setelah fetch berhasil.
	deployID, _ := s.d.Store.CreateDeploy(site.Domain, trigger)
	histStatus := "failed"
	var commit, commitMsg string
	defer func() {
		if deployID != 0 {
			_ = s.d.Store.FinishDeploy(deployID, histStatus, commit, commitMsg)
		}
	}()

	// Siapkan file log deploy per-situs (truncate tiap deploy).
	var logw *os.File
	if s.d.LogDir != "" {
		deployDir := filepath.Join(s.d.LogDir, "deploy")
		if err := os.MkdirAll(deployDir, 0o750); err == nil {
			f, err := os.Create(s.deployLogPath(site.Domain))
			if err == nil {
				logw = f
				defer logw.Close()
			}
		}
	}
	var logWriter interface{ Write([]byte) (int, error) }
	if logw != nil {
		logWriter = logw
	} else {
		logWriter = nopWriter{}
	}

	s.setDeployStatus(site.Domain, "building")

	if err := gitdeploy.Fetch(repoURL, site.Branch, dir, logWriter); err != nil {
		fmt.Fprintf(logWriter, "ERROR fetch: %v\n", err)
		s.setDeployStatus(site.Domain, "failed")
		return
	}
	// Kode terkini sudah di dir — catat commit yang di-deploy untuk riwayat.
	commit, commitMsg = gitdeploy.Head(dir)

	if site.BuildCmd != "" {
		// Build kustom: npm install + build_cmd (mis. "npm run build").
		if s.d.Builder != nil {
			plan := builder.Plan{Type: "node", Steps: []string{"npm install", site.BuildCmd}}
			if err := builder.Run(plan, dir, nil, logWriter); err != nil {
				fmt.Fprintf(logWriter, "ERROR build: %v\n", err)
				s.setDeployStatus(site.Domain, "failed")
				return
			}
		}
	} else if s.d.Builder != nil {
		// Auto-detect (SPA default: Vite → dist/, React → build/, dsb).
		plan := builder.Detect(dir)
		if len(plan.Steps) > 0 {
			if err := builder.Run(plan, dir, nil, logWriter); err != nil {
				fmt.Fprintf(logWriter, "ERROR build: %v\n", err)
				s.setDeployStatus(site.Domain, "failed")
				return
			}
		}
	}

	out := site.OutputDir
	if out == "" || out == "." {
		out = "."
	}
	root := dir
	if out != "." {
		root = filepath.Join(dir, out)
	}
	// Containment check: pastikan root tidak keluar dari direktori tenant.
	base := s.d.Sites.SiteRoot(site.UserID, site.Domain)
	cleanRoot := filepath.Clean(root)
	cleanBase := filepath.Clean(base)
	if cleanRoot != cleanBase && !strings.HasPrefix(cleanRoot+string(filepath.Separator), cleanBase+string(filepath.Separator)) {
		fmt.Fprintf(logWriter, "ERROR: output_dir keluar dari direktori tenant\n")
		s.setDeployStatus(site.Domain, "failed")
		return // output_dir keluar dari direktori tenant — tolak (histStatus tetap "failed")
	}

	if site.BuildCmd != "" {
		// SPA (ada build_cmd): simpan config dengan spa-fallback agar deep-link
		// tidak 404 saat hard-refresh.
		raw, _ := json.Marshal(spaSiteConfig(root))
		_ = s.d.Store.SetSiteConfig(site.Domain, string(raw))
	} else {
		// Static murni (tanpa build): pakai SetSiteRoot seperti semula.
		_ = s.d.Store.SetSiteRoot(site.Domain, root)
	}
	s.setDeployStatus(site.Domain, "success")
	histStatus = "success"
	s.notifyReload()
}

// nopWriter membuang semua output (pengganti io.Discard yang memenuhi interface).
type nopWriter struct{}

func (nopWriter) Write(p []byte) (int, error) { return len(p), nil }

// spaSiteConfig membangun Config situs statis SPA dengan satu route "/" yang
// menyajikan output_dir dengan fallback ke index.html (Vue/React/Next).
func spaSiteConfig(root string) site.Config {
	return site.Config{
		Version: site.ConfigVersion,
		Routes: []site.RouteSpec{{
			Match:   site.MatchSpec{PathPrefix: "/"},
			Handler: site.HandlerSpec{Static: &site.StaticSpec{Root: root, SPA: true}},
			Use: []site.MiddlewareSpec{
				{Headers: &site.HeadersSpec{Security: true}},
				{Compress: &site.CompressSpec{}},
			},
		}},
	}
}

// handleSiteDeployGit memicu ulang deploy git untuk situs (owner-scoped).
func (s *server) handleSiteDeployGit(w http.ResponseWriter, r *http.Request) {
	site, _, ok := s.ownedSite(w, r)
	if !ok {
		return
	}
	if site.RepoURL == "" {
		writeErr(w, http.StatusBadRequest, "situs ini tidak terhubung ke repositori Git")
		return
	}
	go s.deploySiteGit(*site, "manual")
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "deploying"})
}

type connectGitReq struct {
	RepoURL   string `json:"repo_url"`
	Branch    string `json:"branch"`
	BuildCmd  string `json:"build_cmd"`
	OutputDir string `json:"output_dir"`
}

// handleSiteConnectGit menghubungkan situs static yang sudah ada ke repositori
// Git (owner-scoped), lalu memicu deploy awal. Validasi sama dgn createSite.
func (s *server) handleSiteConnectGit(w http.ResponseWriter, r *http.Request) {
	site, _, ok := s.ownedSite(w, r)
	if !ok {
		return
	}
	if site.Type != "static" {
		writeErr(w, http.StatusBadRequest, "hanya situs static yang bisa dihubungkan ke Git")
		return
	}
	var req connectGitReq
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "body tidak valid")
		return
	}
	if !validRepoURL(req.RepoURL) {
		writeErr(w, http.StatusBadRequest, "repo_url harus URL https:// yang valid")
		return
	}
	if req.Branch == "" {
		req.Branch = "main"
	}
	if !validBranch(req.Branch) {
		writeErr(w, http.StatusBadRequest, "nama branch tidak valid")
		return
	}
	if req.OutputDir != "" && (filepath.IsAbs(req.OutputDir) || strings.Contains(req.OutputDir, "..")) {
		writeErr(w, http.StatusBadRequest, "output_dir tidak valid")
		return
	}
	// Default output_dir: "dist" bila ada build_cmd, "." untuk static murni.
	outDir := req.OutputDir
	if outDir == "" {
		if req.BuildCmd != "" {
			outDir = "dist"
		} else {
			outDir = "."
		}
	}
	if err := s.d.Store.SetSiteGit(site.Domain, req.RepoURL, req.Branch, req.BuildCmd, outDir); err != nil {
		writeErr(w, http.StatusInternalServerError, "gagal menyimpan koneksi git")
		return
	}
	// Muat ulang situs dgn field git terbaru, lalu deploy awal async.
	updated, err := s.d.Store.GetSiteByDomain(site.Domain)
	if err != nil || updated == nil {
		writeErr(w, http.StatusInternalServerError, "gagal memuat situs")
		return
	}
	go s.deploySiteGit(*updated, "manual")
	writeJSON(w, http.StatusAccepted, toJSON(*updated))
}

// handleSiteDisconnectGit memutus koneksi git situs (kosongkan field git).
// File hasil deploy terakhir dibiarkan tetap tersaji; situs kembali manual/zip.
func (s *server) handleSiteDisconnectGit(w http.ResponseWriter, r *http.Request) {
	site, _, ok := s.ownedSite(w, r)
	if !ok {
		return
	}
	if err := s.d.Store.SetSiteGit(site.Domain, "", "", "", ""); err != nil {
		writeErr(w, http.StatusInternalServerError, "gagal memutus koneksi git")
		return
	}
	updated, _ := s.d.Store.GetSiteByDomain(site.Domain)
	if updated == nil {
		updated = site
	}
	writeJSON(w, http.StatusOK, toJSON(*updated))
}

// handleSiteDeploys menyajikan riwayat deploy git (terbaru dulu) untuk situs.
func (s *server) handleSiteDeploys(w http.ResponseWriter, r *http.Request) {
	site, _, ok := s.ownedSite(w, r)
	if !ok {
		return
	}
	list, err := s.d.Store.ListDeploys(site.Domain, 20)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "gagal membaca riwayat deploy")
		return
	}
	out := make([]map[string]any, 0, len(list))
	for _, d := range list {
		out = append(out, map[string]any{
			"id":          d.ID,
			"status":      d.Status,
			"trigger":     d.Trigger,
			"commit":      d.Commit,
			"message":     d.Message,
			"started_at":  d.StartedAt,
			"finished_at": d.FinishedAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"deploys": out})
}

// handleSiteWebhook mengembalikan URL + secret webhook auto-deploy situs git
// (owner-scoped). Secret dibuat sekali (lazy) saat pertama diminta.
func (s *server) handleSiteWebhook(w http.ResponseWriter, r *http.Request) {
	site, _, ok := s.ownedSite(w, r)
	if !ok {
		return
	}
	if site.RepoURL == "" {
		writeErr(w, http.StatusBadRequest, "situs ini tidak terhubung ke repositori Git")
		return
	}
	secret, err := s.d.Store.GetSiteWebhookSecret(site.Domain)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "gagal membaca webhook")
		return
	}
	if secret == "" {
		secret = randomHex(20)
		if err := s.d.Store.SetSiteWebhookSecret(site.Domain, secret); err != nil {
			writeErr(w, http.StatusInternalServerError, "gagal menyimpan webhook")
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"webhook_path":   "/api/v1/hooks/git/" + site.Domain,
		"webhook_secret": secret,
	})
}

// handleSiteWebhookRegen memutar ulang secret webhook (owner-scoped).
func (s *server) handleSiteWebhookRegen(w http.ResponseWriter, r *http.Request) {
	site, _, ok := s.ownedSite(w, r)
	if !ok {
		return
	}
	if site.RepoURL == "" {
		writeErr(w, http.StatusBadRequest, "situs ini tidak terhubung ke repositori Git")
		return
	}
	secret := randomHex(20)
	if err := s.d.Store.SetSiteWebhookSecret(site.Domain, secret); err != nil {
		writeErr(w, http.StatusInternalServerError, "gagal menyimpan webhook")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"webhook_path":   "/api/v1/hooks/git/" + site.Domain,
		"webhook_secret": secret,
	})
}

// handleSiteDeployLog menyajikan status dan log deploy terakhir untuk situs.
func (s *server) handleSiteDeployLog(w http.ResponseWriter, r *http.Request) {
	site, _, ok := s.ownedSite(w, r)
	if !ok {
		return
	}
	status := s.getDeployStatus(site.Domain)
	lines := []string{}
	if s.d.LogDir != "" {
		lines = tailDeployLog(s.deployLogPath(site.Domain), 200)
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": status, "lines": lines})
}

// maxEditBytes membatasi berkas yang bisa dibaca/ditulis lewat editor panel.
const maxEditBytes = 1 << 20 // 1 MiB

// validRelPath memastikan path relatif aman (tak kosong, bukan absolut, tanpa
// segmen ".."). os.Root sudah mencegah keluar dir user, tapi ini juga mencegah
// lintas-situs milik user yang sama (mis. "../situs-lain").
func validRelPath(rel string) bool {
	rel = strings.TrimSpace(rel)
	if rel == "" || filepath.IsAbs(rel) {
		return false
	}
	for _, seg := range strings.Split(filepath.ToSlash(rel), "/") {
		if seg == ".." {
			return false
		}
	}
	return true
}

// gitManagedBlocked menolak modifikasi berkas manual pada situs yang dikelola
// git — deploy git menjalankan reset-hard, jadi editan manual akan hilang.
func (s *server) gitManagedBlocked(w http.ResponseWriter, site *store.Site) bool {
	if site.RepoURL != "" {
		writeErr(w, http.StatusConflict, "situs dikelola Git — ubah lewat repo lalu deploy (editan manual akan ketimpa)")
		return true
	}
	return false
}

// handleSiteReadFile mengembalikan isi satu berkas teks situs (owner-scoped).
func (s *server) handleSiteReadFile(w http.ResponseWriter, r *http.Request) {
	site, _, ok := s.ownedSite(w, r)
	if !ok {
		return
	}
	if s.d.Sites == nil {
		writeErr(w, http.StatusInternalServerError, "penyimpanan tidak aktif")
		return
	}
	rel := r.URL.Query().Get("path")
	if !validRelPath(rel) {
		writeErr(w, http.StatusBadRequest, "path tidak valid")
		return
	}
	data, err := s.d.Sites.ReadSiteFile(site.UserID, site.Domain, rel)
	if err != nil {
		writeErr(w, http.StatusNotFound, "berkas tidak ditemukan")
		return
	}
	if len(data) > maxEditBytes {
		writeErr(w, http.StatusRequestEntityTooLarge, "berkas terlalu besar untuk diedit (maks 1 MB)")
		return
	}
	if bytes.IndexByte(data, 0) >= 0 {
		writeErr(w, http.StatusUnsupportedMediaType, "berkas biner tak bisa diedit")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"path": rel, "content": string(data)})
}

type writeFileReq struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// handleSiteWriteFile membuat/menimpa berkas teks situs (owner-scoped, non-git).
func (s *server) handleSiteWriteFile(w http.ResponseWriter, r *http.Request) {
	site, _, ok := s.ownedSite(w, r)
	if !ok {
		return
	}
	if s.gitManagedBlocked(w, site) {
		return
	}
	var req writeFileReq
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "body tidak valid")
		return
	}
	if !validRelPath(req.Path) {
		writeErr(w, http.StatusBadRequest, "path tidak valid")
		return
	}
	if len(req.Content) > maxEditBytes {
		writeErr(w, http.StatusRequestEntityTooLarge, "isi terlalu besar (maks 1 MB)")
		return
	}
	if s.d.Sites == nil {
		writeErr(w, http.StatusInternalServerError, "penyimpanan tidak aktif")
		return
	}
	// Enforce kuota storage: pemakaian sekarang + isi baru tak boleh lewat batas
	// (konservatif — overwrite dihitung tambah, aman-di-sisi-batas).
	used, _ := s.d.Sites.DirSize(site.UserID, site.Domain)
	if okq, reason, err := quota.CanUseStorage(s.d.Store, site.UserID, ownerRole(site, s), used, int64(len(req.Content))); err != nil {
		writeErr(w, http.StatusInternalServerError, "gagal cek kuota")
		return
	} else if !okq {
		writeErr(w, http.StatusRequestEntityTooLarge, reason)
		return
	}
	if err := s.d.Sites.WriteSiteFile(site.UserID, site.Domain, req.Path, []byte(req.Content)); err != nil {
		writeErr(w, http.StatusInternalServerError, "gagal menyimpan berkas")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "saved"})
}

type mkdirReq struct {
	Path string `json:"path"`
}

// handleSiteMkdir membuat folder di dalam situs (owner-scoped, non-git).
func (s *server) handleSiteMkdir(w http.ResponseWriter, r *http.Request) {
	site, _, ok := s.ownedSite(w, r)
	if !ok {
		return
	}
	if s.gitManagedBlocked(w, site) {
		return
	}
	var req mkdirReq
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "body tidak valid")
		return
	}
	if !validRelPath(req.Path) {
		writeErr(w, http.StatusBadRequest, "path tidak valid")
		return
	}
	if s.d.Sites == nil {
		writeErr(w, http.StatusInternalServerError, "penyimpanan tidak aktif")
		return
	}
	if err := s.d.Sites.MkdirSite(site.UserID, site.Domain, req.Path); err != nil {
		writeErr(w, http.StatusInternalServerError, "gagal membuat folder")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "created"})
}

// handleSiteDeleteFile menghapus berkas/folder situs (owner-scoped, non-git).
func (s *server) handleSiteDeleteFile(w http.ResponseWriter, r *http.Request) {
	site, _, ok := s.ownedSite(w, r)
	if !ok {
		return
	}
	if s.gitManagedBlocked(w, site) {
		return
	}
	rel := r.URL.Query().Get("path")
	if !validRelPath(rel) {
		writeErr(w, http.StatusBadRequest, "path tidak valid")
		return
	}
	if s.d.Sites == nil {
		writeErr(w, http.StatusInternalServerError, "penyimpanan tidak aktif")
		return
	}
	if err := s.d.Sites.DeleteSitePath(site.UserID, site.Domain, rel); err != nil {
		writeErr(w, http.StatusInternalServerError, "gagal menghapus")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

type domainStatusJSON struct {
	Domain     string   `json:"domain"`
	Primary    bool     `json:"primary"`
	PointsHere bool     `json:"points_here"`
	Resolved   []string `json:"resolved"`
	Error      string   `json:"error,omitempty"`
}

// handleSiteDomainStatus mengecek apakah tiap domain (utama + alias) sudah
// mengarah (A/AAAA) ke IP publik server ini — "sudah konek?" di panel.
func (s *server) handleSiteDomainStatus(w http.ResponseWriter, r *http.Request) {
	site, _, ok := s.ownedSite(w, r)
	if !ok {
		return
	}
	type item struct {
		d       string
		primary bool
	}
	items := []item{{site.Domain, true}}
	if aliases, err := s.d.Store.ListSiteDomains(site.ID); err == nil {
		for _, a := range aliases {
			items = append(items, item{a, false})
		}
	}
	var serverIPs []net.IP
	for _, raw := range strings.Split(s.d.PublicIP, ",") {
		if ip := net.ParseIP(strings.TrimSpace(raw)); ip != nil {
			serverIPs = append(serverIPs, ip)
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	out := make([]domainStatusJSON, 0, len(items))
	for _, it := range items {
		st := domainStatusJSON{Domain: it.d, Primary: it.primary, Resolved: []string{}}
		ips, err := net.DefaultResolver.LookupIP(ctx, "ip", it.d)
		if err != nil {
			st.Error = "resolusi DNS gagal"
		} else {
			for _, ip := range ips {
				st.Resolved = append(st.Resolved, ip.String())
				for _, want := range serverIPs {
					if ip.Equal(want) {
						st.PointsHere = true
					}
				}
			}
		}
		out = append(out, st)
	}
	writeJSON(w, http.StatusOK, map[string]any{"public_ip": s.d.PublicIP, "domains": out})
}

// handleListSiteDomains mengembalikan semua domain alias situs (owner-scoped).
func (s *server) handleListSiteDomains(w http.ResponseWriter, r *http.Request) {
	site, _, ok := s.ownedSite(w, r)
	if !ok {
		return
	}
	domains, err := s.d.Store.ListSiteDomains(site.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "gagal membaca domain")
		return
	}
	if domains == nil {
		domains = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"domains": domains})
}

type addSiteDomainReq struct {
	Domain string `json:"domain"`
}

// handleAddSiteDomain menambahkan domain alias ke situs (owner-scoped).
func (s *server) handleAddSiteDomain(w http.ResponseWriter, r *http.Request) {
	site, _, ok := s.ownedSite(w, r)
	if !ok {
		return
	}
	var req addSiteDomainReq
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "body tidak valid")
		return
	}
	// Normalisasi sekali di sini agar cek "sama dengan utama" & yang disimpan
	// konsisten (store juga menormalkan, ini defense-in-depth + kejelasan).
	req.Domain = strings.ToLower(strings.TrimSpace(req.Domain))
	if req.Domain == "" {
		writeErr(w, http.StatusBadRequest, "domain wajib diisi")
		return
	}
	if req.Domain == strings.ToLower(strings.TrimSpace(site.Domain)) {
		writeErr(w, http.StatusBadRequest, "domain sama dengan domain utama")
		return
	}
	if err := s.d.Store.AddSiteDomain(site.ID, req.Domain); err != nil {
		if strings.Contains(err.Error(), "terdaftar") {
			writeErr(w, http.StatusConflict, err.Error())
		} else {
			writeErr(w, http.StatusBadRequest, err.Error())
		}
		return
	}
	s.notifyReload()
	domains, err := s.d.Store.ListSiteDomains(site.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "gagal membaca domain")
		return
	}
	if domains == nil {
		domains = []string{}
	}
	writeJSON(w, http.StatusCreated, map[string]any{"domains": domains})
}

// handleDeleteSiteDomain menghapus domain alias dari situs (owner-scoped).
func (s *server) handleDeleteSiteDomain(w http.ResponseWriter, r *http.Request) {
	site, _, ok := s.ownedSite(w, r)
	if !ok {
		return
	}
	alias := r.PathValue("alias")
	if err := s.d.Store.DeleteSiteDomain(site.ID, alias); err != nil {
		writeErr(w, http.StatusInternalServerError, "gagal menghapus domain")
		return
	}
	s.notifyReload()
	domains, err := s.d.Store.ListSiteDomains(site.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "gagal membaca domain")
		return
	}
	if domains == nil {
		domains = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"domains": domains})
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
