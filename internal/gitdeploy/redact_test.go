package gitdeploy

import "testing"

func TestRedactURL(t *testing.T) {
	cases := map[string]string{
		"https://x-access-token:ghp_secret@github.com/a/b.git": "https://***@github.com/a/b.git",
		"https://github.com/a/b.git":                           "https://github.com/a/b.git",
		"git@github.com:a/b.git":                               "git@github.com:a/b.git",
	}
	for in, want := range cases {
		if got := redactURL(in); got != want {
			t.Errorf("redactURL(%q)=%q, mau %q", in, got, want)
		}
	}
}
