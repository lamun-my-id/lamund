package site

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/lamun-my-id/lamund/internal/store"
)

func TestParseConfigInvalid(t *testing.T) {
	for _, bad := range []string{
		``,                           // bukan JSON
		`{"version":99,"routes":[]}`, // versi tak didukung
		`{"version":1,"routes":[]}`,  // tanpa route
		`{"version":1}`,              // tanpa route
	} {
		if _, err := ParseConfig(bad); err == nil {
			t.Fatalf("config %q harus error", bad)
		}
	}
}

func TestConfigHybridStaticPlusApi(t *testing.T) {
	// backend upstream tiruan untuk /api
	backend := httptest.NewServer(nil)
	defer backend.Close()

	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "index.html"), []byte("STATIC-ROOT"), 0o644)

	cfg := `{"version":1,"routes":[
		{"match":{"path_prefix":"/api"},"handler":{"proxy":{"upstream":"` + backend.URL + `"}}},
		{"match":{"path_prefix":"/"},"handler":{"static":{"root":"` + dir + `"}}}
	]}`

	h, err := Build(store.Site{Domain: "hy.com", Type: "static", Config: cfg})
	if err != nil {
		t.Fatalf("build config: %v", err)
	}

	// "/" → static
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "http://hy.com/", nil))
	if rec.Body.String() != "STATIC-ROOT" {
		t.Fatalf("/ harus static, dapat %q", rec.Body.String())
	}
	// "/api/..." → diteruskan ke backend (bukan static → tak 'STATIC-ROOT')
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "http://hy.com/api/ping", nil))
	if rec.Body.String() == "STATIC-ROOT" {
		t.Fatal("/api harus ke proxy, bukan static")
	}
}

func TestConfigHandlerMustBeExactlyOne(t *testing.T) {
	bad := `{"version":1,"routes":[{"match":{"path_prefix":"/"},"handler":{}}]}`
	if _, err := Build(store.Site{Config: bad}); err == nil {
		t.Fatal("handler kosong harus error")
	}
}

func TestEmptyConfigFallsBackToLegacy(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "index.html"), []byte("LEGACY"), 0o644)
	h, err := Build(store.Site{Domain: "l.com", Type: "static", RootPath: dir, Config: ""})
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "http://l.com/", nil))
	if rec.Body.String() != "LEGACY" {
		t.Fatalf("config kosong harus pakai sintesis legacy, dapat %q", rec.Body.String())
	}
}
