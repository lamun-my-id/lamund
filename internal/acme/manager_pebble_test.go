package acme

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caddyserver/certmagic"
)

// pebbleBinary mencari binary pebble (dipasang via go install ke /tmp/gobin
// atau di PATH). Return "" bila tidak ada → test di-skip.
func pebbleBinary() string {
	for _, p := range []string{"/tmp/gobin/pebble", "pebble"} {
		if lp, err := exec.LookPath(p); err == nil {
			return lp
		}
	}
	return ""
}

func pebbleModuleDir() string {
	out, err := exec.Command("go", "env", "GOMODCACHE").Output()
	if err != nil {
		return ""
	}
	base := filepath.Join(strings.TrimSpace(string(out)), "github.com", "letsencrypt")
	matches, _ := filepath.Glob(filepath.Join(base, "pebble", "v2@*"))
	if len(matches) == 0 {
		return ""
	}
	return matches[0]
}

func freePort(t *testing.T) int {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// TestIssueWithPebble menerbitkan sertifikat NYATA lewat server ACME Pebble
// (VA always-valid, jadi tak perlu koordinasi port challenge). Membuktikan
// wiring certmagic Manager: terbit → simpan → sajikan via TLSConfig.
func TestIssueWithPebble(t *testing.T) {
	bin := pebbleBinary()
	mod := pebbleModuleDir()
	if bin == "" || mod == "" {
		t.Skip("pebble tidak tersedia — lewati test issuance (jalan di CI)")
	}
	cfgFile := filepath.Join(mod, "test", "config", "pebble-config.json")
	rootPEM := filepath.Join(mod, "test", "certs", "pebble.minica.pem")
	if _, err := os.Stat(cfgFile); err != nil {
		t.Skipf("config pebble tak ditemukan: %v", err)
	}

	// jalankan pebble (cwd = module dir agar path cert relatif resolve)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "-config", cfgFile)
	cmd.Dir = mod
	cmd.Env = append(os.Environ(), "PEBBLE_VA_ALWAYS_VALID=1")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start pebble: %v", err)
	}
	defer func() { cancel(); cmd.Wait() }()

	// tunggu ACME dir :14000 siap
	dirURL := "https://localhost:14000/dir"
	ready := false
	for i := 0; i < 100; i++ {
		if c, err := net.DialTimeout("tcp", "localhost:14000", 200*time.Millisecond); err == nil {
			c.Close()
			ready = true
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !ready {
		t.Fatal("pebble tidak siap di :14000")
	}

	// percayai root CA Pebble
	pem, err := os.ReadFile(rootPEM)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pem) {
		t.Fatal("gagal muat root Pebble")
	}

	// arahkan solver HTTP-01 certmagic ke port bebas (VA tak akan connect,
	// tapi solver tetap bind); nonaktifkan TLS-ALPN agar tak sentuh :443.
	certmagic.HTTPPort = freePort(t)
	certmagic.DefaultACME.DisableTLSALPNChallenge = true

	m := NewManager(Config{
		Email:        "test@lamund.local",
		StorageDir:   t.TempDir(),
		CA:           dirURL,
		TrustedRoots: pool,
	})

	mctx, mcancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer mcancel()
	domain := "situs.lamund.test"
	if err := m.Manage(mctx, []string{domain}); err != nil {
		t.Fatalf("Manage/terbit cert gagal: %v", err)
	}

	// cert harus tersaji lewat TLSConfig untuk SNI domain itu
	tc := m.TLSConfig()
	cert, err := tc.GetCertificate(&tls.ClientHelloInfo{ServerName: domain})
	if err != nil || cert == nil {
		t.Fatalf("cert tidak tersaji untuk %s: %v", domain, err)
	}
	leaf, _ := x509.ParseCertificate(cert.Certificate[0])
	if leaf == nil || !contains(leaf.DNSNames, domain) {
		t.Fatalf("cert tidak memuat domain %s (SAN: %v)", domain, dnsNames(leaf))
	}
	t.Logf("cert terbit oleh %q, berlaku s/d %s", leaf.Issuer.CommonName, leaf.NotAfter.Format(time.RFC3339))
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}
func dnsNames(c *x509.Certificate) []string {
	if c == nil {
		return nil
	}
	return c.DNSNames
}
