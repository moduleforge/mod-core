package ordertest

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/moduleforge/core-api/apiresp"
	"github.com/moduleforge/core-api/authz"
	"github.com/moduleforge/core-api/entity"
	"github.com/moduleforge/core-api/opctx"
	coredb "github.com/moduleforge/core-model/db"
)

var (
	existingID = uuid.MustParse("00000000-0000-0000-0000-0000000000a1")
	missingID  = uuid.MustParse("00000000-0000-0000-0000-0000000000a2")
)

const (
	ownerActor    = 1
	strangerActor = 2
)

// stubQuerier embeds the Querier interface (nil) and overrides the single
// method the Resolver calls.
type stubQuerier struct {
	coredb.Querier
	rows map[uuid.UUID]int64
}

func (s stubQuerier) GetEntityByUUID(_ context.Context, u uuid.UUID) (coredb.GetEntityByUUIDRow, error) {
	id, ok := s.rows[u]
	if !ok {
		return coredb.GetEntityByUUIDRow{}, pgx.ErrNoRows
	}
	return coredb.GetEntityByUUIDRow{ID: id, Uuid: u}, nil
}

// ownerAuthorizer allows only the owner actor (or sudo actor) to act.
type ownerAuthorizer struct{}

func (ownerAuthorizer) Authorize(ctx context.Context, _ string, _ *int64) error {
	if id, ok := opctx.EffectiveActorEntityID(ctx); ok && id == ownerActor {
		return nil
	}
	return apiresp.ErrForbidden
}

type payload struct {
	Name string `json:"name"`
}

func (p payload) Validate() []apiresp.FieldError { return nil }

func idFrom(r *http.Request) (uuid.UUID, error) {
	return apiresp.ParseUUIDParam("id", strings.TrimPrefix(r.URL.Path, "/things/"))
}

// lateErr maps the body name to a late (post-authorization) error.
func lateErr(name string) error {
	switch name {
	case "dup":
		return apiresp.Conflict()
	case "unproc":
		return apiresp.Unprocessable()
	}
	return nil
}

// goodHandler is the correct order: auth, shape, authorize (masking misses),
// then late errors.
//
// When domain is nil the entity store lacks missingID, so AuthorizeResolve
// masks the miss as 403 for everyone. When domain is non-nil both ids have
// entity rows (so the authorizer decides) and the handler returns 404 for ids
// absent from domain, after authorization.
func goodHandler(domain map[uuid.UUID]bool) http.Handler {
	rows := map[uuid.UUID]int64{existingID: 10}
	if domain != nil {
		rows[missingID] = 11
	}
	q := stubQuerier{rows: rows}
	res := entity.NewResolver()
	return authz.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var p payload
		if !apiresp.DecodeJSON(w, r, &p) {
			return
		}
		u, err := idFrom(r)
		if err != nil {
			apiresp.WriteError(w, r, err)
			return
		}
		if _, err := res.AuthorizeResolve(r.Context(), q, ownerAuthorizer{}, "update", u, "thing"); err != nil {
			apiresp.WriteError(w, r, err)
			return
		}
		if domain != nil && !domain[u] {
			apiresp.WriteError(w, r, apiresp.ErrNotFound)
			return
		}
		if err := lateErr(p.Name); err != nil {
			apiresp.WriteError(w, r, err)
			return
		}
		apiresp.WriteJSON(w, http.StatusOK, p)
	}))
}

func baseRoute(h http.Handler) Route {
	return Route{
		Name:         "PATCH /things/{id}",
		Handler:      h,
		Method:       http.MethodPatch,
		ExistingPath: "/things/" + existingID.String(),
		MissingPath:  "/things/" + missingID.String(),
		GoodBody:     []byte(`{"name":"ok"}`),
		BadBody:      []byte(`{"name":`),
		Authorized:   Actor(ownerActor),
		Unauthorized: Actor(strangerActor),
		Late: []Late{
			{Name: "duplicate", Body: []byte(`{"name":"dup"}`), WantStatus: http.StatusConflict},
			{Name: "unprocessable", Body: []byte(`{"name":"unproc"}`), WantStatus: http.StatusUnprocessableEntity},
		},
	}
}

func TestRun_MaskedMissRoute(t *testing.T) {
	t.Parallel()
	r := baseRoute(goodHandler(nil))
	r.AuthorizedMissingStatus = http.StatusForbidden
	Run(t, r)
}

func TestRun_DomainLookup404Route(t *testing.T) {
	t.Parallel()
	Run(t, baseRoute(goodHandler(map[uuid.UUID]bool{existingID: true})))
}

func TestRun_AuthzFirstRoute(t *testing.T) {
	t.Parallel()
	q := stubQuerier{rows: map[uuid.UUID]int64{existingID: 10}}
	h := authz.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, err := idFrom(r)
		if err == nil {
			_, err = entity.NewResolver().AuthorizeResolve(r.Context(), q, ownerAuthorizer{}, "create", u, "thing")
		}
		if err != nil {
			apiresp.WriteError(w, r, err)
			return
		}
		var p payload
		if !apiresp.DecodeJSON(w, r, &p) {
			return
		}
		apiresp.WriteJSON(w, http.StatusOK, p)
	}))
	r := baseRoute(h)
	r.AuthzFirst = true
	r.AuthorizedMissingStatus = http.StatusForbidden
	r.Late = nil
	Run(t, r)
}

