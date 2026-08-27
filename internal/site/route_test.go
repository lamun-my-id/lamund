package site

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/lamun-my-id/lamund/internal/store"
)

func TestMatchPrefixPerSegment(t *testing.T) {
	cases := []struct {
		prefix, path string
		want         bool
	}{
		{"", "/apa-saja", true},
		{"/", "/apa-saja", true},
		{"/api", "/api", true},
		{"/api", "/api/", true},
		{"/api", "/api/users", true},
		{"/api", "/apix", false}, // bukan batas segmen
		{"/api", "/", false},
		{"/api", "/other", false},
		{"api", "/api/x", true}, // dinormalkan ke "/api"
	}
	for _, c := range cases {
		m := Match{PathPrefix: c.prefix}
		r := httptest.NewRequest("GET", "http://h"+c.path, nil)
		if got := m.Matches(r); got != c.want {
			t.Errorf("Match{%q}.Matches(%q) = %v, mau %v", c.prefix, c.path, got, c.want)
		}
	}
}

func mark(s string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte(s)) })
}

func TestCompileFirstMatchWins(t *testing.T) {
	h := Compile([]Route{
		{Match: Match{PathPrefix: "/api"}, Handler: mark("API")},
		{Match: Match{}, Handler: mark("STATIC")}, // default terakhir
	})
	get := func(path string) string {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", "http://h"+path, nil))
		return rec.Body.String()
	}
	if got := get("/api/users"); got != "API" {
		t.Fatalf("/api/users → %q, mau API", got)
	}
	if got := get("/index.html"); got != "STATIC" {
		t.Fatalf("/index.html → %q, mau STATIC", got)
	}
	if got := get("/"); got != "STATIC" {
		t.Fatalf("/ → %q, mau STATIC", got)
	}
}

func TestCompileMiddlewareOrder(t *testing.T) {
	tag := func(name string) Middleware {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Add("X-Chain", name)
				next.ServeHTTP(w, r)
			})
		}
	}
	h := Compile([]Route{{
		Match:      Match{},
		Handler:    mark("ok"),
		Middleware: []Middleware{tag("outer"), tag("inner")},
	}})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "http://h/", nil))
	chain := rec.Header().Values("X-Chain")
	if len(chain) != 2 || chain[0] != "outer" || chain[1] != "inner" {
		t.Fatalf("urutan middleware salah: %v (mau [outer inner])", chain)
	}
}

func TestCompileNoMatch404(t *testing.T) {
	h := Compile([]Route{{Match: Match{PathPrefix: "/api"}, Handler: mark("API")}})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "http://h/lain", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("tanpa route cocok harus 404, dapat %d", rec.Code)
	}
}

func TestBuildStaticServes(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "index.html"), []byte("halo-build"), 0o644)
	h, err := Build(store.Site{Domain: "s.com", Type: "static", RootPath: dir})
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "http://s.com/", nil))
	if rec.Body.String() != "halo-build" {
		t.Fatalf("static build tak menyajikan index: %q", rec.Body.String())
	}
}

func TestBuildUnknownType(t *testing.T) {
	if _, err := Build(store.Site{Domain: "x", Type: "wut"}); err == nil {
		t.Fatal("tipe tak dikenal harus error")
	}
}

// Situs default (tanpa config) HARUS dapat middleware aman-by-default:
// security headers + ETag + Cache-Control, dan kompresi bila diminta.
func TestBuildStaticDefaultStack(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html>halo default stack</html>"), 0o644)
	h, err := Build(store.Site{Domain: "s.com", Type: "static", RootPath: dir})
	if err != nil {
		t.Fatal(err)
	}
	// tanpa gzip: cek security headers + validator cache
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "http://s.com/", nil))
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Error("situs default tak dapat security headers")
	}
	if rec.Header().Get("ETag") == "" {
		t.Error("situs default tak dapat ETag")
	}
	if rec.Header().Get("Cache-Control") == "" {
		t.Error("situs default tak dapat Cache-Control")
	}
	// dengan gzip: cek kompresi
	rec2 := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "http://s.com/", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	h.ServeHTTP(rec2, req)
	if rec2.Header().Get("Content-Encoding") != "gzip" {
		t.Errorf("situs default tak terkompresi walau Accept-Encoding: gzip (got %q)", rec2.Header().Get("Content-Encoding"))
	}
}
