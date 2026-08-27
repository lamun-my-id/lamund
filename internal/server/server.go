// Package server merangkai store -> vhost -> handler menjadi data plane HTTP.
package server

import (
	"fmt"
	"log"
	"net/http"

	"github.com/lamun-my-id/lamund/internal/site"
	"github.com/lamun-my-id/lamund/internal/store"
	"github.com/lamun-my-id/lamund/internal/vhost"
)

// BuildTable memuat semua site aktif dari SQLite ke tabel route in-memory.
// Site yang konfigurasinya rusak di-skip dengan warning — satu tenant rusak
// tidak boleh menjatuhkan tenant lain (PRD §4).
// Alias domain didaftarkan ke handler yang sama agar ACME bisa menerbitkan
// sertifikat untuk semua domain (Domains() dibaca oleh manajer ACME).
func BuildTable(st *store.Store) (*vhost.Table, error) {
	sites, err := st.ListSites()
	if err != nil {
		return nil, err
	}
	// Muat semua alias domain sekaligus (satu query). Bila gagal, lanjut dengan
	// peta kosong — alias tidak kritis untuk melayani domain utama.
	aliases, err := st.AllSiteDomains()
	if err != nil {
		log.Printf("WARN: gagal memuat alias domain: %v", err)
		aliases = map[int64][]string{}
	}
	// Resolver app-by-name: nama app → http://127.0.0.1:<port saat ini>. Dibangun
	// ulang tiap reload, jadi mount route otomatis ikut port baru pasca blue-green.
	resolver := appResolver(st)
	table := vhost.NewTable()
	for _, s := range sites {
		if s.Status != "active" {
			continue
		}
		h, err := site.BuildWith(s, resolver)
		if err != nil {
			log.Printf("WARN: site %s di-skip: %v", s.Domain, err)
			continue
		}
		table.Upsert(vhost.Route{Domain: s.Domain, Handler: h})
		for _, d := range aliases[s.ID] {
			table.Upsert(vhost.Route{Domain: d, Handler: h})
		}
	}
	return table, nil
}

// appResolver membangun peta nama-app → upstream loopback dari store.
func appResolver(st *store.Store) site.AppResolver {
	apps, err := st.ListApps()
	if err != nil {
		return func(string) (string, bool) { return "", false }
	}
	ports := make(map[string]int, len(apps))
	for _, a := range apps {
		ports[a.Domain] = a.Port
	}
	return func(name string) (string, bool) {
		p, ok := ports[name]
		if !ok || p == 0 {
			return "", false
		}
		return fmt.Sprintf("http://127.0.0.1:%d", p), true
	}
}

// serve merutekan satu request ke handler site berdasarkan Host header.
func serve(table *vhost.Table, w http.ResponseWriter, r *http.Request) {
	route := table.Get(r.Host)
	if route == nil {
		WriteNotFound(w)
		return
	}
	route.Handler.ServeHTTP(w, r)
}

// WriteNotFound menulis halaman 404 default untuk host/vhost tak dikenal.
// Dipakai vhost :443 dan fallback :80 (agar domain typo dapat halaman sopan,
// bukan redirect-ke-HTTPS yang lalu gagal di TLS karena tak ada cert).
func WriteNotFound(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusNotFound)
	w.Write([]byte("<!doctype html><title>404</title><h1>404</h1><p>Situs tidak ditemukan di server ini.</p><hr><small>lamund</small>"))
}

func New(table *vhost.Table) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { serve(table, w, r) })
}
