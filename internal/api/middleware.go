package api

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/lamun-my-id/lamund/internal/auth"
)

type ctxKey int

const userKey ctxKey = iota

// authUser adalah identitas terverifikasi yang menempel di request context.
type authUser struct {
	ID    int64
	Role  string
	Email string // email user (dibutuhkan untuk validasi binding invite)
}

func userFrom(ctx context.Context) (*authUser, bool) {
	u, ok := ctx.Value(userKey).(*authUser)
	return u, ok
}

// requireAuth menerima Bearer JWT (panel) ATAU API key berprefix "lmd_".
func (s *server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tok := bearer(r)
		if tok == "" {
			writeErr(w, http.StatusUnauthorized, "butuh autentikasi")
			return
		}
		u := s.resolveCredential(tok)
		if u == nil {
			writeErr(w, http.StatusUnauthorized, "kredensial tidak valid")
			return
		}
		ctx := context.WithValue(r.Context(), userKey, u)
		next(w, r.WithContext(ctx))
	}
}

// requireAdmin membungkus requireAuth lalu menolak non-superadmin.
func (s *server) requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return s.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		if u, _ := userFrom(r.Context()); u == nil || u.Role != "superadmin" {
			writeErr(w, http.StatusForbidden, "butuh hak admin")
			return
		}
		next(w, r)
	})
}

// resolveCredential memverifikasi token & memuat user, atau nil bila gagal.
func (s *server) resolveCredential(tok string) *authUser {
	if strings.HasPrefix(tok, "lmd_") {
		key, err := s.d.Store.GetAPIKeyByHash(auth.HashAPIKey(tok))
		if err != nil || key == nil {
			return nil
		}
		u, err := s.d.Store.GetUserByID(key.UserID)
		if err != nil || u == nil || u.Disabled {
			return nil
		}
		return &authUser{ID: u.ID, Role: u.Role, Email: u.Email}
	}
	claims, err := s.d.Issuer.Parse(tok)
	if err != nil {
		return nil
	}
	// Token dengan Kind non-kosong (mis. "mfa_pending") bukan token sesi dan
	// tak boleh mengautentikasi endpoint biasa.
	if claims.Kind != "" {
		return nil
	}
	u, err := s.d.Store.GetUserByID(claims.UserID)
	if err != nil || u == nil || u.Disabled {
		return nil
	}
	// Tolak token yang sudah dibatalkan: ver di token harus cocok dengan
	// token_version terkini di DB. Token lama (sebelum fitur ini) punya ver=0
	// dan user.TokenVersion=0 → tetap valid sampai ada bump.
	if claims.Ver != u.TokenVersion {
		return nil
	}
	return &authUser{ID: u.ID, Role: u.Role, Email: u.Email}
}

func bearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if after, ok := strings.CutPrefix(h, "Bearer "); ok {
		return strings.TrimSpace(after)
	}
	return ""
}

// ---- rate limiter login (in-memory, per-key sliding window) ----

type limiter struct {
	mu      sync.Mutex
	hits    map[string][]time.Time
	now     func() time.Time
	window  time.Duration
	maxHits int
}

func newLimiter(now func() time.Time) *limiter {
	return &limiter{hits: map[string][]time.Time{}, now: now, window: time.Minute, maxHits: 5}
}

// allow mencatat satu percobaan untuk key dan mengembalikan false bila
// jumlah percobaan dalam window melebihi ambang.
func (l *limiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	cutoff := l.now().Add(-l.window)
	kept := l.hits[key][:0]
	for _, t := range l.hits[key] {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	kept = append(kept, l.now())
	l.hits[key] = kept
	return len(kept) <= l.maxHits
}

// clientIP mengembalikan IP asli klien dengan mempertimbangkan reverse proxy.
// Bila RemoteAddr adalah loopback (127.0.0.1 atau ::1), berarti request datang
// dari data-plane proxy internal — percayai hop terakhir X-Forwarded-For.
// Bila RemoteAddr bukan loopback (mis. akses langsung), abaikan XFF untuk
// mencegah klien memalsukan IP dan mem-bypass rate limiter.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if host == "127.0.0.1" || host == "::1" {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			return strings.TrimSpace(parts[len(parts)-1])
		}
	}
	return host
}

// PanelSecurityHeaders membungkus handler dengan header keamanan HTTP untuk
// panel admin Vue dan REST API-nya. Header ini berlaku untuk SEMUA respons
// dari top-level mux (panel + /api/v1/).
//
// CSP: script-src hanya 'self' (bundle Vite eksternal, bukan inline).
// style-src 'unsafe-inline' diizinkan karena Vue scoped style + Google Fonts
// stylesheet menyertakan deklarasi inline. font-src mengizinkan fonts.gstatic.com
// karena panel memuat web font Google Fonts.
// Catatan: panel memuat Google Fonts dari fonts.googleapis.com (style-src) dan
// fonts.gstatic.com (font-src) — ini dikonfirmasi dari web/dist/index.html.
func PanelSecurityHeaders(next http.Handler) http.Handler {
	const csp = "default-src 'self';" +
		" img-src 'self' data:;" +
		" style-src 'self' 'unsafe-inline' https://fonts.googleapis.com;" +
		" font-src 'self' https://fonts.gstatic.com;" +
		" script-src 'self';" +
		" connect-src 'self';" +
		" frame-ancestors 'none';" +
		" base-uri 'self'"
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Content-Security-Policy", csp)
		h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		next.ServeHTTP(w, r)
	})
}

// ---- helper respons JSON ----

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}
