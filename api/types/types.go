// Package types provides a startup-time cache that resolves resource slugs to
// internal type IDs. Because the types table is append-only, the cache is safe
// to populate once at construction time and hold for the lifetime of the
// process.
package types

import (
	"context"
	"fmt"

	coredb "github.com/moduleforge/core-model/db"
)

// Resolver maps a fundamental_type_slug to its internal type ID. Populated
// once at construction time from the types table.
//
// The types table is append-only, so a permanent in-process cache is safe: a
// slug that was present at startup will never be removed or reassigned a
// different ID.
//
// Resolver deliberately includes deprecated types: existing entities of a
// deprecated type and type-level authorization checks still need slug-to-id
// resolution. It is the documented always-include override to the rule that
// lists of types hide deprecated types by default (see List, which hides them
// unless ListOptions.IncludeDeprecated is set).
type Resolver struct {
	bySlug map[string]int64
}

// New loads all rows from the types table and returns a populated Resolver.
// Returns an error if the query fails or the result set is empty (an empty
// types table indicates a misconfigured database).
func New(ctx context.Context, q coredb.Querier) (*Resolver, error) {
	rows, err := q.ListAllTypes(ctx)
	if err != nil {
		return nil, fmt.Errorf("types.New: list types: %w", err)
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("types.New: types table is empty; database may be uninitialized")
	}

	bySlug := make(map[string]int64, len(rows))
	for _, row := range rows {
		bySlug[row.Slug] = row.ID
	}

	return &Resolver{bySlug: bySlug}, nil
}

// IDForSlug returns the internal type ID for the given slug and a boolean
// indicating whether the slug was found in the cache.
func (r *Resolver) IDForSlug(slug string) (int64, bool) {
	id, ok := r.bySlug[slug]
	return id, ok
}

// IDForSlugMust returns the internal type ID for the given slug. It panics
// with a clear message if the slug is not present in the cache.
//
// Use this at call sites that reference compile-time constant slugs.
// Use IDForSlug when the slug comes from runtime input.
func (r *Resolver) IDForSlugMust(slug string) int64 {
	id, ok := r.bySlug[slug]
	if !ok {
		panic(fmt.Sprintf("types.Resolver: unknown slug %q; was it registered in the types table?", slug))
	}
	return id
}

// NewFromMap constructs a Resolver from a pre-built slug→ID map.
// This is intended for use in tests only; production code must use New.
func NewFromMap(m map[string]int64) *Resolver {
	bySlug := make(map[string]int64, len(m))
	for k, v := range m {
		bySlug[k] = v
	}
	return &Resolver{bySlug: bySlug}
}
