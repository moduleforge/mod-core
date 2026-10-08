package service

// type_level_authz_test.go pins the type-level `create` authorization shape of
// NaturalPersonService.Create, CorporationService.Create and
// ServiceAccountService.Create.
//
// Background (security): authz.Authorizer.Authorize's target is always an
// entities.id (or nil). types.id and entities.id are independent sequences
// that both start at 1, so passing a types.id as the target makes an
// entity-semantic Authorizer match it against an unrelated entity. Type-level
// checks must go through authz.AuthorizeType. Each case below fails on a call
// site that still passes `&typeID` to Authorize.

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/moduleforge/core-api/apiresp"
	"github.com/moduleforge/core-api/authz"
	"github.com/moduleforge/core-api/observer"
)

// authzCall is one recorded Authorize call. target is a copy of the pointed-to
// value (hasTarget false means the target was nil).
type authzCall struct {
	op        string
	hasTarget bool
	target    int64
}

// recAuthz is a plain (non type-aware) Authorizer stub that records every
// Authorize call. When entityTypeID is non-zero it behaves like an
// entity-semantic authorizer in which the actor owns, or holds a targeted
// grant over, the entity whose id equals entityTypeID: it returns nil exactly
// when target != nil && *target == entityTypeID and ErrForbidden otherwise
// (including for a nil target, i.e. the actor holds no wildcard grant). When
// entityTypeID is zero it allows everything except where deny is set.
type recAuthz struct {
	entityTypeID int64
	denyAll      bool
	calls        []authzCall
}

