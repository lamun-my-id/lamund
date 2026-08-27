// Package proxy adalah reverse proxy untuk site tipe proxy: meneruskan trafik
// masuk ke aplikasi upstream (mis. app Node di :3000) dengan timeout wajar dan
// halaman 502 yang sopan saat upstream mati (bukan panic/stacktrace).
package proxy

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"syscall"
	"time"
)

// isInternalIP: IP yang berbahaya sebagai target proxy user (SSRF) — loopback,
// privat RFC1918, link-local (termasuk metadata 169.254.169.254), unspecified.
func isInternalIP(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsUnspecified()
}

// hostIsInternalLiteral melaporkan apakah host (dari URL target terkonfigurasi)
// memang sengaja internal — mis. app terkelola yang di-proxy ke 127.0.0.1:port.
// Untuk target seperti ini koneksi internal DIIZINKAN; untuk selainnya (backend
// publik user) koneksi ke IP internal ditolak saat dial (cegah DNS rebinding).
func hostIsInternalLiteral(host string) bool {
	if host == "localhost" {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return isInternalIP(ip)
	}
	return false
}

// dialGuard menolak koneksi ke IP internal (dipanggil SETELAH resolusi DNS,
// pada tiap koneksi) — mempertahankan proteksi walau hostname me-resolve ke IP
// internal (DNS rebinding). Hanya dipasang bila target bukan literal internal.
func dialGuard(network, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	if ip := net.ParseIP(host); ip != nil && isInternalIP(ip) {
		return fmt.Errorf("koneksi proxy ke IP internal ditolak (%s)", host)
	}
	return nil
}

// New membuat handler reverse proxy ke target (harus URL absolut valid).
func New(target string) (http.Handler, error) {
	u, err := url.Parse(target)
	if err != nil {
		return nil, err
	}
	rp := httputil.NewSingleHostReverseProxy(u)

	// Dialer dengan timeout; untuk target publik (bukan literal internal) pasang
	// guard yang menolak koneksi ke IP internal pasca-resolusi (anti DNS rebinding).
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	if !hostIsInternalLiteral(u.Hostname()) {
		dialer.Control = dialGuard
	}
	// Timeout: cegah koneksi menggantung menghabiskan resource.
	rp.Transport = &http.Transport{
		DialContext:           dialer.DialContext,
		ResponseHeaderTimeout: 15 * time.Second,
		IdleConnTimeout:       90 * time.Second,
		MaxIdleConnsPerHost:   32,
	}

	orig := rp.Director
	rp.Director = func(r *http.Request) {
		// Hapus header yang dikirim klien sebelum memanggil Director bawaan,
		// agar httputil tidak menggunakan nilai buatan klien.
		r.Header.Del("X-Forwarded-For")
		r.Header.Del("X-Real-IP")
		r.Header.Del("Forwarded")

		orig(r)

		r.Header.Set("X-Forwarded-Proto", schemeOf(r))
		if r.Host != "" {
			r.Header.Set("X-Forwarded-Host", r.Host)
		}
		// Timpa (Set, bukan Add) X-Forwarded-For & X-Real-IP dengan IP nyata
		// dari RemoteAddr — cegah klien memalsukan XFF ke upstream.
		if ip, _, e := net.SplitHostPort(r.RemoteAddr); e == nil {
			r.Header.Set("X-Forwarded-For", ip)
			r.Header.Set("X-Real-IP", ip)
		}
	}

	rp.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusBadGateway)
		w.Write([]byte("<!doctype html><title>502</title><h1>502</h1><p>Aplikasi di belakang situs ini sedang tidak merespons.</p><hr><small>lamund</small>"))
	}

	return rp, nil
}

func schemeOf(r *http.Request) string {
	if r.TLS != nil {
		return "https"
	}
	return "http"
}
