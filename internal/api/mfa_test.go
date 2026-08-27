package api

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/lamun-my-id/lamund/internal/auth"
	"github.com/lamun-my-id/lamund/internal/store"
)

// mfaSetupResp adalah respons /account/mfa/setup.
type mfaSetupResp struct {
	Secret string `json:"secret"`
	URI    string `json:"uri"`
}

// mfaVerifyResp adalah respons /account/mfa/verify.
type mfaVerifyResp struct {
	RecoveryCodes []string `json:"recovery_codes"`
}

// mfaLoginResp adalah respons /auth/login saat MFA aktif.
type mfaLoginResp struct {
	MFARequired bool   `json:"mfa_required"`
	Pending     string `json:"pending"`
}

// enrollMFA menjalankan setup+verify untuk user (via token) dan mengembalikan
// secret + recovery codes. Menganggap MFA belum aktif.
//
// Enrollment men-set mfa_last_step ke step window saat ini (anti-replay). Agar
// test login/verify berikutnya di window yang sama tidak salah-tolak sebagai
// replay atas kode enrollment, helper mereset lastStep ke 0 (mensimulasikan
// window login yang lebih baru — user asli login belakangan, bukan di detik
// yang sama dengan enrollment). Anti-replay tetap diuji terpisah via
// TestMFAAntiReplay yang mereplay kode DALAM alur login.
func enrollMFA(t *testing.T, h http.Handler, st *store.Store, uid int64, tok string) (secret string, recovery []string) {
	t.Helper()
	rec := do(t, h, "POST", "/api/v1/account/mfa/setup", tok, map[string]string{})
	if rec.Code != 200 {
		t.Fatalf("setup harus 200, dapat %d: %s", rec.Code, rec.Body)
	}
	var sr mfaSetupResp
	json.Unmarshal(rec.Body.Bytes(), &sr)
	if sr.Secret == "" || sr.URI == "" {
		t.Fatalf("setup harus mengembalikan secret+uri: %+v", sr)
	}
	code, err := auth.TOTPCodeAt(sr.Secret, time.Now())
	if err != nil {
		t.Fatalf("TOTPCodeAt: %v", err)
	}
	rec = do(t, h, "POST", "/api/v1/account/mfa/verify", tok, map[string]string{"code": code})
	if rec.Code != 200 {
		t.Fatalf("verify harus 200, dapat %d: %s", rec.Code, rec.Body)
	}
	var vr mfaVerifyResp
	json.Unmarshal(rec.Body.Bytes(), &vr)
	if len(vr.RecoveryCodes) != 10 {
		t.Fatalf("verify harus mengembalikan 10 recovery codes, dapat %d", len(vr.RecoveryCodes))
	}
	// Reset lastStep agar login berikutnya di window yang sama tak dianggap replay
	// atas kode enrollment (simulasi user login di window yang lebih baru).
	if err := st.SetMFALastStep(uid, 0); err != nil {
		t.Fatalf("reset lastStep: %v", err)
	}
	return sr.Secret, vr.RecoveryCodes
}

// adminUID mengembalikan ID user "admin" dari harness.
func adminUID(t *testing.T, st *store.Store) int64 {
	t.Helper()
	u, err := st.GetUserByUsername("admin")
	if err != nil || u == nil {
		t.Fatalf("gagal muat admin: %v", err)
	}
	return u.ID
}

// TestLoginNoMFAReturnsJWT adalah regresi: user tanpa MFA login → JWT langsung.
func TestLoginNoMFAReturnsJWT(t *testing.T) {
	h, _ := harness(t)
	rec := login(t, h, "admin", "rahasia123")
	if rec.Code != 200 {
		t.Fatalf("login harus 200, dapat %d", rec.Code)
	}
	var resp loginResp
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Token == "" {
		t.Fatalf("login tanpa MFA harus mengembalikan token: %+v", resp)
	}
}

