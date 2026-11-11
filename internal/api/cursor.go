package api

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/moshfiqamin/hiredesk/internal/domain"
)

// cursorVersion is written into every cursor. A future change to the
// listing's sort key changes what a cursor must hold; bumping this makes old
// cursors fail cleanly as invalid instead of being misread.
const cursorVersion = 1

// maxCursorLength bounds what is decoded. A real cursor is under 100
// characters; anything far longer is not one, and is refused before any work.
const maxCursorLength = 256

// errInvalidCursor is returned by decodeCursor for anything it did not issue.
var errInvalidCursor = errors.New("invalid cursor")

// cursorPayload is what a cursor holds before encoding. The short keys keep
// the URL short; nobody but this file reads them.
type cursorPayload struct {
	V  int    `json:"v"`
	T  string `json:"t"`
	ID string `json:"id"`
}

// encodeCursor turns a listing position into an opaque string for a URL.
//
// Opaque on purpose: clients must pass it back unchanged and never build one,
// so the sort key behind it can change without breaking them. It is not
// signed, because there is nothing to protect: a forged cursor can only start
// the public listing at some other point in public data.
func encodeCursor(p domain.ListingPosition) string {
	payload := cursorPayload{
		V: cursorVersion,

		// RFC 3339 with nanoseconds, in UTC. PostgreSQL stores microseconds;
		// writing anything coarser, such as whole seconds or milliseconds,
		// would put the cursor before rows created in the same second or
		// millisecond, and they would be returned twice.
		T:  p.CreatedAt.UTC().Format(time.RFC3339Nano),
		ID: p.ID,
	}

	// Marshalling a struct of two strings and an int cannot fail.
	raw, _ := json.Marshal(payload) //nolint:errcheck // See above.

	// URL-safe base64 without padding: no character in it needs escaping in
	// a query string.
	return base64.RawURLEncoding.EncodeToString(raw)
}

// decodeCursor reverses encodeCursor. It returns errInvalidCursor for any
// string encodeCursor could not have produced.
func decodeCursor(s string) (domain.ListingPosition, error) {
	if len(s) > maxCursorLength {
		return domain.ListingPosition{}, errInvalidCursor
	}

	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return domain.ListingPosition{}, errInvalidCursor
	}

	// DisallowUnknownFields, so a payload with extra keys is refused rather
	// than half-read.
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()

	var payload cursorPayload
	if err = dec.Decode(&payload); err != nil || payload.V != cursorVersion {
		return domain.ListingPosition{}, errInvalidCursor
	}

	createdAt, err := time.Parse(time.RFC3339Nano, payload.T)
	if err != nil || !isUUID(payload.ID) {
		return domain.ListingPosition{}, errInvalidCursor
	}

	return domain.ListingPosition{CreatedAt: createdAt, ID: strings.ToLower(payload.ID)}, nil
}
