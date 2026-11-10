package config

import (
	"bufio"
	"errors"
	"os"
	"strings"
	"testing"
)

// Enough for Load to get past its required-variable check; nothing connects.
const testDatabaseURL = "postgres://user:pass@localhost:5432/hiredesk?sslmode=disable"

// These tests cannot use t.Parallel: t.Setenv changes the environment of the
// whole process, and the testing package refuses to combine the two.
func TestLoadJWTSecret(t *testing.T) {
	tests := []struct {
		name string

		// secret is the raw JWT_SECRET value; unset leaves the variable out of
		// the environment altogether.
		secret string
		unset  bool

		wantMissing bool
		wantWeak    bool

		// wantLength is the length WeakSecretError should report.
		wantLength int
	}{
		{name: "unset", unset: true, wantMissing: true},
		{name: "set but empty", secret: "", wantMissing: true},
		{name: "set but only whitespace", secret: "   ", wantMissing: true},
		{name: "far too short", secret: "secret", wantWeak: true, wantLength: 6},
		{name: "one character short", secret: strings.Repeat("k", 31), wantWeak: true, wantLength: 31},
		// Padding must not count towards the length.
		{name: "short, padded with spaces to 40", secret: strings.Repeat("k", 20) + strings.Repeat(" ", 20), wantWeak: true, wantLength: 20},
		{name: "exactly the minimum", secret: strings.Repeat("k", 32)},
		{name: "openssl rand -hex 32", secret: strings.Repeat("0123456789abcdef", 4)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("DATABASE_URL", testDatabaseURL)
			// Setenv first even when unsetting: it registers the cleanup that
			// restores whatever value the variable had before the test.
			t.Setenv("JWT_SECRET", tt.secret)
			if tt.unset {
				os.Unsetenv("JWT_SECRET")
			}

			cfg, err := Load()

			switch {
			case tt.wantMissing:
				var missing *MissingVarsError
				if !errors.As(err, &missing) {
					t.Fatalf("Load() error = %v, want a *MissingVarsError", err)
				}
				if len(missing.Names) != 1 || missing.Names[0] != "JWT_SECRET" {
					t.Errorf("missing = %v, want [JWT_SECRET]", missing.Names)
				}

			case tt.wantWeak:
				var weak *WeakSecretError
				if !errors.As(err, &weak) {
					t.Fatalf("Load() error = %v, want a *WeakSecretError", err)
				}
				if weak.Length != tt.wantLength {
					t.Errorf("reported length = %d, want %d", weak.Length, tt.wantLength)
				}
				if secret := strings.TrimSpace(tt.secret); strings.Contains(err.Error(), secret) {
					t.Errorf("error message contains the secret: %q", err.Error())
				}

			default:
				if err != nil {
					t.Fatalf("Load() unexpected error: %v", err)
				}
				if cfg.JWTSecret != tt.secret {
					t.Errorf("JWTSecret was not loaded unchanged")
				}
			}

			if err != nil && cfg != nil {
				t.Errorf("Load() returned a Config alongside an error")
			}
		})
	}
}

// The placeholder in .env.example is public. If it were long enough to pass,
// `cp .env.example .env` would start the API on a key anyone can read.
func TestEnvExamplePlaceholderIsRejected(t *testing.T) {
	file, err := os.Open("../../.env.example")
	if err != nil {
		t.Fatalf("open .env.example: %v", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		placeholder, found := strings.CutPrefix(scanner.Text(), "JWT_SECRET=")
		if !found {
			continue
		}

		var weak *WeakSecretError
		if err := validateJWTSecret(placeholder); !errors.As(err, &weak) {
			t.Errorf("the .env.example placeholder %q is accepted as a JWT_SECRET", placeholder)
		}
		return
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("read .env.example: %v", err)
	}

	t.Fatal(".env.example has no JWT_SECRET line")
}
