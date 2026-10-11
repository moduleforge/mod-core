package httpapi

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/moduleforge/core-api/apiresp"
	"github.com/moduleforge/core-api/apiresp/ordertest"
	"github.com/moduleforge/core-api/entity"
	"github.com/moduleforge/core-api/internal/fieldcrypto"
	"github.com/moduleforge/core-api/observer"
	"github.com/moduleforge/core-api/opctx"
	"github.com/moduleforge/core-api/service"
	"github.com/moduleforge/core-api/types"
	coredb "github.com/moduleforge/core-model/db"
)

// authorizeCall is one recorded Authorizer.Authorize invocation.
type authorizeCall struct {
	actor     int64
	op        string
	hasTarget bool
	target    int64
}

// orderAuthorizer is a recording authz.Authorizer: the owner is allowed, the
// stranger is denied, and target 0 (the masked-miss probe AuthorizeResolve
// makes for an id that does not exist) is denied for everyone.
type orderAuthorizer struct {
	mu    sync.Mutex
	calls []authorizeCall
}

func (a *orderAuthorizer) Authorize(ctx context.Context, op string, target *int64) error {
	actor, _ := opctx.EffectiveActorEntityID(ctx)
	c := authorizeCall{actor: actor, op: op}
	if target != nil {
		c.hasTarget, c.target = true, *target
	}
	a.mu.Lock()
	a.calls = append(a.calls, c)
	a.mu.Unlock()
	if c.hasTarget && c.target == 0 {
		return apiresp.ErrForbidden
	}
	if actor == orderOwnerID {
		return nil
	}
	return apiresp.ErrForbidden
}

func (a *orderAuthorizer) reset() {
	a.mu.Lock()
	a.calls = nil
	a.mu.Unlock()
}

func (a *orderAuthorizer) snapshot() []authorizeCall {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]authorizeCall(nil), a.calls...)
}

// refusingDB is a txhelper.DB whose BeginTx always fails, so a request that
// clears every gate stops at the persistence boundary (500) instead of
// needing a database. beginCalls proves the gates were cleared.
type refusingDB struct {
	mu         sync.Mutex
	beginCalls int
}

func (d *refusingDB) BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error) {
	d.mu.Lock()
	d.beginCalls++
	d.mu.Unlock()
	return nil, errors.New("order test: persistence refused")
}

func (d *refusingDB) begins() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.beginCalls
}

// entityOrderFixture is the real NewRouter (with RequireAuth) over the real
// services, a stub querier seeded with one entity of each subtype, and the
// recording authorizer.
type entityOrderFixture struct {
	router http.Handler
	az     *orderAuthorizer
	db     *refusingDB

	npUUID, corpUUID, saUUID uuid.UUID
}

func newEntityOrderFixture() *entityOrderFixture {
	f := &entityOrderFixture{
		az:       &orderAuthorizer{},
		db:       &refusingDB{},
		npUUID:   uuid.New(),
		corpUUID: uuid.New(),
		saUUID:   uuid.New(),
	}
	q := &stubQuerier{
		entitiesByUUID: map[uuid.UUID]coredb.GetEntityByUUIDRow{
			f.npUUID:   {ID: 11, Uuid: f.npUUID, FundamentalTypeSlug: "natural_person"},
			f.corpUUID: {ID: 12, Uuid: f.corpUUID, FundamentalTypeSlug: "corporation"},
			f.saUUID:   {ID: 13, Uuid: f.saUUID, FundamentalTypeSlug: "service_account"},
		},
		entitiesByID: map[int64]coredb.GetEntityByIDRow{
			11: {ID: 11, Uuid: f.npUUID, FundamentalTypeSlug: "natural_person"},
			12: {ID: 12, Uuid: f.corpUUID, FundamentalTypeSlug: "corporation"},
			13: {ID: 13, Uuid: f.saUUID, FundamentalTypeSlug: "service_account"},
		},
		naturalPersonsByEntityID: map[int64]coredb.GetNaturalPersonByEntityIDRow{
			11: {ID: 11, EntityID: 11, GivenName: pgtype.Text{String: "Ada", Valid: true}, FamilyName: pgtype.Text{String: "Lovelace", Valid: true}},
		},
		corporationsByEntityID: map[int64]coredb.GetCorporationByEntityIDRow{
			12: {ID: 12, EntityID: 12, LegalName: "Acme Corp"},
		},
		serviceAccountsByEntityID: map[int64]coredb.ServiceAccount{
			13: {ID: 13, EntityID: 13, Label: "ci-bot"},
		},
	}
	// The natural-person profile read decrypts the (empty) SSN blob, so the
	// service needs a real cipher.
	cipher, err := fieldcrypto.NewFromKey(bytes.Repeat([]byte{7}, 32))
	if err != nil {
		panic(err)
	}
	typeResolver := types.NewFromMap(map[string]int64{"natural_person": 101, "corporation": 102, "service_account": 103})
	svcs := service.New(q, f.db, f.az, observer.NewObserverGroup(), cipher, entity.NewResolver(), typeResolver)
	dsp := service.NewDisplayService(service.NewDisplayRegistry(q), f.az, entity.NewResolver())
	f.router = NewRouter(NewDepsWithDisplay(svcs, noopLogger(), dsp))
	return f
}

