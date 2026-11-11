package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/moshfiqamin/hiredesk/internal/auth"
	"github.com/moshfiqamin/hiredesk/internal/domain"
)

// fakeToken is one stored refresh token.
type fakeToken struct {
	userID    string
	expiresAt time.Time
	revoked   bool
}

// fakeRefreshTokens is an in-memory RefreshTokenStore. It follows the rules
// of *store.TokenStore, which the integration tests check against Postgres:
// revoked is reported before expired, a failed rotation writes nothing, and a
// successful one revokes the old token and stores the new together.
type fakeRefreshTokens struct {
	mu     sync.Mutex
	byHash map[string]*fakeToken
	now    func() time.Time

	// failWith, when set, is returned by every call.
	failWith error

	// failRevokeAll, when set, is returned by RevokeAllRefreshTokensForUser
	// only, to test the path where reuse is detected and the response to it
	// cannot be completed.
	failRevokeAll error
}

func newFakeRefreshTokens(now func() time.Time) *fakeRefreshTokens {
	return &fakeRefreshTokens{byHash: make(map[string]*fakeToken), now: now}
}

func (f *fakeRefreshTokens) CreateRefreshToken(_ context.Context, userID, tokenHash string, expiresAt time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.failWith != nil {
		return f.failWith
	}
	f.byHash[tokenHash] = &fakeToken{userID: userID, expiresAt: expiresAt}
	return nil
}

func (f *fakeRefreshTokens) RotateRefreshToken(_ context.Context, oldHash, newHash string, newExpiresAt time.Time) (string, error) {
	// One lock held for the whole exchange stands in for the row lock and
	// transaction of the real store.
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.failWith != nil {
		return "", f.failWith
	}

	old, ok := f.byHash[oldHash]
	if !ok {
		return "", domain.ErrNotFound
	}
	if old.revoked {
		return "", &domain.TokenReuseError{UserID: old.userID}
	}
	if !old.expiresAt.After(f.now()) {
		return "", domain.ErrTokenExpired
	}

	old.revoked = true
	f.byHash[newHash] = &fakeToken{userID: old.userID, expiresAt: newExpiresAt}
	return old.userID, nil
}

func (f *fakeRefreshTokens) RevokeRefreshToken(_ context.Context, tokenHash string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.failWith != nil {
		return f.failWith
	}
	if token, ok := f.byHash[tokenHash]; ok {
		token.revoked = true
	}
	return nil
}

func (f *fakeRefreshTokens) RevokeAllRefreshTokensForUser(_ context.Context, userID string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.failWith != nil {
		return 0, f.failWith
	}
	if f.failRevokeAll != nil {
		return 0, f.failRevokeAll
	}

	var revoked int64
	for _, token := range f.byHash {
		if token.userID == userID && !token.revoked {
			token.revoked = true
			revoked++
		}
	}
	return revoked, nil
}

// live returns how many of the user's tokens are still usable.
func (f *fakeRefreshTokens) live(userID string) int {
	f.mu.Lock()
	defer f.mu.Unlock()

	count := 0
	for _, token := range f.byHash {
		if token.userID == userID && !token.revoked {
			count++
		}
	}
	return count
}

// register creates an account and returns nothing; login is separate so a
// test can open several sessions for one user.
func (s *testServer) register(t *testing.T, email string) {
	t.Helper()

	rec := s.post("/v1/auth/register", `{"email":"`+email+`","password":"correct horse","full_name":"Test User"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("register %s: status = %d, body = %s", email, rec.Code, rec.Body)
	}
}

// login signs in and returns the token pair.
func (s *testServer) login(t *testing.T, email string) tokenResponse {
	t.Helper()

	rec := s.post("/v1/auth/login", `{"email":"`+email+`","password":"correct horse"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("login %s: status = %d, body = %s", email, rec.Code, rec.Body)
	}

	var pair tokenResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &pair); err != nil {
		t.Fatalf("decode login response: %v", err)
	}
	return pair
}

