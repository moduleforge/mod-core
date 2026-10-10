//go:build integration

package types_test

import (
	"context"
	"testing"

	coredb "github.com/moduleforge/core-model/db"

	"github.com/moduleforge/core-api/service"
	"github.com/moduleforge/core-api/types"
)

// TestResolveProfileForRealTypeEntity reads a freshly registered type's entity
// through service.ResolveProfileByEntityID end to end, and checks that List
// hides the type once deprecated unless the override is set.
func TestResolveProfileForRealTypeEntity(t *testing.T) {
	ctx := context.Background()
	slug := newSlug("profile")
	insertType(t, slug)
	entityID := typeEntityID(t, slug)
	q := coredb.New(pool)

	profile, err := service.ResolveProfileByEntityID(ctx, q, entityID)
	if err != nil {
		t.Fatalf("resolve profile: %v", err)
	}
	if profile.Kind != types.SentinelSlug || profile.Type == nil || profile.Type.Slug != slug {
		t.Fatalf("profile: kind=%q type=%+v", profile.Kind, profile.Type)
	}

	mustExec(t, `UPDATE types SET deprecated_at = now() WHERE slug = $1`, slug)
	hidden, err := types.List(ctx, q, types.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, ti := range hidden {
		if ti.Slug == slug {
			t.Fatal("deprecated type listed without override")
		}
	}
	shown, err := types.List(ctx, q, types.ListOptions{IncludeDeprecated: true})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, ti := range shown {
		found = found || ti.Slug == slug
	}
	if !found {
		t.Fatal("deprecated type missing with IncludeDeprecated")
	}
	if p, err := service.ResolveProfileByEntityID(ctx, q, entityID); err != nil || p.Type == nil || p.Type.DeprecatedAt == nil {
		t.Fatalf("deprecated type entity must stay readable: %v %+v", err, p.Type)
	}
}
