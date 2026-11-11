package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"

	"github.com/moshfiqamin/hiredesk/internal/auth"
	"github.com/moshfiqamin/hiredesk/internal/domain"
)

const testJWTSecret = "test-only-signing-key-0123456789-abcdefghijklmnop"

// fakeUsers is an in-memory UserStore. It keeps the two behaviours of the
// real one that the handlers depend on: emails are unique ignoring case, and
// a missing user is domain.ErrNotFound.
type fakeUsers struct {
	mu      sync.Mutex
	byEmail map[string]*domain.User
	created int

	// failWith, when set, is returned by every call, to stand in for a
	// database that is down.
	failWith error
}

func newFakeUsers() *fakeUsers {
	return &fakeUsers{byEmail: make(map[string]*domain.User)}
}

func (f *fakeUsers) CreateUser(_ context.Context, in domain.NewUser) (*domain.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.failWith != nil {
		return nil, f.failWith
	}

	key := strings.ToLower(in.Email)
	if _, taken := f.byEmail[key]; taken {
		return nil, domain.ErrDuplicate
	}

	// A distinct, UUID-shaped ID per user, as the database would assign.
	f.created++
	user := &domain.User{
		ID:           fmt.Sprintf("00000000-0000-4000-8000-%012d", f.created),
		Email:        in.Email,
		PasswordHash: in.PasswordHash,
		FullName:     in.FullName,
		Role:         in.Role,
		CreatedAt:    time.Date(2026, time.January, 15, 12, 0, 0, 0, time.UTC),
		UpdatedAt:    time.Date(2026, time.January, 15, 12, 0, 0, 0, time.UTC),
	}
	f.byEmail[key] = user
	return user, nil
}

func (f *fakeUsers) GetUserByEmail(_ context.Context, email string) (*domain.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.failWith != nil {
		return nil, f.failWith
	}

	user, ok := f.byEmail[strings.ToLower(email)]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return user, nil
}

func (f *fakeUsers) GetUserByID(_ context.Context, id string) (*domain.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.failWith != nil {
		return nil, f.failWith
	}

	for _, user := range f.byEmail {
		if user.ID == id {
			return user, nil
		}
	}
	return nil, domain.ErrNotFound
}

// okPinger is a database that is always reachable.
type okPinger struct{}

func (okPinger) Ping(context.Context) error { return nil }

// testServer is a full router over fake dependencies.
type testServer struct {
	engine  *gin.Engine
	users   *fakeUsers
	refresh *fakeRefreshTokens
	apps    *fakeApplications
	posts   *fakeJobPosts
	tokens  *auth.TokenManager

	// clock is what the handler and the fake token store both read as "now".
	// Tests move it forward to make a refresh token expire.
	clock time.Time

	// verifyCalls records the hash argument of every password comparison.
	verifyCalls []string
}

func newTestServer(t *testing.T) *testServer {
	t.Helper()

	s := &testServer{
		users:  newFakeUsers(),
		tokens: auth.NewTokenManager(testJWTSecret),
		clock:  time.Date(2026, time.January, 15, 12, 0, 0, 0, time.UTC),
	}
	s.refresh = newFakeRefreshTokens(func() time.Time { return s.clock })

	handler := NewAuthHandler(s.users, s.refresh, s.tokens)
	handler.now = func() time.Time { return s.clock }

	// Real bcrypt at its lowest cost: the handlers' logic does not depend on
	// the cost, and the production cost takes seconds under the race detector.
	handler.hashPassword = func(plain string) (string, error) {
		if len(plain) > auth.MaxPasswordBytes {
			return "", auth.ErrPasswordTooLong
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(plain), bcrypt.MinCost)
		return string(hash), err
	}
	handler.verifyPassword = func(hash, plain string) bool {
		s.verifyCalls = append(s.verifyCalls, hash)
		return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain)) == nil
	}

	s.apps = newFakeApplications()
	s.posts = newFakeJobPosts()
	engine, err := NewRouter(Deps{
		Auth:         handler,
		Applications: NewApplicationHandler(s.apps),
		JobPosts:     NewJobPostHandler(s.posts),
		Tokens:       s.tokens,
		Users:        s.users,
		DB:           okPinger{},
		PingTimeout:  time.Second,
	})
	if err != nil {
		t.Fatalf("NewRouter() unexpected error: %v", err)
	}
	s.engine = engine

	return s
}

