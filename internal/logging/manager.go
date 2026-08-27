package logging

import (
	"bufio"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Manager menulis access log per-domain ke <dir>/<domain>.log (rotasi ukuran),
// dan opsional melaporkan byte respons ke sink bandwidth. Dipakai data plane.
type Manager struct {
	dir      string
	maxBytes int64
	now      func() time.Time
	bw       BandwidthSink

	mu    sync.Mutex
	files map[string]*Rotator
}

// NewManager membuat manajer log berakar di dir. maxBytes=0 → tanpa rotasi.
// now boleh nil (time.Now); bw boleh nil.
func NewManager(dir string, maxBytes int64, now func() time.Time, bw BandwidthSink) (*Manager, error) {
	if now == nil {
		now = time.Now
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, err
	}
	return &Manager{dir: dir, maxBytes: maxBytes, now: now, bw: bw, files: map[string]*Rotator{}}, nil
}

func (m *Manager) rotatorFor(domain string) *Rotator {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r, ok := m.files[domain]; ok {
		return r
	}
	r, err := NewRotator(filepath.Join(m.dir, safeName(domain)+".log"), m.maxBytes)
	if err != nil {
		return nil // gagal buka log tak boleh menjatuhkan request
	}
	m.files[domain] = r
	return r
}

// Handler membungkus next: mencatat satu baris per request ke log domainnya.
func (m *Manager) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		cw := &countingWriter{ResponseWriter: rw, status: 200}
		next.ServeHTTP(cw, r)
		domain := hostOnly(r.Host)
		e := Entry{
			Time: m.now(), Domain: domain, Remote: remoteHost(r.RemoteAddr),
			Method: r.Method, Path: r.URL.Path, Status: cw.status, Bytes: cw.bytes,
		}
		if rot := m.rotatorFor(domain); rot != nil {
			_, _ = rot.Write([]byte(e.Format()))
		}
		if m.bw != nil {
			m.bw(domain, cw.bytes)
		}
	})
}

// Close menutup semua file log yang terbuka.
func (m *Manager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range m.files {
		r.Close()
	}
	return nil
}

// Tail membaca hingga n baris terakhir dari log domain di dir. Aman dipanggil
// proses lain (panel) yang hanya membaca berkas. Mengembalikan slice kosong
// bila belum ada log.
func Tail(dir, domain string, n int) ([]string, error) {
	path := filepath.Join(dir, safeName(domain)+".log")
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, err
	}
	defer f.Close()

	// Ring buffer sederhana untuk n baris terakhir.
	ring := make([]string, 0, n)
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		if len(ring) == n {
			ring = ring[1:]
		}
		ring = append(ring, sc.Text())
	}
	return ring, sc.Err()
}

// safeName mencegah nama domain dipakai untuk keluar direktori log.
func safeName(domain string) string {
	d := strings.ToLower(strings.TrimSpace(domain))
	d = strings.ReplaceAll(d, "/", "_")
	d = strings.ReplaceAll(d, "..", "_")
	if d == "" {
		return "_unknown"
	}
	return d
}

func hostOnly(host string) string {
	if i := strings.IndexByte(host, ':'); i >= 0 {
		return host[:i]
	}
	return host
}
