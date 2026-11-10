package domain

import (
	"errors"
	"time"
)

var (
	// ErrUnauthenticated means the request did not prove who is making it: no
	// credential, or one that is unknown, revoked or otherwise unusable.
	ErrUnauthenticated = errors.New("authentication required")

	// ErrTokenExpired means the credential was genuine and is past its expiry.
	ErrTokenExpired = errors.New("token has expired")
)

// RefreshToken is one row of the refresh_tokens table.
//
// It has no field for the token itself. The database holds only a hash, and
// nothing that reads a row back has any use for that either.
type RefreshToken struct {
	ID        string
	UserID    string
	ExpiresAt time.Time

	// RevokedAt is nil while the token is still usable. A pointer because the
	// column is nullable, and a time.Time has no value that means "never".
	RevokedAt *time.Time

	CreatedAt time.Time
}

// TokenReuseError reports that a refresh token was presented after it had
// already been revoked. A legitimate client never does that: it discards a
// token the moment it is exchanged. So either the token was stolen, or the
// thief used it first and this is the real owner arriving second.
type TokenReuseError struct {
	// UserID is the owner of the token, whose every session must now end.
	UserID string
}

// Error deliberately omits the token and its hash.
func (e *TokenReuseError) Error() string {
	return "refresh token reused for user " + e.UserID
}
