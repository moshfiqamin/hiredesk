package api

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"

	"github.com/gin-gonic/gin"
)

// Recovery returns middleware that turns a panic in any later handler into a
// 500 with the error envelope and the request ID, and logs the panic value
// and stack trace with that same request ID. The process keeps running, and
// so do the other requests it is serving.
//
// It replaces gin.Recovery, which answers with an empty body and writes a
// dump of the request headers to the log.
func Recovery() gin.HandlerFunc {
	return func(c *gin.Context) {
		// A deferred function runs while the goroutine unwinds from a panic,
		// and recover stops the unwinding there. It only works in the same
		// goroutine as the panic, which is why every request needs its own.
		defer func() {
			rec := recover()
			if rec == nil {
				return
			}

			// http.ErrAbortHandler is net/http's way of saying "drop this
			// connection, quietly". Re-panicking hands it back to the server,
			// which does exactly that.
			if err, ok := rec.(error); ok && errors.Is(err, http.ErrAbortHandler) {
				panic(rec)
			}

			// The stack is the only clue to where it happened, and it is
			// logged, never sent. Headers and the body are not logged: they
			// are where credentials travel.
			slog.ErrorContext(c.Request.Context(), "panic recovered",
				slog.String("panic", fmt.Sprint(rec)),
				slog.String("method", c.Request.Method),
				slog.String("path", c.Request.URL.Path),
				slog.String("stack", string(debug.Stack())),
			)

			if c.Writer.Written() {
				// The handler had started its response, so a status and body
				// can no longer be sent. Stop the chain and let the client
				// see a truncated response.
				c.Abort()
				return
			}
			write(c, http.StatusInternalServerError, codeInternal, internalMessage, nil)
		}()

		c.Next()
	}
}
