// Package apiresp is the shared HTTP response and error-writing contract for
// ModuleForge modules. It owns the canonical sentinel error set, the wire
// envelope types, and the encoders (WriteJSON, WriteError) that every
// module's HTTP handlers use in place of a locally copy-pasted
// jsonOK/jsonErr/writeServiceErr trio.
//
// See docs/mf-standards/architecture/api-response-design.md — the
// "Go-layer ownership" section is the source of truth for the shape and
// behaviour this package implements; the sentinel names, function
// signatures, and envelope shape here follow it exactly.
package apiresp

import "errors"

// Sentinel errors recognized by WriteError. Each corresponds to exactly one
// reserved error.code / HTTP status pair (see the design doc's "Reserved
// core codes" table). Handlers and service methods return these directly,
// or wrap them with fmt.Errorf("...: %w", ...) — WriteError classifies via
// errors.Is, so wrapping does not break the mapping.
var (
	ErrUnauthenticated = errors.New("unauthenticated")
	ErrForbidden       = errors.New("forbidden")
	ErrNotFound        = errors.New("not_found")
	ErrInvalidInput    = errors.New("invalid_input")
	ErrConflict        = errors.New("conflict")

	// ErrUnprocessable marks a request body that is well-formed and
	// authorized but semantically invalid, e.g. a referenced value that does
	// not exist or lies outside a registry. Reserved code
	// unprocessable_entity, status 422.
	ErrUnprocessable = errors.New("unprocessable")
)
