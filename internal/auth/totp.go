package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// base32Alphabet adalah alfabet base32 standar RFC 4648 (uppercase A-Z, 2-7).
// Untuk recovery codes dikonversi ke lowercase.
var recoveryAlphabet = "abcdefghijklmnopqrstuvwxyz234567"

// GenerateTOTPSecret menghasilkan secret TOTP: 20 byte acak dikodekan base32
// StdEncoding tanpa padding (uppercase), sesuai standar otpauth.
func GenerateTOTPSecret() (string, error) {
	b := make([]byte, 20)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b), nil
}

// TOTPURI menghasilkan URI otpauth://totp/<issuer>:<account>?...
// yang dapat dikodekan sebagai QR untuk dibaca aplikasi authenticator.
func TOTPURI(secret, account, issuer string) string {
	// Label: "issuer:account" — di-URL-encode sesuai standar otpauth.
	label := url.PathEscape(issuer + ":" + account)
	q := url.Values{}
	q.Set("secret", secret)
	q.Set("issuer", issuer)
	q.Set("algorithm", "SHA1")
	q.Set("digits", "6")
	q.Set("period", "30")
	return "otpauth://totp/" + label + "?" + q.Encode()
}

// TOTPCodeAt menghitung kode TOTP 6-digit untuk waktu t menggunakan HMAC-SHA1
// dengan step 30 detik, sesuai RFC 6238.
func TOTPCodeAt(secret string, t time.Time) (string, error) {
	key, err := decodeBase32Secret(secret)
	if err != nil {
		return "", fmt.Errorf("totp: decode secret: %w", err)
	}

	// T = floor(unix / 30)
	step := uint64(t.Unix() / 30) //nolint:gosec // nilai selalu positif untuk waktu valid

	// Pesan: T dalam 8 byte big-endian.
	msg := make([]byte, 8)
	binary.BigEndian.PutUint64(msg, step)

	// HMAC-SHA1(key, msg)
	mac := hmac.New(sha1.New, key)
	mac.Write(msg)
	h := mac.Sum(nil) // 20 byte

	// Dynamic truncation (RFC 4226 §5.4).
	offset := h[len(h)-1] & 0x0f
	binCode := (uint32(h[offset]&0x7f) << 24) |
		(uint32(h[offset+1]) << 16) |
		(uint32(h[offset+2]) << 8) |
		uint32(h[offset+3])

	code := binCode % 1_000_000
	return fmt.Sprintf("%06d", code), nil
}

// VerifyTOTP memverifikasi kode TOTP untuk waktu t dengan window ±1 step
// (toleransi clock skew 30 detik). Perbandingan dilakukan constant-time.
// Mengembalikan (step, true) bila kode valid, atau (0, false) bila tidak.
// step adalah nilai unix/30 dari waktu yang cocok, berguna untuk anti-replay.
//
// Kode yang bukan tepat 6 digit ASCII langsung ditolak.
func VerifyTOTP(secret, code string, t time.Time) (step int64, ok bool) {
	// Tolak kode non-6-digit ASCII.
	if len(code) != 6 {
		return 0, false
	}
	for _, c := range code {
		if c < '0' || c > '9' {
			return 0, false
		}
	}

	codeBytes := []byte(code)

	// Periksa window: t-30s, t, t+30s.
	offsets := []int64{-30, 0, 30}
	for _, off := range offsets {
		candidate := t.Add(time.Duration(off) * time.Second)
		got, err := TOTPCodeAt(secret, candidate)
		if err != nil {
			return 0, false
		}
		if subtle.ConstantTimeCompare(codeBytes, []byte(got)) == 1 {
			s := candidate.Unix() / 30
			return s, true
		}
	}
	return 0, false
}

// GenerateRecoveryCodes menghasilkan n kode pemulihan acak dari crypto/rand.
// Format setiap kode: "xxxxx-xxxxx" (dua grup 5 karakter, alfabet base32 lowercase).
func GenerateRecoveryCodes(n int) ([]string, error) {
	// Setiap karakter base32 mewakili 5 bit. Untuk 10 karakter kita butuh 50 bit = 7 byte.
	// Kita menghasilkan 10 karakter per kode (dua grup 5) via pendekatan indeks byte.
	codes := make([]string, n)
	for i := range codes {
		// 10 karakter masing-masing dari alfabet 32 simbol.
		// Kita baca 10 byte acak, masing-masing di-mod 32 → karakter.
		// Entropi: 10 × 5 = 50 bit — cukup untuk recovery codes.
		b := make([]byte, 10)
		if _, err := rand.Read(b); err != nil {
			return nil, err
		}
		var sb strings.Builder
		sb.Grow(11)
		for j, byt := range b {
			if j == 5 {
				sb.WriteByte('-')
			}
			sb.WriteByte(recoveryAlphabet[int(byt)%32])
		}
		codes[i] = sb.String()
	}
	return codes, nil
}

// decodeBase32Secret mendekode secret base32 (case-insensitive, padding opsional).
func decodeBase32Secret(secret string) ([]byte, error) {
	// Normalkan ke uppercase dan hapus spasi/tanda hubung yang mungkin ada.
	secret = strings.ToUpper(strings.ReplaceAll(strings.ReplaceAll(secret, " ", ""), "-", ""))

	// Tambahkan padding '=' agar panjang kelipatan 8.
	if pad := len(secret) % 8; pad != 0 {
		secret += strings.Repeat("=", 8-pad)
	}
	return base32.StdEncoding.DecodeString(secret)
}
