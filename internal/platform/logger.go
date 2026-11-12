// Package platform holds process-wide plumbing that every layer shares: the
// structured logger, and the request-scoped values it reads from a context.
package platform

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
)

// Recognised values for the log format.
const (
	FormatJSON = "json"
	FormatText = "text"
)

// Redacted replaces the value of any attribute whose key looks sensitive.
const Redacted = "[REDACTED]"

// sensitiveKeyParts are matched against attribute keys, lowercased. Any key
// containing one is redacted, whatever its value: "password", "new_password",
// "refresh_token", "Authorization", "jwt_secret" and so on.
var sensitiveKeyParts = []string{
	"password",
	"passwd",
	"secret",
	"token",
	"authorization",
	"cookie",
	"api_key",
	"apikey",
	"database_url",
	"dsn",
}

// ParseLevel reads a level name as LOG_LEVEL spells it: debug, info, warn or
// error, in any case.
func ParseLevel(s string) (slog.Level, error) {
	var level slog.Level
	name := strings.ToLower(strings.TrimSpace(s))
	// UnmarshalText also accepts offsets such as "info+2"; those are refused
	// here so the variable stays one of four documented words.
	switch name {
	case "debug", "info", "warn", "error":
		if err := level.UnmarshalText([]byte(name)); err != nil {
			return 0, err
		}
		return level, nil
	default:
		return 0, errors.New("must be one of debug, info, warn, error")
	}
}

// NewLogger returns a logger that writes to w at level and above, as JSON
// lines or, with FormatText, as key=value text for reading in a terminal.
//
// Every record passes through two safeguards: attributes with sensitive keys
// are redacted, and the request ID and user ID carried by the context given
// to a *Context method (InfoContext, ErrorContext...) are added.
func NewLogger(w io.Writer, level slog.Level, format string) *slog.Logger {
	opts := &slog.HandlerOptions{
		Level:       level,
		ReplaceAttr: redact,
	}

	var base slog.Handler
	if format == FormatText {
		base = slog.NewTextHandler(w, opts)
	} else {
		base = slog.NewJSONHandler(w, opts)
	}

	return slog.New(contextHandler{Handler: base})
}

// redact is the ReplaceAttr hook: it sees every attribute before it is
// written, and swaps the value of a sensitive one for Redacted.
//
// A safety net, not the defence: it cannot see inside a struct logged as one
// value, which is why no request, header map or config struct is ever logged
// whole.
func redact(_ []string, a slog.Attr) slog.Attr {
	key := strings.ToLower(a.Key)
	for _, part := range sensitiveKeyParts {
		if strings.Contains(key, part) {
			return slog.String(a.Key, Redacted)
		}
	}
	return a
}

// contextHandler adds request-scoped attributes from the context to every
// record. Embedding slog.Handler promotes Enabled, so only the methods that
// must change are written here.
type contextHandler struct {
	slog.Handler
}

// Handle adds request_id and user_id, when the context carries them, then
// passes the record on.
func (h contextHandler) Handle(ctx context.Context, r slog.Record) error {
	if id := RequestID(ctx); id != "" {
		r.AddAttrs(slog.String("request_id", id))
	}
	if id := UserID(ctx); id != "" {
		r.AddAttrs(slog.String("user_id", id))
	}
	return h.Handler.Handle(ctx, r)
}

// WithAttrs returns a contextHandler around the wrapped handler's own
// WithAttrs, so a logger made with logger.With(...) keeps adding the context
// attributes.
func (h contextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return contextHandler{Handler: h.Handler.WithAttrs(attrs)}
}

// WithGroup returns a contextHandler around the wrapped handler's own
// WithGroup, for the same reason as WithAttrs.
func (h contextHandler) WithGroup(name string) slog.Handler {
	return contextHandler{Handler: h.Handler.WithGroup(name)}
}
