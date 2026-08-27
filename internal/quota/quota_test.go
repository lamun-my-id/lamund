package quota

import (
	"path/filepath"
	"testing"

	"github.com/lamun-my-id/lamund/internal/store"
)

func openStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "q.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestCanCreateSite(t *testing.T) {
	st := openStore(t)
	uid, _ := st.CreateUser(store.User{Username: "u", PasswordHash: "h", Role: "member"})
	st.SetQuota(store.Quota{UserID: uid, MaxSites: 1})

	ok, _, _ := CanCreateSite(st, uid, "member")
	if !ok {
		t.Fatal("situs pertama harus boleh")
	}
	st.CreateSite(store.Site{Domain: "a.com", Type: "static", RootPath: "/a", UserID: uid})
	ok, reason, _ := CanCreateSite(st, uid, "member")
	if ok || reason == "" {
		t.Fatalf("situs kedua harus ditolak, ok=%v reason=%q", ok, reason)
	}
	// superadmin tak dibatasi
	if ok, _, _ := CanCreateSite(st, uid, "superadmin"); !ok {
		t.Fatal("superadmin harus selalu boleh")
	}
}

func TestCanCreateTeam(t *testing.T) {
	st := openStore(t)
	uid, _ := st.CreateUser(store.User{Username: "u", PasswordHash: "h", Role: "member"})
	st.SetQuota(store.Quota{UserID: uid, MaxTeams: 2}) // batas 2 tim dimiliki

	// tim 1 & 2 boleh, tim 3 ditolak
	for i := 1; i <= 2; i++ {
		if ok, reason, _ := CanCreateTeam(st, uid, "member"); !ok {
			t.Fatalf("tim ke-%d harus boleh, reason=%q", i, reason)
		}
		tm, _ := st.CreateTeam("team", "team-"+itoa(i))
		st.AddTeamMember(tm.ID, uid, "owner")
	}
	if ok, reason, _ := CanCreateTeam(st, uid, "member"); ok || reason == "" {
		t.Fatalf("tim ke-3 harus ditolak, ok=%v reason=%q", ok, reason)
	}
	// Keanggotaan sbg member di tim lain TIDAK menghitung kuota owner.
	other, _ := st.CreateTeam("other", "other")
	st.AddTeamMember(other.ID, uid, "member")
	if ok, _, _ := CanCreateTeam(st, uid, "superadmin"); !ok {
		t.Fatal("superadmin tak dibatasi tim")
	}
}

func itoa(i int) string {
	return string(rune('0' + i))
}

func TestCanUseStorage(t *testing.T) {
	st := openStore(t)
	uid, _ := st.CreateUser(store.User{Username: "u", PasswordHash: "h", Role: "member"})
	st.SetQuota(store.Quota{UserID: uid, MaxStorageMB: 1}) // 1 MB

	if ok, _, _ := CanUseStorage(st, uid, "member", 0, 512*1024); !ok {
		t.Fatal("512KB dalam batas 1MB harus boleh")
	}
	if ok, _, _ := CanUseStorage(st, uid, "member", 900*1024, 512*1024); ok {
		t.Fatal("melebihi 1MB harus ditolak")
	}
	if ok, _, _ := CanUseStorage(st, uid, "superadmin", 0, 1<<40); !ok {
		t.Fatal("superadmin tak dibatasi storage")
	}
}
