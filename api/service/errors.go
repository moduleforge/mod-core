package service

import "github.com/moduleforge/core-api/apiresp"

// ErrNotFound is returned when a requested resource does not exist.
var ErrNotFound = apiresp.ErrNotFound

// ErrForbidden is returned when the caller lacks permission for the operation.
var ErrForbidden = apiresp.ErrForbidden

// ErrInvalidInput is returned when the caller supplies invalid or missing input.
var ErrInvalidInput = apiresp.ErrInvalidInput

// ErrConflict is returned when the request conflicts with the current state
// (e.g. a uniqueness violation or optimistic-concurrency clash).
var ErrConflict = apiresp.ErrConflict

// ErrUnprocessable is returned when the request is well-formed and authorized
// but semantically invalid (e.g. a referenced value that does not exist).
var ErrUnprocessable = apiresp.ErrUnprocessable
