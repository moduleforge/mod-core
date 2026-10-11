package service

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/moduleforge/core-api/apiresp"
	"github.com/moduleforge/core-api/observer"
)

// requireInvalidInputCode fails unless err is an InvalidInput carrying wantCode
// in its rendered 400 envelope.
func requireInvalidInputCode(t *testing.T, err error, wantCode string) {
	t.Helper()
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput, got %v", err)
	}
	rec := httptest.NewRecorder()
	apiresp.WriteError(rec, httptest.NewRequest(http.MethodPut, "/", nil), err)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), wantCode) {
		t.Errorf("envelope: got %d %s, want 400 containing %q", rec.Code, rec.Body.String(), wantCode)
	}
}

func strp(s string) *string { return &s }

// --- service account ---

func TestServiceAccountService_UpdateByEntityUUID_BlankLabelAfterAuthorization(t *testing.T) {
	for _, blank := range []string{"", "   ", "\t\n"} {
		q := newMockQuerier()
		svc := newSAService(q)
		sa, entityUUID, err := svc.Create(context.Background(), q, CreateServiceAccountInput{Label: "keep"})
		if err != nil {
			t.Fatalf("setup Create: %v", err)
		}

		err = svc.UpdateByEntityUUID(authedCtx(), q, entityUUID, UpdateServiceAccountInput{Label: strp(blank)})
		requireInvalidInputCode(t, err, "service_accounts.label_required")
		if got := q.serviceAccts[sa.EntityID].Label; got != "keep" {
			t.Errorf("blank %q still changed the label to %q", blank, got)
		}
	}

	// Authorization still comes first: a denied caller sees the authz error,
	// never the blank-label error.
	q := newMockQuerier()
	authzErr := errors.New("denied")
	_, entityUUID, _ := newSAService(q).Create(context.Background(), q, CreateServiceAccountInput{Label: "x"})
	denied := newSAService(q)
	denied.az = denyAllAuthz{err: authzErr}
	err := denied.UpdateByEntityUUID(authedCtx(), q, entityUUID, UpdateServiceAccountInput{Label: strp(" ")})
	if !errors.Is(err, authzErr) {
		t.Errorf("denied+blank: expected authz error, got %v", err)
	}
}

func TestServiceAccountService_UpdateByEntityUUID_StoresTrimmedLabel(t *testing.T) {
	q := newMockQuerier()
	svc := newSAService(q)
	sa, entityUUID, err := svc.Create(context.Background(), q, CreateServiceAccountInput{Label: "old"})
	if err != nil {
		t.Fatalf("setup Create: %v", err)
	}

	if err := svc.UpdateByEntityUUID(authedCtx(), q, entityUUID, UpdateServiceAccountInput{Label: strp("  new  ")}); err != nil {
		t.Fatalf("UpdateByEntityUUID: %v", err)
	}
	if got := q.serviceAccts[sa.EntityID].Label; got != "new" {
		t.Errorf("stored label: got %q, want %q", got, "new")
	}
}

