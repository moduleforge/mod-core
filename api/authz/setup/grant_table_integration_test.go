//go:build integration

package setup_test

// grant_table_integration_test.go exercises the REAL GrantTableGenerator (via
// setup.ApplyFuncs, not a hand-copied SQL mirror) against a live, ephemeral
// Postgres seeded with real rows, and proves — by executing the generated
// accessible_<slug>_ids_for_actor SQL functions and inspecting actual query
// results — correct row-level access for the six resource slugs mod-core can
// exercise without depending on any downstream module's schema:
// natural_person, corporation, service_account, legal_entity,
// authz_actor_group, authz_target_group. ("tag" and "task", which depend on
// mod-tags/mod-tasks schema, are covered by sibling integration-verification
// tasks in this phase.)
//
// Structural precedent: mod-authz/api/service/grants_integration_test.go,
// mod-audit/api/service/integration_main_test.go, and
// mod-users/api/internal/authz/authz_integration_test.go establish the
// TestMain / prerequisite-check / DB-reset / seed-helper pattern this file
// follows. Two load-bearing adaptations from that precedent:
//
//  1. Migrations directory. The precedent tests hard-code an absolute,
//     machine-specific, cross-repo composed-schema path (see the
//     integrationMigrationsDir / migrationsDir constants in those files)
//     that does not exist in this environment. This file instead resolves
//     mod-core's OWN migrations directory (model/migrations, mod-core's
//     1-99 range) relative to this source file's location via
//     runtime.Caller, so it works from any checkout.
//  2. Postgres host resolution. The precedent tests resolve the
//     users-module-postgres container's Docker-network IP (falling back to a
//     hard-coded sandbox IP), on the theory that the test binary runs as a
//     network peer of the container and "localhost" resolves to the test
//     process's own loopback. Empirically, in the environment this task was
//     implemented in (Docker Desktop for macOS), the reverse holds: the
//     container's internal Docker-network IP is NOT reachable from the host
//     network namespace, but the container's published port IS reachable via
//     "localhost". resolvePostgresHost therefore verifies reachability with
//     an actual TCP dial and prefers "localhost", falling back to the
//     docker-inspect IP and then the historical hard-coded IP for
//     environments shaped like the precedent tests'. CORE_DEV_PG_HOST always
//     wins when set.
//
// Local grants / actor-group / target-group schema: GrantTableGenerator's
// shared CTE (sharedCTEPrefix in grant_table.go) references the grants,
// authz_actor_group_members, and authz_target_group_members tables by name.
// Those tables are owned by mod-authz (migrations 0501-0505), not mod-core,
// and mod-authz is out of scope for this plan (see plan/overview.md scope
// boundaries); mod-core's own migration range (1-99) does not define them
// either. In the real composed application, mod-authz's migrations create
// these tables before any Authorizer implementation calls setup.ApplyFuncs.
// setupLocalAuthzSchema below creates minimal local stand-ins so the
// generated access functions execute correctly against a database built from
// only mod-core's own migrations.
//
// Run with (preferred: a throwaway Postgres container with a unique name and
// a random host port; safe on a shared Docker host). From the repo root:
//
//	name="mod-core-integ-$(openssl rand -hex 4)"
//	docker run -d --rm --name "$name" -e POSTGRES_USER=users -e POSTGRES_PASSWORD=users \
//	  -e POSTGRES_DB=postgres -p 127.0.0.1::5432 postgres:16
//	port="$(docker port "$name" 5432/tcp | head -1 | sed 's/.*://')"
//	until docker exec "$name" pg_isready -U users >/dev/null 2>&1; do sleep 1; done
//	(cd api && CORE_DEV_PG_HOST=127.0.0.1 CORE_DEV_PG_PORT="$port" \
//	   go test -tags=integration -p 1 -count=1 -v ./authz/setup/...)
//	docker rm -f "$name"
//
// CORE_DEV_PG_HOST and CORE_DEV_PG_PORT (default 5432) select the Postgres
// endpoint. When CORE_DEV_PG_HOST is set the users-module-postgres container
// check is skipped and an unreachable Postgres FAILS the run (exit 1) rather
// than skipping it.
//
// Legacy alternative (NOT for use on a shared Docker host): with no env vars
// set, the suite targets the shared users-module-postgres container on port
// 5432 and silently skips (exit 0) if it is not running.
//
//	cd mod-core/api && go test -tags=integration -p 1 -v ./authz/setup/...
//
// The -p 1 flag matches the sibling suites' convention: TestMain drops and
// recreates the shadow database, so concurrent runs against the same Postgres
// host would conflict.

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/moduleforge/core-api/authz/setup"
	coredb "github.com/moduleforge/core-model/db"
)

