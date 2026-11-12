package platform

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

// decodeLine parses the single JSON log line in buf.
func decodeLine(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	var line map[string]any
	if err := json.Unmarshal(buf.Bytes(), &line); err != nil {
		t.Fatalf("log output is not one JSON object: %v\n%s", err, buf)
	}
	return line
}

func TestContextAttributesAreAdded(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLogger(&buf, slog.LevelInfo, FormatJSON)

	ctx := WithUserID(WithRequestID(context.Background(), "req-123"), "user-456")
	logger.InfoContext(ctx, "doing work", "step", 1)

	line := decodeLine(t, &buf)
	if line["request_id"] != "req-123" || line["user_id"] != "user-456" {
		t.Errorf("request_id = %v, user_id = %v; want req-123 and user-456", line["request_id"], line["user_id"])
	}
}

func TestContextAttributesSurviveWith(t *testing.T) {
	var buf bytes.Buffer
	// With wraps the handler; the context attributes must still be added.
	logger := NewLogger(&buf, slog.LevelInfo, FormatJSON).With("component", "rescore")

	logger.InfoContext(WithRequestID(context.Background(), "req-789"), "done")

	line := decodeLine(t, &buf)
	if line["request_id"] != "req-789" || line["component"] != "rescore" {
		t.Errorf("line = %v; want request_id req-789 and component rescore", line)
	}
}

func TestNoContextMeansNoAttributes(t *testing.T) {
	var buf bytes.Buffer
	NewLogger(&buf, slog.LevelInfo, FormatJSON).Info("startup")

	line := decodeLine(t, &buf)
	if _, ok := line["request_id"]; ok {
		t.Errorf("a log line outside a request has a request_id: %v", line)
	}
}

func TestSensitiveKeysAreRedacted(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLogger(&buf, slog.LevelInfo, FormatJSON)

	logger.Info("careless",
		"password", "hunter2hunter2",
		"refresh_token", "rt-plaintext",
		"Authorization", "Bearer eyJhbGciOi",
		"jwt_secret", "signing-key",
		"database_url", "postgres://u:dbpass@h/db",
		slog.Group("request", slog.String("access_token", "at-plaintext")),
		"email", "someone@example.com",
	)

	out := buf.String()
	for _, leaked := range []string{"hunter2hunter2", "rt-plaintext", "eyJhbGciOi", "signing-key", "dbpass", "at-plaintext"} {
		if strings.Contains(out, leaked) {
			t.Errorf("log output contains %q:\n%s", leaked, out)
		}
	}
	// Only sensitive keys are touched.
	if !strings.Contains(out, "someone@example.com") {
		t.Errorf("a harmless attribute was redacted:\n%s", out)
	}
}

func TestLevelFiltersRecords(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLogger(&buf, slog.LevelWarn, FormatJSON)

	logger.Info("dropped")
	if buf.Len() != 0 {
		t.Fatalf("an info record was written at level warn: %s", &buf)
	}
	logger.Warn("kept")
	if buf.Len() == 0 {
		t.Fatal("a warn record was dropped at level warn")
	}
}

func TestTextFormat(t *testing.T) {
	var buf bytes.Buffer
	NewLogger(&buf, slog.LevelInfo, FormatText).InfoContext(WithRequestID(context.Background(), "abc"), "hello")

	out := buf.String()
	if !strings.Contains(out, "msg=hello") || !strings.Contains(out, "request_id=abc") {
		t.Errorf("text output = %q; want key=value pairs including request_id", out)
	}
}

func TestParseLevel(t *testing.T) {
	tests := []struct {
		in      string
		want    slog.Level
		wantErr bool
	}{
		{in: "debug", want: slog.LevelDebug},
		{in: "INFO", want: slog.LevelInfo},
		{in: " warn ", want: slog.LevelWarn},
		{in: "error", want: slog.LevelError},
		{in: "trace", wantErr: true},
		{in: "info+4", wantErr: true},
		{in: "", wantErr: true},
	}
	for _, tt := range tests {
		got, err := ParseLevel(tt.in)
		if (err != nil) != tt.wantErr || (!tt.wantErr && got != tt.want) {
			t.Errorf("ParseLevel(%q) = %v, %v; want %v, error %v", tt.in, got, err, tt.want, tt.wantErr)
		}
	}
}
