package api

import (
	"archive/zip"
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lamun-my-id/lamund/internal/auth"
	"github.com/lamun-my-id/lamund/internal/logging"
	"github.com/lamun-my-id/lamund/internal/store"
)

func newRec() *httptest.ResponseRecorder { return httptest.NewRecorder() }

// writeLeafFile menaruh leaf cert produksi-palsu di layout certmagic.
func writeLeafFile(t *testing.T, certDir, domain string) {
	t.Helper()
	now := time.Now()
	caKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	caTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(9), Subject: pkix.Name{CommonName: "Lamund Test CA"},
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(365 * 24 * time.Hour),
		IsCA: true, KeyUsage: x509.KeyUsageCertSign, BasicConstraintsValid: true,
	}
	caDER, _ := x509.CreateCertificate(rand.Reader, caTmpl, caTmpl, &caKey.PublicKey, caKey)
	caCert, _ := x509.ParseCertificate(caDER)
	leafKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	leafTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: domain},
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(60 * 24 * time.Hour), DNSNames: []string{domain},
	}
	leafDER, _ := x509.CreateCertificate(rand.Reader, leafTmpl, caCert, &leafKey.PublicKey, caKey)
	dir := filepath.Join(certDir, "certificates", "acme-v02.api.letsencrypt.org-directory", domain)
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, domain+".crt"),
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leafDER}), 0o644)
}

// harnessDirs seperti harness tapi menyetel CertDir + LogDir agar endpoint
// sertifikat & log bisa diuji. Mengembalikan handler, store, certDir, logDir.
func harnessDirs(t *testing.T) (http.Handler, *store.Store, string, string) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	hash, _ := auth.HashPassword("rahasia123")
	st.CreateUser(store.User{Username: "admin", PasswordHash: hash, Role: "superadmin"})
	secret, _ := auth.GenerateSecret()
	certDir, logDir := t.TempDir(), t.TempDir()
	h := New(Deps{
		Store: st, Issuer: auth.NewIssuer(secret, time.Hour),
		Sites:   store.NewSiteFS(t.TempDir()),
		CertDir: certDir, LogDir: logDir,
	})
	return h, st, certDir, logDir
}

func TestChangePassword(t *testing.T) {
	h, _ := harness(t)
	tok := loginToken(t, h)

	// current salah → 401
	if c := do(t, h, "POST", "/api/v1/auth/password", tok, map[string]string{"current": "salah", "new": "passwordbaru1"}).Code; c != 401 {
		t.Fatalf("current salah harus 401, dapat %d", c)
	}
	// new terlalu pendek → 400
	if c := do(t, h, "POST", "/api/v1/auth/password", tok, map[string]string{"current": "rahasia123", "new": "pendek"}).Code; c != 400 {
		t.Fatalf("new pendek harus 400, dapat %d", c)
	}
	// valid → 200
	if c := do(t, h, "POST", "/api/v1/auth/password", tok, map[string]string{"current": "rahasia123", "new": "passwordbaru1"}).Code; c != 200 {
		t.Fatalf("ganti sandi harus 200, dapat %d", c)
	}
	// sandi lama tak berlaku, sandi baru berlaku
	if login(t, h, "admin", "rahasia123").Code != 401 {
		t.Fatal("sandi lama harus ditolak")
	}
	if login(t, h, "admin", "passwordbaru1").Code != 200 {
		t.Fatal("sandi baru harus diterima")
	}
}

func TestCertsEndpoint(t *testing.T) {
	h, st, certDir, _ := harnessDirs(t)
	tok := loginToken(t, h)
	st.CreateSite(store.Site{Domain: "situs.com", Type: "static", RootPath: "/s", Status: "active"})
	writeLeafFile(t, certDir, "situs.com")

	rec := do(t, h, "GET", "/api/v1/certs", tok, nil)
	if rec.Code != 200 {
		t.Fatalf("GET /certs mau 200, dapat %d", rec.Code)
	}
	var out struct {
		Certs []struct {
			Domain, Status string
		}
	}
	json.Unmarshal(rec.Body.Bytes(), &out)
	if len(out.Certs) != 1 || out.Certs[0].Domain != "situs.com" || out.Certs[0].Status != "valid" {
		t.Fatalf("cert list salah: %+v", out.Certs)
	}
	// per-situs
	if c := do(t, h, "GET", "/api/v1/sites/situs.com/cert", tok, nil).Code; c != 200 {
		t.Fatalf("GET site cert mau 200, dapat %d", c)
	}
}

func TestSiteLogsEndpoint(t *testing.T) {
	h, st, _, logDir := harnessDirs(t)
	tok := loginToken(t, h)
	st.CreateSite(store.Site{Domain: "log.com", Type: "static", RootPath: "/s", Status: "active"})

	// tulis 3 baris log via manager (seperti data plane)
	m, _ := logging.NewManager(logDir, 0, nil, nil)
	hh := m.Handler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("x")) }))
	for i := 0; i < 3; i++ {
		req, _ := http.NewRequest("GET", "http://log.com/p", nil)
		req.Host = "log.com"
		hh.ServeHTTP(newRec(), req)
	}
	m.Close()

	rec := do(t, h, "GET", "/api/v1/sites/log.com/logs?n=2", tok, nil)
	var out struct{ Lines []string }
	json.Unmarshal(rec.Body.Bytes(), &out)
	if len(out.Lines) != 2 {
		t.Fatalf("tail n=2 harus 2 baris, dapat %d (%v)", len(out.Lines), out.Lines)
	}
}