// post sends body as JSON and returns the recorded response.
func (s *testServer) post(path, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	s.engine.ServeHTTP(rec, req)
	return rec
}

// mustRegister creates the account the login tests sign in to.
func (s *testServer) mustRegister(t *testing.T) {
	t.Helper()

	rec := s.post("/v1/auth/register", `{"email":"alice@example.com","password":"correct horse","full_name":"Alice Rahman"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("register: status = %d, body = %s", rec.Code, rec.Body)
	}
}

// keysOf returns the top-level keys of a JSON object.
func keysOf(t *testing.T, body []byte) map[string]json.RawMessage {
	t.Helper()

	var object map[string]json.RawMessage
	if err := json.Unmarshal(body, &object); err != nil {
		t.Fatalf("body is not a JSON object: %v\n%s", err, body)
	}
	return object
}

func TestRegister(t *testing.T) {
	t.Parallel()

	s := newTestServer(t)

	// Padded, mixed case, and asking for a role it must not get.
	rec := s.post("/v1/auth/register",
		`{"email":"  Alice@Example.COM ","password":"correct horse","full_name":" Alice Rahman ","role":"admin"}`)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusCreated, rec.Body)
	}

	// Exactly these keys: a new field on domain.User must not appear here
	// until someone adds it to userResponse.
	got := keysOf(t, rec.Body.Bytes())
	want := []string{"id", "email", "full_name", "role", "created_at"}
	if len(got) != len(want) {
		t.Errorf("response has keys %v, want exactly %v", reflect.ValueOf(got).MapKeys(), want)
	}
	for _, key := range want {
		if _, ok := got[key]; !ok {
			t.Errorf("response has no %q", key)
		}
	}

	var user userResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &user); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if user.Email != "alice@example.com" {
		t.Errorf("email = %q, want it trimmed and lowercased", user.Email)
	}
	if user.FullName != "Alice Rahman" {
		t.Errorf("full_name = %q, want it trimmed", user.FullName)
	}
	if user.Role != domain.RoleCandidate {
		t.Errorf("role = %q, want %q whatever the request asked for", user.Role, domain.RoleCandidate)
	}

	stored := s.users.byEmail["alice@example.com"]
	if stored == nil {
		t.Fatalf("nothing stored under the normalised email; store holds %v", s.users.byEmail)
	}
	if stored.PasswordHash == "correct horse" || !strings.HasPrefix(stored.PasswordHash, "$2a$") {
		t.Errorf("stored hash = %q, want a bcrypt hash", stored.PasswordHash)
	}

	// The hash, and the password, in any spelling.
	for _, secret := range []string{stored.PasswordHash, "correct horse", "password", "hash", "$2a$"} {
		if strings.Contains(rec.Body.String(), secret) {
			t.Errorf("response contains %q: %s", secret, rec.Body)
		}
	}
}

func TestRegisterDuplicateEmail(t *testing.T) {
	t.Parallel()

	s := newTestServer(t)
	s.mustRegister(t)

	// Same address, different case.
	rec := s.post("/v1/auth/register", `{"email":"ALICE@example.com","password":"another one","full_name":"Someone Else"}`)

	if rec.Code != http.StatusConflict {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusConflict)
	}
	if got := decode(t, rec.Body.Bytes()); got.Code != "duplicate" {
		t.Errorf("code = %q, want %q", got.Code, "duplicate")
	}
}

func TestRegisterValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
		want map[string]string
	}{
		{
			name: "empty object reports every field at once",
			body: `{}`,
			want: map[string]string{
				"email":     "is required",
				"password":  "is required",
				"full_name": "is required",
			},
		},
		{
			name: "seven-character password",
			body: `{"email":"a@example.com","password":"1234567","full_name":"A"}`,
			want: map[string]string{"password": "must be at least 8 characters"},
		},
		{
			name: "not an email address",
			body: `{"email":"alice-at-example","password":"12345678","full_name":"A"}`,
			want: map[string]string{"email": "must be a valid email address"},
		},
		{
			name: "email that is only spaces",
			body: `{"email":"   ","password":"12345678","full_name":"A"}`,
			want: map[string]string{"email": "is required"},
		},
		{
			name: "name that is only spaces",
			body: `{"email":"a@example.com","password":"12345678","full_name":"  "}`,
			want: map[string]string{"full_name": "is required"},
		},
		{
			name: "password over 72 characters",
			body: `{"email":"a@example.com","password":"` + strings.Repeat("a", 73) + `","full_name":"A"}`,
			want: map[string]string{"password": "must be at most 72 characters"},
		},
		{
			// 20 characters, 80 bytes: passes the character rule, fails bcrypt's.
			name: "password under 72 characters but over 72 bytes",
			body: `{"email":"a@example.com","password":"` + strings.Repeat("🔑", 20) + `","full_name":"A"}`,
			want: map[string]string{"password": "must be at most 72 bytes"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s := newTestServer(t)
			rec := s.post("/v1/auth/register", tt.body)

			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusUnprocessableEntity, rec.Body)
			}

			got := decode(t, rec.Body.Bytes())
			if got.Code != "validation_failed" {
				t.Errorf("code = %q, want %q", got.Code, "validation_failed")
			}
			if !reflect.DeepEqual(got.Details, tt.want) {
				t.Errorf("details = %v, want %v", got.Details, tt.want)
			}
			if len(s.users.byEmail) != 0 {
				t.Errorf("a user was stored despite the validation failure")
			}
		})
	}
}

func TestMalformedBody(t *testing.T) {
	t.Parallel()

	bodies := map[string]string{
		"not JSON":             `email=alice`,
		"truncated":            `{"email":"a@example.com"`,
		"empty":                ``,
		"wrong type for field": `{"email":"a@example.com","password":12345678,"full_name":"A"}`,
		"array":                `[]`,
	}

	for name, body := range bodies {
		for _, path := range []string{"/v1/auth/register", "/v1/auth/login"} {
			t.Run(name+" "+path, func(t *testing.T) {
				t.Parallel()

				rec := newTestServer(t).post(path, body)

				if rec.Code != http.StatusBadRequest {
					t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
				}
				if got := decode(t, rec.Body.Bytes()); got.Code != codeMalformedRequest {
					t.Errorf("code = %q, want %q", got.Code, codeMalformedRequest)
				}
				// The decoder's own message names Go types.
				if strings.Contains(rec.Body.String(), "registerRequest") || strings.Contains(rec.Body.String(), "Go ") {
					t.Errorf("body leaks the decoder's message: %s", rec.Body)
				}
			})
		}
	}
}

func TestLogin(t *testing.T) {
	t.Parallel()

	s := newTestServer(t)
	s.mustRegister(t)

	// Different case and padding from registration.
	rec := s.post("/v1/auth/login", `{"email":" Alice@Example.com","password":"correct horse"}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}

	var got tokenResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.TokenType != "Bearer" {
		t.Errorf("token_type = %q, want Bearer", got.TokenType)
	}
	if got.ExpiresIn != 900 {
		t.Errorf("expires_in = %d, want 900", got.ExpiresIn)
	}
	if got.User.Email != "alice@example.com" || got.User.Role != domain.RoleCandidate {
		t.Errorf("user = %+v, want alice@example.com as a candidate", got.User)
	}

	// The token must be one this server's TokenManager accepts, for this user.
	claims, err := s.tokens.ParseAccessToken(got.AccessToken)
	if err != nil {
		t.Fatalf("ParseAccessToken() unexpected error: %v", err)
	}
	if claims.Subject != got.User.ID {
		t.Errorf("sub = %q, want the user's id %q", claims.Subject, got.User.ID)
	}
	if claims.Role != domain.RoleCandidate {
		t.Errorf("role claim = %q, want %q", claims.Role, domain.RoleCandidate)
	}

	stored := s.users.byEmail["alice@example.com"]
	if strings.Contains(rec.Body.String(), stored.PasswordHash) || strings.Contains(rec.Body.String(), "correct horse") {
		t.Errorf("response contains the hash or the password: %s", rec.Body)
	}
	if user := keysOf(t, keysOf(t, rec.Body.Bytes())["user"]); len(user) != 5 {
		t.Errorf("user object has %d keys, want 5: %s", len(user), rec.Body)
	}
}

// The heart of login: nothing about the response may reveal whether the
// email exists.
func TestLoginFailuresAreIndistinguishable(t *testing.T) {
	t.Parallel()

	s := newTestServer(t)
	s.mustRegister(t)
	s.verifyCalls = nil

	wrongPassword := s.post("/v1/auth/login", `{"email":"alice@example.com","password":"wrong horse"}`)
	unknownEmail := s.post("/v1/auth/login", `{"email":"nobody@example.com","password":"wrong horse"}`)

	for name, rec := range map[string]*httptest.ResponseRecorder{"wrong password": wrongPassword, "unknown email": unknownEmail} {
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s: status = %d, want %d", name, rec.Code, http.StatusUnauthorized)
		}
		if got := decode(t, rec.Body.Bytes()); got.Code != "invalid_credentials" {
			t.Errorf("%s: code = %q, want invalid_credentials", name, got.Code)
		}
	}

	if wrongPassword.Body.String() != unknownEmail.Body.String() {
		t.Errorf("bodies differ:\n  wrong password: %s\n  unknown email:  %s", wrongPassword.Body, unknownEmail.Body)
	}
	if !reflect.DeepEqual(wrongPassword.Header(), unknownEmail.Header()) {
		t.Errorf("headers differ:\n  wrong password: %v\n  unknown email:  %v", wrongPassword.Header(), unknownEmail.Header())
	}

	// Identical bytes are not enough if one path skips the slow comparison:
	// the response time would give the answer away. Both must compare once.
	if len(s.verifyCalls) != 2 {
		t.Fatalf("password comparisons = %d, want one per request", len(s.verifyCalls))
	}
	if s.verifyCalls[1] != "" {
		t.Errorf("unknown-email path compared against %q, want the empty hash that triggers the dummy comparison", s.verifyCalls[1])
	}
}

