package api

import (
	"errors"
	"fmt"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/moshfiqamin/hiredesk/internal/auth"
	"github.com/moshfiqamin/hiredesk/internal/domain"
)

// Deps is everything the router needs from outside this package. main builds
// it once; nothing in here reaches for configuration or opens a connection.
type Deps struct {
	Auth         *AuthHandler
	Applications *ApplicationHandler
	JobPosts     *JobPostHandler

	// Tokens verifies the access token on every protected route, and Users
	// loads the user it names.
	Tokens *auth.TokenManager
	Users  UserGetter

	// DB is pinged by /readyz, for at most PingTimeout.
	DB          Pinger
	PingTimeout time.Duration
}

// NewRouter builds the engine with every middleware and route attached.
//
// Call gin.SetMode before this if the mode is to change: the engine reads it
// while it is being built.
func NewRouter(deps Deps) (*gin.Engine, error) {
	// Checked here, at startup, because a nil Users would otherwise surface as
	// a panic on the first authenticated request.
	if deps.Auth == nil || deps.Applications == nil || deps.JobPosts == nil ||
		deps.Tokens == nil || deps.Users == nil || deps.DB == nil {
		return nil, errors.New("build router: Auth, Applications, JobPosts, Tokens, Users and DB are all required")
	}

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

		// Neither of these is behind RequireAuth. Refresh exists for the
		// moment the access token has expired, and logout must work then too.
		// The refresh token in the body is the credential.
		authRoutes.POST("/refresh", deps.Auth.Refresh)
		authRoutes.POST("/logout", deps.Auth.Logout)
	}

	// Everything registered on this group runs RequireAuth first, so a handler
	// here can rely on CurrentUser. A route limited to some roles adds
	// RequireRole after it, either on a nested group or on the route itself.
	protected := v1.Group("", RequireAuth(deps.Tokens, deps.Users))
	{
		protected.GET("/me", deps.Auth.Me)

		// No RequireRole: every role may call these. What each caller gets is
		// decided per row, by the store's query and auth.CanViewApplication.
		protected.GET("/applications", deps.Applications.List)
		protected.GET("/applications/:id", deps.Applications.Get)

		// Every role may read a post. Whether a draft is visible to the
		// caller is decided by auth.CanViewDraftJobPosts.
		protected.GET("/job-posts/:id", deps.JobPosts.Get)

		// A group with an empty path adds middleware without adding a URL
		// segment. These routes run RequireAuth, then RequireRole, then the
		// handler, so a non-admin is refused before the body is read.
		admin := protected.Group("", RequireRole(domain.RoleAdmin))
		{
			admin.POST("/job-posts", deps.JobPosts.Create)
			admin.PATCH("/job-posts/:id", deps.JobPosts.Update)
		}
	}

	return router, nil
}