// TestEntityRoutesErrorOrder pins the 401 -> 400 -> 403 -> 404 order for every
// entity route with apiresp/ordertest.
//
// Every route that reads or mutates an existing entity masks a miss as 403
// (default Resolve for the GETs, AuthorizeResolve for PUT/DELETE), so each
// sets AuthorizedMissingStatus: 403.
//
// The three POST create routes are type-level (AuthorizeType in the service)
// but their handlers DecodeJSON first, so they do NOT set AuthzFirst:
// malformed JSON from an unauthorized caller is 400, not 403. A create route
// has no path id, so ExistingPath and MissingPath are the collection path,
// and the "authorized missing" assertion reduces to "an authorized good
// request clears every gate": it reaches the persistence layer, where
// refusingDB makes it a deterministic 500 (hence AuthorizedMissingStatus
// 500). Blank-name bodies on create are stateless shape errors checked by the
// handler before the service's AuthorizeType; see
// TestEntityCreateRoutes_BlankNameIsStatelessBadRequest.
//
// No Late entries: the state-conflict audit found no 409/422 on these routes
// (a blank label/name on the PUT routes is a stateless shape 400 checked
// before authorization, not a state conflict; see
// TestEntityUpdateRoutes_BlankNameIsStatelessBadRequest).
func TestEntityRoutesErrorOrder(t *testing.T) {
	f := newEntityOrderFixture()
	missing := uuid.New().String()
	owner := ordertest.Actor(orderOwnerID)
	stranger := ordertest.Actor(orderStrangerID)

	get := func(name, collection string, existing uuid.UUID) ordertest.Route {
		return ordertest.Route{
			Name:                    name,
			Handler:                 f.router,
			Method:                  http.MethodGet,
			ExistingPath:            collection + "/" + existing.String(),
			MissingPath:             collection + "/" + missing,
			Authorized:              owner,
			Unauthorized:            stranger,
			AuthorizedMissingStatus: http.StatusForbidden,
		}
	}
	put := func(name, collection string, existing uuid.UUID, good, bad string) ordertest.Route {
		return ordertest.Route{
			Name:                    name,
			Handler:                 f.router,
			Method:                  http.MethodPut,
			ExistingPath:            collection + "/" + existing.String(),
			MissingPath:             collection + "/" + missing,
			GoodBody:                []byte(good),
			BadBody:                 []byte(bad),
			Authorized:              owner,
			Unauthorized:            stranger,
			AuthorizedMissingStatus: http.StatusForbidden,
		}
	}
	post := func(name, path, good, bad string) ordertest.Route {
		return ordertest.Route{
			Name:                    name,
			Handler:                 f.router,
			Method:                  http.MethodPost,
			ExistingPath:            path,
			MissingPath:             path,
			GoodBody:                []byte(good),
			BadBody:                 []byte(bad),
			Authorized:              owner,
			Unauthorized:            stranger,
			AuthorizedMissingStatus: http.StatusInternalServerError,
		}
	}

	ordertest.Run(t,
		get("GET /entities/natural-persons/{uuid}", "/entities/natural-persons", f.npUUID),
		put("PUT /entities/natural-persons/{uuid}", "/entities/natural-persons", f.npUUID, `{"given_name":"Ada"}`, `{"given_name":`),
		post("POST /entities/natural-persons", "/entities/natural-persons", `{"given_name":"Ada","family_name":"Lovelace"}`, `{"given_name":`),

		get("GET /entities/corporations/{uuid}", "/entities/corporations", f.corpUUID),
		put("PUT /entities/corporations/{uuid}", "/entities/corporations", f.corpUUID, `{"legal_name":"Acme"}`, `{"legal_name":`),
		post("POST /entities/corporations", "/entities/corporations", `{"legal_name":"Acme"}`, `{"legal_name":`),

		get("GET /entities/service-accounts/{uuid}", "/entities/service-accounts", f.saUUID),
		put("PUT /entities/service-accounts/{uuid}", "/entities/service-accounts", f.saUUID, `{"label":"bot"}`, `{"label":`),
		post("POST /entities/service-accounts", "/entities/service-accounts", `{"label":"bot"}`, `{"label":`),

		get("GET /entities/{uuid}", "/entities", f.npUUID),
		ordertest.Route{
			Name:                    "DELETE /entities/{uuid}",
			Handler:                 f.router,
			Method:                  http.MethodDelete,
			ExistingPath:            "/entities/" + f.npUUID.String(),
			MissingPath:             "/entities/" + missing,
			Authorized:              owner,
			Unauthorized:            stranger,
			AuthorizedMissingStatus: http.StatusForbidden,
		},
		ordertest.Route{
			Name:                    "GET /entities/{uuid}/display-name",
			Handler:                 f.router,
			Method:                  http.MethodGet,
			ExistingPath:            "/entities/" + f.npUUID.String() + "/display-name",
			MissingPath:             "/entities/" + missing + "/display-name",
			Authorized:              owner,
			Unauthorized:            stranger,
			AuthorizedMissingStatus: http.StatusForbidden,
		},
	)
}

