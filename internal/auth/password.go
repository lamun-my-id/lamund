// Package auth menangani hashing password (argon2id) dan token JWT lamund.
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Parameter argon2id — konservatif untuk VPS kecil (ARM 1 OCPU Batam).
const (
	argonTime    = 1
	argonMemory  = 64 * 1024 // 64 MiB
	argonThreads = 2
	argonKeyLen  = 32
	argonSaltLen = 16
)

// ErrMismatch dikembalikan saat password tidak cocok dengan hash.
var ErrMismatch = errors.New("password salah")

// HashPassword menghasilkan hash terenkode PHC:
// $argon2id$v=19$m=65536,t=1,p=2$<salt-b64>$<hash-b64>
func HashPassword(password string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	hash := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonTime, argonThreads,
		b64(salt), b64(hash)), nil
}

// VerifyPassword membandingkan password dengan hash PHC secara constant-time.
func VerifyPassword(password, encoded string) error {
	p, salt, hash, err := decodeHash(encoded)
	if err != nil {
		return err
	}
	other := argon2.IDKey([]byte(password), salt, p.time, p.memory, p.threads, uint32(len(hash)))
	if subtle.ConstantTimeCompare(hash, other) == 1 {
		return nil
	}
	return ErrMismatch
}

type argonParams struct {
	memory  uint32
	time    uint32
	threads uint8
}

func decodeHash(encoded string) (argonParams, []byte, []byte, error) {
	var p argonParams
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return p, nil, nil, errors.New("format hash tidak valid")
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return p, nil, nil, err
	}
	if version != argon2.Version {
		return p, nil, nil, fmt.Errorf("versi argon2 tidak cocok: %d", version)
	}
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &p.memory, &p.time, &p.threads); err != nil {
		return p, nil, nil, err
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return p, nil, nil, err
	}
	hash, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return p, nil, nil, err
	}
	return p, salt, hash, nil
}

func b64(b []byte) string { return base64.RawStdEncoding.EncodeToString(b) }