// ---------------------------------------------------------------------------
// Package-level state
// ---------------------------------------------------------------------------

// pool is the connection pool for the core_grant_verify_dev shadow database.
// Initialized by TestMain; closed after all tests complete.
var pool *pgxpool.Pool

// coreQ wraps pool with mod-core's own sqlc-generated queries (CreateEntity,
// CreateLegalEntity, CreateNaturalPerson, CreateCorporation,
// CreateServiceAccount, GetTypeBySlug) — the same queries mod-core's own
// service layer uses, so seed data is created exactly the way production
// code creates it. In particular, every CreateEntity call here exercises the
// real entities_owner_self_default trigger from migration 0013.
var coreQ *coredb.Queries

// coreGrantVerifyDB is the dedicated shadow database for this suite, distinct
// from the shadow databases other modules' integration tests use on the same
// shared Postgres host (authz_dev, audit_dev, ...).
const coreGrantVerifyDB = "core_grant_verify_dev"

// pgContainerName is the shared Postgres container reused by every
// integration test suite in this environment; this suite does not start its
// own container.
const pgContainerName = "users-module-postgres"

// pgPort is the Postgres TCP port: CORE_DEV_PG_PORT when set, else 5432.
// Resolved once at package init and used for every DSN and dial.
var pgPort = envOrDefault("CORE_DEV_PG_PORT", "5432")

// envOrDefault returns the value of env var key, or def when unset or empty.
func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

const (
	pgUser     = "users"
	pgPassword = "users"
)

// testOpID is an arbitrary stand-in authz_operations.id. This suite's local
// grants table (see setupLocalAuthzSchema) has no authz_operations FK — the
// generated access functions only ever compare g.operation_id = ANY(p_op_ids)
// as plain integers — so a single fixed value exercises every arm without
// wiring the full operation registry.
const testOpID int32 = 1

// ---------------------------------------------------------------------------
// TestMain
// ---------------------------------------------------------------------------

func TestMain(m *testing.M) {
	if err := checkIntegrationPrerequisites(); err != nil {
		fmt.Fprintf(os.Stderr, "integration: skipping mod-core grant-table tests — %v\n", err)
		os.Exit(0) // exit 0 so `go test` reports skipped, not failed.
	}

	pgHost := resolvePostgresHost()
	if !postgresReachable(pgHost, 2*time.Second) {
		fmt.Fprintf(os.Stderr,
			"integration: skipping mod-core grant-table tests — postgres not reachable at %s:%s\n",
			pgHost, pgPort)
		if os.Getenv("CORE_DEV_PG_HOST") != "" {
			os.Exit(1) // explicitly configured: a silent skip would hide a red run.
		}
		os.Exit(0)
	}

	if err := resetCoreGrantVerifyDB(pgHost); err != nil {
		fmt.Fprintf(os.Stderr, "integration: shadow DB reset failed: %v\n", err)
		os.Exit(1)
	}

	ctx := context.Background()
	dsn := fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable", pgUser, pgPassword, pgHost, pgPort, coreGrantVerifyDB)
	p, err := pgxpool.New(ctx, dsn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "integration: open pool: %v\n", err)
		os.Exit(1)
	}
	pool = p
	coreQ = coredb.New(pool)

	if err := setupLocalAuthzSchema(ctx, pool); err != nil {
		fmt.Fprintf(os.Stderr, "integration: local authz schema setup: %v\n", err)
		pool.Close()
		os.Exit(1)
	}

	slugs := []string{
		"natural_person", "corporation", "service_account",
		"legal_entity", "authz_actor_group", "authz_target_group",
	}
	if err := setup.ApplyFuncs(ctx, pool, setup.NewGrantTableGenerator(), slugs); err != nil {
		fmt.Fprintf(os.Stderr, "integration: apply grant functions: %v\n", err)
		pool.Close()
		os.Exit(1)
	}

	code := m.Run()
	pool.Close()
	os.Exit(code)
}

// checkIntegrationPrerequisites verifies that docker and goose are available
// and the shared Postgres container is running. Matches the structural
// precedent's prerequisite check.
func checkIntegrationPrerequisites() error {
	// An explicit CORE_DEV_PG_HOST targets a caller-managed (e.g. throwaway)
	// Postgres, so the shared-container check does not apply.
	if os.Getenv("CORE_DEV_PG_HOST") == "" {
		cmd := exec.Command("docker", "inspect", "--format={{.State.Running}}", pgContainerName)
		out, err := cmd.Output()
		if err != nil {
			return fmt.Errorf("docker inspect: %w", err)
		}
		if strings.TrimSpace(string(out)) != "true" {
			return fmt.Errorf("container %s is not running", pgContainerName)
		}
	}
	if _, err := exec.LookPath("goose"); err != nil {
		return fmt.Errorf("goose not in PATH: %w", err)
	}
	return nil
}

