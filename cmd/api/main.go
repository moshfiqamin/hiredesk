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

	"github.com/moshfiqamin/hiredesk/internal/api"
	"github.com/moshfiqamin/hiredesk/internal/auth"
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

	// Must happen before the router is built, which reads the mode as it
	// installs its middleware.
	if cfg.IsProduction() {
		gin.SetMode(gin.ReleaseMode)
	}

	// Every dependency is constructed here and handed down, so this is the one
	// place that shows what the server is made of.
	tokens := auth.NewTokenManager(cfg.JWTSecret)

	router, err := api.NewRouter(api.Deps{
		Auth:        api.NewAuthHandler(store.NewUserStore(pool), store.NewTokenStore(pool), tokens),
		Tokens:      tokens,
		DB:          pool,
		PingTimeout: cfg.DBPingTimeout,
	})
	if err != nil {
		return fmt.Errorf("build router: %w", err)
	}

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
