package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/moduleforge/core-api/types"
	coredb "github.com/moduleforge/core-model/db"
)

// Profile is a composite view of any entity with its sub-type data populated.
// Entity holds the entity row together with its resolved fundamental_type_slug.
// Kind is populated from Entity.FundamentalTypeSlug for convenience.
//
// TaxID and TaxIDType are populated (plaintext) when a cipher is available.
// The http layer is responsible for stripping them from responses when the
// caller lacks authorization to view them.
type Profile struct {
	Entity         coredb.GetEntityByUUIDRow
	Kind           string // "natural_person" | "corporation" | "service_account" | "type"
	NaturalPerson  *coredb.GetNaturalPersonByEntityIDRow
	Corporation    *coredb.GetCorporationByEntityIDRow
	ServiceAccount *coredb.ServiceAccount
	Type           *types.TypeInfo // populated when Kind is the type sentinel (types.SentinelSlug)
	TaxID          string          // plaintext; "" if none or not decrypted
	TaxIDType      string          // "SSN" | "EIN" | ""
}

// ResolveProfileByEntityID loads an entity's sub-type records from the database
// and returns a populated Profile. Dispatches via fundamental_type_slug.
// cipher is optional; when non-nil, TaxID/TaxIDType are populated from the
// decrypted field, and a blob written under a retired key is re-encrypted and
// persisted under the active key on read. When nil they remain empty strings.
func ResolveProfileByEntityID(ctx context.Context, q coredb.Querier, entityID int64, cipher ...*RotatingCipher) (Profile, error) {
	entity, err := q.GetEntityByID(ctx, entityID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Profile{}, ErrNotFound
		}
		return Profile{}, fmt.Errorf("resolve profile: entity %d: %w", entityID, err)
	}

	profile := Profile{
		Entity: coredb.GetEntityByUUIDRow{
			ID:                  entity.ID,
			Uuid:                entity.Uuid,
			FundamentalTypeID:   entity.FundamentalTypeID,
			FundamentalTypeSlug: entity.FundamentalTypeSlug,
			CreatedAt:           entity.CreatedAt,
			UpdatedAt:           entity.UpdatedAt,
			ArchivedAt:          entity.ArchivedAt,
		},
		Kind: entity.FundamentalTypeSlug,
	}

	// Resolve optional cipher (variadic, at most one element).
	var c *RotatingCipher
	if len(cipher) > 0 {
		c = cipher[0]
	}

	switch entity.FundamentalTypeSlug {
	case "natural_person":
		np, err := q.GetNaturalPersonByEntityID(ctx, entity.ID)
		if err != nil {
			return Profile{}, fmt.Errorf("resolve profile natural_person: %w", err)
		}
		profile.NaturalPerson = &np
		if c != nil {
			val, err := c.DecryptSSN(ctx, np.EntityID, np.Ssn)
			if err != nil {
				return Profile{}, fmt.Errorf("resolve profile decrypt ssn: %w", err)
			}
			profile.TaxID = val
			profile.TaxIDType = "SSN"
		}

	case "corporation":
		corp, err := q.GetCorporationByEntityID(ctx, entity.ID)
		if err != nil {
			return Profile{}, fmt.Errorf("resolve profile corporation: %w", err)
		}
		profile.Corporation = &corp
		if c != nil {
			val, err := c.DecryptEIN(ctx, corp.EntityID, corp.Ein)
			if err != nil {
				return Profile{}, fmt.Errorf("resolve profile decrypt ein: %w", err)
			}
			profile.TaxID = val
			profile.TaxIDType = "EIN"
		}

	case "service_account":
		sa, err := q.GetServiceAccountByEntityID(ctx, entity.ID)
		if err != nil {
			return Profile{}, fmt.Errorf("resolve profile service_account: %w", err)
		}
		profile.ServiceAccount = &sa

	case types.SentinelSlug:
		info, err := types.EntityTypeInfo(ctx, q, entity.ID)
		if err != nil {
			return Profile{}, fmt.Errorf("resolve profile type: %w", err)
		}
		profile.Type = &info

	default:
		return Profile{}, fmt.Errorf("resolve profile: unknown fundamental_type_slug %q", entity.FundamentalTypeSlug)
	}

	return profile, nil
}
