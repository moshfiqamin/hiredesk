package api

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/moshfiqamin/hiredesk/internal/auth"
	"github.com/moshfiqamin/hiredesk/internal/domain"
)

// UserStore is the part of the user storage the auth handlers need.
//
// It is declared here, by the code that calls it, and *store.UserStore
// satisfies it without saying so. That lets the tests substitute an in-memory
// implementation and run without a database.
type UserStore interface {
	CreateUser(ctx context.Context, in domain.NewUser) (*domain.User, error)
	GetUserByEmail(ctx context.Context, email string) (*domain.User, error)
	GetUserByID(ctx context.Context, id string) (*domain.User, error)
}

// RefreshTokenStore is the part of the refresh-token storage the auth
// handlers need. Every method deals in hashes; see *store.TokenStore for what
// each one guarantees.
type RefreshTokenStore interface {
	CreateRefreshToken(ctx context.Context, userID, tokenHash string, expiresAt time.Time) error
	RotateRefreshToken(ctx context.Context, oldHash, newHash string, newExpiresAt time.Time) (userID string, err error)
	RevokeRefreshToken(ctx context.Context, tokenHash string) error
	RevokeAllRefreshTokensForUser(ctx context.Context, userID string) (int64, error)
}

// AuthHandler serves registration, login, token refresh, logout and the
// current-user endpoint.
type AuthHandler struct {
	users         UserStore
	refreshTokens RefreshTokenStore
	tokens        *auth.TokenManager

	// Fields rather than direct calls to the auth package so the tests can
	// hash at bcrypt's minimum cost; at the real cost each call takes seconds
	// under the race detector.
	hashPassword   func(plain string) (string, error)
	verifyPassword func(hash, plain string) bool

	// now is the clock used to set a refresh token's expiry, replaceable in
	// tests for the same reason as TokenManager's.
	now func() time.Time
}

// NewAuthHandler returns an AuthHandler that stores users in users, refresh
// tokens in refreshTokens, and signs access tokens with tokens.
func NewAuthHandler(users UserStore, refreshTokens RefreshTokenStore, tokens *auth.TokenManager) *AuthHandler {
	return &AuthHandler{
		users:          users,
		refreshTokens:  refreshTokens,
		tokens:         tokens,
		hashPassword:   auth.Hash,
		verifyPassword: auth.Verify,
		now:            time.Now,
	}
}

// registerRequest is the body of POST /v1/auth/register.
//
// There is no role field. Anything a client sends under "role" is dropped by
// the JSON decoder, so a request cannot ask to be anything but a candidate.
type registerRequest struct {
	Email    string `json:"email" validate:"required,email,max=254"`
	Password string `json:"password" validate:"required,min=8,max=72"`
	FullName string `json:"full_name" validate:"required,max=200"`
}

// loginRequest is the body of POST /v1/auth/login.
//
// The password has no length rule here. Login checks a password against a
// stored hash; it does not judge it, and an account created under an older,
// shorter minimum must still be able to sign in.
type loginRequest struct {
	Email    string `json:"email" validate:"required,email"`
	Password string `json:"password" validate:"required"`
}

// userResponse is a user as a client sees one. It is a separate type from
// domain.User so that a field reaches a response only by being added here.
type userResponse struct {
	ID        string    `json:"id"`
	Email     string    `json:"email"`
	FullName  string    `json:"full_name"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"created_at"`
}

func newUserResponse(u *domain.User) userResponse {
	return userResponse{
		ID:       u.ID,
		Email:    u.Email,
		FullName: u.FullName,
		Role:     u.Role,
		// The driver returns timestamps in the server's local zone. UTC keeps
		// the API's output the same wherever it is deployed.
		CreatedAt: u.CreatedAt.UTC(),
	}
}

// refreshRequest is the body of POST /v1/auth/refresh and /v1/auth/logout.
type refreshRequest struct {
	RefreshToken string `json:"refresh_token" validate:"required"`
}

