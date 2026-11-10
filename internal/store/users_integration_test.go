//go:build integration

package store

import (
	"context"
	"crypto/rand"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/moshfiqamin/hiredesk/internal/config"
	"github.com/moshfiqamin/hiredesk/internal/domain"
)

// These tests run the real SQL against a real Postgres with the migrations
// applied. They are behind the "integration" build tag so that a plain
// `go test ./...` needs no database; run them with `make test-integration`.

// newTestStore connects using the same configuration the server reads.
func newTestStore(t *testing.T) *UserStore {
	t.Helper()

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("integration tests need DATABASE_URL and JWT_SECRET: %v", err)
	}

	pool, err := New(context.Background(), cfg)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)

	return NewUserStore(pool)
}

// newCandidate returns a user with an address no other test or run has used,
// and removes the row again when the test ends.
func newCandidate(t *testing.T, s *UserStore) domain.NewUser {
	t.Helper()

	email := "it-" + strings.ToLower(rand.Text()) + "@example.test"

	t.Cleanup(func() {
		_, err := s.db.Exec(context.Background(), `DELETE FROM users WHERE lower(email) = lower($1)`, email)
		if err != nil {
			t.Errorf("clean up %s: %v", email, err)
		}
	})

	return domain.NewUser{
		Email: email,
		// A string of the right shape; nothing here verifies it.
		PasswordHash: "$2a$04$" + strings.Repeat("x", 53),
		FullName:     "Integration Test",
		Role:         domain.RoleCandidate,
	}
}

func TestUserStoreRoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	in := newCandidate(t, s)

	created, err := s.CreateUser(ctx, in)
	if err != nil {
		t.Fatalf("CreateUser() unexpected error: %v", err)
	}

	if len(created.ID) != 36 {
		t.Errorf("ID = %q, want a 36-character UUID assigned by the database", created.ID)
	}
	if created.Email != in.Email || created.FullName != in.FullName || created.Role != in.Role {
		t.Errorf("created = %+v, want the fields of %+v", created, in)
	}
	if created.PasswordHash != in.PasswordHash {
		t.Errorf("PasswordHash = %q, want it stored unchanged", created.PasswordHash)
	}
	if time.Since(created.CreatedAt) > time.Minute || created.CreatedAt.IsZero() {
		t.Errorf("CreatedAt = %v, want roughly now", created.CreatedAt)
	}

	byEmail, err := s.GetUserByEmail(ctx, in.Email)
	if err != nil {
		t.Fatalf("GetUserByEmail() unexpected error: %v", err)
	}
	if *byEmail != *created {
		t.Errorf("GetUserByEmail() = %+v, want %+v", byEmail, created)
	}

	// The lookup must not care about case.
	byUpper, err := s.GetUserByEmail(ctx, strings.ToUpper(in.Email))
	if err != nil {
		t.Fatalf("GetUserByEmail(upper case) unexpected error: %v", err)
	}
	if byUpper.ID != created.ID {
		t.Errorf("upper-case lookup found %q, want %q", byUpper.ID, created.ID)
	}

	byID, err := s.GetUserByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetUserByID() unexpected error: %v", err)
	}
	if *byID != *created {
		t.Errorf("GetUserByID() = %+v, want %+v", byID, created)
	}
}

func TestUserStoreDuplicateEmail(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	in := newCandidate(t, s)

	if _, err := s.CreateUser(ctx, in); err != nil {
		t.Fatalf("CreateUser() unexpected error: %v", err)
	}

	// Same address in different case: the unique index is on lower(email).
	again := in
	again.Email = strings.ToUpper(in.Email)

	user, err := s.CreateUser(ctx, again)
	if !errors.Is(err, domain.ErrDuplicate) {
		t.Fatalf("CreateUser(duplicate) error = %v, want ErrDuplicate", err)
	}
	if user != nil {
		t.Errorf("CreateUser(duplicate) returned %+v alongside an error", user)
	}

	// What a real server sends, read back out of the wrapped error.
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		t.Fatalf("no *pgconn.PgError in the chain: %v", err)
	}
	if pgErr.Code != "23505" || pgErr.ConstraintName != "users_email_lower_idx" {
		t.Errorf("Code = %q, ConstraintName = %q, want 23505 and users_email_lower_idx", pgErr.Code, pgErr.ConstraintName)
	}
}

func TestUserStoreNotFound(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	tests := []struct {
		name string
		get  func() (*domain.User, error)
	}{
		{"unknown email", func() (*domain.User, error) { return s.GetUserByEmail(ctx, "nobody@example.test") }},
		{"unknown id", func() (*domain.User, error) { return s.GetUserByID(ctx, "00000000-0000-0000-0000-000000000000") }},
		{"id that is not a uuid", func() (*domain.User, error) { return s.GetUserByID(ctx, "42") }},
		{"empty id", func() (*domain.User, error) { return s.GetUserByID(ctx, "") }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			user, err := tt.get()
			if !errors.Is(err, domain.ErrNotFound) {
				t.Fatalf("error = %v, want ErrNotFound", err)
			}
			if user != nil {
				t.Errorf("returned %+v alongside an error", user)
			}
		})
	}
}

// A role the database does not allow must fail, and must not be mistaken for
// a duplicate just because it is also a constraint violation.
func TestUserStoreRejectsUnknownRole(t *testing.T) {
	s := newTestStore(t)
	in := newCandidate(t, s)
	in.Role = "superuser"

	_, err := s.CreateUser(context.Background(), in)
	if err == nil {
		t.Fatalf("CreateUser(role superuser) succeeded, want users_role_check to refuse it")
	}
	if errors.Is(err, domain.ErrDuplicate) {
		t.Errorf("a check-constraint violation was reported as ErrDuplicate: %v", err)
	}
}