// refresh presents a refresh token.
func (s *testServer) refreshWith(token string) *httptest.ResponseRecorder {
	return s.post("/v1/auth/refresh", `{"refresh_token":"`+token+`"}`)
}

// mustRefresh presents a refresh token that is expected to work.
func (s *testServer) mustRefresh(t *testing.T, token string) tokenResponse {
	t.Helper()

	rec := s.refreshWith(token)
	if rec.Code != http.StatusOK {
		t.Fatalf("refresh: status = %d, body = %s", rec.Code, rec.Body)
	}

	var pair tokenResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &pair); err != nil {
		t.Fatalf("decode refresh response: %v", err)
	}
	return pair
}

// get sends a GET with an Authorization header, or none if header is empty.
func (s *testServer) get(path, header string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if header != "" {
		req.Header.Set("Authorization", header)
	}
	s.engine.ServeHTTP(rec, req)
	return rec
}

// wantUnauthenticated fails unless rec is the standard refusal.
func wantUnauthenticated(t *testing.T, what string, rec *httptest.ResponseRecorder) {
	t.Helper()

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("%s: status = %d, want %d; body = %s", what, rec.Code, http.StatusUnauthorized, rec.Body)
		return
	}
	if got := decode(t, rec.Body.Bytes()); got.Code != "unauthenticated" {
		t.Errorf("%s: code = %q, want unauthenticated", what, got.Code)
	}
}

func TestLoginIssuesARefreshTokenAndStoresOnlyItsHash(t *testing.T) {
	t.Parallel()

	s := newTestServer(t)
	s.register(t, "alice@example.com")
	pair := s.login(t, "alice@example.com")

	if len(pair.RefreshToken) != 43 {
		t.Fatalf("refresh_token = %q, want 43 characters of base64url", pair.RefreshToken)
	}
	if pair.RefreshToken == pair.AccessToken {
		t.Errorf("the refresh token and the access token are the same string")
	}

	if len(s.refresh.byHash) != 1 {
		t.Fatalf("stored tokens = %d, want 1", len(s.refresh.byHash))
	}
	for hash, stored := range s.refresh.byHash {
		if hash == pair.RefreshToken || strings.Contains(hash, pair.RefreshToken) {
			t.Errorf("the store was given the token itself, not a hash")
		}
		want, _ := auth.HashRefreshToken(pair.RefreshToken)
		if hash != want {
			t.Errorf("stored under %q, want the token's SHA-256 %q", hash, want)
		}
		if want := s.clock.Add(30 * 24 * time.Hour); !stored.expiresAt.Equal(want) {
			t.Errorf("expires at %v, want 30 days after login, %v", stored.expiresAt, want)
		}
	}
}

func TestRefreshRotates(t *testing.T) {
	t.Parallel()

	s := newTestServer(t)
	s.register(t, "alice@example.com")
	first := s.login(t, "alice@example.com")

	rec := s.refreshWith(first.RefreshToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}

	var second tokenResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &second); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if second.RefreshToken == first.RefreshToken {
		t.Errorf("refresh returned the same refresh token; it must be replaced")
	}
	if second.AccessToken == first.AccessToken {
		t.Errorf("refresh returned the same access token")
	}
	if second.User.Email != "alice@example.com" {
		t.Errorf("user = %+v, want alice", second.User)
	}

	claims, err := s.tokens.ParseAccessToken(second.AccessToken)
	if err != nil {
		t.Fatalf("the new access token does not verify: %v", err)
	}
	if claims.Subject != first.User.ID {
		t.Errorf("sub = %q, want %q", claims.Subject, first.User.ID)
	}

	// Exactly one usable token before, exactly one after: the old one is
	// gone and the new one took its place.
	if live := s.refresh.live(first.User.ID); live != 1 {
		t.Errorf("usable refresh tokens = %d, want 1", live)
	}

	// The chain continues from the new token.
	third := s.mustRefresh(t, second.RefreshToken)
	if third.RefreshToken == second.RefreshToken {
		t.Errorf("second rotation returned the same token")
	}
}

