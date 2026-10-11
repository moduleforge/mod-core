package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/moduleforge/core-api/apiresp"
	"github.com/moduleforge/core-api/authz"
	"github.com/moduleforge/core-api/entity"
	"github.com/moduleforge/core-api/observer"
	"github.com/moduleforge/core-api/txhelper"
	"github.com/moduleforge/core-api/types"
	coredb "github.com/moduleforge/core-model/db"
)

// CreateCorporationInput carries the fields required to create a corporation.
type CreateCorporationInput struct {
	LegalName    string
	Jurisdiction string // optional
	// DisplayName is no longer stored; display is derived via the display registry.
	EIN string // optional plaintext; "" means not recorded
}

// UpdateCorporationInput carries the fields that may be updated on a corporation.
// Nil fields are left unchanged.
// For EIN: nil = leave unchanged; pointer-to-"" = clear; non-empty pointer = set.
type UpdateCorporationInput struct {
	LegalName    *string
	Jurisdiction *string
	EIN          *string
}

// CorporationServicer defines corporation operations available to httpapi handlers.
type CorporationServicer interface {
	Create(ctx context.Context, q coredb.Querier, in CreateCorporationInput) (coredb.CreateCorporationRow, uuid.UUID, error)
	GetByEntityUUID(ctx context.Context, q coredb.Querier, entityUUID uuid.UUID) (Profile, error)
	UpdateByEntityUUID(ctx context.Context, q coredb.Querier, entityUUID uuid.UUID, in UpdateCorporationInput) error
}

// CorporationService implements corporation CRUD with authorization,
// transactional mutation, and observer dispatch.
type CorporationService struct {
	db             txhelper.DB
	az             authz.Authorizer
	obs            *observer.ObserverGroup
	cipher         *RotatingCipher
	newQuerier     func(pgx.Tx) coredb.Querier // injectable for tests; defaults to coredb.New
	entityResolver *entity.Resolver
	typeResolver   *types.Resolver
}

// Compile-time assertion.
var _ CorporationServicer = (*CorporationService)(nil)

func (s *CorporationService) querier(tx pgx.Tx) coredb.Querier {
	if s.newQuerier != nil {
		return s.newQuerier(tx)
	}
	return coredb.New(tx)
}

// Create inserts entity → legal_entity → corporation rows atomically inside a
// transaction. The caller-supplied q parameter is accepted for interface
// compatibility but the service opens its own transaction via s.db.
// Requires admin authorization.
func (s *CorporationService) Create(
	ctx context.Context,
	_ coredb.Querier,
	in CreateCorporationInput,
) (coredb.CreateCorporationRow, uuid.UUID, error) {
	// 1. Type-level authorization: creating a resource of this type is a
	// type-level question, answered through the type-level helper. Today that
	// is wildcard-only authority. The type id is a types.id, never an
	// entities.id, so it must NEVER be passed to Authorize as an entity
	// target: an entity-semantic Authorizer would match it against an
	// unrelated entity and grant create to its owner or a targeted grantee.
	typeID := s.typeResolver.IDForSlugMust("corporation")
	if err := authz.AuthorizeType(ctx, s.az, "create", typeID); err != nil {
		return coredb.CreateCorporationRow{}, uuid.UUID{}, err
	}

	in.LegalName = strings.TrimSpace(in.LegalName)
	if in.LegalName == "" {
		return coredb.CreateCorporationRow{}, uuid.UUID{}, apiresp.InvalidInput(apiresp.FieldError{
			Field:   "legal_name",
			Code:    "corporations.legal_name_required",
			Message: "legal_name is required",
		})
	}

	// 2. Mutate inside a transaction; observers participate in the same tx.
	var (
		corp       coredb.CreateCorporationRow
		entityUUID uuid.UUID
		entityID   int64
	)

	err := txhelper.Run(ctx, s.db, func(ctx context.Context, tx pgx.Tx) error {
		q := s.querier(tx)

		// Resolve the type ID for 'corporation' from the registry.
		t, err := q.GetTypeBySlug(ctx, "corporation")
		if err != nil {
			return fmt.Errorf("corporation.Create resolve type: %w", err)
		}

		ent, err := q.CreateEntity(ctx, t.ID)
		if err != nil {
			return fmt.Errorf("corporation.Create entity: %w", err)
		}

		_, err = q.CreateLegalEntity(ctx, ent.ID)
		if err != nil {
			return fmt.Errorf("corporation.Create legal_entity: %w", err)
		}

		var einBlob []byte
		einAudit := "unchanged"
		if strings.TrimSpace(in.EIN) != "" {
			blob, err := s.cipher.Encrypt(ctx, strings.TrimSpace(in.EIN))
			if err != nil {
				return fmt.Errorf("encrypt ein: %w", err)
			}
			einBlob = blob
			einAudit = "set"
		}

		corp, err = q.CreateCorporation(ctx, coredb.CreateCorporationParams{
			EntityID:     ent.ID,
			LegalName:    in.LegalName,
			Jurisdiction: pgtype.Text{String: in.Jurisdiction, Valid: in.Jurisdiction != ""},
			Ein:          einBlob,
		})
		if err != nil {
			return fmt.Errorf("corporation.Create corporation: %w", err)
		}

		entityUUID = ent.Uuid
		entityID = ent.ID

		after := map[string]any{
			"uuid":         ent.Uuid.String(),
			"legal_name":   in.LegalName,
			"jurisdiction": in.Jurisdiction,
			"ein":          einAudit,
		}
		return s.obs.Observe(ctx, tx, "create", "corporation", &entityID, nil, after)
	})
	if err != nil {
		return coredb.CreateCorporationRow{}, uuid.UUID{}, err
	}

	// 3. Post-commit observers.
	after := map[string]any{
		"uuid":         entityUUID.String(),
		"legal_name":   in.LegalName,
		"jurisdiction": in.Jurisdiction,
	}
	s.obs.ObserveAfterCommit(ctx, "create", "corporation", &entityID, after)

	return corp, entityUUID, nil
}

