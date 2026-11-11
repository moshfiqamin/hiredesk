package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/moshfiqamin/hiredesk/internal/domain"
)

// The kind of text a database driver produces, and exactly what must never
// reach a client: a table, a constraint, a column and a user's email.
const driverMessage = `ERROR: duplicate key value violates unique constraint "users_email_key" ` +
	`(SQLSTATE 23505) DETAIL: Key (email)=(alice@example.com) already exists.`

func TestMain(m *testing.M) {
	// Silences Gin's route-registration banner and debug warnings.
	gin.SetMode(gin.TestMode)
	os.Exit(m.Run())
}

// captureLog redirects the standard logger into a buffer for one test. The
// logger is process-wide, so tests using this must not run in parallel.
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()

	var buf bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(previous) })

	return &buf
}

// respond runs Respond against a recorded request and returns the response.
func respond(t *testing.T, err error) *httptest.ResponseRecorder {
	t.Helper()

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/auth/register?token=query-secret", nil)

	Respond(c, err)

	return rec
}

// detail returns details[name] as a string, and "" if it is absent or not a
// string.
func (d errorDetail) detail(name string) string {
	s, _ := d.Details[name].(string)
	return s
}

// stringDetails returns details as a map of strings, for comparing with a
// table of expected field problems. A value that is not a string is kept as
// its %v form, so a comparison with it fails rather than passing silently.
func (d errorDetail) stringDetails() map[string]string {
	out := make(map[string]string, len(d.Details))
	for key, value := range d.Details {
		out[key] = fmt.Sprintf("%v", value)
		if s, ok := value.(string); ok {
			out[key] = s
		}
	}
	return out
}

// decode fails the test unless body is exactly the envelope: one top-level
// key, and code, message and details beneath it.
func decode(t *testing.T, body []byte) errorDetail {
	t.Helper()

	var raw map[string]map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatalf("body is not the envelope: %v\n%s", err, body)
	}
	if len(raw) != 1 || raw["error"] == nil {
		t.Fatalf("body = %s, want exactly one top-level key, \"error\"", body)
	}
	if len(raw["error"]) != 3 {
		t.Fatalf("error object = %s, want exactly code, message and details", body)
	}
	// An absent or null details would be a second shape.
	if got := string(raw["error"]["details"]); !strings.HasPrefix(got, "{") {
		t.Fatalf("details = %s, want a JSON object", got)
	}

	var envelope errorBody
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	return envelope.Error
}

func TestRespondMapsDomainErrors(t *testing.T) {
	logged := captureLog(t)

	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{"not found", domain.ErrNotFound, http.StatusNotFound, "not_found"},
		{"duplicate", domain.ErrDuplicate, http.StatusConflict, "duplicate"},
		{"invalid credentials", domain.ErrInvalidCredentials, http.StatusUnauthorized, "invalid_credentials"},
		{"forbidden", domain.ErrForbidden, http.StatusForbidden, "forbidden"},
		{"validation", domain.ErrValidation, http.StatusUnprocessableEntity, "validation_failed"},
		{"conflict", domain.ErrConflict, http.StatusConflict, "conflict"},
		{
			name:       "wrapped once",
			err:        fmt.Errorf("select job 42: %w", domain.ErrNotFound),
			wantStatus: http.StatusNotFound,
			wantCode:   "not_found",
		},
		{
			name:       "wrapped twice",
			err:        fmt.Errorf("apply: %w", fmt.Errorf("insert application: %w", domain.ErrDuplicate)),
			wantStatus: http.StatusConflict,
			wantCode:   "duplicate",
		},
		{
			// The store wraps the driver's error and the sentinel together.
			name:       "wrapped alongside a driver error",
			err:        fmt.Errorf("insert user: %w: %w", domain.ErrDuplicate, errors.New(driverMessage)),
			wantStatus: http.StatusConflict,
			wantCode:   "duplicate",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := respond(t, tt.err)

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}

			got := decode(t, rec.Body.Bytes())
			if got.Code != tt.wantCode {
				t.Errorf("code = %q, want %q", got.Code, tt.wantCode)
			}
			if got.Message == "" {
				t.Errorf("message is empty")
			}
			if len(got.Details) != 0 {
				t.Errorf("details = %v, want empty", got.Details)
			}

			// The wrapping text belongs to the server.
			for _, internal := range []string{"select job", "insert", "users_email_key", "alice@example.com", "SQLSTATE"} {
				if strings.Contains(rec.Body.String(), internal) {
					t.Errorf("body leaks %q: %s", internal, rec.Body)
				}
			}
		})
	}

	// Expected outcomes are not incidents.
	if logged.Len() != 0 {
		t.Errorf("domain errors were logged, want silence: %s", logged)
	}
}

