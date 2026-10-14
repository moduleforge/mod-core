package service

import (
	"context"
	"fmt"

	"github.com/moduleforge/core-api/types"
	coredb "github.com/moduleforge/core-model/db"
)

// CreateEntityWithOwner creates an entity with an explicit owner via q,
// translating the database's natural-person ownership invariant
// (SQLSTATE MF002, the entities_owner_self_natural_person trigger in model/migrations/0001_baseline.sql)
// into a caller-safe error. A natural_person entity must own itself, so asking
// for any other owner fails with an error wrapping ErrInvalidInput whose
// message is fixed text: the raw Postgres error is not propagated. Every other
// error is wrapped unchanged.
//
// Passing a NULL owner (or the entity's own id, which the caller cannot know
// before insert) is the supported way to create a natural_person; use
// Querier.CreateEntity for that.
func CreateEntityWithOwner(ctx context.Context, q coredb.Querier, arg coredb.CreateEntityWithOwnerParams) (coredb.Entity, error) {
	ent, err := q.CreateEntityWithOwner(ctx, arg)
	if err != nil {
		if types.IsNaturalPersonOwnerError(err) {
			return coredb.Entity{}, fmt.Errorf("%w: a natural_person cannot be owned by another entity", ErrInvalidInput)
		}
		return coredb.Entity{}, fmt.Errorf("create entity with owner: %w", err)
	}
	return ent, nil
}
