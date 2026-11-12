// Command api is the HireDesk HTTP server entry point.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/moshfiqamin/hiredesk/internal/api"
	"github.com/moshfiqamin/hiredesk/internal/auth"
	"github.com/moshfiqamin/hiredesk/internal/config"
	"github.com/moshfiqamin/hiredesk/internal/platform"
	"github.com/moshfiqamin/hiredesk/internal/store"
)

// The /v1/auth/* allowance: 10 requests per IP per minute, with at most
// 10,000 client buckets held at once (about 1 MB).
const (
	authRequestsPerWindow = 10
	authWindow            = time.Minute
	authMaxClients        = 10_000
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		// The logger is configured from cfg, so it does not exist yet; a
		// configuration error goes to stderr as plain text.
		fmt.Fprintf(os.Stderr, "configuration error: %v\n", err)

		var missing *config.MissingVarsError
		if errors.As(err, &missing) {
			fmt.Fprintln(os.Stderr, "set them and try again, for example:")
			fmt.Fprintln(os.Stderr, `  export DATABASE_URL="postgres://localhost:5432/hiredesk?sslmode=disable"`)
			fmt.Fprintln(os.Stderr, `  export JWT_SECRET="$(openssl rand -hex 32)"`)
		}
		os.Exit(1)
	}

	// Installed as the default, so every package logs through slog.InfoContext
	// and friends without a logger being passed to it. Also routes the
	// standard library's log package here, for anything still using it.
	slog.SetDefault(platform.NewLogger(os.Stdout, cfg.LogLevel, cfg.LogFormat))

	// The work lives in run so that its deferred calls, closing the pool above
	// all, have finished before the exit code is chosen. os.Exit skips every
	// pending defer, so it must only be called here, after run has returned.
	code := 0
	if err := run(cfg); err != nil {
		slog.Error("server stopped with an error", slog.String("error", err.Error()))
		code = 1
	} else {
		slog.Info("shutdown complete")
	}

	// Nothing to flush: slog's handlers write each record to os.Stdout in a
	// single unbuffered Write, so every line above has already reached the
	// file descriptor. Were the handler ever made buffered or asynchronous,
	// its flush would go here, before the exit.
	os.Exit(code)
}

