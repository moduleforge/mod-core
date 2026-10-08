package httpapi

// apps_type_level_authz_test.go pins the type-level `create` authorization
// shape of AppsHandler.Create (POST /apps).
//
// Background (security): authz.Authorizer.Authorize's target is always an
// entities.id (or nil). types.id and entities.id are independent sequences
// that both start at 1, so passing the app types.id as the target makes an
// entity-semantic Authorizer match it against an unrelated entity. Type-level
// checks must go through authz.AuthorizeType. Each case below fails on a call
// site that still passes `&typeID` to Authorize.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/moduleforge/core-api/apiresp"
	"github.com/moduleforge/core-api/authz"
	"github.com/moduleforge/core-api/entity"
	"github.com/moduleforge/core-api/observer"
	"github.com/moduleforge/core-api/types"
	coredb "github.com/moduleforge/core-model/db"

	"github.com/jackc/pgx/v5"
)

// appTypeID is the app type id newTestAppsHandler resolves ("app" -> 99).
const appTypeID int64 = 99

type appsAuthzCall struct {
	op        string
	hasTarget bool
	target    int64
}

// appsRecAuthz is a plain (non type-aware) Authorizer stub that records every
// Authorize call. When entityTypeID is non-zero it behaves like an
// entity-semantic authorizer in which the actor owns, or holds a targeted
// grant over, the entity whose id equals entityTypeID: nil exactly when
// target != nil && *target == entityTypeID, ErrForbidden otherwise (including
// for a nil target). When denyAll is set every call is denied; otherwise every
// call is allowed.
type appsRecAuthz struct {
	entityTypeID int64
	denyAll      bool
	calls        []appsAuthzCall
}

func (a *appsRecAuthz) Authorize(_ context.Context, op string, target *int64) error {
	c := appsAuthzCall{op: op}
	if target != nil {
		c.hasTarget, c.target = true, *target
	}
	a.calls = append(a.calls, c)
	if a.denyAll {
		return apiresp.ErrForbidden
	}
	if a.entityTypeID != 0 {
		if target != nil && *target == a.entityTypeID {
			return nil
		}
		return apiresp.ErrForbidden
	}
	return nil
}

func (a *appsRecAuthz) nonNilTargetCalls() []appsAuthzCall {
	var out []appsAuthzCall
	for _, c := range a.calls {
		if c.hasTarget {
			out = append(out, c)
		}
	}
	return out
}

// appsTypeAwareAuthz adds an authz.TypeAuthorizer capability and records each
// AuthorizeType call (the type id is held in target).
type appsTypeAwareAuthz struct {
	appsRecAuthz
	typeResult error
	typeCalls  []appsAuthzCall
}

func (a *appsTypeAwareAuthz) AuthorizeType(_ context.Context, op string, typeID int64) error {
	a.typeCalls = append(a.typeCalls, appsAuthzCall{op: op, target: typeID})
	return a.typeResult
}

var _ authz.TypeAuthorizer = (*appsTypeAwareAuthz)(nil)

// postApp drives POST /apps through the real handler wired with az over q.
func postApp(t *testing.T, q *appsFakeQuerier, az authz.Authorizer) *httptest.ResponseRecorder {
	t.Helper()
	typeResolver := types.NewFromMap(map[string]int64{"app": appTypeID})
	h := NewAppsHandler(newFakeAppsDB(), q, az, observer.NewObserverGroup(), entity.NewResolver(), typeResolver)
	h.newQuerier = func(_ pgx.Tx) coredb.Querier { return q }

	body, _ := json.Marshal(createAppRequest{Slug: "demo", Name: "Demo App"})
	req := adminReq(http.MethodPost, "/apps", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	appsRouter(h).ServeHTTP(rec, req)
	return rec
}

func TestAppsCreateTypeLevel_EntitySemanticAuthorizerCannotBeBypassed(t *testing.T) {
	q := newAppsFakeQuerier()
	az := &appsRecAuthz{entityTypeID: appTypeID}

	rec := postApp(t, q, az)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d; want 403 (an actor owning/granted entity %d must not create apps via the type id) — body: %s",
			rec.Code, appTypeID, rec.Body.String())
	}
	if len(q.apps) != 0 || len(q.entities) != 0 {
		t.Errorf("a row was created (apps=%d, entities=%d); want none", len(q.apps), len(q.entities))
	}
	if got := az.nonNilTargetCalls(); len(got) != 0 {
		t.Errorf("Authorize saw non-nil target call(s) %+v; a type id must never be passed as an entity target", got)
	}
}