func (a *recAuthz) Authorize(_ context.Context, op string, target *int64) error {
	c := authzCall{op: op}
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

// nonNilTargetCalls returns the recorded Authorize calls whose target was
// non-nil.
func (a *recAuthz) nonNilTargetCalls() []authzCall {
	var out []authzCall
	for _, c := range a.calls {
		if c.hasTarget {
			out = append(out, c)
		}
	}
	return out
}

// typeAwareAuthz adds an authz.TypeAuthorizer capability to recAuthz and
// records every AuthorizeType call.
type typeAwareAuthz struct {
	recAuthz
	typeResult error
	typeCalls  []authzCall // hasTarget is unused; target holds the typeID
}

func (a *typeAwareAuthz) AuthorizeType(_ context.Context, op string, typeID int64) error {
	a.typeCalls = append(a.typeCalls, authzCall{op: op, target: typeID})
	return a.typeResult
}

var _ authz.TypeAuthorizer = (*typeAwareAuthz)(nil)

// typeLevelPath describes one type-level create path under test.
type typeLevelPath struct {
	name string
	slug string
	// create wires a fresh service of the path's kind with az over q and runs
	// Create with valid input.
	create func(t *testing.T, q *mockQuerier, az authz.Authorizer) error
}

func typeLevelPaths() []typeLevelPath {
	return []typeLevelPath{
		{
			name: "natural_person", slug: "natural_person",
			create: func(t *testing.T, q *mockQuerier, az authz.Authorizer) error {
				svc := &NaturalPersonService{
					db: newFakeDB(), az: az, obs: observer.NewObserverGroup(),
					cipher: testRotatingCipher(t), newQuerier: mockQuerierFactory(q),
					entityResolver: testEntityResolver(), typeResolver: testTypeResolver(q),
				}
				_, _, err := svc.Create(context.Background(), q, CreateNaturalPersonInput{GivenName: "Ada", FamilyName: "Lovelace"})
				return err
			},
		},
		{
			name: "corporation", slug: "corporation",
			create: func(t *testing.T, q *mockQuerier, az authz.Authorizer) error {
				svc := &CorporationService{
					db: newFakeDB(), az: az, obs: observer.NewObserverGroup(),
					cipher: testRotatingCipher(t), newQuerier: mockQuerierFactory(q),
					entityResolver: testEntityResolver(), typeResolver: testTypeResolver(q),
				}
				_, _, err := svc.Create(context.Background(), q, CreateCorporationInput{LegalName: "Acme Corp"})
				return err
			},
		},
		{
			name: "service_account", slug: "service_account",
			create: func(t *testing.T, q *mockQuerier, az authz.Authorizer) error {
				svc := &ServiceAccountService{
					db: newFakeDB(), az: az, obs: observer.NewObserverGroup(),
					newQuerier:     mockQuerierFactory(q),
					entityResolver: testEntityResolver(), typeResolver: testTypeResolver(q),
				}
				_, _, err := svc.Create(context.Background(), q, CreateServiceAccountInput{Label: "svc-1"})
				return err
			},
		},
	}
}

// typeIDFor reads the path's type id from the resolver, as the production
// call site does.
func typeIDFor(q *mockQuerier, slug string) int64 {
	return testTypeResolver(q).IDForSlugMust(slug)
}

// (a) An entity-semantic authorizer that would accept the type id as an entity
// target must not be consulted with one: the bypass is closed.
func TestTypeLevelCreate_EntitySemanticAuthorizerCannotBeBypassed(t *testing.T) {
	for _, p := range typeLevelPaths() {
		t.Run(p.name, func(t *testing.T) {
			q := newMockQuerier()
			typeID := typeIDFor(q, p.slug)
			az := &recAuthz{entityTypeID: typeID}

			err := p.create(t, q, az)

			if !errors.Is(err, apiresp.ErrForbidden) {
				t.Errorf("Create err = %v; want ErrForbidden (an actor owning/granted entity %d must not create via the type id)", err, typeID)
			}
			if q.nextID != 0 || len(q.entitiesByID) != 0 {
				t.Errorf("a row was created (nextID=%d, entities=%d); want none", q.nextID, len(q.entitiesByID))
			}
			if got := az.nonNilTargetCalls(); len(got) != 0 {
				t.Errorf("Authorize saw non-nil target call(s) %+v; a type id must never be passed as an entity target", got)
			}
		})
	}
}

// (b) A plain recording authorizer sees exactly one call, ("create", nil).
func TestTypeLevelCreate_PlainAuthorizerSeesNilTarget(t *testing.T) {
	for _, p := range typeLevelPaths() {
		t.Run(p.name, func(t *testing.T) {
			q := newMockQuerier()
			az := &recAuthz{}

			if err := p.create(t, q, az); err != nil {
				t.Fatalf("Create: unexpected error: %v", err)
			}
			if len(az.calls) != 1 || az.calls[0].op != "create" || az.calls[0].hasTarget {
				t.Errorf("Authorize calls = %+v; want exactly [{create nil}]", az.calls)
			}
		})
	}
}

// (c) A type-aware deny is final: no fallback to Authorize, even though the
// Authorize arm would have admitted the type id as an entity target.
func TestTypeLevelCreate_TypeAwareDenyIsFinal(t *testing.T) {
	for _, p := range typeLevelPaths() {
		t.Run(p.name, func(t *testing.T) {
			q := newMockQuerier()
			typeID := typeIDFor(q, p.slug)
			az := &typeAwareAuthz{
				recAuthz:   recAuthz{entityTypeID: typeID},
				typeResult: apiresp.ErrForbidden,
			}

			err := p.create(t, q, az)

			if !errors.Is(err, apiresp.ErrForbidden) {
				t.Errorf("Create err = %v; want ErrForbidden", err)
			}
			if len(az.typeCalls) != 1 || az.typeCalls[0].op != "create" || az.typeCalls[0].target != typeID {
				t.Errorf("AuthorizeType calls = %+v; want exactly one (create, %d)", az.typeCalls, typeID)
			}
			if len(az.calls) != 0 {
				t.Errorf("Authorize was called %d time(s) (%+v); a type-aware deny must be final", len(az.calls), az.calls)
			}
			if q.nextID != 0 {
				t.Errorf("a row was created (nextID=%d); want none", q.nextID)
			}
		})
	}
}

// (d) A type-aware allow suffices: Authorize is never needed.
func TestTypeLevelCreate_TypeAwareAllow(t *testing.T) {
	for _, p := range typeLevelPaths() {
		t.Run(p.name, func(t *testing.T) {
			q := newMockQuerier()
			typeID := typeIDFor(q, p.slug)
			az := &typeAwareAuthz{recAuthz: recAuthz{denyAll: true}}

			if err := p.create(t, q, az); err != nil {
				t.Fatalf("Create: unexpected error: %v", err)
			}
			if len(az.typeCalls) != 1 || az.typeCalls[0].op != "create" || az.typeCalls[0].target != typeID {
				t.Errorf("AuthorizeType calls = %+v; want exactly one (create, %d)", az.typeCalls, typeID)
			}
			if len(az.calls) != 0 {
				t.Errorf("Authorize was called %d time(s); want none", len(az.calls))
			}
			if q.nextID == 0 {
				t.Error("no row created; want the create to succeed")
			}
		})
	}
}

// (e) A non-sentinel AuthorizeType error is returned unchanged and Authorize is
// not consulted.
func TestTypeLevelCreate_TypeAwareErrorPassthrough(t *testing.T) {
	sentinel := fmt.Errorf("type authorizer backend unavailable")
	for _, p := range typeLevelPaths() {
		t.Run(p.name, func(t *testing.T) {
			q := newMockQuerier()
			az := &typeAwareAuthz{typeResult: sentinel}

			err := p.create(t, q, az)

			if !errors.Is(err, sentinel) {
				t.Errorf("Create err = %v; want the AuthorizeType sentinel", err)
			}
			if len(az.calls) != 0 {
				t.Errorf("Authorize was called %d time(s); want none", len(az.calls))
			}
			if q.nextID != 0 {
				t.Errorf("a row was created (nextID=%d); want none", q.nextID)
			}
		})
	}
}
