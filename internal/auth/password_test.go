package auth

import (
	"errors"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const testPassword = "correct horse battery staple"

// A hash of testPassword made at cost 12 by an earlier process. Verifying it
// shows a stored hash needs nothing but itself to be checked.
const storedHash = "$2a$12$n88Xsg8Ll97cyK1lcLqe7OWOYPAZij9vxGdlA6m2YJVW3o6pAk4vC"

// The tables below run at bcrypt.MinCost. Under the race detector one cost-12
// operation takes seconds, and the branches being tested do not depend on the
// cost. The production cost is covered by the three tests at the bottom.
const testCost = bcrypt.MinCost

// mustHash fails the test rather than returning an error, so fixtures can be
// built in one line.
func mustHash(t *testing.T, plain string) string {
	t.Helper()

	hash, err := hashAtCost(plain, testCost)
	if err != nil {
		t.Fatalf("hashAtCost(%q) unexpected error: %v", plain, err)
	}
	return hash
}

func TestHash(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		plain   string
		wantErr error
	}{
		{name: "ordinary password", plain: testPassword},
		{name: "exactly the maximum length", plain: strings.Repeat("a", MaxPasswordBytes)},
		{name: "empty password", plain: "", wantErr: ErrEmptyPassword},
		{name: "one byte over the maximum", plain: strings.Repeat("a", MaxPasswordBytes+1), wantErr: ErrPasswordTooLong},
		// 19 four-byte runes: 19 characters, 76 bytes.
		{name: "short in characters but long in bytes", plain: strings.Repeat("🔑", 19), wantErr: ErrPasswordTooLong},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := hashAtCost(tt.plain, testCost)

			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("error = %v, want %v", err, tt.wantErr)
				}
				if got != "" {
					t.Errorf("returned %q alongside an error, want an empty string", got)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !strings.HasPrefix(got, "$2a$04$") {
				t.Errorf("hash = %q, want the prefix $2a$04$", got)
			}
			if len(got) != 60 {
				t.Errorf("len(hash) = %d, want 60", len(got))
			}
			if strings.Contains(got, tt.plain) {
				t.Errorf("hash contains the plaintext password")
			}
		})
	}
}

func TestVerify(t *testing.T) {
	t.Parallel()

	first := mustHash(t, testPassword)
	second := mustHash(t, testPassword)
	if first == second {
		t.Fatalf("hashing the same password twice produced identical hashes: %q", first)
	}

	longest := strings.Repeat("a", MaxPasswordBytes)
	longestHash := mustHash(t, longest)

	fallback := mustHash(t, "stand-in for dummyHash")

	tests := []struct {
		name  string
		hash  string
		plain string
		want  bool
	}{
		{name: "correct password", hash: first, plain: testPassword, want: true},
		{name: "correct password against the second hash", hash: second, plain: testPassword, want: true},
		{name: "correct password at the maximum length", hash: longestHash, plain: longest, want: true},
		{name: "wrong password", hash: first, plain: "incorrect horse battery staple", want: false},
		{name: "differs only in case", hash: first, plain: strings.ToUpper(testPassword), want: false},
		{name: "empty password", hash: first, plain: "", want: false},
		// bcrypt alone would accept this: it stops reading at byte 72.
		{name: "correct 72 bytes followed by more", hash: longestHash, plain: longest + "b", want: false},

		{name: "malformed: empty hash", hash: "", plain: testPassword, want: false},
		{name: "malformed: not a hash at all", hash: "not-a-bcrypt-hash", plain: testPassword, want: false},
		{name: "malformed: the plaintext stored as the hash", hash: testPassword, plain: testPassword, want: false},
		{name: "malformed: truncated", hash: first[:30], plain: testPassword, want: false},
		{name: "malformed: unknown version", hash: "$9a$" + first[4:], plain: testPassword, want: false},
		{name: "malformed: cost out of range", hash: "$2a$99$" + first[7:], plain: testPassword, want: false},
		{name: "malformed: missing leading dollar", hash: "x" + first[1:], plain: testPassword, want: false},
		// The password that produced the fallback must not log in to an
		// account whose stored hash is unusable.
		{name: "malformed: empty hash, fallback's own password", hash: "", plain: "stand-in for dummyHash", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// A panic inside the call fails the subtest, so "returns false
			// rather than panicking" needs no separate recover.
			if got := verifyWithFallback(tt.hash, tt.plain, fallback); got != tt.want {
				t.Errorf("verify(%q, %q) = %v, want %v", tt.hash, tt.plain, got, tt.want)
			}
		})
	}
}

func TestHashUsesProductionCost(t *testing.T) {
	t.Parallel()

	hash, err := Hash(testPassword)
	if err != nil {
		t.Fatalf("Hash() unexpected error: %v", err)
	}
	if !strings.HasPrefix(hash, "$2a$12$") {
		t.Errorf("Hash() = %q, want the prefix $2a$12$", hash)
	}
}

func TestVerifyStoredHash(t *testing.T) {
	t.Parallel()

	if !Verify(storedHash, testPassword) {
		t.Errorf("Verify() = false for a known cost-12 hash of the test password")
	}
}

// Guards the constant in password.go: a dummy hash at a different cost would
// make the malformed-hash path faster or slower than the real one.
func TestDummyHashMatchesCost(t *testing.T) {
	t.Parallel()

	got, err := bcrypt.Cost([]byte(dummyHash))
	if err != nil {
		t.Fatalf("dummyHash does not parse as a bcrypt hash: %v", err)
	}
	if got != Cost {
		t.Errorf("dummyHash has cost %d, want %d — regenerate it", got, Cost)
	}
}

// Not parallel: it compares two wall-clock durations, and sharing CPUs with
// other bcrypt work would distort both.
func TestVerifyMalformedHashTakesComparableTime(t *testing.T) {
	// High enough that one comparison is tens of milliseconds, well clear of
	// timer and scheduler noise, without costing what Cost would.
	const timingCost = 8

	hash, err := hashAtCost(testPassword, timingCost)
	if err != nil {
		t.Fatalf("hashAtCost() unexpected error: %v", err)
	}
	fallback, err := hashAtCost("stand-in for dummyHash", timingCost)
	if err != nil {
		t.Fatalf("hashAtCost() unexpected error: %v", err)
	}

	start := time.Now()
	verifyWithFallback(hash, "wrong password", fallback)
	wellFormed := time.Since(start)

	start = time.Now()
	verifyWithFallback("", "wrong password", fallback)
	malformed := time.Since(start)

	// Deliberately loose. Without the fallback comparison the malformed path
	// returns in microseconds — a ratio in the thousands — so a factor of four
	// separates the two outcomes without being sensitive to a busy machine.
	if malformed < wellFormed/4 {
		t.Errorf("malformed hash took %v, well-formed took %v: the malformed path is skipping the bcrypt work",
			malformed, wellFormed)
	}
}
