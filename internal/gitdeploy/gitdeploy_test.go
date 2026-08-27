package gitdeploy

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// makeRepo membuat repo git lokal dgn satu commit di branch "main".
func makeRepo(t *testing.T) (string, string) {
	t.Helper()
	repo := t.TempDir()
	git := func(args ...string) {
		c := exec.Command("git", args...)
		c.Dir = repo
		c.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q", "-b", "main")
	os.WriteFile(filepath.Join(repo, "index.html"), []byte("v1"), 0o644)
	git("add", ".")
	git("commit", "-q", "-m", "v1")
	return repo, "main"
}

func TestFetchClonesThenPulls(t *testing.T) {
	repo, branch := makeRepo(t)
	work := t.TempDir()
	var log bytes.Buffer

	// clone pertama
	if err := Fetch(repo, branch, work, &log); err != nil {
		t.Fatalf("clone: %v\n%s", err, log.String())
	}
	if b, _ := os.ReadFile(filepath.Join(work, "index.html")); string(b) != "v1" {
		t.Fatalf("isi setelah clone: %q", b)
	}

	// commit baru di repo, lalu fetch harus mengambil v2
	os.WriteFile(filepath.Join(repo, "index.html"), []byte("v2"), 0o644)
	c := exec.Command("git", "commit", "-aqm", "v2")
	c.Dir = repo
	c.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("commit v2: %v %s", err, out)
	}

	if err := Fetch(repo, branch, work, &log); err != nil {
		t.Fatalf("fetch: %v\n%s", err, log.String())
	}
	if b, _ := os.ReadFile(filepath.Join(work, "index.html")); string(b) != "v2" {
		t.Fatalf("fetch tak memperbarui: %q", b)
	}
}

func TestFetchBadRepoErrors(t *testing.T) {
	var log bytes.Buffer
	if err := Fetch(t.TempDir()+"/tidak-ada-repo", "main", t.TempDir(), &log); err == nil {
		t.Fatal("repo tak valid harus error")
	}
}

func TestFetchRejectsFlagInjection(t *testing.T) {
	var log bytes.Buffer
	// branch berupa flag → ditolak sebelum git dipanggil
	for _, bad := range []string{"--upload-pack=touch /tmp/x", "-x", "a b", "a;b"} {
		if err := Fetch("https://example.com/r.git", bad, t.TempDir(), &log); err == nil {
			t.Fatalf("branch %q harus ditolak", bad)
		}
	}
	// URL diawali "-" ditolak
	if err := Fetch("-oProxyCommand=x", "main", t.TempDir(), &log); err == nil {
		t.Fatal("URL diawali - harus ditolak")
	}
}
