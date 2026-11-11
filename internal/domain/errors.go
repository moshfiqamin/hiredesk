// Package domain holds the types and rules of the hiring pipeline that are
// true regardless of how a request arrived or where the data is stored.
//
// It imports neither net/http nor a database driver, and must stay that way:
// the api and store packages depend on domain, never the reverse.
package domain

import (
	"errors"
	"sort"
	"strings"
)

// The outcomes a caller is expected to handle. Each is a category, not a
// description: wrap one with fmt.Errorf and %w to add the specifics, and test
// for it with errors.Is.
var (
	// ErrNotFound means the requested thing does not exist, or is not visible
	// to the caller.
	ErrNotFound = errors.New("not found")

	// ErrDuplicate means a create was refused because something with the same
	// unique key already exists, such as a second account for one email.
	ErrDuplicate = errors.New("already exists")

	// ErrInvalidCredentials means a login failed. Deliberately one error for
	// both "no such user" and "wrong password", so the response cannot be used
	// to discover which emails are registered.
	ErrInvalidCredentials = errors.New("invalid credentials")

	// ErrForbidden means the caller is known and is not allowed to do this.
	ErrForbidden = errors.New("forbidden")

	// ErrValidation means the input was understood and breaks a rule. Use a
	// *ValidationError to say which fields.
	ErrValidation = errors.New("validation failed")

	// ErrConflict means the request is valid on its own and cannot be applied
	// to the resource in its current state, such as hiring into a closed job.
	ErrConflict = errors.New("conflict with current state")

	// ErrInvalidTransition means a status change was refused because the
	// lifecycle does not allow it from the current status. Use a
	// *TransitionError to say which statuses would be allowed.
	ErrInvalidTransition = errors.New("status change not allowed")

	// ErrNotReady means a status change was refused because the resource
	// lacks something the new status depends on. Use a *NotReadyError to say
	// what.
	ErrNotReady = errors.New("not ready for this status")

	// ErrFormLocked means a job post's questions cannot be changed because
	// the post is no longer a draft. Use a *FormLockedError to say why.
	ErrFormLocked = errors.New("application form is locked")
)

// ValidationError is an ErrValidation that names each offending field.
//
// The zero value is ready to use:
//
//	var v domain.ValidationError
//	if email == "" {
//		v.Add("email", "is required")
//	}
//	return v.Err()
type ValidationError struct {
	// Fields maps a field name, as the client sent it, to what is wrong with
	// it. Both are shown to the client, so neither may contain internals.
	Fields map[string]string
}

// Add records a problem with field. A second problem for the same field
// replaces the first.
func (e *ValidationError) Add(field, problem string) {
	// Writing to a nil map panics, and the zero value's map is nil.
	if e.Fields == nil {
		e.Fields = make(map[string]string)
	}
	e.Fields[field] = problem
}

// Err returns e if any field has a problem, and nil otherwise.
//
// Return this rather than e itself. A nil *ValidationError stored in an error
// interface is not equal to nil, so a caller's `if err != nil` would fire for
// input that was perfectly valid.
func (e *ValidationError) Err() error {
	if len(e.Fields) == 0 {
		return nil
	}
	return e
}

// Error lists the offending field names. The per-field text is left out: this
// string is for logs, and the client receives Fields directly.
func (e *ValidationError) Error() string {
	names := make([]string, 0, len(e.Fields))
	for name := range e.Fields {
		names = append(names, name)
	}
	// Map iteration order is random; sorted so the same error always prints
	// the same way.
	sort.Strings(names)

	return ErrValidation.Error() + ": " + strings.Join(names, ", ")
}

// Details returns Fields as a client sees it.
func (e *ValidationError) Details() map[string]any {
	out := make(map[string]any, len(e.Fields))
	for field, problem := range e.Fields {
		out[field] = problem
	}
	return out
}

// Unwrap makes errors.Is(err, ErrValidation) true for a *ValidationError, so
// callers that only need the category do not have to know this type exists.
func (e *ValidationError) Unwrap() error {
	return ErrValidation
}
