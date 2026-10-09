//go:build integration

package types_test

// type_entities_integration_test.go proves the schema invariants of type
// entities (model/migrations/0002_types.sql and 0008_entities.sql) against a
// live, ephemeral Postgres built from mod-core's own migrations: every types
// row is represented by exactly one entity of the sentinel type 'type', that
// entity is created by the type's own insert, and the pairing is guarded at
// insert time, update time, archive time, and commit time.
//
// The tests live in api/types, not api/service or api/httpapi, because those
// packages' TestMains call authztest.SeedOwnerFixture, which refuses once type
// entities occupy the low entity ids.
//
// Harness adapted from api/authz/setup/grant_table_integration_test.go (same
// CORE_DEV_PG_HOST / CORE_DEV_PG_PORT handling, own shadow database). Run, from
// the repo root, against a throwaway Postgres (AGENTS.md, "Integration tests
// (throwaway Postgres)"):
//
//	(cd api && CORE_DEV_PG_HOST=127.0.0.1 CORE_DEV_PG_PORT="$port" \
//	   go test -tags=integration -p 1 -count=1 -v ./types/...)

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	typeEntitiesDB = "core_type_entities_verify_dev"
	pgContainer    = "users-module-postgres"
	pgUser         = "users"
	pgPassword     = "users"
)

// pool is the connection pool (superuser) for the shadow database.
var pool *pgxpool.Pool

var pgPort = envOrDefault("CORE_DEV_PG_PORT", "5432")

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func TestMain(m *testing.M) {
	if err := checkPrerequisites(); err != nil {
		fmt.Fprintf(os.Stderr, "integration: skipping mod-core type-entity tests: %v\n", err)
		os.Exit(0)
	}
	host := resolvePostgresHost()
	if !postgresReachable(host, 2*time.Second) {
		fmt.Fprintf(os.Stderr, "integration: skipping mod-core type-entity tests: postgres not reachable at %s:%s\n", host, pgPort)
		if os.Getenv("CORE_DEV_PG_HOST") != "" {
			os.Exit(1) // explicitly configured: a silent skip would hide a red run.
		}
		os.Exit(0)
	}
	if err := resetDB(host); err != nil {
		fmt.Fprintf(os.Stderr, "integration: shadow DB reset failed: %v\n", err)
		os.Exit(1)
	}
	p, err := pgxpool.New(context.Background(),
		fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable", pgUser, pgPassword, host, pgPort, typeEntitiesDB))
	if err != nil {
		fmt.Fprintf(os.Stderr, "integration: open pool: %v\n", err)
		os.Exit(1)
	}
	pool = p
	code := m.Run()
	pool.Close()
	os.Exit(code)
}

func checkPrerequisites() error {
	if os.Getenv("CORE_DEV_PG_HOST") == "" {
		out, err := exec.Command("docker", "inspect", "--format={{.State.Running}}", pgContainer).Output()
		if err != nil {
			return fmt.Errorf("docker inspect: %w", err)
		}
		if strings.TrimSpace(string(out)) != "true" {
			return fmt.Errorf("container %s is not running", pgContainer)
		}
	}
	if _, err := exec.LookPath("goose"); err != nil {
		return fmt.Errorf("goose not in PATH: %w", err)
	}
	return nil
}

func resolvePostgresHost() string {
	if h := os.Getenv("CORE_DEV_PG_HOST"); h != "" {
		return h
	}
	if postgresReachable("localhost", 500*time.Millisecond) {
		return "localhost"
	}
	out, err := exec.Command("docker", "inspect", "--format",
		"{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}", pgContainer).Output()
	if err == nil {
		if ip := strings.TrimSpace(string(out)); ip != "" && postgresReachable(ip, 500*time.Millisecond) {
			return ip
		}
	}
	return "172.23.0.3"
}

func postgresReachable(host string, timeout time.Duration) bool {
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, pgPort), timeout)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// resetDB drops and recreates the shadow database and applies mod-core's own
// migrations (model/migrations) with goose.
func resetDB(host string) error {
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, fmt.Sprintf("postgres://%s:%s@%s:%s/postgres?sslmode=disable", pgUser, pgPassword, host, pgPort))
	if err != nil {
		return fmt.Errorf("connect admin: %w", err)
	}
	defer conn.Close(ctx) //nolint:errcheck // best-effort close on an admin connection we're done with
	for _, stmt := range []string{"DROP DATABASE IF EXISTS " + typeEntitiesDB, "CREATE DATABASE " + typeEntitiesDB} {
		if _, err := conn.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("%s: %w", stmt, err)
		}
	}
	dsn := fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable", pgUser, pgPassword, host, pgPort, typeEntitiesDB)
	cmd := exec.Command("goose", "-dir", migrationsDir(), "postgres", dsn, "up") //nolint:gosec // fixed args/resolved paths, not user input
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("goose up: %w\n%s", err, out)
	}
	return nil
}