// tokenResponse is what login and refresh return. It follows the field names
// of OAuth 2.0 (RFC 6749 §5.1), which client libraries already know how to
// read.
type tokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`

	// ExpiresIn is the access token's lifetime in seconds, so a client can
	// schedule its refresh without decoding the token.
	ExpiresIn int `json:"expires_in"`

	// RefreshToken is shown exactly once. The server keeps only its hash and
	// could not repeat it if asked.
	RefreshToken string       `json:"refresh_token"`
	User         userResponse `json:"user"`
}

// normalizeEmail trims surrounding whitespace and lowercases, so that
// " Alice@Example.com" and "alice@example.com" are one account.
func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// Register handles POST /v1/auth/register. It creates a candidate account and
// returns it with status 201.
func (h *AuthHandler) Register(c *gin.Context) {
	var req registerRequest
	if !decodeJSON(c, &req) {
		return
	}

	// Before validation, so the email rule judges the cleaned value. The
	// password is left exactly as sent: a space can be part of a password.
	req.Email = normalizeEmail(req.Email)
	req.FullName = strings.TrimSpace(req.FullName)

	if err := validateStruct(req); err != nil {
		Respond(c, err)
		return
	}

	hash, err := h.hashPassword(req.Password)
	if err != nil {
		// The max=72 rule counts characters and bcrypt's limit is 72 bytes,
		// so a password of multi-byte characters can pass one and fail the
		// other. That is still the client's mistake, not a server error.
		if errors.Is(err, auth.ErrPasswordTooLong) {
			var invalid domain.ValidationError
			invalid.Add("password", fmt.Sprintf("must be at most %d bytes", auth.MaxPasswordBytes))
			Respond(c, invalid.Err())
			return
		}
		Respond(c, fmt.Errorf("register: hash password: %w", err))
		return
	}

	// No "does this email exist?" query first. Two requests could both pass
	// such a check before either inserts; the unique index is the only thing
	// that can decide, and the store reports its verdict as ErrDuplicate.
	user, err := h.users.CreateUser(c.Request.Context(), domain.NewUser{
		Email:        req.Email,
		PasswordHash: hash,
		FullName:     req.FullName,
		Role:         domain.RoleCandidate,
	})
	if err != nil {
		Respond(c, fmt.Errorf("register: %w", err))
		return
	}

	c.JSON(http.StatusCreated, newUserResponse(user))
}

// Login handles POST /v1/auth/login. It returns an access token and the user,
// or the same 401 whether the email is unknown or the password is wrong.
func (h *AuthHandler) Login(c *gin.Context) {
	var req loginRequest
	if !decodeJSON(c, &req) {
		return
	}

	req.Email = normalizeEmail(req.Email)

	if err := validateStruct(req); err != nil {
		Respond(c, err)
		return
	}

	user, err := h.users.GetUserByEmail(c.Request.Context(), req.Email)
	if errors.Is(err, domain.ErrNotFound) {
		// There is no hash to check, and returning now would make this path
		// answer in microseconds where a wrong password takes a full bcrypt
		// comparison. Verify with an empty hash does the same amount of work
		// against a built-in dummy and always reports false.
		h.verifyPassword("", req.Password)

		Respond(c, domain.ErrInvalidCredentials)
		return
	}
	if err != nil {
		Respond(c, fmt.Errorf("login: %w", err))
		return
	}

	if !h.verifyPassword(user.PasswordHash, req.Password) {
		Respond(c, domain.ErrInvalidCredentials)
		return
	}

	refreshToken, refreshHash, err := auth.NewRefreshToken()
	if err != nil {
		Respond(c, fmt.Errorf("login: %w", err))
		return
	}

	// Each login starts its own chain of refresh tokens, so a user can be
	// signed in on a phone and a laptop at once.
	err = h.refreshTokens.CreateRefreshToken(c.Request.Context(), user.ID, refreshHash, h.now().Add(auth.RefreshTokenTTL))
	if err != nil {
		Respond(c, fmt.Errorf("login: %w", err))
		return
	}

	h.respondWithTokens(c, user, refreshToken)
}

// Refresh handles POST /v1/auth/refresh. It exchanges a refresh token for a
// new access token and a new refresh token; the one presented stops working.
//
// Presenting a token that was already exchanged or logged out ends every
// session the user has. Every failure returns the same 401.
func (h *AuthHandler) Refresh(c *gin.Context) {
	var req refreshRequest
	if !decodeJSON(c, &req) {
		return
	}
	if err := validateStruct(req); err != nil {
		Respond(c, err)
		return
	}

	// A string that is not 32 bytes of base64url was never issued by this
	// server, so it is refused without a database query.
	oldHash, ok := auth.HashRefreshToken(req.RefreshToken)
	if !ok {
		Respond(c, domain.ErrUnauthenticated)
		return
	}

	// Generated before the exchange because the store swaps old for new in
	// one transaction and needs the new hash to do it.
	newToken, newHash, err := auth.NewRefreshToken()
	if err != nil {
		Respond(c, fmt.Errorf("refresh: %w", err))
		return
	}

	ctx := c.Request.Context()

	userID, err := h.refreshTokens.RotateRefreshToken(ctx, oldHash, newHash, h.now().Add(auth.RefreshTokenTTL))
	if err != nil {
		var reuse *domain.TokenReuseError

		switch {
		case errors.As(err, &reuse):
			// The token was valid once and has been used or logged out since.
			// Two parties hold it and there is no telling which is the thief,
			// so every session of this user ends, including the one the
			// earlier exchange created.
			revoked, revokeErr := h.refreshTokens.RevokeAllRefreshTokensForUser(ctx, reuse.UserID)
			if revokeErr != nil {
				// Still a refusal. The presented token stays revoked, so
				// nothing is granted; the failure to finish is what gets
				// logged and reported as a 500.
				Respond(c, fmt.Errorf("refresh: reuse detected for user %s: %w", reuse.UserID, revokeErr))
				return
			}

			// A security event, so it is logged although the response is an
			// ordinary 401. No token or hash is written.
			log.Printf("security: refresh token reuse detected for user %s from %s; revoked %d session(s)",
				reuse.UserID, c.ClientIP(), revoked)

			Respond(c, domain.ErrUnauthenticated)

		case errors.Is(err, domain.ErrNotFound), errors.Is(err, domain.ErrTokenExpired):
			// Unknown and expired get the same answer as reuse: a caller
			// learns only that this token will not work.
			Respond(c, domain.ErrUnauthenticated)

		default:
			Respond(c, fmt.Errorf("refresh: %w", err))
		}
		return
	}

	// Read again rather than trusted from the old access token, so a role
	// changed since the last login takes effect at the next refresh.
	user, err := h.users.GetUserByID(ctx, userID)
	if errors.Is(err, domain.ErrNotFound) {
		Respond(c, domain.ErrUnauthenticated)
		return
	}
	if err != nil {
		Respond(c, fmt.Errorf("refresh: %w", err))
		return
	}

	h.respondWithTokens(c, user, newToken)
}

// Logout handles POST /v1/auth/logout. It revokes the presented refresh token
// and returns 204 whether or not that token was known, so the endpoint cannot
// be used to test which tokens exist.
func (h *AuthHandler) Logout(c *gin.Context) {
	var req refreshRequest
	if !decodeJSON(c, &req) {
		return
	}
	if err := validateStruct(req); err != nil {
		Respond(c, err)
		return
	}

	if hash, ok := auth.HashRefreshToken(req.RefreshToken); ok {
		if err := h.refreshTokens.RevokeRefreshToken(c.Request.Context(), hash); err != nil {
			Respond(c, fmt.Errorf("logout: %w", err))
			return
		}
	}

	// 204: it worked and there is nothing to say.
	c.Status(http.StatusNoContent)
}

// Me handles GET /v1/me. It returns the authenticated user, and must be
// registered behind RequireAuth.
func (h *AuthHandler) Me(c *gin.Context) {
	user, ok := CurrentUser(c)
	if !ok {
		// Only reachable if the route was registered without RequireAuth,
		// which is a bug here and not something the client did.
		Respond(c, errors.New("me: no authenticated user on the request; is the route behind RequireAuth?"))
		return
	}

	// RequireAuth read the row on this request, so this is current.
	c.JSON(http.StatusOK, newUserResponse(&user))
}

// respondWithTokens signs an access token for user and sends it with
// refreshToken, which the caller has already stored.
func (h *AuthHandler) respondWithTokens(c *gin.Context, user *domain.User, refreshToken string) {
	accessToken, err := h.tokens.NewAccessToken(user.ID, user.Role)
	if err != nil {
		Respond(c, fmt.Errorf("issue access token: %w", err))
		return
	}

	// Tokens must not be kept by a browser cache or a proxy in between.
	c.Header("Cache-Control", "no-store")

	c.JSON(http.StatusOK, tokenResponse{
		AccessToken:  accessToken,
		TokenType:    "Bearer",
		ExpiresIn:    int(auth.AccessTokenTTL.Seconds()),
		RefreshToken: refreshToken,
		User:         newUserResponse(user),
	})
}