// TestEntityRoutes_UnknownFieldIsStatelessBadRequest covers the second
// stateless rule (unknown fields) for each body-bearing route: an
// unauthorized caller gets 400 on both existing and missing ids, before any
// authorize.
func TestEntityRoutes_UnknownFieldIsStatelessBadRequest(t *testing.T) {
	f := newEntityOrderFixture()
	missing := uuid.New().String()
	cases := []struct{ method, path, body string }{
		{http.MethodPut, "/entities/natural-persons/" + f.npUUID.String(), `{"bogus":1}`},
		{http.MethodPut, "/entities/natural-persons/" + missing, `{"bogus":1}`},
		{http.MethodPut, "/entities/corporations/" + f.corpUUID.String(), `{"bogus":1}`},
		{http.MethodPut, "/entities/service-accounts/" + f.saUUID.String(), `{"bogus":1}`},
		{http.MethodPost, "/entities/natural-persons", `{"bogus":1}`},
		{http.MethodPost, "/entities/corporations", `{"bogus":1}`},
		{http.MethodPost, "/entities/service-accounts", `{"bogus":1}`},
	}
	for _, c := range cases {
		t.Run(c.method+" "+c.path, func(t *testing.T) {
			f.az.reset()
			req := httptest.NewRequest(c.method, c.path, strings.NewReader(c.body))
			req.Header.Set("Content-Type", "application/json")
			req = withActor(req, orderStrangerID)
			rec := httptest.NewRecorder()
			f.router.ServeHTTP(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status: got %d, want 400 — %s", rec.Code, rec.Body.String())
			}
			if n := len(f.az.snapshot()); n != 0 {
				t.Errorf("Authorize calls before shape rejection: got %d, want 0", n)
			}
		})
	}
}

