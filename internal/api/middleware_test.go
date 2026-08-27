package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestPanelSecurityHeaders memverifikasi bahwa PanelSecurityHeaders menyetel
// header keamanan wajib pada setiap respons.
func TestPanelSecurityHeaders(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := PanelSecurityHeaders(inner)

	paths := []string{"/", "/api/v1/auth/me"}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest("GET", path, nil)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
				t.Errorf("X-Content-Type-Options = %q, mau nosniff", got)
			}
			if got := rec.Header().Get("X-Frame-Options"); got != "DENY" {
				t.Errorf("X-Frame-Options = %q, mau DENY", got)
			}
			if got := rec.Header().Get("Content-Security-Policy"); got == "" {
				t.Error("Content-Security-Policy kosong")
			}
			if got := rec.Header().Get("Referrer-Policy"); got != "no-referrer" {
				t.Errorf("Referrer-Policy = %q, mau no-referrer", got)
			}
			if got := rec.Header().Get("Strict-Transport-Security"); got == "" {
				t.Error("Strict-Transport-Security kosong")
			}
		})
	}
}

func TestClientIP(t *testing.T) {
	cases := []struct {
		name       string
		remoteAddr string
		xff        string
		want       string
	}{
		{
			name:       "loopback + XFF satu hop",
			remoteAddr: "127.0.0.1:1234",
			xff:        "1.2.3.4",
			want:       "1.2.3.4",
		},
		{
			name:       "loopback + XFF multi hop → hop terakhir",
			remoteAddr: "127.0.0.1:1234",
			xff:        "9.9.9.9, 1.2.3.4",
			want:       "1.2.3.4",
		},
		{
			name:       "non-loopback + XFF → abaikan XFF (cegah spoof)",
			remoteAddr: "203.0.113.5:1234",
			xff:        "1.2.3.4",
			want:       "203.0.113.5",
		},
		{
			name:       "IPv6 loopback + XFF → percaya XFF",
			remoteAddr: "[::1]:1234",
			xff:        "5.6.7.8",
			want:       "5.6.7.8",
		},
		{
			name:       "loopback tanpa XFF → pakai RemoteAddr",
			remoteAddr: "127.0.0.1:1234",
			xff:        "",
			want:       "127.0.0.1",
		},
		{
			name:       "non-loopback tanpa XFF → pakai RemoteAddr",
			remoteAddr: "203.0.113.5:9999",
			xff:        "",
			want:       "203.0.113.5",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			req.RemoteAddr = tc.remoteAddr
			if tc.xff != "" {
				req.Header.Set("X-Forwarded-For", tc.xff)
			}
			got := clientIP(req)
			if got != tc.want {
				t.Errorf("clientIP = %q, mau %q", got, tc.want)
			}
		})
	}
}
