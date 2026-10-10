package apiresp

import "github.com/google/uuid"

// ParseUUIDParam parses value as a UUID. On failure it returns
// InvalidInput carrying a single invalid_uuid FieldError for name.
//
// Callers pass chi.URLParam(r, name) (apiresp has no chi dependency); the
// helper does not touch the request. Parse before any authorization or
// database access only where the error-precedence rule's stage 2 (400)
// applies.
func ParseUUIDParam(name, value string) (uuid.UUID, error) {
	id, err := uuid.Parse(value)
	if err != nil {
		return uuid.Nil, InvalidInput(FieldError{Field: name, Code: "invalid_uuid", Message: "must be a valid UUID"})
	}
	return id, nil
}
