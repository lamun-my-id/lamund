package proxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProxyForwards(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Forwarded-For") == "" {
			t.Error("X-Forwarded-For tidak diteruskan")
		}
		w.Header().Set("X-Upstream", "ya")
		io.WriteString(w, "halo dari upstream "+r.URL.Path)
	}))
	defer upstream.Close()

	h, err := New(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "http://situs.test/api/x", nil)
	req.RemoteAddr = "203.0.113.9:5555"
	h.ServeHTTP(rec, req)

	if rec.Code != 200 || rec.Body.String() != "halo dari upstream /api/x" {
		t.Fatalf("proxy => %d %q", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("X-Upstream") != "ya" {
		t.Error("header upstream tidak diteruskan")
	}
}

// TestProxyXFFOverwrite memastikan Director menimpa (bukan meneruskan) XFF
// buatan klien. Upstream harus menerima IP RemoteAddr nyata, bukan 6.6.6.6.
func TestProxyXFFOverwrite(t *testing.T) {
	var receivedXFF string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedXFF = r.Header.Get("X-Forwarded-For")
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	h, err := New(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", "http://situs.test/", nil)
	req.RemoteAddr = "203.0.113.9:5555"
	// Klien menyisipkan XFF palsu — harus diabaikan.
	req.Header.Set("X-Forwarded-For", "6.6.6.6")

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if strings.Contains(receivedXFF, "6.6.6.6") {
		t.Errorf("upstream menerima XFF palsu dari klien: %q", receivedXFF)
	}
	if !strings.Contains(receivedXFF, "203.0.113.9") {
		t.Errorf("upstream harus menerima IP RemoteAddr nyata, dapat: %q", receivedXFF)
	}
}

// TestProxyXRealIPOverwrite memastikan X-Real-IP buatan klien tidak diteruskan.
func TestProxyXRealIPOverwrite(t *testing.T) {
	var receivedXRealIP string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedXRealIP = r.Header.Get("X-Real-IP")
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	h, err := New(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", "http://situs.test/", nil)
	req.RemoteAddr = "10.0.0.1:1234"
	req.Header.Set("X-Real-IP", "evil.attacker.ip")

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if strings.Contains(receivedXRealIP, "evil.attacker.ip") {
		t.Errorf("upstream menerima X-Real-IP palsu dari klien: %q", receivedXRealIP)
	}
	if receivedXRealIP != "10.0.0.1" {
		t.Errorf("upstream harus menerima X-Real-IP = IP RemoteAddr, dapat: %q", receivedXRealIP)
	}
}

func TestProxyUpstreamDown502(t *testing.T) {
	// target ke port yang pasti tertutup
	h, err := New("http://127.0.0.1:1") // port 1 tidak dilayani
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "http://situs.test/", nil))
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("upstream mati harus 502, dapat %d", rec.Code)
	}
	if rec.Body.Len() == 0 {
		t.Error("502 harus punya body lamund, bukan kosong")
	}
}

// TestDialGuard memastikan koneksi ke IP internal ditolak pasca-resolusi
// (proteksi DNS rebinding untuk target publik user).
func TestDialGuard(t *testing.T) {
	internal := []string{"127.0.0.1:80", "10.0.0.5:3000", "192.168.1.1:8080", "169.254.169.254:80", "[::1]:443", "[fc00::1]:80"}
	for _, a := range internal {
		if err := dialGuard("tcp", a, nil); err == nil {
			t.Errorf("dialGuard(%q) = nil, mau error (IP internal)", a)
		}
	}
	public := []string{"93.184.216.34:80", "8.8.8.8:53", "[2606:2800:220:1:248:1893:25c8:1946]:443"}
	for _, a := range public {
		if err := dialGuard("tcp", a, nil); err != nil {
			t.Errorf("dialGuard(%q) = %v, mau nil (IP publik)", a, err)
		}
	}
}

// TestHostIsInternalLiteral: app-proxy loopback → guard TIDAK dipasang (izinkan);
// target publik → guard dipasang.
func TestHostIsInternalLiteral(t *testing.T) {
	for _, h := range []string{"localhost", "127.0.0.1", "10.0.0.1", "192.168.0.1", "::1"} {
		if !hostIsInternalLiteral(h) {
			t.Errorf("hostIsInternalLiteral(%q) = false, mau true", h)
		}
	}
	for _, h := range []string{"example.com", "93.184.216.34", "api.publik.io"} {
		if hostIsInternalLiteral(h) {
			t.Errorf("hostIsInternalLiteral(%q) = true, mau false", h)
		}
	}
}
