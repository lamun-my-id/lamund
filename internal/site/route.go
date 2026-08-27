// Package site memodelkan sebuah situs sebagai daftar route terurut
// (first-match-wins), tiap route memetakan sebuah matcher ke handler yang
// dibungkus rantai middleware — mengikuti model Caddy. Ini pondasi untuk
// routing per-path (mis. "/api" → proxy, sisanya → static), cache, dan
// perilaku per-path lain. Lihat docs untuk desain lengkap.
package site

import (
	"net/http"
	"strings"
)

// Match menentukan apakah sebuah route berlaku untuk suatu request.
// S1: hanya berdasarkan prefix path (per-segmen). PathPrefix kosong atau "/"
// = cocok untuk semua path (route default).
type Match struct {
	PathPrefix string
}

// Matches benar bila request cocok. Prefix dicocokkan per-segmen: "/api"
// cocok untuk "/api" dan "/api/…" tetapi TIDAK untuk "/apix".
func (m Match) Matches(r *http.Request) bool {
	p := m.PathPrefix
	if p == "" || p == "/" {
		return true
	}
	p = "/" + strings.Trim(p, "/")
	path := r.URL.Path
	return path == p || strings.HasPrefix(path, p+"/")
}

// Middleware membungkus handler (mis. cache, kompresi, header, rate-limit).
type Middleware func(http.Handler) http.Handler

// Route memetakan matcher ke handler + rantai middleware.
type Route struct {
	Match      Match
	Handler    http.Handler
	Middleware []Middleware
}

// Compile menyusun daftar route menjadi satu http.Handler. Middleware
// dibungkus SEKALI saat kompilasi (bukan per-request). Saat request datang,
// route pertama yang cocok yang melayani; bila tak ada, 404.
func Compile(routes []Route) http.Handler {
	compiled := make([]compiledRoute, len(routes))
	for i, rt := range routes {
		h := rt.Handler
		// Bungkus dari dalam ke luar agar Middleware[0] terluar.
		for j := len(rt.Middleware) - 1; j >= 0; j-- {
			h = rt.Middleware[j](h)
		}
		compiled[i] = compiledRoute{match: rt.Match, handler: h}
	}
	return &dispatcher{routes: compiled}
}

type compiledRoute struct {
	match   Match
	handler http.Handler
}

type dispatcher struct{ routes []compiledRoute }

func (d *dispatcher) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	for _, rt := range d.routes {
		if rt.match.Matches(r) {
			rt.handler.ServeHTTP(w, r)
			return
		}
	}
	http.NotFound(w, r)
}
