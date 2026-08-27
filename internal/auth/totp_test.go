package auth

import (
	"strings"
	"testing"
	"time"
)

// TestTOTP_RFC6238Vectors memverifikasi implementasi TOTP lawan vektor resmi
// RFC 6238 Appendix B (SHA1). Secret ASCII "12345678901234567890" = base32
// GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ.
// Vektor RFC menghasilkan kode 8-digit; kita ambil 6-digit terakhir.
func TestTOTP_RFC6238Vectors(t *testing.T) {
	secret := "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"
	cases := []struct {
		unix  int64
		want8 string
	}{
		{59, "94287082"},
		{1111111109, "07081804"},
		{1111111111, "14050471"},
		{1234567890, "89005924"},
		{2000000000, "69279037"},
		{20000000000, "65353130"},
	}
	for _, c := range cases {
		got, err := TOTPCodeAt(secret, time.Unix(c.unix, 0).UTC())
		if err != nil {
			t.Fatalf("TOTPCodeAt(t=%d): %v", c.unix, err)
		}
		want6 := c.want8[len(c.want8)-6:]
		if got != want6 {
			t.Fatalf("t=%d: TOTP=%s, mau %s (dari RFC %s)", c.unix, got, want6, c.want8)
		}
	}
}

// TestVerifyTOTP_Window memverifikasi window ±1 step dan penolakan kode lama/pendek.
func TestVerifyTOTP_Window(t *testing.T) {
	secret, err := GenerateTOTPSecret()
	if err != nil {
		t.Fatalf("GenerateTOTPSecret: %v", err)
	}
	now := time.Unix(1700000000, 0).UTC()

	code, err := TOTPCodeAt(secret, now)
	if err != nil {
		t.Fatalf("TOTPCodeAt: %v", err)
	}

	// kode saat ini cocok
	if _, ok := VerifyTOTP(secret, code, now); !ok {
		t.Fatal("kode saat ini harus valid")
	}

	// kode step sebelumnya masih diterima (skew -30s)
	prev, err := TOTPCodeAt(secret, now.Add(-30*time.Second))
	if err != nil {
		t.Fatalf("TOTPCodeAt(-30s): %v", err)
	}
	if _, ok := VerifyTOTP(secret, prev, now); !ok {
		t.Fatal("kode -30s harus valid (window)")
	}

	// kode 90s lalu ditolak
	old, err := TOTPCodeAt(secret, now.Add(-90*time.Second))
	if err != nil {
		t.Fatalf("TOTPCodeAt(-90s): %v", err)
	}
	if _, ok := VerifyTOTP(secret, old, now); ok {
		t.Fatal("kode -90s harus ditolak")
	}

	// kode non-6-digit ditolak
	if _, ok := VerifyTOTP(secret, "12345", now); ok {
		t.Fatal("kode non-6-digit harus ditolak")
	}
}

// TestGenerateTOTPSecret memverifikasi panjang dan karakter hasil GenerateTOTPSecret.
func TestGenerateTOTPSecret(t *testing.T) {
	s, err := GenerateTOTPSecret()
	if err != nil {
		t.Fatalf("GenerateTOTPSecret: %v", err)
	}
	// 20 byte → base32 tanpa padding = 32 karakter
	if len(s) != 32 {
		t.Fatalf("panjang secret: mau 32, dapat %d", len(s))
	}
	for _, c := range s {
		if !((c >= 'A' && c <= 'Z') || (c >= '2' && c <= '7')) {
			t.Fatalf("karakter tidak valid dalam secret: %c", c)
		}
	}
}

// TestTOTPURI memverifikasi format URI otpauth.
func TestTOTPURI(t *testing.T) {
	secret := "JBSWY3DPEHPK3PXP"
	uri := TOTPURI(secret, "user@example.com", "Lamund")
	if !strings.HasPrefix(uri, "otpauth://totp/") {
		t.Fatalf("URI tidak dimulai dengan otpauth://totp/: %s", uri)
	}
	if !strings.Contains(uri, "secret="+secret) {
		t.Fatalf("URI tidak mengandung secret: %s", uri)
	}
	if !strings.Contains(uri, "issuer=Lamund") {
		t.Fatalf("URI tidak mengandung issuer: %s", uri)
	}
	if !strings.Contains(uri, "algorithm=SHA1") {
		t.Fatalf("URI tidak mengandung algorithm=SHA1: %s", uri)
	}
	if !strings.Contains(uri, "digits=6") {
		t.Fatalf("URI tidak mengandung digits=6: %s", uri)
	}
	if !strings.Contains(uri, "period=30") {
		t.Fatalf("URI tidak mengandung period=30: %s", uri)
	}
}

// TestGenerateRecoveryCodes memverifikasi jumlah dan format kode pemulihan.
func TestGenerateRecoveryCodes(t *testing.T) {
	codes, err := GenerateRecoveryCodes(10)
	if err != nil {
		t.Fatalf("GenerateRecoveryCodes: %v", err)
	}
	if len(codes) != 10 {
		t.Fatalf("jumlah kode: mau 10, dapat %d", len(codes))
	}
	seen := make(map[string]bool)
	for _, c := range codes {
		parts := strings.Split(c, "-")
		if len(parts) != 2 || len(parts[0]) != 5 || len(parts[1]) != 5 {
			t.Fatalf("format kode tidak valid: %s", c)
		}
		// lowercase base32 alphabet: a-z dan 2-7
		for _, ch := range c {
			if ch == '-' {
				continue
			}
			if !((ch >= 'a' && ch <= 'z') || (ch >= '2' && ch <= '7')) {
				t.Fatalf("karakter tidak valid dalam kode pemulihan: %c (kode: %s)", ch, c)
			}
		}
		if seen[c] {
			t.Fatalf("kode duplikat: %s", c)
		}
		seen[c] = true
	}
}

// TestVerifyTOTP_StepReturn memverifikasi nilai step yang dikembalikan VerifyTOTP.
func TestVerifyTOTP_StepReturn(t *testing.T) {
	secret, _ := GenerateTOTPSecret()
	now := time.Unix(1700000000, 0).UTC()
	nowStep := int64(1700000000 / 30)

	code, _ := TOTPCodeAt(secret, now)
	step, ok := VerifyTOTP(secret, code, now)
	if !ok {
		t.Fatal("harus valid")
	}
	if step != nowStep {
		t.Fatalf("step mau %d, dapat %d", nowStep, step)
	}

	// step -1 (kode dari 30s lalu)
	prev, _ := TOTPCodeAt(secret, now.Add(-30*time.Second))
	stepPrev, ok := VerifyTOTP(secret, prev, now)
	if !ok {
		t.Fatal("kode -30s harus valid")
	}
	if stepPrev != nowStep-1 {
		t.Fatalf("step -1 mau %d, dapat %d", nowStep-1, stepPrev)
	}
}
