package main

import (
	"path/filepath"
	"testing"

	"github.com/lamun-my-id/lamund/internal/store"
)

func TestSiteAddProxyAndEdit(t *testing.T) {
	db := filepath.Join(t.TempDir(), "t.db")

	// add proxy — operator (flag --external) boleh target loopback (jalur
	// tepercaya di host); jalur user API memblokir loopback (anti-SSRF).
	if err := siteAddProxy(db, "px.com", "127.0.0.1:3000", true); err != nil {
		t.Fatalf("siteAddProxy valid: %v", err)
	}
	// tanpa --external, target loopback ditolak (konsisten dgn jalur user)
	if err := siteAddProxy(db, "nope.com", "127.0.0.1:3000", false); err == nil {
		t.Fatal("loopback tanpa allowExternal harus ditolak")
	}
	st, _ := store.Open(db)
	defer st.Close()
	s, _ := st.GetSiteByDomain("px.com")
	if s == nil || s.Type != "proxy" || s.ProxyTarget != "http://127.0.0.1:3000" {
		t.Fatalf("proxy tidak tersimpan benar: %+v", s)
	}

	// target eksternal tanpa izin → ditolak
	if err := siteAddProxy(db, "bad.com", "10.0.0.5:80", false); err == nil {
		t.Fatal("target eksternal harus ditolak tanpa allowExternal")
	}

	// edit target (operator --external → loopback diizinkan)
	if err := siteEdit(db, "px.com", editOpts{target: "127.0.0.1:4000", allowExternal: true}); err != nil {
		t.Fatalf("edit target: %v", err)
	}
	s, _ = st.GetSiteByDomain("px.com")
	if s.ProxyTarget != "http://127.0.0.1:4000" {
		t.Fatalf("target tidak berubah: %q", s.ProxyTarget)
	}

	// edit status → disable
	if err := siteEdit(db, "px.com", editOpts{disable: true}); err != nil {
		t.Fatalf("disable: %v", err)
	}
	s, _ = st.GetSiteByDomain("px.com")
	if s.Status != "disabled" {
		t.Fatalf("status tidak berubah: %q", s.Status)
	}

	// edit domain tak ada → error
	if err := siteEdit(db, "nyasar.com", editOpts{enable: true}); err == nil {
		t.Fatal("edit domain tak ada harus error")
	}
}
