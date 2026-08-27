package acme

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net/http"

	"github.com/caddyserver/certmagic"
)

// Config menyetel cert Manager.
type Config struct {
	Email        string              // email akun ACME
	StorageDir   string              // lokasi simpan cert (mis. /var/lib/lamund/certs)
	CA           string              // "staging" | "production" | URL directory (Pebble)
	TrustedRoots *x509.CertPool     // root CA tambahan untuk dipercaya (Pebble saat test)
	DNSProvider  certmagic.DNSProvider // provider DNS-01 (nil = HTTP-01 saja)
	DNSResolvers []string            // resolver cek propagasi DNS-01 (kosong = 8.8.8.8/1.1.1.1)
}

// Manager membungkus certmagic: terbit + simpan + perpanjang sertifikat HTTPS
// otomatis. certmagic memegang cert & renewal loop internal; Manager
// menyediakan TLSConfig (:443), responder HTTP-01 (:80), dan Manage/Unmanage.
type Manager struct {
	magic  *certmagic.Config
	issuer *certmagic.ACMEIssuer
}

func caURL(ca string) string {
	switch ca {
	case "", "staging":
		return certmagic.LetsEncryptStagingCA
	case "production", "prod":
		return certmagic.LetsEncryptProductionCA
	default:
		return ca // URL directory kustom (mis. Pebble)
	}
}

func NewManager(c Config) *Manager {
	// GetConfigForCert dipanggil saat serving; `magic` sudah terisi saat itu.
	var magic *certmagic.Config
	cache := certmagic.NewCache(certmagic.CacheOptions{
		GetConfigForCert: func(certmagic.Certificate) (*certmagic.Config, error) {
			return magic, nil
		},
	})
	magic = certmagic.New(cache, certmagic.Config{
		Storage: &certmagic.FileStorage{Path: c.StorageDir},
	})
	// Issuer HTTP-01 (default) — melayani SEMUA domain, termasuk yang bukan zona
	// terkelola. Ini SELALU issuer pertama.
	httpIssuer := certmagic.NewACMEIssuer(magic, certmagic.ACMEIssuer{
		CA: caURL(c.CA), Email: c.Email, Agreed: true, TrustedRoots: c.TrustedRoots,
	})
	issuers := []certmagic.Issuer{httpIssuer}
	// Bila ada DNS provider, tambahkan issuer DNS-01 sebagai FALLBACK (kedua).
	// certmagic mencoba issuer berurutan: domain non-wildcard diselesaikan
	// HTTP-01 oleh issuer pertama; nama wildcard (yang TAK bisa HTTP-01) jatuh
	// ke issuer DNS-01. Dengan begitu mengaktifkan DNS-01 TIDAK mematikan
	// HTTP-01 untuk domain non-terkelola (kalau DNS01Solver dipasang pada satu-
	// satunya issuer, certmagic menonaktifkan HTTP-01 secara global).
	if c.DNSProvider != nil {
		// Resolvers publik untuk cek propagasi TXT: certmagic mengecek lewat
		// resolver rekursif ini (bukan query authoritative langsung). Penting di
		// lingkungan NAT (mis. OCII) di mana host TAK bisa menghubungi IP
		// publiknya sendiri (hairpin) — resolver publik menjangkau NS kita dari
		// luar tanpa hairpin. DNS-01 memang butuh internet (ACME) jadi ini aman.
		resolvers := c.DNSResolvers
		if len(resolvers) == 0 {
			resolvers = []string{"8.8.8.8:53", "1.1.1.1:53"}
		}
		dnsIssuer := certmagic.NewACMEIssuer(magic, certmagic.ACMEIssuer{
			CA: caURL(c.CA), Email: c.Email, Agreed: true, TrustedRoots: c.TrustedRoots,
			DNS01Solver: &certmagic.DNS01Solver{DNSManager: certmagic.DNSManager{
				DNSProvider: c.DNSProvider,
				Resolvers:   resolvers,
			}},
		})
		issuers = append(issuers, dnsIssuer)
	}
	magic.Issuers = issuers
	// issuer HTTP-01 dipakai untuk HTTPChallengeHandler (:80).
	return &Manager{magic: magic, issuer: httpIssuer}
}

// TLSConfig untuk listener :443 — menyajikan cert per-SNI & solusi TLS-ALPN.
func (m *Manager) TLSConfig() *tls.Config {
	t := m.magic.TLSConfig()
	t.NextProtos = append([]string{"h2", "http/1.1"}, t.NextProtos...)
	return t
}

// HTTPChallengeHandler membungkus handler :80: intercept
// /.well-known/acme-challenge/, sisanya diteruskan ke next.
func (m *Manager) HTTPChallengeHandler(next http.Handler) http.Handler {
	return m.issuer.HTTPChallengeHandler(next)
}

// Manage menerbitkan (jika perlu) & mengelola perpanjangan domain-domain ini.
func (m *Manager) Manage(ctx context.Context, domains []string) error {
	return m.magic.ManageSync(ctx, domains)
}

// Catatan: saat site dihapus, route vhost-nya hilang sehingga tak dilayani;
// cert-nya tetap di storage tanpa bahaya (pembersihan = refinement pasca-v1).
