// Package api adalah control plane REST lamund (/api/v1) untuk panel & klien.
package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/lamun-my-id/lamund/internal/auth"
	"github.com/lamun-my-id/lamund/internal/builder"
	"github.com/lamun-my-id/lamund/internal/email"
	"github.com/lamun-my-id/lamund/internal/runner"
	"github.com/lamun-my-id/lamund/internal/store"
)

// Deps adalah dependensi yang disuntikkan ke API.
type Deps struct {
	Store   *store.Store
	Issuer  *auth.Issuer
	Sites   *store.SiteFS       // penyimpanan berkas situs per-tenant (upload/deploy)
	Runner  *runner.Supervisor  // supervisor proses aplikasi (run-app)
	Builder *builder.Manager    // build step (deteksi + install/build)
	CertDir string           // storage certmagic (untuk baca info sertifikat)
	LogDir  string           // direktori access log per-domain (untuk tail)
	DataDir string           // direktori data (untuk statistik disk)
	Now     func() time.Time // opsional; default time.Now (untuk uji)

	// GitHubClientID: client_id OAuth App untuk Device Flow. Kosong → default
	// Lamun bawaan. Tak butuh client secret (Device Flow).
	GitHubClientID string

	// OnSiteChange dipanggil setelah situs dibuat/diubah/dihapus, agar data
	// plane (proses terpisah) memuat ulang tabel routing-nya. Opsional.
	OnSiteChange func()

	// Mailer dipakai untuk kirim email transaksional (reset sandi, undangan tim).
	// Bila nil, handler email mengembalikan 503.
	Mailer email.Mailer

	// PublicIP adalah IP publik server ini; dipakai untuk echo glue record DNS
	// di endpoint GET /dns/settings dan GET /dns/zones/{domain}. Kosong = tak ditampilkan.
	PublicIP string

	// BaseURL = origin panel publik. Dipakai untuk membentuk URL webhook push
	// GitHub saat fitur "buat repo" memasang webhook. Kosong = webhook tak
	// terbentuk (deploy manual tetap jalan).
	BaseURL string
}

type server struct {
	d            Deps
	limiter      *limiter
	devMu        sync.Mutex
	devPending   map[int64]*devicePending
	deployMu     sync.Mutex
	deployStatus map[string]string
}

// New membangun handler HTTP untuk seluruh /api/v1.
func New(d Deps) http.Handler {
	if d.Now == nil {
		d.Now = time.Now
	}
	s := &server{d: d, limiter: newLimiter(d.Now)}
	s.devPending = map[int64]*devicePending{}
	s.deployStatus = map[string]string{}
	mux := http.NewServeMux()
	s.registerAuth(mux)
	s.registerAccount(mux)
	s.registerTeams(mux)
	s.registerSites(mux)
	s.registerAdmin(mux)
	s.registerCerts(mux)
	s.registerFiles(mux)
	s.registerRoutes(mux)
	s.registerApps(mux)
	s.registerAnalytics(mux)
	s.registerConnections(mux)
	s.registerEmail(mux)
	s.registerDNS(mux)
	mux.HandleFunc("GET /api/v1/server/stats", s.requireAuth(s.handleServerStats))
	mux.HandleFunc("GET /api/v1/health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	return mux
}

func (s *server) setDeployStatus(domain, st string) {
	s.deployMu.Lock()
	s.deployStatus[domain] = st
	s.deployMu.Unlock()
}

func (s *server) getDeployStatus(domain string) string {
	s.deployMu.Lock()
	defer s.deployMu.Unlock()
	return s.deployStatus[domain]
}

func (s *server) registerAuth(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/auth/login", s.handleLogin)
	mux.HandleFunc("POST /api/v1/auth/login/mfa", s.handleLoginMFA)
	mux.HandleFunc("POST /api/v1/auth/logout", s.handleLogout)
	mux.HandleFunc("GET /api/v1/auth/me", s.requireAuth(s.handleMe))
	mux.HandleFunc("POST /api/v1/auth/password", s.requireAuth(s.handleChangePassword))
	// Reset kata sandi: publik (tidak butuh token).
	mux.HandleFunc("POST /api/v1/auth/forgot", s.handleForgot)
	mux.HandleFunc("POST /api/v1/auth/reset", s.handleReset)
	mux.HandleFunc("POST /api/v1/auth/verify", s.handleVerifyEmail)
}

type loginReq struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type loginResp struct {
	Token string `json:"token"`
	Role  string `json:"role"`
}

func (s *server) handleLogin(w http.ResponseWriter, r *http.Request) {
	// Rate-limit per IP+username agar brute-force mahal.
	var req loginReq
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "body tidak valid")
		return
	}
	// Normalisasi username: lowercase + trim agar "Admin ", "admin", "ADMIN"
	// dianggap identitas yang sama dan berbagi satu bucket rate-limiter.
	req.Username = strings.ToLower(strings.TrimSpace(req.Username))
	if !s.limiter.allow(clientIP(r) + "|" + req.Username) {
		writeErr(w, http.StatusTooManyRequests, "terlalu banyak percobaan, coba lagi nanti")
		return
	}
	u, err := s.d.Store.GetUserByUsername(req.Username)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "kesalahan server")
		return
	}
	// Verifikasi password walau user tak ada / disabled agar waktu respons seragam.
	hash := dummyHash
	if u != nil {
		hash = u.PasswordHash
	}
	verr := auth.VerifyPassword(req.Password, hash)
	if u == nil || u.Disabled || verr != nil {
		writeErr(w, http.StatusUnauthorized, "username atau password salah")
		return
	}
	// Bila MFA aktif untuk user ini, jangan terbitkan JWT sesi. Balas
	// pending-token singkat; klien harus menyelesaikan langkah-2 di /login/mfa.
	if _, enabled, _, _ := s.d.Store.GetMFA(u.ID); enabled {
		pending, err := s.d.Issuer.IssueMFAPending(u.ID)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "gagal menerbitkan token")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"mfa_required": true, "pending": pending})
		return
	}
	tok, err := s.d.Issuer.Issue(u.ID, u.Role, u.TokenVersion)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "gagal menerbitkan token")
		return
	}
	writeJSON(w, http.StatusOK, loginResp{Token: tok, Role: u.Role})
}

