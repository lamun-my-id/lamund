// Package logging menulis access log per situs dengan rotasi ukuran.
package logging

import (
	"fmt"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"
)

// Rotator adalah writer append yang berotasi (file -> file.1) saat ukuran
// melewati maxBytes. Aman untuk pemakaian bersamaan.
type Rotator struct {
	mu       sync.Mutex
	path     string
	maxBytes int64
	f        *os.File
	size     int64
}

// NewRotator membuka (atau membuat) file log di path dengan ambang rotasi maxBytes.
func NewRotator(path string, maxBytes int64) (*Rotator, error) {
	r := &Rotator{path: path, maxBytes: maxBytes}
	if err := r.open(); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *Rotator) open() error {
	f, err := os.OpenFile(r.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o640)
	if err != nil {
		return err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return err
	}
	r.f, r.size = f, info.Size()
	return nil
}

func (r *Rotator) rotate() error {
	if err := r.f.Close(); err != nil {
		return err
	}
	if err := os.Rename(r.path, r.path+".1"); err != nil {
		return err
	}
	r.size = 0
	return r.open()
}

// Write menambahkan p; merotasi lebih dulu bila akan melewati ambang.
func (r *Rotator) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.maxBytes > 0 && r.size+int64(len(p)) > r.maxBytes {
		if err := r.rotate(); err != nil {
			return 0, err
		}
	}
	n, err := r.f.Write(p)
	r.size += int64(n)
	return n, err
}

// Close menutup file yang sedang aktif.
func (r *Rotator) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.f.Close()
}

// Entry adalah satu baris access log.
type Entry struct {
	Time   time.Time
	Domain string
	Remote string
	Method string
	Path   string
	Status int
	Bytes  int64
}

// Format menghasilkan baris log gaya-gabungan ringkas (diakhiri newline).
func (e Entry) Format() string {
	return fmt.Sprintf("%s\t%s\t%s\t%s\t%s\t%d\t%d\n",
		e.Time.UTC().Format(time.RFC3339), e.Domain, e.Remote,
		e.Method, e.Path, e.Status, e.Bytes)
}

// countingWriter membungkus ResponseWriter untuk merekam status & jumlah byte.
type countingWriter struct {
	http.ResponseWriter
	status int
	bytes  int64
}

func (c *countingWriter) WriteHeader(code int) {
	c.status = code
	c.ResponseWriter.WriteHeader(code)
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.ResponseWriter.Write(p)
	c.bytes += int64(n)
	return n, err
}

// BandwidthSink menerima jumlah byte respons untuk agregasi kuota.
// domainOwner memetakan host -> userID; nil melewati agregasi.
type BandwidthSink func(domain string, bytes int64)

// Middleware membungkus next: menulis satu Entry ke sink log dan (bila
// disetel) melaporkan byte ke bw. now boleh nil (pakai time.Now).
func Middleware(next http.Handler, w interface{ Write([]byte) (int, error) }, bw BandwidthSink, now func() time.Time) http.Handler {
	if now == nil {
		now = time.Now
	}
	return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		cw := &countingWriter{ResponseWriter: rw, status: 200}
		next.ServeHTTP(cw, r)
		e := Entry{
			Time: now(), Domain: r.Host, Remote: remoteHost(r.RemoteAddr),
			Method: r.Method, Path: r.URL.Path, Status: cw.status, Bytes: cw.bytes,
		}
		_, _ = w.Write([]byte(e.Format()))
		if bw != nil {
			bw(r.Host, cw.bytes)
		}
	})
}

func remoteHost(addr string) string {
	for i := len(addr) - 1; i >= 0; i-- {
		if addr[i] == ':' {
			return addr[:i]
		}
	}
	return addr
}

// Month mengembalikan penanda bulan "YYYYMM" dari t.
func Month(t time.Time) string {
	return strconv.Itoa(t.Year()) + fmt.Sprintf("%02d", int(t.Month()))
}
