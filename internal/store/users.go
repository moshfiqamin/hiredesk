package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/moshfiqamin/hiredesk/internal/domain"
)

// PostgreSQL error codes (SQLSTATE) this package translates. The full list is
// Appendix A of the PostgreSQL manual.
const (
	// A UNIQUE constraint or unique index rejected the row.
	codeUniqueViolation = "23505"

	// A value could not be parsed as the column's type, e.g. "abc" as a uuid.
	codeInvalidTextRepresentation = "22P02"
)

// userColumns is the column list every query here returns, in the order
// scanUser reads them. One constant so the two cannot drift apart.
//
// id is cast to text because the Go field is a string and the column is a
// uuid; the cast makes the conversion Postgres's job instead of the driver's.
const userColumns = `id::text, email, password_hash, full_name, role, created_at, updated_at`

// UserStore reads and writes the users table.
type UserStore struct {
	db *pgxpool.Pool
}

// NewUserStore returns a UserStore that runs its queries on db.
func NewUserStore(db *pgxpool.Pool) *UserStore {
	return &UserStore{db: db}
}

// CreateUser inserts a user and returns the stored row, including the ID and
// timestamps the database assigned.
//
// It returns an error matching domain.ErrDuplicate if the email is taken,
// compared case-insensitively.
func (s *UserStore) CreateUser(ctx context.Context, in domain.NewUser) (*domain.User, error) {
	// $1..$4 are placeholders: the values travel separately from the SQL text
	// and are never spliced into it, which is what rules out SQL injection.
	//
	// RETURNING hands back the row as stored, so the generated id and
	// timestamps arrive without a second query.
	const query = `
		INSERT INTO users (email, password_hash, full_name, role)
		VALUES ($1, $2, $3, $4)
		RETURNING ` + userColumns

	user, err := scanUser(s.db.QueryRow(ctx, query, in.Email, in.PasswordHash, in.FullName, in.Role))
	if err != nil {
		return nil, translate("create user", err)
	}
	return user, nil
}

// GetUserByEmail returns the user with this email, compared
// case-insensitively, or an error matching domain.ErrNotFound.
func (s *UserStore) GetUserByEmail(ctx context.Context, email string) (*domain.User, error) {
	// lower() on both sides, to match the expression users_email_lower_idx is
	// built on. Written any other way the planner cannot use that index and
	// scans the table.
	const query = `
		SELECT ` + userColumns + `
		FROM users
		WHERE lower(email) = lower($1)`

	user, err := scanUser(s.db.QueryRow(ctx, query, email))
	if err != nil {
		return nil, translate("get user by email", err)
	}
	return user, nil
}

// GetUserByID returns the user with this ID, or an error matching
// domain.ErrNotFound. An ID that is not a UUID is reported as not found.
func (s *UserStore) GetUserByID(ctx context.Context, id string) (*domain.User, error) {
	// The parameter is sent as text and cast by Postgres, so a malformed ID
	// comes back as SQLSTATE 22P02 rather than a driver-side encoding error.
	const query = `
		SELECT ` + userColumns + `
		FROM users
		WHERE id = $1::text::uuid`

	user, err := scanUser(s.db.QueryRow(ctx, query, id))
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == codeInvalidTextRepresentation {
			// No user can have an ID that is not a UUID.
			return nil, fmt.Errorf("get user by id: %w", domain.ErrNotFound)
		}
		return nil, translate("get user by id", err)
	}
	return user, nil
}

// scanUser reads one row in userColumns order.
//
// pgx.Row defers every failure to Scan, including the query's own: a failed
// INSERT surfaces here, and so does pgx.ErrNoRows.
func scanUser(row pgx.Row) (*domain.User, error) {
	var u domain.User

	err := row.Scan(&u.ID, &u.Email, &u.PasswordHash, &u.FullName, &u.Role, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// translate turns a database error into a domain error where one applies, so
// nothing above this package needs to import pgx to understand a failure.
//
// op names the operation and becomes the prefix of the message.
func translate(op string, err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%s: %w", op, domain.ErrNotFound)
	}

	// errors.As searches the chain for a *pgconn.PgError, the type pgx uses
	// for any error the server itself reported, and copies it into pgErr.
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == codeUniqueViolation {
		// Both are wrapped: the sentinel for the api layer to map, and the
		// original so the constraint name is still there for a log.
		return fmt.Errorf("%s: %w: %w", op, domain.ErrDuplicate, err)
	}

	return fmt.Errorf("%s: %w", op, err)
}