func TestLoginValidation(t *testing.T) {
	t.Parallel()

	s := newTestServer(t)
	rec := s.post("/v1/auth/login", `{"email":"not-an-email"}`)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}

	want := map[string]string{"email": "must be a valid email address", "password": "is required"}
	if got := decode(t, rec.Body.Bytes()); !reflect.DeepEqual(got.Details, want) {
		t.Errorf("details = %v, want %v", got.Details, want)
	}
}

// A password shorter than today's minimum must still be able to sign in.
func TestLoginDoesNotApplyThePasswordPolicy(t *testing.T) {
	t.Parallel()

	s := newTestServer(t)

	hash, err := bcrypt.GenerateFromPassword([]byte("old"), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	s.users.byEmail["legacy@example.com"] = &domain.User{
		ID: "legacy", Email: "legacy@example.com", PasswordHash: string(hash), Role: domain.RoleCandidate,
	}

	if rec := s.post("/v1/auth/login", `{"email":"legacy@example.com","password":"old"}`); rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body)
	}
}

func TestStoreFailureIsAnInternalError(t *testing.T) {
	logged := captureLog(t)

	for _, tt := range []struct{ path, body string }{
		{"/v1/auth/register", `{"email":"a@example.com","password":"12345678","full_name":"A"}`},
		{"/v1/auth/login", `{"email":"a@example.com","password":"12345678"}`},
	} {
		s := newTestServer(t)
		s.users.failWith = errors.New(driverMessage)

		rec := s.post(tt.path, tt.body)

		if rec.Code != http.StatusInternalServerError {
			t.Errorf("%s: status = %d, want %d", tt.path, rec.Code, http.StatusInternalServerError)
		}
		if got := decode(t, rec.Body.Bytes()); got.Code != codeInternal {
			t.Errorf("%s: code = %q, want %q", tt.path, got.Code, codeInternal)
		}
		if strings.Contains(rec.Body.String(), "users_email_key") {
			t.Errorf("%s: body leaks the driver message: %s", tt.path, rec.Body)
		}
	}

	if !strings.Contains(logged.String(), "users_email_key") {
		t.Errorf("the cause was not logged: %s", logged)
	}
}