// TestMFAEnrollment menguji setup → verify → status enabled.
func TestMFAEnrollment(t *testing.T) {
	h, st0 := harness(t)
	tok := loginToken(t, h)

	// Status awal: nonaktif.
	rec := do(t, h, "GET", "/api/v1/account/mfa", tok, nil)
	if rec.Code != 200 {
		t.Fatalf("status harus 200, dapat %d", rec.Code)
	}
	var st map[string]bool
	json.Unmarshal(rec.Body.Bytes(), &st)
	if st["enabled"] {
		t.Fatal("MFA harus nonaktif di awal")
	}

	enrollMFA(t, h, st0, adminUID(t, st0), tok)

	// Status setelah enroll: aktif.
	rec = do(t, h, "GET", "/api/v1/account/mfa", tok, nil)
	json.Unmarshal(rec.Body.Bytes(), &st)
	if !st["enabled"] {
		t.Fatal("MFA harus aktif setelah verify")
	}
}

// TestMFASetupRejectedWhenEnabled memastikan setup ulang saat sudah aktif → 409.
func TestMFASetupRejectedWhenEnabled(t *testing.T) {
	h, st := harness(t)
	tok := loginToken(t, h)
	enrollMFA(t, h, st, adminUID(t, st), tok)
	rec := do(t, h, "POST", "/api/v1/account/mfa/setup", tok, map[string]string{})
	if rec.Code != 409 {
		t.Fatalf("setup saat sudah aktif harus 409, dapat %d", rec.Code)
	}
}

// TestMFAVerifyWrongCode memastikan verify dengan kode salah → 400.
func TestMFAVerifyWrongCode(t *testing.T) {
	h, _ := harness(t)
	tok := loginToken(t, h)
	rec := do(t, h, "POST", "/api/v1/account/mfa/setup", tok, map[string]string{})
	if rec.Code != 200 {
		t.Fatalf("setup harus 200, dapat %d", rec.Code)
	}
	rec = do(t, h, "POST", "/api/v1/account/mfa/verify", tok, map[string]string{"code": "000000"})
	if rec.Code != 400 {
		t.Fatalf("verify kode salah harus 400, dapat %d", rec.Code)
	}
}

// TestMFALoginFlow menguji login dua-langkah: mfa_required + pending → /login/mfa.
func TestMFALoginFlow(t *testing.T) {
	h, st := harness(t)
	tok := loginToken(t, h)
	secret, _ := enrollMFA(t, h, st, adminUID(t, st), tok)

	// Login sekarang → mfa_required + pending, tanpa token.
	rec := login(t, h, "admin", "rahasia123")
	if rec.Code != 200 {
		t.Fatalf("login MFA harus 200, dapat %d: %s", rec.Code, rec.Body)
	}
	var lr mfaLoginResp
	json.Unmarshal(rec.Body.Bytes(), &lr)
	if !lr.MFARequired || lr.Pending == "" {
		t.Fatalf("login MFA harus mfa_required+pending: %+v", lr)
	}
	// Pastikan tidak ada token sesi.
	var sess loginResp
	json.Unmarshal(rec.Body.Bytes(), &sess)
	if sess.Token != "" {
		t.Fatalf("login MFA TAK boleh mengembalikan token sesi: %q", sess.Token)
	}

	// /login/mfa dengan kode TOTP valid → token.
	code, _ := auth.TOTPCodeAt(secret, time.Now())
	rec = do(t, h, "POST", "/api/v1/auth/login/mfa", "", map[string]string{"pending": lr.Pending, "code": code})
	if rec.Code != 200 {
		t.Fatalf("/login/mfa kode valid harus 200, dapat %d: %s", rec.Code, rec.Body)
	}
	var lr2 loginResp
	json.Unmarshal(rec.Body.Bytes(), &lr2)
	if lr2.Token == "" || lr2.Role != "superadmin" {
		t.Fatalf("/login/mfa harus mengembalikan token: %+v", lr2)
	}
}

// TestMFALoginWrongCode memastikan /login/mfa dengan kode salah → 401.
func TestMFALoginWrongCode(t *testing.T) {
	h, st := harness(t)
	tok := loginToken(t, h)
	enrollMFA(t, h, st, adminUID(t, st), tok)

	rec := login(t, h, "admin", "rahasia123")
	var lr mfaLoginResp
	json.Unmarshal(rec.Body.Bytes(), &lr)

	rec = do(t, h, "POST", "/api/v1/auth/login/mfa", "", map[string]string{"pending": lr.Pending, "code": "000000"})
	if rec.Code != 401 {
		t.Fatalf("/login/mfa kode salah harus 401, dapat %d", rec.Code)
	}
}

