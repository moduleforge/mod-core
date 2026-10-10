package authz_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/moduleforge/core-api/authz"
	"github.com/moduleforge/core-api/opctx"
)

// probe is a handler recording whether it was reached.
type probe struct {
	called bool
	status int
}

func (p *probe) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	p.called = true
	w.WriteHeader(p.status)
}

func assertUnauthenticated(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	var env struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope: %v (body %q)", err, rec.Body.String())
	}
	if env.Error.Code != "unauthenticated" {
		t.Fatalf("error.code = %q, want unauthenticated", env.Error.Code)
	}
}

func TestRequireAuthNoActor(t *testing.T) {
	p := &probe{status: http.StatusOK}
	rec := httptest.NewRecorder()
	authz.RequireAuth(p).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	assertUnauthenticated(t, rec)
	if p.called {
		t.Fatal("next invoked without an actor")
	}
}

func TestRequireAuthWithActor(t *testing.T) {
	p := &probe{status: http.StatusTeapot}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = req.WithContext(opctx.WithActor(req.Context(), 7))
	rec := httptest.NewRecorder()
	authz.RequireAuth(p).ServeHTTP(rec, req)
	if !p.called {
		t.Fatal("next not invoked for authenticated actor")
	}
	if rec.Code != http.StatusTeapot {
		t.Fatalf("status = %d, want %d preserved from next", rec.Code, http.StatusTeapot)
	}
}

func TestRequireAuthSudoAware(t *testing.T) {
	p := &probe{status: http.StatusOK}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	ctx := opctx.WithSudoActor(req.Context(), 9)
	if _, ok := opctx.ActorEntityID(ctx); ok {
		t.Fatal("opctx.ActorEntityID unexpectedly reported an actor for a sudo-only ctx")
	}
	rec := httptest.NewRecorder()
	authz.RequireAuth(p).ServeHTTP(rec, req.WithContext(ctx))
	if !p.called {
		t.Fatal("next not invoked for sudo-only ctx")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestRequireAuthPrecedenceOverBadBody(t *testing.T) {
	reached := false
	h := authz.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reached = true
		w.WriteHeader(http.StatusBadRequest)
	}))
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("{not json"))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	assertUnauthenticated(t, rec)
	if reached {
		t.Fatal("handler reached despite missing authentication")
	}
}

func TestRequireAuthChi(t *testing.T) {
	protected := &probe{status: http.StatusOK}
	public := &probe{status: http.StatusOK}

	r := chi.NewRouter()
	r.Get("/public", public.ServeHTTP)
	r.Group(func(g chi.Router) {
		g.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				if req.Header.Get("X-Actor") != "" {
					req = req.WithContext(opctx.WithActor(req.Context(), 1))
				}
				next.ServeHTTP(w, req)
			})
		})
		g.Use(authz.RequireAuth)
		g.Get("/private", protected.ServeHTTP)
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/private", nil))
	assertUnauthenticated(t, rec)
	if protected.called {
		t.Fatal("protected handler reached without actor")
	}

	req := httptest.NewRequest(http.MethodGet, "/private", nil)
	req.Header.Set("X-Actor", "1")
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if !protected.called || rec.Code != http.StatusOK {
		t.Fatalf("authenticated request: called=%v status=%d", protected.called, rec.Code)
	}

	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/public", nil))
	if !public.called || rec.Code != http.StatusOK {
		t.Fatalf("public route: called=%v status=%d", public.called, rec.Code)
	}
}
