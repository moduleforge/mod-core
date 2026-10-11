package entity_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/moduleforge/core-api/apiresp"
	"github.com/moduleforge/core-api/entity"
	"github.com/moduleforge/core-api/opctx"
	coredb "github.com/moduleforge/core-model/db"
)

// countingQuerier counts GetEntityByUUID calls on top of the shared stub.
type countingQuerier struct {
	resolverStubQuerier
	calls int
}

func (c *countingQuerier) GetEntityByUUID(ctx context.Context, u uuid.UUID) (coredb.GetEntityByUUIDRow, error) {
	c.calls++
	return c.resolverStubQuerier.GetEntityByUUID(ctx, u)
}

// stubAuthorizer records its inputs and returns a fixed error.
type stubAuthorizer struct {
	err    error
	calls  int
	gotOp  string
	gotTgt *int64
}

func (s *stubAuthorizer) Authorize(_ context.Context, op string, target *int64) error {
	s.calls++
	s.gotOp = op
	s.gotTgt = target
	return s.err
}

func actorCtx() context.Context { return opctx.WithActor(context.Background(), 7) }

func errBody(t *testing.T, err error) (int, string) {
	t.Helper()
	w := httptest.NewRecorder()
	apiresp.WriteError(w, httptest.NewRequest(http.MethodGet, "/", nil), err)
	return w.Code, w.Body.String()
}

func TestAuthorizeResolve_Unauthenticated(t *testing.T) {
	t.Parallel()
	q := &countingQuerier{}
	az := &stubAuthorizer{}
	_, err := entity.NewResolver().AuthorizeResolve(context.Background(), q, az, "read", uuid.New(), "s")
	if !errors.Is(err, apiresp.ErrUnauthenticated) {
		t.Fatalf("want ErrUnauthenticated; got %v", err)
	}
	if q.calls != 0 || az.calls != 0 {
		t.Errorf("no DB/authz call expected; got q=%d az=%d", q.calls, az.calls)
	}
}

func TestAuthorizeResolve_SudoOnlyProceeds(t *testing.T) {
	t.Parallel()
	row := makeEntityRow(5)
	q := &countingQuerier{resolverStubQuerier: resolverStubQuerier{entity: row}}
	az := &stubAuthorizer{}
	ctx := opctx.WithSudoActor(context.Background(), 9)
	id, err := entity.NewResolver().AuthorizeResolve(ctx, q, az, "read", row.Uuid, "s")
	if err != nil || id != 5 {
		t.Fatalf("got (%d, %v); want (5, nil)", id, err)
	}
}

func TestAuthorizeResolve_NilAuthorizerFailsClosed(t *testing.T) {
	t.Parallel()
	q := &countingQuerier{}
	_, err := entity.NewResolver().AuthorizeResolve(actorCtx(), q, nil, "read", uuid.New(), "s")
	if !errors.Is(err, entity.ErrForbidden) {
		t.Fatalf("want ErrForbidden; got %v", err)
	}
	if q.calls != 0 {
		t.Errorf("no DB read expected; got %d", q.calls)
	}
}

func missQuerier() *countingQuerier {
	return &countingQuerier{resolverStubQuerier: resolverStubQuerier{err: pgx.ErrNoRows}}
}

func TestAuthorizeResolve_MissAuthorizesSentinelTarget(t *testing.T) {
	t.Parallel()
	az := &stubAuthorizer{err: apiresp.ErrForbidden}
	id, err := entity.NewResolver().AuthorizeResolve(actorCtx(), missQuerier(), az, "read", uuid.New(), "s")
	if id != 0 || err != entity.ErrForbidden {
		t.Fatalf("got (%d, %v); want (0, ErrForbidden)", id, err)
	}
	if az.calls != 1 || az.gotOp != "read" || az.gotTgt == nil || *az.gotTgt != 0 {
		t.Errorf("want one call op=read target=&0; got calls=%d op=%q target=%v", az.calls, az.gotOp, az.gotTgt)
	}
}

func TestAuthorizeResolve_MissAuthorizerAllowStillForbidden(t *testing.T) {
	t.Parallel()
	az := &stubAuthorizer{err: nil}
	id, err := entity.NewResolver().AuthorizeResolve(actorCtx(), missQuerier(), az, "read", uuid.New(), "s")
	if id != 0 || err != entity.ErrForbidden {
		t.Fatalf("got (%d, %v); want (0, ErrForbidden)", id, err)
	}
	if az.calls != 1 {
		t.Errorf("want one Authorize call; got %d", az.calls)
	}
}

