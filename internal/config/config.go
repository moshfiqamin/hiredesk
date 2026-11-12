// Package config loads all runtime settings from environment variables once,
// at startup, into a single typed struct.
//
// Nothing outside this package calls os.Getenv; everything receives a *Config.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/moshfiqamin/hiredesk/internal/platform"
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

// Connection pool defaults. These are set explicitly rather than left to pgx,
// whose own default is MaxConns = max(4, NumCPU) and MinConns = 0 — a value
// that depends on the machine the container happens to land on.
const (
	defaultDBMaxConns        = 10
	defaultDBMinConns        = 2
	defaultDBMaxConnLifetime = time.Hour
	defaultDBMaxConnIdleTime = 30 * time.Minute

	// Budget for the startup Ping. Long enough for a container still
	// accepting connections, short enough that a wrong host fails visibly.
	defaultDBConnectTimeout = 5 * time.Second

	// Budget for the /readyz Ping. Deliberately shorter than any load
	// balancer's own probe timeout, so the handler answers first.
	defaultDBPingTimeout = 2 * time.Second

	// Grace period for in-flight requests to finish during shutdown.
	defaultShutdownTimeout = 10 * time.Second
)

// Upper bounds, so a typo such as DB_MAX_CONNS=1000000 is refused at startup
// rather than exhausting the server's connection slots at runtime.
const (
	maxAllowedDBConns      = 500
	maxAllowedDuration     = 24 * time.Hour
	minAllowedPoolDuration = time.Second
)

// minJWTSecretLength is the shortest JWT_SECRET accepted, in bytes. HMAC-SHA256
// gains nothing from a key longer than its 32-byte output, and loses strength
// with every byte below it.
const minJWTSecretLength = 32

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

	// DBMaxConns is the hard ceiling on connections this instance may hold.
	// int32 rather than int because that is pgxpool.Config's own type; keeping
	// it here avoids a lossy conversion at the call site.
	DBMaxConns int32

	// DBMinConns is the number of idle connections kept open even when the API
	// is idle, so the first request after a quiet spell does not pay for a TCP
	// handshake, TLS and Postgres authentication.
	DBMinConns int32

	// DBMaxConnLifetime caps how long a single connection may live, however
	// healthy. Bounds the blast radius of a leaked server-side session and
	// lets connections migrate after a failover.
	DBMaxConnLifetime time.Duration

	// DBMaxConnIdleTime closes a connection that has gone unused for this
	// long, releasing server memory during quiet periods.
	DBMaxConnIdleTime time.Duration

	// DBConnectTimeout bounds the startup Ping.
	DBConnectTimeout time.Duration

	// DBPingTimeout bounds the /readyz Ping.
	DBPingTimeout time.Duration

	// ShutdownTimeout bounds how long the server waits for in-flight requests
	// before giving up and closing anyway.
	ShutdownTimeout time.Duration

	// LogLevel is the lowest level written. slog.Level rather than a string,
	// so a typo fails at startup instead of silently logging everything.
	LogLevel slog.Level

	// LogFormat is platform.FormatJSON or platform.FormatText. Text is for a
	// developer's terminal and is refused in production, where logs are read
	// by machines.
	LogFormat string

	// AllowedOrigins are the browser origins allowed to call the API across
	// origins, exactly as a browser sends them in the Origin header
	// ("https://app.example.com"). Empty means no cross-origin access at all.
	AllowedOrigins []string
}

