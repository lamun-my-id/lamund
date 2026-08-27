package api

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/lamun-my-id/lamund/internal/auth"
	"github.com/lamun-my-id/lamund/internal/store"
)

// emptyHarness membangun API dengan store tanpa user (untuk uji setup wizard).
func emptyHarness(t *testing.T) (http.Handler, *store.Store) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	secret, _ := auth.GenerateSecret()
	h := New(Deps{Store: st, Issuer: auth.NewIssuer(secret, time.Hour)})
	return h, st
}

func TestSetupStatusEmpty(t *testing.T) {
	h, _ := emptyHarness(t)
	rec := do(t, h, "GET", "/api/v1/setup/status", "", nil)
	if rec.Code != 200 {
		t.Fatalf("mau 200, dapat %d", rec.Code)
	}
	var resp map[string]bool
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if !resp["needs_setup"] {
		t.Fatalf("instance kosong harus needs_setup=true: %v", resp)
	}
}

func TestSetupCreatesAdminThenGuards(t *testing.T) {
	h, st := emptyHarness(t)
	body := map[string]string{
		"username": "boss", "password": "rahasia123",
		"name": "Big Boss", "email": "boss@lamun.test",
		"locale": "id", "theme": "terminal",
	}
	rec := do(t, h, "POST", "/api/v1/setup", "", body)
	if rec.Code != 201 {
		t.Fatalf("setup pertama harus 201, dapat %d: %s", rec.Code, rec.Body)
	}
	var resp loginResp
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Token == "" || resp.Role != "superadmin" {
		t.Fatalf("setup harus mengembalikan token superadmin: %+v", resp)
	}
	// superadmin dibuat dengan profil + prefs
	u, _ := st.GetUserByUsername("boss")
	if u == nil || u.Role != "superadmin" || u.Name != "Big Boss" || u.Email != "boss@lamun.test" {
		t.Fatalf("profil admin tak tersimpan: %+v", u)
	}
	if u.Theme != "terminal" || u.Locale != "id" {
		t.Fatalf("prefs tak tersimpan: theme=%q locale=%q", u.Theme, u.Locale)
	}
	// status kini needs_setup=false
	rec = do(t, h, "GET", "/api/v1/setup/status", "", nil)
	var st2 map[string]bool
	json.Unmarshal(rec.Body.Bytes(), &st2)
	if st2["needs_setup"] {
		t.Fatal("setelah ada admin, needs_setup harus false")
	}
	// setup kedua ditolak (guard 0-user)
	rec = do(t, h, "POST", "/api/v1/setup", "", body)
	if rec.Code != 403 {
		t.Fatalf("setup kedua harus 403, dapat %d", rec.Code)
	}
}

func TestForgotAlwaysOKAndResetWorks(t *testing.T) {
	cap := &capMailer{}
	h, st := harnessMail(t, cap) // varian harness dgn Deps.Mailer=cap
	u, _ := st.GetUserByUsername("admin")
	st.UpdateUserProfile(u.ID, "Admin", "admin@x")
	// forgot untuk email ada → 200 + email terkirim
	if rec := do(t, h, "POST", "/api/v1/auth/forgot", "", map[string]string{"email": "admin@x"}); rec.Code != 200 {
		t.Fatalf("forgot 200, dapat %d", rec.Code)
	}
	// forgot untuk email tak ada → tetap 200 (anti-enumeration)
	if rec := do(t, h, "POST", "/api/v1/auth/forgot", "", map[string]string{"email": "nope@x"}); rec.Code != 200 {
		t.Fatalf("forgot email asing tetap 200, dapat %d", rec.Code)
	}
	if cap.token == "" {
		t.Fatal("harus ada token reset terkirim untuk email valid")
	}
	// reset pakai token → sandi baru
	if rec := do(t, h, "POST", "/api/v1/auth/reset", "", map[string]string{"token": cap.token, "new": "sandibaru123"}); rec.Code != 200 {
		t.Fatalf("reset 200, dapat %d: %s", rec.Code, rec.Body)
	}
	// login sandi baru berhasil
	if rec := login(t, h, "admin", "sandibaru123"); rec.Code != 200 {
		t.Fatalf("login sandi baru harus 200, dapat %d", rec.Code)
	}
}

func TestAccountProfileAndPrefs(t *testing.T) {
	h, st := harness(t)
	tok := loginToken(t, h)
	// edit profil
	rec := do(t, h, "PATCH", "/api/v1/account", tok, map[string]string{"name": "Sisa Admin", "email": "a@b.c"})
	if rec.Code != 200 {
		t.Fatalf("PATCH account harus 200, dapat %d: %s", rec.Code, rec.Body)
	}
	// set prefs
	rec = do(t, h, "PUT", "/api/v1/account/prefs", tok, map[string]string{"theme": "seagrass", "locale": "ja"})
	if rec.Code != 200 {
		t.Fatalf("PUT prefs harus 200, dapat %d", rec.Code)
	}
	u, _ := st.GetUserByUsername("admin")
	if u.Name != "Sisa Admin" || u.Email != "a@b.c" || u.Theme != "seagrass" || u.Locale != "ja" {
		t.Fatalf("profil/prefs tak tersimpan: %+v", u)
	}
	// /me memantulkan profil + prefs
	rec = do(t, h, "GET", "/api/v1/auth/me", tok, nil)
	var me map[string]any
	json.Unmarshal(rec.Body.Bytes(), &me)
	if me["name"] != "Sisa Admin" || me["theme"] != "seagrass" || me["locale"] != "ja" {
		t.Fatalf("/me tak memantulkan profil/prefs: %v", me)
	}
}
