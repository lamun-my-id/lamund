package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Claims adalah isi token akses panel lamund.
type Claims struct {
	UserID int64  `json:"uid"`
	Role   string `json:"role"`
	Ver    int64  `json:"ver,omitempty"` // token_version saat terbit; 0 = legacy (cocok dengan token_version=0)
	// Kind membedakan jenis token. Kosong = token sesi biasa. "mfa_pending" =
	// token sementara langkah-2 login MFA yang TIDAK boleh dipakai sebagai sesi.
	Kind string `json:"knd,omitempty"`
	jwt.RegisteredClaims
}

// Issuer menandatangani & memverifikasi JWT HS256 dengan secret server.
type Issuer struct {
	secret []byte
	ttl    time.Duration
	now    func() time.Time // dapat di-override untuk uji
}

// NewIssuer membuat issuer. ttl=0 memakai default 24 jam.
func NewIssuer(secret []byte, ttl time.Duration) *Issuer {
	if ttl == 0 {
		ttl = 24 * time.Hour
	}
	return &Issuer{secret: secret, ttl: ttl, now: time.Now}
}

// Issue menandatangani token akses untuk user.
// tokenVersion harus diambil dari User.TokenVersion saat login — disematkan
// sebagai klaim "ver" sehingga middleware bisa menolak token yang sudah
// dibatalkan (ver < token_version di DB).
func (i *Issuer) Issue(userID int64, role string, tokenVersion int64) (string, error) {
	now := i.now()
	claims := Claims{
		UserID: userID,
		Role:   role,
		Ver:    tokenVersion,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(i.ttl)),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(i.secret)
}

// mfaPendingKind adalah nilai Claims.Kind untuk pending-token MFA.
const mfaPendingKind = "mfa_pending"

// IssueMFAPending menerbitkan token sementara untuk langkah-2 login MFA.
// TTL singkat (2 menit, sengaja TIDAK memakai i.ttl) dan Kind="mfa_pending"
// sehingga token ini ditolak sebagai kredensial sesi biasa.
func (i *Issuer) IssueMFAPending(userID int64) (string, error) {
	now := i.now()
	claims := Claims{
		UserID: userID,
		Kind:   mfaPendingKind,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(2 * time.Minute)),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(i.secret)
}

// ParseMFAPending memvalidasi pending-token MFA dan mengembalikan userID.
// Hanya valid bila Kind=="mfa_pending" dan belum kedaluwarsa. Token sesi biasa
// (Kind="") ditolak.
func (i *Issuer) ParseMFAPending(token string) (int64, error) {
	c, err := i.Parse(token)
	if err != nil {
		return 0, err
	}
	if c.Kind != mfaPendingKind {
		return 0, errors.New("bukan token mfa_pending")
	}
	return c.UserID, nil
}

// Parse memvalidasi token dan mengembalikan klaimnya.
func (i *Issuer) Parse(token string) (*Claims, error) {
	var c Claims
	_, err := jwt.ParseWithClaims(token, &c, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("metode tanda tangan tak terduga")
		}
		return i.secret, nil
	})
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// GenerateSecret menghasilkan secret acak 32 byte (untuk bootstrap awal).
func GenerateSecret() ([]byte, error) {
	b := make([]byte, 32)
	_, err := rand.Read(b)
	return b, err
}

// GenerateAPIKey membuat API key baru. Mengembalikan plaintext (ditampilkan
// SEKALI ke user) dan hash SHA-256 (yang disimpan). Key berentropi tinggi,
// jadi SHA-256 cukup — tak perlu argon2.
func GenerateAPIKey() (plaintext, hash string, err error) {
	b := make([]byte, 24)
	if _, err = rand.Read(b); err != nil {
		return "", "", err
	}
	plaintext = "lmd_" + base64.RawURLEncoding.EncodeToString(b)
	hash = HashAPIKey(plaintext)
	return plaintext, hash, nil
}

// HashAPIKey memberi hash SHA-256 hex dari API key (untuk lookup di DB).
func HashAPIKey(plaintext string) string {
	sum := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(sum[:])
}
