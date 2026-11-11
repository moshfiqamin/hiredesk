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
	Questions    *QuestionHandler
	Listings     *JobListingHandler
	Rubrics      *RubricHandler
	Reviews      *ReviewHandler

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
	if deps.Auth == nil || deps.Applications == nil || deps.JobPosts == nil || deps.Questions == nil ||
		deps.Listings == nil || deps.Rubrics == nil || deps.Reviews == nil || deps.Tokens == nil || deps.Users == nil || deps.DB == nil {
		return nil, errors.New("build router: Auth, Applications, JobPosts, Questions, Listings, Rubrics, Reviews, Tokens, Users and DB are all required")
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

	// The one public read: open job posts, for anyone, signed in or not. It is
	// on v1 itself, not the protected group, so no Authorization header is
	// read at all. A visitor with an expired or forged token gets the same
	// page as one with none.
	v1.GET("/job-posts", deps.Listings.List)

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

		// Admins and the interviewers assigned to the application. Not
		// RequireRole: an interviewer's access depends on the row, so the
		// handler decides, with auth.CanViewScore.
		protected.GET("/applications/:id/score", deps.Applications.Score)

		// The timeline: admins and the assigned interviewers, decided per row
		// by auth.CanViewTimeline, as the score is.
		protected.GET("/applications/:id/events", deps.Applications.Timeline)

		// Reviews are filed by interviewers only. RequireRole refuses everyone
		// else before the body is read; the handler then requires an
		// assignment, which depends on the row.
		reviewer := protected.Group("", RequireRole(domain.RoleInterviewer))
		{
			reviewer.POST("/applications/:id/reviews", deps.Reviews.CreateReview)
			reviewer.PATCH("/applications/:id/reviews/mine", deps.Reviews.UpdateMyReview)
		}

		// Every role may read a post. Whether a draft is visible to the
		// caller is decided by auth.CanViewDraftJobPosts.
		protected.GET("/job-posts/:id", deps.JobPosts.Get)

		// Only a candidate applies. An admin or interviewer is refused before
		// the body is read.
		protected.POST("/job-posts/:id/applications", RequireRole(domain.RoleCandidate), deps.Applications.Submit)

		// A group with an empty path adds middleware without adding a URL
		// segment. These routes run RequireAuth, then RequireRole, then the
		// handler, so a non-admin is refused before the body is read.
		admin := protected.Group("", RequireRole(domain.RoleAdmin))
		{
			admin.POST("/job-posts", deps.JobPosts.Create)
			admin.PATCH("/job-posts/:id", deps.JobPosts.Update)

			// One route per transition rather than a writable status field,
			// so each move has its own URL, its own rules and its own entry
			// in the access log. The lifecycle itself is in domain.
			admin.POST("/job-posts/:id/open", deps.JobPosts.Open)
			admin.POST("/job-posts/:id/close", deps.JobPosts.Close)

			// The application pipeline. A single status route rather than one
			// per transition, as job posts have: five targets reachable from
			// four statuses would be a dozen routes saying the same thing.
			// The lifecycle map in domain decides what is legal.
			admin.PATCH("/applications/:id/status", deps.Applications.UpdateStatus)

			// Interviewer assignment. POST /assign is idempotent despite the
			// method: a repeat answers 200 and changes nothing. DELETE names
			// the interviewer in the path, as a DELETE has no body.
			admin.POST("/applications/:id/assign", deps.Reviews.Assign)
			admin.DELETE("/applications/:id/assign/:interviewerID", deps.Reviews.Unassign)
			admin.GET("/applications/:id/assignments", deps.Reviews.Assignments)

			// Rescoring against the current rubric: one application, or every
			// application to a post. POST, not PUT: each call appends an event,
			// so repeating it is not a no-op on the timeline.
			admin.POST("/applications/:id/rescore", deps.Applications.Rescore)
			admin.POST("/job-posts/:id/rescore-all", deps.Applications.RescoreAll)

			// Every applicant to a post, best score first.
			admin.GET("/job-posts/:id/ranking", deps.Applications.Ranking)

			// Full-text search over the answers to one post. GET with q in the
			// query string, so a search is bookmarkable and safe to retry.
			// Gin keeps a route tree per method, so this sits beside POST
			// /job-posts/:id/applications without conflict.
			admin.GET("/job-posts/:id/applications/search", deps.Applications.Search)

			// The application form. Everyone else sees it inside GET
			// /v1/job-posts/:id; these routes are for building it, and every
			// write is refused once the post leaves draft.
			admin.GET("/job-posts/:id/questions", deps.Questions.List)
			admin.POST("/job-posts/:id/questions", deps.Questions.Create)
			admin.PUT("/job-posts/:id/questions/order", deps.Questions.Reorder)
			admin.PATCH("/job-posts/:id/questions/:questionID", deps.Questions.Update)
			admin.DELETE("/job-posts/:id/questions/:questionID", deps.Questions.Delete)

			// The scoring rubric. PUT, not PATCH: the body is the whole rubric,
			// which becomes the next version. Editable in any status; scores
			// already stored keep the version that produced them.
			admin.GET("/job-posts/:id/rubric", deps.Rubrics.Get)
			admin.PUT("/job-posts/:id/rubric", deps.Rubrics.Replace)
		}
	}

	return router, nil
}
