package version

import (
	"strings"
	"testing"
)

func TestString(t *testing.T) {
	s := String()
	if !strings.HasPrefix(s, "lamund v") {
		t.Fatalf("version %q tidak berprefix 'lamund v'", s)
	}
}
