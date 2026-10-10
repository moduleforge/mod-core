package apiresp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type decTarget struct {
	Name string `json:"name"`
	N    int    `json:"n"`
}

type validTarget struct {
	Name string `json:"name"`
}

var validateCalled bool

func (v *validTarget) Validate() []FieldError {
	validateCalled = true
	if v.Name == "" {
		return []FieldError{{Field: "name", Code: "required", Message: "name is required"}}
	}
	return nil
}

func runDecode(t *testing.T, body string, dst any, opts ...DecodeOption) (bool, *httptest.ResponseRecorder) {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	return DecodeJSON(rec, req, dst, opts...), rec
}

func assert400(t *testing.T, rec *httptest.ResponseRecorder, code, field string) {
	t.Helper()
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	var env Envelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.Error.Code != "invalid_input" || len(env.Error.Details) != 1 {
		t.Fatalf("envelope = %+v", env)
	}
	d := env.Error.Details[0]
	if d.Code != code || d.Field != field {
		t.Fatalf("detail = %+v, want code %q field %q", d, code, field)
	}
}

func TestDecodeJSON_Success(t *testing.T) {
	t.Parallel()
	var d decTarget
	ok, rec := runDecode(t, `{"name":"a","n":2}`+"\n", &d)
	if !ok || d.Name != "a" || d.N != 2 {
		t.Fatalf("ok=%v d=%+v", ok, d)
	}
	if rec.Body.Len() != 0 || rec.Code != http.StatusOK {
		t.Fatalf("wrote on success: %d %s", rec.Code, rec.Body)
	}
}

func TestDecodeJSON_Failures(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		body  string
		opts  []DecodeOption
		code  string
		field string
	}{
		{"malformed", `{"name":`, nil, "invalid_json_body", ""},
		{"empty", ``, nil, "invalid_json_body", ""},
		{"unknown field", `{"bogus":1}`, nil, "invalid_json_body", ""},
		{"trailing data", `{"name":"a"} {"name":"b"}`, nil, "invalid_json_body", ""},
		{"trailing garbage", `{"name":"a"} x`, nil, "invalid_json_body", ""},
		{"wrong type", `{"n":"str"}`, nil, "invalid_json_body", "n"},
		{"too large", `{"name":"` + strings.Repeat("a", 64) + `"}`, []DecodeOption{WithMaxBytes(16)}, "body_too_large", ""},
		{"prefix", `{`, []DecodeOption{WithCodePrefix("tasks")}, "tasks.invalid_json_body", ""},
		{"prefix too large", strings.Repeat(" ", 40) + `{}`, []DecodeOption{WithMaxBytes(16), WithCodePrefix("x")}, "x.body_too_large", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var d decTarget
			ok, rec := runDecode(t, tc.body, &d, tc.opts...)
			if ok {
				t.Fatal("expected failure")
			}
			assert400(t, rec, tc.code, tc.field)
		})
	}
}

func TestDecodeJSON_AllowUnknownFields(t *testing.T) {
	t.Parallel()
	var d decTarget
	ok, _ := runDecode(t, `{"name":"a","bogus":1}`, &d, WithAllowUnknownFields())
	if !ok || d.Name != "a" {
		t.Fatalf("ok=%v d=%+v", ok, d)
	}
}

func TestDecodeJSON_MaxBytesBoundary(t *testing.T) {
	t.Parallel()
	body := `{"name":"ab"}` // 13 bytes
	var d decTarget
	if ok, _ := runDecode(t, body, &d, WithMaxBytes(int64(len(body)))); !ok {
		t.Fatal("body at cap should succeed")
	}
	if ok, rec := runDecode(t, body, &d, WithMaxBytes(int64(len(body))-1)); ok {
		t.Fatal("body over cap should fail")
	} else {
		assert400(t, rec, "body_too_large", "")
	}
	// Non-positive keeps the default.
	if ok, _ := runDecode(t, body, &d, WithMaxBytes(0)); !ok {
		t.Fatal("WithMaxBytes(0) should keep default")
	}
	if DefaultMaxBodyBytes != 1<<20 {
		t.Fatal("default cap changed")
	}
}

func TestDecodeJSON_Validator(t *testing.T) {
	validateCalled = false
	var v validTarget
	ok, rec := runDecode(t, `{"name":""}`, &v)
	if ok {
		t.Fatal("expected failure")
	}
	assert400(t, rec, "required", "name")

	validateCalled = false
	ok, _ = runDecode(t, `{"bogus":`, &v)
	if ok || validateCalled {
		t.Fatalf("validator must not run on decode failure (ok=%v called=%v)", ok, validateCalled)
	}

	ok, rec = runDecode(t, `{"name":"x"}`, &v)
	if !ok || rec.Body.Len() != 0 {
		t.Fatalf("ok=%v body=%s", ok, rec.Body)
	}
}

func TestDecodeJSON_NilBodyAndDst(t *testing.T) {
	t.Parallel()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Body = nil
	var d decTarget
	if DecodeJSON(rec, req, &d) {
		t.Fatal("nil body should fail")
	}
	assert400(t, rec, "invalid_json_body", "")

	for _, dst := range []any{nil, d, (*decTarget)(nil)} {
		ok, rec := runDecode(t, `{}`, dst)
		if ok || rec.Code != http.StatusInternalServerError {
			t.Fatalf("dst %T: ok=%v status=%d", dst, ok, rec.Code)
		}
	}
}
