// Package certinfo membaca metadata sertifikat langsung dari penyimpanan
// certmagic (berkas .crt), tanpa perlu berbagi state dengan data plane.
package certinfo

import (
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Status sertifikat relatif terhadap waktu sekarang.
const (
	StatusValid    = "valid"
	StatusExpiring = "expiring" // < 21 hari
	StatusExpired  = "expired"
	StatusNone     = "none" // belum ada sertifikat
)

// renewWindow: certmagic memperpanjang ~30 hari sebelum kedaluwarsa;
// tandai "expiring" sedikit di bawahnya agar panel memberi sinyal dini.
const renewWindow = 21 * 24 * time.Hour

// Info adalah ringkasan sertifikat satu domain.
type Info struct {
	Domain   string    `json:"domain"`
	Issuer   string    `json:"issuer"`
	NotAfter time.Time `json:"not_after"`
	Status   string    `json:"status"`
}

// Read mengembalikan info sertifikat terbaik untuk domain dari certmagic
// storage di bawah certDir. Bila tak ada, Status=none.
func Read(certDir, domain string, now time.Time) Info {
	info := Info{Domain: domain, Status: StatusNone}
	// Layout: <certDir>/certificates/<ca-dir>/<domain>/<domain>.crt
	pattern := filepath.Join(certDir, "certificates", "*", domain, domain+".crt")
	matches, _ := filepath.Glob(pattern)

	var best *x509.Certificate
	var bestStaging bool
	for _, path := range matches {
		cert := parseLeaf(path)
		if cert == nil {
			continue
		}
		staging := strings.Contains(strings.ToUpper(cert.Issuer.String()), "STAGING")
		// Prefer sertifikat produksi; di antara yang sekelas pilih NotAfter terbaru.
		if best == nil || (bestStaging && !staging) ||
			(bestStaging == staging && cert.NotAfter.After(best.NotAfter)) {
			best, bestStaging = cert, staging
		}
	}
	if best == nil {
		return info
	}
	info.Issuer = issuerName(best)
	info.NotAfter = best.NotAfter
	info.Status = statusFor(best.NotAfter, now)
	return info
}

// List mengembalikan info sertifikat untuk banyak domain, urut per domain.
func List(certDir string, domains []string, now time.Time) []Info {
	out := make([]Info, 0, len(domains))
	for _, d := range domains {
		out = append(out, Read(certDir, d, now))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Domain < out[j].Domain })
	return out
}

func parseLeaf(path string) *x509.Certificate {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	block, _ := pem.Decode(raw) // leaf adalah blok pertama
	if block == nil {
		return nil
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil
	}
	return cert
}

func statusFor(notAfter, now time.Time) string {
	switch {
	case notAfter.Before(now):
		return StatusExpired
	case notAfter.Before(now.Add(renewWindow)):
		return StatusExpiring
	default:
		return StatusValid
	}
}

func issuerName(c *x509.Certificate) string {
	if c.Issuer.CommonName != "" {
		return c.Issuer.CommonName
	}
	if len(c.Issuer.Organization) > 0 {
		return c.Issuer.Organization[0]
	}
	return c.Issuer.String()
}
