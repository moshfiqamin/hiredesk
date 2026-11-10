package api

import (
	"fmt"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/moshfiqamin/hiredesk/internal/auth"
)

// Deps is everything the router needs from outside this package. main builds
// it once; nothing in here reaches for configuration or opens a connection.
type Deps struct {
	Auth *AuthHandler

	// Tokens verifies the access token on every protected route.
	Tokens *auth.TokenManager

	// DB is pinged by /readyz, for at most PingTimeout.
	DB          Pinger
	PingTimeout time.Duration
}

// NewRouter builds the engine with every middleware and route attached.
//
// Call gin.SetMode before this if the mode is to change: the engine reads it
// while it is being built.
func NewRouter(deps Deps) (*gin.Engine, error) {
	// gin.Default() with one substitution: its recovery middleware answers a
	// panic with an empty body, and Recovery answers with the envelope.
	router := gin.New()
	router.Use(gin.Logger(), Recovery())

	// Off by default, in which case a wrong method on a real path is reported
	// as a 404.
	router.HandleMethodNotAllowed = true
	router.NoRoute(NoRoute)
	router.NoMethod(NoMethod)

	// Trust no proxy headers, so c.ClientIP() cannot be spoofed via
	// X-Forwarded-For. Behind a reverse proxy, replace nil with its CIDR.
	if err := router.SetTrustedProxies(nil); err != nil {
		return nil, fmt.Errorf("configure trusted proxies: %w", err)
	}

	// Probes stay outside /v1. They are a contract with the platform running
	// the process, not with API clients, and do not change with the API.
	router.GET("/healthz", healthz)
	router.GET("/readyz", readyz(deps.DB, deps.PingTimeout))

	// Everything a client calls lives under a version prefix. A breaking
	// change then ships as /v2 beside /v1, and existing clients keep working
	// until they choose to move.
	v1 := router.Group("/v1")

	// Braces only to indent the routes under their group; they do not affect
	// behaviour.
	authRoutes := v1.Group("/auth")
	{
		authRoutes.POST("/register", deps.Auth.Register)
		authRoutes.POST("/login", deps.Auth.Login)

		// Neither of these is behind requireAuth. Refresh exists for the
		// moment the access token has expired, and logout must work then too.
		// The refresh token in the body is the credential.
		authRoutes.POST("/refresh", deps.Auth.Refresh)
		authRoutes.POST("/logout", deps.Auth.Logout)
	}

	// Everything registered on this group runs requireAuth first, so a handler
	// here can rely on verified claims being present.
	protected := v1.Group("", requireAuth(deps.Tokens))
	{
		protected.GET("/me", deps.Auth.Me)
	}

	return router, nil
}
