package api

import (
	"net/http"

	"github.com/lamun-my-id/lamund/internal/serverstat"
)

// handleServerStats mengembalikan pemakaian CPU/RAM/swap/disk server.
func (s *server) handleServerStats(w http.ResponseWriter, _ *http.Request) {
	path := s.d.DataDir
	if path == "" {
		path = "/"
	}
	st, err := serverstat.Collect(path)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "gagal membaca statistik server")
		return
	}
	writeJSON(w, http.StatusOK, st)
}
