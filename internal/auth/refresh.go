package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"time"
)

// RefreshTokenTTL is how long a refresh token can be exchanged for a new pair
// if it is never used. Each use replaces it, so an active session lasts
// indefinitely and an abandoned one ends after this long.
const RefreshTokenTTL = 30 * 24 * time.Hour

// refreshTokenBytes is the size of the random value behind a refresh token.
// 32 bytes is 256 bits: guessing one is as hard as guessing an AES-256 key.
const refreshTokenBytes = 32

// NewRefreshToken returns a new random refresh token and its hash.
//
// The token goes to the client and is never stored. The hash goes to the
// database and is useless to anyone who reads it: it cannot be turned back
// into the token.
func NewRefreshToken() (token, hash string, err error) {
	raw := make([]byte, refreshTokenBytes)

	// crypto/rand reads from the operating system's cryptographic generator.
	// math/rand must never be used here: its output can be predicted from a
	// handful of earlier values. Since Go 1.24 this call cannot fail, but its
	// signature still returns an error, so it is still checked.
	if _, err := rand.Read(raw); err != nil {
		return "", "", fmt.Errorf("generate refresh token: %w", err)
	}

	// base64url without padding: 43 characters that are safe in a JSON
	// string, a header or a cookie with no escaping.
	return base64.RawURLEncoding.EncodeToString(raw), hashRefreshBytes(raw), nil
}

// HashRefreshToken returns the hash under which a presented token would be
// stored. ok is false if token cannot be one this server issued, in which
// case there is nothing to look up.
func HashRefreshToken(token string) (hash string, ok bool) {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(raw) != refreshTokenBytes {
		return "", false
	}
	return hashRefreshBytes(raw), true
}

// hashRefreshBytes is SHA-256 in lower-case hex: 64 characters.
//
// A fast, unsalted hash is correct here although it would be wrong for a
// password. The input is 256 bits of randomness, so there is no dictionary to
// try and nothing for a slow hash to slow down.
func hashRefreshBytes(raw []byte) string {
	sum := sha256.Sum256(raw)

	// sum is an array, [32]byte. sum[:] is a slice over it, which is the type
	// EncodeToString takes.
	return hex.EncodeToString(sum[:])
}
