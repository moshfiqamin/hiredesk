// Command api is the HireDesk HTTP server entry point.
package main

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"

	"github.com/moshfiqamin/hiredesk/internal/config"
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

	// Must happen before gin.Default(), which reads the mode as it installs
	// its middleware.
	if cfg.IsProduction() {
		gin.SetMode(gin.ReleaseMode)
	}

	router := gin.Default()

	// Trust no proxy headers, so c.ClientIP() cannot be spoofed via
	// X-Forwarded-For. Behind a reverse proxy, replace nil with its CIDR.
	if err := router.SetTrustedProxies(nil); err != nil {
		log.Fatalf("failed to configure trusted proxies: %v", err)
	}

	// Liveness: "is this process able to answer HTTP?" A failure means restart
	// me. Deliberately does not touch the database — a database outage must not
	// cause every API process to be killed.
	router.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// Readiness: "should I receive traffic?" A failure means remove me from the
	// load balancer but leave me running. Gains a database ping in module 1;
	// the response shape is already final.
	router.GET("/readyz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	log.Printf("HireDesk API listening on %s (env=%s)", cfg.Addr(), cfg.Env)

	if err := router.Run(cfg.Addr()); err != nil {
		log.Fatalf("server stopped: %v", err)
	}
}