func TestRun_SudoHelpers(t *testing.T) {
	t.Parallel()
	ctx := Sudo(3, 4)(context.Background())
	if a, _ := opctx.ActorEntityID(ctx); a != 3 {
		t.Errorf("Sudo actor = %d; want 3", a)
	}
	if s, _ := opctx.SudoActorEntityID(ctx); s != 4 {
		t.Errorf("Sudo sudo actor = %d; want 4", s)
	}
	ctx = SudoOnly(5)(context.Background())
	if _, ok := opctx.ActorEntityID(ctx); ok {
		t.Error("SudoOnly must not install a regular actor")
	}
	if s, _ := opctx.SudoActorEntityID(ctx); s != 5 {
		t.Errorf("SudoOnly sudo actor = %d; want 5", s)
	}
}

func TestEvaluate_GoodHandlerHasNoFailures(t *testing.T) {
	t.Parallel()
	r := baseRoute(goodHandler(nil))
	r.AuthorizedMissingStatus = http.StatusForbidden
	if fs := evaluate(r); len(fs) != 0 {
		t.Fatalf("unexpected failures: %v", fs)
	}
}

// wrongHandler is a deliberately mis-ordered handler; each flag introduces
// one ordering violation.
type wrongHandler struct {
	shapeBeforeAuth     bool // 400 before 401
	notFoundBeforeAuthz bool // 404 for a miss before 403
	shapeAfterAuthz     bool // 400 after 403
	lateLeaks           bool // late 422 before authorization
}

func (wh wrongHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body := func() (payload, bool) {
		var p payload
		ok := apiresp.DecodeJSON(w, r, &p)
		return p, ok
	}
	var p payload
	if wh.shapeBeforeAuth {
		var ok bool
		if p, ok = body(); !ok {
			return
		}
	}
	if err := authz.RequireAuthenticated(r.Context()); err != nil {
		apiresp.WriteError(w, r, err)
		return
	}
	u, err := idFrom(r)
	if err != nil {
		apiresp.WriteError(w, r, err)
		return
	}
	if wh.notFoundBeforeAuthz && u == missingID {
		apiresp.WriteError(w, r, apiresp.ErrNotFound)
		return
	}
	if wh.lateLeaks {
		if !wh.shapeBeforeAuth {
			var ok bool
			if p, ok = body(); !ok {
				return
			}
		}
		if err := lateErr(p.Name); err != nil {
			apiresp.WriteError(w, r, err)
			return
		}
	}
	if !wh.shapeBeforeAuth && !wh.shapeAfterAuthz && !wh.lateLeaks {
		var ok bool
		if p, ok = body(); !ok {
			return
		}
	}
	id, _ := opctx.EffectiveActorEntityID(r.Context())
	if id != ownerActor {
		apiresp.WriteError(w, r, apiresp.ErrForbidden)
		return
	}
	if wh.shapeAfterAuthz {
		var ok bool
		if p, ok = body(); !ok {
			return
		}
	}
	if u == missingID {
		apiresp.WriteError(w, r, apiresp.ErrNotFound)
		return
	}
	if !wh.lateLeaks {
		if err := lateErr(p.Name); err != nil {
			apiresp.WriteError(w, r, err)
			return
		}
	}
	apiresp.WriteJSON(w, http.StatusOK, p)
}

func TestEvaluate_DetectsViolations(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		h    wrongHandler
		want string
	}{
		{"400-before-401", wrongHandler{shapeBeforeAuth: true}, "anonymous + bad body"},
		{"404-before-403", wrongHandler{notFoundBeforeAuthz: true}, "unauthorized + good body on missing path"},
		{"400-after-403", wrongHandler{shapeAfterAuthz: true}, "unauthorized + bad body"},
		{"late-422-leaks-to-unauthorized", wrongHandler{lateLeaks: true}, "must not leak to unauthorized caller"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if fs := evaluate(baseRoute(tc.h)); len(fs) == 0 {
				t.Fatal("expected failures; got none")
			} else if !anyContains(fs, tc.want) {
				t.Errorf("no failure mentions %q; got %v", tc.want, fs)
			}
		})
	}
}

func TestEvaluate_Sanity_WrongHandlerBaselinePasses(t *testing.T) {
	t.Parallel()
	// A wrongHandler with no flags is itself correctly ordered (404 for an
	// authorized miss, unmasked for the unauthorized caller it is 403).
	if fs := evaluate(baseRoute(wrongHandler{})); len(fs) != 0 {
		t.Fatalf("baseline should pass; got %v", fs)
	}
}

func anyContains(fs []string, sub string) bool {
	for _, f := range fs {
		if strings.Contains(f, sub) {
			return true
		}
	}
	return false
}
