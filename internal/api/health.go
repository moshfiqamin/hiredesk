package api

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// Pinger is the one thing the readiness probe needs from the database.
// *pgxpool.Pool satisfies it.
type Pinger interface {
	Ping(ctx context.Context) error
}

// healthz is liveness: "is this process able to answer HTTP?" A failure means
// restart me. It deliberately does not touch the database — a database outage
// must not cause every API process to be killed and restarted in a loop.
func healthz(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// readyz returns the readiness handler: "should I receive traffic?" A failure
// means remove me from the load balancer but leave me running, so I can rejoin
// once the database returns without ever having been restarted.
func readyz(db Pinger, timeout time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Derived from the request's context, so a client that disconnects
		// cancels the query instead of leaving it running. The timeout caps
		// the wait independently: a database that accepts the connection and
		// then stalls must not stall the health check with it.
		ctx, cancel := context.WithTimeout(c.Request.Context(), timeout)
		defer cancel()

		// Takes a connection from the pool and round-trips to Postgres, so it
		// proves the credentials and the network, not just that a pool object
		// exists.
		if err := db.Ping(ctx); err != nil {
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
	}
}
