package main

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lamun-my-id/lamund/internal/store"
)

func TestRedirectHTTPS(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "http://situs.test/path?x=1", nil)
	req.Host = "situs.test"
	redirectHTTPS(rec, req)
	if rec.Code != http.StatusMovedPermanently {
		t.Fatalf("code=%d, mau 301", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "https://situs.test/path?x=1" {
		t.Fatalf("Location=%q", loc)
	}
}

func TestParseIPs(t *testing.T) {
	got := parseIPs("168.110.200.136, , 10.0.0.1 ,bukan-ip")
	if len(got) != 2 {
		t.Fatalf("n=%d, mau 2 (yang valid saja): %v", len(got), got)
	}
	if got[0].String() != "168.110.200.136" || got[1].String() != "10.0.0.1" {
		t.Fatalf("hasil: %v", got)
	}
}

func storeOpenTemp(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestCertDomains(t *testing.T) {
	st := storeOpenTemp(t)
	uid, _ := st.CreateUser(store.User{Username: "u", PasswordHash: "h", Role: "member"})
	st.CreateDNSZoneWithSettings(store.DNSZone{Domain: "managed.com", UserID: uid}, store.DNSSettings{})
	active := []string{"blog.managed.com", "app.managed.com", "other.example.net"}
	got := certDomains(st, active, true)
	// managed.com → wildcard+apex (bukan tiap host); other.example.net apa adanya
	want := map[string]bool{"*.managed.com": true, "managed.com": true, "other.example.net": true}
	if len(got) != len(want) {
		t.Fatalf("mau %v, dapat %v", want, got)
	}
	for _, d := range got {
		if !want[d] {
			t.Fatalf("domain tak diharapkan: %s (got=%v)", d, got)
		}
	}
	// dnsOn=false → semua host apa adanya (tanpa wildcard)
	got2 := certDomains(st, active, false)
	for _, d := range got2 {
		if strings.HasPrefix(d, "*.") {
			t.Fatalf("tanpa --dns tak boleh wildcard: %v", got2)
		}
	}

	// Subdomain berlapis tak ditutup wildcard single-label → host eksak dipertahankan.
	got3 := certDomains(st, []string{"a.b.managed.com"}, true)
	want3 := map[string]bool{"*.managed.com": true, "managed.com": true, "a.b.managed.com": true}
	if len(got3) != len(want3) {
		t.Fatalf("subdomain berlapis: mau %v, dapat %v", want3, got3)
	}
	for _, d := range got3 {
		if !want3[d] {
			t.Fatalf("subdomain berlapis, domain tak diharapkan: %s (got=%v)", d, got3)
		}
	}
}