func zipBytes(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range files {
		w, _ := zw.Create(name)
		w.Write([]byte(content))
	}
	zw.Close()
	return buf.Bytes()
}

// deployZip mengunggah zip ke endpoint deploy sebagai multipart field "archive".
func deployZip(t *testing.T, h http.Handler, tok, domain string, files map[string]string) int {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, _ := mw.CreateFormFile("archive", "site.zip")
	fw.Write(zipBytes(t, files))
	mw.Close()
	req := httptest.NewRequest("POST", "/api/v1/sites/"+domain+"/deploy", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code
}

func TestStaticDeployFlow(t *testing.T) {
	h, _, _, _ := harnessDirs(t)
	tok := loginToken(t, h)

	// buat situs statis (root terkelola + placeholder otomatis)
	if c := do(t, h, "POST", "/api/v1/sites", tok, map[string]string{"domain": "web.com", "type": "static"}).Code; c != 201 {
		t.Fatalf("create static mau 201, dapat %d", c)
	}
	// deploy zip
	if c := deployZip(t, h, tok, "web.com", map[string]string{"index.html": "<h1>live</h1>", "style.css": "body{}"}); c != 200 {
		t.Fatalf("deploy mau 200, dapat %d", c)
	}
	// daftar berkas menampilkan hasil deploy (bukan placeholder lagi)
	rec := do(t, h, "GET", "/api/v1/sites/web.com/files", tok, nil)
	var out struct{ Files []struct{ Path string } }
	json.Unmarshal(rec.Body.Bytes(), &out)
	names := map[string]bool{}
	for _, f := range out.Files {
		names[f.Path] = true
	}
	if !names["index.html"] || !names["style.css"] {
		t.Fatalf("daftar berkas salah: %+v", out.Files)
	}
}

func TestDeployRejectsNonStatic(t *testing.T) {
	h, st, _, _ := harnessDirs(t)
	tok := loginToken(t, h)
	// Sisipkan situs proxy langsung via store (melewati validasi API) untuk menguji
	// bahwa endpoint deploy menolak situs proxy — terlepas dari asal pembuatannya.
	// (API user sudah memblokir pembuatan proxy loopback sejak hardening SSRF.)
	st.CreateSite(store.Site{Domain: "p.com", Type: "proxy", ProxyTarget: "http://127.0.0.1:9000",
		Status: "active", UserID: 1, OwnerType: "user", OwnerID: 1})
	if c := deployZip(t, h, tok, "p.com", map[string]string{"index.html": "x"}); c != 400 {
		t.Fatalf("deploy ke situs proxy harus 400, dapat %d", c)
	}
}

func TestRoutesConfig(t *testing.T) {
	h, st, _, _ := harnessDirs(t)
	tok := loginToken(t, h)
	do(t, h, "POST", "/api/v1/sites", tok, map[string]string{"domain": "hy.com", "type": "static"})

	// GET default: satu route static "/"
	rec := do(t, h, "GET", "/api/v1/sites/hy.com/routes", tok, nil)
	var g struct{ Routes []map[string]any }
	json.Unmarshal(rec.Body.Bytes(), &g)
	if len(g.Routes) != 1 || g.Routes[0]["type"] != "static" {
		t.Fatalf("default route salah: %+v", g.Routes)
	}

	// SSRF hardening: PUT hybrid dengan loopback proxy HARUS ditolak dari jalur user.
	// Loopback di routes juga merupakan SSRF — panel kontrol terlindungi loopback.
	loopback := map[string]any{"routes": []map[string]string{
		{"path_prefix": "/api", "type": "proxy", "upstream": "http://127.0.0.1:7788"},
		{"path_prefix": "/", "type": "static"},
	}}
	if c := do(t, h, "PUT", "/api/v1/sites/hy.com/routes", tok, loopback).Code; c != 400 {
		t.Fatalf("PUT routes loopback harus 400 (SSRF), dapat %d", c)
	}

	// Config belum berubah (PUT ditolak)
	site2, _ := st.GetSiteByDomain("hy.com")
	if site2.Config != "" {
		t.Fatal("config tidak boleh berubah setelah PUT ditolak")
	}

	// SSRF: upstream metadata endpoint juga ditolak
	bad := map[string]any{"routes": []map[string]string{
		{"path_prefix": "/api", "type": "proxy", "upstream": "http://169.254.169.254/"},
	}}
	if c := do(t, h, "PUT", "/api/v1/sites/hy.com/routes", tok, bad).Code; c != 400 {
		t.Fatalf("upstream SSRF harus 400, dapat %d", c)
	}

	// Route statis saja tetap bisa disimpan
	good := map[string]any{"routes": []map[string]string{
		{"path_prefix": "/", "type": "static"},
	}}
	if c := do(t, h, "PUT", "/api/v1/sites/hy.com/routes", tok, good).Code; c != 200 {
		t.Fatalf("PUT routes static mau 200, dapat %d", c)
	}
	site3, _ := st.GetSiteByDomain("hy.com")
	if site3.Config == "" {
		t.Fatal("config harus tersimpan setelah PUT valid")
	}
}
