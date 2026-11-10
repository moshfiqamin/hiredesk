package domain

import (
	"errors"
	"fmt"
	"testing"
)

func TestSentinelsAreDistinct(t *testing.T) {
	t.Parallel()

	sentinels := map[string]error{
		"ErrNotFound":           ErrNotFound,
		"ErrDuplicate":          ErrDuplicate,
		"ErrInvalidCredentials": ErrInvalidCredentials,
		"ErrForbidden":          ErrForbidden,
		"ErrValidation":         ErrValidation,
		"ErrConflict":           ErrConflict,
	}

	for name, sentinel := range sentinels {
		for otherName, other := range sentinels {
			if got, want := errors.Is(sentinel, other), name == otherName; got != want {
				t.Errorf("errors.Is(%s, %s) = %v, want %v", name, otherName, got, want)
			}
		}
	}
}

func TestSentinelSurvivesWrapping(t *testing.T) {
	t.Parallel()

	// Two layers, as a store and then a service would add them.
	inner := fmt.Errorf("select user by email: %w", ErrNotFound)
	outer := fmt.Errorf("login: %w", inner)

	if !errors.Is(outer, ErrNotFound) {
		t.Errorf("errors.Is(outer, ErrNotFound) = false, want true")
	}
	//nolint:errorlint // The direct comparison is the point of this assertion.
	if outer == ErrNotFound {
		t.Errorf("outer == ErrNotFound, want the wrapped error to be a different value")
	}
	if errors.Is(outer, ErrForbidden) {
		t.Errorf("errors.Is(outer, ErrForbidden) = true, want false")
	}

	// %v keeps the text and drops the link.
	//nolint:errorlint // Deliberately the wrong verb, to show what it loses.
	flattened := fmt.Errorf("login: %v", inner)
	if errors.Is(flattened, ErrNotFound) {
		t.Errorf("errors.Is(flattened, ErrNotFound) = true, want %%v to break the chain")
	}
}

func TestValidationError(t *testing.T) {
	t.Parallel()

	t.Run("no problems is a nil error", func(t *testing.T) {
		t.Parallel()

		var v ValidationError
		if err := v.Err(); err != nil {
			t.Errorf("Err() = %v, want nil", err)
		}
	})

	t.Run("collects fields and matches ErrValidation", func(t *testing.T) {
		t.Parallel()

		var v ValidationError
		v.Add("password", "must be at least 12 characters")
		v.Add("email", "is required")
		v.Add("email", "is not a valid address")

		err := v.Err()
		if err == nil {
			t.Fatalf("Err() = nil, want an error")
		}
		if !errors.Is(err, ErrValidation) {
			t.Errorf("errors.Is(err, ErrValidation) = false, want true")
		}
		if got, want := err.Error(), "validation failed: email, password"; got != want {
			t.Errorf("Error() = %q, want %q", got, want)
		}

		// Wrapped once more, the fields must still be recoverable.
		wrapped := fmt.Errorf("register: %w", err)

		var got *ValidationError
		if !errors.As(wrapped, &got) {
			t.Fatalf("errors.As(wrapped, *ValidationError) = false, want true")
		}
		if len(got.Fields) != 2 {
			t.Errorf("len(Fields) = %d, want 2: %v", len(got.Fields), got.Fields)
		}
		if got.Fields["email"] != "is not a valid address" {
			t.Errorf("Fields[email] = %q, want the later problem to replace the earlier", got.Fields["email"])
		}
	})
}
