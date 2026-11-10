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

const codeMalformedRequest = "malformed_request"

// maxBodyBytes caps a request body. Every body this API accepts is a small
// JSON object; without a cap a client could send gigabytes and have the
// server read all of it.
const maxBodyBytes = 1 << 20

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

// decodeJSON reads the request body into dst. On failure it writes a 400 and
// returns false, and the handler should return without writing anything else.
func decodeJSON(c *gin.Context, dst any) bool {
	body := http.MaxBytesReader(c.Writer, c.Request.Body, maxBodyBytes)

	if err := json.NewDecoder(body).Decode(dst); err != nil {
		// 400, not 422: the body could not be read at all, so there are no
		// fields to report on. The decoder's message is not passed on; it
		// quotes fragments of the input and names Go types.
		write(c, http.StatusBadRequest, codeMalformedRequest, "The request body must be a valid JSON object.", nil)
		return false
	}
	return true
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