// TestPendingTokenNotUsableAsSession memastikan pending-token ditolak sebagai
// Bearer pada endpoint ber-auth.
func TestPendingTokenNotUsableAsSession(t *testing.T) {
	h, st := harness(t)
	tok := loginToken(t, h)
	enrollMFA(t, h, st, adminUID(t, st), tok)

	rec := login(t, h, "admin", "rahasia123")
	var lr mfaLoginResp
	json.Unmarshal(rec.Body.Bytes(), &lr)
	if lr.Pending == "" {
		t.Fatal("harus ada pending token")
	}

	// Coba pakai pending-token sebagai Bearer pada endpoint ber-auth → 401.
	if rec := do(t, h, "GET", "/api/v1/auth/me", lr.Pending, nil); rec.Code != 401 {
		t.Fatalf("pending-token sebagai sesi harus 401, dapat %d", rec.Code)
	}
}

// TestMFARecoveryCode menguji jalur recovery code: sekali-pakai.
func TestMFARecoveryCode(t *testing.T) {
	h, st := harness(t)
	tok := loginToken(t, h)
	_, recovery := enrollMFA(t, h, st, adminUID(t, st), tok)
	rc := recovery[0]

	// Login → pending.
	rec := login(t, h, "admin", "rahasia123")
	var lr mfaLoginResp
	json.Unmarshal(rec.Body.Bytes(), &lr)

	// /login/mfa dengan recovery code → token.
	rec = do(t, h, "POST", "/api/v1/auth/login/mfa", "", map[string]string{"pending": lr.Pending, "code": rc})
	if rec.Code != 200 {
		t.Fatalf("/login/mfa recovery code harus 200, dapat %d: %s", rec.Code, rec.Body)
	}

	// Login lagi → pending baru.
	rec = login(t, h, "admin", "rahasia123")
	json.Unmarshal(rec.Body.Bytes(), &lr)

	// Recovery code yang sama dipakai lagi → 401 (single-use).
	rec = do(t, h, "POST", "/api/v1/auth/login/mfa", "", map[string]string{"pending": lr.Pending, "code": rc})
	if rec.Code != 401 {
		t.Fatalf("recovery code reuse harus 401, dapat %d", rec.Code)
	}
}

// TestMFADisable menguji disable dengan kode valid → login berikutnya langsung JWT.
func TestMFADisable(t *testing.T) {
	h, st := harness(t)
	tok := loginToken(t, h)
	secret, _ := enrollMFA(t, h, st, adminUID(t, st), tok)

	// Disable tanpa kode / kode salah → 400.
	rec := do(t, h, "POST", "/api/v1/account/mfa/disable", tok, map[string]string{"code": "000000"})
	if rec.Code != 400 {
		t.Fatalf("disable kode salah harus 400, dapat %d", rec.Code)
	}

	// Disable dengan kode valid → 200.
	code, _ := auth.TOTPCodeAt(secret, time.Now())
	rec = do(t, h, "POST", "/api/v1/account/mfa/disable", tok, map[string]string{"code": code})
	if rec.Code != 200 {
		t.Fatalf("disable kode valid harus 200, dapat %d: %s", rec.Code, rec.Body)
	}

	// Login berikutnya → JWT langsung (tanpa mfa_required).
	rec = login(t, h, "admin", "rahasia123")
	var lr mfaLoginResp
	json.Unmarshal(rec.Body.Bytes(), &lr)
	if lr.MFARequired {
		t.Fatal("setelah disable, login TAK boleh mfa_required")
	}
	var sess loginResp
	json.Unmarshal(rec.Body.Bytes(), &sess)
	if sess.Token == "" {
		t.Fatal("setelah disable, login harus mengembalikan token langsung")
	}
}

