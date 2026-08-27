package server

import (
	"net/http"
	"sync/atomic"

	"github.com/lamun-my-id/lamund/internal/store"
	"github.com/lamun-my-id/lamund/internal/vhost"
)

// Reloadable memegang tabel route yang bisa di-swap secara atomik saat
// konfigurasi berubah — inti hot reload: perubahan site berlaku TANPA restart
// dan tanpa menjatuhkan request yang sedang berjalan (PRD §4). Build yang gagal
// tidak pernah meng-swap: tabel lama tetap melayani.
type Reloadable struct {
	st  *store.Store
	tbl atomic.Pointer[vhost.Table]
}

func NewReloadable(st *store.Store) (*Reloadable, error) {
	r := &Reloadable{st: st}
	if err := r.Reload(); err != nil {
		return nil, err
	}
	return r, nil
}

// Reload membangun tabel baru dari store lalu meng-swap atomik. Gagal build →
// tabel lama dipertahankan, error dikembalikan (tidak menjatuhkan server).
func (r *Reloadable) Reload() error {
	t, err := BuildTable(r.st)
	if err != nil {
		return err
	}
	r.tbl.Store(t)
	return nil
}

// Handler membaca pointer tabel terkini per request (lock-free).
func (r *Reloadable) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		serve(r.tbl.Load(), w, req)
	})
}

// Len jumlah route aktif saat ini (untuk log).
func (r *Reloadable) Len() int { return r.tbl.Load().Len() }

// ActiveDomains mengembalikan domain semua site aktif saat ini (untuk ACME).
func (r *Reloadable) ActiveDomains() []string { return r.tbl.Load().Domains() }

// HasHost melaporkan apakah host (boleh ber-port) cocok route aktif. Dipakai
// fallback :80 untuk membedakan "redirect ke HTTPS" (host dikenal) vs "404
// sopan" (host tak dikenal).
func (r *Reloadable) HasHost(host string) bool { return r.tbl.Load().Get(host) != nil }
