package store

import "testing"

func TestValidateProxyTarget(t *testing.T) {
	cases := []struct {
		raw           string
		allowExternal bool
		want          string
		wantErr       bool
	}{
		// --- loopback: TOLAK bila !allowExternal (SSRF ke panel) ---
		{"127.0.0.1:3000", false, "", true},
		{"http://127.0.0.1:8080", false, "", true},
		{"http://localhost:3000", false, "", true},
		{"http://[::1]:8080", false, "", true},

		// --- loopback: IZINKAN bila allowExternal=true (jalur operator/server) ---
		{"http://127.0.0.1:8080", true, "http://127.0.0.1:8080", false},
		{"http://localhost:8080", true, "http://localhost:8080", false},
		{"http://[::1]:9000", true, "http://[::1]:9000", false},

		// --- eksternal non-loopback: butuh allowExternal ---
		{"https://api.internal:443", true, "https://api.internal:443", false},
		{"http://10.0.0.5:3000", false, "", true},
		{"http://10.0.0.5:3000", true, "http://10.0.0.5:3000", false},

		// --- metadata endpoint cloud: TOLAK tanpa allowExternal (sudah ada) ---
		{"http://169.254.169.254", false, "", true},
		{"http://169.254.169.254/", false, "", true},

		// --- target eksternal valid dengan allowExternal ---
		{"http://example.com", true, "http://example.com", false},

		// --- backend PUBLIK diizinkan di jalur user (reverse-proxy) ---
		{"http://example.com", false, "http://example.com", false},
		{"https://api.publik.io:8443", false, "https://api.publik.io:8443", false},
		{"http://93.184.216.34:8080", false, "http://93.184.216.34:8080", false},
		// --- privat/link-local/ULA di jalur user: TOLAK ---
		{"http://192.168.1.10:3000", false, "", true},
		{"http://172.16.0.5", false, "", true},
		{"http://[fc00::1]:80", false, "", true},

		// --- kasus lain ---
		{"ftp://x:21", false, "", true},
		{"", false, "", true},
	}
	for _, c := range cases {
		got, err := ValidateProxyTarget(c.raw, c.allowExternal)
		if c.wantErr {
			if err == nil {
				t.Errorf("ValidateProxyTarget(%q,%v) = %q, mau error", c.raw, c.allowExternal, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("ValidateProxyTarget(%q,%v) error: %v", c.raw, c.allowExternal, err)
		} else if got != c.want {
			t.Errorf("ValidateProxyTarget(%q,%v) = %q, mau %q", c.raw, c.allowExternal, got, c.want)
		}
	}
}
