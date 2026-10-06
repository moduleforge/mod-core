//go:build integration

package httpapi_test

// apps_type_level_authz_integration_test.go reproduces, against a real
// Postgres database, the type-id-as-entity-target authorization bypass in
// AppsHandler.Create (POST /v1/apps), and pins the fix. See
// api/service/type_level_authz_integration_test.go for the full background:
// the handler passed the app types.id as the Authorize target, the production
// Authorizer reads every non-nil target as an entities.id, and the two id
// sequences collide, so an actor owning (or holding a targeted grant over) the
// entity whose id equals the app type id could create apps.
//
// The authorizers are the entity-semantic and type-aware test authorizers in
// api/internal/authztest, which MODEL the production authorizer; they are not
// production code. TestMain (field_crypto_keys_integration_test.go) calls
// authztest.SetupSchema and seedTypeLevelFixture after the DB reset.
//
// Run recipe (throwaway Postgres container) is in the header of
// field_crypto_keys_integration_test.go.

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/go-chi/chi/v5"
	"github.com/moduleforge/core-api/authz"
	"github.com/moduleforge/core-api/entity"
	"github.com/moduleforge/core-api/httpapi"
	"github.com/moduleforge/core-api/internal/authztest"
	"github.com/moduleforge/core-api/observer"
	"github.com/moduleforge/core-api/opctx"
	"github.com/moduleforge/core-api/types"
)

// typeLevelSlugs are the types whose ids this package's fixture collides with
// entity ids.
var typeLevelSlugs = []string{"app"}

// typeLevelFx is populated by seedTypeLevelFixture from TestMain.
var typeLevelFx authztest.Fixture

// seedTypeLevelFixture builds the deterministic ownership arm after the DB
// reset and the stand-in schema, before m.Run. It returns an error (TestMain
// exits 1) rather than skipping if any required entity id is already taken.
func seedTypeLevelFixture() error {
	fx, err := authztest.SeedOwnerFixture(context.Background(), pool, typeLevelSlugs...)
	if err != nil {
		return err
	}
	typeLevelFx = fx
	return nil
}

// typeLevelActors seeds the non-wildcard actors keyed by arm: ownership (the
// shared fixture owner), a direct targeted grant over the app-type-id entity,
// and a group path (actor-group membership plus a grant on a target group
// containing that entity).
func typeLevelActors(t *testing.T) map[string]int64 {
	t.Helper()
	ctx := context.Background()

	ugrant, err := authztest.SeedNaturalPerson(ctx, pool, "Direct", "Grantee")
	if err != nil {
		t.Fatalf("seed Ugrant: %v", err)
	}
	ugroup, err := authztest.SeedNaturalPerson(ctx, pool, "Group", "Grantee")
	if err != nil {
		t.Fatalf("seed Ugroup: %v", err)
	}
	actorGroup, err := authztest.SeedEntity(ctx, pool, "authz_actor_group")
	if err != nil {
		t.Fatalf("seed actor group: %v", err)
	}
	targetGroup, err := authztest.SeedEntity(ctx, pool, "authz_target_group")
	if err != nil {
		t.Fatalf("seed target group: %v", err)
	}
	if err := authztest.AddActorGroupMember(ctx, pool, actorGroup, ugroup); err != nil {
		t.Fatal(err)
	}
	if err := authztest.Grant(ctx, pool, actorGroup, authztest.OpCreate, &targetGroup); err != nil {
		t.Fatal(err)
	}
	for _, slug := range typeLevelSlugs {
		te := typeLevelFx.TypeEntityIDs[slug]
		if err := authztest.Grant(ctx, pool, ugrant, authztest.OpCreate, &te); err != nil {
			t.Fatal(err)
		}
		if err := authztest.AddTargetGroupMember(ctx, pool, targetGroup, te); err != nil {
			t.Fatal(err)
		}
	}
	return map[string]int64{
		"ownership":      typeLevelFx.OwnerID,
		"targeted-grant": ugrant,
		"group-path":     ugroup,
	}
}

func wildcardHolder(t *testing.T, opID int32) int64 {
	t.Helper()
	ctx := context.Background()
	id, err := authztest.SeedNaturalPerson(ctx, pool, "Wildcard", "Holder")
	if err != nil {
		t.Fatalf("seed wildcard holder: %v", err)
	}
	if err := authztest.Grant(ctx, pool, id, opID, nil); err != nil {
		t.Fatal(err)
	}
	return id
}

