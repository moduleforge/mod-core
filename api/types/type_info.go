package types

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	coredb "github.com/moduleforge/core-model/db"
)

// SentinelSlug is the slug of the sentinel root type whose instances are the
// type entities themselves (one per row of the types table). Callers must
// reference it through this constant rather than hard-coding the string.
const SentinelSlug = "type"

// ErrTypeNotFound is returned by EntityTypeInfo when no type is represented by
// the given entity id. Callers distinguish it with errors.Is.
var ErrTypeNotFound = errors.New("types: type not found")

// TypeInfo is the read view of one row of the types table together with the id
// of the type's own entity.
type TypeInfo struct {
	ID           int64
	Slug         string
	Name         string
	Description  string // "" when the column is NULL
	ParentID     *int64 // nil for a root type
	Concrete     bool
	DeprecatedAt *time.Time // nil when the type is not deprecated
	EntityID     int64      // entities.id of the type's own entity
}

// Deprecated reports whether the type has been deprecated.
func (t TypeInfo) Deprecated() bool { return t.DeprecatedAt != nil }

// ListOptions controls List.
type ListOptions struct {
	// IncludeDeprecated is the explicit override of the default rule that
	// lists of types hide deprecated types.
	IncludeDeprecated bool
}

func typeInfoFromRow(row coredb.Type) TypeInfo {
	info := TypeInfo{
		ID:           row.ID,
		Slug:         row.Slug,
		Name:         row.Name,
		Description:  row.Description.String,
		Concrete:     row.Concrete,
		DeprecatedAt: row.DeprecatedAt,
		EntityID:     row.EntityID.Int64,
	}
	if row.ParentID.Valid {
		pid := row.ParentID.Int64
		info.ParentID = &pid
	}
	return info
}

// List returns the types ordered by id (the append-only registration order).
// Deprecated types are excluded unless opts.IncludeDeprecated is true. It is
// built on coredb.Querier.ListAllTypes; the types table is small and
// append-only, so no paging or dedicated query is needed.
//
// Contrast with Resolver, which deliberately includes deprecated types.
func List(ctx context.Context, q coredb.Querier, opts ListOptions) ([]TypeInfo, error) {
	rows, err := q.ListAllTypes(ctx)
	if err != nil {
		return nil, fmt.Errorf("types.List: list types: %w", err)
	}
	out := make([]TypeInfo, 0, len(rows))
	for _, row := range rows {
		if row.DeprecatedAt != nil && !opts.IncludeDeprecated {
			continue
		}
		out = append(out, typeInfoFromRow(row))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// EntityTypeInfo returns the type represented by the type entity with the
// given entities.id. Deprecated types are included: a type entity stays
// readable after its type is deprecated. It returns ErrTypeNotFound (wrapped)
// when no type is represented by entityID.
func EntityTypeInfo(ctx context.Context, q coredb.Querier, entityID int64) (TypeInfo, error) {
	rows, err := q.ListAllTypes(ctx)
	if err != nil {
		return TypeInfo{}, fmt.Errorf("types.EntityTypeInfo: list types: %w", err)
	}
	for _, row := range rows {
		if row.EntityID.Valid && row.EntityID.Int64 == entityID {
			return typeInfoFromRow(row), nil
		}
	}
	return TypeInfo{}, fmt.Errorf("types.EntityTypeInfo: entity %d: %w", entityID, ErrTypeNotFound)
}
