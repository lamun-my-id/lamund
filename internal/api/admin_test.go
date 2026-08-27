package api

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/lamun-my-id/lamund/internal/auth"
	"github.com/lamun-my-id/lamund/internal/store"
)

func TestAdminCreateUserAndQuota(t *testing.T) {
	h, st := harness(t)
	admin := loginToken(t, h)

	// admin buat user baru
	rec := do(t, h, "POST", "/api/v1/users", admin, map[string]any{
		"username": "citra", "password": "password123", "role": "member", "max_sites": 3,
	})
	if rec.Code != 201 {
		t.Fatalf("create user mau 201, dapat %d: %s", rec.Code, rec.Body)
	}
	var u userJSON
	json.Unmarshal(rec.Body.Bytes(), &u)
	if u.ID == 0 || u.Username != "citra" {
		t.Fatalf("user resp: %+v", u)
	}
	if q, _ := st.GetQuota(u.ID); q == nil || q.MaxSites != 3 {
		t.Fatalf("kuota awal tidak terpasang: %+v", q)
	}

	// set kuota baru
	rec = do(t, h, "PATCH", "/api/v1/users/"+itoa(u.ID)+"/quota", admin, map[string]int{
		"max_sites": 10, "max_storage_mb": 5000,
	})
	if rec.Code != 200 {
		t.Fatalf("set quota mau 200, dapat %d", rec.Code)
	}
	if q, _ := st.GetQuota(u.ID); q.MaxSites != 10 || q.MaxStorageMB != 5000 {
		t.Fatalf("kuota tak ter-update: %+v", q)
	}

	// disable user
	rec = do(t, h, "PATCH", "/api/v1/users/"+itoa(u.ID)+"/status", admin, map[string]bool{"disabled": true})
	if rec.Code != 200 {
		t.Fatalf("disable mau 200, dapat %d", rec.Code)
	}
	if uu, _ := st.GetUserByID(u.ID); !uu.Disabled {
		t.Fatal("user harus disabled")
	}
}

func TestNonAdminForbidden(t *testing.T) {
	h, st := harness(t)
	userTok := mkUser(t, h, st, "biasa", 2)
	for _, ep := range []struct{ m, p string }{
		{"GET", "/api/v1/users"},
		{"POST", "/api/v1/users"},
		{"PATCH", "/api/v1/users/1/quota"},
	} {
		if c := do(t, h, ep.m, ep.p, userTok, map[string]string{}).Code; c != 403 {
			t.Fatalf("%s %s untuk non-admin harus 403, dapat %d", ep.m, ep.p, c)
		}
	}
}

func TestAPIKeyLifecycle(t *testing.T) {
	h, st := harness(t)
	tok := mkUser(t, h, st, "devi", 5)

	rec := do(t, h, "POST", "/api/v1/apikeys", tok, map[string]string{"name": "deploy"})
	if rec.Code != 201 {
		t.Fatalf("create key mau 201, dapat %d", rec.Code)
	}
	var created struct {
		ID  int64  `json:"id"`
		Key string `json:"key"`
	}
	json.Unmarshal(rec.Body.Bytes(), &created)
	if created.Key == "" || created.Key[:4] != "lmd_" {
		t.Fatalf("key plaintext tak dikembalikan: %+v", created)
	}
	// key baru langsung bisa dipakai autentikasi
	if c := do(t, h, "GET", "/api/v1/auth/me", created.Key, nil).Code; c != 200 {
		t.Fatalf("key baru harus bisa auth, dapat %d", c)
	}
	// list menampilkan 1 key, TANPA membocorkan hash/plaintext
	rec = do(t, h, "GET", "/api/v1/apikeys", tok, nil)
	if !contains(rec.Body.String(), "deploy") || contains(rec.Body.String(), created.Key) {
		t.Fatalf("list keys bocor/kurang: %s", rec.Body)
	}
	// hapus
	if c := do(t, h, "DELETE", "/api/v1/apikeys/"+itoa(created.ID), tok, nil).Code; c != 200 {
		t.Fatalf("hapus key mau 200, dapat %d", c)
	}
	if c := do(t, h, "GET", "/api/v1/auth/me", created.Key, nil).Code; c != 401 {
		t.Fatalf("key terhapus harus 401, dapat %d", c)
	}
}

func itoa(i int64) string         { return strconv.FormatInt(i, 10) }
func contains(s, sub string) bool { return strings.Contains(s, sub) }

