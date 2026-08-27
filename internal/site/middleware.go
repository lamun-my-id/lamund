package site

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// SecurityHeaders menambah header keamanan default yang aman untuk situs.
func SecurityHeaders() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("X-Frame-Options", "SAMEORIGIN")
			h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
			next.ServeHTTP(w, r)
		})
	}
}

// compressibleType menandai tipe konten yang layak di-gzip.
func compressibleType(ct string) bool {
	ct = strings.ToLower(ct)
	for _, p := range []string{"text/", "application/json", "application/javascript",
		"application/xml", "image/svg+xml", "application/wasm"} {
		if strings.HasPrefix(ct, p) {
			return true
		}
	}
	return false
}

// Compress mengompres respons dengan gzip bila klien mendukung & tipe konten
// layak. Konten yang sudah terkompresi (gambar/video/zip) dilewati.
func Compress() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
				next.ServeHTTP(w, r)
				return
			}
			gw := &gzipWriter{ResponseWriter: w}
			defer gw.Close()
			next.ServeHTTP(gw, r)
		})
	}
}

type gzipWriter struct {
	http.ResponseWriter
	gz      *gzip.Writer
	started bool
	pass    bool // true = teruskan tanpa kompresi
}

func (g *gzipWriter) WriteHeader(code int) {
	if !g.started {
		g.started = true
		ct := g.Header().Get("Content-Type")
		if code != http.StatusOK || !compressibleType(ct) {
			g.pass = true
		} else {
			g.Header().Set("Content-Encoding", "gzip")
			g.Header().Del("Content-Length") // panjang berubah setelah kompresi
			g.Header().Add("Vary", "Accept-Encoding")
			g.gz = gzip.NewWriter(g.ResponseWriter)
		}
	}
	g.ResponseWriter.WriteHeader(code)
}

func (g *gzipWriter) Write(p []byte) (int, error) {
	if !g.started {
		// tebak content-type bila handler langsung Write tanpa WriteHeader
		if g.Header().Get("Content-Type") == "" {
			g.Header().Set("Content-Type", http.DetectContentType(p))
		}
		g.WriteHeader(http.StatusOK)
	}
	if g.pass {
		return g.ResponseWriter.Write(p)
	}
	return g.gz.Write(p)
}

func (g *gzipWriter) Close() {
	if g.gz != nil {
		g.gz.Close()
	}
}

// ---- cache respons in-memory (GET 200, TTL) ----

type cacheEntry struct {
	body    []byte
	header  http.Header
	expires time.Time
}

type respCache struct {
	mu      sync.Mutex
	entries map[string]cacheEntry
	ttl     time.Duration
	max     int
	now     func() time.Time
}

// Cache membungkus handler dengan cache respons in-memory sederhana: hanya GET
// dengan status 200 yang di-cache selama ttl. Header X-Lamund-Cache = HIT|MISS.
// Batasi jumlah entri agar tak tumbuh tanpa batas.
func Cache(ttl time.Duration) Middleware {
	c := &respCache{entries: map[string]cacheEntry{}, ttl: ttl, max: 1024, now: time.Now}
	return c.wrap
}

func (c *respCache) wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			next.ServeHTTP(w, r)
			return
		}
		// Shared-cache safety (RFC 7234 §3): request ter-autentikasi bisa
		// menghasilkan respons user-specific. Jangan sajikan dari cache MAUPUN
		// simpan — cegah respons user A bocor ke user B pada cache bersama.
		if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
			next.ServeHTTP(w, r)
			return
		}
		// Kunci menyertakan dimensi encoding: klien gzip & non-gzip disimpan
		// terpisah, sehingga replay tak mengirim gzip ke klien yang tak dukung.
		enc := "id"
		if strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			enc = "gz"
		}
		key := enc + " " + r.Host + " " + r.URL.RequestURI()
		if e, ok := c.get(key); ok {
			copyHeader(w.Header(), e.header)
			w.Header().Set("X-Lamund-Cache", "HIT")
			w.WriteHeader(http.StatusOK)
			w.Write(e.body)
			return
		}
		rec := &cacheRecorder{ResponseWriter: w, buf: &bytes.Buffer{}, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		if rec.status == http.StatusOK && !rec.noStore {
			c.set(key, cacheEntry{
				body:    rec.buf.Bytes(),
				header:  cloneHeader(rec.Header()),
				expires: c.now().Add(c.ttl),
			})
		}
	})
}

func (c *respCache) get(key string) (cacheEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	if !ok || c.now().After(e.expires) {
		if ok {
			delete(c.entries, key)
		}
		return cacheEntry{}, false
	}
	return e, true
}

func (c *respCache) set(key string, e cacheEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.entries) >= c.max {
		if _, exists := c.entries[key]; !exists {
			return // penuh: jangan tumbuh, lewati caching entri baru
		}
	}
	c.entries[key] = e
}

type cacheRecorder struct {
	http.ResponseWriter
	buf     *bytes.Buffer
	status  int
	wrote   bool
	noStore bool
}

func (r *cacheRecorder) WriteHeader(code int) {
	r.status = code
	// Jangan simpan respons user-specific: Cache-Control no-store/private, atau
	// respons yang menetapkan Set-Cookie (umumnya sesi per-user). Tanpa ini,
	// cache bersama bisa menyajikan sesi user A ke user B.
	cc := strings.ToLower(r.Header().Get("Cache-Control"))
	if strings.Contains(cc, "no-store") || strings.Contains(cc, "private") ||
		len(r.Header().Values("Set-Cookie")) > 0 {
		r.noStore = true
	}
	r.wrote = true
	r.ResponseWriter.WriteHeader(code)
}

func (r *cacheRecorder) Write(p []byte) (int, error) {
	if !r.wrote {
		r.WriteHeader(http.StatusOK)
	}
	r.buf.Write(p)
	return r.ResponseWriter.Write(p)
}

func copyHeader(dst, src http.Header) {
	for k, vs := range src {
		for _, v := range vs {
			dst.Add(k, v)
		}
	}
}

func cloneHeader(src http.Header) http.Header {
	dst := make(http.Header, len(src))
	copyHeader(dst, src)
	return dst
}

var _ io.Writer = (*gzipWriter)(nil)
