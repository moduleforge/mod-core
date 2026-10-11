package httpapi

// apps_order_test.go drives every /apps route through apiresp/ordertest to
// pin the HTTP error-precedence order (401, 400, 403, 404, then 409) on the
// router as the module manifest composes it: RegisterAppRoutes on a shared
// /v1 router next to a peer route.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/moduleforge/core-api/apiresp"
	"github.com/moduleforge/core-api/apiresp/ordertest"
	"github.com/moduleforge/core-api/entity"
	"github.com/moduleforge/core-api/observer"
	"github.com/moduleforge/core-api/opctx"
	"github.com/moduleforge/core-api/types"
	coredb "github.com/moduleforge/core-model/db"
)

// orderAdminOnlyAuthz allows exactly the admin actor and denies everyone
// else, including the target-0 call AuthorizeResolve makes on a masked miss
// (the denial is moot there, but a real authorizer must not panic or grant).
type orderAdminOnlyAuthz struct{}

func (orderAdminOnlyAuthz) Authorize(ctx context.Context, _ string, target *int64) error {
	if target != nil && *target == 0 {
		return apiresp.ErrForbidden
	}
	if id, ok := opctx.EffectiveActorEntityID(ctx); ok && id == orderAdminID {
		return nil
	}
	return apiresp.ErrForbidden
}

// appsOrderQuerier adds the one piece of state-dependent rejection the
// ordering tests need: a slug uniqueness violation (pg 23505), which the
// handler must map to 409 apps.slug_taken. Only the reserved slug on create,
// and a slug owned by a different app on rename, collide, so repeated
// well-formed requests from the harness stay deterministic.
type appsOrderQuerier struct {
	*appsFakeQuerier
}

const orderTakenSlug = "taken"

func (q appsOrderQuerier) InsertApp(ctx context.Context, arg coredb.InsertAppParams) (coredb.InsertAppRow, error) {
	if arg.Slug == orderTakenSlug {
		return coredb.InsertAppRow{}, &pgconn.PgError{Code: pgerrcodeUniqueViolation, ConstraintName: appsSlugConstraint}
	}
	return q.appsFakeQuerier.InsertApp(ctx, arg)
}

func (q appsOrderQuerier) UpdateApp(ctx context.Context, arg coredb.UpdateAppParams) error {
	for id, a := range q.apps {
		if id != arg.ID && a.Slug == arg.Slug {
			return &pgconn.PgError{Code: pgerrcodeUniqueViolation, ConstraintName: appsSlugConstraint}
		}
	}
	return q.appsFakeQuerier.UpdateApp(ctx, arg)
}

