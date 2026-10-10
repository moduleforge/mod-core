// Package ordertest is a table-driven test helper that asserts the HTTP
// error-precedence order for a handler: 401 (unauthenticated), then 400
// (stateless request shape), then 403 (authorization, which also masks
// existence), then 404, with 409/422 only after authorization. See
// docs/mf-standards/architecture/http-error-precedence.md.
//
// It contains no handler framework: it drives an [http.Handler] with
// [httptest] requests and asserts statuses, so it works against any router.
//
// # Wiring a route
//
// Describe each route as a [Route] and pass them to [Run] from a test:
//
//	func TestErrorOrder(t *testing.T) {
//	    ordertest.Run(t, ordertest.Route{
//	        Name:         "PATCH /things/{id}",
//	        Handler:      router, // fully wired, including auth middleware
//	        Method:       http.MethodPatch,
//	        ExistingPath: "/things/" + existingID,
//	        MissingPath:  "/things/" + missingID,
//	        GoodBody:     []byte(`{"name":"ok"}`),
//	        BadBody:      []byte(`{"name":`),
//	        Authorized:   ordertest.Actor(1),
//	        Unauthorized: ordertest.Actor(2),
//	        Late: []ordertest.Late{
//	            {Name: "duplicate", Path: existingPath, Body: dupBody, WantStatus: 409},
//	        },
//	    })
//	}
//
// The Handler must resolve the actor from the request context (the
// [Identity] values install it via opctx), so wire the route without the
// production middleware that would overwrite the context identity.
//
// # Type-level routes (AuthzFirst)
//
// A route whose authorization is type-level (for example create-under-type)
// authorizes before it inspects the body. Set AuthzFirst: the Unauthorized
// plus BadBody assertions then expect 403 instead of 400. This is a
// documented, reviewed exception; do not use it to paper over a mis-ordered
// handler.
//
// # Routes that mask every miss
//
// A route that answers 403 for every id the caller may not act on, including
// ids that do not exist (for example one built on AuthorizeResolve), gives an
// authorized caller 403 on MissingPath too. Set AuthorizedMissingStatus to 403
// for such a route; the default is 404.
package ordertest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/moduleforge/core-api/apiresp"
	"github.com/moduleforge/core-api/opctx"
)

// Identity installs (or omits) an actor on the request context.
type Identity func(ctx context.Context) context.Context

// Anonymous returns the context unchanged: no actor.
var Anonymous Identity = func(ctx context.Context) context.Context { return ctx }

// Actor installs entityID as the regular actor (opctx.WithActor).
func Actor(entityID int64) Identity {
	return func(ctx context.Context) context.Context {
		return opctx.WithActor(ctx, entityID)
	}
}

// Sudo installs actorID as the regular actor and sudoID as the sudo actor.
func Sudo(actorID, sudoID int64) Identity {
	return func(ctx context.Context) context.Context {
		return opctx.WithSudoActor(opctx.WithActor(ctx, actorID), sudoID)
	}
}

// SudoOnly installs only a sudo actor, with no regular actor.
func SudoOnly(sudoID int64) Identity {
	return func(ctx context.Context) context.Context {
		return opctx.WithSudoActor(ctx, sudoID)
	}
}

// Late is a request that must be rejected only AFTER authorization.
type Late struct {
	Name       string
	Path       string // may differ from Route.ExistingPath; empty means ExistingPath
	Body       []byte
	WantStatus int // 409, 422, or a late 400
}

// Route describes one handler and the callers and requests used to probe it.
type Route struct {
	Name                    string
	Handler                 http.Handler // fully wired handler or router under test
	Method                  string
	ExistingPath            string   // a resource that exists and Unauthorized may not access
	MissingPath             string   // a well-formed id that does not exist
	GoodBody                []byte   // well-formed body; nil for bodyless routes
	BadBody                 []byte   // stateless-invalid body (malformed or shape-invalid); nil skips 400 assertions
	ContentType             string   // default "application/json" when a body is sent
	Authorized              Identity // allowed to act
	Unauthorized            Identity // authenticated but not allowed
	AuthorizedMissingStatus int      // status an authorized caller gets for MissingPath; default 404; use 403 when the route masks all misses
	AuthzFirst              bool     // documented, reviewed exception: type-level authorization before shape; then Unauthorized+BadBody must be 403
	Late                    []Late
}

// Run creates one subtest per route and per assertion
// (route.Name + "/<assertion>") and reports every violated ordering.
func Run(t *testing.T, routes ...Route) {
	t.Helper()
	for _, route := range routes {
		for _, a := range assertions(route) {
			t.Run(route.Name+"/"+a.name, func(t *testing.T) {
				for _, f := range a.run() {
					t.Error(f)
				}
			})
		}
	}
}

// assertion is one named check; run returns a failure message per violation.
type assertion struct {
	name string
	run  func() []string
}

// evaluate runs every assertion for routes and returns all failure messages.
// Run forwards the same assertions to t.Run; this form exists so the package
// can test that violations are detected.
func evaluate(routes ...Route) []string {
	var out []string
	for _, route := range routes {
		for _, a := range assertions(route) {
			out = append(out, a.run()...)
		}
	}
	return out
}

type response struct {
	status int
	body   []byte
}

func (r Route) do(id Identity, path string, body []byte) response {
	var rd *bytes.Reader
	var req *http.Request
	if body != nil {
		rd = bytes.NewReader(body)
		req = httptest.NewRequest(r.Method, path, rd)
		ct := r.ContentType
		if ct == "" {
			ct = "application/json"
		}
		req.Header.Set("Content-Type", ct)
	} else {
		req = httptest.NewRequest(r.Method, path, nil)
	}
	if id == nil {
		id = Anonymous
	}
	req = req.WithContext(id(req.Context()))
	rec := httptest.NewRecorder()
	r.Handler.ServeHTTP(rec, req)
	return response{status: rec.Code, body: rec.Body.Bytes()}
}

