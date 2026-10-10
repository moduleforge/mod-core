package apiresp

// unprocessableError wraps ErrUnprocessable and carries the field-level
// details supplied to Unprocessable. Its Unwrap method makes errors.Is(err,
// ErrUnprocessable) succeed; WriteError's fieldErrors accessor recovers the
// details to surface in the response envelope.
type unprocessableError struct {
	details []FieldError
}

// Error implements the error interface. It returns the underlying
// sentinel's text; the client-facing message is decided separately by
// publicMessage in WriteError.
func (e *unprocessableError) Error() string {
	return ErrUnprocessable.Error()
}

// Unwrap makes errors.Is(err, ErrUnprocessable) succeed.
func (e *unprocessableError) Unwrap() error {
	return ErrUnprocessable
}

// fieldDetails implements fieldDetailCarrier.
func (e *unprocessableError) fieldDetails() []FieldError {
	return e.details
}

// Unprocessable builds an error that satisfies errors.Is(err,
// ErrUnprocessable) and carries the supplied field-level details.
// WriteError maps it to 422 unprocessable_entity. Calling Unprocessable()
// with no details is valid; WriteError then omits error.details entirely.
func Unprocessable(details ...FieldError) error {
	return &unprocessableError{details: details}
}