func TestAuthorizeResolve_MissAuthorizerOtherErrorNotLeaked(t *testing.T) {
	t.Parallel()
	azErr := errors.New("policy backend down")
	az := &stubAuthorizer{err: azErr}
	id, err := entity.NewResolver().AuthorizeResolve(actorCtx(), missQuerier(), az, "read", uuid.New(), "s")
	if id != 0 || err != entity.ErrForbidden || !errors.Is(err, entity.ErrForbidden) {
		t.Fatalf("got (%d, %v); want (0, ErrForbidden)", id, err)
	}
	if errors.Is(err, azErr) {
		t.Errorf("authorizer error leaked: %v", err)
	}
	if az.calls != 1 {
		t.Errorf("want one Authorize call; got %d", az.calls)
	}
}

func TestAuthorizeResolve_AllowNotFoundStillMasked(t *testing.T) {
	t.Parallel()
	q := &countingQuerier{resolverStubQuerier: resolverStubQuerier{err: pgx.ErrNoRows}}
	r := entity.NewResolver().AllowNotFound("s")
	_, err := r.AuthorizeResolve(actorCtx(), q, &stubAuthorizer{}, "read", uuid.New(), "s")
	if !errors.Is(err, entity.ErrForbidden) {
		t.Fatalf("AuthorizeResolve: want ErrForbidden; got %v", err)
	}
	_, err = r.Resolve(actorCtx(), q, uuid.New(), "s")
	if !errors.Is(err, entity.ErrNotFound) {
		t.Fatalf("Resolve: want ErrNotFound; got %v", err)
	}
}

func TestAuthorizeResolve_ExistingDenyIndistinguishableFromMissing(t *testing.T) {
	t.Parallel()
	row := makeEntityRow(3)
	r := entity.NewResolver()
	_, missErr := r.AuthorizeResolve(actorCtx(),
		&countingQuerier{resolverStubQuerier: resolverStubQuerier{err: pgx.ErrNoRows}},
		&stubAuthorizer{}, "read", uuid.New(), "s")
	_, denyErr := r.AuthorizeResolve(actorCtx(),
		&countingQuerier{resolverStubQuerier: resolverStubQuerier{entity: row}},
		&stubAuthorizer{err: apiresp.ErrForbidden}, "read", row.Uuid, "s")
	if !errors.Is(missErr, entity.ErrForbidden) || !errors.Is(denyErr, entity.ErrForbidden) {
		t.Fatalf("both must be ErrForbidden; miss=%v deny=%v", missErr, denyErr)
	}
	mc, mb := errBody(t, missErr)
	dc, db := errBody(t, denyErr)
	if mc != http.StatusForbidden || dc != http.StatusForbidden {
		t.Errorf("status miss=%d deny=%d; want 403", mc, dc)
	}
	if mb != db {
		t.Errorf("bodies differ: miss=%q deny=%q", mb, db)
	}
}

func TestAuthorizeResolve_ExistingAllow(t *testing.T) {
	t.Parallel()
	row := makeEntityRow(42)
	az := &stubAuthorizer{}
	id, err := entity.NewResolver().AuthorizeResolve(actorCtx(),
		&countingQuerier{resolverStubQuerier: resolverStubQuerier{entity: row}}, az, "update", row.Uuid, "s")
	if err != nil || id != 42 {
		t.Fatalf("got (%d, %v); want (42, nil)", id, err)
	}
	if az.calls != 1 || az.gotOp != "update" || az.gotTgt == nil || *az.gotTgt != 42 {
		t.Errorf("authorizer got calls=%d op=%q target=%v", az.calls, az.gotOp, az.gotTgt)
	}
}

func TestAuthorizeResolve_AuthorizerErrorUnchanged(t *testing.T) {
	t.Parallel()
	row := makeEntityRow(1)
	azErr := errors.New("policy backend down")
	id, err := entity.NewResolver().AuthorizeResolve(actorCtx(),
		&countingQuerier{resolverStubQuerier: resolverStubQuerier{entity: row}},
		&stubAuthorizer{err: azErr}, "read", row.Uuid, "s")
	if id != 0 || err != azErr {
		t.Fatalf("got (%d, %v); want (0, same error)", id, err)
	}
}

func TestAuthorizeResolve_DBErrorWrappedNotMasked(t *testing.T) {
	t.Parallel()
	dbErr := errors.New("connection reset")
	az := &stubAuthorizer{}
	_, err := entity.NewResolver().AuthorizeResolve(actorCtx(),
		&countingQuerier{resolverStubQuerier: resolverStubQuerier{err: dbErr}}, az, "read", uuid.New(), "s")
	if !errors.Is(err, dbErr) || errors.Is(err, entity.ErrForbidden) || err == dbErr {
		t.Fatalf("want wrapped unmasked dbErr; got %v", err)
	}
	if az.calls != 0 {
		t.Errorf("Authorize must not be called on DB error")
	}
}
