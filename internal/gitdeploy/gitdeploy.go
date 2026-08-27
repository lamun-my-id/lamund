// Package gitdeploy mengambil kode aplikasi dari repositori Git ke folder kerja
// app (clone segar atau fetch+reset). Perintah git dijalankan lewat argv
// (bukan shell) sehingga URL/branch tak bisa jadi injeksi shell; validasi
// bentuk URL/branch dilakukan pemanggil (lapisan API).
package gitdeploy

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// validRef membatasi nama branch ke karakter ref Git yang aman dan menolak
// yang diawali "-" (agar tak ditafsir git sebagai flag).
func validRef(ref string) bool {
	if ref == "" || strings.HasPrefix(ref, "-") {
		return false
	}
	for _, r := range ref {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '.', r == '_', r == '/', r == '-':
		default:
			return false
		}
	}
	return true
}

// Fetch memastikan dir berisi kode terkini dari repoURL@branch. Bila dir sudah
// berupa repo, lakukan fetch + hard reset; jika belum, clone dangkal. Output
// git ditulis ke logw.
func Fetch(repoURL, branch, dir string, logw io.Writer) error {
	if branch == "" {
		branch = "main"
	}
	// Cegah argument-injection (flag smuggling) ke git: branch & URL tak boleh
	// diawali "-", dan branch dibatasi karakter ref yang aman.
	if !validRef(branch) {
		return fmt.Errorf("nama branch tidak valid: %q", branch)
	}
	if strings.HasPrefix(repoURL, "-") {
		return fmt.Errorf("repo URL tidak valid: %q", repoURL)
	}
	if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
		fmt.Fprintf(logw, "› git fetch %s\n", branch)
		if err := run(logw, dir, "git", "fetch", "--depth", "1", "origin", "--", branch); err != nil {
			return err
		}
		return run(logw, dir, "git", "reset", "--hard", "FETCH_HEAD")
	}
	// Clone segar butuh direktori kosong — bersihkan isi (git = sumber kebenaran).
	if err := emptyDir(dir); err != nil {
		return err
	}
	fmt.Fprintf(logw, "› git clone %s (branch %s)\n", redactURL(repoURL), branch)
	// "--" memisahkan opsi dari argumen agar URL/branch tak ditafsir sbagai flag.
	return run(logw, dir, "git", "clone", "--depth", "1", "--branch", branch, "--", repoURL, ".")
}

// Head mengembalikan short SHA dan subjek commit HEAD di dir (best-effort:
// string kosong bila dir bukan repo atau git gagal). Dipakai untuk mencatat
// riwayat deploy — kegagalan di sini tak boleh menggagalkan deploy.
func Head(dir string) (sha, subject string) {
	out, err := exec.Command("git", "-C", dir, "log", "-1", "--format=%h%x1f%s").Output()
	if err != nil {
		return "", ""
	}
	parts := strings.SplitN(strings.TrimRight(string(out), "\n"), "\x1f", 2)
	sha = parts[0]
	if len(parts) == 2 {
		subject = parts[1]
	}
	return sha, subject
}

// redactURL menyembunyikan kredensial userinfo (mis. token di
// https://x-access-token:TOKEN@host/…) sebelum URL ditulis ke log.
func redactURL(u string) string {
	i := strings.Index(u, "://")
	if i < 0 {
		return u
	}
	rest := u[i+3:]
	at := strings.IndexByte(rest, '@')
	if at < 0 {
		return u
	}
	return u[:i+3] + "***@" + rest[at+1:]
}

func run(logw io.Writer, dir, name string, args ...string) error {
	c := exec.Command(name, args...)
	c.Dir = dir
	c.Stdout = logw
	c.Stderr = logw
	if err := c.Run(); err != nil {
		// Redaksi kredensial pada arg (mis. URL clone dgn token) di pesan error.
		safe := make([]string, len(args))
		for i, a := range args {
			safe[i] = redactURL(a)
		}
		return fmt.Errorf("%s %v gagal: %w", name, safe, err)
	}
	return nil
}

// Seed menginisialisasi repo Git baru di dir (berisi konten site), commit semua
// isi, lalu push ke repoURL/branch. Dipakai untuk mengisi repo GitHub yang baru
// dibuat dengan konten awal site sebagai commit pertama (flow "bikin repo" ala
// Vercel). repoURL biasanya bertoken (x-access-token:…@github.com) → di-redact
// di log. Identitas committer disuntik lewat -c agar tak butuh git config global.
func Seed(repoURL, branch, dir, authorName, authorEmail string, logw io.Writer) error {
	if branch == "" {
		branch = "main"
	}
	if !validRef(branch) {
		return fmt.Errorf("nama branch tidak valid: %q", branch)
	}
	if strings.HasPrefix(repoURL, "-") {
		return fmt.Errorf("repo URL tidak valid: %q", repoURL)
	}
	// Repo fresh: buang .git lama bila ada (git = sumber kebenaran baru).
	_ = os.RemoveAll(filepath.Join(dir, ".git"))
	if authorName == "" {
		authorName = "lamund"
	}
	if authorEmail == "" {
		authorEmail = "lamund@users.noreply.github.com"
	}
	ident := []string{"-c", "user.name=" + authorName, "-c", "user.email=" + authorEmail}
	fmt.Fprintf(logw, "› git init & push ke %s (branch %s)\n", redactURL(repoURL), branch)
	if err := run(logw, dir, "git", "init", "-b", branch); err != nil {
		return err
	}
	if err := run(logw, dir, "git", "add", "-A"); err != nil {
		return err
	}
	commitArgs := append(append([]string{}, ident...), "commit", "--allow-empty", "-m", "initial commit from lamund")
	if err := run(logw, dir, "git", commitArgs...); err != nil {
		return err
	}
	if err := run(logw, dir, "git", "remote", "add", "origin", "--", repoURL); err != nil {
		return err
	}
	return run(logw, dir, "git", "push", "-u", "origin", branch)
}

// emptyDir mengosongkan isi dir tanpa menghapus dir-nya sendiri.
func emptyDir(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return os.MkdirAll(dir, 0o750)
		}
		return err
	}
	for _, e := range entries {
		if err := os.RemoveAll(filepath.Join(dir, e.Name())); err != nil {
			return err
		}
	}
	return nil
}
