package api

import (
	"net/http"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/lamun-my-id/lamund/internal/auth"
	"github.com/lamun-my-id/lamund/internal/store"
)

// capMailer adalah Mailer palsu yang menangkap email terakhir yang dikirim.
// Mengekstrak token reset dari path /reset/<token> dan token join dari /join/<token>.
type capMailer struct {
	lastTo    string
	lastBody  string
	token     string // diekstrak dari /reset/<token>
	joinToken string // diekstrak dari /join/<token>
}

var resetRe = regexp.MustCompile(`/reset/([a-f0-9]+)`)
var joinRe = regexp.MustCompile(`/join/([a-f0-9]+)`)

func (c *capMailer) Send(to, subject, htmlBody string) error {
	c.lastTo = to
	c.lastBody = htmlBody
	if m := resetRe.FindStringSubmatch(htmlBody); m != nil {
		c.token = m[1]
	}
	if m := joinRe.FindStringSubmatch(htmlBody); m != nil {
		c.joinToken = m[1]
	}
	return nil
}

// harnessMail membangun API sama seperti harness(), tapi menyuntikkan Mailer penangkap.
func harnessMail(t *testing.T, cap *capMailer) (http.Handler, *store.Store) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	hash, _ := auth.HashPassword("rahasia123")
	if _, err := st.CreateUser(store.User{Username: "admin", PasswordHash: hash, Role: "superadmin"}); err != nil {
		t.Fatal(err)
	}
	secret, _ := auth.GenerateSecret()
	h := New(Deps{Store: st, Issuer: auth.NewIssuer(secret, time.Hour), Mailer: cap})
	return h, st
}
