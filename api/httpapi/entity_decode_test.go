package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// TestEntityHandlers_StatelessShape400 verifies that the entity-family
// handlers reject malformed bodies, unknown fields, and malformed path UUIDs
// with a structured 400 carrying the shared detail codes.
func TestEntityHandlers_StatelessShape400(t *testing.T) {
	good := uuid.New().String()
	cases := []struct {
		name, method, path, body, wantCode string
	}{
		{"np create malformed", http.MethodPost, "/entities/natural-persons", "{bad", "invalid_json_body"},
		{"np create unknown field", http.MethodPost, "/entities/natural-persons", `{"given_name":"a","bogus":1}`, ""},
		{"np create empty body", http.MethodPost, "/entities/natural-persons", "", "invalid_json_body"},
		{"np update malformed", http.MethodPut, "/entities/natural-persons/" + good, "{bad", "invalid_json_body"},
		{"np update unknown field", http.MethodPut, "/entities/natural-persons/" + good, `{"bogus":1}`, ""},
		{"np update bad uuid", http.MethodPut, "/entities/natural-persons/not-a-uuid", `{}`, "invalid_uuid"},
		{"np get bad uuid", http.MethodGet, "/entities/natural-persons/not-a-uuid", "", "invalid_uuid"},
		{"corp create malformed", http.MethodPost, "/entities/corporations", "{bad", "invalid_json_body"},
		{"corp create unknown field", http.MethodPost, "/entities/corporations", `{"legal_name":"a","bogus":1}`, ""},
		{"corp update malformed", http.MethodPut, "/entities/corporations/" + good, "{bad", "invalid_json_body"},
		{"corp update bad uuid", http.MethodPut, "/entities/corporations/not-a-uuid", `{}`, "invalid_uuid"},
		{"corp get bad uuid", http.MethodGet, "/entities/corporations/not-a-uuid", "", "invalid_uuid"},
		{"sa create malformed", http.MethodPost, "/entities/service-accounts", "{bad", "invalid_json_body"},
		{"sa create unknown field", http.MethodPost, "/entities/service-accounts", `{"label":"a","bogus":1}`, ""},
		{"sa update malformed", http.MethodPut, "/entities/service-accounts/" + good, "{bad", "invalid_json_body"},
		{"sa update bad uuid", http.MethodPut, "/entities/service-accounts/not-a-uuid", `{}`, "invalid_uuid"},
		{"sa get bad uuid", http.MethodGet, "/entities/service-accounts/not-a-uuid", "", "invalid_uuid"},
		{"entity get bad uuid", http.MethodGet, "/entities/not-a-uuid", "", "invalid_uuid"},
		{"entity archive bad uuid", http.MethodDelete, "/entities/not-a-uuid", "", "invalid_uuid"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			router := NewRouter(buildTestDeps(&fakeEntityService{}, &fakeNaturalPersonService{}, &fakeCorporationService{}, &fakeServiceAccountService{}))
			var body *bytes.Buffer
			if tc.body != "" || tc.method != http.MethodGet && tc.method != http.MethodDelete {
				body = bytes.NewBufferString(tc.body)
			}
			req := adminReq(tc.method, tc.path, body)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status: got %d, want 400; body=%s", rec.Code, rec.Body.String())
			}
			var env map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
				t.Fatalf("decode envelope: %v", err)
			}
			raw := rec.Body.String()
			if !strings.Contains(raw, "invalid_input") {
				t.Errorf("want invalid_input code in %s", raw)
			}
			if tc.wantCode != "" && !strings.Contains(raw, tc.wantCode) {
				t.Errorf("want detail code %q in %s", tc.wantCode, raw)
			}
		})
	}
}
