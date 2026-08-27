package api

import (
	"net/http"
	"strconv"

	"github.com/lamun-my-id/lamund/internal/certinfo"
	"github.com/lamun-my-id/lamund/internal/logging"
	"github.com/lamun-my-id/lamund/internal/store"
)

func (s *server) registerCerts(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/certs", s.requireAuth(s.handleListCerts))
	mux.HandleFunc("GET /api/v1/sites/{domain}/cert", s.requireAuth(s.handleSiteCert))
	mux.HandleFunc("GET /api/v1/sites/{domain}/logs", s.requireAuth(s.handleSiteLogs))
}

// callerDomains mengembalikan domain yang boleh dilihat caller (admin: semua).
func (s *server) callerDomains(u *authUser) ([]string, error) {
	var sites []store.Site
	var err error
	if u.Role == "superadmin" {
		sites, err = s.d.Store.ListSites()
	} else {
		sites, err = s.d.Store.ListSitesVisibleTo(u.ID)
	}
	if err != nil {
		return nil, err
	}
	domains := make([]string, 0, len(sites))
	for _, st := range sites {
		domains = append(domains, st.Domain)
	}
	return domains, nil
}

func (s *server) handleListCerts(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r.Context())
	domains, err := s.callerDomains(u)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "gagal membaca situs")
		return
	}
	certs := certinfo.List(s.d.CertDir, domains, s.d.Now())
	writeJSON(w, http.StatusOK, map[string]any{"certs": certs})
}

func (s *server) handleSiteCert(w http.ResponseWriter, r *http.Request) {
	site, _, ok := s.ownedSite(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, certinfo.Read(s.d.CertDir, site.Domain, s.d.Now()))
}

func (s *server) handleSiteLogs(w http.ResponseWriter, r *http.Request) {
	site, _, ok := s.ownedSite(w, r)
	if !ok {
		return
	}
	n := 100
	if q := r.URL.Query().Get("n"); q != "" {
		if v, err := strconv.Atoi(q); err == nil && v > 0 && v <= 1000 {
			n = v
		}
	}
	lines, err := logging.Tail(s.d.LogDir, site.Domain, n)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "gagal membaca log")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"lines": lines})
}