func TestServiceAccountService_UpdateByEntityUUID_MissingRowIsNotFound(t *testing.T) {
	q := newMockQuerier()
	svc := newSAService(q)
	sa, entityUUID, err := svc.Create(context.Background(), q, CreateServiceAccountInput{Label: "x"})
	if err != nil {
		t.Fatalf("setup Create: %v", err)
	}
	// The entity still resolves (authorized caller) but its subtype row is gone.
	delete(q.serviceAccts, sa.EntityID)

	err = svc.UpdateByEntityUUID(authedCtx(), q, entityUUID, UpdateServiceAccountInput{Label: strp("y")})
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestServiceAccountService_UpdateByEntityUUID_DispatchesObserversInTransaction(t *testing.T) {
	q := newMockQuerier()
	rec := &recordingObserver{}
	tx := &fakeTx{}
	svc := newSAService(q)
	svc.db = &fakeDB{tx: tx}
	svc.obs = observer.NewObserverGroup(rec)

	sa, entityUUID, err := svc.Create(context.Background(), q, CreateServiceAccountInput{Label: "old"})
	if err != nil {
		t.Fatalf("setup Create: %v", err)
	}
	rec.observeCalls, rec.observeAfterCommitCalls = nil, nil
	tx.committed = false

	if err := svc.UpdateByEntityUUID(authedCtx(), q, entityUUID, UpdateServiceAccountInput{Label: strp(" new ")}); err != nil {
		t.Fatalf("UpdateByEntityUUID: %v", err)
	}
	if !tx.committed {
		t.Error("mutation did not run in (and commit) a transaction")
	}
	if len(rec.observeCalls) != 1 || len(rec.observeAfterCommitCalls) != 1 {
		t.Fatalf("observer calls: in-tx %d, after-commit %d, want 1 and 1", len(rec.observeCalls), len(rec.observeAfterCommitCalls))
	}
	in := rec.observeCalls[0]
	if in.op != "update" || in.resource != "service_account" || in.targetEntityID == nil || *in.targetEntityID != sa.EntityID {
		t.Errorf("in-tx observe call: %+v", in)
	}
	if b, _ := in.before.(map[string]any); b["label"] != "old" {
		t.Errorf("before: got %+v, want label old", in.before)
	}
	if a, _ := in.after.(map[string]any); a["label"] != "new" {
		t.Errorf("after: got %+v, want trimmed label new", in.after)
	}
	if post := rec.observeAfterCommitCalls[0]; post.op != "update" || post.resource != "service_account" {
		t.Errorf("after-commit observe call: %+v", post)
	}
}

func TestServiceAccountService_UpdateByEntityUUID_ObserverErrorRollsBack(t *testing.T) {
	q := newMockQuerier()
	rec := &recordingObserver{}
	tx := &fakeTx{}
	svc := newSAService(q)
	svc.db = &fakeDB{tx: tx}
	svc.obs = observer.NewObserverGroup(rec)
	_, entityUUID, err := svc.Create(context.Background(), q, CreateServiceAccountInput{Label: "old"})
	if err != nil {
		t.Fatalf("setup Create: %v", err)
	}
	rec.observeCalls, rec.observeAfterCommitCalls = nil, nil
	rec.observeErr = errors.New("audit write failed")
	tx.rolledBack = false

	err = svc.UpdateByEntityUUID(authedCtx(), q, entityUUID, UpdateServiceAccountInput{Label: strp("new")})
	if !errors.Is(err, rec.observeErr) {
		t.Fatalf("expected observer error, got %v", err)
	}
	if !tx.rolledBack {
		t.Error("observer failure did not roll the transaction back")
	}
	if len(rec.observeAfterCommitCalls) != 0 {
		t.Errorf("after-commit observers ran despite rollback: %+v", rec.observeAfterCommitCalls)
	}
}

// --- natural person / corporation ---

func TestNaturalPersonService_Update_BlankNameAfterAuthorization(t *testing.T) {
	cases := []struct {
		name string
		in   UpdateNaturalPersonInput
		code string
	}{
		{"given_name empty", UpdateNaturalPersonInput{GivenName: strp("")}, "natural_persons.given_name_required"},
		{"given_name blanks", UpdateNaturalPersonInput{GivenName: strp("  \t")}, "natural_persons.given_name_required"},
		{"family_name blanks", UpdateNaturalPersonInput{FamilyName: strp("   ")}, "natural_persons.family_name_required"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			q := newMockQuerier()
			svc := newNPService(t, q)
			entityUUID := q.seedNaturalPerson("Ada", "Lovelace")

			err := svc.UpdateByEntityUUID(authedCtx(), q, entityUUID, c.in)
			requireInvalidInputCode(t, err, c.code)

			authzErr := errors.New("denied")
			svc.az = denyAllAuthz{err: authzErr}
			if err := svc.UpdateByEntityUUID(authedCtx(), q, entityUUID, c.in); !errors.Is(err, authzErr) {
				t.Errorf("denied+blank: expected authz error, got %v", err)
			}
		})
	}
}

func TestNaturalPersonService_Update_AbsentNamesAreNoOps(t *testing.T) {
	q := newMockQuerier()
	svc := newNPService(t, q)
	entityUUID := q.seedNaturalPerson("Ada", "Lovelace")

	if err := svc.UpdateByEntityUUID(authedCtx(), q, entityUUID, UpdateNaturalPersonInput{}); err != nil {
		t.Fatalf("empty update: %v", err)
	}
	if err := svc.UpdateByEntityUUID(authedCtx(), q, entityUUID, UpdateNaturalPersonInput{GivenName: strp(" Grace ")}); err != nil {
		t.Fatalf("trimmed update: %v", err)
	}
}

func TestCorporationService_Update_BlankLegalNameAfterAuthorization(t *testing.T) {
	for _, blank := range []string{"", "  ", "\n"} {
		q := newMockQuerier()
		svc := newCorpService(t, q)
		entityUUID := q.seedCorporation("Acme", nil)
		in := UpdateCorporationInput{LegalName: strp(blank)}

		requireInvalidInputCode(t, svc.UpdateByEntityUUID(authedCtx(), q, entityUUID, in), "corporations.legal_name_required")

		authzErr := errors.New("denied")
		svc.az = denyAllAuthz{err: authzErr}
		if err := svc.UpdateByEntityUUID(authedCtx(), q, entityUUID, in); !errors.Is(err, authzErr) {
			t.Errorf("denied+blank %q: expected authz error, got %v", blank, err)
		}
	}
}

func TestCorporationService_Update_AbsentLegalNameIsNoOp(t *testing.T) {
	q := newMockQuerier()
	svc := newCorpService(t, q)
	entityUUID := q.seedCorporation("Acme", nil)

	if err := svc.UpdateByEntityUUID(authedCtx(), q, entityUUID, UpdateCorporationInput{Jurisdiction: strp("DE")}); err != nil {
		t.Fatalf("absent legal_name: %v", err)
	}
}
