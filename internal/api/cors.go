package api

import (
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// The CORS answers this API gives. Every list is written out rather than
// echoed from the request, so a browser is told exactly what the API uses.
var (
	corsAllowMethods = strings.Join([]string{
		http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete,
	}, ", ")

	// Authorization for the bearer token, Content-Type for JSON bodies,
	// X-Request-ID so a frontend can supply its own correlation ID.
	corsAllowHeaders = strings.Join([]string{"Authorization", "Content-Type", RequestIDHeader}, ", ")

	// Response headers a browser hides from scripts unless listed here.
	corsExposeHeaders = strings.Join([]string{RequestIDHeader, "Retry-After", "WWW-Authenticate"}, ", ")
)

// corsMaxAge is how long, in seconds, a browser may reuse a preflight answer
// before asking again. Ten minutes is Chromium's own ceiling.
const corsMaxAge = 600

// CORS returns middleware that lets the listed origins call the API from a
// browser, with credentials, and nobody else. An empty list allows no
// cross-origin calls at all; same-origin pages, such as /docs, never need
// CORS.
//
// It must run before authentication, so that a 401 still carries the headers
// a browser needs to show the script the error rather than a CORS failure.
func CORS(allowedOrigins []string) gin.HandlerFunc {
	allowed := slices.Clone(allowedOrigins)

	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if origin == "" {
			// Not a cross-origin browser request: curl, a server, or a
			// same-origin page. Nothing to do.
			return
		}

		// The answer differs by Origin, so a shared cache must not serve one
		// origin's response to another. Set even for refused origins.
		c.Header("Vary", "Origin")

		if !slices.Contains(allowed, origin) {
			// No CORS headers: the browser withholds the response from the
			// calling script. The request itself is still served normally;
			// CORS is enforced by the browser, never by the server, which is
			// why it is no substitute for authentication.
			return
		}

		// The one origin that asked, never "*": with credentials allowed the
		// specification forbids the wildcard, and browsers reject it.
		c.Header("Access-Control-Allow-Origin", origin)
		c.Header("Access-Control-Allow-Credentials", "true")
		c.Header("Access-Control-Expose-Headers", corsExposeHeaders)

		// A preflight is an OPTIONS request naming the method it is asking
		// about. It is answered here, in full, and goes no further: no route
		// is registered for OPTIONS.
		if c.Request.Method == http.MethodOptions && c.GetHeader("Access-Control-Request-Method") != "" {
			c.Header("Access-Control-Allow-Methods", corsAllowMethods)
			c.Header("Access-Control-Allow-Headers", corsAllowHeaders)
			c.Header("Access-Control-Max-Age", strconv.Itoa(corsMaxAge))
			c.AbortWithStatus(http.StatusNoContent)
		}
	}
}
