// Package static melayani file situs user. Keamanan adalah fitur utamanya:
// os.Root menjamin SEMUA akses file terkurung di root situs — termasuk serangan
// via symlink, yang lolos dari filepath.Clean biasa (PRD §6.3).
package static

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
)

// Handler menyajikan file statis. Bila SPA=true, path yang tak ditemukan dan
// bukan aset (tanpa ekstensi berkas) dilayani index.html — sehingga aplikasi
// SPA (Vue/React, Next.js export) menangani routing sisi-klien & refresh
// halaman tidak 404. Aset yang hilang (mis. app.js) tetap 404.
type Handler struct {
	root *os.Root
	spa  bool
}

func New(dir string, spa bool) (*Handler, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	return &Handler{root: root, spa: spa}, nil
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	name := path.Clean(r.URL.Path)[1:] // buang "/" depan; "" berarti root
	if name == "" {
		name = "index.html"
	}
	h.serveFile(w, r, name, true)
}

// notFound: untuk SPA, path rute (tanpa ekstensi) jatuh ke index.html;
// aset yang hilang (ada ekstensi) tetap 404.
func (h *Handler) notFound(w http.ResponseWriter, r *http.Request, name string) {
	if h.spa && path.Ext(name) == "" {
		h.serveFile(w, r, "index.html", false)
		return
	}
	// Custom 404: sajikan 404.html tenant bila ada (ekspektasi ala Vercel).
	// Dibuka langsung (bukan via serveFile) agar tak rekursi bila 404.html hilang.
	if f, err := h.root.Open("404.html"); err == nil {
		defer f.Close()
		if st, e := f.Stat(); e == nil && !st.IsDir() {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusNotFound)
			io.Copy(w, f)
			return
		}
	}
	http.NotFound(w, r)
}

func (h *Handler) serveFile(w http.ResponseWriter, r *http.Request, name string, tryIndex bool) {
	f, err := h.root.Open(name) // os.Root menolak escape, termasuk symlink keluar
	if err != nil {
		h.notFound(w, r, name)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if st.IsDir() {
		if tryIndex {
			h.serveFile(w, r, path.Join(name, "index.html"), false)
			return
		}
		h.notFound(w, r, name) // tanpa directory listing
		return
	}
	// Validator caching: ETag (mtime+size) untuk revalidasi cepat via 304, plus
	// Cache-Control singkat agar navigasi cepat tanpa risiko konten basah lama.
	// http.ServeContent menghormati If-None-Match/If-Modified-Since → 304.
	if w.Header().Get("ETag") == "" {
		w.Header().Set("ETag", fmt.Sprintf(`"%x-%x"`, st.ModTime().UnixNano(), st.Size()))
	}
	if w.Header().Get("Cache-Control") == "" {
		w.Header().Set("Cache-Control", "public, max-age=60")
	}
	http.ServeContent(w, r, st.Name(), st.ModTime(), f)
}
