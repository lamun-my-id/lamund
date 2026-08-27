package vhost

import (
	"net/http"
	"testing"
)

var dummy = http.NotFoundHandler()

func TestGetNormalizesHost(t *testing.T) {
	tb := NewTable()
	tb.Upsert(Route{Domain: "example.com", Handler: dummy})
	for _, h := range []string{"example.com", "EXAMPLE.com", "Example.COM:8080", "example.com:443"} {
		if tb.Get(h) == nil {
			t.Errorf("Get(%q) = nil, mau route", h)
		}
	}
	if tb.Get("lain.com") != nil {
		t.Error("host tak terdaftar harus nil")
	}
	if tb.Get("") != nil {
		t.Error("host kosong harus nil")
	}
}

func TestUpsertReplacesAndRemove(t *testing.T) {
	tb := NewTable()
	tb.Upsert(Route{Domain: "a.com", Handler: dummy})
	tb.Upsert(Route{Domain: "a.com", Handler: dummy}) // replace, bukan duplikat
	if tb.Len() != 1 {
		t.Fatalf("Len=%d, mau 1", tb.Len())
	}
	tb.Remove("a.com")
	if tb.Get("a.com") != nil || tb.Len() != 0 {
		t.Fatal("Remove tidak bekerja")
	}
}

func TestDomains(t *testing.T) {
	tb := NewTable()
	tb.Upsert(Route{Domain: "b.com", Handler: dummy})
	tb.Upsert(Route{Domain: "a.com", Handler: dummy})
	got := tb.Domains()
	if len(got) != 2 {
		t.Fatalf("Domains n=%d, mau 2", len(got))
	}
	// harus tersortir agar deterministik
	if got[0] != "a.com" || got[1] != "b.com" {
		t.Fatalf("Domains tidak tersortir: %v", got)
	}
}
