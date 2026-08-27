package site

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/lamun-my-id/lamund/internal/proxy"
	"github.com/lamun-my-id/lamund/internal/static"
)

// ConfigVersion adalah versi skema dokumen config saat ini.
const ConfigVersion = 1

// Config adalah dokumen routing per-situs (disimpan sebagai JSON di kolom
// sites.config). Kosong = situs memakai sintesis dari type/root/target.
type Config struct {
	Version int         `json:"version"`
	Routes  []RouteSpec `json:"routes"`
}

// RouteSpec = matcher → handler + rantai middleware opsional.
type RouteSpec struct {
	Match   MatchSpec        `json:"match"`
	Handler HandlerSpec      `json:"handler"`
	Use     []MiddlewareSpec `json:"use,omitempty"`
}

// MiddlewareSpec: tiap entri mengaktifkan satu middleware (kehadiran = aktif).
type MiddlewareSpec struct {
	Cache    *CacheSpec    `json:"cache,omitempty"`
	Compress *CompressSpec `json:"compress,omitempty"`
	Headers  *HeadersSpec  `json:"headers,omitempty"`
}
type CacheSpec struct {
	TTLSeconds int `json:"ttl_seconds"`
}
type CompressSpec struct{}
type HeadersSpec struct {
	Security bool `json:"security"`
}

type MatchSpec struct {
	PathPrefix string `json:"path_prefix"`
}

// HandlerSpec: tepat SATU dari static/proxy/app yang diisi.
type HandlerSpec struct {
	Static *StaticSpec `json:"static,omitempty"`
	Proxy  *ProxySpec  `json:"proxy,omitempty"`
	App    *AppSpec    `json:"app,omitempty"`
}

type StaticSpec struct {
	Root string `json:"root"`
	SPA  bool   `json:"spa,omitempty"` // fallback ke index.html (Vue/React/Next export)
}
type ProxySpec struct {
	Upstream string `json:"upstream"`
}

// AppSpec me-mount aplikasi terkelola (run-app) ke sebuah path — proxy ke port
// app yang di-resolve SAAT BUILD lewat nama. Karena tabel routing dibangun
// ulang tiap reload (termasuk setelah blue-green swap yang mengganti port),
// route tetap nyambung tanpa menyimpan port statis. Contoh: domain.com/api →
// app "backend".
type AppSpec struct {
	Name string `json:"name"`
}

// AppResolver memetakan nama app → upstream URL (mis. http://127.0.0.1:40001).
// Data plane menyuplai ini dari store; nil = tak ada app yang bisa di-mount.
type AppResolver func(name string) (upstream string, ok bool)

// ParseConfig mengurai dokumen config JSON.
func ParseConfig(raw string) (Config, error) {
	var c Config
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		return c, fmt.Errorf("config JSON tidak valid: %w", err)
	}
	if c.Version == 0 {
		c.Version = ConfigVersion
	}
	if c.Version != ConfigVersion {
		return c, fmt.Errorf("versi config tidak didukung: %d", c.Version)
	}
	if len(c.Routes) == 0 {
		return c, fmt.Errorf("config tidak punya route")
	}
	return c, nil
}

// Compile membangun http.Handler dari config tanpa app resolver (route app
// akan error). Dipakai pemakaian yang tak melibatkan run-app.
func (c Config) Compile() (http.Handler, error) { return c.CompileWith(nil) }

// CompileWith membangun http.Handler dari config; resolver dipakai untuk
// me-resolve route app-by-name ke upstream port saat build.
func (c Config) CompileWith(resolver AppResolver) (http.Handler, error) {
	routes := make([]Route, 0, len(c.Routes))
	for i, rs := range c.Routes {
		h, err := rs.Handler.build(resolver)
		if err != nil {
			return nil, fmt.Errorf("route %d (%q): %w", i, rs.Match.PathPrefix, err)
		}
		routes = append(routes, Route{
			Match:      Match{PathPrefix: rs.Match.PathPrefix},
			Handler:    h,
			Middleware: buildMiddleware(rs.Use),
		})
	}
	return Compile(routes), nil
}

// buildMiddleware menyusun rantai dengan URUTAN TETAP (luar→dalam):
// cache → headers → compress. Cache TERLUAR: pada HIT ia langsung membalas
// respons tersimpan (sudah lengkap header+body terkompresi) tanpa menjalankan
// ulang middleware dalam — sehingga tak ada header ganda. Kunci cache
// menyertakan Accept-Encoding agar aman untuk klien gzip & non-gzip.
func buildMiddleware(specs []MiddlewareSpec) []Middleware {
	var cache, headers, compress Middleware
	for _, m := range specs {
		switch {
		case m.Headers != nil && m.Headers.Security:
			headers = SecurityHeaders()
		case m.Compress != nil:
			compress = Compress()
		case m.Cache != nil:
			ttl := time.Duration(m.Cache.TTLSeconds) * time.Second
			if ttl > 0 {
				cache = Cache(ttl)
			}
		}
	}
	var out []Middleware
	for _, mw := range []Middleware{cache, headers, compress} {
		if mw != nil {
			out = append(out, mw)
		}
	}
	return out
}

func (h HandlerSpec) build(resolver AppResolver) (http.Handler, error) {
	switch {
	case h.Static != nil && h.Proxy == nil && h.App == nil:
		return static.New(h.Static.Root, h.Static.SPA)
	case h.Proxy != nil && h.Static == nil && h.App == nil:
		return proxy.New(h.Proxy.Upstream)
	case h.App != nil && h.Static == nil && h.Proxy == nil:
		if resolver == nil {
			return nil, fmt.Errorf("route app %q: resolver app tak tersedia", h.App.Name)
		}
		upstream, ok := resolver(h.App.Name)
		if !ok {
			return nil, fmt.Errorf("app %q tidak ditemukan / tidak jalan", h.App.Name)
		}
		return proxy.New(upstream)
	default:
		return nil, fmt.Errorf("handler harus tepat satu dari static|proxy|app")
	}
}
