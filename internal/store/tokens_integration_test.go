//go:build integration

package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/moshfiqamin/hiredesk/internal/domain"
)

// newTokenFixture returns a token store and the ID of a freshly created user
// to own tokens. The user is deleted when the test ends, and ON DELETE CASCADE
// takes their tokens with them.
func newTokenFixture(t *testing.T) (*TokenStore, string) {
	t.Helper()

	users := newTestStore(t)

	user, err := users.CreateUser(context.Background(), newCandidate(t, users))
	if err != nil {
		t.Fatalf("CreateUser() unexpected error: %v", err)
	}

	return NewTokenStore(users.db), user.ID
}

// randomHash returns a value of the shape a real token hash has: 64 hex
// characters that no other test will produce.
func randomHash() string {
	sum := sha256.Sum256([]byte(rand.Text()))
	return hex.EncodeToString(sum[:])
}

// mustCreate stores a token expiring in an hour and returns its hash.
func mustCreate(t *testing.T, s *TokenStore, userID string) string {
	t.Helper()

	hash := randomHash()
	if err := s.CreateRefreshToken(context.Background(), userID, hash, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("CreateRefreshToken() unexpected error: %v", err)
	}
	return hash
}

// mustGet reads a token that is expected to exist.
func mustGet(t *testing.T, s *TokenStore, hash string) *domain.RefreshToken {
	t.Helper()

	token, err := s.GetRefreshTokenByHash(context.Background(), hash)
	if err != nil {
		t.Fatalf("GetRefreshTokenByHash() unexpected error: %v", err)
	}
	return token
}

// liveCount is how many of the user's tokens are not revoked.
func liveCount(t *testing.T, s *TokenStore, userID string) int {
	t.Helper()

	var count int
	err := s.db.QueryRow(context.Background(),
		`SELECT count(*) FROM refresh_tokens WHERE user_id = $1::text::uuid AND revoked_at IS NULL`, userID).Scan(&count)
	if err != nil {
		t.Fatalf("count live tokens: %v", err)
	}
	return count
}

func TestTokenStoreCreateAndGet(t *testing.T) {
	s, userID := newTokenFixture(t)

	hash := randomHash()
	expiresAt := time.Now().Add(30 * 24 * time.Hour)

	if err := s.CreateRefreshToken(context.Background(), userID, hash, expiresAt); err != nil {
		t.Fatalf("CreateRefreshToken() unexpected error: %v", err)
	}

	got := mustGet(t, s, hash)

	if got.UserID != userID {
		t.Errorf("UserID = %q, want %q", got.UserID, userID)
	}
	if got.RevokedAt != nil {
		t.Errorf("RevokedAt = %v, want nil for a new token", got.RevokedAt)
	}
	// Postgres keeps microseconds; Go's clock has nanoseconds.
	if diff := got.ExpiresAt.Sub(expiresAt); diff > time.Millisecond || diff < -time.Millisecond {
		t.Errorf("ExpiresAt = %v, want %v", got.ExpiresAt, expiresAt)
	}
	if len(got.ID) != 36 {
		t.Errorf("ID = %q, want a UUID", got.ID)
	}

	// The same hash twice is a unique violation, not a second row.
	err := s.CreateRefreshToken(context.Background(), userID, hash, expiresAt)
	if !errors.Is(err, domain.ErrDuplicate) {
		t.Errorf("second CreateRefreshToken() error = %v, want ErrDuplicate", err)
	}

	if _, err := s.GetRefreshTokenByHash(context.Background(), randomHash()); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("GetRefreshTokenByHash(unknown) error = %v, want ErrNotFound", err)
	}
}