// TestEntityCreateRoutes_BlankNameIsStatelessBadRequest pins that a blank (or
// whitespace-only) name on the three create routes is a 400 for an
// unauthorized caller, with no Authorize call, while an unauthorized caller
// with a valid body still gets 403 and an anonymous caller still gets 401.
func TestEntityCreateRoutes_BlankNameIsStatelessBadRequest(t *testing.T) {
	f := newEntityOrderFixture()
	cases := []struct{ name, path, blank, valid, code string }{
		{"natural person given_name", "/entities/natural-persons", `{"given_name":"  ","family_name":"Lovelace"}`, `{"given_name":"Ada","family_name":"Lovelace"}`, "natural_persons.given_name_required"},
		{"natural person family_name", "/entities/natural-persons", `{"given_name":"Ada","family_name":""}`, `{"given_name":"Ada","family_name":"Lovelace"}`, "natural_persons.family_name_required"},
		{"corporation legal_name", "/entities/corporations", `{"legal_name":" "}`, `{"legal_name":"Acme"}`, "corporations.legal_name_required"},
		{"service account label", "/entities/service-accounts", `{"label":""}`, `{"label":"bot"}`, "service_accounts.label_required"},
	}
	do := func(path, body string, actor int64) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if actor != 0 {
			req = withActor(req, actor)
		}
		rec := httptest.NewRecorder()
		f.router.ServeHTTP(rec, req)
		return rec
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f.az.reset()
			rec := do(c.path, c.blank, orderStrangerID)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("unauthorized+blank: got %d, want 400 - %s", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), c.code) {
				t.Errorf("body missing code %q: %s", c.code, rec.Body.String())
			}
			if n := len(f.az.snapshot()); n != 0 {
				t.Errorf("Authorize calls before blank-name rejection: got %d, want 0", n)
			}
			if rec := do(c.path, c.valid, orderStrangerID); rec.Code != http.StatusForbidden {
				t.Errorf("unauthorized+valid: got %d, want 403 - %s", rec.Code, rec.Body.String())
			}
			if rec := do(c.path, c.blank, 0); rec.Code != http.StatusUnauthorized {
				t.Errorf("anonymous+blank: got %d, want 401 - %s", rec.Code, rec.Body.String())
			}
			if rec := do(c.path, c.blank, orderOwnerID); rec.Code != http.StatusBadRequest {
				t.Errorf("authorized+blank: got %d, want 400 - %s", rec.Code, rec.Body.String())
			}
		})
	}
}

// TestEntityUpdateRoutes_BlankNameIsStatelessBadRequest pins the same contract
// on the three PUT routes: a present-but-blank (or whitespace-only) name/label
// is a 400 for an unauthorized caller with no Authorize call, an unauthorized
// caller with a valid body still gets 403, an anonymous caller 401, and a
// valid untrimmed value from the authorized caller clears the gates. An
// absent or null field stays a no-op (never the blank-name 400).
func TestEntityUpdateRoutes_BlankNameIsStatelessBadRequest(t *testing.T) {
	f := newEntityOrderFixture()
	cases := []struct{ name, path, blank, valid, code string }{
		{"natural person given_name", "/entities/natural-persons/" + f.npUUID.String(), `{"given_name":"  "}`, `{"given_name":"Ada"}`, "natural_persons.given_name_required"},
		{"natural person family_name", "/entities/natural-persons/" + f.npUUID.String(), `{"family_name":""}`, `{"family_name":"Lovelace"}`, "natural_persons.family_name_required"},
		{"corporation legal_name", "/entities/corporations/" + f.corpUUID.String(), `{"legal_name":" "}`, `{"legal_name":"Acme"}`, "corporations.legal_name_required"},
		{"service account label", "/entities/service-accounts/" + f.saUUID.String(), `{"label":"\t"}`, `{"label":"bot"}`, "service_accounts.label_required"},
	}
	do := func(path, body string, actor int64) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPut, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if actor != 0 {
			req = withActor(req, actor)
		}
		rec := httptest.NewRecorder()
		f.router.ServeHTTP(rec, req)
		return rec
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f.az.reset()
			rec := do(c.path, c.blank, orderStrangerID)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("unauthorized+blank: got %d, want 400 - %s", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), c.code) {
				t.Errorf("body missing code %q: %s", c.code, rec.Body.String())
			}
			if n := len(f.az.snapshot()); n != 0 {
				t.Errorf("Authorize calls before blank rejection: got %d, want 0", n)
			}
			if rec := do(c.path, c.valid, orderStrangerID); rec.Code != http.StatusForbidden {
				t.Errorf("unauthorized+valid: got %d, want 403 - %s", rec.Code, rec.Body.String())
			}
			if rec := do(c.path, c.blank, 0); rec.Code != http.StatusUnauthorized {
				t.Errorf("anonymous+blank: got %d, want 401 - %s", rec.Code, rec.Body.String())
			}
			if rec := do(c.path, c.blank, orderOwnerID); rec.Code != http.StatusBadRequest {
				t.Errorf("authorized+blank: got %d, want 400 - %s", rec.Code, rec.Body.String())
			}
			// Absent/null is a no-op, not the blank-name 400: the authorized
			// caller clears the stateless gate (refusingDB then stops the
			// service-account/natural-person mutation at the persistence
			// boundary or, for a no-op body, the update succeeds).
			if rec := do(c.path, `{}`, orderOwnerID); rec.Code == http.StatusBadRequest {
				t.Errorf("authorized+absent field: got 400, want a no-op - %s", rec.Body.String())
			}
		})
	}
}