// The scenario the whole design exists for. A thief copies a refresh token.
// Whoever uses it second is caught, and every session ends — including the
// one the first use created.
func TestRefreshReplayRevokesEverySession(t *testing.T) {
	logged := captureLog(t)

	s := newTestServer(t)
	s.register(t, "alice@example.com")
	s.register(t, "bob@example.com")

	phone := s.login(t, "alice@example.com")
	laptop := s.login(t, "alice@example.com")
	bob := s.login(t, "bob@example.com")

	// The first use succeeds. It does not matter whether this is the owner
	// or the thief: the server cannot tell, and does not need to.
	rotated := s.mustRefresh(t, phone.RefreshToken)

	// The second use of the same token.
	replay := s.refreshWith(phone.RefreshToken)
	wantUnauthenticated(t, "replayed token", replay)

	if live := s.refresh.live(phone.User.ID); live != 0 {
		t.Errorf("alice has %d usable refresh tokens after a replay, want 0", live)
	}

	// The token the first use obtained is dead. Had only the replayed token
	// been revoked, whoever got here first would keep their session.
	wantUnauthenticated(t, "token issued by the first use", s.refreshWith(rotated.RefreshToken))

	// So is the unrelated session on the other device.
	wantUnauthenticated(t, "the user's other session", s.refreshWith(laptop.RefreshToken))

	// Another user is untouched.
	if live := s.refresh.live(bob.User.ID); live != 1 {
		t.Errorf("bob has %d usable refresh tokens, want 1: revoking must not cross users", live)
	}
	s.mustRefresh(t, bob.RefreshToken)

	// Logging in again works: the account is not locked, only signed out.
	fresh := s.login(t, "alice@example.com")
	s.mustRefresh(t, fresh.RefreshToken)

	if !strings.Contains(logged.String(), "refresh token reuse detected for user "+phone.User.ID) {
		t.Errorf("the reuse was not logged as a security event: %s", logged)
	}
	if !strings.Contains(logged.String(), "revoked 2 session(s)") {
		t.Errorf("log does not report the two sessions revoked: %s", logged)
	}
	for _, secret := range []string{phone.RefreshToken, rotated.RefreshToken, laptop.RefreshToken} {
		hash, _ := auth.HashRefreshToken(secret)
		if strings.Contains(logged.String(), secret) || strings.Contains(logged.String(), hash) {
			t.Errorf("the log contains a refresh token or its hash")
		}
	}
}

// A caller must not be able to tell a stolen-and-replayed token from an
// expired one or one that never existed.
func TestRefreshFailuresAreIndistinguishable(t *testing.T) {
	captureLog(t)

	s := newTestServer(t)
	s.register(t, "alice@example.com")

	replayed := s.login(t, "alice@example.com")
	s.mustRefresh(t, replayed.RefreshToken)

	loggedOut := s.login(t, "alice@example.com")
	if rec := s.post("/v1/auth/logout", `{"refresh_token":"`+loggedOut.RefreshToken+`"}`); rec.Code != http.StatusNoContent {
		t.Fatalf("logout: status = %d", rec.Code)
	}

	// Never issued, but the right shape.
	unknown, _, err := auth.NewRefreshToken()
	if err != nil {
		t.Fatalf("NewRefreshToken() unexpected error: %v", err)
	}

	expired := s.login(t, "alice@example.com")

	responses := map[string]*httptest.ResponseRecorder{
		"replayed":         s.refreshWith(replayed.RefreshToken),
		"logged out":       s.refreshWith(loggedOut.RefreshToken),
		"never issued":     s.refreshWith(unknown),
		"not even a token": s.refreshWith("garbage"),
		"an access token":  s.refreshWith(expired.AccessToken),
	}

	// Checked last, after the clock moves past the 30 days.
	s.clock = s.clock.Add(auth.RefreshTokenTTL)
	responses["expired at exactly 30 days"] = s.refreshWith(expired.RefreshToken)

	var reference string
	for name, rec := range responses {
		wantUnauthenticated(t, name, rec)

		if reference == "" {
			reference = rec.Body.String()
		}
		if rec.Body.String() != reference {
			t.Errorf("%s: body = %s, want the same as every other failure, %s", name, rec.Body, reference)
		}
	}
}

