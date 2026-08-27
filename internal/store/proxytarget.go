package store

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

// isInternalIP melaporkan apakah ip termasuk ruang internal yang berbahaya untuk
// target proxy user (SSRF): loopback, privat RFC1918, link-local (termasuk
// metadata cloud 169.254.169.254), unique-local IPv6, dan unspecified.
func isInternalIP(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsUnspecified()
}

// ValidateProxyTarget memvalidasi & menormalisasi alamat upstream untuk site
// proxy. Pertahanan anti-SSRF (PRD §11):
//   - allowExternal=false (jalur user): target INTERNAL ditolak — loopback
//     (localhost/127.x/::1), privat (RFC1918), link-local/metadata, ULA. Host
//     PUBLIK (IP/hostname) diizinkan → fitur reverse-proxy ke backend publik.
//     Ini mencegah tenant memproxy ke panel kontrol loopback-only atau metadata.
//   - allowExternal=true (jalur operator/server): semua diizinkan (termasuk
//     loopback), mis. app terkelola yang dibuat server-side.
// Return bentuk ternormalisasi "scheme://host[:port]" (tanpa path/query).
func ValidateProxyTarget(raw string, allowExternal bool) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("target proxy kosong")
	}
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw // default skema
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("target proxy tidak valid: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("skema %q tidak didukung (pakai http/https)", u.Scheme)
	}
	host := u.Hostname()
	if host == "" {
		return "", fmt.Errorf("target proxy tanpa host")
	}

	if !allowExternal {
		// Jalur user: tolak target internal (SSRF), izinkan host publik.
		if host == "localhost" {
			return "", fmt.Errorf("target loopback tidak diizinkan (pakai fitur app untuk proxy ke port lokal)")
		}
		if ip := net.ParseIP(host); ip != nil && isInternalIP(ip) {
			return "", fmt.Errorf("target internal/privat tidak diizinkan (%s) — pakai backend publik atau fitur app", host)
		}
		// Hostname publik & IP publik diizinkan. DNS rebinding (hostname yang
		// me-resolve ke IP internal) dicegah di lapisan proxy: dialGuard menolak
		// koneksi ke IP internal pasca-resolusi pada tiap dial (internal/proxy).
	}

	return u.Scheme + "://" + u.Host, nil
}