// TestEntityRoutes_MaskedMissProbesAuthorizerOnceWithTargetZero asserts the
// AuthorizeResolve contract on the mutating routes: a miss makes exactly one
// Authorize call with target 0 (discarded) and returns 403 without ever
// opening a transaction; a hit makes exactly one call with the real id. The
// GET routes use the default Resolve, which masks a miss without consulting
// the authorizer at all.
func TestEntityRoutes_MaskedMissProbesAuthorizerOnceWithTargetZero(t *testing.T) {
	f := newEntityOrderFixture()
	missing := uuid.New().String()

	mutating := []struct {
		name, method, collection, op, body string
		existing                           uuid.UUID
		existingID                         int64
	}{
		{"PUT natural-persons", http.MethodPut, "/entities/natural-persons", "update", `{"given_name":"Ada"}`, f.npUUID, 11},
		{"PUT corporations", http.MethodPut, "/entities/corporations", "update", `{"legal_name":"Acme"}`, f.corpUUID, 12},
		{"PUT service-accounts", http.MethodPut, "/entities/service-accounts", "update", `{"label":"bot"}`, f.saUUID, 13},
		{"DELETE entity", http.MethodDelete, "/entities", "delete", ``, f.npUUID, 11},
	}
	do := func(method, path, body string, actor int64) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req = withActor(req, actor)
		rec := httptest.NewRecorder()
		f.router.ServeHTTP(rec, req)
		return rec
	}

	for _, c := range mutating {
		t.Run(c.name+"/miss", func(t *testing.T) {
			for _, actor := range []int64{orderOwnerID, orderStrangerID} {
				f.az.reset()
				beginsBefore := f.db.begins()
				rec := do(c.method, c.collection+"/"+missing, c.body, actor)
				if rec.Code != http.StatusForbidden {
					t.Fatalf("actor %d: status: got %d, want 403 — %s", actor, rec.Code, rec.Body.String())
				}
				calls := f.az.snapshot()
				if len(calls) != 1 || !calls[0].hasTarget || calls[0].target != 0 || calls[0].op != c.op {
					t.Fatalf("actor %d: Authorize calls: got %+v, want exactly one %q call with target 0", actor, calls, c.op)
				}
				if f.db.begins() != beginsBefore {
					t.Errorf("actor %d: a transaction was opened for a masked miss", actor)
				}
			}
		})
		t.Run(c.name+"/hit", func(t *testing.T) {
			f.az.reset()
			rec := do(c.method, c.collection+"/"+c.existing.String(), c.body, orderStrangerID)
			if rec.Code != http.StatusForbidden {
				t.Fatalf("status: got %d, want 403 — %s", rec.Code, rec.Body.String())
			}
			calls := f.az.snapshot()
			if len(calls) != 1 || !calls[0].hasTarget || calls[0].target != c.existingID || calls[0].op != c.op {
				t.Fatalf("Authorize calls: got %+v, want exactly one %q call with target %d", calls, c.op, c.existingID)
			}
		})
	}

	t.Run("GET natural-persons/miss does not consult the authorizer", func(t *testing.T) {
		f.az.reset()
		rec := do(http.MethodGet, "/entities/natural-persons/"+missing, "", orderOwnerID)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status: got %d, want 403", rec.Code)
		}
		if n := len(f.az.snapshot()); n != 0 {
			t.Errorf("Authorize calls: got %d, want 0", n)
		}
	})
}