func TestTokenStoreRevokeOne(t *testing.T) {
	s, userID := newTokenFixture(t)
	ctx := context.Background()

	target := mustCreate(t, s, userID)
	other := mustCreate(t, s, userID)

	if err := s.RevokeRefreshToken(ctx, target); err != nil {
		t.Fatalf("RevokeRefreshToken() unexpected error: %v", err)
	}

	first := mustGet(t, s, target).RevokedAt
	if first == nil {
		t.Fatalf("RevokedAt is nil after revoking")
	}
	if mustGet(t, s, other).RevokedAt != nil {
		t.Errorf("revoking one token revoked another")
	}

	// A second revocation must leave the original timestamp alone.
	if err := s.RevokeRefreshToken(ctx, target); err != nil {
		t.Fatalf("second RevokeRefreshToken() unexpected error: %v", err)
	}
	if second := mustGet(t, s, target).RevokedAt; !second.Equal(*first) {
		t.Errorf("RevokedAt moved from %v to %v on a second revocation", first, second)
	}

	if err := s.RevokeRefreshToken(ctx, randomHash()); err != nil {
		t.Errorf("RevokeRefreshToken(unknown) error = %v, want nil", err)
	}
}

func TestTokenStoreRevokeAllForUser(t *testing.T) {
	s, alice := newTokenFixture(t)
	_, bob := newTokenFixture(t)
	ctx := context.Background()

	mustCreate(t, s, alice)
	mustCreate(t, s, alice)
	already := mustCreate(t, s, alice)
	bobsToken := mustCreate(t, s, bob)

	if err := s.RevokeRefreshToken(ctx, already); err != nil {
		t.Fatalf("RevokeRefreshToken() unexpected error: %v", err)
	}

	revoked, err := s.RevokeAllRefreshTokensForUser(ctx, alice)
	if err != nil {
		t.Fatalf("RevokeAllRefreshTokensForUser() unexpected error: %v", err)
	}
	// Three tokens, one of them revoked beforehand.
	if revoked != 2 {
		t.Errorf("revoked = %d, want 2", revoked)
	}
	if live := liveCount(t, s, alice); live != 0 {
		t.Errorf("alice has %d live tokens, want 0", live)
	}
	if mustGet(t, s, bobsToken).RevokedAt != nil {
		t.Errorf("revoking alice's tokens revoked bob's")
	}

	again, err := s.RevokeAllRefreshTokensForUser(ctx, alice)
	if err != nil || again != 0 {
		t.Errorf("second call = %d, %v; want 0, nil", again, err)
	}
}

func TestTokenStoreRotate(t *testing.T) {
	s, userID := newTokenFixture(t)
	ctx := context.Background()

	oldHash := mustCreate(t, s, userID)
	newHash := randomHash()
	newExpiry := time.Now().Add(30 * 24 * time.Hour)

	got, err := s.RotateRefreshToken(ctx, oldHash, newHash, newExpiry)
	if err != nil {
		t.Fatalf("RotateRefreshToken() unexpected error: %v", err)
	}
	if got != userID {
		t.Errorf("user = %q, want %q", got, userID)
	}

	if mustGet(t, s, oldHash).RevokedAt == nil {
		t.Errorf("the old token is still usable after rotation")
	}

	replacement := mustGet(t, s, newHash)
	if replacement.RevokedAt != nil {
		t.Errorf("the new token was stored already revoked")
	}
	if replacement.UserID != userID {
		t.Errorf("the new token belongs to %q, want %q", replacement.UserID, userID)
	}
	if live := liveCount(t, s, userID); live != 1 {
		t.Errorf("live tokens = %d, want exactly 1", live)
	}
}

