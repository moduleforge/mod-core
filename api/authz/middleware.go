package authz

import (
	"net/http"

	"github.com/moduleforge/core-api/apiresp"
)

// RequireAuth is HTTP middleware that enforces stage 1 of the error-precedence
// rule (401 unauthenticated) before any handler code runs. See
// docs/mf-standards/architecture/http-error-precedence.md.
//
// It calls RequireAuthenticated on the request context; when no effective
// actor is present it writes the standard 401 "unauthenticated" envelope via
// apiresp.WriteError and does not call next. Otherwise it calls next with the
// request unchanged. Because it uses RequireAuthenticated, a sudo-assumed
// identity counts as authenticated (unlike opctx.ActorEntityID, which ignores
// the sudo actor).
//
// Mount it after the middleware that populates opctx (actor / sudo actor), and
// apply it only to route groups that require authentication; public routes must
// not get it. It works as r.Use(authz.RequireAuth) on a chi router or group and
// as a plain http.Handler wrapper.
func RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := RequireAuthenticated(r.Context()); err != nil {
			apiresp.WriteError(w, r, err)
			return
		}
		next.ServeHTTP(w, r)
	})
}
