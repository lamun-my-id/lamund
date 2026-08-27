// Package vhost memetakan Host header ke handler situs. Tabel ini adalah
// jantung data plane: SQLite dimuat ke sini saat boot, dimutasi in-place saat
// konfigurasi berubah (hot reload) — TANPA restart proses.
package vhost

import (
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
)

type Route struct {
	Domain  string
	Handler http.Handler
}

type Table struct {
	mu     sync.RWMutex
	routes map[string]*Route
}

func NewTable() *Table { return &Table{routes: map[string]*Route{}} }

// normalize menurunkan case dan membuang :port dari Host header.
func normalize(host string) string {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	return strings.ToLower(strings.TrimSuffix(host, "."))
}

func (t *Table) Get(host string) *Route {
	if host == "" {
		return nil
	}
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.routes[normalize(host)]
}

func (t *Table) Upsert(r Route) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.routes[normalize(r.Domain)] = &r
}

func (t *Table) Remove(domain string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.routes, normalize(domain))
}

func (t *Table) Len() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return len(t.routes)
}

// Domains mengembalikan semua domain aktif (tersortir) — dipakai untuk memberi
// tahu manajer ACME domain mana yang perlu sertifikat.
func (t *Table) Domains() []string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	out := make([]string, 0, len(t.routes))
	for d := range t.routes {
		out = append(out, d)
	}
	sort.Strings(out)
	return out
}
