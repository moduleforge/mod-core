package service

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/moduleforge/core-api/apiresp"
	"github.com/moduleforge/core-api/observer"
)

// ownerOnlyAuthz permits an operation only on the single allowed target id.
// Any other target, including the AuthorizeResolve miss sentinel 0 and a nil
// target, is denied with ErrForbidden (common conformance rule 5).
type ownerOnlyAuthz struct{ allowed int64 }

func (a ownerOnlyAuthz) Authorize(_ context.Context, _ string, target *int64) error {
	if target != nil && *target == a.allowed {
		return nil
	}
	return ErrForbidden
}

// maskedCase is one masked-resolve method under test. call runs the method
// against the target uuid; seed creates an existing entity of the right kind
// and returns its uuid and internal id.
type maskedCase struct {
	name string
	seed func(q *mockQuerier) uuid.UUID
	call func(t *testing.T, q *mockQuerier, az ownerOnlyAuthz, u uuid.UUID) error
}

func maskedCases() []maskedCase {
	return []maskedCase{
		{
			name: "Corporation.UpdateByEntityUUID",
			seed: func(q *mockQuerier) uuid.UUID { return q.seedCorporation("Acme", nil) },
			call: func(t *testing.T, q *mockQuerier, az ownerOnlyAuthz, u uuid.UUID) error {
				svc := newCorpService(t, q)
				svc.az = az
				ln := "Acme 2"
				return svc.UpdateByEntityUUID(authedCtx(), q, u, UpdateCorporationInput{LegalName: &ln})
			},
		},
		{
			name: "NaturalPerson.UpdateByEntityUUID",
			seed: func(q *mockQuerier) uuid.UUID { return q.seedNaturalPerson("Ada", "Lovelace") },
			call: func(t *testing.T, q *mockQuerier, az ownerOnlyAuthz, u uuid.UUID) error {
				svc := newNPService(t, q)
				svc.az = az
				gn := "Augusta"
				return svc.UpdateByEntityUUID(authedCtx(), q, u, UpdateNaturalPersonInput{GivenName: &gn})
			},
		},
		{
			name: "ServiceAccount.UpdateByEntityUUID (nil label)",
			seed: func(q *mockQuerier) uuid.UUID {
				svc := newSAService(q)
				_, u, err := svc.Create(context.Background(), q, CreateServiceAccountInput{Label: "svc"})
				if err != nil {
					panic(err)
				}
				return u
			},
			call: func(t *testing.T, q *mockQuerier, az ownerOnlyAuthz, u uuid.UUID) error {
				svc := newSAService(q)
				svc.az = az
				return svc.UpdateByEntityUUID(authedCtx(), q, u, UpdateServiceAccountInput{Label: nil})
			},
		},
		{
			name: "EntityService.Archive",
			seed: func(q *mockQuerier) uuid.UUID { return q.seedNaturalPerson("Grace", "Hopper") },
			call: func(t *testing.T, q *mockQuerier, az ownerOnlyAuthz, u uuid.UUID) error {
				svc := newEntityService(q)
				svc.az = az
				svc.obs = observer.NewObserverGroup()
				return svc.Archive(authedCtx(), q, u)
			},
		},
	}
}

// TestMaskedResolve_MissAndForbiddenAreIndistinguishable verifies that a
// missing id and an existing-but-forbidden id both give ErrForbidden, and that
// the authorized caller on an existing id succeeds past the authorization gate.
func TestMaskedResolve_MissAndForbiddenAreIndistinguishable(t *testing.T) {
	for _, tc := range maskedCases() {
		t.Run(tc.name, func(t *testing.T) {
			q := newMockQuerier()
			u := tc.seed(q)
			ent := q.entities[u]

			// Missing id, caller allowed on the real entity only: 403.
			missErr := tc.call(t, q, ownerOnlyAuthz{allowed: ent.ID}, uuid.New())
			if !errors.Is(missErr, ErrForbidden) {
				t.Fatalf("missing id: got %v, want ErrForbidden", missErr)
			}
			if errors.Is(missErr, ErrNotFound) {
				t.Errorf("missing id must not be reported as not found: %v", missErr)
			}

			// Existing id the caller may not access: 403, same error value.
			forbErr := tc.call(t, q, ownerOnlyAuthz{allowed: ent.ID + 1000}, u)
			if !errors.Is(forbErr, ErrForbidden) {
				t.Fatalf("forbidden id: got %v, want ErrForbidden", forbErr)
			}
			if missErr.Error() != forbErr.Error() {
				t.Errorf("miss and forbidden differ: %q vs %q", missErr, forbErr)
			}

			// Authorized + existing: succeeds. The service-account case is a
			// nil-label no-op, so success there means authorization passed.
			if err := tc.call(t, q, ownerOnlyAuthz{allowed: ent.ID}, u); err != nil {
				t.Errorf("authorized existing id: unexpected error %v", err)
			}
		})
	}
}

// TestMaskedResolve_Unauthenticated verifies authentication is checked first,
// including for the service-account nil-label no-op that used to return nil
// before any authorization.
func TestMaskedResolve_Unauthenticated(t *testing.T) {
	q := newMockQuerier()
	u := q.seedNaturalPerson("Ada", "Lovelace")

	sa := newSAService(q)
	if err := sa.UpdateByEntityUUID(context.Background(), q, u, UpdateServiceAccountInput{}); !errors.Is(err, apiresp.ErrUnauthenticated) {
		t.Errorf("service account nil label, no actor: got %v, want ErrUnauthenticated", err)
	}
	np := newNPService(t, q)
	gn := "x"
	if err := np.UpdateByEntityUUID(context.Background(), q, u, UpdateNaturalPersonInput{GivenName: &gn}); !errors.Is(err, apiresp.ErrUnauthenticated) {
		t.Errorf("natural person, no actor: got %v, want ErrUnauthenticated", err)
	}
}

// TestMaskedResolve_PostAuthNotFoundStays404 verifies that once authorized, a
// missing subtype row still reports ErrNotFound (the real post-authorization
// lookup).
func TestMaskedResolve_PostAuthNotFoundStays404(t *testing.T) {
	q := newMockQuerier()
	// An entity row with no natural_persons subtype row.
	u := q.seedNaturalPerson("Ada", "Lovelace")
	ent := q.entities[u]
	delete(q.naturalPersons, ent.ID)

	svc := newNPService(t, q)
	gn := "x"
	err := svc.UpdateByEntityUUID(authedCtx(), q, u, UpdateNaturalPersonInput{GivenName: &gn})
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("got %v, want ErrNotFound", err)
	}
}
