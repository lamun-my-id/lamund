package site

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lamun-my-id/lamund/internal/store"
)

func TestRedirectSite(t *testing.T) {
	h, err := Build(store.Site{Domain: "lamund.web.id", Type: "redirect", ProxyTarget: "https://lamund.my.id"})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", "http://lamund.web.id/login?next=/sites", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("harus 302, dapat %d", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if loc != "https://lamund.my.id/login?next=/sites" {
		t.Fatalf("Location salah, pertahankan path+query: %q", loc)
	}
}
