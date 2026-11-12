package platform

import "context"

// ctxKey is the type of every key this package stores in a context. Being
// unexported, no other package can construct one, so no other package can
// read or overwrite these values by accident.
type ctxKey int

const (
	requestIDKey ctxKey = iota
	userIDKey
)

// WithRequestID returns a copy of ctx that carries id as the request ID.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey, id)
}

// RequestID returns the request ID stored in ctx, or "" if there is none.
func RequestID(ctx context.Context) string {
	// The two-value assertion: an absent key gives nil, which is not a
	// string, and reports ok=false instead of panicking.
	id, ok := ctx.Value(requestIDKey).(string)
	if !ok {
		return ""
	}
	return id
}

// WithUserID returns a copy of ctx that carries id as the authenticated
// user's ID.
func WithUserID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, userIDKey, id)
}

// UserID returns the authenticated user's ID stored in ctx, or "" if the
// request is anonymous.
func UserID(ctx context.Context) string {
	// The two-value assertion: an absent key gives nil, which is not a
	// string, and reports ok=false instead of panicking.
	id, ok := ctx.Value(userIDKey).(string)
	if !ok {
		return ""
	}
	return id
}
