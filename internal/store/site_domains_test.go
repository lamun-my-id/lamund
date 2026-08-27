package store

import (
	"strings"
	"testing"
)

// setupSiteDomainTest membuka store sementara, membuat satu user dan satu site,
// mengembalikan store, siteID, dan primary domain situs.
func setupSiteDomainTest(t *testing.T) (*Store, int64, string) {
	t.Helper()
	st := openTemp(t)
	uid, err := st.CreateUser(User{Username: "testuser", PasswordHash: "h", Role: "member"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	const primary = "primary.example.com"
	siteID, err := st.CreateSite(Site{Domain: primary, Type: "static", RootPath: "/var/www", UserID: uid})
	if err != nil {
		t.Fatalf("CreateSite: %v", err)
	}
	return st, siteID, primary
}

func TestAddSiteDomain_Success(t *testing.T) {
	st, siteID, _ := setupSiteDomainTest(t)
	if err := st.AddSiteDomain(siteID, "alias.example.com"); err != nil {
		t.Fatalf("AddSiteDomain harus sukses: %v", err)
	}
}

func TestListSiteDomains_Sorted(t *testing.T) {
	st, siteID, _ := setupSiteDomainTest(t)
	domains := []string{"zebra.example.com", "apple.example.com", "mango.example.com"}
	for _, d := range domains {
		if err := st.AddSiteDomain(siteID, d); err != nil {
			t.Fatalf("AddSiteDomain(%s): %v", d, err)
		}
	}
	got, err := st.ListSiteDomains(siteID)
	if err != nil {
		t.Fatalf("ListSiteDomains: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("mau 3 alias, dapat %d: %v", len(got), got)
	}
	// Harus terurut ascending
	for i := 1; i < len(got); i++ {
		if got[i] < got[i-1] {
			t.Fatalf("urutan tidak ascending: %v", got)
		}
	}
	if got[0] != "apple.example.com" {
		t.Fatalf("urutan pertama harus apple, dapat %q", got[0])
	}
}

func TestAddSiteDomain_PrimaryDomainRejected(t *testing.T) {
	st, siteID, primary := setupSiteDomainTest(t)
	err := st.AddSiteDomain(siteID, primary)
	if err == nil {
		t.Fatal("menambah domain utama situs sebagai alias harus error")
	}
	if !strings.Contains(err.Error(), "sudah terdaftar") {
		t.Fatalf("pesan error tidak sesuai: %v", err)
	}
}

func TestAddSiteDomain_DuplicateAliasRejected(t *testing.T) {
	st, siteID, _ := setupSiteDomainTest(t)
	const alias = "dup.example.com"
	if err := st.AddSiteDomain(siteID, alias); err != nil {
		t.Fatalf("AddSiteDomain pertama harus sukses: %v", err)
	}
	err := st.AddSiteDomain(siteID, alias)
	if err == nil {
		t.Fatal("alias duplikat harus error")
	}
	if !strings.Contains(err.Error(), "sudah terdaftar") {
		t.Fatalf("pesan error tidak sesuai: %v", err)
	}
}

func TestAddSiteDomain_CrossSiteConflict(t *testing.T) {
	st, siteID, _ := setupSiteDomainTest(t)
	// Buat situs kedua
	siteID2, err := st.CreateSite(Site{Domain: "second.example.com", Type: "static", RootPath: "/var/www2"})
	if err != nil {
		t.Fatalf("CreateSite kedua: %v", err)
	}
	const alias = "shared.example.com"
	if err := st.AddSiteDomain(siteID, alias); err != nil {
		t.Fatalf("AddSiteDomain ke situs pertama harus sukses: %v", err)
	}
	// Coba daftarkan alias yang sama ke situs kedua — harus ditolak
	err = st.AddSiteDomain(siteID2, alias)
	if err == nil {
		t.Fatal("alias yang sudah dipakai situs lain harus error")
	}
	if !strings.Contains(err.Error(), "sudah terdaftar") {
		t.Fatalf("pesan error tidak sesuai: %v", err)
	}
}

// TestCreateSite_RejectsExistingAlias menjaga arah sebaliknya: domain utama baru
// tak boleh menabrak alias situs lain (urutan "alias dulu, primary belakangan").
func TestCreateSite_RejectsExistingAlias(t *testing.T) {
	st, siteID, _ := setupSiteDomainTest(t)
	const shared = "collide.example.com"
	if err := st.AddSiteDomain(siteID, shared); err != nil {
		t.Fatalf("AddSiteDomain: %v", err)
	}
	// Buat situs baru dengan domain utama == alias yang sudah ada → harus ditolak.
	_, err := st.CreateSite(Site{Domain: shared, Type: "static", RootPath: "/x"})
	if err == nil {
		t.Fatal("membuat situs dengan domain == alias situs lain harus error")
	}
	if !strings.Contains(err.Error(), "sudah terdaftar") {
		t.Fatalf("pesan error tidak sesuai: %v", err)
	}
}

func TestAllSiteDomains_Grouped(t *testing.T) {
	st, siteID, _ := setupSiteDomainTest(t)
	siteID2, err := st.CreateSite(Site{Domain: "other.example.com", Type: "static", RootPath: "/other"})
	if err != nil {
		t.Fatalf("CreateSite kedua: %v", err)
	}
	must0(t, st.AddSiteDomain(siteID, "b.example.com"))
	must0(t, st.AddSiteDomain(siteID, "a.example.com"))
	must0(t, st.AddSiteDomain(siteID2, "c.example.com"))

	all, err := st.AllSiteDomains()
	if err != nil {
		t.Fatalf("AllSiteDomains: %v", err)
	}
	if len(all[siteID]) != 2 {
		t.Fatalf("siteID harus punya 2 alias, dapat %d: %v", len(all[siteID]), all[siteID])
	}
	if all[siteID][0] != "a.example.com" {
		t.Fatalf("alias siteID harus terurut: %v", all[siteID])
	}
	if len(all[siteID2]) != 1 || all[siteID2][0] != "c.example.com" {
		t.Fatalf("siteID2 harus punya 1 alias: %v", all[siteID2])
	}
}

func TestDeleteSiteDomain_RemovesAlias(t *testing.T) {
	st, siteID, _ := setupSiteDomainTest(t)
	const alias = "del.example.com"
	must0(t, st.AddSiteDomain(siteID, alias))

	list, _ := st.ListSiteDomains(siteID)
	if len(list) != 1 {
		t.Fatalf("mau 1 alias, dapat %d", len(list))
	}

	must0(t, st.DeleteSiteDomain(siteID, alias))

	list, _ = st.ListSiteDomains(siteID)
	if len(list) != 0 {
		t.Fatalf("setelah delete mau 0 alias, dapat %d", len(list))
	}
}

func TestDeleteSiteDomain_MissingRowIsNoop(t *testing.T) {
	st, siteID, _ := setupSiteDomainTest(t)
	// Menghapus alias yang tidak ada harus tidak error
	if err := st.DeleteSiteDomain(siteID, "ghost.example.com"); err != nil {
		t.Fatalf("delete alias tidak ada harus no-op, bukan error: %v", err)
	}
}

func TestDeleteSite_RemovesAliases(t *testing.T) {
	st, siteID, primary := setupSiteDomainTest(t)
	const alias = "gone.example.com"
	must0(t, st.AddSiteDomain(siteID, alias))

	// Pastikan alias ada sebelum delete
	list, _ := st.ListSiteDomains(siteID)
	if len(list) != 1 {
		t.Fatalf("mau 1 alias sebelum delete, dapat %d", len(list))
	}

	// Hapus situs utama — harus ikut menghapus alias
	if err := st.DeleteSite(primary); err != nil {
		t.Fatalf("DeleteSite: %v", err)
	}

	// Buat situs baru dengan domain berbeda lalu coba pakai alias yang sama
	siteID2, err := st.CreateSite(Site{Domain: "new.example.com", Type: "static", RootPath: "/new"})
	if err != nil {
		t.Fatalf("CreateSite situs baru: %v", err)
	}
	// Alias harus bebas digunakan kembali karena sudah terhapus bersama situs lama
	if err := st.AddSiteDomain(siteID2, alias); err != nil {
		t.Fatalf("alias bekas situs yang dihapus harus bisa didaftarkan ulang: %v", err)
	}
}
