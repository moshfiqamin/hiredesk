package api

import (
	"math"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"
)

const codeRateLimited = "rate_limited"

// RateLimiter limits how often each client IP may call the routes it guards,
// with one token bucket per IP. Safe for concurrent use.
//
// Its memory is bounded twice over. Buckets idle for a full window are swept
// out, which forgets nothing: an idle bucket has refilled, and is identical
// to the fresh one a later request would get. And the number of buckets is
// capped, for the case where more distinct addresses arrive within one window
// than the cap allows.
type RateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket

	limit rate.Limit // tokens added per second
	burst int        // bucket size: requests allowed back to back
	idle  time.Duration
	max   int

	lastSweep time.Time

	// now is the clock, replaceable in tests so a window can pass instantly.
	now func() time.Time
}

type bucket struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// NewRateLimiter allows each IP requests per window: a burst of up to that
// many at once, then one more every window/requests as the bucket refills.
// It keeps at most maxClients buckets.
func NewRateLimiter(requests int, window time.Duration, maxClients int) *RateLimiter {
	return &RateLimiter{
		buckets: make(map[string]*bucket),
		// rate.Every turns "one per interval" into a per-second rate: for 10
		// a minute, one token every six seconds.
		limit: rate.Every(window / time.Duration(requests)),
		burst: requests,
		idle:  window,
		max:   maxClients,
		now:   time.Now,
	}
}

// allow takes one token from key's bucket. When there is none, it reports
// how long until there will be.
func (l *RateLimiter) allow(key string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()

	// Sweeping costs a pass over the map, so it runs at most once per idle
	// window rather than on every request. Lazily, inside the request that
	// notices it is due, so there is no background goroutine to start and
	// stop.
	if now.Sub(l.lastSweep) >= l.idle {
		l.sweep(now)
	}

	b, ok := l.buckets[key]
	if !ok {
		if len(l.buckets) >= l.max {
			// Full even after sweeping, so more distinct addresses than the
			// cap are active within one window. Sweep first, in case the
			// last sweep is nearly a window old.
			l.sweep(now)
		}
		if len(l.buckets) >= l.max {
			// Still full: evict one bucket. Go randomises map iteration, so
			// this is a random choice in O(1). Refusing the new address
			// instead would hand anyone with enough addresses a way to lock
			// every other user out of signing in.
			for victim := range l.buckets {
				delete(l.buckets, victim)
				break
			}
		}
		b = &bucket{limiter: rate.NewLimiter(l.limit, l.burst)}
		l.buckets[key] = b
	}
	b.lastSeen = now

	// A reservation, unlike Allow, says how long until the token would be
	// available, which is what Retry-After needs. Cancelled at once when the
	// request is refused, so a refusal does not also consume a future token.
	r := b.limiter.ReserveN(now, 1)
	if delay := r.DelayFrom(now); delay > 0 {
		r.CancelAt(now)
		return false, delay
	}
	return true, 0
}

// sweep removes every bucket unused for a full idle window.
func (l *RateLimiter) sweep(now time.Time) {
	for key, b := range l.buckets {
		// Deleting from a map while ranging over it is allowed in Go; the
		// deleted entry is simply not visited again.
		if now.Sub(b.lastSeen) >= l.idle {
			delete(l.buckets, key)
		}
	}
	l.lastSweep = now
}

// size returns the number of buckets held.
func (l *RateLimiter) size() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.buckets)
}

// Middleware returns middleware that answers 429 with a Retry-After header
// and the error envelope once a client IP has used up its allowance.
func (l *RateLimiter) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		ok, wait := l.allow(clientKey(c.ClientIP()))
		if ok {
			return
		}

		// Retry-After is whole seconds, rounded up: rounding down would tell
		// the client to come back a moment too early, and be refused again.
		seconds := int(math.Ceil(wait.Seconds()))
		c.Header("Retry-After", strconv.Itoa(seconds))
		write(c, http.StatusTooManyRequests, codeRateLimited,
			"Too many attempts. Wait before trying again.",
			map[string]any{"retry_after_seconds": seconds})
	}
}

// clientKey groups an IPv6 address by its /64 network. One IPv6 subscriber is
// routinely given a whole /64, 2^64 addresses, so limiting single addresses
// would let one client rotate through endless fresh buckets. An IPv4 address
// is used as it is.
func clientKey(ip string) string {
	parsed := net.ParseIP(ip)
	if parsed == nil || parsed.To4() != nil {
		return ip
	}
	return parsed.Mask(net.CIDRMask(64, 128)).String() + "/64"
}
