package store

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// FileInfo adalah satu berkas dalam situs (relatif ke root situs).
type FileInfo struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
}

// SiteFS mengurung berkas situs tiap tenant di bawah base/<user_id>/.
// Semua I/O lewat *os.Root sehingga upaya traversal lintas-tenant
// (mis. "../<user_lain>/x") ditolak di level syscall.
type SiteFS struct{ base string }

// NewSiteFS membuat manajer berkas situs berakar di base
// (mis. /var/lib/lamund/sites).
func NewSiteFS(base string) *SiteFS { return &SiteFS{base: base} }

// userDir adalah direktori root milik satu user: base/<user_id>.
func (f *SiteFS) userDir(userID int64) string {
	return filepath.Join(f.base, strconv.FormatInt(userID, 10))
}

// SiteRoot mengembalikan path terkurung untuk sebuah situs:
// base/<user_id>/<domain>. Hanya untuk referensi/penyimpanan di DB —
// operasi berkas tetap harus lewat OpenUserRoot.
func (f *SiteFS) SiteRoot(userID int64, domain string) string {
	return filepath.Join(f.userDir(userID), domain)
}

// OpenUserRoot membuka *os.Root pada direktori user (dibuat bila belum ada).
// Semua operasi pada Root ini tak bisa keluar dari direktori user.
func (f *SiteFS) OpenUserRoot(userID int64) (*os.Root, error) {
	dir := f.userDir(userID)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, err
	}
	return os.OpenRoot(dir)
}

// EnsureSiteDir memastikan direktori situs (<user>/<domain>) ada.
func (f *SiteFS) EnsureSiteDir(userID int64, domain string) error {
	root, err := f.OpenUserRoot(userID)
	if err != nil {
		return err
	}
	defer root.Close()
	return root.MkdirAll(domain, 0o750)
}

// WriteSiteFile menulis data ke <domain>/<rel> di dalam root user.
// rel yang mencoba keluar direktori user akan ditolak os.Root.
func (f *SiteFS) WriteSiteFile(userID int64, domain, rel string, data []byte) error {
	root, err := f.OpenUserRoot(userID)
	if err != nil {
		return err
	}
	defer root.Close()
	full := filepath.Join(domain, rel)
	if dir := filepath.Dir(full); dir != "." {
		if err := root.MkdirAll(dir, 0o750); err != nil {
			return fmt.Errorf("buat direktori: %w", err)
		}
	}
	fh, err := root.Create(full)
	if err != nil {
		return err
	}
	defer fh.Close()
	_, err = fh.Write(data)
	return err
}

// MkdirSite membuat folder <domain>/<rel> (beserta induknya) di root user.
func (f *SiteFS) MkdirSite(userID int64, domain, rel string) error {
	root, err := f.OpenUserRoot(userID)
	if err != nil {
		return err
	}
	defer root.Close()
	return root.MkdirAll(filepath.Join(domain, rel), 0o750)
}

// DeleteSitePath menghapus berkas/folder <domain>/<rel> (rekursif untuk folder).
// Menolak rel kosong agar tak menghapus seluruh situs (pakai RemoveSite untuk itu).
func (f *SiteFS) DeleteSitePath(userID int64, domain, rel string) error {
	rel = strings.Trim(rel, "/")
	if rel == "" || rel == "." {
		return fmt.Errorf("path tidak valid")
	}
	root, err := f.OpenUserRoot(userID)
	if err != nil {
		return err
	}
	defer root.Close()
	return root.RemoveAll(filepath.Join(domain, rel))
}

// ReadSiteFile membaca <domain>/<rel> dari dalam root user.
func (f *SiteFS) ReadSiteFile(userID int64, domain, rel string) ([]byte, error) {
	root, err := f.OpenUserRoot(userID)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	fh, err := root.Open(filepath.Join(domain, rel))
	if err != nil {
		return nil, err
	}
	defer fh.Close()
	return io.ReadAll(fh)
}

