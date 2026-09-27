package db_test

// entities_write_functions_test.go is a live-Postgres integration test
// proving migration 0100_entity_write_functions.sql's SECURITY INVOKER
// wrapper functions (core_create_entity, core_create_entity_with_owner,
// core_archive_entity, core_unarchive_entity) leave standalone entity-write
// behavior byte-equivalent to the raw INSERT/UPDATE statements they replace
// in model/queries/entities.sql: the self-owner default still fires on
// INSERT (including through a NULL p_owner_id, since the function is
// deliberately not STRICT), the returned row is unchanged, and archiving /
// unarchiving an already-archived / already-unarchived entity remains a
// no-op.
//
// Connects via the same connectOrSkip/testDatabaseURL helpers defined in
// entities_owner_test.go (same package); all work happens inside a
// transaction rolled back at the end, so the test is idempotent and leaves
// no fixtures behind in a persistent dev database.

import (
	"context"
	"testing"

	db "github.com/moduleforge/core-model/db"
)

func TestEntityWriteFunctions_StandaloneBehavior(t *testing.T) {
	conn := connectOrSkip(t)
	defer conn.Close(context.Background())

	ctx := context.Background()

	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }() // always roll back; test leaves no fixtures behind

	q := db.New(tx)

	personType, err := q.GetTypeBySlug(ctx, "natural_person")
	if err != nil {
		t.Fatalf("GetTypeBySlug(natural_person): %v", err)
	}
	corpType, err := q.GetTypeBySlug(ctx, "corporation")
	if err != nil {
		t.Fatalf("GetTypeBySlug(corporation): %v", err)
	}

	t.Run("CreateEntity sets self-owner default for natural_person", func(t *testing.T) {
		got, err := q.CreateEntity(ctx, personType.ID)
		if err != nil {
			t.Fatalf("CreateEntity(natural_person): %v", err)
		}
		if got.ID == 0 {
			t.Fatalf("expected non-zero ID, got %d", got.ID)
		}
		if got.FundamentalTypeID != personType.ID {
			t.Fatalf("expected FundamentalTypeID=%d, got %d", personType.ID, got.FundamentalTypeID)
		}
		if got.ArchivedAt != nil {
			t.Fatalf("expected ArchivedAt nil on create, got %v", got.ArchivedAt)
		}
		if !got.OwnerID.Valid || got.OwnerID.Int64 != got.ID {
			t.Fatalf("expected natural_person to own itself via core_create_entity, got OwnerID=%+v ID=%d", got.OwnerID, got.ID)
		}
	})

	t.Run("CreateEntity leaves owner NULL for a non-owns-itself type", func(t *testing.T) {
		got, err := q.CreateEntity(ctx, corpType.ID)
		if err != nil {
			t.Fatalf("CreateEntity(corporation): %v", err)
		}
		if got.OwnerID.Valid {
			t.Fatalf("expected corporation OwnerID to stay NULL, got %+v", got.OwnerID)
		}
	})

	t.Run("CreateEntityWithOwner accepts NULL owner and self-owner trigger fills it in", func(t *testing.T) {
		// OwnerID left at its zero value (Valid: false) -- core_create_entity_with_owner
		// is deliberately not STRICT, so this NULL argument reaches the INSERT,
		// where entities_owner_self_default (migration 0013_entity_ownership.sql)
		// fills it in for a natural_person.
		got, err := q.CreateEntityWithOwner(ctx, db.CreateEntityWithOwnerParams{
			FundamentalTypeID: personType.ID,
		})
		if err != nil {
			t.Fatalf("CreateEntityWithOwner(natural_person, NULL owner): %v", err)
		}
		if !got.OwnerID.Valid || got.OwnerID.Int64 != got.ID {
			t.Fatalf("expected self-owner default to fire on NULL p_owner_id, got OwnerID=%+v ID=%d", got.OwnerID, got.ID)
		}
	})

	t.Run("ArchiveEntity then UnarchiveEntity round-trip, with no-op on repeat", func(t *testing.T) {
		created, err := q.CreateEntity(ctx, corpType.ID)
		if err != nil {
			t.Fatalf("CreateEntity(corporation) fixture: %v", err)
		}

		if err := q.ArchiveEntity(ctx, created.Uuid); err != nil {
			t.Fatalf("ArchiveEntity: %v", err)
		}
		archived, err := q.GetEntityByID(ctx, created.ID)
		if err != nil {
			t.Fatalf("GetEntityByID after archive: %v", err)
		}
		if archived.ArchivedAt == nil {
			t.Fatalf("expected ArchivedAt set after ArchiveEntity, got nil")
		}
		firstArchivedAt := *archived.ArchivedAt

		// Archiving an already-archived entity is a no-op: the
		// WHERE archived_at IS NULL guard means the second call matches zero
		// rows and archived_at does not move forward.
		if err := q.ArchiveEntity(ctx, created.Uuid); err != nil {
			t.Fatalf("ArchiveEntity (second call, no-op): %v", err)
		}
		reArchived, err := q.GetEntityByID(ctx, created.ID)
		if err != nil {
			t.Fatalf("GetEntityByID after second archive: %v", err)
		}
		if reArchived.ArchivedAt == nil || !reArchived.ArchivedAt.Equal(firstArchivedAt) {
			t.Fatalf("expected ArchiveEntity no-op to leave ArchivedAt=%v unchanged, got %v", firstArchivedAt, reArchived.ArchivedAt)
		}

		if err := q.UnarchiveEntity(ctx, created.Uuid); err != nil {
			t.Fatalf("UnarchiveEntity: %v", err)
		}
		unarchived, err := q.GetEntityByID(ctx, created.ID)
		if err != nil {
			t.Fatalf("GetEntityByID after unarchive: %v", err)
		}
		if unarchived.ArchivedAt != nil {
			t.Fatalf("expected ArchivedAt nil after UnarchiveEntity, got %v", unarchived.ArchivedAt)
		}

		// Unarchiving an already-unarchived entity is a no-op too (the
		// WHERE archived_at IS NOT NULL guard matches zero rows).
		if err := q.UnarchiveEntity(ctx, created.Uuid); err != nil {
			t.Fatalf("UnarchiveEntity (second call, no-op): %v", err)
		}
		reUnarchived, err := q.GetEntityByID(ctx, created.ID)
		if err != nil {
			t.Fatalf("GetEntityByID after second unarchive: %v", err)
		}
		if reUnarchived.ArchivedAt != nil {
			t.Fatalf("expected ArchivedAt to stay nil after no-op UnarchiveEntity, got %v", reUnarchived.ArchivedAt)
		}
	})
}
