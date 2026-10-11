package httpapi

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/moduleforge/core-api/apiresp"
)

func serveApps(t *testing.T, q *appsFakeQuerier, az *appsFakeAuthorizer, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	appsRouter(newTestAppsHandler(q, az)).ServeHTTP(rec, req)
	return rec
}

// Bad body + unauthorized caller on an existing app is 400, not 403.
func TestAppsUpdateApp_BadBodyBeforeAuthz(t *testing.T) {
	q := newAppsFakeQuerier()
	appUUID := q.seedApp("demo", "Demo App")
	az := &appsFakeAuthorizer{err: apiresp.ErrForbidden}

	for name, body := range map[string]string{
		"malformed":     `{"name":`,
		"unknown field": `{"name":"x","bogus":1}`,
		"empty":         ``,
	} {
		t.Run(name, func(t *testing.T) {
			rec := serveApps(t, q, az, adminReq(http.MethodPut, "/apps/"+appUUID.String(), bytes.NewBufferString(body)))
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status: got %d, want 400 — %s", rec.Code, rec.Body.String())
			}
			if q.getAppByUUIDCalls != 0 {
				t.Errorf("GetAppByUUID calls: got %d, want 0", q.getAppByUUIDCalls)
			}
		})
	}
}

// Bad body on a missing app is also 400 (no existence signal).
func TestAppsUpdateApp_BadBodyMissingAppIs400(t *testing.T) {
	q := newAppsFakeQuerier()
	missing := "/apps/00000000-0000-0000-0000-000000000abc"
	rec := serveApps(t, q, &appsFakeAuthorizer{err: apiresp.ErrForbidden},
		adminReq(http.MethodPut, missing, bytes.NewBufferString(`{"name":`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400", rec.Code)
	}
}

func TestAppsUpdateApp_BodyOverCapRejected(t *testing.T) {
	q := newAppsFakeQuerier()
	appUUID := q.seedApp("demo", "Demo App")
	big := `{"name":"` + strings.Repeat("a", apiresp.DefaultMaxBodyBytes+10) + `"}`
	rec := serveApps(t, q, &appsFakeAuthorizer{}, adminReq(http.MethodPut, "/apps/"+appUUID.String(), bytes.NewBufferString(big)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "body_too_large") {
		t.Errorf("want body_too_large detail, got %s", rec.Body.String())
	}
}

// Malformed UUID + anonymous is 401; with an identity it is 400 invalid_uuid.
func TestAppsMalformedUUID_Order(t *testing.T) {
	for _, m := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
		t.Run(m, func(t *testing.T) {
			q := newAppsFakeQuerier()
			az := &appsFakeAuthorizer{err: apiresp.ErrForbidden}
			body := func() *bytes.Buffer { return bytes.NewBufferString(`{"name":"x"}`) }

			anon := httptest.NewRequest(m, "/apps/not-a-uuid", body())
			if rec := serveApps(t, q, az, anon); rec.Code != http.StatusUnauthorized {
				t.Errorf("anonymous: got %d, want 401", rec.Code)
			}
			rec := serveApps(t, q, az, adminReq(m, "/apps/not-a-uuid", body()))
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("authenticated: got %d, want 400", rec.Code)
			}
			if !strings.Contains(rec.Body.String(), "invalid_uuid") {
				t.Errorf("want invalid_uuid detail, got %s", rec.Body.String())
			}
		})
	}
}

func TestAppsCreate_UnknownFieldAndOverCapRejected(t *testing.T) {
	q := newAppsFakeQuerier()
	rec := serveApps(t, q, &appsFakeAuthorizer{},
		adminReq(http.MethodPost, "/apps", bytes.NewBufferString(`{"slug":"a","name":"b","extra":true}`)))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("unknown field: got %d, want 400", rec.Code)
	}
	big := `{"slug":"a","name":"` + strings.Repeat("a", apiresp.DefaultMaxBodyBytes+10) + `"}`
	rec = serveApps(t, q, &appsFakeAuthorizer{}, adminReq(http.MethodPost, "/apps", bytes.NewBufferString(big)))
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "body_too_large") {
		t.Errorf("over cap: got %d %s", rec.Code, rec.Body.String())
	}
}
