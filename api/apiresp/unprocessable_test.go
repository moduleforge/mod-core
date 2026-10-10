package apiresp_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/moduleforge/core-api/apiresp"
)

func writeErr(t *testing.T, err error) (int, map[string]json.RawMessage, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	apiresp.WriteError(rec, httptest.NewRequest(http.MethodPost, "/x", nil), err)
	var env struct {
		Error map[string]json.RawMessage `json:"error"`
	}
	if e := json.Unmarshal(rec.Body.Bytes(), &env); e != nil {
		t.Fatalf("decode: %v", e)
	}
	return rec.Code, env.Error, rec.Body.String()
}

func TestUnprocessable_Classification(t *testing.T) {
	for name, err := range map[string]error{
		"constructor": apiresp.Unprocessable(),
		"wrapped":     fmt.Errorf("x: %w", apiresp.ErrUnprocessable),
	} {
		if !errors.Is(err, apiresp.ErrUnprocessable) {
			t.Errorf("%s: errors.Is failed", name)
		}
		code, e, _ := writeErr(t, err)
		if code != http.StatusUnprocessableEntity {
			t.Errorf("%s: status %d", name, code)
		}
		if string(e["code"]) != `"unprocessable_entity"` {
			t.Errorf("%s: code %s", name, e["code"])
		}
	}
}

func TestUnprocessable_DetailsAndMessage(t *testing.T) {
	fe := apiresp.FieldError{Field: "role", Code: "unknown_role", Message: "No such role."}
	status, e, _ := writeErr(t, apiresp.Unprocessable(fe))
	if status != 422 {
		t.Fatalf("status %d", status)
	}
	if string(e["message"]) != `"the request references a value that is not valid"` {
		t.Errorf("message %s", e["message"])
	}
	var details []apiresp.FieldError
	if err := json.Unmarshal(e["details"], &details); err != nil || len(details) != 1 || details[0] != fe {
		t.Errorf("details %s (%v)", e["details"], err)
	}

	_, e, _ = writeErr(t, apiresp.Unprocessable())
	if _, ok := e["details"]; ok {
		t.Error("details key present with no details")
	}
}

func TestUnprocessable_NoTextEcho(t *testing.T) {
	err := fmt.Errorf("secret-detail-xyz: %w", apiresp.ErrUnprocessable)
	_, _, body := writeErr(t, err)
	if strings.Contains(body, "secret-detail-xyz") {
		t.Errorf("underlying error text echoed: %s", body)
	}
}

func TestClassify_ExistingSentinelsUnchanged(t *testing.T) {
	cases := []struct {
		err    error
		code   string
		status int
	}{
		{apiresp.ErrUnauthenticated, "unauthenticated", 401},
		{apiresp.ErrForbidden, "forbidden", 403},
		{apiresp.ErrNotFound, "not_found", 404},
		{apiresp.ErrInvalidInput, "invalid_input", 400},
		{apiresp.ErrConflict, "conflict", 409},
		{errors.New("boom"), "internal_error", 500},
	}
	for _, c := range cases {
		status, e, _ := writeErr(t, c.err)
		if status != c.status || string(e["code"]) != `"`+c.code+`"` {
			t.Errorf("%v: got %d %s", c.err, status, e["code"])
		}
	}
}
