package api

import (
	"net/http"

	"github.com/lamun-my-id/lamund/internal/analytics"
)

func (s *server) registerAnalytics(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/analytics/overview", s.requireAuth(s.handleAnalyticsOverview))
	mux.HandleFunc("GET /api/v1/sites/{domain}/analytics", s.requireAuth(s.handleSiteAnalytics))
}

// handleAnalyticsOverview meringkas trafik & error 24 jam. Non-superadmin
// dibatasi ke domain yang boleh ia lihat (owner-scoped).
func (s *server) handleAnalyticsOverview(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r.Context())
	var only map[string]bool
	if u.Role != "superadmin" {
		sites, err := s.d.Store.ListSitesVisibleTo(u.ID)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "gagal membaca situs")
			return
		}
		only = make(map[string]bool, len(sites))
		for _, st := range sites {
			only[st.Domain] = true
		}
	}
	ov := analytics.ComputeOverview(s.d.LogDir, s.d.Now(), only)
	writeJSON(w, http.StatusOK, ov)
}

// handleSiteAnalytics: laporan per-domain (sparkline 24 jam + error). Owner-scoped.
func (s *server) handleSiteAnalytics(w http.ResponseWriter, r *http.Request) {
	site, _, ok := s.ownedSite(w, r)
	if !ok {
		return
	}
	rep := analytics.ComputeDomain(s.d.LogDir, site.Domain, s.d.Now())
	writeJSON(w, http.StatusOK, rep)
}
