package apiresp

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
)

// Validator may be implemented by a decode target to run stateless,
// shape-level checks (required fields, ranges, formats) immediately after a
// successful decode. It must not touch a database or service; checks that
// need state belong in the handler after authorization. A non-empty result
// is surfaced verbatim as a 400 invalid_input response.
type Validator interface{ Validate() []FieldError }

// DefaultMaxBodyBytes is the request body cap DecodeJSON applies unless
// overridden by WithMaxBytes.
const DefaultMaxBodyBytes = 1 << 20

const (
	codeInvalidJSONBody = "invalid_json_body"
	codeBodyTooLarge    = "body_too_large"
)

type decodeConfig struct {
	maxBytes     int64
	codePrefix   string
	allowUnknown bool
}

// DecodeOption customizes DecodeJSON.
type DecodeOption func(*decodeConfig)

// WithMaxBytes overrides the body size cap. A value <= 0 keeps the default.
func WithMaxBytes(n int64) DecodeOption {
	return func(c *decodeConfig) {
		if n > 0 {
			c.maxBytes = n
		}
	}
}

// WithCodePrefix prefixes failure detail codes, e.g. prefix "tasks" yields
// "tasks.invalid_json_body". The default prefix is empty.
func WithCodePrefix(p string) DecodeOption {
	return func(c *decodeConfig) { c.codePrefix = p }
}

// WithAllowUnknownFields opts out of the default strict rejection of
// unknown JSON fields.
func WithAllowUnknownFields() DecodeOption {
	return func(c *decodeConfig) { c.allowUnknown = true }
}

// DecodeJSON decodes the request body into dst (which must be a non-nil
// pointer) with a size cap, unknown-field rejection (unless
// WithAllowUnknownFields), and trailing-data rejection, then runs dst's
// Validator hook if it implements one.
//
// It returns true on success and writes nothing. On any failure it has
// ALREADY written the response (a 400 invalid_input envelope via WriteError,
// or a 500 for a programmer error such as a nil or non-pointer dst) and
// returns false; the caller must simply return. It never touches a database
// or service.
func DecodeJSON(w http.ResponseWriter, r *http.Request, dst any, opts ...DecodeOption) bool {
	cfg := decodeConfig{maxBytes: DefaultMaxBodyBytes}
	for _, o := range opts {
		if o != nil {
			o(&cfg)
		}
	}

	if rv := reflect.ValueOf(dst); dst == nil || rv.Kind() != reflect.Ptr || rv.IsNil() {
		WriteError(w, r, fmt.Errorf("apiresp.DecodeJSON: dst must be a non-nil pointer, got %T", dst))
		return false
	}

	fail := func(code, field, msg string) bool {
		WriteError(w, r, InvalidInput(FieldError{Field: field, Code: prefixed(cfg.codePrefix, code), Message: msg}))
		return false
	}

	if r.Body == nil || r.Body == http.NoBody {
		return fail(codeInvalidJSONBody, "", "request body must be valid JSON")
	}

	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, cfg.maxBytes))
	if !cfg.allowUnknown {
		dec.DisallowUnknownFields()
	}

	if err := dec.Decode(dst); err != nil {
		return failDecode(cfg, fail, err)
	}
	// A second Decode must hit EOF; anything else is trailing data.
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			return fail(codeBodyTooLarge, "", "request body is too large")
		}
		return fail(codeInvalidJSONBody, "", "request body must contain a single JSON value")
	}

	if v, ok := dst.(Validator); ok {
		if details := v.Validate(); len(details) > 0 {
			WriteError(w, r, InvalidInput(details...))
			return false
		}
	}
	return true
}

func failDecode(_ decodeConfig, fail func(code, field, msg string) bool, err error) bool {
	var mbe *http.MaxBytesError
	if errors.As(err, &mbe) {
		return fail(codeBodyTooLarge, "", "request body is too large")
	}
	var ute *json.UnmarshalTypeError
	if errors.As(err, &ute) {
		return fail(codeInvalidJSONBody, ute.Field, "field has the wrong type")
	}
	if strings.HasPrefix(err.Error(), "json: unknown field") {
		return fail(codeInvalidJSONBody, "", "request body contains an unknown field")
	}
	return fail(codeInvalidJSONBody, "", "request body must be valid JSON")
}

func prefixed(prefix, code string) string {
	if prefix == "" {
		return code
	}
	return prefix + "." + code
}