func TestRefreshJustBeforeExpiry(t *testing.T) {
	t.Parallel()

	s := newTestServer(t)
	s.register(t, "alice@example.com")
	pair := s.login(t, "alice@example.com")

	s.clock = s.clock.Add(auth.RefreshTokenTTL - time.Second)

	// The access token is long expired by now; refresh needs only the
	// refresh token.
	s.mustRefresh(t, pair.RefreshToken)
}

func TestRefreshValidation(t *testing.T) {
	t.Parallel()

	for _, path := range []string{"/v1/auth/refresh", "/v1/auth/logout"} {
		rec := newTestServer(t).post(path, `{}`)

		if rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("%s: status = %d, want %d", path, rec.Code, http.StatusUnprocessableEntity)
			continue
		}
		if got := decode(t, rec.Body.Bytes()); got.detail("refresh_token") != "is required" {
			t.Errorf("%s: details = %v, want refresh_token is required", path, got.Details)
		}
	}
}

// If reuse is detected and the mass revocation then fails, the caller must
// still get nothing, and the failure must be loud.
func TestRefreshReplayWhenRevocationFails(t *testing.T) {
	logged := captureLog(t)

	s := newTestServer(t)
	s.register(t, "alice@example.com")
	pair := s.login(t, "alice@example.com")
	s.mustRefresh(t, pair.RefreshToken)

	s.refresh.failRevokeAll = errors.New(driverMessage)

	rec := s.refreshWith(pair.RefreshToken)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
	if strings.Contains(rec.Body.String(), "access_token") || strings.Contains(rec.Body.String(), "refresh_token") {
		t.Errorf("tokens were issued although the token was a replay: %s", rec.Body)
	}
	if !strings.Contains(logged.String(), "reuse detected for user "+pair.User.ID) {
		t.Errorf("the log does not say what was being attempted: %s", logged)
	}
}

func TestLogout(t *testing.T) {
	t.Parallel()

	s := newTestServer(t)
	s.register(t, "alice@example.com")

	phone := s.login(t, "alice@example.com")
	laptop := s.login(t, "alice@example.com")

	rec := s.post("/v1/auth/logout", `{"refresh_token":"`+phone.RefreshToken+`"}`)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusNoContent, rec.Body)
	}
	if rec.Body.Len() != 0 {
		t.Errorf("body = %q, want empty", rec.Body)
	}

	// Only the presented token: logging out of the phone leaves the laptop.
	if live := s.refresh.live(phone.User.ID); live != 1 {
		t.Errorf("usable refresh tokens = %d, want 1", live)
	}
	s.mustRefresh(t, laptop.RefreshToken)
}

// Logout must answer the same for a token that exists and one that does not,
// or it becomes a way to test guesses.
func TestLogoutIsIdempotentAndRevealsNothing(t *testing.T) {
	t.Parallel()

	s := newTestServer(t)
	s.register(t, "alice@example.com")
	pair := s.login(t, "alice@example.com")

	unknown, _, err := auth.NewRefreshToken()
	if err != nil {
		t.Fatalf("NewRefreshToken() unexpected error: %v", err)
	}

	for name, token := range map[string]string{
		"first logout":     pair.RefreshToken,
		"second logout":    pair.RefreshToken,
		"never issued":     unknown,
		"not even a token": "garbage",
	} {
		rec := s.post("/v1/auth/logout", `{"refresh_token":"`+token+`"}`)
		if rec.Code != http.StatusNoContent || rec.Body.Len() != 0 {
			t.Errorf("%s: status = %d, body = %q; want 204 and no body", name, rec.Code, rec.Body)
		}
	}
}

