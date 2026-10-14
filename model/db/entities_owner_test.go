package db_test

// entities_owner_test.go is a live-Postgres integration test proving that
// CreateEntityWithOwner sets owner_id at INSERT time without tripping the
// entities_owner_immutable trigger (model/migrations/0001_baseline.sql),
// which only fires BEFORE UPDATE OF owner_id — never on INSERT.
//
// This is the regression this query exists to fix: the previous
// CreateEntity-then-UPDATE-owner_id pattern used by downstream modules (e.g.
// mod-tags' TagService.Create) is a NULL -> value transition on UPDATE and
// trips entities_owner_immutable every time.
//
// The test connects to Postgres using DATABASE_URL (falling back to the same
// default used by model/Makefile) and skips itself if no server is reachable,
// since model/ has no other DB-backed Go tests and CI may not always have a
// live Postgres available. All work happens inside a transaction that is
// rolled back at the end, so the test is idempotent and leaves no fixtures
// behind in a persistent dev database.

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	db "github.com/moduleforge/core-model/db"
)

const defaultTestDatabaseURL = "postgresql://core:core@localhost:5432/core?sslmode=disable"

func testDatabaseURL() string {
	if v := os.Getenv("DATABASE_URL"); v != "" {
		return v
	}
	return defaultTestDatabaseURL
}

func connectOrSkip(t *testing.T) *pgx.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := pgx.Connect(ctx, testDatabaseURL())
	if err != nil {
		t.Skipf("skipping: no live Postgres reachable at %q: %v", testDatabaseURL(), err)
	}
	if err := conn.Ping(ctx); err != nil {
		_ = conn.Close(ctx)
		t.Skipf("skipping: Postgres ping failed: %v", err)
	}
	return conn
}

// TestCreateEntityWithOwner_SetsOwnerAtInsert proves that CreateEntityWithOwner
// can set a non-self owner_id on a non-owns-itself type (corporation) at
// INSERT time, and that doing so does not trip entities_owner_immutable
// (which only guards UPDATE OF owner_id, never INSERT).
func TestCreateEntityWithOwner_SetsOwnerAtInsert(t *testing.T) {
	conn := connectOrSkip(t)
	defer conn.Close(context.Background())

	ctx := context.Background()

	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }() // always roll back; test leaves no fixtures behind

	q := db.New(tx)

	// A natural_person owns itself (via entities_owner_self_default), so it's
	// a convenient, already-seeded non-NULL owner_id to point a corporation at.
	personType, err := q.GetTypeBySlug(ctx, "natural_person")
	if err != nil {
		t.Fatalf("GetTypeBySlug(natural_person): %v", err)
	}
	owner, err := q.CreateEntity(ctx, personType.ID)
	if err != nil {
		t.Fatalf("CreateEntity(natural_person) for owner fixture: %v", err)
	}
	if !owner.OwnerID.Valid || owner.OwnerID.Int64 != owner.ID {
		t.Fatalf("expected natural_person to own itself, got OwnerID=%+v ID=%d", owner.OwnerID, owner.ID)
	}

	// corporation does NOT descend from natural_person/service_account, so it
	// gets no owner_id default on INSERT — this is exactly the case
	// CreateEntityWithOwner exists to serve.
	corpType, err := q.GetTypeBySlug(ctx, "corporation")
	if err != nil {
		t.Fatalf("GetTypeBySlug(corporation): %v", err)
	}

	got, err := q.CreateEntityWithOwner(ctx, db.CreateEntityWithOwnerParams{
		FundamentalTypeID: corpType.ID,
		OwnerID:           owner.OwnerID,
	})
	if err != nil {
		t.Fatalf("CreateEntityWithOwner must not trip entities_owner_immutable on INSERT, got error: %v", err)
	}

	if !got.OwnerID.Valid {
		t.Fatalf("expected OwnerID to be set, got invalid/NULL: %+v", got.OwnerID)
	}
	if got.OwnerID.Int64 != owner.ID {
		t.Fatalf("expected OwnerID=%d, got %d", owner.ID, got.OwnerID.Int64)
	}
	if got.FundamentalTypeID != corpType.ID {
		t.Fatalf("expected FundamentalTypeID=%d, got %d", corpType.ID, got.FundamentalTypeID)
	}

	// Re-fetch independently to confirm the write is durable within the
	// transaction (not just an artifact of the RETURNING clause).
	refetched, err := q.GetEntityByID(ctx, got.ID)
	if err != nil {
		t.Fatalf("GetEntityByID(%d): %v", got.ID, err)
	}
	if refetched.FundamentalTypeSlug != "corporation" {
		t.Fatalf("expected refetched type slug=corporation, got %q", refetched.FundamentalTypeSlug)
	}
}
