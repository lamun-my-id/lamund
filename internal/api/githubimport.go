package api

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/lamun-my-id/lamund/internal/gitdeploy"
)

// repoNameRe membatasi nama repo GitHub ke karakter aman (huruf/angka/._-),
// maks 100 — sekaligus mencegah flag-injection ke argv git saat seed.
var repoNameRe = regexp.MustCompile(`^[A-Za-z0-9._-]{1,100}$`)

// registerGitHubWebhook memasang webhook push di repo {fullName} yang menunjuk
// ke endpoint hook Lamund untuk site tsb. Best-effort dari sisi pemanggil.
func (s *server) registerGitHubWebhook(token, fullName, domain, secret string) error {
	hookURL := strings.TrimRight(s.d.BaseURL, "/") + "/api/v1/hooks/git/" + domain
	_, code, err := githubAPI("POST", token, "/repos/"+fullName+"/hooks", map[string]any{
		"name":   "web",
		"active": true,
		"events": []string{"push"},
		"config": map[string]any{"url": hookURL, "content_type": "json", "secret": secret},
	})
	if err != nil {
		return err
	}
	if code != 201 {
		return fmt.Errorf("status %d", code)
	}
	return nil
}

type createRepoReq struct {
	Name      string `json:"name"`
	Private   bool   `json:"private"`
	Branch    string `json:"branch"`
	BuildCmd  string `json:"build_cmd"`
	OutputDir string `json:"output_dir"`
}

// handleCreateRepoForSite (flow "bikin repo" ala Vercel): bikin repo baru di
// akun GitHub caller, push konten site sekarang sebagai commit pertama, hubungkan
// site ke repo itu, pasang webhook push, lalu picu deploy awal. Semuanya pakai
// token GitHub milik caller — repo dimiliki caller di GitHub.
func (s *server) handleCreateRepoForSite(w http.ResponseWriter, r *http.Request) {
	site, u, ok := s.ownedSite(w, r)
	if !ok {
		return
	}
	if site.Type != "static" {
		writeErr(w, http.StatusBadRequest, "hanya situs static yang bisa dibuatkan repo")
		return
	}
	if site.RepoURL != "" {
		writeErr(w, http.StatusConflict, "situs ini sudah terhubung ke repo")
		return
	}
	conn, err := s.d.Store.GetConnection(u.ID, "github")
	if err != nil || conn == nil || conn.Token == "" {
		writeErr(w, http.StatusBadRequest, "hubungkan GitHub dulu")
		return
	}

	var req createRepoReq
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "body tidak valid")
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = slugify(site.Domain)
	}
	if !repoNameRe.MatchString(name) {
		writeErr(w, http.StatusBadRequest, "nama repo tidak valid (huruf/angka/._-, maks 100)")
		return
	}
	branch := req.Branch
	if branch == "" {
		branch = "main"
	}
	if !validBranch(branch) {
		writeErr(w, http.StatusBadRequest, "nama branch tidak valid")
		return
	}
	if req.OutputDir != "" && (filepath.IsAbs(req.OutputDir) || strings.Contains(req.OutputDir, "..")) {
		writeErr(w, http.StatusBadRequest, "output_dir tidak valid")
		return
	}
	outDir := req.OutputDir
	if outDir == "" {
		if req.BuildCmd != "" {
			outDir = "dist"
		} else {
			outDir = "."
		}
	}

	// 1. Bikin repo di GitHub (kosong, tanpa auto-init).
	body, code, err := githubAPI("POST", conn.Token, "/user/repos", map[string]any{
		"name": name, "private": req.Private, "auto_init": false,
	})
	if err != nil {
		writeErr(w, http.StatusBadGateway, "gagal menghubungi GitHub")
		return
	}
	if code == http.StatusUnprocessableEntity { // 422: nama sudah ada / invalid
		writeErr(w, http.StatusConflict, "nama repo sudah dipakai di GitHub")
		return
	}
	if code != http.StatusCreated {
		writeErr(w, http.StatusBadGateway, fmt.Sprintf("GitHub menolak buat repo (status %d)", code))
		return
	}
	var repo struct {
		FullName string `json:"full_name"`
		CloneURL string `json:"clone_url"`
	}
	if err := json.Unmarshal(body, &repo); err != nil || repo.CloneURL == "" {
		writeErr(w, http.StatusBadGateway, "respons GitHub tak terduga")
		return
	}

	// 2. Seed: push konten site sekarang jadi commit pertama.
	if s.d.Sites != nil {
		dir := s.d.Sites.SiteRoot(site.UserID, site.Domain)
		email := u.Email
		if email == "" {
			email = githubPrimaryEmail(conn.Token)
		}
		login, _, _ := githubUser(conn.Token)
		authed := s.authedRepoURL(u.ID, repo.CloneURL)
		if err := gitdeploy.Seed(authed, branch, dir, login, email, io.Discard); err != nil {
			log.Printf("create-repo: seed push gagal utk %s: %v", repo.FullName, err)
			writeErr(w, http.StatusBadGateway, "repo dibuat tapi gagal push konten awal")
			return
		}
	}

	// 3. Connect: simpan koneksi git ke site.
	if err := s.d.Store.SetSiteGit(site.Domain, repo.CloneURL, branch, req.BuildCmd, outDir); err != nil {
		writeErr(w, http.StatusInternalServerError, "gagal menyimpan koneksi git")
		return
	}

	// 4. Webhook push (best-effort — deploy tetap jalan bila gagal).
	secret, _ := s.d.Store.GetSiteWebhookSecret(site.Domain)
	if secret == "" {
		secret = randomHex(20)
		_ = s.d.Store.SetSiteWebhookSecret(site.Domain, secret)
	}
	if err := s.registerGitHubWebhook(conn.Token, repo.FullName, site.Domain, secret); err != nil {
		log.Printf("create-repo: pasang webhook gagal utk %s: %v", repo.FullName, err)
	}

	// 5. Deploy awal (clone balik dari GitHub → konsisten dgn pipeline biasa).
	updated, err := s.d.Store.GetSiteByDomain(site.Domain)
	if err != nil || updated == nil {
		writeErr(w, http.StatusInternalServerError, "gagal memuat situs")
		return
	}
	log.Printf("create-repo: %s → repo %s (private=%t), deploy awal dimulai", site.Domain, repo.FullName, req.Private)
	go s.deploySiteGit(*updated, "manual")
	writeJSON(w, http.StatusAccepted, toJSON(*updated))
}
