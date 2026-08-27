package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/lamun-my-id/lamund/internal/store"
)

func TestHotReloadNoRestart(t *testing.T) {
	st, _ := store.Open(filepath.Join(t.TempDir(), "t.db"))
	defer st.Close()
	st.CreateSite(store.Site{Domain: "satu.test", Type: "static", RootPath: siteDir(t, "situs satu")})

	rl, err := NewReloadable(st)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(rl.Handler())
	defer srv.Close()

	body := func(host string) (int, string) {
		req, _ := http.NewRequest("GET", srv.URL+"/", nil)
		req.Host = host
		resp, _ := http.DefaultClient.Do(req)
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return resp.StatusCode, string(b)
	}

	// site baru belum ada → 404
	if code, _ := body("dua.test"); code != 404 {
		t.Fatalf("sebelum reload dua.test harus 404, dapat %d", code)
	}
	// tambah site lalu reload — TANPA restart server
	st.CreateSite(store.Site{Domain: "dua.test", Type: "static", RootPath: siteDir(t, "situs dua")})
	if err := rl.Reload(); err != nil {
		t.Fatal(err)
	}
	if code, b := body("dua.test"); code != 200 || b != "situs dua" {
		t.Fatalf("setelah reload dua.test => %d %q", code, b)
	}
	if code, b := body("satu.test"); code != 200 || b != "situs satu" {
		t.Fatalf("site lama harus tetap jalan => %d %q", code, b)
	}
}

func TestReloadFailKeepsOldTable(t *testing.T) {
	st, _ := store.Open(filepath.Join(t.TempDir(), "t.db"))
	st.CreateSite(store.Site{Domain: "tetap.test", Type: "static", RootPath: siteDir(t, "tetap")})
	rl, err := NewReloadable(st)
	if err != nil {
		t.Fatal(err)
	}
	st.Close() // bikin ListSites error di reload berikutnya
	if err := rl.Reload(); err == nil {
		t.Fatal("reload dengan store error harus mengembalikan error")
	}
	// tabel lama harus tetap aktif (tidak jatuh)
	srv := httptest.NewServer(rl.Handler())
	defer srv.Close()
	req, _ := http.NewRequest("GET", srv.URL+"/", nil)
	req.Host = "tetap.test"
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request ke tabel lama gagal: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("tabel lama harus tetap melayani, dapat %d", resp.StatusCode)
	}
}
