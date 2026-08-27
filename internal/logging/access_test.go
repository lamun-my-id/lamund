package logging

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRotatorRotates(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "access.log")
	r, err := NewRotator(path, 20) // ambang kecil
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	r.Write([]byte("0123456789"))       // 10 byte
	r.Write([]byte("abcdefghijklmnop")) // 16 byte → total 26 > 20, rotasi dulu
	// file .1 harus ada berisi tulisan pertama
	rotated, err := os.ReadFile(path + ".1")
	if err != nil || string(rotated) != "0123456789" {
		t.Fatalf("file terotasi salah: %q %v", rotated, err)
	}
	cur, _ := os.ReadFile(path)
	if string(cur) != "abcdefghijklmnop" {
		t.Fatalf("file aktif salah: %q", cur)
	}
}

func TestMiddlewareLogsAndCountsBytes(t *testing.T) {
	dir := t.TempDir()
	r, _ := NewRotator(filepath.Join(dir, "a.log"), 0) // 0 = tanpa rotasi
	defer r.Close()

	var gotDomain string
	var gotBytes int64
	bw := func(domain string, b int64) { gotDomain, gotBytes = domain, b }

	fixed := time.Date(2026, 8, 21, 10, 0, 0, 0, time.UTC)
	h := Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(201)
		w.Write([]byte("hello-body")) // 10 byte
	}), r, bw, func() time.Time { return fixed })

	req := httptest.NewRequest("GET", "http://situs.com/path", nil)
	req.RemoteAddr = "203.0.113.9:5555"
	h.ServeHTTP(httptest.NewRecorder(), req)

	line, _ := os.ReadFile(filepath.Join(dir, "a.log"))
	s := string(line)
	for _, want := range []string{"situs.com", "203.0.113.9", "GET", "/path", "201", "10"} {
		if !strings.Contains(s, want) {
			t.Fatalf("baris log tak memuat %q: %s", want, s)
		}
	}
	if gotDomain != "situs.com" || gotBytes != 10 {
		t.Fatalf("sink bandwidth salah: %q %d", gotDomain, gotBytes)
	}
}

func TestMonth(t *testing.T) {
	if m := Month(time.Date(2026, 8, 21, 0, 0, 0, 0, time.UTC)); m != "202608" {
		t.Fatalf("Month salah: %s", m)
	}
}
