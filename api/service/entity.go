package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/moduleforge/core-api/authz"
	"github.com/moduleforge/core-api/entity"
	"github.com/moduleforge/core-api/observer"
	"github.com/moduleforge/core-api/opctx"
	"github.com/moduleforge/core-api/txhelper"
	coredb "github.com/moduleforge/core-model/db"
)

// EntityServicer defines the entity operations available to httpapi handlers.
type EntityServicer interface {
	GetByUUID(ctx context.Context, q coredb.Querier, id uuid.UUID) (coredb.GetEntityByUUIDRow, error)
	GetByID(ctx context.Context, q coredb.Querier, id int64) (coredb.GetEntityByIDRow, error)
	GetSelf(ctx context.Context, q coredb.Querier) (Profile, error)
	Archive(ctx context.Context, q coredb.Querier, entityUUID uuid.UUID) error
	ResolveProfile(ctx context.Context, q coredb.Querier, entityUUID uuid.UUID) (Profile, error)
}

// EntityService implements entity-level operations with authorization,
// transactional mutation, and observer dispatch.
type EntityService struct {
	db             txhelper.DB
	az             authz.Authorizer
	obs            *observer.ObserverGroup
	newQuerier     func(pgx.Tx) coredb.Querier // injectable for tests; defaults to coredb.New
	entityResolver *entity.Resolver
}

// Compile-time assertion.
var _ EntityServicer = (*EntityService)(nil)

func (s *EntityService) querier(tx pgx.Tx) coredb.Querier {
	if s.newQuerier != nil {
		return s.newQuerier(tx)
	}
	return coredb.New(tx)
}

// GetByUUID fetches a single entity by its public UUID.
// Returns ErrForbidden (masked 404) if the entity does not exist.
func (s *EntityService) GetByUUID(ctx context.Context, q coredb.Querier, entityUUID uuid.UUID) (coredb.GetEntityByUUIDRow, error) {
	// Resolve UUID → internal ID; default policy masks missing as 403.
	internalID, err := s.entityResolver.Resolve(ctx, q, entityUUID, "entity")
	if err != nil {
		return coredb.GetEntityByUUIDRow{}, err
	}
	if err := s.az.Authorize(ctx, "read", &internalID); err != nil {
		return coredb.GetEntityByUUIDRow{}, err
	}
	e, err := q.GetEntityByUUID(ctx, entityUUID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return coredb.GetEntityByUUIDRow{}, ErrNotFound
		}
		return coredb.GetEntityByUUIDRow{}, fmt.Errorf("entity.GetByUUID: %w", err)
	}
	return e, nil
}

// GetByID fetches a single entity by internal ID.
// Returns ErrNotFound if no matching row exists.
func (s *EntityService) GetByID(ctx context.Context, q coredb.Querier, id int64) (coredb.GetEntityByIDRow, error) {
	if err := s.az.Authorize(ctx, "read", &id); err != nil {
		return coredb.GetEntityByIDRow{}, err
	}
	e, err := q.GetEntityByID(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return coredb.GetEntityByIDRow{}, ErrNotFound
		}
		return coredb.GetEntityByIDRow{}, fmt.Errorf("entity.GetByID: %w", err)
	}
	return e, nil
}

// GetSelf returns the Profile for the authenticated caller's entity.
// The actor entity ID is read from ctx via opctx.ActorEntityID; returns
// ErrForbidden if the ctx carries no actor (unauthenticated request).
func (s *EntityService) GetSelf(ctx context.Context, q coredb.Querier) (Profile, error) {
	eid, ok := opctx.ActorEntityID(ctx)
	if !ok {
		return Profile{}, ErrForbidden
	}
	if err := s.az.Authorize(ctx, "read", &eid); err != nil {
		return Profile{}, err
	}
	profile, err := ResolveProfileByEntityID(ctx, q, eid)
	if err != nil {
		return Profile{}, fmt.Errorf("entity.GetSelf: %w", err)
	}
	return profile, nil
}

// ResolveProfile loads an entity by UUID then resolves its sub-type profile.
// Returns ErrForbidden (masked 404) if the entity does not exist.
func (s *EntityService) ResolveProfile(ctx context.Context, q coredb.Querier, entityUUID uuid.UUID) (Profile, error) {
	// Resolve UUID → internal ID; default policy masks missing as 403.
	id, err := s.entityResolver.Resolve(ctx, q, entityUUID, "entity")
	if err != nil {
		return Profile{}, err
	}
	if err := s.az.Authorize(ctx, "read", &id); err != nil {
		return Profile{}, err
	}
	profile, err := ResolveProfileByEntityID(ctx, q, id)
	if err != nil {
		return Profile{}, err
	}
	return profile, nil
}

// Archive soft-deletes an entity. Requires admin authorization.
// The caller-supplied q parameter is accepted for interface compatibility but
// the service opens its own transaction via s.db.
func (s *EntityService) Archive(ctx context.Context, q coredb.Querier, entityUUID uuid.UUID) error {
	// 1. Masked resolve + authorize: a missing id and an existing-but-forbidden
	// id both yield ErrForbidden (no existence oracle).
	eid, err := s.entityResolver.AuthorizeResolve(ctx, q, s.az, "delete", entityUUID, "entity")
	if err != nil {
		return err
	}

	// 2. Mutate inside a transaction; observers participate in the same tx.
	err = txhelper.Run(ctx, s.db, func(ctx context.Context, tx pgx.Tx) error {
		txQ := s.querier(tx)
		if err := txQ.ArchiveEntity(ctx, entityUUID); err != nil {
			return fmt.Errorf("entity.Archive: %w", err)
		}

		before := map[string]any{"uuid": entityUUID.String()}
		return s.obs.Observe(ctx, tx, "delete", "entity", &eid, before, nil)
	})
	if err != nil {
		return err
	}

	// 3. Post-commit observers.
	s.obs.ObserveAfterCommit(ctx, "delete", "entity", &eid, nil)
	return nil
}