// ListFiles mengembalikan semua berkas situs (rekursif, path relatif), urut.
func (f *SiteFS) ListFiles(userID int64, domain string) ([]FileInfo, error) {
	siteDir := f.SiteRoot(userID, domain)
	var out []FileInfo
	err := filepath.WalkDir(siteDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil // situs belum punya berkas
			}
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(siteDir, path)
		out = append(out, FileInfo{Path: filepath.ToSlash(rel), Size: info.Size()})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

// DirSize menghitung total byte semua berkas situs (0 bila belum ada).
func (f *SiteFS) DirSize(userID int64, domain string) (int64, error) {
	files, err := f.ListFiles(userID, domain)
	if err != nil {
		return 0, err
	}
	var total int64
	for _, fi := range files {
		total += fi.Size
	}
	return total, nil
}

// RemoveSite menghapus seluruh berkas satu situs (lewat os.Root user).
func (f *SiteFS) RemoveSite(userID int64, domain string) error {
	root, err := f.OpenUserRoot(userID)
	if err != nil {
		return err
	}
	defer root.Close()
	if err := root.RemoveAll(domain); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// deployHardCap adalah plafon absolut ukuran hasil ekstrak, dipakai sebagai
// pertahanan zip-bomb bahkan saat kuota "tanpa batas" (admin). Arsip terkompres
// dibatasi di lapisan HTTP, tapi rasio dekompresi bisa ribuan kali lipat.
const deployHardCap = 5 << 30 // 5 GiB

// DeployZip mengekstrak zip ke folder situs secara atomik: ekstrak ke folder
// sementara di dalam root user, lalu tukar. Bila gagal, situs lama tetap utuh.
// Menolak zip-slip (path keluar folder) via os.Root. maxBytes>0 membatasi total
// ukuran hasil ekstrak (0 = pakai plafon absolut). Batas ditegakkan pada byte
// hasil dekompresi NYATA (bukan header UncompressedSize64 yang bisa dipalsukan).
// Mengembalikan total byte tertulis.
func (f *SiteFS) DeployZip(userID int64, domain string, zr *zip.Reader, maxBytes int64) (int64, error) {
	root, err := f.OpenUserRoot(userID)
	if err != nil {
		return 0, err
	}
	defer root.Close()

	budget := int64(deployHardCap)
	if maxBytes > 0 && maxBytes < budget {
		budget = maxBytes
	}

	tmp := domain + ".deploy-tmp"
	_ = root.RemoveAll(tmp)
	defer root.RemoveAll(tmp) // bersihkan bila gagal / setelah swap

	var written int64
	for _, zf := range zr.File {
		if zf.FileInfo().IsDir() {
			continue
		}
		// Netralkan path & tolak yang mencoba keluar (os.Root juga menjaga).
		clean := strings.TrimPrefix(filepath.ToSlash(filepath.Clean(zf.Name)), "/")
		if clean == "" || strings.HasPrefix(clean, "../") || strings.Contains(clean, "/../") {
			continue
		}
		n, err := extractOne(root, filepath.Join(tmp, clean), zf, budget-written)
		if err != nil {
			return 0, err
		}
		written += n
	}

	// Swap: buang situs lama, ganti dengan hasil ekstrak.
	if err := root.RemoveAll(domain); err != nil && !os.IsNotExist(err) {
		return 0, err
	}
	if err := root.Rename(tmp, domain); err != nil {
		return 0, fmt.Errorf("swap deploy: %w", err)
	}
	return written, nil
}

// extractOne menulis satu entri zip ke dest, menegakkan batas remaining pada
// byte yang benar-benar didekompres (menolak zip bomb / header palsu).
func extractOne(root *os.Root, dest string, zf *zip.File, remaining int64) (int64, error) {
	if dir := filepath.Dir(dest); dir != "." {
		if err := root.MkdirAll(dir, 0o750); err != nil {
			return 0, err
		}
	}
	src, err := zf.Open()
	if err != nil {
		return 0, err
	}
	defer src.Close()
	out, err := root.Create(dest)
	if err != nil {
		return 0, err
	}
	defer out.Close()
	// Salin maksimal remaining+1 byte; bila melebihi remaining → tolak.
	lr := &io.LimitedReader{R: src, N: remaining + 1}
	n, err := io.Copy(out, lr)
	if err != nil {
		return n, err
	}
	if n > remaining {
		return n, fmt.Errorf("arsip melebihi batas storage")
	}
	return n, nil
}
