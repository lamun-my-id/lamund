package site

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestMountAppResolvesAndSurvivesPortChange menguji inti R5: route app-by-name
// di-resolve lewat resolver saat build, dan membangun ulang dengan resolver
// baru (port berubah, mis. pasca blue-green) mengalihkan ke upstream baru.
func TestMountAppResolvesAndSurvivesPortChange(t *testing.T) {
	backA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("A"))
	}))
	defer backA.Close()
	backB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("B"))
	}))
	defer backB.Close()

	cfg := Config{
		Version: ConfigVersion,
		Routes: []RouteSpec{
			{Match: MatchSpec{PathPrefix: "/api"}, Handler: HandlerSpec{App: &AppSpec{Name: "backend"}}},
			{Match: MatchSpec{PathPrefix: "/"}, Handler: HandlerSpec{Static: &StaticSpec{Root: t.TempDir()}}},
		},
	}

	// Resolver awal → backend A.
	get := func(h http.Handler, path string) string {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		return rec.Body.String()
	}

	h1, err := cfg.CompileWith(func(name string) (string, bool) {
		if name == "backend" {
			return backA.URL, true
		}
		return "", false
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := get(h1, "/api/health"); got != "A" {
		t.Fatalf("mount app harus proxy ke backend A, dapat %q", got)
	}

	// Rebuild dengan resolver baru (port berubah) → backend B.
	h2, err := cfg.CompileWith(func(name string) (string, bool) {
		if name == "backend" {
			return backB.URL, true
		}
		return "", false
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := get(h2, "/api/health"); got != "B" {
		t.Fatalf("setelah port ganti, mount app harus ikut backend B, dapat %q", got)
	}
}

// TestMountAppUnknownFails: app yg tak bisa di-resolve → compile error (site
// di-skip di data plane, tak menjatuhkan tenant lain).
func TestMountAppUnknownFails(t *testing.T) {
	cfg := Config{
		Version: ConfigVersion,
		Routes:  []RouteSpec{{Match: MatchSpec{PathPrefix: "/api"}, Handler: HandlerSpec{App: &AppSpec{Name: "ghost"}}}},
	}
	if _, err := cfg.CompileWith(func(string) (string, bool) { return "", false }); err == nil {
		t.Fatal("app tak dikenal harus gagal compile")
	}
	// resolver nil juga error (dipakai di luar data plane)
	if _, err := cfg.Compile(); err == nil {
		t.Fatal("tanpa resolver, route app harus gagal compile")
	}
}