func TestMe(t *testing.T) {
	t.Parallel()

	s := newTestServer(t)
	s.register(t, "alice@example.com")
	pair := s.login(t, "alice@example.com")

	for _, scheme := range []string{"Bearer ", "bearer ", "BEARER "} {
		rec := s.get("/v1/me", scheme+pair.AccessToken)

		if rec.Code != http.StatusOK {
			t.Fatalf("%q: status = %d, want %d; body = %s", scheme, rec.Code, http.StatusOK, rec.Body)
		}

		var user userResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &user); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if user.ID != pair.User.ID || user.Email != "alice@example.com" || user.Role != domain.RoleCandidate {
			t.Errorf("user = %+v, want alice", user)
		}
		if len(keysOf(t, rec.Body.Bytes())) != 5 {
			t.Errorf("response has more than the five public fields: %s", rec.Body)
		}
		if got := rec.Header().Get("WWW-Authenticate"); got != "" {
			t.Errorf("WWW-Authenticate = %q on a success, want it absent", got)
		}
	}
}

func TestMeRejectsBadCredentials(t *testing.T) {
	t.Parallel()

	s := newTestServer(t)
	s.register(t, "alice@example.com")
	pair := s.login(t, "alice@example.com")

	// Tokens made outside the server, the way an attacker would.
	sign := func(method jwt.SigningMethod, key any, expiresIn time.Duration) string {
		claims := auth.Claims{
			Role: domain.RoleAdmin,
			RegisteredClaims: jwt.RegisteredClaims{
				Subject:   pair.User.ID,
				IssuedAt:  jwt.NewNumericDate(time.Now()),
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(expiresIn)),
			},
		}
		token, err := jwt.NewWithClaims(method, claims).SignedString(key)
		if err != nil {
			t.Fatalf("sign %s token: %v", method.Alg(), err)
		}
		return token
	}

	parts := strings.Split(pair.AccessToken, ".")

	tests := []struct {
		name     string
		header   string
		wantCode string
	}{
		{"no header", "", "unauthenticated"},
		{"scheme only", "Bearer", "unauthenticated"},
		{"scheme and a space", "Bearer ", "unauthenticated"},
		{"token without a scheme", pair.AccessToken, "unauthenticated"},
		{"basic auth", "Basic YWxpY2U6cGFzc3dvcmQ=", "unauthenticated"},
		{"not a token", "Bearer garbage", "unauthenticated"},
		{"the refresh token", "Bearer " + pair.RefreshToken, "unauthenticated"},
		{"signature removed", "Bearer " + parts[0] + "." + parts[1] + ".", "unauthenticated"},
		{"signed with another secret", "Bearer " + sign(jwt.SigningMethodHS256, []byte("a-different-signing-key-9876543210-zyxwvutsrq"), time.Hour), "unauthenticated"},
		{"alg none", "Bearer " + sign(jwt.SigningMethodNone, jwt.UnsafeAllowNoneSignatureType, time.Hour), "unauthenticated"},
		{"correctly signed but expired", "Bearer " + sign(jwt.SigningMethodHS256, []byte(testJWTSecret), -time.Minute), "token_expired"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec := s.get("/v1/me", tt.header)

			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusUnauthorized, rec.Body)
			}
			if got := decode(t, rec.Body.Bytes()); got.Code != tt.wantCode {
				t.Errorf("code = %q, want %q", got.Code, tt.wantCode)
			}
			if got := rec.Header().Get("WWW-Authenticate"); !strings.HasPrefix(got, "Bearer") {
				t.Errorf("WWW-Authenticate = %q, want a Bearer challenge", got)
			}
			if strings.Contains(rec.Body.String(), "alice") {
				t.Errorf("a refused request was told about the user: %s", rec.Body)
			}
		})
	}
}

// A token is valid for fifteen minutes whatever happens to its owner, so the
// handler must not answer from the token alone.
func TestMeForADeletedUser(t *testing.T) {
	t.Parallel()

	s := newTestServer(t)
	s.register(t, "alice@example.com")
	pair := s.login(t, "alice@example.com")

	delete(s.users.byEmail, "alice@example.com")

	wantUnauthenticated(t, "deleted user", s.get("/v1/me", "Bearer "+pair.AccessToken))
}
