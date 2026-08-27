package static

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func setup(t *testing.T) *Handler {
	t.Helper()
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "index.html"), []byte("<h1>beranda</h1>"), 0o644)
	os.MkdirAll(filepath.Join(dir, "blog"), 0o755)
	os.WriteFile(filepath.Join(dir, "blog", "index.html"), []byte("blog"), 0o644)
	os.MkdirAll(filepath.Join(dir, "kosong"), 0o755)
	// file rahasia DI LUAR root + symlink jahat ke sana
	secret := filepath.Join(t.TempDir(), "rahasia.txt")
	os.WriteFile(secret, []byte("bocor"), 0o644)
	os.Symlink(secret, filepath.Join(dir, "link.txt"))
	h, err := New(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func TestSPAFallback(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "index.html"), []byte("APP-SHELL"), 0o644)
	os.WriteFile(filepath.Join(dir, "app.js"), []byte("code"), 0o644)
	spa, _ := New(dir, true)
	plain, _ := New(dir, false)

	// deep-link rute (tanpa ekstensi) → index.html di mode SPA
	if rec := get(spa, "/dashboard/settings"); rec.Code != 200 || rec.Body.String() != "APP-SHELL" {
		t.Fatalf("SPA deep-link => %d %q, mau 200 APP-SHELL", rec.Code, rec.Body.String())
	}
	// aset yang benar-benar hilang tetap 404 (jangan kirim HTML utk .js hilang)
	if rec := get(spa, "/hilang.js"); rec.Code != 404 {
		t.Fatalf("aset hilang di SPA harus 404, dapat %d", rec.Code)
	}
	// aset yang ada tetap dilayani apa adanya
	if rec := get(spa, "/app.js"); rec.Code != 200 || rec.Body.String() != "code" {
		t.Fatalf("aset ada => %d %q", rec.Code, rec.Body.String())
	}
	// tanpa SPA, deep-link → 404 (perilaku lama)
	if rec := get(plain, "/dashboard/settings"); rec.Code != 404 {
		t.Fatalf("non-SPA deep-link harus 404, dapat %d", rec.Code)
	}
}

func get(h http.Handler, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
	return rec
}

func TestCustom404(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "index.html"), []byte("home"), 0o644)
	os.WriteFile(filepath.Join(dir, "404.html"), []byte("<h1>custom hilang</h1>"), 0o644)
	h, _ := New(dir, false)
	rec := get(h, "/tidak-ada.html")
	if rec.Code != 404 {
		t.Fatalf("status harus 404, dapat %d", rec.Code)
	}
	if rec.Body.String() != "<h1>custom hilang</h1>" {
		t.Fatalf("404.html tenant tak disajikan: %q", rec.Body.String())
	}
}

func TestDefault404WhenNoCustom(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "index.html"), []byte("home"), 0o644)
	h, _ := New(dir, false)
	if rec := get(h, "/hilang.html"); rec.Code != 404 {
		t.Fatalf("tanpa 404.html harus tetap 404, dapat %d", rec.Code)
	}
}

func TestServesIndexAndNested(t *testing.T) {
	h := setup(t)
	if rec := get(h, "/"); rec.Code != 200 || rec.Body.String() != "<h1>beranda</h1>" {
		t.Fatalf("/ => %d %q", rec.Code, rec.Body.String())
	}
	if rec := get(h, "/blog/"); rec.Code != 200 || rec.Body.String() != "blog" {
		t.Fatalf("/blog/ => %d", rec.Code)
	}
}

func TestNoListingAndMissing(t *testing.T) {
	h := setup(t)
	if rec := get(h, "/kosong/"); rec.Code != 404 {
		t.Fatalf("dir tanpa index harus 404, dapat %d", rec.Code)
	}
	if rec := get(h, "/tidak-ada.html"); rec.Code != 404 {
		t.Fatalf("file hilang harus 404, dapat %d", rec.Code)
	}
}

func TestTraversalBlocked(t *testing.T) {
	h := setup(t)
	for _, p := range []string{"/../rahasia.txt", "/..%2Frahasia.txt", "/link.txt"} {
		rec := get(h, p)
		if rec.Code == 200 {
			t.Errorf("path %q harus diblokir, dapat 200: %q", p, rec.Body.String())
		}
	}
}

func TestMethodNotAllowed(t *testing.T) {
	h := setup(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/", nil))
	if rec.Code != 405 {
		t.Fatalf("POST harus 405, dapat %d", rec.Code)
	}
}