func TestRespondValidationCarriesFieldDetails(t *testing.T) {
	var v domain.ValidationError
	v.Add("email", "is not a valid address")
	v.Add("password", "must be at least 12 characters")

	// Wrapped, to show the fields are found through the chain.
	rec := respond(t, fmt.Errorf("register: %w", v.Err()))

	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}

	got := decode(t, rec.Body.Bytes())
	if got.Code != "validation_failed" {
		t.Errorf("code = %q, want %q", got.Code, "validation_failed")
	}
	if len(got.Details) != 2 {
		t.Fatalf("details = %v, want two fields", got.Details)
	}
	if got.detail("email") != "is not a valid address" {
		t.Errorf("details[email] = %q", got.detail("email"))
	}
	if got.detail("password") != "must be at least 12 characters" {
		t.Errorf("details[password] = %q", got.detail("password"))
	}
}

func TestRespondHidesUnexpectedErrors(t *testing.T) {
	logged := captureLog(t)

	rec := respond(t, fmt.Errorf("insert user: %w", errors.New(driverMessage)))

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}

	got := decode(t, rec.Body.Bytes())
	if got.Code != codeInternal {
		t.Errorf("code = %q, want %q", got.Code, codeInternal)
	}
	if got.Message != internalMessage {
		t.Errorf("message = %q, want the fixed internal message", got.Message)
	}

	for _, internal := range []string{"insert user", "users_email_key", "alice@example.com", "SQLSTATE", "duplicate key"} {
		if strings.Contains(rec.Body.String(), internal) {
			t.Errorf("body leaks %q: %s", internal, rec.Body)
		}
	}

	// The same detail must reach the server's log, with enough to find the
	// request.
	for _, want := range []string{driverMessage, "insert user", "POST", "/auth/register"} {
		if !strings.Contains(logged.String(), want) {
			t.Errorf("log is missing %q: %s", want, logged)
		}
	}
	if strings.Contains(logged.String(), "query-secret") {
		t.Errorf("log contains the query string: %s", logged)
	}
}

func TestRespondAbortsTheChain(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	_, engine := gin.CreateTestContext(rec)

	engine.GET("/jobs/:id",
		func(c *gin.Context) { Respond(c, domain.ErrForbidden) },
		func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"secret": "salary band"}) },
	)

	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/jobs/7", nil))

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
	if strings.Contains(rec.Body.String(), "salary band") {
		t.Errorf("the handler after Respond still ran: %s", rec.Body)
	}
	decode(t, rec.Body.Bytes())
}

func TestRouterLevelErrorsUseTheEnvelope(t *testing.T) {
	// Not parallel: gin.CustomRecovery writes the panic to gin's process-wide
	// error writer, which is redirected here to keep the test output clean.
	previous := gin.DefaultErrorWriter
	gin.DefaultErrorWriter = &bytes.Buffer{}
	t.Cleanup(func() { gin.DefaultErrorWriter = previous })

	engine := gin.New()
	engine.Use(Recovery())
	engine.HandleMethodNotAllowed = true
	engine.NoRoute(NoRoute)
	engine.NoMethod(NoMethod)

	engine.GET("/jobs", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	engine.GET("/panic", func(*gin.Context) {
		panic(`runtime error: nil pointer dereference in store.FindUser`)
	})

	tests := []struct {
		name       string
		method     string
		path       string
		wantStatus int
		wantCode   string
	}{
		{"unknown path", http.MethodGet, "/nope", http.StatusNotFound, codeRouteNotFound},
		{"wrong method on a real path", http.MethodDelete, "/jobs", http.StatusMethodNotAllowed, codeMethodNotAllowed},
		{"panic in a handler", http.MethodGet, "/panic", http.StatusInternalServerError, codeInternal},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			engine.ServeHTTP(rec, httptest.NewRequest(tt.method, tt.path, nil))

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if got := decode(t, rec.Body.Bytes()); got.Code != tt.wantCode {
				t.Errorf("code = %q, want %q", got.Code, tt.wantCode)
			}
			if strings.Contains(rec.Body.String(), "store.FindUser") {
				t.Errorf("body leaks the panic value: %s", rec.Body)
			}
		})
	}
}
