package config

import (
	"errors"
	"log/slog"
	"slices"
	"strings"
	"testing"

	"github.com/moshfiqamin/hiredesk/internal/platform"
)

// setRequired sets the two required variables to valid values, so a test
// exercises only the variable it is about.
func setRequired(t *testing.T) {
	t.Helper()
	t.Setenv("DATABASE_URL", testDatabaseURL)
	t.Setenv("JWT_SECRET", strings.Repeat("k", minJWTSecretLength))
}

func TestLoadLogSettings(t *testing.T) {
	tests := []struct {
		name       string
		env        string
		level      string
		format     string
		wantLevel  slog.Level
		wantFormat string
		wantErrVar string // the variable the InvalidVarError must name; "" for success
	}{
		{name: "defaults in development", env: "development", wantLevel: slog.LevelInfo, wantFormat: platform.FormatText},
		{name: "defaults in production", env: "production", wantLevel: slog.LevelInfo, wantFormat: platform.FormatJSON},
		{name: "debug, any case", env: "development", level: "DEBUG", wantLevel: slog.LevelDebug, wantFormat: platform.FormatText},
		{name: "json in development", env: "development", format: "json", wantLevel: slog.LevelInfo, wantFormat: platform.FormatJSON},
		{name: "unknown level", env: "development", level: "verbose", wantErrVar: "LOG_LEVEL"},
		{name: "level with an offset", env: "development", level: "info+2", wantErrVar: "LOG_LEVEL"},
		{name: "unknown format", env: "development", format: "xml", wantErrVar: "LOG_FORMAT"},
		{name: "text refused in production", env: "production", format: "text", wantErrVar: "LOG_FORMAT"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setRequired(t)
			t.Setenv("ENV", tt.env)
			t.Setenv("LOG_LEVEL", tt.level)
			t.Setenv("LOG_FORMAT", tt.format)

			cfg, err := Load()
			if tt.wantErrVar != "" {
				var invalid *InvalidVarError
				if !errors.As(err, &invalid) || invalid.Name != tt.wantErrVar {
					t.Fatalf("Load() error = %v, want an InvalidVarError for %s", err, tt.wantErrVar)
				}
				return
			}
			if err != nil {
				t.Fatalf("Load() unexpected error: %v", err)
			}
			if cfg.LogLevel != tt.wantLevel || cfg.LogFormat != tt.wantFormat {
				t.Errorf("got level %v format %q, want %v %q", cfg.LogLevel, cfg.LogFormat, tt.wantLevel, tt.wantFormat)
			}
		})
	}
}

func TestLoadAllowedOrigins(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    []string
		wantErr bool
	}{
		{name: "unset means none", raw: "", want: nil},
		{name: "one", raw: "https://app.example.com", want: []string{"https://app.example.com"}},
		{name: "several, spaced, with a port", raw: " http://localhost:3000 , https://App.Example.com ", want: []string{"http://localhost:3000", "https://app.example.com"}},
		{name: "empty entries skipped", raw: "https://a.example,,", want: []string{"https://a.example"}},
		{name: "bare wildcard", raw: "*", wantErr: true},
		{name: "wildcard subdomain", raw: "https://*.example.com", wantErr: true},
		{name: "trailing slash", raw: "https://app.example.com/", wantErr: true},
		{name: "a page URL", raw: "https://app.example.com/login", wantErr: true},
		{name: "no scheme", raw: "app.example.com", wantErr: true},
		{name: "not http", raw: "ftp://app.example.com", wantErr: true},
		{name: "user information", raw: "https://me@app.example.com", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setRequired(t)
			t.Setenv("ALLOWED_ORIGINS", tt.raw)

			cfg, err := Load()
			if tt.wantErr {
				var invalid *InvalidVarError
				if !errors.As(err, &invalid) || invalid.Name != "ALLOWED_ORIGINS" {
					t.Fatalf("Load() error = %v, want an InvalidVarError for ALLOWED_ORIGINS", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Load() unexpected error: %v", err)
			}
			if !slices.Equal(cfg.AllowedOrigins, tt.want) {
				t.Errorf("AllowedOrigins = %q, want %q", cfg.AllowedOrigins, tt.want)
			}
		})
	}
}

// TestConfigLogValueHidesSecrets logs a whole *Config through the production
// logger and checks that neither secret reaches the output.
func TestConfigLogValueHidesSecrets(t *testing.T) {
	const secret = "s3cr3t-signing-key-that-is-32-bytes!"
	t.Setenv("DATABASE_URL", "postgres://hiredesk:db-pa55word@db:5432/hiredesk")
	t.Setenv("JWT_SECRET", secret)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}

	var out strings.Builder
	platform.NewLogger(&out, slog.LevelInfo, platform.FormatJSON).Info("config loaded", "config", cfg)

	for _, leaked := range []string{secret, "db-pa55word"} {
		if strings.Contains(out.String(), leaked) {
			t.Errorf("log output contains %q:\n%s", leaked, out.String())
		}
	}
	if !strings.Contains(out.String(), `"port":8080`) {
		t.Errorf("log output is missing the safe fields:\n%s", out.String())
	}
}
