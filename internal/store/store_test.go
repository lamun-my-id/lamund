package store

import (
	"os"
	"path/filepath"
	"testing"
)

func openTemp(t *testing.T) *Store {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestCreateAndGetSite(t *testing.T) {
	st := openTemp(t)
	id, err := st.CreateSite(Site{Domain: "example.com", Type: "static", RootPath: "/tmp/x"})
	if err != nil || id == 0 {
		t.Fatalf("CreateSite: id=%d err=%v", id, err)
	}
	got, err := st.GetSiteByDomain("example.com")
	if err != nil || got == nil || got.RootPath != "/tmp/x" || got.Status != "active" {
		t.Fatalf("GetSiteByDomain: %+v err=%v", got, err)
	}
	if miss, _ := st.GetSiteByDomain("nyasar.com"); miss != nil {
		t.Fatalf("domain tak terdaftar harus nil, dapat %+v", miss)
	}
}

func TestDuplicateDomainRejected(t *testing.T) {
	st := openTemp(t)
	st.CreateSite(Site{Domain: "dup.com", Type: "static", RootPath: "/a"})
	if _, err := st.CreateSite(Site{Domain: "dup.com", Type: "static", RootPath: "/b"}); err == nil {
		t.Fatal("duplikat domain harus error")
	}
}

func TestInvalidDomainRejected(t *testing.T) {
	st := openTemp(t)
	if _, err := st.CreateSite(Site{Domain: "UPPER.com", Type: "static", RootPath: "/a"}); err == nil {
		t.Fatal("domain invalid harus ditolak CreateSite")
	}
}

func TestSiteGitFields(t *testing.T) {
	st, _ := Open(filepath.Join(t.TempDir(), "g.db"))
	defer st.Close()
	st.CreateSite(Site{Domain: "g.test", Type: "static", RepoURL: "https://github.com/a/b", Branch: "main", BuildCmd: "npm run build", OutputDir: "dist"})
	s, _ := st.GetSiteByDomain("g.test")
	if s.RepoURL == "" || s.Branch != "main" || s.BuildCmd == "" || s.OutputDir != "dist" {
		t.Fatalf("field git tak tersimpan: %+v", s)
	}
}

// TestOpenDBFilePermission memverifikasi bahwa Open menyetel izin file DB ke
// 0600 (hanya owner baca/tulis) setelah migrasi sukses.
func TestOpenDBFilePermission(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "perm.db")
	st, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open gagal: %v", err)
	}
	st.Close()

	info, err := os.Stat(dbPath)
	if err != nil {
		t.Fatalf("Stat gagal: %v", err)
	}
	got := info.Mode().Perm()
	if got != 0o600 {
		t.Errorf("izin DB file = %o, mau 0600", got)
	}
}

func TestListAndDelete(t *testing.T) {
	st := openTemp(t)
	st.CreateSite(Site{Domain: "a.com", Type: "static", RootPath: "/a"})
	st.CreateSite(Site{Domain: "b.com", Type: "static", RootPath: "/b"})
	sites, err := st.ListSites()
	if err != nil || len(sites) != 2 {
		t.Fatalf("ListSites: n=%d err=%v", len(sites), err)
	}
	if err := st.DeleteSite("a.com"); err != nil {
		t.Fatal(err)
	}
	sites, _ = st.ListSites()
	if len(sites) != 1 || sites[0].Domain != "b.com" {
		t.Fatalf("setelah delete: %+v", sites)
	}
}
