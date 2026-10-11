package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/moduleforge/core-api/apiresp/ordertest"
	"github.com/moduleforge/core-api/observer"
)

// mountedRouters builds the three real routers the way a composing app does:
// NewRouter for /entities/*, and the two registrars onto one shared router.
func mountedRouters(t *testing.T) (core, shared chi.Router) {
	t.Helper()
	core = NewRouter(buildTestDeps(nil, nil, nil, nil))

	apps := newTestAppsHandler(newAppsFakeQuerier(), &appsFakeAuthorizer{})
	fck := NewFieldCryptoKeyHandler(&fakeAppsDB{tx: &fakeAppsTx{}}, newFCKFakeQuerier(), &appsFakeAuthorizer{}, observer.NewObserverGroup(), nil)
	shared = chi.NewRouter()
	RegisterAppRoutes(shared, apps)
	RegisterFieldCryptoKeyRoutes(shared, fck)
	shared.Get("/peer/ping", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	return core, shared
}

func do(h http.Handler, id ordertest.Identity, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(id(req.Context()))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

type mountedRoute struct {
	name   string
	h      http.Handler
	method string
	path   string
}

// allMountedRoutes lists every route of the three mounted groups.
func allMountedRoutes(core, shared http.Handler) []mountedRoute {
	const badUUID = "not-a-uuid"
	return []mountedRoute{
		{"core POST natural-persons", core, http.MethodPost, "/entities/natural-persons"},
		{"core GET natural-persons", core, http.MethodGet, "/entities/natural-persons/" + badUUID},
		{"core PUT natural-persons", core, http.MethodPut, "/entities/natural-persons/" + badUUID},
		{"core POST corporations", core, http.MethodPost, "/entities/corporations"},
		{"core GET corporations", core, http.MethodGet, "/entities/corporations/" + badUUID},
		{"core PUT corporations", core, http.MethodPut, "/entities/corporations/" + badUUID},
		{"core POST service-accounts", core, http.MethodPost, "/entities/service-accounts"},
		{"core GET service-accounts", core, http.MethodGet, "/entities/service-accounts/" + badUUID},
		{"core PUT service-accounts", core, http.MethodPut, "/entities/service-accounts/" + badUUID},
		{"core GET entity", core, http.MethodGet, "/entities/" + badUUID},
		{"core GET display-name", core, http.MethodGet, "/entities/" + badUUID + "/display-name"},
		{"core DELETE entity", core, http.MethodDelete, "/entities/" + badUUID},
		{"apps POST", shared, http.MethodPost, "/apps"},
		{"apps GET list", shared, http.MethodGet, "/apps"},
		{"apps GET one", shared, http.MethodGet, "/apps/" + badUUID},
		{"apps PUT", shared, http.MethodPut, "/apps/" + badUUID},
		{"apps DELETE", shared, http.MethodDelete, "/apps/" + badUUID},
		{"fck GET list", shared, http.MethodGet, "/field-crypto-keys"},
		{"fck POST rotations", shared, http.MethodPost, "/field-crypto-keys/rotations"},
		{"fck POST mark-compromised", shared, http.MethodPost, "/field-crypto-keys/not-a-number/mark-compromised"},
		{"fck PUT grace", shared, http.MethodPut, "/field-crypto-keys/not-a-number/grace"},
	}
}

// TestRequireAuthMounted_AnonymousIs401 proves the gate covers every route of
// every group, ahead of body parsing and path-UUID parsing: a malformed body
// and a malformed UUID are both 401 for an anonymous caller, never 400.
func TestRequireAuthMounted_AnonymousIs401(t *testing.T) {
	core, shared := mountedRouters(t)
	good := `{"slug":"a","name":"b"}`
	malformed := `{"slug":`

	routes := allMountedRoutes(core, shared)
	for _, rt := range routes {
		for _, body := range []string{malformed, good, ""} {
			t.Run(rt.name+"/body="+body, func(t *testing.T) {
				rec := do(rt.h, ordertest.Anonymous, rt.method, rt.path, body)
				if rec.Code != http.StatusUnauthorized {
					t.Fatalf("status: got %d, want 401 - body: %s", rec.Code, rec.Body.String())
				}
			})
		}
	}
}

// TestRequireAuthMounted_SudoOnlyPassesGate proves the gate is sudo-aware on
// every route: a sudo-only identity is never rejected with 401 once the
// sudo-unaware inline checks are gone. The status past the gate is
// unconstrained here (400/403/404 from the stubbed deps are all fine).
func TestRequireAuthMounted_SudoOnlyPassesGate(t *testing.T) {
	core, shared := mountedRouters(t)
	for _, rt := range allMountedRoutes(core, shared) {
		t.Run(rt.name, func(t *testing.T) {
			// The core router is built with nil services, so a request that
			// clears the gate may panic in the handler. A panic proves the
			// request got past the 401 gate, which is all this test asserts.
			defer func() { _ = recover() }()
			rec := do(rt.h, ordertest.SudoOnly(7), rt.method, rt.path, `{"slug":"a","name":"b"}`)
			if rec.Code == http.StatusUnauthorized {
				t.Fatalf("sudo-only identity got 401 - body: %s", rec.Body.String())
			}
		})
	}
}

// TestRequireAuthMounted_RegistrarDoesNotGatePeers proves the registrars scope
// RequireAuth to their own group: an unrelated route on the shared router
// stays reachable anonymously.
func TestRequireAuthMounted_RegistrarDoesNotGatePeers(t *testing.T) {
	_, shared := mountedRouters(t)
	rec := do(shared, ordertest.Anonymous, http.MethodGet, "/peer/ping", "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("peer route: got %d, want 204 - body: %s", rec.Code, rec.Body.String())
	}
}