// GetByEntityUUID resolves the entity by UUID and returns its full Profile.
// Returns ErrForbidden (masked 404) if the entity does not exist.
// The cipher stored on the service is forwarded to ResolveProfileByEntityID so
// that TaxID/TaxIDType are always populated when the cipher is configured.
func (s *CorporationService) GetByEntityUUID(ctx context.Context, q coredb.Querier, entityUUID uuid.UUID) (Profile, error) {
	// 1. Resolve UUID → internal ID; default policy masks missing as 403.
	id, err := s.entityResolver.Resolve(ctx, q, entityUUID, "corporation")
	if err != nil {
		return Profile{}, err
	}

	// 2. Authorize against the resolved entity ID.
	if err := s.az.Authorize(ctx, "read", &id); err != nil {
		return Profile{}, err
	}

	profile, err := ResolveProfileByEntityID(ctx, q, id, s.cipher)
	if err != nil {
		return Profile{}, fmt.Errorf("corporation.GetByEntityUUID profile: %w", err)
	}
	return profile, nil
}

// UpdateByEntityUUID updates corporation fields for the given entity UUID.
// Requires admin authorization. Nil fields in the input are left unchanged.
// The caller-supplied q parameter is accepted for interface compatibility but
// the service opens its own transaction via s.db.
func (s *CorporationService) UpdateByEntityUUID(
	ctx context.Context,
	q coredb.Querier,
	entityUUID uuid.UUID,
	in UpdateCorporationInput,
) error {
	// 1. Masked resolve + authorize: a missing id and an existing-but-forbidden
	// id both yield ErrForbidden (no existence oracle); ErrNotFound below is
	// only reachable by an authorized caller.
	eid, err := s.entityResolver.AuthorizeResolve(ctx, q, s.az, "update", entityUUID, "corporation")
	if err != nil {
		return err
	}

	// Blank legal_name on a present field is rejected after authorization
	// (non-HTTP callers; the handler checks statelessly first).
	if in.LegalName != nil && strings.TrimSpace(*in.LegalName) == "" {
		return apiresp.InvalidInput(apiresp.FieldError{
			Field:   "legal_name",
			Code:    "corporations.legal_name_required",
			Message: "legal_name is required",
		})
	}

	// 2. Mutate inside a transaction; observers participate in the same tx.
	err = txhelper.Run(ctx, s.db, func(ctx context.Context, tx pgx.Tx) error {
		txQ := s.querier(tx)

		corp, err := txQ.GetCorporationByEntityID(ctx, eid)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNotFound
			}
			return fmt.Errorf("corporation.UpdateByEntityUUID corporation: %w", err)
		}

		before := map[string]any{
			"legal_name":   corp.LegalName,
			"jurisdiction": corp.Jurisdiction.String,
			"ein":          "unchanged",
		}

		legalName := corp.LegalName
		jurisdiction := corp.Jurisdiction
		if in.LegalName != nil {
			legalName = strings.TrimSpace(*in.LegalName)
		}
		if in.Jurisdiction != nil {
			jurisdiction = pgtype.Text{String: *in.Jurisdiction, Valid: true}
		}

		// einParam: nil = leave unchanged (COALESCE keeps DB value); []byte{} = clear; non-empty = set.
		var einParam []byte
		einAudit := "unchanged"
		if in.EIN != nil {
			val := strings.TrimSpace(*in.EIN)
			if val == "" {
				einParam = []byte{} // clear
				einAudit = "cleared"
			} else {
				b, err := s.cipher.Encrypt(ctx, val)
				if err != nil {
					return fmt.Errorf("encrypt ein: %w", err)
				}
				einParam = b
				einAudit = "set"
			}
		}

		if err := txQ.UpdateCorporation(ctx, coredb.UpdateCorporationParams{
			EntityID:     eid,
			LegalName:    legalName,
			Jurisdiction: jurisdiction,
			Ein:          einParam,
		}); err != nil {
			return fmt.Errorf("corporation.UpdateByEntityUUID update: %w", err)
		}

		after := map[string]any{
			"legal_name":   legalName,
			"jurisdiction": jurisdiction.String,
			"ein":          einAudit,
		}

		return s.obs.Observe(ctx, tx, "update", "corporation", &eid, before, after)
	})
	if err != nil {
		return err
	}

	// 3. Post-commit observers.
	s.obs.ObserveAfterCommit(ctx, "update", "corporation", &eid, nil)
	return nil
}

// GetDecryptedEIN returns the plaintext EIN for the given entity, re-encrypting
// and persisting it under the active key when it was written under an older
// one. Returns "" if not set. Returns an error only on decrypt failure
// (i.e. stored blob is corrupt or the key is wrong) — not for NULL.
func (s *CorporationService) GetDecryptedEIN(ctx context.Context, q coredb.Querier, entityID int64) (string, error) {
	corp, err := q.GetCorporationByEntityID(ctx, entityID)
	if err != nil {
		return "", fmt.Errorf("corporation.GetDecryptedEIN: %w", err)
	}
	return s.cipher.DecryptEIN(ctx, entityID, corp.Ein)
}