type loginMFAReq struct {
	Pending string `json:"pending"`
	Code    string `json:"code"`
}

// handleLoginMFA menyelesaikan langkah-2 login MFA: verifikasi kode TOTP atau
// recovery code terhadap pending-token, lalu terbitkan JWT sesi.
func (s *server) handleLoginMFA(w http.ResponseWriter, r *http.Request) {
	if !s.limiter.allow(clientIP(r) + "|mfa") {
		writeErr(w, http.StatusTooManyRequests, "terlalu banyak percobaan, coba lagi nanti")
		return
	}
	var req loginMFAReq
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "body tidak valid")
		return
	}
	userID, err := s.d.Issuer.ParseMFAPending(req.Pending)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "sesi MFA tidak valid")
		return
	}
	u, err := s.d.Store.GetUserByID(userID)
	if err != nil || u == nil || u.Disabled {
		writeErr(w, http.StatusUnauthorized, "sesi MFA tidak valid")
		return
	}
	secret, enabled, lastStep, err := s.d.Store.GetMFA(userID)
	if err != nil || !enabled {
		writeErr(w, http.StatusUnauthorized, "sesi MFA tidak valid")
		return
	}

	authenticated := false
	// Coba TOTP dengan anti-replay: step yang cocok harus lebih baru dari lastStep.
	if step, ok := auth.VerifyTOTP(secret, req.Code, time.Now()); ok && step > lastStep {
		if err := s.d.Store.SetMFALastStep(userID, step); err != nil {
			writeErr(w, http.StatusInternalServerError, "kesalahan server")
			return
		}
		authenticated = true
	}
	// Bila TOTP gagal/replay, coba recovery code (sekali-pakai).
	if !authenticated {
		used, err := s.d.Store.ConsumeRecoveryCode(userID, func(h string) bool {
			return auth.VerifyPassword(req.Code, h) == nil
		})
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "kesalahan server")
			return
		}
		authenticated = used
	}
	if !authenticated {
		writeErr(w, http.StatusUnauthorized, "kode salah")
		return
	}

	tok, err := s.d.Issuer.Issue(u.ID, u.Role, u.TokenVersion)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "gagal menerbitkan token")
		return
	}
	writeJSON(w, http.StatusOK, loginResp{Token: tok, Role: u.Role})
}

// handleLogout tanpa server-state: klien membuang token. Disediakan agar
// kontrak API lengkap dan siap bila kelak ada blacklist token.
func (s *server) handleLogout(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "logged_out"})
}

func (s *server) handleMe(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r.Context())
	full, err := s.d.Store.GetUserByID(u.ID)
	if err != nil || full == nil {
		writeErr(w, http.StatusInternalServerError, "kesalahan server")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id": full.ID, "username": full.Username, "role": full.Role,
		"name": full.Name, "email": full.Email,
		"theme": full.Theme, "locale": full.Locale,
	})
}

type changePasswordReq struct {
	Current string `json:"current"`
	New     string `json:"new"`
}

func (s *server) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r.Context())
	var req changePasswordReq
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "body tidak valid")
		return
	}
	if len(req.New) < 8 {
		writeErr(w, http.StatusBadRequest, "password baru minimal 8 karakter")
		return
	}
	full, err := s.d.Store.GetUserByID(u.ID)
	if err != nil || full == nil {
		writeErr(w, http.StatusInternalServerError, "kesalahan server")
		return
	}
	if auth.VerifyPassword(req.Current, full.PasswordHash) != nil {
		writeErr(w, http.StatusUnauthorized, "password sekarang salah")
		return
	}
	hash, err := auth.HashPassword(req.New)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "gagal hash password")
		return
	}
	if err := s.d.Store.SetUserPassword(u.ID, hash); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Batalkan semua token lama dengan menaikkan token_version.
	_ = s.d.Store.BumpTokenVersion(u.ID)
	writeJSON(w, http.StatusOK, map[string]string{"status": "password_changed"})
}

func decode(r *http.Request, v any) error {
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

// dummyHash adalah hash argon2id valid dari string acak; dipakai saat user
// tak ditemukan agar login melakukan kerja verifikasi yang sama (anti user-enumeration timing).
var dummyHash = mustHash("l4mund-dummy-constant-untuk-timing")

func mustHash(s string) string {
	h, err := auth.HashPassword(s)
	if err != nil {
		panic(err)
	}
	return h
}
