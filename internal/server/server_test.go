package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/lamun-my-id/lamund/internal/store"
)

func siteDir(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "index.html"), []byte(content), 0o644)
	return dir
}

func TestVhostRoutingEndToEnd(t *testing.T) {
	st, _ := store.Open(filepath.Join(t.TempDir(), "t.db"))
	defer st.Close()
	st.CreateSite(store.Site{Domain: "a.test", Type: "static", RootPath: siteDir(t, "situs A")})
	st.CreateSite(store.Site{Domain: "b.test", Type: "static", RootPath: siteDir(t, "situs B")})
	st.CreateSite(store.Site{Domain: "mati.test", Type: "static", RootPath: siteDir(t, "X"), Status: "disabled"})

	table, err := BuildTable(st)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(New(table))
	defer srv.Close()

	body := func(host string) (int, string) {
		req, _ := http.NewRequest("GET", srv.URL+"/", nil)
		req.Host = host
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b)
	}

	if code, b := body("a.test"); code != 200 || b != "situs A" {
		t.Fatalf("a.test => %d %q", code, b)
	}
	if code, b := body("b.test"); code != 200 || b != "situs B" {
		t.Fatalf("b.test => %d %q", code, b)
	}
	if code, _ := body("mati.test"); code != 404 {
		t.Fatalf("site disabled harus 404, dapat %d", code)
	}
	if code, _ := body("asing.test"); code != 404 {
		t.Fatalf("host asing harus 404, dapat %d", code)
	}
}

func TestBrokenRootDoesNotKillServer(t *testing.T) {
	st, _ := store.Open(filepath.Join(t.TempDir(), "t.db"))
	defer st.Close()
	st.CreateSite(store.Site{Domain: "rusak.test", Type: "static", RootPath: "/path/tidak/ada"})
	st.CreateSite(store.Site{Domain: "sehat.test", Type: "static", RootPath: siteDir(t, "ok")})
	table, err := BuildTable(st)
	if err != nil {
		t.Fatalf("satu site rusak tidak boleh menggagalkan BuildTable: %v", err)
	}
	if table.Get("sehat.test") == nil {
		t.Fatal("site sehat harus tetap terdaftar")
	}
	if table.Get("rusak.test") != nil {
		t.Fatal("site rusak harus di-skip")
	}
}

func TestBuildTableAliasRouting(t *testing.T) {
	st, _ := store.Open(filepath.Join(t.TempDir(), "t.db"))
	defer st.Close()

	// Site aktif dengan 2 alias.
	id, err := st.CreateSite(store.Site{Domain: "utama.test", Type: "static", RootPath: siteDir(t, "halaman utama")})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddSiteDomain(id, "alias1.test"); err != nil {
		t.Fatal(err)
	}
	if err := st.AddSiteDomain(id, "alias2.test"); err != nil {
		t.Fatal(err)
	}

	// Site tidak aktif dengan 1 alias — aliasnya TIDAK boleh terdaftar.
	idMati, err := st.CreateSite(store.Site{Domain: "mati.test", Type: "static", RootPath: siteDir(t, "X"), Status: "disabled"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddSiteDomain(idMati, "alias-mati.test"); err != nil {
		t.Fatal(err)
	}

	table, err := BuildTable(st)
	if err != nil {
		t.Fatal(err)
	}

	// Domain utama dan kedua alias harus terdaftar dengan handler non-nil.
	for _, domain := range []string{"utama.test", "alias1.test", "alias2.test"} {
		r := table.Get(domain)
		if r == nil {
			t.Fatalf("domain %s harus terdaftar di tabel route", domain)
		}
		if r.Handler == nil {
			t.Fatalf("domain %s: handler tidak boleh nil", domain)
		}
	}

	// Semua tiga domain harus muncul di Domains() (dipakai ACME).
	domains := table.Domains()
	domainSet := make(map[string]bool, len(domains))
	for _, d := range domains {
		domainSet[d] = true
	}
	for _, want := range []string{"utama.test", "alias1.test", "alias2.test"} {
		if !domainSet[want] {
			t.Errorf("Domains() harus memuat %s, dapat: %v", want, domains)
		}
	}

	// Alias site tidak aktif TIDAK boleh terdaftar.
	if table.Get("alias-mati.test") != nil {
		t.Fatal("alias site tidak aktif tidak boleh terdaftar di tabel route")
	}

	// Verifikasi fungsional: ketiga domain melayani konten yang sama.
	srv := httptest.NewServer(New(table))
	defer srv.Close()

	body := func(host string) (int, string) {
		req, _ := http.NewRequest("GET", srv.URL+"/", nil)
		req.Host = host
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b)
	}

	for _, domain := range []string{"utama.test", "alias1.test", "alias2.test"} {
		if code, b := body(domain); code != 200 || b != "halaman utama" {
			t.Fatalf("%s => %d %q, ingin 200 \"halaman utama\"", domain, code, b)
		}
	}
}

func TestProxySiteRouting(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "dari upstream proxy")
	}))
	defer upstream.Close()

	st, _ := store.Open(filepath.Join(t.TempDir(), "t.db"))
	defer st.Close()
	st.CreateSite(store.Site{Domain: "px.test", Type: "proxy", ProxyTarget: upstream.URL})
	st.CreateSite(store.Site{Domain: "rusak.test", Type: "proxy", ProxyTarget: "://tidak-valid"})
	st.CreateSite(store.Site{Domain: "ok.test", Type: "static", RootPath: siteDir(t, "statik ok")})

	table, err := BuildTable(st)
	if err != nil {
		t.Fatalf("site proxy rusak tidak boleh menggagalkan BuildTable: %v", err)
	}
	srv := httptest.NewServer(New(table))
	defer srv.Close()
	req, _ := http.NewRequest("GET", srv.URL+"/", nil)
	req.Host = "px.test"
	resp, _ := http.DefaultClient.Do(req)
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || string(b) != "dari upstream proxy" {
		t.Fatalf("px.test => %d %q", resp.StatusCode, string(b))
	}
	if table.Get("rusak.test") != nil {
		t.Error("site proxy dengan target rusak harus di-skip")
	}
	if table.Get("ok.test") == nil {
		t.Error("site lain harus tetap terdaftar")
	}
}
