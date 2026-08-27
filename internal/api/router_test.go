package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/lamun-my-id/lamund/internal/auth"
	"github.com/lamun-my-id/lamund/internal/store"
)

// harness membangun API dengan store sementara + 1 user "admin"/"rahasia123".
func harness(t *testing.T) (http.Handler, *store.Store) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	hash, _ := auth.HashPassword("rahasia123")
	if _, err := st.CreateUser(store.User{Username: "admin", PasswordHash: hash, Role: "superadmin"}); err != nil {
		t.Fatal(err)
	}
	secret, _ := auth.GenerateSecret()
	// client_id uji agar device flow "aktif" di test (produksi: kosong = nonaktif).
	h := New(Deps{Store: st, Issuer: auth.NewIssuer(secret, time.Hour), GitHubClientID: "test-client-id"})
	return h, st
}

func do(t *testing.T, h http.Handler, method, path, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func login(t *testing.T, h http.Handler, user, pass string) *httptest.ResponseRecorder {
	return do(t, h, "POST", "/api/v1/auth/login", "", map[string]string{"username": user, "password": pass})
}

func TestLoginSuccess(t *testing.T) {
	h, _ := harness(t)
	rec := login(t, h, "admin", "rahasia123")
	if rec.Code != 200 {
		t.Fatalf("mau 200, dapat %d: %s", rec.Code, rec.Body)
	}
	var resp loginResp
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Token == "" || resp.Role != "superadmin" {
		t.Fatalf("respon login: %+v", resp)
	}
}

func TestLoginWrongPassword(t *testing.T) {
	h, _ := harness(t)
	rec := login(t, h, "admin", "salah")
	if rec.Code != 401 {
		t.Fatalf("mau 401, dapat %d", rec.Code)
	}
}

func TestLoginRateLimit(t *testing.T) {
	h, _ := harness(t)
	var last int
	for i := 0; i < 7; i++ {
		last = login(t, h, "admin", "salah").Code
	}
	if last != 429 {
		t.Fatalf("setelah banyak gagal harus 429, dapat %d", last)
	}
}

func TestProtectedNeedsToken(t *testing.T) {
	h, _ := harness(t)
	if rec := do(t, h, "GET", "/api/v1/auth/me", "", nil); rec.Code != 401 {
		t.Fatalf("tanpa token harus 401, dapat %d", rec.Code)
	}
	tok := loginToken(t, h)
	if rec := do(t, h, "GET", "/api/v1/auth/me", tok, nil); rec.Code != 200 {
		t.Fatalf("dengan token harus 200, dapat %d: %s", rec.Code, rec.Body)
	}
}

func TestAPIKeyAuth(t *testing.T) {
	h, st := harness(t)
	u, _ := st.GetUserByUsername("admin")
	pt, hash, _ := auth.GenerateAPIKey()
	st.CreateAPIKey(u.ID, "ci", hash)
	if rec := do(t, h, "GET", "/api/v1/auth/me", pt, nil); rec.Code != 200 {
		t.Fatalf("API key valid harus 200, dapat %d", rec.Code)
	}
	if rec := do(t, h, "GET", "/api/v1/auth/me", "lmd_palsu", nil); rec.Code != 401 {
		t.Fatalf("API key palsu harus 401, dapat %d", rec.Code)
	}
}

func TestDisabledUserRejected(t *testing.T) {
	h, st := harness(t)
	tok := loginToken(t, h)
	u, _ := st.GetUserByUsername("admin")
	st.SetUserDisabled(u.ID, true)
	if rec := do(t, h, "GET", "/api/v1/auth/me", tok, nil); rec.Code != 401 {
		t.Fatalf("user disabled harus 401, dapat %d", rec.Code)
	}
}

func loginToken(t *testing.T, h http.Handler) string {
	t.Helper()
	rec := login(t, h, "admin", "rahasia123")
	var resp loginResp
	json.Unmarshal(rec.Body.Bytes(), &resp)
	return resp.Token
}

// TestLoginUsernameNormalization memastikan login berhasil walau username
// dikirim dengan variasi huruf besar / spasi — normalisasi harus menyatukan
// identitas dan bucket rate-limiter.
func TestLoginUsernameNormalization(t *testing.T) {
	h, _ := harness(t) // harness membuat user "admin" (sudah lowercase)

	// Login dengan variasi huruf besar dan spasi harus berhasil.
	variants := []string{"Admin", "ADMIN", "admin ", " Admin "}
	for _, uname := range variants {
		rec := login(t, h, uname, "rahasia123")
		if rec.Code != 200 {
			t.Errorf("login(%q) = %d, mau 200: %s", uname, rec.Code, rec.Body)
		}
	}
}

// TestLoginRateLimitSharedBucket memastikan percobaan login dengan variasi
// username yang berbeda (tapi sama setelah normalisasi) berbagi bucket
// rate-limiter yang sama.
func TestLoginRateLimitSharedBucket(t *testing.T) {
	h, _ := harness(t)
	// Kirim lebih dari maxHits (5) percobaan gagal dengan variasi username.
	// Bucket harus sudah penuh sehingga percobaan ke-7+ mendapat 429.
	usernames := []string{"admin", "Admin", "ADMIN", "admin ", " admin", "Admin ", " ADMIN"}
	var last int
	for _, u := range usernames {
		last = login(t, h, u, "salah").Code
	}
	if last != 429 {
		t.Fatalf("bucket bersama harus 429 setelah banyak percobaan, dapat %d", last)
	}
}

// TestTokenVersionRevocationOnPasswordChange memastikan token lama tidak valid
// setelah user mengganti password, namun token baru (login ulang) tetap bekerja.
func TestTokenVersionRevocationOnPasswordChange(t *testing.T) {
	h, _ := harness(t)

	// Login → dapat token
	oldTok := loginToken(t, h)

	// Token lama harus valid
	if rec := do(t, h, "GET", "/api/v1/auth/me", oldTok, nil); rec.Code != 200 {
		t.Fatalf("token lama harus 200 sebelum ganti password, dapat %d", rec.Code)
	}

	// Ganti password → BumpTokenVersion dipanggil
	rec := do(t, h, "POST", "/api/v1/auth/password", oldTok, map[string]string{
		"current": "rahasia123",
		"new":     "passwordbaru123",
	})
	if rec.Code != 200 {
		t.Fatalf("ganti password harus 200, dapat %d: %s", rec.Code, rec.Body)
	}

	// Token LAMA sekarang harus ditolak (401)
	if rec := do(t, h, "GET", "/api/v1/auth/me", oldTok, nil); rec.Code != 401 {
		t.Fatalf("token lama harus 401 setelah ganti password, dapat %d", rec.Code)
	}

	// Login ulang dengan password baru → token baru harus bekerja
	rec = login(t, h, "admin", "passwordbaru123")
	if rec.Code != 200 {
		t.Fatalf("login dengan password baru harus 200, dapat %d", rec.Code)
	}
	var resp loginResp
	json.Unmarshal(rec.Body.Bytes(), &resp)
	newTok := resp.Token
	if rec := do(t, h, "GET", "/api/v1/auth/me", newTok, nil); rec.Code != 200 {
		t.Fatalf("token baru harus 200, dapat %d", rec.Code)
	}
}

// TestTokenVersionRevocationOnDisable memastikan token user yang di-disable
// langsung tidak valid (token_version juga di-bump saat disable).
func TestTokenVersionRevocationOnDisable(t *testing.T) {
	h, st := harness(t)

	// Buat user biasa
	hash, _ := auth.HashPassword("pass12345")
	uid, _ := st.CreateUser(store.User{Username: "victim", PasswordHash: hash, Role: "member"})
	_ = uid

	// Login sebagai victim
	rec := login(t, h, "victim", "pass12345")
	if rec.Code != 200 {
		t.Fatalf("login victim harus 200, dapat %d", rec.Code)
	}
	var resp loginResp
	json.Unmarshal(rec.Body.Bytes(), &resp)
	victimTok := resp.Token

	// Token victim valid
	if rec := do(t, h, "GET", "/api/v1/auth/me", victimTok, nil); rec.Code != 200 {
		t.Fatalf("token victim harus 200 sebelum disable, dapat %d", rec.Code)
	}

	// Admin disable victim
	adminTok := loginToken(t, h)
	u, _ := st.GetUserByUsername("victim")
	disableRec := do(t, h, "PATCH", "/api/v1/users/"+itoa(u.ID)+"/status", adminTok,
		map[string]bool{"disabled": true})
	if disableRec.Code != 200 {
		t.Fatalf("disable harus 200, dapat %d: %s", disableRec.Code, disableRec.Body)
	}

	// Token victim lama harus 401 (disabled + token_version bump)
	if rec := do(t, h, "GET", "/api/v1/auth/me", victimTok, nil); rec.Code != 401 {
		t.Fatalf("token victim harus 401 setelah disable, dapat %d", rec.Code)
	}
}

// TestAPIKeyUnaffectedByTokenVersionBump memastikan API key masih bekerja
// setelah token_version user di-bump (API key tidak diverifikasi via JWT).
func TestAPIKeyUnaffectedByTokenVersionBump(t *testing.T) {
	h, st := harness(t)

	// Buat API key untuk admin
	u, _ := st.GetUserByUsername("admin")
	pt, hash, _ := auth.GenerateAPIKey()
	st.CreateAPIKey(u.ID, "ci", hash)

	// API key bekerja sebelum bump
	if rec := do(t, h, "GET", "/api/v1/auth/me", pt, nil); rec.Code != 200 {
		t.Fatalf("API key harus 200 sebelum bump, dapat %d", rec.Code)
	}

	// Ganti password (bump token_version)
	tok := loginToken(t, h)
	rec := do(t, h, "POST", "/api/v1/auth/password", tok, map[string]string{
		"current": "rahasia123",
		"new":     "passwordbaru123",
	})
	if rec.Code != 200 {
		t.Fatalf("ganti password harus 200, dapat %d", rec.Code)
	}

	// API key MASIH harus bekerja setelah bump
	if rec := do(t, h, "GET", "/api/v1/auth/me", pt, nil); rec.Code != 200 {
		t.Fatalf("API key harus tetap 200 setelah token_version bump, dapat %d", rec.Code)
	}
}
