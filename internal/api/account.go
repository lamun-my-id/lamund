package api

import (
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/lamun-my-id/lamund/internal/auth"
	"github.com/lamun-my-id/lamund/internal/quota"
	"github.com/lamun-my-id/lamund/internal/store"
)

var usernameRe = regexp.MustCompile(`^[a-z0-9-]{4,32}$`)

// validUsername: huruf kecil/angka/'-', 4–32 karakter. Menormalkan dulu
// (lowercase+trim) agar konsisten dgn normalisasi di store.createUser.
func validUsername(u string) bool {
	return usernameRe.MatchString(strings.ToLower(strings.TrimSpace(u)))
}

// handleMyUsage mengembalikan kuota + pemakaian caller (jatah situs & storage).
func (s *server) handleMyUsage(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r.Context())
	maxSites, maxStorageMB := quota.DefaultMaxSites, quota.DefaultMaxStorageMB
	if q, _ := s.d.Store.GetQuota(u.ID); q != nil {
		if q.MaxSites > 0 {
			maxSites = q.MaxSites
		}
		if q.MaxStorageMB > 0 {
			maxStorageMB = q.MaxStorageMB
		}
	}
	usedSites, _ := s.d.Store.CountUserSites(u.ID)
	var usedBytes int64
	if s.d.Sites != nil {
		sites, _ := s.d.Store.ListSitesByUser(u.ID)
		for _, st := range sites {
			n, _ := s.d.Sites.DirSize(st.UserID, st.Domain)
			usedBytes += n
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"unlimited":          u.Role == "superadmin",
		"max_sites":          maxSites,
		"max_storage_mb":     maxStorageMB,
		"used_sites":         usedSites,
		"used_storage_bytes": usedBytes,
	})
}

func (s *server) registerAccount(mux *http.ServeMux) {
	// Setup wizard first-run: status publik, create hanya saat 0 user.
	mux.HandleFunc("GET /api/v1/setup/status", s.handleSetupStatus)
	mux.HandleFunc("POST /api/v1/setup", s.handleSetup)
	// Profil & preferensi milik caller sendiri.
	mux.HandleFunc("PATCH /api/v1/account", s.requireAuth(s.handleUpdateAccount))
	mux.HandleFunc("PUT /api/v1/account/prefs", s.requireAuth(s.handleSetPrefs))
	mux.HandleFunc("GET /api/v1/account/usage", s.requireAuth(s.handleMyUsage))
	// MFA (TOTP) milik caller sendiri.
	mux.HandleFunc("GET /api/v1/account/mfa", s.requireAuth(s.handleMFAStatus))
	mux.HandleFunc("POST /api/v1/account/mfa/setup", s.requireAuth(s.handleMFASetup))
	mux.HandleFunc("POST /api/v1/account/mfa/verify", s.requireAuth(s.handleMFAVerify))
	mux.HandleFunc("POST /api/v1/account/mfa/disable", s.requireAuth(s.handleMFADisable))
}

// handleMFAStatus mengembalikan status MFA caller (tanpa membocorkan secret).
func (s *server) handleMFAStatus(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r.Context())
	_, enabled, _, err := s.d.Store.GetMFA(u.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "kesalahan server")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"enabled": enabled})
}

// handleMFASetup memulai enrollment: buat secret baru (belum aktif), balas
// secret + URI otpauth untuk QR. Menolak bila MFA sudah aktif.
func (s *server) handleMFASetup(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r.Context())
	_, enabled, _, err := s.d.Store.GetMFA(u.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "kesalahan server")
		return
	}
	if enabled {
		writeErr(w, http.StatusConflict, "MFA sudah aktif")
		return
	}
	full, err := s.d.Store.GetUserByID(u.ID)
	if err != nil || full == nil {
		writeErr(w, http.StatusInternalServerError, "kesalahan server")
		return
	}
	secret, err := auth.GenerateTOTPSecret()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "gagal buat secret")
		return
	}
	if err := s.d.Store.SetMFASecret(u.ID, secret); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"secret": secret,
		"uri":    auth.TOTPURI(secret, full.Username, "Lamund"),
	})
}

type mfaCodeReq struct {
	Code string `json:"code"`
}