func TestAppsCreateTypeLevel_PlainAuthorizerSeesNilTarget(t *testing.T) {
	q := newAppsFakeQuerier()
	az := &appsRecAuthz{}

	rec := postApp(t, q, az)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d; want 201 — body: %s", rec.Code, rec.Body.String())
	}
	if len(az.calls) != 1 || az.calls[0].op != "create" || az.calls[0].hasTarget {
		t.Errorf("Authorize calls = %+v; want exactly [{create nil}]", az.calls)
	}
}

func TestAppsCreateTypeLevel_TypeAwareDenyIsFinal(t *testing.T) {
	q := newAppsFakeQuerier()
	az := &appsTypeAwareAuthz{
		appsRecAuthz: appsRecAuthz{entityTypeID: appTypeID},
		typeResult:   apiresp.ErrForbidden,
	}

	rec := postApp(t, q, az)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d; want 403 — body: %s", rec.Code, rec.Body.String())
	}
	if len(az.typeCalls) != 1 || az.typeCalls[0].op != "create" || az.typeCalls[0].target != appTypeID {
		t.Errorf("AuthorizeType calls = %+v; want exactly one (create, %d)", az.typeCalls, appTypeID)
	}
	if len(az.calls) != 0 {
		t.Errorf("Authorize was called %d time(s) (%+v); a type-aware deny must be final", len(az.calls), az.calls)
	}
	if len(q.apps) != 0 || len(q.entities) != 0 {
		t.Errorf("a row was created (apps=%d, entities=%d); want none", len(q.apps), len(q.entities))
	}
}

func TestAppsCreateTypeLevel_TypeAwareAllow(t *testing.T) {
	q := newAppsFakeQuerier()
	az := &appsTypeAwareAuthz{appsRecAuthz: appsRecAuthz{denyAll: true}}

	rec := postApp(t, q, az)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d; want 201 — body: %s", rec.Code, rec.Body.String())
	}
	if len(az.typeCalls) != 1 || az.typeCalls[0].op != "create" || az.typeCalls[0].target != appTypeID {
		t.Errorf("AuthorizeType calls = %+v; want exactly one (create, %d)", az.typeCalls, appTypeID)
	}
	if len(az.calls) != 0 {
		t.Errorf("Authorize was called %d time(s); want none", len(az.calls))
	}
	if len(q.apps) != 1 {
		t.Errorf("apps created = %d; want 1", len(q.apps))
	}
}

// A non-sentinel AuthorizeType error is passed to apiresp.WriteError unchanged
// (so it maps to a 500, never to a 403/201) and Authorize is not consulted.
func TestAppsCreateTypeLevel_TypeAwareErrorPassthrough(t *testing.T) {
	sentinel := errors.New("type authorizer backend unavailable")
	q := newAppsFakeQuerier()
	az := &appsTypeAwareAuthz{typeResult: sentinel}

	rec := postApp(t, q, az)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d; want 500 (WriteError mapping of a non-sentinel error) — body: %s", rec.Code, rec.Body.String())
	}
	if len(az.calls) != 0 {
		t.Errorf("Authorize was called %d time(s); want none", len(az.calls))
	}
	if len(q.apps) != 0 || len(q.entities) != 0 {
		t.Errorf("a row was created (apps=%d, entities=%d); want none", len(q.apps), len(q.entities))
	}
}
