package entity

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/moduleforge/core-api/authz"
	coredb "github.com/moduleforge/core-model/db"
)

// missDelayFloor and missDelayJitter tune the randomized delay applied only on
// the AuthorizeResolve miss path: the delay is uniform in
// [missDelayFloor, missDelayFloor+missDelayJitter). Variables so tests can
// override them; if both are <= 0 no wait occurs.
var (
	missDelayFloor  = 5 * time.Millisecond
	missDelayJitter = 15 * time.Millisecond
)

// missDelay waits a randomized duration, returning early if ctx is done.
func missDelay(ctx context.Context) {
	d := missDelayFloor
	if missDelayJitter > 0 {
		d += rand.N(missDelayJitter)
	}
	if d <= 0 {
		return
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
	case <-ctx.Done():
	}
}

// AuthorizeResolve resolves u to its entities.id and authorizes op against
// that id, in an order that leaks no existence information: a missing
// resource and an existing-but-forbidden one are indistinguishable to an
// unauthorized caller.
//
// Order:
//  1. authz.RequireAuthenticated(ctx); on error returns 0 and
//     apiresp.ErrUnauthenticated before any DB read. It is sudo-aware: an
//     effective sudo actor counts as authenticated.
//  2. A nil az fails closed with ErrForbidden, before any DB read.
//  3. q.GetEntityByUUID(ctx, u). On pgx.ErrNoRows it returns 0 and
//     ErrForbidden ALWAYS, ignoring any AllowNotFound opt-in for slug: an
//     opted-in slug is exactly the existence oracle this method closes.
//     AllowNotFound still governs plain Resolve. The miss path first adds a
//     small randomized delay, which blunts (but does not eliminate) the
//     timing difference versus the authorized-hit path; it honors ctx
//     cancellation and still returns ErrForbidden. Other DB errors are
//     wrapped, not masked, and not delayed.
//  4. az.Authorize(ctx, op, &id). Its error is returned unchanged (no
//     remapping) with a 0 id; on success the id is returned.
//
// The masked lookup in step 3 is a necessary DB read: the authorizer needs
// the entities.id as its target.
func (r *Resolver) AuthorizeResolve(ctx context.Context, q coredb.Querier, az authz.Authorizer, op string, u uuid.UUID, slug string) (int64, error) {
	if err := authz.RequireAuthenticated(ctx); err != nil {
		return 0, err
	}
	if az == nil {
		return 0, ErrForbidden
	}
	row, err := q.GetEntityByUUID(ctx, u)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			missDelay(ctx)
			return 0, ErrForbidden
		}
		return 0, fmt.Errorf("entity.AuthorizeResolve %s %s: %w", slug, u, err)
	}
	id := row.ID
	if err := az.Authorize(ctx, op, &id); err != nil {
		return 0, err
	}
	return id, nil
}
