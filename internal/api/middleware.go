package api

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/moshfiqamin/hiredesk/internal/auth"
	"github.com/moshfiqamin/hiredesk/internal/domain"
)

// contextKey is the type of every key this package stores on a gin.Context.
// Being unexported, no other package can construct a value of it, so no other
// package can read or overwrite what is stored under one.
type contextKey string

// currentUserKey is where RequireAuth leaves the authenticated user.
const currentUserKey contextKey = "currentUser"

// bearerPrefix is the scheme a client puts before its access token:
//
//	Authorization: Bearer eyJhbGciOi...
const bearerPrefix = "Bearer "

// UserGetter loads a user by ID. *store.UserStore satisfies it, as does the
// wider UserStore the auth handlers take.
type UserGetter interface {
	GetUserByID(ctx context.Context, id string) (*domain.User, error)
}

// RequireAuth returns middleware that lets a request through only if it
// carries a valid access token for a user who still exists. It stores that
// user, as read from the database, for CurrentUser to return.
//
// It answers 401 when the credential is missing, malformed, expired, forged,
// or names a user who no longer exists, and 500 when the database cannot be
// asked.
func RequireAuth(tokens *auth.TokenManager, users UserGetter) gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")

		// The scheme name is case-insensitive in the HTTP specification, so
		// "bearer" and "BEARER" are accepted as well.
		if len(header) <= len(bearerPrefix) || !strings.EqualFold(header[:len(bearerPrefix)], bearerPrefix) {
			rejectBearer(c, domain.ErrUnauthenticated)
			return
		}

		claims, err := tokens.ParseAccessToken(header[len(bearerPrefix):])
		if err != nil {
			// The one failure worth distinguishing: it tells a client to use
			// its refresh token instead of sending the user back to the login
			// form. It reveals nothing, since exp is readable in the token.
			if errors.Is(err, auth.ErrExpiredToken) {
				rejectBearer(c, domain.ErrTokenExpired)
				return
			}
			rejectBearer(c, domain.ErrUnauthenticated)
			return
		}

		// The token is only trusted for who the caller is. Their role, and
		// whether they still have an account at all, come from the row.
		user, err := users.GetUserByID(c.Request.Context(), claims.Subject)
		if errors.Is(err, domain.ErrNotFound) {
			rejectBearer(c, domain.ErrUnauthenticated)
			return
		}
		if err != nil {
			// Not a 401: the credential may be fine, and a client told it is
			// not would discard its tokens and send the user to log in again.
			Respond(c, fmt.Errorf("require auth: load user: %w", err))
			return
		}

		c.Set(currentUserKey, user)

		// No c.Next() is needed: when this function returns without aborting,
		// Gin runs the next handler in the chain.
	}
}

// RequireRole returns middleware that lets a request through only if the user
// RequireAuth stored holds one of roles, and answers 403 otherwise. It must be
// registered after RequireAuth.
//
// It panics if roles is empty or names a role that does not exist. Both are
// mistakes in the route table that would otherwise refuse every request, and
// panicking surfaces them when the router is built rather than in production.
func RequireRole(roles ...string) gin.HandlerFunc {
	if len(roles) == 0 {
		panic("api.RequireRole: no roles given")
	}
	for _, role := range roles {
		if !domain.IsRole(role) {
			panic(fmt.Sprintf("api.RequireRole: unknown role %q", role))
		}
	}

	// Copied so a caller that passed a slice with roles... and later changes
	// it cannot alter which roles this route accepts.
	allowed := slices.Clone(roles)

	return func(c *gin.Context) {
		user, ok := CurrentUser(c)
		if !ok {
			// The route was registered without RequireAuth in front, which is
			// a bug in this server, not something the client did.
			Respond(c, errors.New("require role: no authenticated user on the request; is RequireAuth registered first?"))
			return
		}

		if !slices.Contains(allowed, user.Role) {
			Respond(c, domain.ErrForbidden)
			return
		}
	}
}

// CurrentUser returns the user RequireAuth authenticated for this request,
// and false on a route that RequireAuth does not guard.
//
// It returns a copy, so a handler that changes a field cannot affect what
// later middleware on the same request sees.
func CurrentUser(c *gin.Context) (domain.User, bool) {
	value, exists := c.Get(currentUserKey)
	if !exists {
		return domain.User{}, false
	}

	// c.Get returns any. The two-value assertion reports a mismatch through ok
	// instead of panicking.
	user, ok := value.(*domain.User)
	if !ok || user == nil {
		return domain.User{}, false
	}
	return *user, true
}

// rejectBearer answers 401 with the WWW-Authenticate header RFC 6750 requires
// on one, which tells a client which scheme the server expects.
func rejectBearer(c *gin.Context, err error) {
	c.Header("WWW-Authenticate", `Bearer realm="hiredesk"`)
	Respond(c, err)
}