// resolvePostgresHost returns the host to use for connecting to the shared
// Postgres container. CORE_DEV_PG_HOST always wins when set. Otherwise it
// tries, in order, "localhost" (published-port forwarding), the container's
// Docker-network IP (docker inspect), and finally the historical hard-coded
// sandbox IP used by the structural precedent tests — verifying each
// candidate with an actual TCP dial before returning it, so this function
// returns a host TestMain can actually reach rather than assuming the
// precedent's network topology (see the file-level doc comment).
func resolvePostgresHost() string {
	if h := os.Getenv("CORE_DEV_PG_HOST"); h != "" {
		return h
	}

	const dialTimeout = 500 * time.Millisecond
	if postgresReachable("localhost", dialTimeout) {
		return "localhost"
	}

	cmd := exec.Command("docker", "inspect",
		"--format", "{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}", pgContainerName)
	if out, err := cmd.Output(); err == nil {
		if ip := strings.TrimSpace(string(out)); ip != "" && postgresReachable(ip, dialTimeout) {
			return ip
		}
	}

	// Historical fallback IP from the structural precedent tests, for
	// environments shaped like theirs (test binary as a Docker-network peer).
	return "172.23.0.3"
}

// postgresReachable reports whether a TCP connection to host:pgPort succeeds
// within timeout. It does not verify the Postgres wire protocol or
// credentials — only that something is listening — which is enough to
// distinguish "wrong host" from a genuine connection failure surfaced later
// by pgx.
func postgresReachable(host string, timeout time.Duration) bool {
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, pgPort), timeout)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// resetCoreGrantVerifyDB drops and recreates core_grant_verify_dev, then
// applies mod-core's own migrations (model/migrations, the 1-99 range) via
// goose. Idempotent: safe to re-run on every test binary invocation.
func resetCoreGrantVerifyDB(pgHost string) error {
	ctx := context.Background()
	adminURL := fmt.Sprintf("postgres://%s:%s@%s:%s/postgres?sslmode=disable", pgUser, pgPassword, pgHost, pgPort)

	conn, err := pgx.Connect(ctx, adminURL)
	if err != nil {
		return fmt.Errorf("connect admin: %w", err)
	}
	defer conn.Close(ctx) //nolint:errcheck // best-effort close on an admin connection we're done with

	for _, stmt := range []string{
		"DROP DATABASE IF EXISTS " + coreGrantVerifyDB,
		"CREATE DATABASE " + coreGrantVerifyDB,
	} {
		if _, err := conn.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("%s: %w", stmt, err)
		}
	}

	dsn := fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable", pgUser, pgPassword, pgHost, pgPort, coreGrantVerifyDB)
	cmd := exec.Command("goose", "-dir", migrationsDir(), "postgres", dsn, "up") //nolint:gosec // fixed args/resolved paths, not user input
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("goose up: %w\n%s", err, out)
	}
	return nil
}

// migrationsDir resolves mod-core's own model/migrations directory (the 1-99
// range) relative to this source file's location, so the test does not
// depend on a hand-typed absolute path tied to one machine's checkout
// layout. This file lives at <repo>/api/authz/setup/grant_table_integration_test.go;
// migrations live at <repo>/model/migrations.
func migrationsDir() string {
	_, thisFile, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "model", "migrations")
}

// ---------------------------------------------------------------------------
// Local authz schema (grants / actor-group / target-group stand-ins)
// ---------------------------------------------------------------------------

