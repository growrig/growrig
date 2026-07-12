package domain

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
)

// Password hashing uses PBKDF2-HMAC-SHA256 from the Go standard library, so
// Grow Core keeps its pure-Go, no-CGO, minimal-dependency build. A random
// per-user salt defends against precomputation; the iteration count is a
// deliberate work factor.
const (
	pbkdf2Iterations = 600_000
	pbkdf2KeyLen     = 32
	pbkdf2SaltLen    = 16
)

// HashPassword derives a hex-encoded hash and salt for a plaintext password.
func HashPassword(password string) (hash, salt string, err error) {
	saltBytes := make([]byte, pbkdf2SaltLen)
	if _, err := rand.Read(saltBytes); err != nil {
		return "", "", err
	}
	key, err := pbkdf2.Key(sha256.New, password, saltBytes, pbkdf2Iterations, pbkdf2KeyLen)
	if err != nil {
		return "", "", err
	}
	return hex.EncodeToString(key), hex.EncodeToString(saltBytes), nil
}

// VerifyPassword reports whether password matches the stored hash/salt, using a
// constant-time comparison.
func VerifyPassword(password, hash, salt string) bool {
	saltBytes, err := hex.DecodeString(salt)
	if err != nil {
		return false
	}
	want, err := hex.DecodeString(hash)
	if err != nil {
		return false
	}
	got, err := pbkdf2.Key(sha256.New, password, saltBytes, pbkdf2Iterations, pbkdf2KeyLen)
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(got, want) == 1
}