// TestListUsersEnriched memastikan GET /users mengembalikan email, mfa_enabled, max_sites.
func TestListUsersEnriched(t *testing.T) {
	h, st := harness(t)
	admin := loginToken(t, h)

	hash, _ := auth.HashPassword("rahasia123")
	id2, err := st.CreateUser(store.User{Username: "rini", PasswordHash: hash, Role: "member", Email: "rini@test.com"})
	if err != nil {
		t.Fatal(err)
	}
	st.SetQuota(store.Quota{UserID: id2, MaxSites: 7})

	rec := do(t, h, "GET", "/api/v1/users", admin, nil)
	if rec.Code != 200 {
		t.Fatalf("list users harus 200, dapat %d: %s", rec.Code, rec.Body)
	}
	var resp struct {
		Users []struct {
			Username   string `json:"username"`
			Email      string `json:"email"`
			MFAEnabled bool   `json:"mfa_enabled"`
			MaxSites   int    `json:"max_sites"`
		} `json:"users"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("parse resp: %v", err)
	}
	found := false
	for _, u := range resp.Users {
		if u.Username == "rini" {
			found = true
			if u.Email != "rini@test.com" {
				t.Errorf("email salah: %q", u.Email)
			}
			if u.MFAEnabled {
				t.Error("mfa_enabled harus false")
			}
			if u.MaxSites != 7 {
				t.Errorf("max_sites harus 7, dapat %d", u.MaxSites)
			}
		}
	}
	if !found {
		t.Fatal("user rini tidak ditemukan dalam respons")
	}
}

// TestDeleteUserSelf memastikan superadmin tidak bisa menghapus akun dirinya sendiri (400).
func TestDeleteUserSelf(t *testing.T) {
	h, st := harness(t)
	admin := loginToken(t, h)
	u, _ := st.GetUserByUsername("admin")
	if rec := do(t, h, "DELETE", "/api/v1/users/"+itoa(u.ID), admin, nil); rec.Code != 400 {
		t.Fatalf("hapus diri sendiri harus 400, dapat %d: %s", rec.Code, rec.Body)
	}
}

// TestDeleteUserOther memastikan superadmin bisa menghapus user biasa (tanpa resource)
// dan user hilang dari GET /users.
func TestDeleteUserOther(t *testing.T) {
	h, st := harness(t)
	admin := loginToken(t, h)

	hash, _ := auth.HashPassword("rahasia123")
	id2, err := st.CreateUser(store.User{Username: "hapus_saya", PasswordHash: hash, Role: "member"})
	if err != nil {
		t.Fatal(err)
	}

	rec := do(t, h, "DELETE", "/api/v1/users/"+itoa(id2), admin, nil)
	if rec.Code != 200 {
		t.Fatalf("hapus user lain harus 200, dapat %d: %s", rec.Code, rec.Body)
	}
	var resp map[string]string
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["status"] != "deleted" {
		t.Fatalf("status harus 'deleted', dapat %q", resp["status"])
	}
	if contains(do(t, h, "GET", "/api/v1/users", admin, nil).Body.String(), "hapus_saya") {
		t.Fatal("user terhapus masih muncul di GET /users")
	}
}

// TestDeleteLastSuperadminGuard: superadmin terakhir tidak bisa dihapus oleh superadmin lain.
func TestDeleteLastSuperadminGuard(t *testing.T) {
	h, st := harness(t)
	// harness: "admin" = satu-satunya superadmin.
	// Butuh superadmin kedua agar bisa mencoba menghapus "admin".
	hash2, _ := auth.HashPassword("rahasia123")
	id2, _ := st.CreateUser(store.User{Username: "admin2x", PasswordHash: hash2, Role: "superadmin"})
	rec2 := login(t, h, "admin2x", "rahasia123")
	var lr loginResp
	json.Unmarshal(rec2.Body.Bytes(), &lr)
	tok2 := lr.Token

	// Ada 2 superadmin → hapus "admin" oleh "admin2x" → 200 (bukan last)
	origAdmin, _ := st.GetUserByUsername("admin")
	if rec := do(t, h, "DELETE", "/api/v1/users/"+itoa(origAdmin.ID), tok2, nil); rec.Code != 200 {
		t.Fatalf("hapus superadmin non-terakhir harus 200, dapat %d: %s", rec.Code, rec.Body)
	}

	// Sekarang "admin2x" satu-satunya superadmin.
	// Self-delete → 400
	if rec := do(t, h, "DELETE", "/api/v1/users/"+itoa(id2), tok2, nil); rec.Code != 400 {
		t.Fatalf("self-delete satu-satunya superadmin harus 400, dapat %d", rec.Code)
	}

	// Buat superadmin ketiga, coba hapus admin2x → 200 (ada 2 lagi)
	hash3, _ := auth.HashPassword("rahasia123")
	_, _ = st.CreateUser(store.User{Username: "admin3x", PasswordHash: hash3, Role: "superadmin"})
	rec3 := login(t, h, "admin3x", "rahasia123")
	var lr3 loginResp
	json.Unmarshal(rec3.Body.Bytes(), &lr3)
	if rec := do(t, h, "DELETE", "/api/v1/users/"+itoa(id2), lr3.Token, nil); rec.Code != 200 {
		t.Fatalf("hapus superadmin ke-2 (ada 2) harus 200, dapat %d: %s", rec.Code, rec.Body)
	}
	// admin3x sekarang satu-satunya. Self-delete → 400
	admin3u, _ := st.GetUserByUsername("admin3x")
	if rec := do(t, h, "DELETE", "/api/v1/users/"+itoa(admin3u.ID), lr3.Token, nil); rec.Code != 400 {
		t.Fatalf("self-delete (satu-satunya) harus 400, dapat %d", rec.Code)
	}
}
