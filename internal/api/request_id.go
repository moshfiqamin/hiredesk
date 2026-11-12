package api

import (
	"crypto/rand"

	"github.com/gin-gonic/gin"

	"github.com/moshfiqamin/hiredesk/internal/platform"
)

// RequestIDHeader carries a request's ID, in both directions.
const RequestIDHeader = "X-Request-ID"

// maxRequestIDLength bounds an inbound ID. Long enough for a UUID, a ULID or
// a trace ID with a prefix; short enough that it cannot bloat every log line.
const maxRequestIDLength = 128

// RequestID returns middleware that gives every request an ID: the client's
// own X-Request-ID when it sent a usable one, otherwise a new random one. The
// ID is stored in the request's context, where the logger and the error
// envelope find it, and echoed in the response's X-Request-ID header.
//
// It must be the first middleware, so every later one, and every log line,
// can see the ID.
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader(RequestIDHeader)
		if !validRequestID(id) {
			// 26 characters of base32 from crypto/rand: 130 bits, unguessable
			// and collision-free in practice, and safe in a header or a URL.
			id = rand.Text()
		}

		// The request's context, not gin's key-value store: the context is
		// what handlers pass down to the store, so a log line written three
		// calls deep still carries the ID without any function taking it as
		// a parameter.
		c.Request = c.Request.WithContext(platform.WithRequestID(c.Request.Context(), id))

		// Set before the handler runs, so the header is on every response,
		// including one written by a panic recovery.
		c.Header(RequestIDHeader, id)
	}
}

// validRequestID accepts 1 to 128 characters of letters, digits, '.', '_',
// ':' and '-'. Anything else is replaced rather than trusted: the ID is
// written into every log line, and a client-chosen newline or quote there
// could forge log entries.
func validRequestID(id string) bool {
	if id == "" || len(id) > maxRequestIDLength {
		return false
	}
	for i := 0; i < len(id); i++ {
		// Indexing a string yields bytes; every allowed character is ASCII,
		// so a multi-byte rune fails the check on its first byte.
		b := id[i]
		switch {
		case b >= 'a' && b <= 'z', b >= 'A' && b <= 'Z', b >= '0' && b <= '9':
		case b == '.', b == '_', b == ':', b == '-':
		default:
			return false
		}
	}
	return true
}
