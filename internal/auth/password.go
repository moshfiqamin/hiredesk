// Package auth holds the primitives behind authentication: password hashing
// and signed access tokens.
//
// Nothing here knows about HTTP or the database. A handler asks this package
// for a hash to store or a yes/no on a login attempt, and decides for itself
// what status code that becomes.
package auth

import (
	"errors"
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

// Cost is the bcrypt work factor used for every new hash. It is an exponent:
// bcrypt runs 2^Cost rounds of key setup, so each +1 doubles the time.
//
// Raising it later is safe — the cost is stored inside each hash, so existing
// hashes keep verifying at the cost they were created with.
const Cost = 12

// MaxPasswordBytes is the longest password bcrypt can hash. The limit is in
// bytes, not characters: a password of 30 emoji is already over it.
const MaxPasswordBytes = 72

// dummyHash is a well-formed hash, at Cost, of random bytes that were
// discarded. Verify compares against it when the stored hash is unusable, so
// that path costs the same as a real comparison. It must be regenerated if
// Cost changes; TestDummyHashMatchesCost fails when the two drift apart.
const dummyHash = "$2a$12$n1hUdVd7nDgqOSQkWFlN/.9suh8CbvcxKKGRMyHzHe5m5SkfHi796"

var (
	// ErrEmptyPassword is returned by Hash for a zero-length password.
	ErrEmptyPassword = errors.New("auth: password is empty")

	// ErrPasswordTooLong is returned by Hash for a password over
	// MaxPasswordBytes.
	ErrPasswordTooLong = errors.New("auth: password is longer than 72 bytes")
)

// Hash returns the bcrypt hash of plain, in the standard 60-character encoding
// that carries its own version, cost and salt. The result is what gets stored;
// plain itself must never be stored or logged.
//
// Each call generates a fresh random salt, so hashing the same password twice
// returns two different strings. Both verify.
func Hash(plain string) (string, error) {
	return hashAtCost(plain, Cost)
}

// Verify reports whether plain is the password that produced hash.
//
// It never returns an error and never panics: an empty, truncated or otherwise
// malformed hash is simply a failed verification. That path still performs one
// full bcrypt comparison, so a caller that passes "" for an account that does
// not exist takes as long as one that passes a real hash — the response time
// does not reveal which accounts exist.
//
// The guarantee holds as far as bcrypt allows: a stored hash created at a
// different cost takes a different time, because the cost is the running time.
func Verify(hash, plain string) bool {
	return verifyWithFallback(hash, plain, dummyHash)
}

// hashAtCost is Hash with the cost as a parameter, so the tests can exercise
// every branch at bcrypt.MinCost instead of paying for Cost on each case.
func hashAtCost(plain string, cost int) (string, error) {
	// bcrypt itself accepts an empty password and returns a valid hash for it,
	// so this has to be refused here.
	if plain == "" {
		return "", ErrEmptyPassword
	}

	// Checked here rather than left to bcrypt so callers can test for this
	// package's own error without importing bcrypt. len on a string counts
	// bytes, which is the unit the limit is defined in.
	if len(plain) > MaxPasswordBytes {
		return "", ErrPasswordTooLong
	}

	hashed, err := bcrypt.GenerateFromPassword([]byte(plain), cost)
	if err != nil {
		// Reachable if the system's random source fails while the salt is
		// being generated, or if cost is outside bcrypt's 4–31 range.
		return "", fmt.Errorf("hash password: %w", err)
	}

	return string(hashed), nil
}

// verifyWithFallback is Verify with the stand-in hash as a parameter, for the
// same reason hashAtCost takes its cost.
func verifyWithFallback(hash, plain, fallback string) bool {
	password := []byte(plain)

	err := bcrypt.CompareHashAndPassword([]byte(hash), password)

	// Anything other than "matched" or "did not match" means bcrypt rejected
	// the hash before doing any work, in microseconds. Spend the missing time
	// on a hash that is known to parse.
	if err != nil && !errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
		//nolint:errcheck // Only the time spent matters; the outcome is discarded.
		bcrypt.CompareHashAndPassword([]byte(fallback), password)
		return false
	}

	// bcrypt ignores every byte after the 72nd when comparing. Hash refuses
	// such passwords, so one arriving here cannot be genuine even when its
	// first 72 bytes match. Checked after the comparison, not before, so the
	// length of the attempt does not shorten the response either.
	return err == nil && len(password) <= MaxPasswordBytes
}