// migrationsDir resolves <repo>/model/migrations from this file's location
// (<repo>/api/types/).
func migrationsDir() string {
	_, thisFile, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(thisFile), "..", "..", "model", "migrations")
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

var slugSeq atomic.Int64

// newSlug returns a slug unique within this test run.
func newSlug(prefix string) string {
	return fmt.Sprintf("%s_%d", prefix, slugSeq.Add(1))
}

func queryInt(t *testing.T, sql string, args ...any) int64 {
	t.Helper()
	var n int64
	if err := pool.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		t.Fatalf("query %q: %v", sql, err)
	}
	return n
}

func mustExec(t *testing.T, sql string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), sql, args...); err != nil {
		t.Fatalf("exec %q: %v", sql, err)
	}
}

func execErr(sql string, args ...any) error {
	_, err := pool.Exec(context.Background(), sql, args...)
	return err
}

// insertType registers an ordinary concrete child of 'entity' and returns its
// types.id.
func insertType(t *testing.T, slug string) int64 {
	t.Helper()
	return queryInt(t, `
INSERT INTO types (slug, parent_id, concrete, name)
SELECT $1, id, true, $1 FROM types WHERE slug = 'entity'
RETURNING id`, slug)
}

func sentinelID(t *testing.T) int64 {
	t.Helper()
	return queryInt(t, `SELECT id FROM types WHERE slug = 'type'`)
}

func typeEntityID(t *testing.T, slug string) int64 {
	t.Helper()
	return queryInt(t, `SELECT entity_id FROM types WHERE slug = $1`, slug)
}