// handleMFAVerify menyelesaikan enrollment: verifikasi kode TOTP terhadap
// secret tersimpan, aktifkan MFA, dan hasilkan recovery codes (ditampilkan
// SEKALI ini saja sebagai plaintext).
func (s *server) handleMFAVerify(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r.Context())
	var req mfaCodeReq
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "body tidak valid")
		return
	}
	secret, _, _, err := s.d.Store.GetMFA(u.ID)
	if err != nil || secret == "" {
		writeErr(w, http.StatusBadRequest, "MFA belum di-setup")
		return
	}
	step, ok := auth.VerifyTOTP(secret, req.Code, time.Now())
	if !ok {
		writeErr(w, http.StatusBadRequest, "kode salah")
		return
	}
	if err := s.d.Store.SetMFALastStep(u.ID, step); err != nil {
		writeErr(w, http.StatusInternalServerError, "kesalahan server")
		return
	}
	if err := s.d.Store.EnableMFA(u.ID); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	codes, err := auth.GenerateRecoveryCodes(10)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "gagal buat recovery codes")
		return
	}
	hashes := make([]string, len(codes))
	for i, c := range codes {
		h, err := auth.HashPassword(c)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "gagal hash recovery codes")
			return
		}
		hashes[i] = h
	}
	if err := s.d.Store.AddRecoveryCodes(u.ID, hashes); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"recovery_codes": codes})
}

// handleMFADisable menonaktifkan MFA milik caller. Butuh kode valid (TOTP atau
// recovery) agar sesi yang dibajak tak bisa mematikan MFA diam-diam.
func (s *server) handleMFADisable(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r.Context())
	var req mfaCodeReq
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "body tidak valid")
		return
	}
	secret, enabled, lastStep, err := s.d.Store.GetMFA(u.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "kesalahan server")
		return
	}
	if !enabled {
		writeErr(w, http.StatusBadRequest, "MFA belum aktif")
		return
	}
	ok := false
	if step, valid := auth.VerifyTOTP(secret, req.Code, time.Now()); valid && step > lastStep {
		ok = true
	}
	if !ok {
		used, err := s.d.Store.ConsumeRecoveryCode(u.ID, func(h string) bool {
			return auth.VerifyPassword(req.Code, h) == nil
		})
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "kesalahan server")
			return
		}
		ok = used
	}
	if !ok {
		writeErr(w, http.StatusBadRequest, "kode salah")
		return
	}
	if err := s.d.Store.DisableMFA(u.ID); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "mfa_disabled"})
}

// handleSetupStatus memberi tahu panel apakah instance masih kosong
// (perlu wizard) atau sudah ada admin (tampilkan login). Publik & tanpa rate-limit.
func (s *server) handleSetupStatus(w http.ResponseWriter, _ *http.Request) {
	n, err := s.d.Store.CountUsers()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "kesalahan server")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"needs_setup": n == 0})
}

type setupReq struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Name     string `json:"name"`
	Email    string `json:"email"`
	Locale   string `json:"locale"`
	Theme    string `json:"theme"`
}

// handleSetup membuat admin pertama. Guard: HANYA berhasil saat 0 user —
// setelah ada satu user, endpoint ini 403 selamanya (tak bisa dipakai untuk
// menambah admin diam-diam).
func (s *server) handleSetup(w http.ResponseWriter, r *http.Request) {
	n, err := s.d.Store.CountUsers()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "kesalahan server")
		return
	}
	if n > 0 {
		writeErr(w, http.StatusForbidden, "instance sudah disiapkan")
		return
	}
	var req setupReq
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "body tidak valid")
		return
	}
	if !validUsername(req.Username) || len(req.Password) < 8 {
		writeErr(w, http.StatusBadRequest, "username 4-32 (huruf kecil/angka/-) & password min 8 karakter")
		return
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "gagal hash password")
		return
	}
	id, err := s.d.Store.CreateUser(store.User{
		Username: req.Username, PasswordHash: hash, Role: "superadmin",
		Name: strings.TrimSpace(req.Name), Email: strings.TrimSpace(req.Email),
	})
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Theme != "" || req.Locale != "" {
		_ = s.d.Store.SetUserPrefs(id, req.Theme, req.Locale)
	}
	// User baru mulai dengan token_version=0; sematkan 0 agar konsisten.
	tok, err := s.d.Issuer.Issue(id, "superadmin", 0)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "gagal menerbitkan token")
		return
	}
	writeJSON(w, http.StatusCreated, loginResp{Token: tok, Role: "superadmin"})
}

type updateAccountReq struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

func (s *server) handleUpdateAccount(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r.Context())
	var req updateAccountReq
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "body tidak valid")
		return
	}
	if err := s.d.Store.UpdateUserProfile(u.ID, strings.TrimSpace(req.Name), strings.TrimSpace(req.Email)); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"name": req.Name, "email": req.Email})
}

type prefsReq struct {
	Theme  string `json:"theme"`
	Locale string `json:"locale"`
}

func (s *server) handleSetPrefs(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r.Context())
	var req prefsReq
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "body tidak valid")
		return
	}
	if err := s.d.Store.SetUserPrefs(u.ID, req.Theme, req.Locale); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, req)
}

// ---- reset kata sandi ----

type forgotReq struct {
	Email string `json:"email"`
}

