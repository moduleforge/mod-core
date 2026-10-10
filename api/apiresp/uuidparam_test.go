package apiresp

import (
	"errors"
	"testing"

	"github.com/google/uuid"
)

func TestParseUUIDParam(t *testing.T) {
	t.Parallel()
	want := uuid.New()
	got, err := ParseUUIDParam("id", want.String())
	if err != nil || got != want {
		t.Fatalf("valid: got %v, %v", got, err)
	}
	for _, v := range []string{"", "nope", "123"} {
		id, err := ParseUUIDParam("id", v)
		if err == nil || id != uuid.Nil {
			t.Fatalf("%q: expected error, got %v, %v", v, id, err)
		}
		if !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("%q: not invalid input: %v", v, err)
		}
		fe, ok := fieldErrors(err)
		if !ok || len(fe) != 1 || fe[0].Field != "id" || fe[0].Code != "invalid_uuid" {
			t.Fatalf("%q: bad details %+v", v, fe)
		}
	}
}
