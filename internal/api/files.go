package api

import (
	"archive/zip"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/lamun-my-id/lamund/internal/logging"
	"github.com/lamun-my-id/lamund/internal/quota"
	"github.com/lamun-my-id/lamund/internal/store"
)

// maxUploadBytes membatasi ukuran arsip yang diunggah (pra-ekstrak).
const maxUploadBytes = 200 << 20 // 200 MB

func (s *server) registerFiles(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/sites/{domain}/deploy", s.requireAuth(s.handleDeploy))
	mux.HandleFunc("GET /api/v1/sites/{domain}/files", s.requireAuth(s.handleListFiles))
}

func (s *server) handleListFiles(w http.ResponseWriter, r *http.Request) {
	site, u, ok := s.ownedSite(w, r)
	if !ok {
		return
	}
	if s.d.Sites == nil {
		writeJSON(w, http.StatusOK, map[string]any{"files": []any{}})
		return
	}
	files, err := s.d.Sites.ListFiles(site.UserID, site.Domain)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "gagal membaca berkas")
		return
	}
	_ = u
	writeJSON(w, http.StatusOK, map[string]any{"files": files})
}

// handleDeploy menerima unggahan .zip (field "archive") dan mengekstraknya ke
// folder situs (mengganti isi lama secara atomik).
func (s *server) handleDeploy(w http.ResponseWriter, r *http.Request) {
	site, _, ok := s.ownedSite(w, r)
	if !ok {
		return
	}
	if s.d.Sites == nil {
		writeErr(w, http.StatusServiceUnavailable, "penyimpanan berkas tidak tersedia")
		return
	}
	// Deploy mengisi folder terkelola: untuk situs statis (disajikan langsung)
	// atau untuk aplikasi (source yang di-build & dijalankan).
	if site.Type != "static" {
		if app, _ := s.d.Store.GetAppByDomain(site.Domain); app == nil {
			writeErr(w, http.StatusBadRequest, "deploy hanya untuk situs statis atau aplikasi")
			return
		}
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes)
	file, hdr, err := r.FormFile("archive")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "unggah file .zip pada field 'archive'")
		return
	}
	defer file.Close()

	// Buffer ke berkas sementara agar bisa dibaca sbg zip (butuh ReaderAt).
	tmp, err := os.CreateTemp("", "lamund-deploy-*.zip")
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "gagal menyiapkan unggahan")
		return
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	size, err := io.Copy(tmp, file)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "unggahan terpotong atau terlalu besar")
		return
	}

	zr, err := zip.NewReader(tmp, size)
	if err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Sprintf("bukan arsip .zip yang valid (%s)", hdr.Filename))
		return
	}

	limit, err := quota.StorageLimitBytes(s.d.Store, site.UserID, ownerRole(site, s))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "gagal cek kuota")
		return
	}
	written, err := s.d.Sites.DeployZip(site.UserID, site.Domain, zr, limit)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}

	// Catat pemakaian storage bulan ini.
	s.d.Store.SetStorageBytes(site.UserID, logging.Month(s.d.Now()), written)
	// Deploy menukar inode folder situs; picu reload agar data plane membuka
	// ulang os.Root ke folder baru (kalau tidak, ia menyajikan folder lama).
	s.notifyReload()
	writeJSON(w, http.StatusOK, map[string]any{"status": "deployed", "bytes": written})
}

// ownerRole mengembalikan peran pemilik situs (untuk kuota). Admin yang
// mengelola situs milik user lain tetap dibatasi kuota si pemilik.
func ownerRole(site *store.Site, s *server) string {
	u, err := s.d.Store.GetUserByID(site.UserID)
	if err != nil || u == nil {
		return "user"
	}
	return u.Role
}

// placeholderHTML adalah isi awal situs statis sebelum di-deploy.
func placeholderHTML(domain string) []byte {
	return []byte(`<!doctype html><html lang="id"><head><meta charset="utf-8">` +
		`<meta name="viewport" content="width=device-width,initial-scale=1">` +
		`<title>` + domain + `</title>` +
		`<style>body{font-family:system-ui,sans-serif;background:#f6f5fb;color:#1b1832;` +
		`display:grid;place-items:center;min-height:100vh;margin:0}` +
		`.c{text-align:center}.c b{font-size:20px}.c p{color:#7b8496;margin-top:8px}` +
		`code{background:#ebe0ff;color:#4d3095;padding:2px 8px;border-radius:6px;font-size:13px}</style>` +
		`</head><body><div class="c"><b>` + domain + `</b>` +
		`<p>Situs ini siap. Deploy file kamu lewat panel — <code>Deploy .zip</code>.</p>` +
		`</div></body></html>`)
}
