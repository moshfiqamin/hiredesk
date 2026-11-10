package auth

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Test-only keys. Neither is used anywhere outside this file.
const (
	testSecret  = "test-only-signing-key-0123456789-abcdefghijklmnop"
	otherSecret = "a-different-signing-key-9876543210-zyxwvutsrqponm"
)

const (
	testUserID = "5b0f3a52-7c1e-4d0a-9a35-2f6c1f0e8d11"
	testRole   = "candidate"
)

// issuedAt is the fixed instant every test token is issued at. Whole seconds,
// because that is the precision a JWT timestamp carries.
var issuedAt = time.Date(2026, time.January, 15, 12, 0, 0, 0, time.UTC)

// newTestManager returns a manager whose clock reads issuedAt plus offset.
func newTestManager(secret string, offset time.Duration) *TokenManager {
	m := NewTokenManager(secret)
	m.now = func() time.Time { return issuedAt.Add(offset) }
	return m
}

// mustIssue issues an ordinary token at issuedAt with testSecret.
func mustIssue(t *testing.T) string {
	t.Helper()

	token, err := newTestManager(testSecret, 0).NewAccessToken(testUserID, testRole)
	if err != nil {
		t.Fatalf("NewAccessToken() unexpected error: %v", err)
	}
	return token
}

// mustSign builds a token outside TokenManager, the way an attacker would:
// any algorithm, any key, any claims.
func mustSign(t *testing.T, method jwt.SigningMethod, key any, claims jwt.Claims) string {
	t.Helper()

	token, err := jwt.NewWithClaims(method, claims).SignedString(key)
	if err != nil {
		t.Fatalf("sign %s token: %v", method.Alg(), err)
	}
	return token
}

// validClaims are claims that would be accepted if correctly signed: issued at
// issuedAt, expiring AccessTokenTTL later.
func validClaims() Claims {
	return Claims{
		Role: "admin",
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   testUserID,
			IssuedAt:  jwt.NewNumericDate(issuedAt),
			ExpiresAt: jwt.NewNumericDate(issuedAt.Add(AccessTokenTTL)),
			ID:        "forged",
		},
	}
}

// decodePayload reads the middle segment of a token using nothing but base64
// and JSON — no key, no jwt library.
func decodePayload(t *testing.T, token string) map[string]any {
	t.Helper()

	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("token has %d dot-separated parts, want 3", len(parts))
	}

	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("payload is not base64url: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("payload is not JSON: %v", err)
	}
	return payload
}

// withRole rewrites the role inside a token's payload and leaves the header
// and signature exactly as they were.
func withRole(t *testing.T, token, role string) string {
	t.Helper()

	payload := decodePayload(t, token)
	payload["role"] = role

	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("re-encode payload: %v", err)
	}

	parts := strings.Split(token, ".")
	parts[1] = base64.RawURLEncoding.EncodeToString(raw)
	return strings.Join(parts, ".")
}

func TestAccessTokenRoundTrip(t *testing.T) {
	t.Parallel()

	m := newTestManager(testSecret, 0)

	token, err := m.NewAccessToken(testUserID, testRole)
	if err != nil {
		t.Fatalf("NewAccessToken() unexpected error: %v", err)
	}

	got, err := m.ParseAccessToken(token)
	if err != nil {
		t.Fatalf("ParseAccessToken() unexpected error: %v", err)
	}

	if got.Subject != testUserID {
		t.Errorf("sub = %q, want %q", got.Subject, testUserID)
	}
	if got.Role != testRole {
		t.Errorf("role = %q, want %q", got.Role, testRole)
	}
	if got.IssuedAt == nil || !got.IssuedAt.Equal(issuedAt) {
		t.Errorf("iat = %v, want %v", got.IssuedAt, issuedAt)
	}
	if want := issuedAt.Add(15 * time.Minute); got.ExpiresAt == nil || !got.ExpiresAt.Equal(want) {
		t.Errorf("exp = %v, want %v", got.ExpiresAt, want)
	}
	if got.ID == "" {
		t.Errorf("jti is empty, want a random identifier")
	}

	// Same user, same role, same second: only the jti can differ.
	second, err := m.NewAccessToken(testUserID, testRole)
	if err != nil {
		t.Fatalf("NewAccessToken() unexpected error: %v", err)
	}
	secondClaims, err := m.ParseAccessToken(second)
	if err != nil {
		t.Fatalf("ParseAccessToken() unexpected error: %v", err)
	}
	if secondClaims.ID == got.ID {
		t.Errorf("two tokens share the jti %q", got.ID)
	}
}

