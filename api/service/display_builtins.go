package service

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/moduleforge/core-api/display"
	"github.com/moduleforge/core-api/types"
	coredb "github.com/moduleforge/core-model/db"
)

// RegisterBuiltins wires the default display renderers for the three core
// concrete types (natural_person, corporation, service_account) and the type
// entity sentinel (types.SentinelSlug) into reg.
// Call this once at service startup after constructing the registry.
//
// Renderer behaviour:
//   - natural_person name: given_name + " " + family_name, trimmed.
//   - natural_person description: "" (v1 placeholder).
//   - corporation name: legal_name.
//   - corporation description: jurisdiction if present, else "".
//   - service_account name: label.
//   - service_account description: "".
//   - type name: the represented type's name.
//   - type description: the represented type's description, else "".
func RegisterBuiltins(reg *display.Registry, q coredb.Querier) {
	reg.Register("natural_person", display.FieldName, func(ctx context.Context, tx pgx.Tx, entityID int64) (string, error) {
		var querier coredb.Querier
		if tx != nil {
			querier = coredb.New(tx)
		} else {
			querier = q
		}
		np, err := querier.GetNaturalPersonByEntityID(ctx, entityID)
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(np.GivenName.String + " " + np.FamilyName.String), nil
	})

	reg.Register("natural_person", display.FieldDescription, func(_ context.Context, _ pgx.Tx, _ int64) (string, error) {
		return "", nil
	})

	reg.Register("corporation", display.FieldName, func(ctx context.Context, tx pgx.Tx, entityID int64) (string, error) {
		var querier coredb.Querier
		if tx != nil {
			querier = coredb.New(tx)
		} else {
			querier = q
		}
		corp, err := querier.GetCorporationByEntityID(ctx, entityID)
		if err != nil {
			return "", err
		}
		return corp.LegalName, nil
	})

	reg.Register("corporation", display.FieldDescription, func(ctx context.Context, tx pgx.Tx, entityID int64) (string, error) {
		var querier coredb.Querier
		if tx != nil {
			querier = coredb.New(tx)
		} else {
			querier = q
		}
		corp, err := querier.GetCorporationByEntityID(ctx, entityID)
		if err != nil {
			return "", err
		}
		if corp.Jurisdiction.Valid {
			return corp.Jurisdiction.String, nil
		}
		return "", nil
	})

	reg.Register("service_account", display.FieldName, func(ctx context.Context, tx pgx.Tx, entityID int64) (string, error) {
		var querier coredb.Querier
		if tx != nil {
			querier = coredb.New(tx)
		} else {
			querier = q
		}
		sa, err := querier.GetServiceAccountByEntityID(ctx, entityID)
		if err != nil {
			return "", err
		}
		return sa.Label, nil
	})

	reg.Register("service_account", display.FieldDescription, func(_ context.Context, _ pgx.Tx, _ int64) (string, error) {
		return "", nil
	})

	typeInfo := func(ctx context.Context, tx pgx.Tx, entityID int64) (types.TypeInfo, error) {
		var querier coredb.Querier
		if tx != nil {
			querier = coredb.New(tx)
		} else {
			querier = q
		}
		return types.EntityTypeInfo(ctx, querier, entityID)
	}

	reg.Register(types.SentinelSlug, display.FieldName, func(ctx context.Context, tx pgx.Tx, entityID int64) (string, error) {
		info, err := typeInfo(ctx, tx, entityID)
		if err != nil {
			return "", err
		}
		return info.Name, nil
	})

	reg.Register(types.SentinelSlug, display.FieldDescription, func(ctx context.Context, tx pgx.Tx, entityID int64) (string, error) {
		info, err := typeInfo(ctx, tx, entityID)
		if err != nil {
			return "", err
		}
		return info.Description, nil
	})
}