// newAppsOrderRouter composes RegisterAppRoutes on a shared /v1 router with
// a public peer route, exactly as the manifest mounts them. It seeds the app
// the stranger may not touch and an app already holding the reserved slug.
func newAppsOrderRouter() (h http.Handler, existing uuid.UUID) {
	fq := newAppsFakeQuerier()
	existing = fq.seedApp("demo", "Demo App")
	fq.seedApp(orderTakenSlug, "Taken App")
	q := appsOrderQuerier{fq}

	typeResolver := types.NewFromMap(map[string]int64{"app": 99})
	ah := NewAppsHandler(newFakeAppsDB(), q, orderAdminOnlyAuthz{}, observer.NewObserverGroup(), entity.NewResolver(), typeResolver)
	ah.newQuerier = func(_ pgx.Tx) coredb.Querier { return q }

	r := chi.NewRouter()
	r.Route("/v1", func(r chi.Router) {
		RegisterAppRoutes(r, ah)
		r.Get("/peer", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	})
	return r, existing
}

func TestAppsErrorOrder(t *testing.T) {
	router, existing := newAppsOrderRouter()
	existingPath := "/v1/apps/" + existing.String()
	missingPath := "/v1/apps/" + uuid.NewString()

	// Every {uuid} route answers 403 for a missing app, for every caller
	// (admin included): the masked AuthorizeResolve, so AuthorizedMissingStatus
	// is 403. The 404 only survives in a vanishing-row race.
	ordertest.Run(t,
		ordertest.Route{
			Name:                    "GET /apps/{uuid}",
			Handler:                 router,
			Method:                  http.MethodGet,
			ExistingPath:            existingPath,
			MissingPath:             missingPath,
			Authorized:              ordertest.Actor(orderAdminID),
			Unauthorized:            ordertest.Actor(orderStrangerID),
			AuthorizedMissingStatus: http.StatusForbidden,
		},
		ordertest.Route{
			Name:                    "PUT /apps/{uuid}",
			Handler:                 router,
			Method:                  http.MethodPut,
			ExistingPath:            existingPath,
			MissingPath:             missingPath,
			GoodBody:                []byte(`{"name":"Renamed"}`),
			BadBody:                 []byte(`{"name":`),
			Authorized:              ordertest.Actor(orderAdminID),
			Unauthorized:            ordertest.Actor(orderStrangerID),
			AuthorizedMissingStatus: http.StatusForbidden,
			Late: []ordertest.Late{
				{Name: "duplicate-slug", Body: []byte(`{"slug":"` + orderTakenSlug + `"}`), WantStatus: http.StatusConflict},
			},
		},
		ordertest.Route{
			Name:                    "PUT /apps/{uuid} unknown field",
			Handler:                 router,
			Method:                  http.MethodPut,
			ExistingPath:            existingPath,
			MissingPath:             missingPath,
			GoodBody:                []byte(`{"name":"Renamed"}`),
			BadBody:                 []byte(`{"name":"x","bogus":1}`),
			Authorized:              ordertest.Actor(orderAdminID),
			Unauthorized:            ordertest.Actor(orderStrangerID),
			AuthorizedMissingStatus: http.StatusForbidden,
		},
		ordertest.Route{
			Name:                    "DELETE /apps/{uuid}",
			Handler:                 router,
			Method:                  http.MethodDelete,
			ExistingPath:            existingPath,
			MissingPath:             missingPath,
			Authorized:              ordertest.Actor(orderAdminID),
			Unauthorized:            ordertest.Actor(orderStrangerID),
			AuthorizedMissingStatus: http.StatusForbidden,
		},
	)
}

// POST /apps and GET /apps carry no entity id: the ordertest existing/missing
// pair does not apply, so the same path stands in for both. POST authorizes
// by type (authz.AuthorizeType) but AFTER the stateless decode and field
// checks (it is deliberately not an AuthzFirst route), and its 409 slug
// conflict is a Late entry. GET /apps authorizes against a nil target.
func TestAppsCollectionErrorOrder(t *testing.T) {
	router, _ := newAppsOrderRouter()

	ordertest.Run(t,
		ordertest.Route{
			Name:                    "POST /apps",
			Handler:                 router,
			Method:                  http.MethodPost,
			ExistingPath:            "/v1/apps",
			MissingPath:             "/v1/apps",
			GoodBody:                []byte(`{"slug":"fresh","name":"Fresh App"}`),
			BadBody:                 []byte(`{"slug":`),
			Authorized:              ordertest.Actor(orderAdminID),
			Unauthorized:            ordertest.Actor(orderStrangerID),
			AuthorizedMissingStatus: http.StatusCreated,
			Late: []ordertest.Late{
				{Name: "duplicate-slug", Body: []byte(`{"slug":"` + orderTakenSlug + `","name":"Dup"}`), WantStatus: http.StatusConflict},
			},
		},
		ordertest.Route{
			Name:                    "POST /apps missing slug",
			Handler:                 router,
			Method:                  http.MethodPost,
			ExistingPath:            "/v1/apps",
			MissingPath:             "/v1/apps",
			GoodBody:                []byte(`{"slug":"fresh","name":"Fresh App"}`),
			BadBody:                 []byte(`{"name":"No Slug"}`),
			Authorized:              ordertest.Actor(orderAdminID),
			Unauthorized:            ordertest.Actor(orderStrangerID),
			AuthorizedMissingStatus: http.StatusCreated,
		},
		ordertest.Route{
			Name:                    "GET /apps",
			Handler:                 router,
			Method:                  http.MethodGet,
			ExistingPath:            "/v1/apps",
			MissingPath:             "/v1/apps",
			Authorized:              ordertest.Actor(orderAdminID),
			Unauthorized:            ordertest.Actor(orderStrangerID),
			AuthorizedMissingStatus: http.StatusOK,
		},
	)
}

// The 401 gate is scoped to the /apps group: a peer route on the same /v1
// router stays reachable anonymously.
func TestAppsRouteGroupDoesNotGatePeers(t *testing.T) {
	router, _ := newAppsOrderRouter()

	peer := httptest.NewRecorder()
	router.ServeHTTP(peer, httptest.NewRequest(http.MethodGet, "/v1/peer", nil))
	if peer.Code != http.StatusOK {
		t.Errorf("anonymous peer route: got %d, want 200", peer.Code)
	}
	apps := httptest.NewRecorder()
	router.ServeHTTP(apps, httptest.NewRequest(http.MethodGet, "/v1/apps", nil))
	if apps.Code != http.StatusUnauthorized {
		t.Errorf("anonymous /apps: got %d, want 401", apps.Code)
	}
}
