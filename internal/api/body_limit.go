package api

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

const codePayloadTooLarge = "payload_too_large"

// maxBodyBytes caps a request body at 1 MiB.
//
// The largest body this API accepts is an application: a handful of answers,
// each of which a form may require to be up to 10,000 characters long. Even
// at four bytes per character that is 40 KB an answer, so 1 MiB leaves room
// for a long form while keeping the cost of a hostile body small: one request
// can make the server read at most 1 MiB, never gigabytes.
const maxBodyBytes = 1 << 20

// BodyLimit returns middleware that refuses any request body larger than
// limit bytes with 413 and the error envelope.
//
// A body that declares its size in Content-Length is refused before a byte of
// it is read. One that does not, such as a chunked upload, is wrapped in
// http.MaxBytesReader, which fails the read that crosses the limit; the
// handler's decoder then reports that as a 413 too (see decodeJSON).
func BodyLimit(limit int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.ContentLength > limit {
			writeTooLarge(c, limit)
			return
		}

		// MaxBytesReader also tells the server to close the connection once
		// the limit is hit, rather than reading and discarding the rest.
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit)
	}
}

// writeTooLarge answers 413, stating the limit so a client knows what to aim
// for.
func writeTooLarge(c *gin.Context, limit int64) {
	write(c, http.StatusRequestEntityTooLarge, codePayloadTooLarge,
		"The request body must be at most "+strconv.FormatInt(limit, 10)+" bytes.",
		map[string]any{"limit_bytes": limit})
}
