package site

import (
	"net/http"
	"strings"

	"github.com/lamun-my-id/lamund/internal/proxy"
	"github.com/lamun-my-id/lamund/internal/static"
	"github.com/lamun-my-id/lamund/internal/store"
)

// Build menyusun http.Handler untuk sebuah situs dari model store.Site.
//
// Bila s.Config diisi, situs memakai dokumen config (routing multi-path S2).
// Bila kosong, disintesis menjadi SATU route default ("/" → handler sesuai
// type) — sehingga situs lama jalan tanpa perubahan.
func Build(s store.Site) (http.Handler, error) { return BuildWith(s, nil) }

// BuildWith seperti Build namun menyuplai resolver app untuk route app-by-name.
func BuildWith(s store.Site, resolver AppResolver) (http.Handler, error) {
	if s.Config != "" {
		cfg, err := ParseConfig(s.Config)
		if err != nil {
			return nil, err
		}
		return cfg.CompileWith(resolver)
	}
	routes, err := routesFor(s)
	if err != nil {
		return nil, err
	}
	return Compile(routes), nil
}

func routesFor(s store.Site) ([]Route, error) {
	switch s.Type {
	case "static":
		h, err := static.New(s.RootPath, false)
		if err != nil {
			return nil, err
		}
		return []Route{{Match: Match{}, Handler: defaultStack(h, true)}}, nil
	case "proxy":
		h, err := proxy.New(s.ProxyTarget)
		if err != nil {
			return nil, err
		}
		return []Route{{Match: Match{}, Handler: defaultStack(h, false)}}, nil
	case "redirect":
		// ProxyTarget menyimpan URL tujuan (mis. https://lamund.my.id). Redirect
		// 302 (bukan 301) agar tak di-cache permanen — aman bila dikonfig ulang.
		return []Route{{Match: Match{}, Handler: defaultStack(redirectHandler(s.ProxyTarget), false)}}, nil
	default:
		return nil, &unknownTypeError{s.Type}
	}
}

// redirectHandler me-redirect (302) ke base target, mempertahankan path & query.
func redirectHandler(target string) http.Handler {
	base := strings.TrimRight(target, "/")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, base+r.URL.RequestURI(), http.StatusFound)
	})
}

// defaultStack membungkus handler situs default (tanpa config) dengan middleware
// aman-by-default — sebelumnya jalur sintesis ini melewati seluruh sistem
// middleware, sehingga mayoritas situs tak dapat security headers/kompresi.
// Security headers selalu; kompresi hanya untuk static (respons proxy bisa sudah
// terkompresi upstream, hindari kompresi ganda). Cache-Control/ETag di handler static.
func defaultStack(h http.Handler, isStatic bool) http.Handler {
	h = SecurityHeaders()(h)
	if isStatic {
		h = Compress()(h)
	}
	return h
}

type unknownTypeError struct{ typ string }

func (e *unknownTypeError) Error() string { return "tipe situs tidak dikenal: " + e.typ }