func (r Route) fail(assertion, identity string, got response, want string) string {
	return fmt.Sprintf("route %q: %s: identity %s: got status %d, want %s; body: %s",
		r.Name, assertion, identity, got.status, want, got.body)
}

// expect builds a check that issues one request and requires wantStatus.
func (r Route) expect(label, identity string, id Identity, path string, body []byte, wantStatus int) []string {
	got := r.do(id, path, body)
	if got.status != wantStatus {
		return []string{r.fail(label, identity, got, fmt.Sprint(wantStatus))}
	}
	return nil
}

func (r Route) missingStatus() int {
	if r.AuthorizedMissingStatus != 0 {
		return r.AuthorizedMissingStatus
	}
	return http.StatusNotFound
}

func assertions(r Route) []assertion {
	var as []assertion
	add := func(name string, f func() []string) { as = append(as, assertion{name, f}) }

	// 1. Authentication comes first.
	if r.BadBody != nil {
		add("anonymous-bad-body-existing=401", func() []string {
			return r.expect("anonymous + bad body on existing path", "anonymous", Anonymous, r.ExistingPath, r.BadBody, http.StatusUnauthorized)
		})
	}
	add("anonymous-good-body-existing=401", func() []string {
		return r.expect("anonymous + good body on existing path", "anonymous", Anonymous, r.ExistingPath, r.GoodBody, http.StatusUnauthorized)
	})
	add("anonymous-good-body-missing=401", func() []string {
		return r.expect("anonymous + good body on missing path", "anonymous", Anonymous, r.MissingPath, r.GoodBody, http.StatusUnauthorized)
	})
	if r.Authorized != nil {
		add("sudo-only-not-401", func() []string {
			got := r.do(SudoOnly(1<<40), r.ExistingPath, r.GoodBody)
			if got.status == http.StatusUnauthorized {
				return []string{r.fail("sudo-only actor is authenticated", "sudo-only", got, "anything but 401")}
			}
			return nil
		})
	}

	if r.Unauthorized != nil {
		// 2. Stateless shape (400) precedes authorization, with no existence signal.
		if r.BadBody != nil {
			want := http.StatusBadRequest
			if r.AuthzFirst {
				want = http.StatusForbidden
			}
			add("unauthorized-bad-body-existing", func() []string {
				return r.expect("unauthorized + bad body on existing path", "unauthorized", r.Unauthorized, r.ExistingPath, r.BadBody, want)
			})
			add("unauthorized-bad-body-missing", func() []string {
				return r.expect("unauthorized + bad body on missing path (must match existing-path status)", "unauthorized", r.Unauthorized, r.MissingPath, r.BadBody, want)
			})
		}

		// 3. 403 precedes 404 and is indistinguishable.
		add("unauthorized-good-body-403-indistinguishable", func() []string {
			ex := r.do(r.Unauthorized, r.ExistingPath, r.GoodBody)
			mi := r.do(r.Unauthorized, r.MissingPath, r.GoodBody)
			var fs []string
			if ex.status != http.StatusForbidden {
				fs = append(fs, r.fail("unauthorized + good body on existing path", "unauthorized", ex, "403"))
			}
			if mi.status != http.StatusForbidden {
				fs = append(fs, r.fail("unauthorized + good body on missing path", "unauthorized", mi, "403"))
			}
			if len(fs) == 0 && !sameError(ex.body, mi.body) {
				fs = append(fs, fmt.Sprintf("route %q: 403 on existing and missing paths are distinguishable; existing body: %s; missing body: %s",
					r.Name, ex.body, mi.body))
			}
			return fs
		})
	}

	// 4. Authorized caller.
	if r.Authorized != nil {
		add("authorized-good-body-missing", func() []string {
			return r.expect("authorized + good body on missing path", "authorized", r.Authorized, r.MissingPath, r.GoodBody, r.missingStatus())
		})
		add("authorized-good-body-existing-not-401-403", func() []string {
			got := r.do(r.Authorized, r.ExistingPath, r.GoodBody)
			if got.status == http.StatusUnauthorized || got.status == http.StatusForbidden {
				return []string{r.fail("authorized + good body on existing path", "authorized", got, "neither 401 nor 403")}
			}
			return nil
		})
	}

	// 5. Late errors only after authorization.
	for _, l := range r.Late {
		path := l.Path
		if path == "" {
			path = r.ExistingPath
		}
		if r.Authorized != nil {
			add("late/"+l.Name+"/authorized", func() []string {
				return r.expect("late "+l.Name, "authorized", r.Authorized, path, l.Body, l.WantStatus)
			})
		}
		if r.Unauthorized != nil {
			add("late/"+l.Name+"/unauthorized", func() []string {
				return r.expect("late "+l.Name+" must not leak to unauthorized caller", "unauthorized", r.Unauthorized, path, l.Body, http.StatusForbidden)
			})
		}
		add("late/"+l.Name+"/anonymous", func() []string {
			return r.expect("late "+l.Name, "anonymous", Anonymous, path, l.Body, http.StatusUnauthorized)
		})
	}
	return as
}

// sameError reports whether two error responses carry the same error.code and
// error.message after JSON normalization. Bodies that do not parse as the
// error envelope are compared byte for byte.
func sameError(a, b []byte) bool {
	var ea, eb apiresp.Envelope
	if json.Unmarshal(a, &ea) != nil || json.Unmarshal(b, &eb) != nil {
		return bytes.Equal(a, b)
	}
	return ea.Error.Code == eb.Error.Code && ea.Error.Message == eb.Error.Message
}
