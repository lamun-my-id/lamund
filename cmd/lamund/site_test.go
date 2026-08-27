package main

import (
	"path/filepath"
	"testing"

	"github.com/lamun-my-id/lamund/internal/store"
)

func TestSiteAddValidatesAndPersists(t *testing.T) {
	db := filepath.Join(t.TempDir(), "t.db")
	root := t.TempDir()

	if err := siteAdd(db, "contoh.com", root); err != nil {
		t.Fatalf("siteAdd valid: %v", err)
	}
	st, _ := store.Open(db)
	defer st.Close()
	s, _ := st.GetSiteByDomain("contoh.com")
	if s == nil || s.RootPath != root || s.Type != "static" {
		t.Fatalf("site tidak tersimpan benar: %+v", s)
	}

	if err := siteAdd(db, "contoh.com", root); err == nil {
		t.Fatal("duplikat harus error")
	}
	if err := siteAdd(db, "TIDAKVALID", root); err == nil {
		t.Fatal("domain invalid harus error")
	}
	if err := siteAdd(db, "ok.com", "/path/tidak/ada"); err == nil {
		t.Fatal("root tidak ada harus error")
	}
}