func TestParseAccessToken(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string

		// token builds the string handed to ParseAccessToken.
		token func(t *testing.T) string

		// elapsed is how long after issuedAt the token is parsed.
		elapsed time.Duration

		// wantErr is nil when the token should be accepted.
		wantErr error
	}{
		{
			name:    "fresh token",
			token:   mustIssue,
			elapsed: 0,
		},
		{
			name:    "one second before expiry",
			token:   mustIssue,
			elapsed: AccessTokenTTL - time.Second,
		},
		{
			name:    "at the moment of expiry",
			token:   mustIssue,
			elapsed: AccessTokenTTL,
			wantErr: ErrExpiredToken,
		},
		{
			name:    "one second after expiry",
			token:   mustIssue,
			elapsed: AccessTokenTTL + time.Second,
			wantErr: ErrExpiredToken,
		},
		{
			name: "signed with a different secret",
			token: func(t *testing.T) string {
				token, err := newTestManager(otherSecret, 0).NewAccessToken(testUserID, testRole)
				if err != nil {
					t.Fatalf("NewAccessToken() unexpected error: %v", err)
				}
				return token
			},
			wantErr: ErrInvalidToken,
		},
		{
			// Expired and forged at once must report as forged: "expired" is a
			// statement about a token this server issued.
			name: "signed with a different secret and expired",
			token: func(t *testing.T) string {
				token, err := newTestManager(otherSecret, 0).NewAccessToken(testUserID, testRole)
				if err != nil {
					t.Fatalf("NewAccessToken() unexpected error: %v", err)
				}
				return token
			},
			elapsed: time.Hour,
			wantErr: ErrInvalidToken,
		},
		{
			name: "payload tampered: role raised to admin",
			token: func(t *testing.T) string {
				return withRole(t, mustIssue(t), "admin")
			},
			wantErr: ErrInvalidToken,
		},
		{
			name: "signature removed",
			token: func(t *testing.T) string {
				parts := strings.Split(mustIssue(t), ".")
				return parts[0] + "." + parts[1] + "."
			},
			wantErr: ErrInvalidToken,
		},
		{
			name: "alg none",
			token: func(t *testing.T) string {
				// The library refuses to produce an unsigned token unless
				// handed this exact constant, hence its name.
				return mustSign(t, jwt.SigningMethodNone, jwt.UnsafeAllowNoneSignatureType, validClaims())
			},
			wantErr: ErrInvalidToken,
		},
		{
			name: "alg HS384 with the correct secret",
			token: func(t *testing.T) string {
				return mustSign(t, jwt.SigningMethodHS384, []byte(testSecret), validClaims())
			},
			wantErr: ErrInvalidToken,
		},
		{
			name: "alg ES256 signed with the attacker's own key",
			token: func(t *testing.T) string {
				key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
				if err != nil {
					t.Fatalf("generate ECDSA key: %v", err)
				}
				return mustSign(t, jwt.SigningMethodES256, key, validClaims())
			},
			wantErr: ErrInvalidToken,
		},
		{
			name: "correctly signed but with no exp claim",
			token: func(t *testing.T) string {
				claims := validClaims()
				claims.ExpiresAt = nil
				return mustSign(t, jwt.SigningMethodHS256, []byte(testSecret), claims)
			},
			wantErr: ErrInvalidToken,
		},
		{
			name:    "empty string",
			token:   func(*testing.T) string { return "" },
			wantErr: ErrInvalidToken,
		},
		{
			name:    "not a token",
			token:   func(*testing.T) string { return "not.a.token" },
			wantErr: ErrInvalidToken,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			m := newTestManager(testSecret, tt.elapsed)

			claims, err := m.ParseAccessToken(tt.token(t))

			if tt.wantErr == nil {
				if err != nil {
					t.Fatalf("ParseAccessToken() unexpected error: %v", err)
				}
				if claims.Subject != testUserID {
					t.Errorf("sub = %q, want %q", claims.Subject, testUserID)
				}
				return
			}

			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("ParseAccessToken() error = %v, want %v", err, tt.wantErr)
			}
			if claims != nil {
				t.Errorf("ParseAccessToken() returned claims %+v alongside an error", claims)
			}
		})
	}
}

// Decodes a token with the standard library alone, to show that the payload
// is readable by anyone who holds the token.
func TestPayloadIsReadableWithoutTheSecret(t *testing.T) {
	t.Parallel()

	payload := decodePayload(t, mustIssue(t))

	if payload["sub"] != testUserID {
		t.Errorf("sub = %v, want %q", payload["sub"], testUserID)
	}
	if payload["role"] != testRole {
		t.Errorf("role = %v, want %q", payload["role"], testRole)
	}

	want := []string{"sub", "role", "iat", "exp", "jti"}
	for _, claim := range want {
		if _, ok := payload[claim]; !ok {
			t.Errorf("payload has no %q claim", claim)
		}
	}
	if len(payload) != len(want) {
		t.Errorf("payload has %d claims %v, want exactly %v", len(payload), payload, want)
	}
}

func TestNewAccessTokenRejectsEmptyInput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		userID string
		role   string
	}{
		{name: "empty user ID", userID: "", role: testRole},
		{name: "empty role", userID: testUserID, role: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			token, err := newTestManager(testSecret, 0).NewAccessToken(tt.userID, tt.role)
			if err == nil {
				t.Fatalf("NewAccessToken() = %q, want an error", token)
			}
			if token != "" {
				t.Errorf("NewAccessToken() returned %q alongside an error", token)
			}
		})
	}
}