// handleForgot selalu mengembalikan 200 (anti-enumerasi user).
// Pengecualian: 429 bila rate-limit per-IP terlampaui (tidak membocorkan info user).
// Bila email ada dan Mailer tersedia, token reset dikirim via email.
// Error pengiriman dicatat di log tapi tidak membocorkan status ke klien.
func (s *server) handleForgot(w http.ResponseWriter, r *http.Request) {
	if !s.limiter.allow(clientIP(r) + "|forgot") {
		writeErr(w, http.StatusTooManyRequests, "terlalu banyak percobaan")
		return
	}
	var req forgotReq
	if err := decode(r, &req); err != nil {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}
	s.tryForgot(strings.TrimSpace(req.Email))
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// tryForgot melakukan pengiriman token reset bila user+email ditemukan.
// Selalu silent — dipanggil dari handleForgot yang selalu 200.
func (s *server) tryForgot(email string) {
	if email == "" || s.d.Mailer == nil {
		return
	}
	u, err := s.d.Store.GetUserByEmail(email)
	if err != nil || u == nil {
		return
	}
	token := randomHex(24)
	expires := s.d.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	if err := s.d.Store.CreatePasswordReset(u.ID, token, expires); err != nil {
		log.Printf("WARN: gagal simpan token reset: %v", err)
		return
	}
	body := fmt.Sprintf(
		"<p>Klik tautan berikut untuk mereset kata sandi Anda (berlaku 1 jam):</p>"+
			"<p><a href=\"/reset/%s\">/reset/%s</a></p>"+
			"<p>Abaikan email ini jika Anda tidak meminta reset.</p>",
		token, token,
	)
	if err := s.d.Mailer.Send(u.Email, "Reset sandi Lamund", body); err != nil {
		log.Printf("WARN: gagal kirim email reset ke %s: %v", u.Email, err)
	}
}

type resetReq struct {
	Token string `json:"token"`
	New   string `json:"new"`
}

// handleReset memvalidasi token sekali-pakai dan menetapkan kata sandi baru.
func (s *server) handleReset(w http.ResponseWriter, r *http.Request) {
	if !s.limiter.allow(clientIP(r) + "|reset") {
		writeErr(w, http.StatusTooManyRequests, "terlalu banyak percobaan")
		return
	}
	var req resetReq
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "body tidak valid")
		return
	}
	if len(req.New) < 8 {
		writeErr(w, http.StatusBadRequest, "password baru minimal 8 karakter")
		return
	}
	reset, ok := s.d.Store.GetPasswordReset(req.Token)
	if !ok {
		writeErr(w, http.StatusBadRequest, "token tidak valid")
		return
	}
	exp, err := time.Parse(time.RFC3339, reset.ExpiresAt)
	if err != nil || s.d.Now().After(exp) {
		_ = s.d.Store.DeletePasswordReset(req.Token)
		writeErr(w, http.StatusBadRequest, "token sudah kedaluwarsa")
		return
	}
	hash, err := auth.HashPassword(req.New)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "gagal hash password")
		return
	}
	// Burn token sekali-pakai sebelum mengubah sandi — cegah TOCTOU.
	_ = s.d.Store.DeletePasswordReset(req.Token)
	if err := s.d.Store.SetUserPassword(reset.UserID, hash); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Batalkan semua token JWT lama yang mungkin beredar.
	_ = s.d.Store.BumpTokenVersion(reset.UserID)
	writeJSON(w, http.StatusOK, map[string]string{"status": "password_reset"})
}

// handleVerifyEmail mengonsumsi token verifikasi sekali-pakai → tandai terverifikasi.
func (s *server) handleVerifyEmail(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimSpace(r.URL.Query().Get("token"))
	if token == "" {
		var req struct {
			Token string `json:"token"`
		}
		_ = decode(r, &req)
		token = strings.TrimSpace(req.Token)
	}
	if token == "" {
		writeErr(w, http.StatusBadRequest, "token wajib")
		return
	}
	uid, exp, ok := s.d.Store.GetEmailVerification(token)
	if !ok {
		writeErr(w, http.StatusBadRequest, "token tidak valid")
		return
	}
	if t, err := time.Parse(time.RFC3339, exp); err != nil || s.d.Now().After(t) {
		_ = s.d.Store.DeleteEmailVerification(token)
		writeErr(w, http.StatusBadRequest, "token kedaluwarsa")
		return
	}
	if err := s.d.Store.SetEmailVerified(uid, true); err != nil {
		writeErr(w, http.StatusInternalServerError, "gagal verifikasi")
		return
	}
	_ = s.d.Store.DeleteEmailVerification(token)
	writeJSON(w, http.StatusOK, map[string]string{"status": "verified"})
}
