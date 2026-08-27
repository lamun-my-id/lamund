package store

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// makeZip membuat zip in-memory dari map path→isi, dikembalikan sbg *zip.Reader.
func makeZip(t *testing.T, files map[string]string) *zip.Reader {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(content))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	return zr
}

func TestDeployZipExtracts(t *testing.T) {
	fs := NewSiteFS(t.TempDir())
	zr := makeZip(t, map[string]string{
		"index.html":    "<h1>halo</h1>",
		"assets/app.js": "console.log(1)",
	})
	n, err := fs.DeployZip(7, "situs.com", zr, 0)
	if err != nil {
		t.Fatal(err)
	}
	if n != int64(len("<h1>halo</h1>")+len("console.log(1)")) {
		t.Fatalf("total byte salah: %d", n)
	}
	b, _ := fs.ReadSiteFile(7, "situs.com", "index.html")
	if string(b) != "<h1>halo</h1>" {
		t.Fatalf("isi index salah: %q", b)
	}
	files, _ := fs.ListFiles(7, "situs.com")
	if len(files) != 2 {
		t.Fatalf("harus 2 berkas, dapat %d", len(files))
	}
	if sz, _ := fs.DirSize(7, "situs.com"); sz != n {
		t.Fatalf("DirSize %d != %d", sz, n)
	}
}

func TestDeployZipRejectsZipSlip(t *testing.T) {
	base := t.TempDir()
	fs := NewSiteFS(base)
	// entri jahat mencoba keluar folder situs
	zr := makeZip(t, map[string]string{
		"index.html":         "ok",
		"../../../etc/pwned":  "hack",
		"../sibling/pwned.txt": "hack",
	})
	if _, err := fs.DeployZip(1, "korban.com", zr, 0); err != nil {
		t.Fatalf("deploy tak boleh gagal total: %v", err)
	}
	// berkas jahat tak boleh muncul di mana pun di bawah base
	for _, bad := range []string{
		filepath.Join(base, "etc", "pwned"),
		filepath.Join(base, "1", "sibling", "pwned.txt"),
	} {
		if _, err := os.Stat(bad); err == nil {
			t.Fatalf("zip-slip lolos: %s tercipta", bad)
		}
	}
	// index.html yang sah tetap masuk
	if b, _ := fs.ReadSiteFile(1, "korban.com", "index.html"); string(b) != "ok" {
		t.Fatalf("berkas sah harus tetap masuk, dapat %q", b)
	}
}

func TestDeployZipQuota(t *testing.T) {
	fs := NewSiteFS(t.TempDir())
	// deploy pertama sukses
	fs.DeployZip(2, "a.com", makeZip(t, map[string]string{"index.html": "v1-lama"}), 0)
	// deploy kedua melebihi kuota → gagal, situs lama utuh
	big := makeZip(t, map[string]string{"index.html": "isi yang jauh lebih besar dari batas"})
	if _, err := fs.DeployZip(2, "a.com", big, 5); err == nil {
		t.Fatal("melebihi kuota harus error")
	}
	if b, _ := fs.ReadSiteFile(2, "a.com", "index.html"); string(b) != "v1-lama" {
		t.Fatalf("situs lama harus utuh setelah deploy gagal, dapat %q", b)
	}
}

func TestDeployZipStreamLimitEnforced(t *testing.T) {
	fs := NewSiteFS(t.TempDir())
	fs.DeployZip(9, "c.com", makeZip(t, map[string]string{"index.html": "aman"}), 0)
	// batas ditegakkan pada byte hasil dekompresi NYATA, terakumulasi lintas berkas.
	big := makeZip(t, map[string]string{
		"a.txt": "0123456789", // 10
		"b.txt": "0123456789", // 10 → total 20 > batas 15
	})
	if _, err := fs.DeployZip(9, "c.com", big, 15); err == nil {
		t.Fatal("total melebihi batas harus ditolak (enforcement pada stream)")
	}
	// situs lama tetap utuh (tak ada swap saat gagal)
	if b, _ := fs.ReadSiteFile(9, "c.com", "index.html"); string(b) != "aman" {
		t.Fatalf("situs lama harus utuh, dapat %q", b)
	}
}

func TestDeployZipAtomicReplace(t *testing.T) {
	fs := NewSiteFS(t.TempDir())
	fs.DeployZip(3, "b.com", makeZip(t, map[string]string{"lama.html": "x", "index.html": "1"}), 0)
	fs.DeployZip(3, "b.com", makeZip(t, map[string]string{"index.html": "2"}), 0)
	files, _ := fs.ListFiles(3, "b.com")
	if len(files) != 1 || files[0].Path != "index.html" {
		t.Fatalf("deploy harus mengganti total (lama.html hilang), dapat %+v", files)
	}
}

func TestSiteFSWriteRead(t *testing.T) {
	fs := NewSiteFS(t.TempDir())
	if err := fs.WriteSiteFile(1, "situs.com", "index.html", []byte("halo")); err != nil {
		t.Fatal(err)
	}
	b, err := fs.ReadSiteFile(1, "situs.com", "index.html")
	if err != nil || string(b) != "halo" {
		t.Fatalf("baca kembali: %q %v", b, err)
	}
	// subdir juga boleh
	if err := fs.WriteSiteFile(1, "situs.com", "assets/app.js", []byte("x")); err != nil {
		t.Fatalf("subdir: %v", err)
	}
}

func TestSiteFSTenantIsolation(t *testing.T) {
	base := t.TempDir()
	fs := NewSiteFS(base)
	// user 2 punya berkas rahasia
	if err := fs.WriteSiteFile(2, "rahasia.com", "secret.txt", []byte("TOP")); err != nil {
		t.Fatal(err)
	}
	// user 1 mencoba traversal ke direktori user 2 → HARUS ditolak os.Root
	err := fs.WriteSiteFile(1, "..", "2/rahasia.com/pwned.txt", []byte("hack"))
	if err == nil {
		t.Fatal("traversal lintas-tenant harus ditolak")
	}
	// pastikan berkas user 2 tak tersentuh & tak ada pwned.txt
	if _, statErr := os.Stat(filepath.Join(base, "2", "rahasia.com", "pwned.txt")); statErr == nil {
		t.Fatal("berkas hasil traversal seharusnya tak pernah dibuat")
	}
	// baca lintas-tenant juga ditolak
	if _, err := fs.ReadSiteFile(1, "..", "2/rahasia.com/secret.txt"); err == nil {
		t.Fatal("baca lintas-tenant harus ditolak")
	}
}

func TestUsageAggregation(t *testing.T) {
	st := openTemp(t)
	uid, _ := st.CreateUser(User{Username: "u", PasswordHash: "h"})
	if err := st.AddBandwidth(uid, "202608", 100); err != nil {
		t.Fatal(err)
	}
	st.AddBandwidth(uid, "202608", 250)
	u, _ := st.GetUsage(uid, "202608")
	if u.BandwidthBytes != 350 {
		t.Fatalf("bandwidth harus terakumulasi 350, dapat %d", u.BandwidthBytes)
	}
	// bulan lain terpisah
	if u2, _ := st.GetUsage(uid, "202609"); u2.BandwidthBytes != 0 {
		t.Fatalf("bulan lain harus 0, dapat %d", u2.BandwidthBytes)
	}
}
