package api

import (
	"log/slog"
	"net/http"
	"slices"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/moshfiqamin/hiredesk/internal/platform"
)

// InFlight tracks the requests currently being served, so a shutdown that
// runs out of time can say which ones it abandoned. Safe for concurrent use.
type InFlight struct {
	mu       sync.Mutex
	requests map[uint64]ActiveRequest

	// next numbers the requests. The request ID cannot be the key: a client
	// chooses it, so two concurrent requests may share one.
	next uint64
}

// ActiveRequest is one request still being served.
type ActiveRequest struct {
	RequestID string
	Method    string
	Path      string
	Started   time.Time
}

// NewInFlight returns an empty tracker.
func NewInFlight() *InFlight {
	return &InFlight{requests: make(map[uint64]ActiveRequest)}
}

// Count returns how many requests are being served.
func (f *InFlight) Count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.requests)
}

// Snapshot returns the requests being served, oldest first.
func (f *InFlight) Snapshot() []ActiveRequest {
	f.mu.Lock()
	out := make([]ActiveRequest, 0, len(f.requests))
	for _, r := range f.requests {
		out = append(out, r)
	}
	f.mu.Unlock()

	slices.SortFunc(out, func(a, b ActiveRequest) int { return a.Started.Compare(b.Started) })
	return out
}

// add records r and returns the key that removes it.
func (f *InFlight) add(r ActiveRequest) uint64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.next++
	f.requests[f.next] = r
	return f.next
}

func (f *InFlight) remove(key uint64) {
	f.mu.Lock()
	delete(f.requests, key)
	f.mu.Unlock()
}

// AccessLog returns middleware that writes one log line per request, after it
// completes: method, path, route, status, duration, response size and client
// IP, plus the request ID and the user ID, which the logger takes from the
// request's context. It also registers each request with inFlight while it
// runs.
//
// It must come after RequestID, and before Recovery, so that a panic is
// logged with the 500 that Recovery turned it into.
//
// What it never logs: headers, the query string, or any part of a body.
// Those are where tokens and passwords travel.
func AccessLog(inFlight *InFlight) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path

		key := inFlight.add(ActiveRequest{
			RequestID: platform.RequestID(c.Request.Context()),
			Method:    c.Request.Method,
			Path:      path,
			Started:   start,
		})
		// Deferred, so a request is removed even if a panic escapes past
		// Recovery.
		defer inFlight.remove(key)

		c.Next()

		status := c.Writer.Status()
		level := slog.LevelInfo
		if status >= http.StatusInternalServerError {
			level = slog.LevelError
		}

		// c.Request, read again after c.Next: RequireAuth replaced it with a
		// copy whose context carries the user ID, and the logger adds it.
		slog.LogAttrs(c.Request.Context(), level, "request",
			slog.String("method", c.Request.Method),
			slog.String("path", path),
			// The pattern, such as /v1/applications/:id, groups requests
			// for one endpoint whatever the ID; empty for an unknown route.
			slog.String("route", c.FullPath()),
			slog.Int("status", status),
			slog.Float64("duration_ms", float64(time.Since(start).Microseconds())/1000),
			slog.Int("bytes", c.Writer.Size()),
			slog.String("client_ip", c.ClientIP()),
		)
	}
}
