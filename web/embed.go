// Package webpanel meng-embed panel Vue yang sudah di-build (web/dist) ke
// dalam binary lamund, sehingga instalasi tetap satu berkas.
//
// Jalankan `npm --prefix web run build` sebelum `go build` agar dist terisi.
package webpanel

import (
	"embed"
	"io"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed all:dist
var assets embed.FS

// Handler menyajikan panel statis dengan SPA fallback: path yang cocok
// dengan berkas dikirim apa adanya; selain itu index.html dikembalikan
// agar rute sisi-klien (mis. /sites/x) tetap termuat.
func Handler() http.Handler {
	sub, err := fs.Sub(assets, "dist")
	if err != nil {
		panic(err) // dist wajib ada saat build (jalankan npm build dulu)
	}
	fileServer := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(r.URL.Path, "/")
		if p != "" {
			if f, err := sub.Open(p); err == nil {
				f.Close()
				// Aset /assets/* punya nama ber-hash konten → aman di-cache lama.
				if strings.HasPrefix(p, "assets/") {
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				}
				fileServer.ServeHTTP(w, r)
				return
			}
		}
		// SPA fallback → index.html. WAJIB no-cache (selalu revalidasi) agar
		// setelah deploy baru browser memuat index.html terbaru yang menunjuk
		// chunk ber-hash terbaru — cegah panel basi ("klik tak terjadi apa-apa").
		f, err := sub.Open("index.html")
		if err != nil {
			http.Error(w, "panel tidak tersedia", http.StatusInternalServerError)
			return
		}
		defer f.Close()
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache, must-revalidate")
		io.Copy(w, f)
	})
}