func TestTokenStoreRotateRefusals(t *testing.T) {
	s, userID := newTokenFixture(t)
	ctx := context.Background()

	revoked := mustCreate(t, s, userID)
	if err := s.RevokeRefreshToken(ctx, revoked); err != nil {
		t.Fatalf("RevokeRefreshToken() unexpected error: %v", err)
	}

	expired := randomHash()
	if err := s.CreateRefreshToken(ctx, userID, expired, time.Now().Add(-time.Second)); err != nil {
		t.Fatalf("CreateRefreshToken() unexpected error: %v", err)
	}

	// Both at once must report as reuse: a replay is a replay however old.
	revokedAndExpired := randomHash()
	if err := s.CreateRefreshToken(ctx, userID, revokedAndExpired, time.Now().Add(-time.Second)); err != nil {
		t.Fatalf("CreateRefreshToken() unexpected error: %v", err)
	}
	if err := s.RevokeRefreshToken(ctx, revokedAndExpired); err != nil {
		t.Fatalf("RevokeRefreshToken() unexpected error: %v", err)
	}

	tests := []struct {
		name      string
		oldHash   string
		wantReuse bool
		wantIs    error
	}{
		{name: "unknown token", oldHash: randomHash(), wantIs: domain.ErrNotFound},
		{name: "expired token", oldHash: expired, wantIs: domain.ErrTokenExpired},
		{name: "revoked token", oldHash: revoked, wantReuse: true},
		{name: "revoked and expired token", oldHash: revokedAndExpired, wantReuse: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			newHash := randomHash()

			got, err := s.RotateRefreshToken(ctx, tt.oldHash, newHash, time.Now().Add(time.Hour))
			if err == nil {
				t.Fatalf("RotateRefreshToken() succeeded and returned user %q", got)
			}
			if got != "" {
				t.Errorf("returned user %q alongside an error", got)
			}

			var reuse *domain.TokenReuseError
			if isReuse := errors.As(err, &reuse); isReuse != tt.wantReuse {
				t.Fatalf("error = %v; is a TokenReuseError = %v, want %v", err, isReuse, tt.wantReuse)
			}
			if tt.wantReuse && reuse.UserID != userID {
				t.Errorf("TokenReuseError.UserID = %q, want %q", reuse.UserID, userID)
			}
			if tt.wantIs != nil && !errors.Is(err, tt.wantIs) {
				t.Errorf("error = %v, want %v", err, tt.wantIs)
			}

			// A refusal must not leave a new token behind.
			if _, err := s.GetRefreshTokenByHash(ctx, newHash); !errors.Is(err, domain.ErrNotFound) {
				t.Errorf("a new token was stored although rotation was refused: %v", err)
			}
		})
	}
}

// If storing the new token fails, the old one must not have been revoked:
// the user would be left with no usable token at all.
func TestTokenStoreRotateIsAllOrNothing(t *testing.T) {
	s, userID := newTokenFixture(t)
	ctx := context.Background()

	oldHash := mustCreate(t, s, userID)
	taken := mustCreate(t, s, userID)

	// Reusing an existing hash makes the INSERT fail after the UPDATE has
	// already run inside the transaction.
	_, err := s.RotateRefreshToken(ctx, oldHash, taken, time.Now().Add(time.Hour))
	if !errors.Is(err, domain.ErrDuplicate) {
		t.Fatalf("RotateRefreshToken() error = %v, want ErrDuplicate", err)
	}

	if mustGet(t, s, oldHash).RevokedAt != nil {
		t.Errorf("the old token was revoked although the exchange failed: the UPDATE was not rolled back")
	}
}

// The race the row lock exists for: many requests present the same token at
// the same instant. Exactly one may win.
func TestTokenStoreRotateConcurrently(t *testing.T) {
	s, userID := newTokenFixture(t)
	ctx := context.Background()

	oldHash := mustCreate(t, s, userID)

	const attempts = 20

	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		wins    int
		reuses  int
		others  []error
		release = make(chan struct{})
	)

	for range attempts {
		wg.Add(1)
		go func() {
			defer wg.Done()

			// Every goroutine waits here and is let go at once, so the
			// requests really do overlap.
			<-release

			_, err := s.RotateRefreshToken(ctx, oldHash, randomHash(), time.Now().Add(time.Hour))

			mu.Lock()
			defer mu.Unlock()

			var reuse *domain.TokenReuseError
			switch {
			case err == nil:
				wins++
			case errors.As(err, &reuse):
				reuses++
			default:
				others = append(others, err)
			}
		}()
	}

	close(release)
	wg.Wait()

	if wins != 1 {
		t.Errorf("successful rotations = %d, want exactly 1", wins)
	}
	if reuses != attempts-1 {
		t.Errorf("rotations refused as reuse = %d, want %d", reuses, attempts-1)
	}
	if len(others) != 0 {
		t.Errorf("unexpected errors: %v", others)
	}
	if live := liveCount(t, s, userID); live != 1 {
		t.Errorf("live tokens = %d, want exactly 1: one old token must yield one new token", live)
	}
}