// run starts the server and blocks until a shutdown signal arrives or the
// server fails. It returns nil only for a clean, complete shutdown.
func run(cfg *config.Config) error {
	// Cancelled when the process receives SIGINT (Ctrl-C) or SIGTERM (what
	// docker stop, Kubernetes and Fly.io send). This context is the shutdown
	// signal for everything below.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	slog.Info("starting", slog.Any("config", cfg))

	pool, err := store.New(ctx, cfg)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	// Deferred, so it runs on every path out of run; and run returns only
	// after the server has stopped, so the pool always outlives the requests
	// that use it.
	defer func() {
		pool.Close()
		slog.Info("database pool closed")
	}()

	slog.Info("database pool ready",
		slog.Int("max_conns", int(cfg.DBMaxConns)),
		slog.Int("min_conns", int(cfg.DBMinConns)),
	)

	// Must happen before the router is built, which reads the mode as it
	// installs its middleware.
	if cfg.IsProduction() {
		gin.SetMode(gin.ReleaseMode)
	}

	// Gin's debug mode prints every route and some warnings as bare text on
	// stdout, in between the structured lines. These send both through slog
	// at debug level instead, so LOG_LEVEL=debug shows them and every line
	// stays parseable.
	gin.DebugPrintRouteFunc = func(method, path, handler string, handlers int) {
		slog.Debug("route registered", slog.String("method", method), slog.String("path", path), slog.Int("handlers", handlers))
	}
	gin.DebugPrintFunc = func(format string, values ...any) {
		slog.Debug("gin", slog.String("detail", strings.TrimSpace(fmt.Sprintf(format, values...))))
	}

	// Every dependency is constructed here and handed down, so this is the one
	// place that shows what the server is made of.
	tokens := auth.NewTokenManager(cfg.JWTSecret)
	users := store.NewUserStore(pool)
	jobPosts := store.NewJobPostStore(pool)
	// Shared by two handlers: the review routes load the application through
	// it to check who may review it.
	applications := store.NewApplicationStore(pool)
	inFlight := api.NewInFlight()

	router, err := api.NewRouter(api.Deps{
		Auth:           api.NewAuthHandler(users, store.NewTokenStore(pool), tokens),
		Applications:   api.NewApplicationHandler(applications),
		JobPosts:       api.NewJobPostHandler(jobPosts),
		Questions:      api.NewQuestionHandler(store.NewQuestionStore(pool)),
		Listings:       api.NewJobListingHandler(jobPosts),
		Rubrics:        api.NewRubricHandler(store.NewRubricStore(pool)),
		Reviews:        api.NewReviewHandler(applications, store.NewReviewStore(pool)),
		Tokens:         tokens,
		Users:          users,
		DB:             pool,
		PingTimeout:    cfg.DBPingTimeout,
		AuthLimiter:    api.NewRateLimiter(authRequestsPerWindow, authWindow, authMaxClients),
		InFlight:       inFlight,
		AllowedOrigins: cfg.AllowedOrigins,
	})
	if err != nil {
		return fmt.Errorf("build router: %w", err)
	}

	srv := &http.Server{
		Addr:    cfg.Addr(),
		Handler: router,

		// Without this, a client that opens a connection and dribbles out
		// header bytes holds it open indefinitely: the Slowloris attack.
		ReadHeaderTimeout: 10 * time.Second,

		// net/http's own complaints, such as TLS handshake errors, go to the
		// structured log rather than to stderr as bare text.
		ErrorLog: slog.NewLogLogger(slog.Default().Handler(), slog.LevelWarn),
	}

	// ListenAndServe blocks, so it runs in its own goroutine and reports back
	// over a channel. Buffered with room for one value so the goroutine can
	// exit even if nobody is left reading.
	serverErr := make(chan error, 1)

	go func() {
		slog.Info("listening", slog.String("addr", cfg.Addr()), slog.String("env", cfg.Env))

		// Returns ErrServerClosed on a deliberate Shutdown, which is a normal
		// outcome rather than a failure.
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	select {
	case err := <-serverErr:
		// Failed to bind, or died while serving. The deferred pool.Close still
		// runs on the way out.
		return fmt.Errorf("server: %w", err)

	case <-ctx.Done():
	}

	// Restores the default signal behaviour, so a second Ctrl-C or SIGTERM
	// during the drain kills the process at once instead of being swallowed.
	stop()

	slog.Info("shutdown signal received, draining",
		slog.Duration("timeout", cfg.ShutdownTimeout),
		slog.Int("in_flight", inFlight.Count()),
	)

	// A fresh context, deliberately NOT derived from ctx: ctx is already
	// cancelled at this point, and a cancelled parent would make Shutdown give
	// up immediately instead of granting the grace period.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	// Closes the listener, so new connections are refused, closes idle
	// keep-alive connections, then waits for every active request to finish,
	// up to the timeout.
	if err := srv.Shutdown(shutdownCtx); err != nil {
		// Out of time. Say exactly what is being abandoned, for whoever reads
		// the logs after the deploy.
		for _, r := range inFlight.Snapshot() {
			slog.Warn("request still running at shutdown deadline",
				slog.String("request_id", r.RequestID),
				slog.String("method", r.Method),
				slog.String("path", r.Path),
				slog.Duration("running_for", time.Since(r.Started).Round(time.Millisecond)),
			)
		}

		// Close drops the remaining connections at once. Their handlers see
		// their request context cancelled, which aborts any query they are
		// waiting on, and so returns their connections to the pool before the
		// deferred pool.Close waits for them.
		if closeErr := srv.Close(); closeErr != nil {
			slog.Warn("forced close failed", slog.String("error", closeErr.Error()))
		}
		return fmt.Errorf("graceful shutdown timed out after %s: %w", cfg.ShutdownTimeout, err)
	}

	slog.Info("http server drained; every in-flight request finished")
	return nil
}