// LogValue implements slog.LogValuer, so logging a *Config writes only the
// settings that are safe to read. The secret and the connection string, which
// carries the database password, are never included.
func (c *Config) LogValue() slog.Value {
	return slog.GroupValue(
		slog.Int("port", c.Port),
		slog.String("env", c.Env),
		slog.Int("db_max_conns", int(c.DBMaxConns)),
		slog.Int("db_min_conns", int(c.DBMinConns)),
		slog.Duration("db_max_conn_lifetime", c.DBMaxConnLifetime),
		slog.Duration("db_max_conn_idle_time", c.DBMaxConnIdleTime),
		slog.Duration("shutdown_timeout", c.ShutdownTimeout),
		slog.String("log_level", c.LogLevel.String()),
		slog.String("log_format", c.LogFormat),
		slog.Any("allowed_origins", c.AllowedOrigins),
	)
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

// WeakSecretError reports a secret that is set but too short to be safe.
//
// A separate type from InvalidVarError because that one prints the offending
// value, and a secret must never reach a log — not even a rejected one.
type WeakSecretError struct {
	Name      string
	Length    int
	MinLength int
}

func (e *WeakSecretError) Error() string {
	return fmt.Sprintf(
		"%s is too short: %d characters, need at least %d (generate one with: openssl rand -hex 32)",
		e.Name, e.Length, e.MinLength,
	)
}

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

	if err := validateJWTSecret(jwtSecret); err != nil {
		return nil, err
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

	maxConns, err := lookupInt32("DB_MAX_CONNS", defaultDBMaxConns, 1, maxAllowedDBConns)
	if err != nil {
		return nil, err
	}

	minConns, err := lookupInt32("DB_MIN_CONNS", defaultDBMinConns, 0, maxAllowedDBConns)
	if err != nil {
		return nil, err
	}

	// Cross-field check: pgx would accept this and then never honour MinConns,
	// so catching it here turns a silent misconfiguration into a startup error.
	if minConns > maxConns {
		return nil, &InvalidVarError{
			Name:  "DB_MIN_CONNS",
			Value: strconv.FormatInt(int64(minConns), 10),
			Err:   fmt.Errorf("must not exceed DB_MAX_CONNS (%d)", maxConns),
		}
	}

	maxConnLifetime, err := lookupDuration("DB_MAX_CONN_LIFETIME", defaultDBMaxConnLifetime)
	if err != nil {
		return nil, err
	}

	maxConnIdleTime, err := lookupDuration("DB_MAX_CONN_IDLE_TIME", defaultDBMaxConnIdleTime)
	if err != nil {
		return nil, err
	}

	connectTimeout, err := lookupDuration("DB_CONNECT_TIMEOUT", defaultDBConnectTimeout)
	if err != nil {
		return nil, err
	}

	pingTimeout, err := lookupDuration("DB_PING_TIMEOUT", defaultDBPingTimeout)
	if err != nil {
		return nil, err
	}

	shutdownTimeout, err := lookupDuration("SHUTDOWN_TIMEOUT", defaultShutdownTimeout)
	if err != nil {
		return nil, err
	}

	logLevel := slog.LevelInfo
	if raw, ok := lookupNonEmpty("LOG_LEVEL"); ok {
		logLevel, err = platform.ParseLevel(raw)
		if err != nil {
			return nil, &InvalidVarError{Name: "LOG_LEVEL", Value: raw, Err: err}
		}
	}

	logFormat, err := lookupLogFormat(env)
	if err != nil {
		return nil, err
	}

	var origins []string
	if raw, ok := lookupNonEmpty("ALLOWED_ORIGINS"); ok {
		origins, err = parseOrigins(raw)
		if err != nil {
			return nil, &InvalidVarError{Name: "ALLOWED_ORIGINS", Value: raw, Err: err}
		}
	}

	return &Config{
		Port:              port,
		DatabaseURL:       databaseURL,
		JWTSecret:         jwtSecret,
		Env:               env,
		DBMaxConns:        maxConns,
		DBMinConns:        minConns,
		DBMaxConnLifetime: maxConnLifetime,
		DBMaxConnIdleTime: maxConnIdleTime,
		DBConnectTimeout:  connectTimeout,
		DBPingTimeout:     pingTimeout,
		ShutdownTimeout:   shutdownTimeout,
		LogLevel:          logLevel,
		LogFormat:         logFormat,
		AllowedOrigins:    origins,
	}, nil
}

// lookupLogFormat reads LOG_FORMAT. Unset, it is text in development, where a
// person reads the output, and JSON everywhere else. Text is refused in
// production, where the logs feed a collector that parses JSON.
func lookupLogFormat(env string) (string, error) {
	raw, ok := lookupNonEmpty("LOG_FORMAT")
	if !ok {
		if env == EnvDevelopment {
			return platform.FormatText, nil
		}
		return platform.FormatJSON, nil
	}

	format := strings.ToLower(raw)
	switch {
	case format != platform.FormatJSON && format != platform.FormatText:
		return "", &InvalidVarError{Name: "LOG_FORMAT", Value: raw, Err: errors.New("must be json or text")}
	case format == platform.FormatText && env == EnvProduction:
		return "", &InvalidVarError{Name: "LOG_FORMAT", Value: raw, Err: errors.New("text is for development only; use json in production")}
	}
	return format, nil
}

// parseOrigins splits a comma-separated list of origins and checks that each
// is exactly what a browser sends in an Origin header: a scheme, a host, an
// optional port, and nothing else.
//
// A wildcard is refused outright. With credentials allowed, "*" is not even
// legal CORS, and the usual workaround — reflecting whatever Origin arrives —
// lets any website make authenticated calls on a visitor's behalf.
func parseOrigins(raw string) ([]string, error) {
	var origins []string
	for _, part := range strings.Split(raw, ",") {
		origin := strings.TrimSpace(part)
		if origin == "" {
			continue
		}
		if strings.Contains(origin, "*") {
			return nil, errors.New("wildcards are not allowed; list each origin explicitly")
		}

		u, err := url.Parse(origin)
		if err != nil {
			return nil, fmt.Errorf("%q is not a URL", origin)
		}
		// An Origin header never has a path, query, fragment or user
		// information, so a configured origin with one would never match,
		// and is almost certainly a pasted page URL.
		if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" ||
			u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
			return nil, fmt.Errorf("%q must be scheme://host[:port], with no path or trailing slash", origin)
		}

		// Browsers send the scheme and host in lower case.
		origins = append(origins, strings.ToLower(origin))
	}
	return origins, nil
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

// validateJWTSecret refuses a signing key short enough to be guessed.
//
// Anyone holding a token can test candidate keys against its signature offline,
// with no request to this server, so the key's length is the only defence.
// Length is all that is checked: 32 repeated characters pass, which is why the
// key should come from a random generator rather than a keyboard.
func validateJWTSecret(secret string) error {
	// len counts bytes, the unit HMAC measures a key in.
	if len(secret) < minJWTSecretLength {
		return &WeakSecretError{
			Name:      "JWT_SECRET",
			Length:    len(secret),
			MinLength: minJWTSecretLength,
		}
	}
	return nil
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

// lookupInt32 reads name as an int32 within [lo, hi], returning def when unset.
//
// Parsed with a bit size of 32 so a value above the int32 range is rejected by
// strconv rather than silently truncated by a conversion.
func lookupInt32(name string, def, lo, hi int32) (int32, error) {
	raw, ok := lookupNonEmpty(name)
	if !ok {
		return def, nil
	}

	parsed, err := strconv.ParseInt(raw, 10, 32)
	if err != nil {
		return 0, &InvalidVarError{Name: name, Value: raw, Err: err}
	}

	value := int32(parsed)
	if value < lo || value > hi {
		return 0, &InvalidVarError{
			Name:  name,
			Value: raw,
			Err:   fmt.Errorf("must be between %d and %d", lo, hi),
		}
	}

	return value, nil
}

// lookupDuration reads name as a Go duration string ("30s", "1h30m"),
// returning def when unset.
//
// A bare number is rejected: time.ParseDuration("30") is an error, which is
// the desired outcome — "30" is ambiguous between seconds and minutes.
func lookupDuration(name string, def time.Duration) (time.Duration, error) {
	raw, ok := lookupNonEmpty(name)
	if !ok {
		return def, nil
	}

	parsed, err := time.ParseDuration(raw)
	if err != nil {
		return 0, &InvalidVarError{Name: name, Value: raw, Err: err}
	}

	if parsed < minAllowedPoolDuration || parsed > maxAllowedDuration {
		return 0, &InvalidVarError{
			Name:  name,
			Value: raw,
			Err:   fmt.Errorf("must be between %s and %s", minAllowedPoolDuration, maxAllowedDuration),
		}
	}

	return parsed, nil
}
