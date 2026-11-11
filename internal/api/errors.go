// Package api is the HTTP layer: it turns requests into calls on the domain
// and turns the results, errors included, back into responses.
package api

import (
	"errors"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/moshfiqamin/hiredesk/internal/domain"
)

// Machine-readable codes for errors that do not come from the domain.
const (
	codeInternal         = "internal_error"
	codeRouteNotFound    = "route_not_found"
	codeMethodNotAllowed = "method_not_allowed"
)

// internalMessage is all a client is ever told about an error this package
// does not recognise.
const internalMessage = "Something went wrong on our side. Please try again."

// errorBody is the one shape every error response has:
//
//	{"error":{"code":"...","message":"...","details":{...}}}
type errorBody struct {
	Error errorDetail `json:"error"`
}

type errorDetail struct {
	// Code is stable and meant for programs: clients branch on it.
	Code string `json:"code"`

	// Message is meant for people and may be reworded at any time.
	Message string `json:"message"`

	// Details says more about the problem: for a validation failure, each
	// field's problem; for a refused status change, the statuses that are
	// allowed. No omitempty, and never nil, so it is always a JSON object and
	// a client can read it without first checking that it exists.
	//
	// any, not string, so a value can be a list.
	Details map[string]any `json:"details"`
}

// detailedError is an error that carries details written to be shown to a
// client. *domain.ValidationError, *domain.TransitionError and
// *domain.NotReadyError implement it.
type detailedError interface {
	error
	Details() map[string]any
}

// mapping is one row of the table that translates a domain error to HTTP.
type mapping struct {
	target  error
	status  int
	code    string
	message string
}

// mappings is checked in order and the first match wins. A slice rather than
// a map keyed by error: a wrapped error is a different value from its
// sentinel, so a map lookup would miss it, and each row needs errors.Is.
var mappings = []mapping{
	{domain.ErrValidation, http.StatusUnprocessableEntity, "validation_failed", "One or more fields are invalid."},
	{domain.ErrInvalidCredentials, http.StatusUnauthorized, "invalid_credentials", "Email or password is incorrect."},
	{domain.ErrTokenExpired, http.StatusUnauthorized, "token_expired", "The access token has expired. Use your refresh token to get a new one."},
	{domain.ErrUnauthenticated, http.StatusUnauthorized, "unauthenticated", "Authentication is required."},
	{domain.ErrForbidden, http.StatusForbidden, "forbidden", "You do not have permission to do that."},
	{domain.ErrNotFound, http.StatusNotFound, "not_found", "The requested resource was not found."},
	{domain.ErrDuplicate, http.StatusConflict, "duplicate", "A resource with these details already exists."},
	{domain.ErrInvalidTransition, http.StatusConflict, "invalid_transition", "The resource cannot move to that status from its current one."},
	{domain.ErrNotReady, http.StatusConflict, "not_ready", "The resource is missing something it needs before it can move to that status."},
	{domain.ErrConflict, http.StatusConflict, "conflict", "The request conflicts with the current state of the resource."},
}

// lookup returns the mapping for err, and false if err is not a domain error.
func lookup(err error) (mapping, bool) {
	for _, m := range mappings {
		if errors.Is(err, m.target) {
			return m, true
		}
	}
	return mapping{}, false
}

// Respond writes err to the client as an error envelope and stops the handler
// chain. Handlers call it instead of c.JSON for every failure:
//
//	user, err := svc.Register(ctx, input)
//	if err != nil {
//		api.Respond(c, err)
//		return
//	}
//
// A domain error becomes its mapped status, code and message. Anything else is
// logged in full and reported to the client as a 500 with a fixed message.
func Respond(c *gin.Context, err error) {
	m, known := lookup(err)
	if !known {
		// The only place the real cause is recorded. The path is quoted so a
		// crafted URL cannot forge extra log lines; the query string is left
		// out because it can carry tokens.
		log.Printf("internal error: %s %q: %v", c.Request.Method, c.Request.URL.Path, err)

		write(c, http.StatusInternalServerError, codeInternal, internalMessage, nil)
		return
	}

	// err.Error() is never sent, even for a known error: whatever wrapped the
	// sentinel may have added a query, an ID or a driver message. The client
	// gets the table's fixed text, plus details that were written to be
	// shown.
	var details map[string]any

	// errors.As accepts a pointer to an interface type: it finds the first
	// error in the chain that has a Details method, whatever its type.
	var detailed detailedError
	if errors.As(err, &detailed) {
		details = detailed.Details()
	}

	write(c, m.status, m.code, m.message, details)
}

// NoRoute answers a request for a path no route matches. Without it Gin
// replies with the plain text "404 page not found".
func NoRoute(c *gin.Context) {
	write(c, http.StatusNotFound, codeRouteNotFound, "No such endpoint.", nil)
}

// NoMethod answers a request whose path exists under a different HTTP method.
// It is only reached when the engine has HandleMethodNotAllowed set.
func NoMethod(c *gin.Context) {
	write(c, http.StatusMethodNotAllowed, codeMethodNotAllowed, "That method is not allowed on this endpoint.", nil)
}

// Recovery returns middleware that turns a panic in a handler into the same
// 500 envelope Respond produces, in place of gin.Recovery's empty body.
func Recovery() gin.HandlerFunc {
	// CustomRecovery has already logged the panic value and stack trace by the
	// time it calls this function.
	return gin.CustomRecovery(func(c *gin.Context, _ any) {
		write(c, http.StatusInternalServerError, codeInternal, internalMessage, nil)
	})
}

// write sends the envelope and aborts, so no later handler or middleware in
// the chain can add a second body.
func write(c *gin.Context, status int, code, message string, details map[string]any) {
	// A nil map encodes as JSON null, which would be a second shape.
	if details == nil {
		details = map[string]any{}
	}

	c.AbortWithStatusJSON(status, errorBody{
		Error: errorDetail{Code: code, Message: message, Details: details},
	})
}
