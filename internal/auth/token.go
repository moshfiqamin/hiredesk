package auth

import (
	"crypto/rand"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// AccessTokenTTL is how long an access token is valid after it is issued.
//
// Short on purpose: a signed token cannot be recalled, so its lifetime is the
// longest a stolen one stays useful and the longest a role change takes to
// reach a client that is already logged in.
const AccessTokenTTL = 15 * time.Minute

var (
	// ErrInvalidToken is returned by ParseAccessToken for a token that cannot
	// be trusted: malformed, wrongly signed, altered, or using an algorithm
	// other than HS256.
	ErrInvalidToken = errors.New("auth: invalid token")

	// ErrExpiredToken is returned by ParseAccessToken for a token that was
	// genuinely issued by this server and has passed its expiry. It is distinct
	// from ErrInvalidToken so a caller can tell the client to refresh rather
	// than to log in again.
	ErrExpiredToken = errors.New("auth: token has expired")
)

// Claims is the payload of an access token.
//
// The payload is signed, not encrypted: anyone holding the token can decode
// and read every field. Nothing secret belongs here.
type Claims struct {
	// Role is the user's role when the token was issued. It is not re-read
	// from the database on each request, so it can be up to AccessTokenTTL out
	// of date.
	Role string `json:"role"`

	// Embedded, so the standard fields (Subject, ExpiresAt, IssuedAt, ID) and
	// the methods the jwt library requires of a claims type are promoted onto
	// Claims itself.
	jwt.RegisteredClaims
}

// TokenManager issues and verifies access tokens with one HMAC secret.
type TokenManager struct {
	// A byte slice because that is the only key type the library's HMAC
	// methods accept. Passing the secret as a string fails at signing time.
	secret []byte

	// now is the clock. A field rather than a direct call to time.Now so the
	// tests can issue a token "in the past" without sleeping.
	now func() time.Time

	parser *jwt.Parser
}

// NewTokenManager returns a TokenManager that signs and verifies with secret.
//
// The secret's strength is not checked here. That is config.Load's job, which
// refuses to start the process with one shorter than 32 bytes.
func NewTokenManager(secret string) *TokenManager {
	m := &TokenManager{
		secret: []byte(secret),
		now:    time.Now,
	}

	m.parser = jwt.NewParser(
		// A closure over m rather than m.now itself, so the parser follows the
		// field if a test replaces it after construction.
		jwt.WithTimeFunc(func() time.Time { return m.now() }),

		// Without this the library accepts a token that has no exp claim at
		// all, and such a token would never expire.
		jwt.WithExpirationRequired(),
	)

	return m
}

// NewAccessToken issues a signed token for the user, valid for AccessTokenTTL.
//
// It carries sub (the user ID), role, iat, exp, and a random jti that makes
// every token unique even when two are issued for one user in the same second.
func (m *TokenManager) NewAccessToken(userID, role string) (string, error) {
	// A token with no subject or no role would pass every signature check and
	// then identify nobody.
	if userID == "" {
		return "", errors.New("auth: cannot issue a token for an empty user ID")
	}
	if role == "" {
		return "", errors.New("auth: cannot issue a token for an empty role")
	}

	now := m.now()

	claims := Claims{
		Role: role,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(AccessTokenTTL)),
			// 26 characters carrying 128 bits from the operating system's
			// random source. Unpredictable, so it can later serve as the key
			// for revoking one specific token.
			ID: rand.Text(),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	signed, err := token.SignedString(m.secret)
	if err != nil {
		return "", fmt.Errorf("sign access token: %w", err)
	}

	return signed, nil
}

// ParseAccessToken verifies tokenString and returns its claims.
//
// The claims are returned only if the token is signed with this manager's
// secret using HS256 and has not expired. Every failure is ErrExpiredToken or
// ErrInvalidToken, wrapping the library's own error for logging; neither
// should be shown to the client in detail.
func (m *TokenManager) ParseAccessToken(tokenString string) (*Claims, error) {
	claims := &Claims{}

	_, err := m.parser.ParseWithClaims(tokenString, claims, m.keyFor)
	if err != nil {
		// The library validates claims only after the signature has been
		// verified, so "expired" here always describes a token this server
		// really issued, never a forgery with an old date.
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, fmt.Errorf("%w: %w", ErrExpiredToken, err)
		}
		return nil, fmt.Errorf("%w: %w", ErrInvalidToken, err)
	}

	return claims, nil
}

// keyFor is called by the parser after it has decoded the header and before it
// verifies the signature, to ask which key to verify with.
//
// The header is written by whoever made the token, so at this point alg is
// attacker-controlled input. Returning the secret without checking it would let
// the token choose its own verification algorithm — including "none".
func (m *TokenManager) keyFor(token *jwt.Token) (any, error) {
	// Compared with the exact algorithm, not asserted to the *SigningMethodHMAC
	// type: that type also covers HS384 and HS512, which this server never
	// issues.
	if token.Method.Alg() != jwt.SigningMethodHS256.Alg() {
		return nil, fmt.Errorf("unexpected signing method %q", token.Method.Alg())
	}

	return m.secret, nil
}
