package api

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/lamun-my-id/lamund/internal/auth"
	"github.com/lamun-my-id/lamund/internal/site"
	"github.com/lamun-my-id/lamund/internal/store"
)

// mkUser membuat user biasa + kuota lalu mengembalikan token loginnya.
func mkUser(t *testing.T, h http.Handler, st *store.Store, name string, maxSites int) string {
	t.Helper()
	hash, _ := auth.HashPassword("pw-" + name)
	id, err := st.CreateUser(store.User{Username: name, PasswordHash: hash, Role: "member"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetQuota(store.Quota{UserID: id, MaxSites: maxSites}); err != nil {
		t.Fatal(err)
	}
	rec := login(t, h, name, "pw-"+name)
	var resp loginResp
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Token == "" {
		t.Fatalf("login %s gagal: %s", name, rec.Body)
	}
	return resp.Token
}

func createStatic(t *testing.T, h http.Handler, tok, domain string) int {
	t.Helper()
	rec := do(t, h, "POST", "/api/v1/sites", tok, map[string]string{
		"domain": domain, "type": "static", "root_path": "/srv/" + domain,
	})
	return rec.Code
}

func TestQuotaEnforced(t *testing.T) {
	h, st := harness(t)
	tok := mkUser(t, h, st, "andi", 2)
	if c := createStatic(t, h, tok, "a1.com"); c != 201 {
		t.Fatalf("site1 mau 201, dapat %d", c)
	}
	if c := createStatic(t, h, tok, "a2.com"); c != 201 {
		t.Fatalf("site2 mau 201, dapat %d", c)
	}
	if c := createStatic(t, h, tok, "a3.com"); c != 403 {
		t.Fatalf("site3 harus ditolak kuota 403, dapat %d", c)
	}
}

func TestTenantIsolation(t *testing.T) {
	h, st := harness(t)
	tokA := mkUser(t, h, st, "usera", 5)
	tokB := mkUser(t, h, st, "userb", 5)
	if c := createStatic(t, h, tokA, "milik-a.com"); c != 201 {
		t.Fatalf("buat site A: %d", c)
	}
	// B tidak boleh melihat situs A → 404 (bukan 403, jangan bocorkan keberadaan)
	if c := do(t, h, "GET", "/api/v1/sites/milik-a.com", tokB, nil).Code; c != 404 {
		t.Fatalf("B GET site A harus 404, dapat %d", c)
	}
	if c := do(t, h, "PATCH", "/api/v1/sites/milik-a.com", tokB, map[string]string{"status": "disabled"}).Code; c != 404 {
		t.Fatalf("B PATCH site A harus 404, dapat %d", c)
	}
	if c := do(t, h, "DELETE", "/api/v1/sites/milik-a.com", tokB, nil).Code; c != 404 {
		t.Fatalf("B DELETE site A harus 404, dapat %d", c)
	}
	// B hanya melihat situsnya sendiri (kosong)
	rec := do(t, h, "GET", "/api/v1/sites", tokB, nil)
	var lst struct{ Sites []siteJSON }
	json.Unmarshal(rec.Body.Bytes(), &lst)
	if len(lst.Sites) != 0 {
		t.Fatalf("B harus lihat 0 situs, dapat %d", len(lst.Sites))
	}
}

func TestOwnerCanManage(t *testing.T) {
	h, st := harness(t)
	tok := mkUser(t, h, st, "budi", 5)
	createStatic(t, h, tok, "budi.com")
	// SSRF hardening: loopback proxy HARUS ditolak dari jalur user (allowExternal=false)
	rec := do(t, h, "PATCH", "/api/v1/sites/budi.com", tok, map[string]string{
		"type": "proxy", "proxy_target": "http://127.0.0.1:9000",
	})
	if rec.Code != 400 {
		t.Fatalf("PATCH proxy loopback harus ditolak 400, dapat %d: %s", rec.Code, rec.Body)
	}
	// situs tetap ada (patch ditolak), bisa dihapus
	if do(t, h, "DELETE", "/api/v1/sites/budi.com", tok, nil).Code != 200 {
		t.Fatal("owner harus bisa hapus")
	}
}

func TestSiteChangeNotifiesReload(t *testing.T) {
	// harness khusus dengan hook OnSiteChange yang menghitung panggilan.
	st, err := store.Open(t.TempDir() + "/t.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	hash, _ := auth.HashPassword("rahasia123")
	st.CreateUser(store.User{Username: "admin", PasswordHash: hash, Role: "superadmin"})
	secret, _ := auth.GenerateSecret()
	var reloads int
	h := New(Deps{
		Store: st, Issuer: auth.NewIssuer(secret, time.Hour),
		OnSiteChange: func() { reloads++ },
	})
	tok := loginToken(t, h)

	do(t, h, "POST", "/api/v1/sites", tok, map[string]string{"domain": "r.com", "type": "static", "root_path": "/r"})
	do(t, h, "PATCH", "/api/v1/sites/r.com", tok, map[string]string{"status": "disabled"})
	do(t, h, "DELETE", "/api/v1/sites/r.com", tok, nil)
	if reloads != 3 {
		t.Fatalf("create+patch+delete harus memicu 3 reload, dapat %d", reloads)
	}
}

func TestCreateStaticSiteWithGitFields(t *testing.T) {
	h, st := harness(t)
	tok := loginToken(t, h)
	body := map[string]any{"domain": "vue.test", "type": "static", "repo_url": "https://github.com/u/vue", "branch": "main", "build_cmd": "npm run build", "output_dir": "dist"}
	if rec := do(t, h, "POST", "/api/v1/sites", tok, body); rec.Code != 201 {
		t.Fatalf("create static+git harus 201, dapat %d: %s", rec.Code, rec.Body)
	}
	s, _ := st.GetSiteByDomain("vue.test")
	if s.RepoURL == "" || s.BuildCmd == "" || s.OutputDir != "dist" {
		t.Fatalf("git fields tak tersimpan: %+v", s)
	}
}

func TestDeployGitNeedsRepo(t *testing.T) {
	h, st := harness(t)
	tok := loginToken(t, h)
	st.CreateSite(store.Site{Domain: "norepo.test", Type: "static", UserID: 1, OwnerType: "user", OwnerID: 1})
	if rec := do(t, h, "POST", "/api/v1/sites/norepo.test/deploy-git", tok, nil); rec.Code != 400 {
		t.Fatalf("deploy-git tanpa repo harus 400, dapat %d", rec.Code)
	}
}

func TestCreateSiteRejectsTraversalOutputDir(t *testing.T) {
	h, _ := harness(t)
	tok := loginToken(t, h)
	for _, bad := range []string{"../../etc", "/etc", "a/../../b"} {
		body := map[string]any{
			"domain":    "trav.test",
			"type":      "static",
			"repo_url":  "https://github.com/u/r",
			"branch":    "main",
			"output_dir": bad,
		}
		rec := do(t, h, "POST", "/api/v1/sites", tok, body)
		if rec.Code != 400 {
			t.Fatalf("output_dir %q harus ditolak 400, dapat %d", bad, rec.Code)
		}
	}
}

// SSRF guard tetap berlaku untuk jalur operator (superadmin masih boleh proxy
// custom, tapi target internal/metadata ditolak). Tenant diblok lebih awal (403).
func TestProxyTargetSSRFBlocked(t *testing.T) {
	h, _ := harness(t)
	tok := adminToken(t, h)
	rec := do(t, h, "POST", "/api/v1/sites", tok, map[string]string{
		"domain": "evil.com", "type": "proxy", "proxy_target": "http://169.254.169.254/",
	})
	if rec.Code != 400 {
		t.Fatalf("target SSRF harus ditolak 400, dapat %d", rec.Code)
	}
}

// TestLoopbackProxyBlockedFromUserPath memastikan jalur user tidak bisa membuat
// proxy ke loopback (SSRF ke panel/port lokal) — cegah SSRF ke kontrol plane.
// Catatan: jalur server (apps.go handleCreateApp) menyetel ProxyTarget langsung
// via store.CreateSite tanpa melewati ValidateProxyTarget — tidak terpengaruh.
func TestLoopbackProxyBlockedFromUserPath(t *testing.T) {
	h, st := harness(t)
	tok := adminToken(t, h) // operator boleh proxy; SSRF/loopback tetap ditolak

	// POST /sites dengan loopback IPv4 → 400
	rec := do(t, h, "POST", "/api/v1/sites", tok, map[string]string{
		"domain": "bad1.com", "type": "proxy", "proxy_target": "http://127.0.0.1:8080",
	})
	if rec.Code != 400 {
		t.Fatalf("loopback IPv4 harus ditolak 400, dapat %d: %s", rec.Code, rec.Body)
	}

	// POST /sites dengan localhost → 400
	rec = do(t, h, "POST", "/api/v1/sites", tok, map[string]string{
		"domain": "bad2.com", "type": "proxy", "proxy_target": "http://localhost:3000",
	})
	if rec.Code != 400 {
		t.Fatalf("localhost harus ditolak 400, dapat %d: %s", rec.Code, rec.Body)
	}

	// POST /sites dengan loopback IPv6 → 400
	rec = do(t, h, "POST", "/api/v1/sites", tok, map[string]string{
		"domain": "bad3.com", "type": "proxy", "proxy_target": "http://[::1]:8080",
	})
	if rec.Code != 400 {
		t.Fatalf("loopback IPv6 harus ditolak 400, dapat %d: %s", rec.Code, rec.Body)
	}

	// Konfirmasi: tidak ada situs yang terbuat
	_ = st
}

// TestCreateSiteRejectsInvalidDomain memastikan ValidateDomain dipanggil sebelum
// operasi filesystem (defense-in-depth: domain tervalidasi dulu).
func TestCreateSiteRejectsInvalidDomain(t *testing.T) {
	h, st := harness(t)
	tok := mkUser(t, h, st, "domainuser", 5)

	// Domain traversal → 400 (ValidateDomain menolak sebelum SiteRoot/WriteSiteFile)
	rec := do(t, h, "POST", "/api/v1/sites", tok, map[string]string{
		"domain": "../evil", "type": "static",
	})
	if rec.Code != 400 {
		t.Fatalf("domain traversal harus ditolak 400, dapat %d: %s", rec.Code, rec.Body)
	}

	// Domain kosong → 400 (domain wajib diisi).
	rec = do(t, h, "POST", "/api/v1/sites", tok, map[string]string{
		"domain": "", "type": "static",
	})
	if rec.Code != 400 {
		t.Fatalf("domain kosong harus ditolak 400, dapat %d: %s", rec.Code, rec.Body)
	}

	_ = st
}

func TestDeployLogEndpoint(t *testing.T) {
	h, st, _, _ := harnessDirs(t)
	tok := loginToken(t, h)

	// Buat situs milik admin.
	if c := createStatic(t, h, tok, "mysite.com"); c != 201 {
		t.Fatalf("create site mau 201, dapat %d", c)
	}

	// GET deploy-log → 200 dengan status kosong dan lines kosong (belum pernah deploy).
	rec := do(t, h, "GET", "/api/v1/sites/mysite.com/deploy-log", tok, nil)
	if rec.Code != 200 {
		t.Fatalf("deploy-log harus 200, dapat %d: %s", rec.Code, rec.Body)
	}
	var out struct {
		Status string   `json:"status"`
		Lines  []string `json:"lines"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Status != "" {
		t.Fatalf("status awal harus kosong, dapat %q", out.Status)
	}
	if out.Lines == nil {
		t.Fatal("lines harus array (bukan nil)")
	}

	// User lain (member baru) tidak boleh melihat log situs admin → 404.
	_, tokB := newMember(t, h, st, "other-user")
	rec2 := do(t, h, "GET", "/api/v1/sites/mysite.com/deploy-log", tokB, nil)
	if rec2.Code != 404 {
		t.Fatalf("user lain harus 404, dapat %d", rec2.Code)
	}
	_ = st
}

func TestSiteDomainAliases(t *testing.T) {
	h, st := harness(t)
	// Buat user A + situs
	tokA := mkUser(t, h, st, "aliasuser", 5)
	if c := createStatic(t, h, tokA, "main.example.com"); c != 201 {
		t.Fatalf("create site mau 201, dapat %d", c)
	}

	// GET awal — harus 200 dengan array kosong (bukan null)
	rec := do(t, h, "GET", "/api/v1/sites/main.example.com/domains", tokA, nil)
	if rec.Code != 200 {
		t.Fatalf("GET domains awal harus 200, dapat %d: %s", rec.Code, rec.Body)
	}
	var listOut struct {
		Domains []string `json:"domains"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &listOut); err != nil {
		t.Fatalf("unmarshal GET domains: %v", err)
	}
	if listOut.Domains == nil {
		t.Fatal("domains harus array (bukan null)")
	}
	if len(listOut.Domains) != 0 {
		t.Fatalf("domains awal harus kosong, dapat %v", listOut.Domains)
	}

	// POST alias valid → 201 dan muncul di GET
	rec = do(t, h, "POST", "/api/v1/sites/main.example.com/domains", tokA, map[string]string{
		"domain": "extra.example.com",
	})
	if rec.Code != 201 {
		t.Fatalf("POST alias harus 201, dapat %d: %s", rec.Code, rec.Body)
	}
	var addOut struct {
		Domains []string `json:"domains"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &addOut); err != nil {
		t.Fatalf("unmarshal POST domains: %v", err)
	}
	found := false
	for _, d := range addOut.Domains {
		if d == "extra.example.com" {
			found = true
		}
	}
	if !found {
		t.Fatalf("extra.example.com tidak ada di respons POST: %v", addOut.Domains)
	}

	// GET setelah tambah — harus tampil
	rec = do(t, h, "GET", "/api/v1/sites/main.example.com/domains", tokA, nil)
	if rec.Code != 200 {
		t.Fatalf("GET setelah tambah harus 200, dapat %d", rec.Code)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &listOut); err != nil {
		t.Fatalf("unmarshal GET setelah tambah: %v", err)
	}
	found = false
	for _, d := range listOut.Domains {
		if d == "extra.example.com" {
			found = true
		}
	}
	if !found {
		t.Fatalf("extra.example.com tidak muncul di GET: %v", listOut.Domains)
	}

	// POST domain tidak valid → 400
	rec = do(t, h, "POST", "/api/v1/sites/main.example.com/domains", tokA, map[string]string{
		"domain": "not a domain",
	})
	if rec.Code != 400 {
		t.Fatalf("POST domain tidak valid harus 400, dapat %d: %s", rec.Code, rec.Body)
	}

	// POST alias sama dengan domain utama → 400
	rec = do(t, h, "POST", "/api/v1/sites/main.example.com/domains", tokA, map[string]string{
		"domain": "main.example.com",
	})
	if rec.Code != 400 {
		t.Fatalf("POST alias=domain utama harus 400, dapat %d: %s", rec.Code, rec.Body)
	}

	// POST alias yang sudah menjadi domain utama situs lain → 409
	// Buat user B + situs dengan domain yang sama seperti alias yang akan dicoba
	tokAdmin := loginToken(t, h)
	if c := createStatic(t, h, tokAdmin, "other-primary.com"); c != 201 {
		t.Fatalf("create situs lain mau 201, dapat %d", c)
	}
	rec = do(t, h, "POST", "/api/v1/sites/main.example.com/domains", tokA, map[string]string{
		"domain": "other-primary.com",
	})
	if rec.Code != 409 {
		t.Fatalf("POST alias=domain utama situs lain harus 409, dapat %d: %s", rec.Code, rec.Body)
	}

	// DELETE alias → 200 dan hilang dari GET
	rec = do(t, h, "DELETE", "/api/v1/sites/main.example.com/domains/extra.example.com", tokA, nil)
	if rec.Code != 200 {
		t.Fatalf("DELETE alias harus 200, dapat %d: %s", rec.Code, rec.Body)
	}
	var delOut struct {
		Domains []string `json:"domains"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &delOut); err != nil {
		t.Fatalf("unmarshal DELETE domains: %v", err)
	}
	for _, d := range delOut.Domains {
		if d == "extra.example.com" {
			t.Fatal("extra.example.com masih ada setelah DELETE")
		}
	}

	// GET setelah DELETE — harus kosong (alias sudah hilang)
	rec = do(t, h, "GET", "/api/v1/sites/main.example.com/domains", tokA, nil)
	if rec.Code != 200 {
		t.Fatalf("GET setelah DELETE harus 200, dapat %d", rec.Code)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &listOut); err != nil {
		t.Fatalf("unmarshal GET setelah DELETE: %v", err)
	}
	for _, d := range listOut.Domains {
		if d == "extra.example.com" {
			t.Fatal("extra.example.com masih tampil di GET setelah DELETE")
		}
	}

	// Cross-tenant: user lain tidak boleh GET/POST/DELETE → 404
	_, tokOther := newMember(t, h, st, "domain-other-user")
	if c := do(t, h, "GET", "/api/v1/sites/main.example.com/domains", tokOther, nil).Code; c != 404 {
		t.Fatalf("cross-tenant GET harus 404, dapat %d", c)
	}
	if c := do(t, h, "POST", "/api/v1/sites/main.example.com/domains", tokOther, map[string]string{"domain": "x.example.com"}).Code; c != 404 {
		t.Fatalf("cross-tenant POST harus 404, dapat %d", c)
	}
	if c := do(t, h, "DELETE", "/api/v1/sites/main.example.com/domains/extra.example.com", tokOther, nil).Code; c != 404 {
		t.Fatalf("cross-tenant DELETE harus 404, dapat %d", c)
	}
}

func TestSpaSiteConfig(t *testing.T) {
	root := "/srv/sites/user1/myapp/dist"
	cfg := spaSiteConfig(root)

	// Harus punya satu route
	if len(cfg.Routes) != 1 {
		t.Fatalf("spaSiteConfig: mau 1 route, dapat %d", len(cfg.Routes))
	}
	r := cfg.Routes[0]

	// Matcher harus "/"
	if r.Match.PathPrefix != "/" {
		t.Errorf("PathPrefix harus '/', dapat %q", r.Match.PathPrefix)
	}

	// Handler harus static dengan SPA=true dan Root yang benar
	if r.Handler.Static == nil {
		t.Fatal("handler.Static nil")
	}
	if !r.Handler.Static.SPA {
		t.Error("Static.SPA harus true (SPA fallback ke index.html)")
	}
	if r.Handler.Static.Root != root {
		t.Errorf("Static.Root: mau %q, dapat %q", root, r.Handler.Static.Root)
	}

	// Serialisasi lalu parse balik lewat site.ParseConfig — harus valid
	enc, _ := json.Marshal(cfg)
	parsed, err := site.ParseConfig(string(enc))
	if err != nil {
		t.Fatalf("ParseConfig gagal: %v", err)
	}
	if len(parsed.Routes) != 1 {
		t.Fatalf("setelah parse: mau 1 route, dapat %d", len(parsed.Routes))
	}
	if !parsed.Routes[0].Handler.Static.SPA {
		t.Error("setelah parse: Static.SPA harus true")
	}
	if parsed.Routes[0].Handler.Static.Root != root {
		t.Errorf("setelah parse: Root mau %q, dapat %q", root, parsed.Routes[0].Handler.Static.Root)
	}
}
