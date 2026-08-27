package logging

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestManagerPerDomainLogAndTail(t *testing.T) {
	dir := t.TempDir()
	fixed := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	m, err := NewManager(dir, 0, func() time.Time { return fixed }, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()

	h := m.Handler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("ok"))
	}))

	// dua request ke domain berbeda → dua berkas log terpisah
	for _, host := range []string{"a.com", "a.com", "b.com"} {
		req := httptest.NewRequest("GET", "http://"+host+"/p", nil)
		req.Host = host
		req.RemoteAddr = "1.2.3.4:9"
		h.ServeHTTP(httptest.NewRecorder(), req)
	}

	aLines, _ := Tail(dir, "a.com", 10)
	if len(aLines) != 2 {
		t.Fatalf("a.com harus 2 baris, dapat %d", len(aLines))
	}
	bLines, _ := Tail(dir, "b.com", 10)
	if len(bLines) != 1 {
		t.Fatalf("b.com harus 1 baris, dapat %d", len(bLines))
	}
	// tail membatasi jumlah baris
	if got, _ := Tail(dir, "a.com", 1); len(got) != 1 {
		t.Fatalf("tail n=1 harus 1 baris, dapat %d", len(got))
	}
}

func TestTailMissingIsEmpty(t *testing.T) {
	lines, err := Tail(t.TempDir(), "belum-ada.com", 5)
	if err != nil || len(lines) != 0 {
		t.Fatalf("domain tanpa log harus kosong tanpa error, dapat %v %v", lines, err)
	}
}

func TestSafeNameBlocksTraversal(t *testing.T) {
	if n := safeName("../../etc/passwd"); n == "../../etc/passwd" {
		t.Fatalf("safeName harus menetralkan traversal, dapat %q", n)
	}
}
