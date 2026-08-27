package site

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func serveMW(mw Middleware, h http.Handler, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	mw(h).ServeHTTP(rec, req)
	return rec
}

func TestCompressGzipsText(t *testing.T) {
	body := "halo " + string(make([]byte, 500)) // cukup panjang
	h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte(body))
	})
	req := httptest.NewRequest("GET", "http://h/", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := serveMW(Compress(), h, req)

	if rec.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("harus gzip, header: %v", rec.Header())
	}
	gr, err := gzip.NewReader(rec.Body)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(gr)
	if string(got) != body {
		t.Fatal("hasil dekompresi tak sama dengan asli")
	}
}

func TestCompressSkipsWhenNoAcceptEncoding(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("data"))
	})
	rec := serveMW(Compress(), h, httptest.NewRequest("GET", "http://h/", nil))
	if rec.Header().Get("Content-Encoding") == "gzip" {
		t.Fatal("tanpa Accept-Encoding tak boleh gzip")
	}
	if rec.Body.String() != "data" {
		t.Fatalf("body salah: %q", rec.Body.String())
	}
}

func TestCompressSkipsBinary(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write([]byte("\x89PNG..."))
	})
	req := httptest.NewRequest("GET", "http://h/", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := serveMW(Compress(), h, req)
	if rec.Header().Get("Content-Encoding") == "gzip" {
		t.Fatal("konten biner tak boleh di-gzip")
	}
}

func TestCacheHitMiss(t *testing.T) {
	calls := 0
	h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.Write([]byte("mahal"))
	})
	mw := Cache(time.Minute)
	wrapped := mw(h)

	do := func() *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		wrapped.ServeHTTP(rec, httptest.NewRequest("GET", "http://h/x", nil))
		return rec
	}
	r1 := do()
	if r1.Header().Get("X-Lamund-Cache") == "HIT" {
		t.Fatal("permintaan pertama harus MISS")
	}
	r2 := do()
	if r2.Header().Get("X-Lamund-Cache") != "HIT" {
		t.Fatal("permintaan kedua harus HIT")
	}
	if r2.Body.String() != "mahal" {
		t.Fatalf("body cache salah: %q", r2.Body.String())
	}
	if calls != 1 {
		t.Fatalf("handler harus dipanggil sekali (cache), dapat %d", calls)
	}
}

func TestCacheOnlyGet(t *testing.T) {
	calls := 0
	h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls++; w.Write([]byte("x")) })
	wrapped := Cache(time.Minute)(h)
	for i := 0; i < 2; i++ {
		rec := httptest.NewRecorder()
		wrapped.ServeHTTP(rec, httptest.NewRequest("POST", "http://h/x", nil))
	}
	if calls != 2 {
		t.Fatalf("POST tak boleh di-cache; handler harus dipanggil 2x, dapat %d", calls)
	}
}

func TestSecurityHeaders(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("ok")) })
	rec := serveMW(SecurityHeaders(), h, httptest.NewRequest("GET", "http://h/", nil))
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("nosniff harus di-set")
	}
	if rec.Header().Get("X-Frame-Options") == "" {
		t.Fatal("X-Frame-Options harus di-set")
	}
}

// Shared-cache safety: request ter-autentikasi (Cookie/Authorization) tak boleh
// pernah di-cache — respons user A tak boleh bisa disajikan ke user lain.
func TestCacheSkipsAuthenticatedRequest(t *testing.T) {
	for _, hdr := range []struct{ k, v string }{{"Cookie", "sid=userA"}, {"Authorization", "Bearer t"}} {
		calls := 0
		h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls++; w.Write([]byte("private-data")) })
		wrapped := Cache(time.Minute)(h)
		for i := 0; i < 2; i++ {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest("GET", "http://h/me", nil)
			req.Header.Set(hdr.k, hdr.v)
			wrapped.ServeHTTP(rec, req)
			if rec.Header().Get("X-Lamund-Cache") == "HIT" {
				t.Fatalf("%s: request ter-autentikasi tak boleh HIT cache", hdr.k)
			}
		}
		if calls != 2 {
			t.Fatalf("%s: respons ter-autentikasi tak boleh di-cache; handler harus 2x, dapat %d", hdr.k, calls)
		}
	}
}

// Respons yang menetapkan Set-Cookie (sesi per-user) tak boleh disimpan.
func TestCacheSkipsSetCookieResponse(t *testing.T) {
	calls := 0
	h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.Header().Set("Set-Cookie", "sid=abc123; Path=/")
		w.Write([]byte("hi"))
	})
	wrapped := Cache(time.Minute)(h)
	for i := 0; i < 2; i++ {
		rec := httptest.NewRecorder()
		wrapped.ServeHTTP(rec, httptest.NewRequest("GET", "http://h/x", nil))
	}
	if calls != 2 {
		t.Fatalf("respons Set-Cookie tak boleh di-cache; handler harus 2x, dapat %d", calls)
	}
}

// Cache-Control: private tak boleh disimpan pada cache bersama.
func TestCacheSkipsPrivate(t *testing.T) {
	calls := 0
	h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.Header().Set("Cache-Control", "private, max-age=60")
		w.Write([]byte("hi"))
	})
	wrapped := Cache(time.Minute)(h)
	for i := 0; i < 2; i++ {
		rec := httptest.NewRecorder()
		wrapped.ServeHTTP(rec, httptest.NewRequest("GET", "http://h/x", nil))
	}
	if calls != 2 {
		t.Fatalf("respons private tak boleh di-cache; handler harus 2x, dapat %d", calls)
	}
}

func TestConfigWithMiddleware(t *testing.T) {
	// config dengan cache+compress+headers pada route static harus ter-build.
	cfg := `{"version":1,"routes":[{"match":{"path_prefix":"/"},` +
		`"handler":{"proxy":{"upstream":"http://127.0.0.1:1"}},` +
		`"use":[{"headers":{"security":true}},{"compress":{}},{"cache":{"ttl_seconds":30}}]}]}`
	c, err := ParseConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Compile(); err != nil {
		t.Fatalf("compile config bermiddleware: %v", err)
	}
}
