package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/lamun-my-id/lamund/internal/site"
	"github.com/lamun-my-id/lamund/internal/store"
)

func (s *server) registerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/sites/{domain}/routes", s.requireAuth(s.handleGetRoutes))
	mux.HandleFunc("PUT /api/v1/sites/{domain}/routes", s.requireAuth(s.handlePutRoutes))
}

// routeJSON adalah bentuk ramah-panel satu route (lebih ringkas dari config internal).
type routeJSON struct {
	PathPrefix string `json:"path_prefix"`
	Type       string `json:"type"`               // static|proxy|app
	Upstream   string `json:"upstream,omitempty"` // untuk proxy
	App        string `json:"app,omitempty"`      // untuk app: nama (domain) app yg di-mount
	Cache      bool   `json:"cache"`              // percepat: cache respons
	SPA        bool   `json:"spa"`               // static: fallback index.html (Vue/React)
}

// handleGetRoutes mengembalikan route efektif situs (dari config, atau
// sintesis default dari type bila belum ada config).
func (s *server) handleGetRoutes(w http.ResponseWriter, r *http.Request) {
	st, _, ok := s.ownedSite(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"routes": effectiveRoutes(*st)})
}

func effectiveRoutes(st store.Site) []routeJSON {
	if st.Config != "" {
		if cfg, err := site.ParseConfig(st.Config); err == nil {
			out := make([]routeJSON, 0, len(cfg.Routes))
			for _, rs := range cfg.Routes {
				out = append(out, specToJSON(rs))
			}
			return out
		}
	}
	// default: satu route dari type
	rj := routeJSON{PathPrefix: "/", Type: st.Type}
	if st.Type == "proxy" {
		rj.Upstream = st.ProxyTarget
	}
	return []routeJSON{rj}
}

func specToJSON(rs site.RouteSpec) routeJSON {
	rj := routeJSON{PathPrefix: rs.Match.PathPrefix}
	switch {
	case rs.Handler.App != nil:
		rj.Type, rj.App = "app", rs.Handler.App.Name
	case rs.Handler.Proxy != nil:
		rj.Type, rj.Upstream = "proxy", rs.Handler.Proxy.Upstream
	case rs.Handler.Static != nil:
		rj.Type = "static"
		rj.SPA = rs.Handler.Static.SPA
	}
	for _, m := range rs.Use {
		if m.Cache != nil {
			rj.Cache = true
		}
	}
	if rj.PathPrefix == "" {
		rj.PathPrefix = "/"
	}
	return rj
}

// middlewareFor menyusun default middleware per route: static → security +
// compress; proxy → security. cache ditambah bila diminta (static 60s, proxy 30s).
func middlewareFor(routeType string, cache bool) []site.MiddlewareSpec {
	use := []site.MiddlewareSpec{{Headers: &site.HeadersSpec{Security: true}}}
	if routeType == "static" {
		use = append(use, site.MiddlewareSpec{Compress: &site.CompressSpec{}})
	}
	if cache {
		ttl := 30
		if routeType == "static" {
			ttl = 60
		}
		use = append(use, site.MiddlewareSpec{Cache: &site.CacheSpec{TTLSeconds: ttl}})
	}
	return use
}

// handlePutRoutes menyetel routing multi-path. Static root selalu di-resolve ke
// folder terkelola tenant (bukan dari klien); proxy upstream wajib loopback.
func (s *server) handlePutRoutes(w http.ResponseWriter, r *http.Request) {
	st, u, ok := s.ownedSite(w, r)
	if !ok {
		return
	}
	if s.d.Sites == nil {
		writeErr(w, http.StatusServiceUnavailable, "penyimpanan berkas tidak tersedia")
		return
	}
	var req struct {
		Routes []routeJSON `json:"routes"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "body tidak valid")
		return
	}
	if len(req.Routes) == 0 {
		writeErr(w, http.StatusBadRequest, "minimal satu route")
		return
	}

	specs := make([]site.RouteSpec, 0, len(req.Routes))
	hasStatic := false
	for i, rj := range req.Routes {
		prefix := "/" + strings.Trim(strings.TrimSpace(rj.PathPrefix), "/")
		if rj.PathPrefix == "/" || rj.PathPrefix == "" {
			prefix = "/"
		}
		spec := site.RouteSpec{Match: site.MatchSpec{PathPrefix: prefix}}
		switch rj.Type {
		case "static":
			hasStatic = true
			spec.Handler.Static = &site.StaticSpec{Root: s.d.Sites.SiteRoot(st.UserID, st.Domain), SPA: rj.SPA}
		case "proxy":
			// Reverse-proxy custom (upstream sembarang) HANYA operator/superadmin.
			// Tenant: deploy app lalu mount via type 'app' (port yang KITA sediakan).
			if u.Role != "superadmin" {
				writeErr(w, http.StatusForbidden, "route "+prefix+": reverse-proxy ke upstream custom tidak diizinkan — deploy app lalu pakai type 'app'")
				return
			}
			norm, err := store.ValidateProxyTarget(rj.Upstream, false)
			if err != nil {
				writeErr(w, http.StatusBadRequest, "route "+prefix+": "+err.Error())
				return
			}
			spec.Handler.Proxy = &site.ProxySpec{Upstream: norm}
		case "app":
			// Mount app-by-name: app harus ada & boleh diakses caller (owner-scoped).
			app, err := s.d.Store.GetAppByDomain(rj.App)
			if err != nil {
				writeErr(w, http.StatusInternalServerError, "gagal membaca app")
				return
			}
			if app == nil || !s.canAccessOwner(u, app.OwnerType, app.OwnerID) {
				writeErr(w, http.StatusBadRequest, "route "+prefix+": app "+rj.App+" tidak ditemukan")
				return
			}
			spec.Handler.App = &site.AppSpec{Name: app.Domain}
		default:
			writeErr(w, http.StatusBadRequest, "route "+prefix+": type harus static|proxy|app")
			return
		}
		spec.Use = middlewareFor(rj.Type, rj.Cache)
		_ = i
		specs = append(specs, spec)
	}

	// Pastikan folder terkelola ada bila ada route static.
	if hasStatic {
		if files, _ := s.d.Sites.ListFiles(st.UserID, st.Domain); len(files) == 0 {
			_ = s.d.Sites.WriteSiteFile(st.UserID, st.Domain, "index.html", placeholderHTML(st.Domain))
		}
	}

	cfg := site.Config{Version: site.ConfigVersion, Routes: specs}
	raw, err := json.Marshal(cfg)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "gagal menyusun config")
		return
	}
	if err := s.d.Store.SetSiteConfig(st.Domain, string(raw)); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.notifyReload()
	writeJSON(w, http.StatusOK, map[string]any{"routes": req.Routes})
}
