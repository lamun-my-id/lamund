package gitdeploy

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestSeed: init+commit+push konten dir ke bare remote lokal, lalu pastikan
// ref branch muncul di remote (commit pertama terkirim).
func TestSeed(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git tak tersedia")
	}
	base := t.TempDir()
	remote := filepath.Join(base, "remote.git")
	if err := exec.Command("git", "init", "--bare", remote).Run(); err != nil {
		t.Fatalf("init bare: %v", err)
	}

	// dir konten site dengan satu file.
	work := filepath.Join(base, "work")
	if err := os.MkdirAll(work, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work, "index.html"), []byte("<h1>hi</h1>"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := Seed("file://"+remote, "main", work, "tester", "t@e.x", io.Discard); err != nil {
		t.Fatalf("Seed error: %v", err)
	}
	out, err := exec.Command("git", "ls-remote", "--heads", remote).Output()
	if err != nil {
		t.Fatalf("ls-remote: %v", err)
	}
	if len(out) == 0 || !contains(string(out), "refs/heads/main") {
		t.Fatalf("branch main tak ditemukan di remote, ls-remote=%q", out)
	}
}

// TestSeedEmptyDir: dir kosong tetap sukses (commit --allow-empty).
func TestSeedEmptyDir(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git tak tersedia")
	}
	base := t.TempDir()
	remote := filepath.Join(base, "remote.git")
	if err := exec.Command("git", "init", "--bare", remote).Run(); err != nil {
		t.Fatalf("init bare: %v", err)
	}
	work := filepath.Join(base, "empty")
	if err := os.MkdirAll(work, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := Seed("file://"+remote, "main", work, "", "", io.Discard); err != nil {
		t.Fatalf("Seed dir kosong harus sukses: %v", err)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
