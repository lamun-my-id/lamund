package auth

import (
	"testing"
	"time"
)

// TestMFAPendingRoundTrip memastikan pending-token bisa diterbitkan & diparse,
// mengembalikan userID yang sama.
func TestMFAPendingRoundTrip(t *testing.T) {
	secret, _ := GenerateSecret()
	iss := NewIssuer(secret, time.Hour)
	tok, err := iss.IssueMFAPending(42)
	if err != nil {
		t.Fatalf("IssueMFAPending: %v", err)
	}
	uid, err := iss.ParseMFAPending(tok)
	if err != nil {
		t.Fatalf("ParseMFAPending: %v", err)
	}
	if uid != 42 {
		t.Fatalf("userID = %d, mau 42", uid)
	}
}

// TestMFAPendingRejectsSessionToken memastikan token sesi biasa (Kind="")
// ditolak oleh ParseMFAPending.
func TestMFAPendingRejectsSessionToken(t *testing.T) {
	secret, _ := GenerateSecret()
	iss := NewIssuer(secret, time.Hour)
	sess, _ := iss.Issue(7, "member", 0)
	if _, err := iss.ParseMFAPending(sess); err == nil {
		t.Fatal("token sesi biasa harus ditolak ParseMFAPending")
	}
}

// TestSessionTokenHasNoKind memastikan Issue() menghasilkan Kind="" sehingga
// Parse mengembalikan klaim tanpa kind.
func TestSessionTokenHasNoKind(t *testing.T) {
	secret, _ := GenerateSecret()
	iss := NewIssuer(secret, time.Hour)
	sess, _ := iss.Issue(7, "member", 0)
	c, err := iss.Parse(sess)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if c.Kind != "" {
		t.Fatalf("token sesi Kind = %q, mau kosong", c.Kind)
	}
}

// TestMFAPendingExpires memastikan pending-token kedaluwarsa setelah 2 menit.
// jwt memvalidasi ExpiresAt terhadap waktu nyata, jadi kita terbitkan token
// dengan waktu 3 menit di masa lalu — token sudah kedaluwarsa saat diparse now.
func TestMFAPendingExpires(t *testing.T) {
	secret, _ := GenerateSecret()
	iss := NewIssuer(secret, time.Hour)
	iss.now = func() time.Time { return time.Now().Add(-3 * time.Minute) }
	tok, _ := iss.IssueMFAPending(9)
	// Reset now ke waktu nyata; token yang diterbitkan 3 menit lalu (TTL 2 menit)
	// harus sudah kedaluwarsa.
	iss.now = time.Now
	if _, err := iss.ParseMFAPending(tok); err == nil {
		t.Fatal("pending-token harus kedaluwarsa setelah 2 menit")
	}
}

// TestMFAPendingParsedBySessionParseHasKind memastikan pending-token yang
// diparse via Parse() (jalur sesi) tetap membawa Kind non-kosong sehingga
// resolveCredential bisa menolaknya.
func TestMFAPendingParsedBySessionParseHasKind(t *testing.T) {
	secret, _ := GenerateSecret()
	iss := NewIssuer(secret, time.Hour)
	tok, _ := iss.IssueMFAPending(3)
	c, err := iss.Parse(tok)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if c.Kind != "mfa_pending" {
		t.Fatalf("pending-token Kind = %q, mau mfa_pending", c.Kind)
	}
}