func countSentinelEntities(t *testing.T) int64 {
	t.Helper()
	return queryInt(t, `SELECT count(*) FROM entities WHERE fundamental_type_id = (SELECT id FROM types WHERE slug = 'type')`)
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestSeededTypesHaveDistinctSentinelEntities(t *testing.T) {
	ctx := context.Background()
	rows, err := pool.Query(ctx, `
SELECT t.slug, t.entity_id, e.fundamental_type_id
FROM types t LEFT JOIN entities e ON e.id = t.entity_id
WHERE t.slug IN ('entity','legal_entity','natural_person','corporation','service_account','type','app')`)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()
	sentinel := sentinelID(t)
	seen := map[int64]string{}
	n := 0
	for rows.Next() {
		var slug string
		var entityID, ftype *int64
		if err := rows.Scan(&slug, &entityID, &ftype); err != nil {
			t.Fatalf("scan: %v", err)
		}
		n++
		if entityID == nil {
			t.Errorf("type %q: entity_id is NULL", slug)
			continue
		}
		if prev, dup := seen[*entityID]; dup {
			t.Errorf("types %q and %q share entity %d", prev, slug, *entityID)
		}
		seen[*entityID] = slug
		if ftype == nil || *ftype != sentinel {
			t.Errorf("type %q: entity %d fundamental_type_id = %v, want sentinel %d", slug, *entityID, ftype, sentinel)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	if n != 7 {
		t.Errorf("expected 7 seeded types, saw %d", n)
	}
	// Globally: no type lacks an entity, and entity_id values are distinct.
	if got := queryInt(t, `SELECT count(*) FROM types WHERE entity_id IS NULL`); got != 0 {
		t.Errorf("%d types have NULL entity_id", got)
	}
	if got := queryInt(t, `SELECT count(*) FROM types`) - queryInt(t, `SELECT count(DISTINCT entity_id) FROM types`); got != 0 {
		t.Errorf("types.entity_id is not distinct (diff %d)", got)
	}
}

func TestSentinelEntityIsSelfReferential(t *testing.T) {
	sentinel := sentinelID(t)
	var fundamental int64
	if err := pool.QueryRow(context.Background(), `
SELECT e.fundamental_type_id FROM types t JOIN entities e ON e.id = t.entity_id WHERE t.slug = 'type'`).Scan(&fundamental); err != nil {
		t.Fatalf("query: %v", err)
	}
	if fundamental != sentinel {
		t.Errorf("sentinel entity fundamental_type_id = %d, want %d (the sentinel itself)", fundamental, sentinel)
	}
	if got := queryInt(t, `SELECT count(*) FROM types WHERE slug = 'type' AND parent_id IS NULL AND concrete`); got != 1 {
		t.Errorf("sentinel must be a concrete root type")
	}
}

func TestSeedTypeIDsUnchanged(t *testing.T) {
	want := map[string]int64{"entity": 1, "legal_entity": 2, "natural_person": 3, "corporation": 4, "service_account": 5}
	for slug, id := range want {
		if got := queryInt(t, `SELECT id FROM types WHERE slug = $1`, slug); got != id {
			t.Errorf("types.id for %q = %d, want %d", slug, got, id)
		}
	}
}

func TestPlainInsertCreatesAndLinksOneEntity(t *testing.T) {
	before := countSentinelEntities(t)
	totalBefore := queryInt(t, `SELECT count(*) FROM entities`)
	slug := newSlug("plain")
	typeID := insertType(t, slug)

	if got := countSentinelEntities(t) - before; got != 1 {
		t.Errorf("sentinel-typed entities grew by %d, want 1", got)
	}
	if got := queryInt(t, `SELECT count(*) FROM entities`) - totalBefore; got != 1 {
		t.Errorf("total entities grew by %d, want 1", got)
	}
	var fundamental int64
	if err := pool.QueryRow(context.Background(), `
SELECT e.fundamental_type_id FROM types t JOIN entities e ON e.id = t.entity_id WHERE t.id = $1`, typeID).Scan(&fundamental); err != nil {
		t.Fatalf("new type is not linked to an entity: %v", err)
	}
	if fundamental != sentinelID(t) {
		t.Errorf("new type entity fundamental_type_id = %d, want sentinel", fundamental)
	}
}

func TestOnConflictDoNothingCreatesNoEntity(t *testing.T) {
	slug := newSlug("conflict")
	insertType(t, slug)
	before := queryInt(t, `SELECT count(*) FROM entities`)
	tag, err := pool.Exec(context.Background(), `
INSERT INTO types (slug, parent_id, concrete, name)
SELECT $1, id, true, $1 FROM types WHERE slug = 'entity'
ON CONFLICT (slug) DO NOTHING`, slug)
	if err != nil {
		t.Fatalf("on conflict insert: %v", err)
	}
	if tag.RowsAffected() != 0 {
		t.Fatalf("expected the conflicting insert to skip the row, affected %d", tag.RowsAffected())
	}
	if got := queryInt(t, `SELECT count(*) FROM entities`); got != before {
		t.Errorf("entity count changed from %d to %d on a skipped insert", before, got)
	}
	// Same for a pre-existing seed type.
	mustExec(t, `
INSERT INTO types (slug, parent_id, concrete, name)
SELECT 'natural_person', id, true, 'x' FROM types WHERE slug = 'entity'
ON CONFLICT (slug) DO NOTHING`)
	if got := queryInt(t, `SELECT count(*) FROM entities`); got != before {
		t.Errorf("entity count changed from %d to %d on a skipped insert of a seed slug", before, got)
	}
}

func TestInsertWithCallerChosenEntityIDRejected(t *testing.T) {
	// A real, unreferenced-by-this-type entity id: the type entity of 'entity'.
	other := typeEntityID(t, "entity")
	before := queryInt(t, `SELECT count(*) FROM entities`)
	err := execErr(`
INSERT INTO types (slug, parent_id, concrete, name, entity_id)
SELECT $1, id, true, 'x', $2 FROM types WHERE slug = 'entity'`, newSlug("chosen"), other)
	if err == nil {
		t.Fatal("INSERT with a caller-chosen entity_id succeeded; want rejection")
	}
	if got := queryInt(t, `SELECT count(*) FROM entities`); got != before {
		t.Errorf("rejected insert left entities behind (%d -> %d)", before, got)
	}
}

func TestEntityIDUpdateRejected(t *testing.T) {
	slug := newSlug("upd")
	insertType(t, slug)
	other := typeEntityID(t, "entity")
	if err := execErr(`UPDATE types SET entity_id = $1 WHERE slug = $2`, other, slug); err == nil {
		t.Error("UPDATE entity_id to another value succeeded; want rejection")
	}
	if err := execErr(`UPDATE types SET entity_id = NULL WHERE slug = $1`, slug); err == nil {
		t.Error("UPDATE entity_id to NULL succeeded; want rejection")
	}
	// Re-setting the same value is not a change.
	if err := execErr(`UPDATE types SET entity_id = entity_id WHERE slug = $1`, slug); err != nil {
		t.Errorf("no-op entity_id update rejected: %v", err)
	}
}

func TestNullToNonNullRequiresSentinelEntity(t *testing.T) {
	// Directly exercise the relaxed append-only rule's second half: a NULL ->
	// non-NULL transition must reference a sentinel-typed entity. Build the
	// NULL state by disabling the creation trigger inside a rolled-back
	// transaction (ALTER TABLE is transactional in Postgres).
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback of a test transaction
	if _, err := tx.Exec(ctx, `ALTER TABLE types DISABLE TRIGGER types_create_entity`); err != nil {
		t.Fatalf("disable trigger: %v", err)
	}
	var typeID int64
	if err := tx.QueryRow(ctx, `
INSERT INTO types (slug, parent_id, concrete, name)
SELECT $1, id, true, 'x' FROM types WHERE slug = 'entity' RETURNING id`, newSlug("nulltype")).Scan(&typeID); err != nil {
		t.Fatalf("insert: %v", err)
	}
	var plainEntity int64
	if err := tx.QueryRow(ctx, `
INSERT INTO entities (fundamental_type_id) SELECT id FROM types WHERE slug = 'app' RETURNING id`).Scan(&plainEntity); err != nil {
		t.Fatalf("insert plain entity: %v", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE types SET entity_id = $1 WHERE id = $2`, plainEntity, typeID); err == nil {
		t.Error("NULL -> non-sentinel-typed entity succeeded; want rejection")
	}
}

func TestDeprecation(t *testing.T) {
	slug := newSlug("dep")
	insertType(t, slug)
	if err := execErr(`UPDATE types SET deprecated_at = now() WHERE slug = $1`, slug); err != nil {
		t.Errorf("deprecating an ordinary type failed: %v", err)
	}
	if err := execErr(`UPDATE types SET deprecated_at = NULL WHERE slug = $1`, slug); err != nil {
		t.Errorf("un-deprecating an ordinary type failed: %v", err)
	}
	if err := execErr(`UPDATE types SET deprecated_at = now() WHERE slug = 'type'`); err == nil {
		t.Error("deprecating the sentinel succeeded; want rejection")
	}
	if got := queryInt(t, `SELECT count(*) FROM types WHERE slug = 'type' AND deprecated_at IS NOT NULL`); got != 0 {
		t.Error("sentinel is deprecated")
	}
}

func TestStraySentinelEntityRejectedAtCommit(t *testing.T) {
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after a failed commit
	if _, err := tx.Exec(ctx, `INSERT INTO entities (fundamental_type_id) SELECT id FROM types WHERE slug = 'type'`); err != nil {
		return // rejected immediately: also acceptable ("at the latest at commit")
	}
	if err := tx.Commit(ctx); err == nil {
		t.Error("a direct sentinel-typed entity insert committed; want rejection at commit")
	}
}

func TestArchive(t *testing.T) {
	ctx := context.Background()

	var typeEntityUUID string
	if err := pool.QueryRow(ctx, `
SELECT e.uuid::text FROM types t JOIN entities e ON e.id = t.entity_id WHERE t.slug = 'entity'`).Scan(&typeEntityUUID); err != nil {
		t.Fatalf("lookup type entity uuid: %v", err)
	}
	if err := execErr(`SELECT core_archive_entity($1::uuid)`, typeEntityUUID); err == nil {
		t.Error("archiving a type entity succeeded; want rejection")
	}
	if got := queryInt(t, `SELECT count(*) FROM entities WHERE uuid = $1::uuid AND archived_at IS NOT NULL`, typeEntityUUID); got != 0 {
		t.Error("type entity has archived_at set")
	}

	var ordinaryUUID string
	if err := pool.QueryRow(ctx, `
SELECT (core_create_entity((SELECT id FROM types WHERE slug = 'app'))).uuid::text`).Scan(&ordinaryUUID); err != nil {
		t.Fatalf("create ordinary entity: %v", err)
	}
	if err := execErr(`SELECT core_archive_entity($1::uuid)`, ordinaryUUID); err != nil {
		t.Errorf("archiving an ordinary entity failed: %v", err)
	}
	if got := queryInt(t, `SELECT count(*) FROM entities WHERE uuid = $1::uuid AND archived_at IS NOT NULL`, ordinaryUUID); got != 1 {
		t.Error("ordinary entity was not archived")
	}
}

func TestTypeEntityOwnerIsNull(t *testing.T) {
	insertType(t, newSlug("owner"))
	if got := queryInt(t, `
SELECT count(*) FROM entities
WHERE fundamental_type_id = (SELECT id FROM types WHERE slug = 'type') AND owner_id IS NOT NULL`); got != 0 {
		t.Errorf("%d type entities have a non-NULL owner_id", got)
	}
}

// TestInsertTypeUnderManagedModeGrants mirrors app-mfmanager's managed-mode
// module owner grants (SELECT, INSERT on types; USAGE on types_id_seq; SELECT,
// REFERENCES on entities): INSERT INTO types must still create the entity,
// which proves the creation functions run with definer rights.
func TestInsertTypeUnderManagedModeGrants(t *testing.T) {
	ctx := context.Background()
	role := "tg_managed_mod_owner"
	for _, stmt := range []string{
		"DROP OWNED BY " + role,
		"DROP ROLE " + role,
	} {
		_, _ = pool.Exec(ctx, stmt) // best-effort pre-clean of a leftover from a crashed run
	}
	for _, stmt := range []string{
		"CREATE ROLE " + role + " NOLOGIN",
		"GRANT SELECT, INSERT ON types TO " + role,
		"GRANT USAGE ON SEQUENCE types_id_seq TO " + role,
		"GRANT SELECT, REFERENCES ON entities TO " + role,
	} {
		if _, err := pool.Exec(ctx, stmt); err != nil {
			t.Fatalf("setup %q: %v", stmt, err)
		}
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DROP OWNED BY "+role)
		_, _ = pool.Exec(ctx, "DROP ROLE "+role)
	})

	// Sanity: the role really lacks INSERT on entities.
	if got := queryInt(t, `SELECT has_table_privilege($1, 'entities', 'INSERT')::int`, role); got != 0 {
		t.Fatal("test role unexpectedly holds INSERT on entities")
	}

	before := countSentinelEntities(t)
	slug := newSlug("managed")
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after commit
	if _, err := tx.Exec(ctx, "SET LOCAL ROLE "+role); err != nil {
		t.Fatalf("set role: %v", err)
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO types (slug, parent_id, concrete, name)
SELECT $1, id, true, $1 FROM types WHERE slug = 'entity'`, slug); err != nil {
		t.Fatalf("INSERT INTO types under managed-mode grants: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if got := countSentinelEntities(t) - before; got != 1 {
		t.Errorf("sentinel-typed entities grew by %d, want 1", got)
	}
	if got := queryInt(t, `SELECT count(*) FROM types t JOIN entities e ON e.id = t.entity_id WHERE t.slug = $1`, slug); got != 1 {
		t.Error("type inserted under managed-mode grants has no linked entity")
	}
}