// setupLocalAuthzSchema creates minimal local stand-ins for the three tables
// GrantTableGenerator's shared CTE references (grants,
// authz_actor_group_members, authz_target_group_members) — owned by
// mod-authz in the real system, out of scope for this plan, and absent from
// mod-core's own 1-99 migration range — and registers the two group types
// (authz_actor_group, authz_target_group) that mod-authz's migrations
// register in the real system. Both are locally seeded here, rather than by
// applying mod-authz's migrations, because touching mod-authz is out of this
// plan's scope (see plan/overview.md scope boundaries); the generic access
// function only needs entities + type_is_or_descends_from, so no CTI subtype
// table (authz_actor_groups / authz_target_groups) is required for either
// type.
func setupLocalAuthzSchema(ctx context.Context, pool *pgxpool.Pool) error {
	const schemaSQL = `
CREATE TABLE grants (
    id           BIGSERIAL PRIMARY KEY,
    actor_id     BIGINT NOT NULL REFERENCES entities(id),
    operation_id INT    NOT NULL,
    target_id    BIGINT REFERENCES entities(id)
);

CREATE TABLE authz_actor_group_members (
    group_id  BIGINT NOT NULL REFERENCES entities(id),
    member_id BIGINT NOT NULL REFERENCES entities(id),
    PRIMARY KEY (group_id, member_id)
);

CREATE TABLE authz_target_group_members (
    group_id  BIGINT NOT NULL REFERENCES entities(id),
    member_id BIGINT NOT NULL REFERENCES entities(id),
    PRIMARY KEY (group_id, member_id)
);
`
	if _, err := pool.Exec(ctx, schemaSQL); err != nil {
		return fmt.Errorf("create local authz tables: %w", err)
	}

	const seedGroupType = `
INSERT INTO types (slug, parent_id, concrete, name, description)
SELECT $1, id, true, $2,
       'Locally seeded for integration tests: registered by mod-authz migrations in the real system (out of this plan''s scope).'
FROM types WHERE slug = 'entity'
ON CONFLICT (slug) DO NOTHING`
	if _, err := pool.Exec(ctx, seedGroupType, "authz_actor_group", "Authorization Actor Group"); err != nil {
		return fmt.Errorf("seed authz_actor_group type: %w", err)
	}
	if _, err := pool.Exec(ctx, seedGroupType, "authz_target_group", "Authorization Target Group"); err != nil {
		return fmt.Errorf("seed authz_target_group type: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Entity seed helpers
// ---------------------------------------------------------------------------

// seedNaturalPerson inserts entity -> legal_entity -> natural_person and
// returns the entity's internal ID. The owns-itself trigger
// (entities_owner_self_default, migration 0013) sets owner_id = id
// automatically on the entities INSERT.
func seedNaturalPerson(t *testing.T, ctx context.Context) int64 {
	t.Helper()
	typ, err := coreQ.GetTypeBySlug(ctx, "natural_person")
	if err != nil {
		t.Fatalf("seedNaturalPerson: resolve type: %v", err)
	}
	ent, err := coreQ.CreateEntity(ctx, typ.ID)
	if err != nil {
		t.Fatalf("seedNaturalPerson: create entity: %v", err)
	}
	if _, err := coreQ.CreateLegalEntity(ctx, ent.ID); err != nil {
		t.Fatalf("seedNaturalPerson: create legal_entity: %v", err)
	}
	if _, err := coreQ.CreateNaturalPerson(ctx, coredb.CreateNaturalPersonParams{EntityID: ent.ID}); err != nil {
		t.Fatalf("seedNaturalPerson: create natural_person: %v", err)
	}
	return ent.ID
}

// seedCorporation inserts entity -> legal_entity -> corporation and returns
// the entity's internal ID. No creation path ever sets owner_id for
// corporations; it stays NULL.
func seedCorporation(t *testing.T, ctx context.Context, legalName string) int64 {
	t.Helper()
	typ, err := coreQ.GetTypeBySlug(ctx, "corporation")
	if err != nil {
		t.Fatalf("seedCorporation: resolve type: %v", err)
	}
	ent, err := coreQ.CreateEntity(ctx, typ.ID)
	if err != nil {
		t.Fatalf("seedCorporation: create entity: %v", err)
	}
	if _, err := coreQ.CreateLegalEntity(ctx, ent.ID); err != nil {
		t.Fatalf("seedCorporation: create legal_entity: %v", err)
	}
	if _, err := coreQ.CreateCorporation(ctx, coredb.CreateCorporationParams{EntityID: ent.ID, LegalName: legalName}); err != nil {
		t.Fatalf("seedCorporation: create corporation: %v", err)
	}
	return ent.ID
}

// seedServiceAccount inserts entity -> service_account and returns the
// entity's internal ID. Like natural_person, the owns-itself trigger sets
// owner_id = id automatically.
func seedServiceAccount(t *testing.T, ctx context.Context, label string) int64 {
	t.Helper()
	typ, err := coreQ.GetTypeBySlug(ctx, "service_account")
	if err != nil {
		t.Fatalf("seedServiceAccount: resolve type: %v", err)
	}
	ent, err := coreQ.CreateEntity(ctx, typ.ID)
	if err != nil {
		t.Fatalf("seedServiceAccount: create entity: %v", err)
	}
	if _, err := coreQ.CreateServiceAccount(ctx, coredb.CreateServiceAccountParams{EntityID: ent.ID, Label: label}); err != nil {
		t.Fatalf("seedServiceAccount: create service_account: %v", err)
	}
	return ent.ID
}

// seedGroupEntity inserts a bare entities row of the given (locally seeded)
// group type slug — authz_actor_group or authz_target_group — with no
// subtype table. This matches the real system, where these are full Entity
// rows (backed by authz_actor_groups / authz_target_groups CTI tables), but
// the generic access function only needs entities + type_is_or_descends_from.
func seedGroupEntity(t *testing.T, ctx context.Context, typeSlug string) int64 {
	t.Helper()
	typ, err := coreQ.GetTypeBySlug(ctx, typeSlug)
	if err != nil {
		t.Fatalf("seedGroupEntity(%s): resolve type: %v", typeSlug, err)
	}
	ent, err := coreQ.CreateEntity(ctx, typ.ID)
	if err != nil {
		t.Fatalf("seedGroupEntity(%s): create entity: %v", typeSlug, err)
	}
	return ent.ID
}

// ---------------------------------------------------------------------------
// Grant seed helpers
// ---------------------------------------------------------------------------

// seedWildcardGrant inserts a wildcard grant (target_id IS NULL) for the
// given actor entity and testOpID.
func seedWildcardGrant(t *testing.T, ctx context.Context, actorEntityID int64) {
	t.Helper()
	const q = `INSERT INTO grants (actor_id, operation_id, target_id) VALUES ($1, $2, NULL)`
	if _, err := pool.Exec(ctx, q, actorEntityID, testOpID); err != nil {
		t.Fatalf("seedWildcardGrant: %v", err)
	}
}

// seedTargetChainGrant wires an actor -> actor-group -> grant -> target-group
// -> target chain: actorEntityID is added as a member of a fresh
// authz_actor_group, targetEntityID is added as a member of a fresh
// authz_target_group, and a grant links the two groups for testOpID. Returns
// the two group entity IDs (useful for debugging; most callers ignore them).
func seedTargetChainGrant(t *testing.T, ctx context.Context, actorEntityID, targetEntityID int64) (actorGroupID, targetGroupID int64) {
	t.Helper()
	actorGroupID = seedGroupEntity(t, ctx, "authz_actor_group")
	targetGroupID = seedGroupEntity(t, ctx, "authz_target_group")

	const addActor = `INSERT INTO authz_actor_group_members (group_id, member_id) VALUES ($1, $2)`
	if _, err := pool.Exec(ctx, addActor, actorGroupID, actorEntityID); err != nil {
		t.Fatalf("seedTargetChainGrant: add actor to group: %v", err)
	}

	const addTarget = `INSERT INTO authz_target_group_members (group_id, member_id) VALUES ($1, $2)`
	if _, err := pool.Exec(ctx, addTarget, targetGroupID, targetEntityID); err != nil {
		t.Fatalf("seedTargetChainGrant: add target to group: %v", err)
	}

	const grantSQL = `INSERT INTO grants (actor_id, operation_id, target_id) VALUES ($1, $2, $3)`
	if _, err := pool.Exec(ctx, grantSQL, actorGroupID, testOpID, targetGroupID); err != nil {
		t.Fatalf("seedTargetChainGrant: insert grant: %v", err)
	}
	return actorGroupID, targetGroupID
}

// ---------------------------------------------------------------------------
// Access-function query helper
// ---------------------------------------------------------------------------

// accessibleIDs executes accessible_<slug>_ids_for_actor(actorEntityID,
// {testOpID}) and returns the resulting entity IDs. slug is always one of the
// small fixed set of literals this file passes to setup.ApplyFuncs in
// TestMain — never caller/user-supplied input — so interpolating it into the
// function-name portion of the SQL text (identifiers cannot be bound
// parameters) does not admit injection; the actor ID and operation IDs are
// bound parameters.
func accessibleIDs(t *testing.T, ctx context.Context, slug string, actorEntityID int64) []int64 {
	t.Helper()
	q := fmt.Sprintf(`SELECT entity_id FROM accessible_%s_ids_for_actor($1, $2::int[])`, slug)
	rows, err := pool.Query(ctx, q, actorEntityID, []int32{testOpID})
	if err != nil {
		t.Fatalf("accessibleIDs(%s): query: %v", slug, err)
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("accessibleIDs(%s): scan: %v", slug, err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("accessibleIDs(%s): rows: %v", slug, err)
	}
	return ids
}

// containsID reports whether ids contains want.
func containsID(ids []int64, want int64) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Tests — one per slug, plus the cross-type isolation proof
// ---------------------------------------------------------------------------

func TestGrantVerification_NaturalPerson(t *testing.T) {
	ctx := context.Background()

	actorA := seedNaturalPerson(t, ctx)
	actorB := seedNaturalPerson(t, ctx)

	// Own arm: the owns-itself trigger sets owner_id = id, so actorA sees
	// exactly their own row, not actorB's.
	ownIDs := accessibleIDs(t, ctx, "natural_person", actorA)
	if !containsID(ownIDs, actorA) {
		t.Errorf("own arm: expected actorA (%d) to see itself; got %v", actorA, ownIDs)
	}
	if containsID(ownIDs, actorB) {
		t.Errorf("own arm: actorA must not see actorB's (%d) row; got %v", actorB, ownIDs)
	}

	// Wildcard arm: a wildcard-grant holder sees every seeded natural_person
	// row, including ones seeded above.
	wildcardActor := seedNaturalPerson(t, ctx)
	seedWildcardGrant(t, ctx, wildcardActor)
	wildcardIDs := accessibleIDs(t, ctx, "natural_person", wildcardActor)
	for _, want := range []int64{actorA, actorB, wildcardActor} {
		if !containsID(wildcardIDs, want) {
			t.Errorf("wildcard arm: expected to see entity %d; got %v", want, wildcardIDs)
		}
	}

	// Target-chain arm: a non-owning, non-wildcard actor granted access via
	// an actor-group -> target-group -> entity chain sees exactly the
	// granted row (plus their own, via the unrelated own-arm) and not an
	// ungranted control row.
	chainActor := seedNaturalPerson(t, ctx)
	grantedTarget := seedNaturalPerson(t, ctx)
	ungrantedControl := seedNaturalPerson(t, ctx)
	seedTargetChainGrant(t, ctx, chainActor, grantedTarget)
	chainIDs := accessibleIDs(t, ctx, "natural_person", chainActor)
	if !containsID(chainIDs, grantedTarget) {
		t.Errorf("target-chain arm: expected to see granted target %d; got %v", grantedTarget, chainIDs)
	}
	if containsID(chainIDs, ungrantedControl) {
		t.Errorf("target-chain arm: must not see ungranted control %d; got %v", ungrantedControl, chainIDs)
	}
}

func TestGrantVerification_ServiceAccount(t *testing.T) {
	ctx := context.Background()

	actorA := seedServiceAccount(t, ctx, "Service A")
	actorB := seedServiceAccount(t, ctx, "Service B")

	// Own arm: identical owns-itself semantics as natural_person.
	ownIDs := accessibleIDs(t, ctx, "service_account", actorA)
	if !containsID(ownIDs, actorA) {
		t.Errorf("own arm: expected actorA (%d) to see itself; got %v", actorA, ownIDs)
	}
	if containsID(ownIDs, actorB) {
		t.Errorf("own arm: actorA must not see actorB's (%d) row; got %v", actorB, ownIDs)
	}

	// Wildcard arm.
	wildcardActor := seedNaturalPerson(t, ctx)
	seedWildcardGrant(t, ctx, wildcardActor)
	wildcardIDs := accessibleIDs(t, ctx, "service_account", wildcardActor)
	for _, want := range []int64{actorA, actorB} {
		if !containsID(wildcardIDs, want) {
			t.Errorf("wildcard arm: expected to see entity %d; got %v", want, wildcardIDs)
		}
	}

	// Target-chain arm.
	chainActor := seedNaturalPerson(t, ctx)
	grantedTarget := seedServiceAccount(t, ctx, "Granted Service")
	ungrantedControl := seedServiceAccount(t, ctx, "Ungranted Service")
	seedTargetChainGrant(t, ctx, chainActor, grantedTarget)
	chainIDs := accessibleIDs(t, ctx, "service_account", chainActor)
	if !containsID(chainIDs, grantedTarget) {
		t.Errorf("target-chain arm: expected to see granted target %d; got %v", grantedTarget, chainIDs)
	}
	if containsID(chainIDs, ungrantedControl) {
		t.Errorf("target-chain arm: must not see ungranted control %d; got %v", ungrantedControl, chainIDs)
	}
}

func TestGrantVerification_Corporation(t *testing.T) {
	ctx := context.Background()

	corpA := seedCorporation(t, ctx, "Corp A")
	requester := seedNaturalPerson(t, ctx) // any actor entity; corp ownership is never set

	// Own arm invariant: no creation path ever sets a corporation's owner_id
	// (the owns-itself trigger's predicate only matches natural_person /
	// service_account), so it stays NULL. A plain, non-wildcard, non-granted
	// actor must see NO corporation rows — this is now a data-layer
	// invariant, verified here against a live query result rather than by
	// SQL-text inspection (see plan/notes/generic-generator-collapse.md).
	noAccessIDs := accessibleIDs(t, ctx, "corporation", requester)
	if containsID(noAccessIDs, corpA) {
		t.Errorf("own arm: non-owning actor must not see corporation %d; got %v", corpA, noAccessIDs)
	}

	// Wildcard arm.
	wildcardActor := seedNaturalPerson(t, ctx)
	seedWildcardGrant(t, ctx, wildcardActor)
	wildcardIDs := accessibleIDs(t, ctx, "corporation", wildcardActor)
	if !containsID(wildcardIDs, corpA) {
		t.Errorf("wildcard arm: expected to see corporation %d; got %v", corpA, wildcardIDs)
	}

	// Target-chain arm.
	chainActor := seedNaturalPerson(t, ctx)
	grantedCorp := seedCorporation(t, ctx, "Granted Corp")
	ungrantedCorp := seedCorporation(t, ctx, "Ungranted Corp")
	seedTargetChainGrant(t, ctx, chainActor, grantedCorp)
	chainIDs := accessibleIDs(t, ctx, "corporation", chainActor)
	if !containsID(chainIDs, grantedCorp) {
		t.Errorf("target-chain arm: expected to see granted corporation %d; got %v", grantedCorp, chainIDs)
	}
	if containsID(chainIDs, ungrantedCorp) {
		t.Errorf("target-chain arm: must not see ungranted corporation %d; got %v", ungrantedCorp, chainIDs)
	}
}

func TestGrantVerification_LegalEntity(t *testing.T) {
	ctx := context.Background()

	// Own arm: legal_entity's own-arm is the union of natural_person (owns
	// itself) and corporation (never owned) rows, scoped by
	// type_is_or_descends_from(..., 'legal_entity'). actorNP owns itself;
	// corpA is never owned by anyone; otherNP is a different actor's row.
	actorNP := seedNaturalPerson(t, ctx)
	otherNP := seedNaturalPerson(t, ctx)
	corpA := seedCorporation(t, ctx, "LE Corp A")

	ownIDs := accessibleIDs(t, ctx, "legal_entity", actorNP)
	if !containsID(ownIDs, actorNP) {
		t.Errorf("own arm: expected actor to see its own natural_person row %d; got %v", actorNP, ownIDs)
	}
	if containsID(ownIDs, otherNP) {
		t.Errorf("own arm: must not see other actor's natural_person row %d; got %v", otherNP, ownIDs)
	}
	if containsID(ownIDs, corpA) {
		t.Errorf("own arm: must not see unowned corporation %d; got %v", corpA, ownIDs)
	}

	// Wildcard arm: sees both natural_person and corporation rows under
	// legal_entity — proves the dropped sub-type delegation is behaviorally
	// identical to the union the old delegation produced.
	wildcardActor := seedNaturalPerson(t, ctx)
	seedWildcardGrant(t, ctx, wildcardActor)
	wildcardIDs := accessibleIDs(t, ctx, "legal_entity", wildcardActor)
	for _, want := range []int64{actorNP, otherNP, corpA} {
		if !containsID(wildcardIDs, want) {
			t.Errorf("wildcard arm: expected to see legal_entity row %d; got %v", want, wildcardIDs)
		}
	}

	// Target-chain arm, exercised against a corporation target — proves the
	// chain works across concrete sub-types via the single generic
	// legal_entity predicate (no per-subtype delegation).
	chainActor := seedNaturalPerson(t, ctx)
	grantedCorp := seedCorporation(t, ctx, "LE Granted Corp")
	ungrantedCorp := seedCorporation(t, ctx, "LE Ungranted Corp")
	seedTargetChainGrant(t, ctx, chainActor, grantedCorp)
	chainIDs := accessibleIDs(t, ctx, "legal_entity", chainActor)
	if !containsID(chainIDs, grantedCorp) {
		t.Errorf("target-chain arm: expected to see granted corporation %d; got %v", grantedCorp, chainIDs)
	}
	if containsID(chainIDs, ungrantedCorp) {
		t.Errorf("target-chain arm: must not see ungranted corporation %d; got %v", ungrantedCorp, chainIDs)
	}
}

func TestGrantVerification_AuthzActorGroup(t *testing.T) {
	ctx := context.Background()

	groupA := seedGroupEntity(t, ctx, "authz_actor_group")
	requester := seedNaturalPerson(t, ctx)

	// Own arm invariant: authz_actor_group is an administrative object; no
	// creation path ever populates its owner_id, so it stays NULL. A plain,
	// non-wildcard, non-granted actor must see no authz_actor_group rows.
	noAccessIDs := accessibleIDs(t, ctx, "authz_actor_group", requester)
	if containsID(noAccessIDs, groupA) {
		t.Errorf("own arm: non-owning actor must not see authz_actor_group %d; got %v", groupA, noAccessIDs)
	}

	// Wildcard arm.
	wildcardActor := seedNaturalPerson(t, ctx)
	seedWildcardGrant(t, ctx, wildcardActor)
	wildcardIDs := accessibleIDs(t, ctx, "authz_actor_group", wildcardActor)
	if !containsID(wildcardIDs, groupA) {
		t.Errorf("wildcard arm: expected to see authz_actor_group %d; got %v", groupA, wildcardIDs)
	}

	// Target-chain arm.
	chainActor := seedNaturalPerson(t, ctx)
	grantedGroup := seedGroupEntity(t, ctx, "authz_actor_group")
	ungrantedGroup := seedGroupEntity(t, ctx, "authz_actor_group")
	seedTargetChainGrant(t, ctx, chainActor, grantedGroup)
	chainIDs := accessibleIDs(t, ctx, "authz_actor_group", chainActor)
	if !containsID(chainIDs, grantedGroup) {
		t.Errorf("target-chain arm: expected to see granted authz_actor_group %d; got %v", grantedGroup, chainIDs)
	}
	if containsID(chainIDs, ungrantedGroup) {
		t.Errorf("target-chain arm: must not see ungranted authz_actor_group %d; got %v", ungrantedGroup, chainIDs)
	}
}

func TestGrantVerification_AuthzTargetGroup(t *testing.T) {
	ctx := context.Background()

	groupA := seedGroupEntity(t, ctx, "authz_target_group")
	requester := seedNaturalPerson(t, ctx)

	// Own arm invariant: same "no own access" reasoning as authz_actor_group.
	noAccessIDs := accessibleIDs(t, ctx, "authz_target_group", requester)
	if containsID(noAccessIDs, groupA) {
		t.Errorf("own arm: non-owning actor must not see authz_target_group %d; got %v", groupA, noAccessIDs)
	}

	// Wildcard arm.
	wildcardActor := seedNaturalPerson(t, ctx)
	seedWildcardGrant(t, ctx, wildcardActor)
	wildcardIDs := accessibleIDs(t, ctx, "authz_target_group", wildcardActor)
	if !containsID(wildcardIDs, groupA) {
		t.Errorf("wildcard arm: expected to see authz_target_group %d; got %v", groupA, wildcardIDs)
	}

	// Target-chain arm.
	chainActor := seedNaturalPerson(t, ctx)
	grantedGroup := seedGroupEntity(t, ctx, "authz_target_group")
	ungrantedGroup := seedGroupEntity(t, ctx, "authz_target_group")
	seedTargetChainGrant(t, ctx, chainActor, grantedGroup)
	chainIDs := accessibleIDs(t, ctx, "authz_target_group", chainActor)
	if !containsID(chainIDs, grantedGroup) {
		t.Errorf("target-chain arm: expected to see granted authz_target_group %d; got %v", grantedGroup, chainIDs)
	}
	if containsID(chainIDs, ungrantedGroup) {
		t.Errorf("target-chain arm: must not see ungranted authz_target_group %d; got %v", ungrantedGroup, chainIDs)
	}
}

// TestGrantVerification_CrossTypeIsolation is the one non-obvious correctness
// requirement flagged in plan/notes/generic-generator-collapse.md: the
// type_is_or_descends_from(...) predicate must appear on EVERY arm, including
// the own-arm. Without it, an actor's own-arm match on one type could leak
// into another type's access function purely because they share the same
// owner_id column. This is the assertion that would fail if that predicate
// were missing from the own arm.
func TestGrantVerification_CrossTypeIsolation(t *testing.T) {
	ctx := context.Background()

	actorNP := seedNaturalPerson(t, ctx) // owns itself
	corpEntity := seedCorporation(t, ctx, "Isolation Corp")

	corpIDs := accessibleIDs(t, ctx, "corporation", actorNP)
	if containsID(corpIDs, actorNP) {
		t.Errorf("cross-type isolation: actor's own natural_person entity %d leaked into corporation access; got %v", actorNP, corpIDs)
	}

	npIDs := accessibleIDs(t, ctx, "natural_person", actorNP)
	if !containsID(npIDs, actorNP) {
		t.Errorf("cross-type isolation: expected actor to see its own natural_person row %d; got %v", actorNP, npIDs)
	}
	if containsID(npIDs, corpEntity) {
		t.Errorf("cross-type isolation: corporation entity %d leaked into natural_person access; got %v", corpEntity, npIDs)
	}
}
