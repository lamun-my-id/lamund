package auth

import (
	"testing"
	"time"
)

func TestPasswordHashVerify(t *testing.T) {
	hash, err := HashPassword("gajah-terbang-42")
	if err != nil {
		t.Fatal(err)
	}
	if hash == "gajah-terbang-42" {
		t.Fatal("hash tidak boleh sama dengan plaintext")
	}
	if err := VerifyPassword("gajah-terbang-42", hash); err != nil {
		t.Fatalf("password benar harus lolos: %v", err)
	}
	if err := VerifyPassword("salah", hash); err != ErrMismatch {
		t.Fatalf("password salah harus ErrMismatch, dapat: %v", err)
	}
	// dua hash dari password sama harus beda (salt acak)
	hash2, _ := HashPassword("gajah-terbang-42")
	if hash == hash2 {
		t.Fatal("salt harus acak; hash tidak boleh identik")
	}
}

func TestVerifyRejectsGarbage(t *testing.T) {
	for _, bad := range []string{"", "bukan-hash", "$argon2id$rusak"} {
		if err := VerifyPassword("x", bad); err == nil {
			t.Fatalf("hash rusak %q harus error", bad)
		}
	}
}

func TestJWTIssueParse(t *testing.T) {
	secret, _ := GenerateSecret()
	iss := NewIssuer(secret, time.Hour)
	tok, err := iss.Issue(7, "admin", 0)
	if err != nil {
		t.Fatal(err)
	}
	c, err := iss.Parse(tok)
	if err != nil {
		t.Fatalf("token valid harus terparse: %v", err)
	}
	if c.UserID != 7 || c.Role != "admin" {
		t.Fatalf("klaim salah: %+v", c)
	}
	// secret beda harus ditolak
	other := NewIssuer([]byte("secret-lain-yang-panjang-sekali"), time.Hour)
	if _, err := other.Parse(tok); err == nil {
		t.Fatal("token dengan secret berbeda harus ditolak")
	}
}

func TestJWTTokenVersion(t *testing.T) {
	secret, _ := GenerateSecret()
	iss := NewIssuer(secret, time.Hour)

	// ver=3 harus terbaca kembali
	tok, err := iss.Issue(1, "member", 3)
	if err != nil {
		t.Fatal(err)
	}
	c, err := iss.Parse(tok)
	if err != nil {
		t.Fatalf("token ver=3 harus terparse: %v", err)
	}
	if c.Ver != 3 {
		t.Fatalf("ver harus 3, dapat %d", c.Ver)
	}

	// ver=0 harus terbaca sebagai 0
	tok0, err := iss.Issue(2, "member", 0)
	if err != nil {
		t.Fatal(err)
	}
	c0, err := iss.Parse(tok0)
	if err != nil {
		t.Fatalf("token ver=0 harus terparse: %v", err)
	}
	if c0.Ver != 0 {
		t.Fatalf("ver harus 0, dapat %d", c0.Ver)
	}
}

func TestJWTExpired(t *testing.T) {
	secret, _ := GenerateSecret()
	iss := NewIssuer(secret, time.Hour)
	base := time.Now()
	iss.now = func() time.Time { return base.Add(-2 * time.Hour) } // terbit 2 jam lalu
	tok, _ := iss.Issue(1, "user", 0)
	iss.now = func() time.Time { return base }
	if _, err := iss.Parse(tok); err == nil {
		t.Fatal("token kedaluwarsa harus ditolak")
	}
}

func TestAPIKeyGenerate(t *testing.T) {
	pt, hash, err := GenerateAPIKey()
	if err != nil {
		t.Fatal(err)
	}
	if len(pt) < 20 || pt[:4] != "lmd_" {
		t.Fatalf("format key tak terduga: %q", pt)
	}
	if hash != HashAPIKey(pt) {
		t.Fatal("hash harus deterministik dari plaintext")
	}
	pt2, _, _ := GenerateAPIKey()
	if pt == pt2 {
		t.Fatal("key harus unik")
	}
}
