package store

import "testing"

func TestValidateDomain(t *testing.T) {
	valid := []string{"example.com", "a.b.c.example.co.id", "xn--nama-punycode.id", "123.example.com"}
	invalid := []string{"", "EXAMPLE.com", "*.example.com", "example", "-a.example.com",
		"a-.example.com", "a..example.com", "exam ple.com",
		string(make([]byte, 254)) + ".com"}
	for _, d := range valid {
		if err := ValidateDomain(d); err != nil {
			t.Errorf("ValidateDomain(%q) = %v, mau nil", d, err)
		}
	}
	for _, d := range invalid {
		if err := ValidateDomain(d); err == nil {
			t.Errorf("ValidateDomain(%q) = nil, mau error", d)
		}
	}
}
