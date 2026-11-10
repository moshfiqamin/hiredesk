package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/moshfiqamin/hiredesk/internal/domain"
)

// refreshTokenColumns is the column list every query here returns, in the
// order scanRefreshToken reads them. token_hash is absent: it is only ever a
// search key.
const refreshTokenColumns = `id::text, user_id::text, expires_at, revoked_at, created_at`

// TokenStore reads and writes the refresh_tokens table.
//
// Every method takes or matches a token's hash. The token itself never
// reaches this package.
type TokenStore struct {
	db *pgxpool.Pool
}

// NewTokenStore returns a TokenStore that runs its queries on db.
func NewTokenStore(db *pgxpool.Pool) *TokenStore {
	return &TokenStore{db: db}
}

// CreateRefreshToken stores a new, unrevoked token for the user.
func (s *TokenStore) CreateRefreshToken(ctx context.Context, userID, tokenHash string, expiresAt time.Time) error {
	const query = `
		INSERT INTO refresh_tokens (user_id, token_hash, expires_at)
		VALUES ($1::text::uuid, $2, $3)`

	if _, err := s.db.Exec(ctx, query, userID, tokenHash, expiresAt); err != nil {
		return translate("create refresh token", err)
	}
	return nil
}

// GetRefreshTokenByHash returns the token stored under tokenHash, or an error
// matching domain.ErrNotFound. It returns revoked and expired tokens too; the
// caller decides what those mean.
func (s *TokenStore) GetRefreshTokenByHash(ctx context.Context, tokenHash string) (*domain.RefreshToken, error) {
	const query = `
		SELECT ` + refreshTokenColumns + `
		FROM refresh_tokens
		WHERE token_hash = $1`

	token, err := scanRefreshToken(s.db.QueryRow(ctx, query, tokenHash))
	if err != nil {
		return nil, translate("get refresh token", err)
	}
	return token, nil
}

// RevokeRefreshToken revokes the token stored under tokenHash. Revoking a
// token that is unknown or already revoked is not an error and changes
// nothing.
func (s *TokenStore) RevokeRefreshToken(ctx context.Context, tokenHash string) error {
	// "AND revoked_at IS NULL" keeps the first revocation's timestamp. Without
	// it a second call would overwrite the record of when the token really
	// stopped being valid.
	const query = `
		UPDATE refresh_tokens
		SET revoked_at = now()
		WHERE token_hash = $1 AND revoked_at IS NULL`

	if _, err := s.db.Exec(ctx, query, tokenHash); err != nil {
		return translate("revoke refresh token", err)
	}
	return nil
}

// RevokeAllRefreshTokensForUser revokes every token of the user that is still
// usable and returns how many that was.
func (s *TokenStore) RevokeAllRefreshTokensForUser(ctx context.Context, userID string) (int64, error) {
	const query = `
		UPDATE refresh_tokens
		SET revoked_at = now()
		WHERE user_id = $1::text::uuid AND revoked_at IS NULL`

	tag, err := s.db.Exec(ctx, query, userID)
	if err != nil {
		return 0, translate("revoke all refresh tokens", err)
	}
	return tag.RowsAffected(), nil
}

// RotateRefreshToken exchanges the token stored under oldHash for a new one
// stored under newHash, and returns the ID of the user both belong to.
//
// On success the old token is revoked and the new one exists, or neither
// happened. It fails with:
//
//   - an error matching domain.ErrNotFound if oldHash is unknown;
//   - a *domain.TokenReuseError if the old token was already revoked;
//   - an error matching domain.ErrTokenExpired if it has expired.
//
// In none of those cases is anything written.
func (s *TokenStore) RotateRefreshToken(ctx context.Context, oldHash, newHash string, newExpiresAt time.Time) (string, error) {
	// One transaction, so the three statements below are all-or-nothing. A
	// crash between revoking the old token and storing the new one must not
	// leave the user with neither.
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return "", fmt.Errorf("rotate refresh token: begin: %w", err)
	}
	// On every early return below this undoes whatever was done. After a
	// successful Commit it does nothing and returns ErrTxClosed, so its error
	// carries no information on either path.
	defer tx.Rollback(ctx) //nolint:errcheck // See above.

	// FOR UPDATE locks the row until this transaction ends. That is the whole
	// defence against two requests presenting the same token at the same
	// moment: the second waits here, and when it is let through it sees the
	// row as the first one left it — revoked.
	//
	// Without the lock both would read "not revoked", both would issue a new
	// token, and a thief racing the owner would walk away with a valid one.
	const selectQuery = `
		SELECT id::text, user_id::text, revoked_at IS NOT NULL, expires_at <= now()
		FROM refresh_tokens
		WHERE token_hash = $1
		FOR UPDATE`

	var (
		id, userID       string
		revoked, expired bool
	)
	if err := tx.QueryRow(ctx, selectQuery, oldHash).Scan(&id, &userID, &revoked, &expired); err != nil {
		return "", translate("rotate refresh token", err)
	}

	// Revoked is checked before expired: a replayed token is a replay however
	// old it is.
	if revoked {
		return "", fmt.Errorf("rotate refresh token: %w", &domain.TokenReuseError{UserID: userID})
	}
	if expired {
		return "", fmt.Errorf("rotate refresh token: %w", domain.ErrTokenExpired)
	}

	const revokeQuery = `UPDATE refresh_tokens SET revoked_at = now() WHERE id = $1::text::uuid`
	if _, err := tx.Exec(ctx, revokeQuery, id); err != nil {
		return "", translate("rotate refresh token: revoke old", err)
	}

	const insertQuery = `
		INSERT INTO refresh_tokens (user_id, token_hash, expires_at)
		VALUES ($1::text::uuid, $2, $3)`
	if _, err := tx.Exec(ctx, insertQuery, userID, newHash, newExpiresAt); err != nil {
		return "", translate("rotate refresh token: store new", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("rotate refresh token: commit: %w", err)
	}
	return userID, nil
}

// scanRefreshToken reads one row in refreshTokenColumns order.
func scanRefreshToken(row pgx.Row) (*domain.RefreshToken, error) {
	var t domain.RefreshToken

	// &t.RevokedAt is a **time.Time: pgx sets the inner pointer to nil for a
	// NULL column and to the value otherwise.
	if err := row.Scan(&t.ID, &t.UserID, &t.ExpiresAt, &t.RevokedAt, &t.CreatedAt); err != nil {
		return nil, err
	}
	return &t, nil
}
