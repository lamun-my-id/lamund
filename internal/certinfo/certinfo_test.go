package certinfo

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// leafPEM membuat leaf cert yang ditandatangani CA ber-CommonName issuerCN,
// sehingga Issuer berbeda dari Subject (seperti cert Let's Encrypt asli).
func leafPEM(t *testing.T, domain, issuerCN string, notAfter time.Time) []byte {
	t.Helper()
	now := time.Now()
	caKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	caTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(100), Subject: pkix.Name{CommonName: issuerCN},
		NotBefore: now.Add(-365 * 24 * time.Hour), NotAfter: now.Add(365 * 24 * time.Hour),
		IsCA: true, KeyUsage: x509.KeyUsageCertSign, BasicConstraintsValid: true,
	}
	caDER, _ := x509.CreateCertificate(rand.Reader, caTmpl, caTmpl, &caKey.PublicKey, caKey)
	caCert, _ := x509.ParseCertificate(caDER)

	leafKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	leafTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: domain},
		NotBefore: notAfter.Add(-90 * 24 * time.Hour), NotAfter: notAfter,
		DNSNames: []string{domain},
	}
	leafDER, err := x509.CreateCertificate(rand.Reader, leafTmpl, caCert, &leafKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leafDER})
}

// writeCert menulis leaf cert ke layout certmagic dan mengembalikan certDir.
func writeCert(t *testing.T, domain, caDir, issuerCN string, notAfter time.Time) string {
	t.Helper()
	certDir := t.TempDir()
	writeCertInto(t, certDir, domain, caDir, issuerCN, notAfter)
	return certDir
}

func writeCertInto(t *testing.T, certDir, domain, caDir, issuerCN string, notAfter time.Time) {
	t.Helper()
	dir := filepath.Join(certDir, "certificates", caDir, domain)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, domain+".crt"), leafPEM(t, domain, issuerCN, notAfter), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestReadValid(t *testing.T) {
	now := time.Now()
	certDir := writeCert(t, "a.com", "acme-v02.api.letsencrypt.org-directory", "R3", now.Add(60*24*time.Hour))
	info := Read(certDir, "a.com", now)
	if info.Status != StatusValid || info.Issuer != "R3" {
		t.Fatalf("mau valid/R3, dapat %+v", info)
	}
}

func TestReadExpiringAndExpired(t *testing.T) {
	now := time.Now()
	ca := "acme-v02.api.letsencrypt.org-directory"
	exp := writeCert(t, "soon.com", ca, "R3", now.Add(10*24*time.Hour))
	if s := Read(exp, "soon.com", now).Status; s != StatusExpiring {
		t.Fatalf("10 hari harus expiring, dapat %s", s)
	}
	dead := writeCert(t, "old.com", ca, "R3", now.Add(-24*time.Hour))
	if s := Read(dead, "old.com", now).Status; s != StatusExpired {
		t.Fatalf("kadaluarsa harus expired, dapat %s", s)
	}
}

func TestReadNone(t *testing.T) {
	if info := Read(t.TempDir(), "kosong.com", time.Now()); info.Status != StatusNone {
		t.Fatalf("tanpa cert harus none, dapat %+v", info)
	}
}

func TestPrefersProductionOverStaging(t *testing.T) {
	now := time.Now()
	domain := "dual.com"
	certDir := t.TempDir()
	// tulis dua cert domain sama: staging (NotAfter lebih jauh) + produksi.
	writeCertInto(t, certDir, domain, "acme-staging-v02.api.letsencrypt.org-directory", "STAGING Fake CN", now.Add(80*24*time.Hour))
	writeCertInto(t, certDir, domain, "acme-v02.api.letsencrypt.org-directory", "R3", now.Add(40*24*time.Hour))

	info := Read(certDir, domain, now)
	if info.Issuer != "R3" {
		t.Fatalf("harus pilih produksi (R3), dapat %q", info.Issuer)
	}
}
