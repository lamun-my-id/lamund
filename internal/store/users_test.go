package store

import (
	"path/filepath"
	"testing"
)

func TestRoleMigrationAndNewRoles(t *testing.T) {
	st := openTemp(t)
	// superadmin (ex-admin) & member (ex-user) bisa dibuat dgn nilai baru
	if _, err := st.CreateUser(User{Username: "boss", PasswordHash: "h", Role: "superadmin"}); err != nil {
		t.Fatalf("superadmin harus valid: %v", err)
	}
	if _, err := st.CreateUser(User{Username: "tm", PasswordHash: "h", Role: "team_manager"}); err != nil {
		t.Fatalf("team_manager harus valid: %v", err)
	}
	if _, err := st.CreateUser(User{Username: "mem", PasswordHash: "h", Role: "member"}); err != nil {
		t.Fatalf("member harus valid: %v", err)
	}
}

func TestUserCRUD(t *testing.T) {
	st := openTemp(t)
	id, err := st.CreateUser(User{Username: "rani", PasswordHash: "hash1", Role: "superadmin"})
	if err != nil || id == 0 {
		t.Fatalf("CreateUser: %v", err)
	}
	u, _ := st.GetUserByUsername("rani")
	if u == nil || u.Role != "superadmin" || u.PasswordHash != "hash1" || u.Disabled {
		t.Fatalf("GetUserByUsername: %+v", u)
	}
	if byID, _ := st.GetUserByID(id); byID == nil || byID.Username != "rani" {
		t.Fatalf("GetUserByID: %+v", byID)
	}
	// duplikat username ditolak
	if _, err := st.CreateUser(User{Username: "rani", PasswordHash: "x"}); err == nil {
		t.Fatal("username duplikat harus error")
	}
	// disable
	st.CreateUser(User{Username: "budi", PasswordHash: "h", Role: "member"})
	if err := st.SetUserDisabled(id, true); err != nil {
		t.Fatal(err)
	}
	u, _ = st.GetUserByUsername("rani")
	if !u.Disabled {
		t.Fatal("user harus disabled")
	}
	users, _ := st.ListUsers()
	if len(users) != 2 {
		t.Fatalf("ListUsers n=%d", len(users))
	}
	if miss, _ := st.GetUserByUsername("nyasar"); miss != nil {
		t.Fatal("user tak ada harus nil")
	}
}

func TestQuotaAndAPIKey(t *testing.T) {
	st := openTemp(t)
	uid, _ := st.CreateUser(User{Username: "sari", PasswordHash: "h"})
	// quota upsert
	must0(t, st.SetQuota(Quota{UserID: uid, MaxSites: 5, MaxStorageMB: 15000, MaxBandwidthGB: 100}))
	q, _ := st.GetQuota(uid)
	if q == nil || q.MaxSites != 5 {
		t.Fatalf("GetQuota: %+v", q)
	}
	must0(t, st.SetQuota(Quota{UserID: uid, MaxSites: 2})) // update
	q, _ = st.GetQuota(uid)
	if q.MaxSites != 2 {
		t.Fatalf("quota tidak ter-update: %+v", q)
	}
	// api key
	kid, err := st.CreateAPIKey(uid, "ci", "hashkey123")
	if err != nil || kid == 0 {
		t.Fatal(err)
	}
	k, _ := st.GetAPIKeyByHash("hashkey123")
	if k == nil || k.UserID != uid {
		t.Fatalf("GetAPIKeyByHash: %+v", k)
	}
	keys, _ := st.ListAPIKeys(uid)
	if len(keys) != 1 {
		t.Fatalf("ListAPIKeys n=%d", len(keys))
	}
	must0(t, st.DeleteAPIKey(kid, uid))
	keys, _ = st.ListAPIKeys(uid)
	if len(keys) != 0 {
		t.Fatal("api key harus terhapus")
	}
}

func TestSiteOwnershipScoping(t *testing.T) {
	st := openTemp(t)
	a, _ := st.CreateUser(User{Username: "a", PasswordHash: "h"})
	b, _ := st.CreateUser(User{Username: "b", PasswordHash: "h"})
	st.CreateSite(Site{Domain: "a1.com", Type: "static", RootPath: "/a1", UserID: a})
	st.CreateSite(Site{Domain: "a2.com", Type: "static", RootPath: "/a2", UserID: a})
	st.CreateSite(Site{Domain: "b1.com", Type: "static", RootPath: "/b1", UserID: b})

	if n, _ := st.CountUserSites(a); n != 2 {
		t.Fatalf("CountUserSites(a)=%d, mau 2", n)
	}
	sitesB, _ := st.ListSitesByUser(b)
	if len(sitesB) != 1 || sitesB[0].Domain != "b1.com" {
		t.Fatalf("ListSitesByUser(b): %+v", sitesB)
	}
}

func TestUsersMigrationFromF3(t *testing.T) {
	path := filepath.Join(t.TempDir(), "m.db")
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	st.CreateSite(Site{Domain: "x.com", Type: "static", RootPath: "/x"})
	st.Close()
	st2, err := Open(path) // migrasi 2→3 mulus
	if err != nil {
		t.Fatalf("buka ulang: %v", err)
	}
	defer st2.Close()
	if _, err := st2.CreateUser(User{Username: "z", PasswordHash: "h"}); err != nil {
		t.Fatalf("tabel users harus ada: %v", err)
	}
}

// TestUsernameNormalization memastikan CreateUser dan GetUserByUsername
// menormalisasi username (lowercase + trim) sehingga "Admin ", "admin", "ADMIN"
// dianggap identitas yang sama.
func TestUsernameNormalization(t *testing.T) {
	st := openTemp(t)

	// Buat user dengan username yang belum dinormalisasi (huruf besar + spasi).
	id, err := st.CreateUser(User{Username: "  Admin  ", PasswordHash: "h", Role: "superadmin"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if id == 0 {
		t.Fatal("id harus > 0")
	}

	// Cari dengan variasi berbeda — semua harus menemukan user yang sama.
	cases := []string{"admin", "Admin", "ADMIN", "  admin  ", "  Admin  "}
	for _, uname := range cases {
		u, err := st.GetUserByUsername(uname)
		if err != nil {
			t.Fatalf("GetUserByUsername(%q): %v", uname, err)
		}
		if u == nil {
			t.Fatalf("GetUserByUsername(%q): harus ditemukan tapi nil", uname)
		}
		if u.Username != "admin" {
			t.Errorf("Username tersimpan = %q, mau %q", u.Username, "admin")
		}
	}

	// Duplikat dengan case berbeda harus ditolak (sudah ada "admin").
	if _, err := st.CreateUser(User{Username: "ADMIN", PasswordHash: "h2"}); err == nil {
		t.Fatal("duplikat username (case berbeda) harus error")
	}
}

func must0(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
