package store

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/moshfiqamin/hiredesk/internal/domain"
)

// These tests need no database: they hand translate the errors pgx would
// produce and check what comes out.
func TestTranslate(t *testing.T) {
	t.Parallel()

	unique := &pgconn.PgError{
		Code:           codeUniqueViolation,
		Message:        `duplicate key value violates unique constraint "users_email_lower_idx"`,
		ConstraintName: "users_email_lower_idx",
	}
	check := &pgconn.PgError{
		Code:           "23514",
		Message:        `new row for relation "users" violates check constraint "users_role_check"`,
		ConstraintName: "users_role_check",
	}
	network := errors.New("dial tcp 10.0.0.5:5432: connect: connection refused")

	tests := []struct {
		name string
		err  error

		// want is nil when the error must match no domain sentinel.
		want error
	}{
		{name: "no rows", err: pgx.ErrNoRows, want: domain.ErrNotFound},
		{name: "no rows, already wrapped", err: fmt.Errorf("scan: %w", pgx.ErrNoRows), want: domain.ErrNotFound},
		{name: "unique violation", err: unique, want: domain.ErrDuplicate},
		{name: "unique violation, already wrapped", err: fmt.Errorf("exec: %w", unique), want: domain.ErrDuplicate},
		{name: "a different constraint violation", err: check},
		{name: "not a database error at all", err: network},
	}

	sentinels := []error{
		domain.ErrNotFound, domain.ErrDuplicate, domain.ErrInvalidCredentials,
		domain.ErrForbidden, domain.ErrValidation, domain.ErrConflict,
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := translate("create user", tt.err)

			for _, sentinel := range sentinels {
				//nolint:errorlint // Comparing two sentinels by identity, not inspecting a chain.
				if matches, want := errors.Is(got, sentinel), sentinel == tt.want; matches != want {
					t.Errorf("errors.Is(got, %q) = %v, want %v", sentinel, matches, want)
				}
			}

			if !strings.HasPrefix(got.Error(), "create user: ") {
				t.Errorf("message = %q, want it to start with the operation", got)
			}
			// A server error stays in the chain for the log. pgx.ErrNoRows is
			// dropped on purpose: it says nothing ErrNotFound does not.
			if !errors.Is(tt.want, domain.ErrNotFound) && !errors.Is(got, tt.err) {
				t.Errorf("the original error is no longer in the chain: %v", got)
			}
		})
	}
}

func TestTranslateKeepsThePostgresErrorReadable(t *testing.T) {
	t.Parallel()

	original := &pgconn.PgError{Code: codeUniqueViolation, ConstraintName: "users_email_lower_idx"}

	// Three layers on top: translate, then two callers adding context.
	err := fmt.Errorf("register: %w", fmt.Errorf("service: %w", translate("create user", original)))

	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		t.Fatalf("errors.As found no *pgconn.PgError in %v", err)
	}
	if pgErr.Code != "23505" {
		t.Errorf("Code = %q, want 23505", pgErr.Code)
	}
	if pgErr.ConstraintName != "users_email_lower_idx" {
		t.Errorf("ConstraintName = %q, want users_email_lower_idx", pgErr.ConstraintName)
	}
}