// TestMFAAntiReplay memastikan kode TOTP yang sama (step sama) ditolak pada
// percobaan kedua via /login/mfa.
func TestMFAAntiReplay(t *testing.T) {
	h, st := harness(t)
	tok := loginToken(t, h)
	secret, _ := enrollMFA(t, h, st, adminUID(t, st), tok)

	code, _ := auth.TOTPCodeAt(secret, time.Now())

	// Login pertama + verify → sukses.
	rec := login(t, h, "admin", "rahasia123")
	var lr mfaLoginResp
	json.Unmarshal(rec.Body.Bytes(), &lr)
	rec = do(t, h, "POST", "/api/v1/auth/login/mfa", "", map[string]string{"pending": lr.Pending, "code": code})
	if rec.Code != 200 {
		t.Fatalf("percobaan pertama harus 200, dapat %d: %s", rec.Code, rec.Body)
	}

	// Login kedua + kode TOTP sama (step sama) → ditolak (replay).
	rec = login(t, h, "admin", "rahasia123")
	json.Unmarshal(rec.Body.Bytes(), &lr)
	rec = do(t, h, "POST", "/api/v1/auth/login/mfa", "", map[string]string{"pending": lr.Pending, "code": code})
	if rec.Code != 401 {
		t.Fatalf("replay kode TOTP sama harus 401, dapat %d", rec.Code)
	}
}

// TestAdminMFAReset memastikan superadmin bisa reset MFA user lain, dan login
// user itu setelahnya tak lagi mfa_required.
func TestAdminMFAReset(t *testing.T) {
	h, st := harness(t)

	// Buat user member + login + enroll MFA.
	hash, _ := auth.HashPassword("pass12345")
	uid, _ := st.CreateUser(store.User{Username: "victim", PasswordHash: hash, Role: "member"})
	vrec := login(t, h, "victim", "pass12345")
	var vresp loginResp
	json.Unmarshal(vrec.Body.Bytes(), &vresp)
	enrollMFA(t, h, st, uid, vresp.Token)

	// Login victim sekarang → mfa_required.
	vrec = login(t, h, "victim", "pass12345")
	var lr mfaLoginResp
	json.Unmarshal(vrec.Body.Bytes(), &lr)
	if !lr.MFARequired {
		t.Fatal("victim harus mfa_required sebelum reset")
	}

	// Superadmin reset MFA victim.
	adminTok := loginToken(t, h)
	rec := do(t, h, "POST", "/api/v1/admin/users/"+itoa(uid)+"/mfa/reset", adminTok, map[string]string{})
	if rec.Code != 200 {
		t.Fatalf("admin reset harus 200, dapat %d: %s", rec.Code, rec.Body)
	}

	// Login victim setelah reset → JWT langsung (tanpa mfa_required).
	// Pakai variabel baru: json.Unmarshal tak mereset field yang tak ada di body.
	vrec = login(t, h, "victim", "pass12345")
	var lr2 mfaLoginResp
	json.Unmarshal(vrec.Body.Bytes(), &lr2)
	if lr2.MFARequired {
		t.Fatal("setelah reset, victim TAK boleh mfa_required")
	}
	var sess loginResp
	json.Unmarshal(vrec.Body.Bytes(), &sess)
	if sess.Token == "" {
		t.Fatal("setelah reset, victim harus login langsung dapat token")
	}
}

// TestAdminMFAResetRequiresSuperadmin memastikan non-superadmin ditolak.
func TestAdminMFAResetRequiresSuperadmin(t *testing.T) {
	h, st := harness(t)
	hash, _ := auth.HashPassword("pass12345")
	uid, _ := st.CreateUser(store.User{Username: "victim", PasswordHash: hash, Role: "member"})
	memberTok := func() string {
		rec := login(t, h, "victim", "pass12345")
		var r loginResp
		json.Unmarshal(rec.Body.Bytes(), &r)
		return r.Token
	}()
	rec := do(t, h, "POST", "/api/v1/admin/users/"+itoa(uid)+"/mfa/reset", memberTok, map[string]string{})
	if rec.Code != 403 {
		t.Fatalf("non-superadmin reset harus 403, dapat %d", rec.Code)
	}
}
