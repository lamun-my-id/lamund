package store

import (
	"path/filepath"
	"testing"
)

func openES(t *testing.T) *Store {
	st, err := Open(filepath.Join(t.TempDir(), "e.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestEmailSettingsRoundtrip(t *testing.T) {
	st := openES(t)
	if err := st.SetEmailSettings(EmailSettings{Backend: "smtp", Host: "mail.x", Port: 587, From: "a@x", TLS: true}); err != nil {
		t.Fatal(err)
	}
	got, err := st.GetEmailSettings()
	if err != nil {
		t.Fatal(err)
	}
	if got.Backend != "smtp" || got.Host != "mail.x" || got.Port != 587 || !got.TLS {
		t.Fatalf("settings salah: %+v", got)
	}
}

func TestPasswordResetRoundtrip(t *testing.T) {
	st := openES(t)
	if err := st.CreatePasswordReset(7, "tok123", "2999-01-01T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	r, ok := st.GetPasswordReset("tok123")
	if !ok || r.UserID != 7 {
		t.Fatalf("reset salah: %+v ok=%v", r, ok)
	}
	st.DeletePasswordReset("tok123")
	if _, ok := st.GetPasswordReset("tok123"); ok {
		t.Fatal("token harus terhapus")
	}
}