// postAppAs drives POST /apps through the real AppsHandler wired with az over
// the real database, as actor, creating an app whose slug is marker.
func postAppAs(t *testing.T, az authz.Authorizer, actor int64, marker string) *httptest.ResponseRecorder {
	t.Helper()
	typeResolver, err := types.New(context.Background(), coreQ)
	if err != nil {
		t.Fatalf("types.New: %v", err)
	}
	h := httpapi.NewAppsHandler(pool, coreQ, az, observer.NewObserverGroup(), entity.NewResolver(), typeResolver)
	r := chi.NewRouter()
	httpapi.RegisterAppRoutes(r, h)

	body, _ := json.Marshal(map[string]string{"slug": marker, "name": "Type Level " + marker})
	req := httptest.NewRequest(http.MethodPost, "/apps", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(opctx.WithActor(req.Context(), actor))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func appRows(t *testing.T, slug string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM apps WHERE slug = $1`, slug).Scan(&n); err != nil {
		t.Fatalf("count apps: %v", err)
	}
	return n
}

func uniqueSlug(prefix string) string {
	return prefix + "-" + strings.ToLower(uuid.NewString()[:13])
}

func assertAppDenied(t *testing.T, az authz.Authorizer, actor int64) {
	t.Helper()
	slug := uniqueSlug("denied")
	rec := postAppAs(t, az, actor, slug)
	if rec.Code != http.StatusForbidden {
		t.Errorf("POST /apps as actor %d: status = %d; want 403 — body: %s", actor, rec.Code, rec.Body.String())
	}
	if n := appRows(t, slug); n != 0 {
		t.Errorf("%d app row(s) created for denied actor %d; want none", n, actor)
	}
}

func assertAppAllowed(t *testing.T, az authz.Authorizer, actor int64) {
	t.Helper()
	slug := uniqueSlug("allowed")
	rec := postAppAs(t, az, actor, slug)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /apps as wildcard holder %d: status = %d; want 201 — body: %s", actor, rec.Code, rec.Body.String())
	}
	if n := appRows(t, slug); n != 1 {
		t.Errorf("app rows created = %d; want 1", n)
	}
}

// Characterization: the entity-semantic authorizer ADMITS the app type id as
// an entity target, for the ownership arm and both targeted-grant arms. That
// is its contract (target is an entities.id) and is exactly why call sites
// must never pass a type id. If this fails, the stand-in no longer models the
// production authorizer and the red/green evidence is void.
func TestAppsTypeLevelAuthz_EntitySemanticAuthorizerAdmitsTypeIDAsEntityTarget(t *testing.T) {
	actors := typeLevelActors(t)
	az := &authztest.EntitySemantic{Pool: pool}
	typeID := typeLevelFx.TypeIDs["app"]
	for arm, actor := range actors {
		ctx := opctx.WithActor(context.Background(), actor)
		if err := az.Authorize(ctx, "create", &typeID); err != nil {
			t.Errorf("%s: Authorize(create, &%d) = %v; want nil (entity-semantic authorizer matches the type id against entity %d)",
				arm, typeID, err, typeID)
		}
	}
}

// Red before the fix, green after: with the plain entity-semantic authorizer
// every non-wildcard arm gets 403 and no app is created.
func TestAppsTypeLevelAuthz_EntitySemanticWiring_NonWildcardActorsDenied(t *testing.T) {
	actors := typeLevelActors(t)
	az := &authztest.EntitySemantic{Pool: pool}
	for arm, actor := range actors {
		t.Run(arm, func(t *testing.T) { assertAppDenied(t, az, actor) })
	}
}

// With the type-aware authorizer the non-wildcard arms are denied through
// AuthorizeType (proved by the invocation counter).
func TestAppsTypeLevelAuthz_TypeAwareWiring_NonWildcardActorsDenied(t *testing.T) {
	actors := typeLevelActors(t)
	az := authztest.NewTypeAware(pool)
	for arm, actor := range actors {
		t.Run(arm, func(t *testing.T) {
			before := az.TypeCalls()
			assertAppDenied(t, az, actor)
			if got := az.TypeCalls() - before; got != 1 {
				t.Errorf("AuthorizeType invoked %d time(s); want 1", got)
			}
		})
	}
}

// A wildcard `create` holder and a wildcard `manage`-style holder can create
// apps under both wirings.
func TestAppsTypeLevelAuthz_WildcardHoldersAllowed(t *testing.T) {
	holders := map[string]int64{
		"wildcard-create": wildcardHolder(t, authztest.OpCreate),
		"wildcard-manage": wildcardHolder(t, authztest.OpManage),
	}
	for hname, actor := range holders {
		t.Run("entity-semantic/"+hname, func(t *testing.T) {
			assertAppAllowed(t, &authztest.EntitySemantic{Pool: pool}, actor)
		})
		t.Run("type-aware/"+hname, func(t *testing.T) {
			az := authztest.NewTypeAware(pool)
			assertAppAllowed(t, az, actor)
			if got := az.TypeCalls(); got != 1 {
				t.Errorf("AuthorizeType invoked %d time(s); want 1", got)
			}
		})
	}
}
