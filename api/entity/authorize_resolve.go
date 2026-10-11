package entity

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/moduleforge/core-api/authz"
	coredb "github.com/moduleforge/core-model/db"
)

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
//  3. q.GetEntityByUUID(ctx, u). On pgx.ErrNoRows the method authorizes op
//     against a sentinel never-valid target (0) and discards the result, so a
//     miss performs the same authorizer call as a hit and the two paths do
//     comparable work. The miss still returns 0 and ErrForbidden ALWAYS,
//     ignoring any AllowNotFound opt-in for slug (an opted-in slug is exactly
//     the existence oracle this method closes) and ignoring whatever the
//     authorizer returned. AllowNotFound still governs plain Resolve. This
//     equalizes the work but is not a proof of equal latency. Authorizers
//     therefore see an extra Authorize call with target 0 on misses; they
//     should treat it as a deny and may log it. Other DB errors are wrapped,
//     not masked, and do not call the authorizer.
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
			missID := int64(0) // never a real entities.id (positive serials)
			_ = az.Authorize(ctx, op, &missID)
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
