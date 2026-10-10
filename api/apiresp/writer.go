package apiresp

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/moduleforge/core-api/opctx"
)

// WriteJSON encodes v as the top-level JSON body and writes status to w.
// It does not wrap v in any envelope — use it for bare success responses.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// WriteError maps err to the canonical status and nested error envelope,
// and writes it to w. It recognizes the sentinel errors declared in this
// package — and any error that wraps them, via errors.Is — attaches any
// field-level details err carries (see InvalidInput), logs 5xx responses
// server-side with request context, and never leaks err's raw text to the
// client on a 5xx.
func WriteError(w http.ResponseWriter, r *http.Request, err error) {
	code, status := classify(err)
	env := Envelope{Error: ErrorBody{Code: code, Message: publicMessage(err, code)}}
	if fe, ok := fieldErrors(err); ok {
		env.Error.Details = fe
	}
	if status >= 500 {
		logServerError(r.Context(), err)
	}
	WriteJSON(w, status, env)
}

// classify maps err to its reserved error.code and HTTP status via
// errors.Is against the sentinel set. Any error that does not match a
// known sentinel maps to ("internal_error", 500) — the default per the
// design doc's sentinel-to-status table.
func classify(err error) (code string, status int) {
	switch {
	case errors.Is(err, ErrUnauthenticated):
		return "unauthenticated", http.StatusUnauthorized
	case errors.Is(err, ErrForbidden):
		return "forbidden", http.StatusForbidden
	case errors.Is(err, ErrNotFound):
		return "not_found", http.StatusNotFound
	case errors.Is(err, ErrInvalidInput):
		return "invalid_input", http.StatusBadRequest
	case errors.Is(err, ErrConflict):
		return "conflict", http.StatusConflict
	case errors.Is(err, ErrUnprocessable):
		return "unprocessable_entity", http.StatusUnprocessableEntity
	default:
		return "internal_error", http.StatusInternalServerError
	}
}

// publicMessage returns the message safe to surface in the response body
// for the given error and its mapped code. For internal_error (5xx) it is
// always a fixed generic string — err's raw text is never returned to the
// client; it is only logged server-side by WriteError via logServerError.
// For the mapped 4xx sentinels it returns a safe, per-code default summary.
func publicMessage(err error, code string) string {
	switch code {
	case "unauthenticated":
		return "authentication is required"
	case "forbidden":
		return "you do not have access to this resource"
	case "not_found":
		return "the requested resource was not found"
	case "invalid_input":
		return "one or more fields are invalid"
	case "conflict":
		return "the request conflicts with the current state"
	case "unprocessable_entity":
		return "the request references a value that is not valid"
	default:
		// internal_error, or any future unmapped code: never echo err here.
		return "an internal error occurred"
	}
}

// logServerError logs a 5xx failure server-side with request context. It
// includes the request ID from opctx when one is available on ctx — opctx
// is set by HTTP middleware, so a request handled without that middleware
// (e.g. some test paths) simply logs without it.
//
// TODO: opctx has no dependency on apiresp today, so this one-way import is
// not a cycle; if that ever changes, this accessor needs to move to a
// location both packages can reach.
func logServerError(ctx context.Context, err error) {
	if reqID := opctx.RequestID(ctx); reqID != "" {
		slog.ErrorContext(ctx, "request failed", "err", err, "request_id", reqID)
		return
	}
	slog.ErrorContext(ctx, "request failed", "err", err)
}
