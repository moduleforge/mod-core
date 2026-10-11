package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/moduleforge/core-api/apiresp"
	"github.com/moduleforge/core-api/authz"
	"github.com/moduleforge/core-api/entity"
	"github.com/moduleforge/core-api/observer"
	"github.com/moduleforge/core-api/txhelper"
	"github.com/moduleforge/core-api/types"
	coredb "github.com/moduleforge/core-model/db"
)

// CreateServiceAccountInput carries the fields required to create a service account.
type CreateServiceAccountInput struct {
	Label string
}

// UpdateServiceAccountInput carries the fields that may be updated on a service account.
// Nil fields are left unchanged.
type UpdateServiceAccountInput struct {
	Label *string
}

// ServiceAccountServicer defines service account operations available to httpapi handlers.
type ServiceAccountServicer interface {
	Create(ctx context.Context, q coredb.Querier, in CreateServiceAccountInput) (coredb.ServiceAccount, uuid.UUID, error)
	GetByEntityUUID(ctx context.Context, q coredb.Querier, entityUUID uuid.UUID) (Profile, error)
	UpdateByEntityUUID(ctx context.Context, q coredb.Querier, entityUUID uuid.UUID, in UpdateServiceAccountInput) error
}

// ServiceAccountService implements service account CRUD with authorization,
// transactional mutation, and observer dispatch.
type ServiceAccountService struct {
	db             txhelper.DB
	az             authz.Authorizer
	obs            *observer.ObserverGroup
	newQuerier     func(pgx.Tx) coredb.Querier // injectable for tests; defaults to coredb.New
	entityResolver *entity.Resolver
	typeResolver   *types.Resolver
}

// Compile-time assertion.
var _ ServiceAccountServicer = (*ServiceAccountService)(nil)

func (s *ServiceAccountService) querier(tx pgx.Tx) coredb.Querier {
	if s.newQuerier != nil {
		return s.newQuerier(tx)
	}
	return coredb.New(tx)
}

// Create inserts entity → service_account rows atomically inside a transaction.
// The caller-supplied q parameter is accepted for interface compatibility but
// the service opens its own transaction via s.db.
// Note that service accounts do not have a legal_entity row — they attach
// directly to entity. Requires admin authorization.
func (s *ServiceAccountService) Create(
	ctx context.Context,
	_ coredb.Querier,
	in CreateServiceAccountInput,
) (coredb.ServiceAccount, uuid.UUID, error) {
	// 1. Type-level authorization: creating a resource of this type is a
	// type-level question, answered through the type-level helper. Today that
	// is wildcard-only authority. The type id is a types.id, never an
	// entities.id, so it must NEVER be passed to Authorize as an entity
	// target: an entity-semantic Authorizer would match it against an
	// unrelated entity and grant create to its owner or a targeted grantee.
	typeID := s.typeResolver.IDForSlugMust("service_account")
	if err := authz.AuthorizeType(ctx, s.az, "create", typeID); err != nil {
		return coredb.ServiceAccount{}, uuid.UUID{}, err
	}

	in.Label = strings.TrimSpace(in.Label)
	if in.Label == "" {
		return coredb.ServiceAccount{}, uuid.UUID{}, apiresp.InvalidInput(apiresp.FieldError{
			Field:   "label",
			Code:    "service_accounts.label_required",
			Message: "label is required",
		})
	}

	// 2. Mutate inside a transaction; observers participate in the same tx.
	var (
		sa         coredb.ServiceAccount
		entityUUID uuid.UUID
		entityID   int64
	)

	err := txhelper.Run(ctx, s.db, func(ctx context.Context, tx pgx.Tx) error {
		q := s.querier(tx)

		// Resolve the type ID for 'service_account' from the registry.
		t, err := q.GetTypeBySlug(ctx, "service_account")
		if err != nil {
			return fmt.Errorf("service_account.Create resolve type: %w", err)
		}

		ent, err := q.CreateEntity(ctx, t.ID)
		if err != nil {
			return fmt.Errorf("service_account.Create entity: %w", err)
		}

		sa, err = q.CreateServiceAccount(ctx, coredb.CreateServiceAccountParams{
			EntityID: ent.ID,
			Label:    in.Label,
		})
		if err != nil {
			return fmt.Errorf("service_account.Create service_account: %w", err)
		}

		entityUUID = ent.Uuid
		entityID = ent.ID

		after := map[string]any{
			"uuid":  ent.Uuid.String(),
			"label": in.Label,
		}
		return s.obs.Observe(ctx, tx, "create", "service_account", &entityID, nil, after)
	})
	if err != nil {
		return coredb.ServiceAccount{}, uuid.UUID{}, err
	}

	// 3. Post-commit observers.
	after := map[string]any{
		"uuid":  entityUUID.String(),
		"label": in.Label,
	}
	s.obs.ObserveAfterCommit(ctx, "create", "service_account", &entityID, after)

	return sa, entityUUID, nil
}

// GetByEntityUUID resolves the entity by UUID and returns its full Profile.
// Returns ErrForbidden (masked 404) if the entity does not exist.
func (s *ServiceAccountService) GetByEntityUUID(ctx context.Context, q coredb.Querier, entityUUID uuid.UUID) (Profile, error) {
	// 1. Resolve UUID → internal ID; default policy masks missing as 403.
	id, err := s.entityResolver.Resolve(ctx, q, entityUUID, "service_account")
	if err != nil {
		return Profile{}, err
	}

	// 2. Authorize against the resolved entity ID.
	if err := s.az.Authorize(ctx, "read", &id); err != nil {
		return Profile{}, err
	}

	profile, err := ResolveProfileByEntityID(ctx, q, id)
	if err != nil {
		return Profile{}, fmt.Errorf("service_account.GetByEntityUUID profile: %w", err)
	}
	return profile, nil
}

// UpdateByEntityUUID updates service_account fields for the given entity UUID.
// Requires admin authorization. Since there is no sqlc-emitted UpdateServiceAccount
// query, this returns ErrInvalidInput to signal the operation is not yet
// implemented at the DB layer, preserving a clean interface for consumers.
func (s *ServiceAccountService) UpdateByEntityUUID(
	ctx context.Context,
	q coredb.Querier,
	entityUUID uuid.UUID,
	in UpdateServiceAccountInput,
) error {
	// 1. Masked resolve + authorize, before the no-op shortcut so an
	// unauthenticated or unauthorized caller never learns anything from a
	// nil-Label request: a missing id and an existing-but-forbidden id both
	// yield ErrForbidden.
	eid, err := s.entityResolver.AuthorizeResolve(ctx, q, s.az, "update", entityUUID, "service_account")
	if err != nil {
		return err
	}

	if in.Label == nil {
		// Nothing to update.
		return nil
	}

	_, err = q.GetServiceAccountByEntityID(ctx, eid)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return fmt.Errorf("service_account.UpdateByEntityUUID service_account: %w", err)
	}

	// UpdateServiceAccount is not in the generated Querier interface.
	// Signal that the update is unsupported until the query is added to sqlc.
	// Returning ErrInvalidInput here is intentional: the interface is defined
	// and the handler wired up, but the DB mutation isn't available yet.
	return fmt.Errorf("%w: UpdateServiceAccount query not yet generated by sqlc", ErrInvalidInput)
}
