// Command api is the HireDesk HTTP server entry point.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/moshfiqamin/hiredesk/internal/config"
	"github.com/moshfiqamin/hiredesk/internal/store"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		// Missing configuration is an operator mistake rather than a crash, so
		// print actionable guidance instead of a bare error.
		var missing *config.MissingVarsError
		if errors.As(err, &missing) {
			fmt.Fprintf(os.Stderr, "configuration error: %v\n", err)
			fmt.Fprintln(os.Stderr, "set them and try again, for example:")
			fmt.Fprintln(os.Stderr, `  export DATABASE_URL="postgres://localhost:5432/hiredesk?sslmode=disable"`)
			fmt.Fprintln(os.Stderr, `  export JWT_SECRET="$(openssl rand -hex 32)"`)
			os.Exit(1)
		}
		log.Fatalf("configuration error: %v", err)
	}

	// The real work lives in run so that its deferred calls — closing the pool
	// above all — actually execute. log.Fatalf calls os.Exit, which skips every
	// pending defer, so a defer placed in main would silently never run.
	if err := run(cfg); err != nil {
		log.Fatalf("fatal: %v", err)
	}

	log.Println("shutdown complete")
}

// run starts the server and blocks until a shutdown signal arrives or the
// server fails, returning the first error encountered.
func run(cfg *config.Config) error {
	// Cancelled when the process receives SIGINT (Ctrl-C) or SIGTERM (what
	// `docker stop` and Kubernetes send). This context is the shutdown signal
	// for everything below.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := store.New(ctx, cfg)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	// Waits for in-use connections to be returned, then closes them. Reached
	// on every path out of run, including the error returns below.
	defer pool.Close()

	log.Printf(
		"database pool ready (max=%d min=%d lifetime=%s idle=%s)",
		cfg.DBMaxConns, cfg.DBMinConns, cfg.DBMaxConnLifetime, cfg.DBMaxConnIdleTime,
	)

	// Must happen before gin.Default(), which reads the mode as it installs
	// its middleware.
	if cfg.IsProduction() {
		gin.SetMode(gin.ReleaseMode)
	}

	router := gin.Default()

	// Trust no proxy headers, so c.ClientIP() cannot be spoofed via
	// X-Forwarded-For. Behind a reverse proxy, replace nil with its CIDR.
	if err := router.SetTrustedProxies(nil); err != nil {
		return fmt.Errorf("configure trusted proxies: %w", err)
	}

	registerRoutes(router, cfg, pool)

	srv := &http.Server{
		Addr:    cfg.Addr(),
		Handler: router,

		// Without this, a client that opens a connection and dribbles out
		// header bytes holds it open indefinitely — the Slowloris attack.
		ReadHeaderTimeout: 10 * time.Second,
	}

	// ListenAndServe blocks, so it runs in its own goroutine and reports back
	// over a channel. Buffered with room for one value so the goroutine can
	// exit even if nobody is left reading.
	serverErr := make(chan error, 1)

	go func() {
		log.Printf("HireDesk API listening on %s (env=%s)", cfg.Addr(), cfg.Env)

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
		log.Println("shutdown signal received, draining connections")
	}

	// A fresh context, deliberately NOT derived from ctx: ctx is already
	// cancelled at this point, and a cancelled parent would make Shutdown give
	// up immediately instead of granting the grace period.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	// Stops accepting new connections and waits for in-flight requests to
	// finish, up to the timeout. Returns the context's error if it expires
	// with requests still running.
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("graceful shutdown: %w", err)
	}

	return nil
}

// registerRoutes attaches every HTTP route to router.
//
// Split out of run so the route table is readable in one screen, and so a
// test can build the same router without starting a listener.
func registerRoutes(router *gin.Engine, cfg *config.Config, pool *pgxpool.Pool) {
	// Liveness: "is this process able to answer HTTP?" A failure means restart
	// me. Deliberately does not touch the database — a database outage must
	// not cause every API process to be killed and restarted in a loop.
	router.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// Readiness: "should I receive traffic?" A failure means remove me from
	// the load balancer but leave me running, so I can rejoin once the
	// database returns without ever having been restarted.
	router.GET("/readyz", func(c *gin.Context) {
		// Derived from the request's context, so a client that disconnects
		// cancels the query instead of leaving it running. The timeout caps
		// the wait independently: a database that accepts the connection and
		// then stalls must not stall the health check with it.
		ctx, cancel := context.WithTimeout(c.Request.Context(), cfg.DBPingTimeout)
		defer cancel()

		// Takes a connection from the pool and round-trips to Postgres, so it
		// proves the credentials and the network, not just that a pool object
		// exists.
		if err := pool.Ping(ctx); err != nil {
			// Logged here because the response deliberately does not carry the
			// detail: a health endpoint is usually unauthenticated, and the
			// driver's error can name the host, port and user.
			log.Printf("readiness check failed: %v", err)

			// 503, not 500: the instance is working but its dependency is not,
			// and the distinction is what tells a load balancer to stop
			// sending traffic rather than to restart the process.
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"status":   "degraded",
				"database": "unreachable",
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
}
