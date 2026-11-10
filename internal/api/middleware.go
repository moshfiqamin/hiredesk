package api

import (
	"errors"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/moshfiqamin/hiredesk/internal/auth"
	"github.com/moshfiqamin/hiredesk/internal/domain"
)

// claimsKey is where requireAuth leaves the verified claims on the request's
// context for the handlers behind it.
const claimsKey = "auth.claims"

// bearerPrefix is the scheme a client puts before its access token:
//
//	Authorization: Bearer eyJhbGciOi...
const bearerPrefix = "Bearer "

// requireAuth returns middleware that lets a request through only if it
// carries a valid access token, and answers 401 otherwise.
func requireAuth(tokens *auth.TokenManager) gin.HandlerFunc {
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

		// Returning without aborting lets Gin run the next handler in the
		// chain, which finds the claims here.
		c.Set(claimsKey, claims)
	}
}

// rejectBearer answers 401 with the WWW-Authenticate header RFC 6750 requires
// on one, which tells a client which scheme the server expects.
func rejectBearer(c *gin.Context, err error) {
	c.Header("WWW-Authenticate", `Bearer realm="hiredesk"`)
	Respond(c, err)
}

// claimsFrom returns the claims requireAuth verified for this request, and
// false on a route that requireAuth does not guard.
func claimsFrom(c *gin.Context) (*auth.Claims, bool) {
	value, exists := c.Get(claimsKey)
	if !exists {
		return nil, false
	}

	// c.Get returns any. The two-value assertion reports failure through ok
	// instead of panicking if something else was ever stored under the key.
	claims, ok := value.(*auth.Claims)
	return claims, ok
}
