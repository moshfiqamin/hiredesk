// Package config loads all runtime settings from environment variables once,
// at startup, into a single typed struct.
//
// Nothing outside this package calls os.Getenv; everything receives a *Config.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Recognised values for Config.Env.
const (
	EnvDevelopment = "development"
	EnvProduction  = "production"
)

const (
	defaultPort = 8080
	defaultEnv  = EnvDevelopment
)

// Config holds every setting the API needs. It is built once in main and then
// passed explicitly, so a function's dependencies are visible in its signature.
type Config struct {
	// Port is the TCP port the HTTP server listens on. An int rather than a
	// string, so a non-numeric PORT fails at startup instead of on first use.
	Port int

	// DatabaseURL is the full Postgres connection string. Required.
	DatabaseURL string

	// JWTSecret signs and verifies auth tokens. Required, with no default: a
	// fallback secret that reaches production is worse than refusing to boot.
	JWTSecret string

	// Env is one of the Env* constants.
	Env string
}

// MissingVarsError reports every required environment variable that was absent
// or empty, rather than only the first one found.
type MissingVarsError struct {
	// Names is in the order Load checks them, so the message is deterministic.
	Names []string
}

func (e *MissingVarsError) Error() string {
	return fmt.Sprintf(
		"missing required environment variable(s): %s",
		strings.Join(e.Names, ", "),
	)
}

// InvalidVarError reports a variable that was present but unparseable.
type InvalidVarError struct {
	Name  string
	Value string
	Err   error
}

func (e *InvalidVarError) Error() string {
	return fmt.Sprintf("invalid value for %s: %q: %v", e.Name, e.Value, e.Err)
}

// Unwrap exposes the underlying parse failure to errors.Is and errors.As.
func (e *InvalidVarError) Unwrap() error { return e.Err }

// Load reads the environment and returns a fully populated *Config.
//
// Every missing required variable is collected before returning, so one run
// reports all of them. Returns (nil, error) on failure, never a partially
// filled Config.
func Load() (*Config, error) {
	var missing []string

	databaseURL := lookupRequired("DATABASE_URL", &missing)
	jwtSecret := lookupRequired("JWT_SECRET", &missing)

	if len(missing) > 0 {
		return nil, &MissingVarsError{Names: missing}
	}

	port := defaultPort
	if raw, ok := lookupNonEmpty("PORT"); ok {
		parsed, err := strconv.ParseInt(raw, 10, 0)
		if err != nil {
			return nil, &InvalidVarError{Name: "PORT", Value: raw, Err: err}
		}
		if parsed < 1 || parsed > 65535 {
			return nil, &InvalidVarError{
				Name:  "PORT",
				Value: raw,
				Err:   fmt.Errorf("must be between 1 and 65535"),
			}
		}
		port = int(parsed)
	}

	env := defaultEnv
	if raw, ok := lookupNonEmpty("ENV"); ok {
		env = strings.ToLower(strings.TrimSpace(raw))
	}

	return &Config{
		Port:        port,
		DatabaseURL: databaseURL,
		JWTSecret:   jwtSecret,
		Env:         env,
	}, nil
}

// IsProduction reports whether the API is running in production.
func (c *Config) IsProduction() bool { return c.Env == EnvProduction }

// Addr returns the listen address in the form net/http expects, e.g. ":8080".
func (c *Config) Addr() string { return fmt.Sprintf(":%d", c.Port) }

// lookupRequired returns the value of name, or appends name to *missing and
// returns "".
//
// missing is a pointer because append may reallocate: passing the slice by
// value would hide the result from the caller.
func lookupRequired(name string, missing *[]string) string {
	value, ok := lookupNonEmpty(name)
	if !ok {
		*missing = append(*missing, name)
		return ""
	}
	return value
}

// lookupNonEmpty treats a variable that is set but blank as unset, and trims
// surrounding whitespace so a stray newline never becomes part of a secret.
func lookupNonEmpty(name string) (string, bool) {
	value, ok := os.LookupEnv(name)
	if !ok {
		return "", false
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return "", false
	}
	return value, true
}
