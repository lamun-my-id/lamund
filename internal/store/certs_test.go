package store

import (
	"path/filepath"
	"testing"
)

func TestCertUpsertGetList(t *testing.T) {
	st := openTemp(t)

	// upsert baru
	if err := st.UpsertCert(CertInfo{Domain: "a.com", Issuer: "Pebble", NotAfter: "2026-11-01T00:00:00Z", Status: "valid"}); err != nil {
		t.Fatal(err)
	}
	got, err := st.GetCert("a.com")
	if err != nil || got == nil || got.Issuer != "Pebble" || got.Status != "valid" {
		t.Fatalf("GetCert: %+v err=%v", got, err)
	}
	if miss, _ := st.GetCert("tidakada.com"); miss != nil {
		t.Fatalf("cert tak ada harus nil, dapat %+v", miss)
	}

	// upsert lagi (domain sama) = update, bukan duplikat
	if err := st.UpsertCert(CertInfo{Domain: "a.com", Issuer: "Pebble", NotAfter: "2027-01-01T00:00:00Z", Status: "valid"}); err != nil {
		t.Fatal(err)
	}
	st.UpsertCert(CertInfo{Domain: "b.com", Issuer: "LE", NotAfter: "2026-10-01T00:00:00Z", Status: "pending"})
	certs, err := st.ListCerts()
	if err != nil || len(certs) != 2 {
		t.Fatalf("ListCerts: n=%d err=%v", len(certs), err)
	}
	if a, _ := st.GetCert("a.com"); a.NotAfter != "2027-01-01T00:00:00Z" {
		t.Fatalf("upsert kedua tidak meng-update: %+v", a)
	}
}

func TestCertMigrationFromExistingDB(t *testing.T) {
	// buka DB, tutup, buka lagi → migrasi idempoten (tak error, tabel cert ada)
	path := filepath.Join(t.TempDir(), "m.db")
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	st.CreateSite(Site{Domain: "x.com", Type: "static", RootPath: "/x"})
	st.Close()

	st2, err := Open(path) // migrasi jalan ulang tanpa error
	if err != nil {
		t.Fatalf("buka ulang: %v", err)
	}
	defer st2.Close()
	if err := st2.UpsertCert(CertInfo{Domain: "x.com", Status: "valid"}); err != nil {
		t.Fatalf("tabel certificates harus ada setelah migrasi: %v", err)
	}
}
