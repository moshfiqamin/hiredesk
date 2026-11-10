package auth

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"strings"
	"testing"
)

func TestNewRefreshToken(t *testing.T) {
	t.Parallel()

	token, hash, err := NewRefreshToken()
	if err != nil {
		t.Fatalf("NewRefreshToken() unexpected error: %v", err)
	}

	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		t.Fatalf("token %q is not unpadded base64url: %v", token, err)
	}
	if len(raw) != 32 {
		t.Errorf("token decodes to %d bytes, want 32", len(raw))
	}
	if len(token) != 43 {
		t.Errorf("len(token) = %d, want 43", len(token))
	}

	// Computed here independently of the code under test.
	sum := sha256.Sum256(raw)
	if want := hex.EncodeToString(sum[:]); hash != want {
		t.Errorf("hash = %q, want SHA-256 of the raw bytes, %q", hash, want)
	}
	if len(hash) != 64 {
		t.Errorf("len(hash) = %d, want 64 hex characters", len(hash))
	}
	if strings.Contains(hash, token) || strings.Contains(token, hash) {
		t.Errorf("the token and its hash overlap")
	}

	// What a presented token is looked up by must be what was stored.
	got, ok := HashRefreshToken(token)
	if !ok || got != hash {
		t.Errorf("HashRefreshToken(token) = %q, %v; want %q, true", got, ok, hash)
	}
}

func TestNewRefreshTokenIsDifferentEveryTime(t *testing.T) {
	t.Parallel()

	// A generator that repeated within a thousand draws would be broken
	// beyond any doubt: for 256 random bits the chance is about 1 in 10^71.
	const draws = 1000

	tokens := make(map[string]bool, draws)
	hashes := make(map[string]bool, draws)

	for range draws {
		token, hash, err := NewRefreshToken()
		if err != nil {
			t.Fatalf("NewRefreshToken() unexpected error: %v", err)
		}
		if tokens[token] {
			t.Fatalf("token %q was generated twice", token)
		}
		if hashes[hash] {
			t.Fatalf("hash %q was generated twice", hash)
		}
		tokens[token], hashes[hash] = true, true
	}
}

func TestHashRefreshToken(t *testing.T) {
	t.Parallel()

	// 32 zero bytes, and the published SHA-256 of 32 zero bytes.
	const zeroToken = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	const zeroHash = "66687aadf862bd776c8fc18b8e9f8e20089714856ee233b3902a591d0d5f2925"

	tests := []struct {
		name     string
		token    string
		wantHash string
		wantOK   bool
	}{
		{name: "known vector", token: zeroToken, wantHash: zeroHash, wantOK: true},
		{name: "empty", token: ""},
		{name: "not base64", token: "not a token!"},
		{name: "one byte short", token: base64.RawURLEncoding.EncodeToString(make([]byte, 31))},
		{name: "one byte long", token: base64.RawURLEncoding.EncodeToString(make([]byte, 33))},
		{name: "padded base64", token: zeroToken + "="},
		{name: "standard alphabet instead of url-safe", token: strings.Repeat("+", 43)},
		{name: "a JWT", token: "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxIn0.c2ln"},
		{name: "megabyte of input", token: strings.Repeat("A", 1<<20)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			hash, ok := HashRefreshToken(tt.token)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if hash != tt.wantHash {
				t.Errorf("hash = %q, want %q", hash, tt.wantHash)
			}
		})
	}
}
