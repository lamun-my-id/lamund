package email

import (
	"errors"
	"testing"

	"github.com/lamun-my-id/lamund/internal/store"
)

func TestManagerOffReturnsError(t *testing.T) {
	m := NewManager(func() (store.EmailSettings, error) { return store.EmailSettings{Backend: "off"}, nil })
	if err := m.Send("a@b", "s", "<b>hi</b>"); err == nil {
		t.Fatal("backend off harus error")
	}
}

func TestManagerUnknownBackend(t *testing.T) {
	m := NewManager(func() (store.EmailSettings, error) { return store.EmailSettings{Backend: "weird"}, nil })
	if err := m.Send("a@b", "s", "x"); err == nil {
		t.Fatal("backend tak dikenal harus error")
	}
}

func TestManagerSettingsError(t *testing.T) {
	m := NewManager(func() (store.EmailSettings, error) { return store.EmailSettings{}, errors.New("db down") })
	if err := m.Send("a@b", "s", "x"); err == nil {
		t.Fatal("error settings harus diteruskan")
	}
}
