package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"

	"github.com/moshfiqamin/hiredesk/internal/domain"
)

// Machine-readable codes for requests refused before any rule is checked.
const (
	codeMalformedRequest = "malformed_request"
	codeInvalidID        = "invalid_id"
)

// uuidLength is the length of a UUID in its canonical text form:
// 32 hexadecimal digits and 4 hyphens.
const uuidLength = 36

// validate is shared by every handler. A Validate caches what it learns about
// each struct type by reflection, so one long-lived instance is both safe for
// concurrent use and much cheaper than one per request.
var validate = newValidator()

func newValidator() *validator.Validate {
	v := validator.New()

	// By default a failure is reported under the Go field name ("FullName").
	// This makes the validator read the json tag instead, so the client sees
	// the name it actually sent ("full_name").
	v.RegisterTagNameFunc(func(field reflect.StructField) string {
		// A json tag is "name,option,option"; only the name is wanted.
		name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		if name == "-" {
			return ""
		}
		return name
	})

	return v
}

// decodeJSON reads the request body into dst. On failure it writes a 400, or
// a 413 for a body over maxBodyBytes, and returns false; the handler should
// then return without writing anything else.
func decodeJSON(c *gin.Context, dst any) bool {
	// BodyLimit has already wrapped the body in production. Wrapping it again
	// keeps a handler safe when it is mounted without that middleware, as in
	// tests; nested limits cost nothing.
	body := http.MaxBytesReader(c.Writer, c.Request.Body, maxBodyBytes)

	if err := json.NewDecoder(body).Decode(dst); err != nil {
		// The read that crossed the limit fails with this type, wrapped by
		// the decoder. errors.As finds it through the wrapping.
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeTooLarge(c, tooLarge.Limit)
			return false
		}

		// 400, not 422: the body could not be read at all, so there are no
		// fields to report on. The decoder's message is not passed on; it
		// quotes fragments of the input and names Go types.
		write(c, http.StatusBadRequest, codeMalformedRequest, "The request body must be a valid JSON object.", nil)
		return false
	}
	return true
}

// pathUUID returns the path parameter called name if it is a UUID. Otherwise
// it writes a 400 and returns false, and the handler should return without
// writing anything else.
func pathUUID(c *gin.Context, name string) (string, bool) {
	id := c.Param(name)
	if !isUUID(id) {
		// 400, not 404: the request itself is wrong, and will be wrong however
		// many times it is retried. The value is not echoed back.
		write(c, http.StatusBadRequest, codeInvalidID, "The ID in the URL must be a UUID.", nil)
		return "", false
	}
	return id, true
}

// isUUID reports whether s is a UUID in the canonical 8-4-4-4-12 form, such
// as "1b9d6bcd-bbfd-4b2d-9b5d-ab8dfbbd4bed", in either case.
//
// It checks the shape only. PostgreSQL's uuid type accepts any 128 bits, so
// requiring a particular version here would reject IDs the database can hold.
func isUUID(s string) bool {
	if len(s) != uuidLength {
		return false
	}

	for i := 0; i < len(s); i++ {
		ch := s[i]

		switch i {
		case 8, 13, 18, 23:
			if ch != '-' {
				return false
			}
		default:
			isHex := (ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f') || (ch >= 'A' && ch <= 'F')
			if !isHex {
				return false
			}
		}
	}
	return true
}

// mergeValidation adds the field problems in err, if it is a
// *domain.ValidationError, to invalid. It does nothing when err is nil, so a
// handler can collect problems found during parsing and the domain's in one
// set, and report all of them in one response.
func mergeValidation(invalid *domain.ValidationError, err error) {
	var found *domain.ValidationError
	if errors.As(err, &found) {
		for field, problem := range found.Fields {
			invalid.Add(field, problem)
		}
	}
}

// validateStruct checks v against its validate tags and returns a
// *domain.ValidationError naming every field that failed, or nil.
func validateStruct(v any) error {
	err := validate.Struct(v)
	if err == nil {
		return nil
	}

	// The only other error Struct returns is for being handed something that
	// is not a struct, which is a bug in the caller, not bad input.
	var failures validator.ValidationErrors
	if !errors.As(err, &failures) {
		return fmt.Errorf("validate %T: %w", v, err)
	}

	var invalid domain.ValidationError
	for _, failure := range failures {
		invalid.Add(failure.Field(), problemFor(failure))
	}
	return invalid.Err()
}

// problemFor words one failed rule for the client. failure.Tag() is the rule
// that failed ("min") and failure.Param() is its argument ("8").
func problemFor(failure validator.FieldError) string {
	switch failure.Tag() {
	case "required":
		return "is required"
	case "email":
		return "must be a valid email address"
	case "min":
		return "must be at least " + failure.Param() + " characters"
	case "max":
		return "must be at most " + failure.Param() + " characters"
	default:
		// A rule added to a struct tag without a case here still produces a
		// usable answer rather than an empty one.
		return "is invalid"
	}
}
