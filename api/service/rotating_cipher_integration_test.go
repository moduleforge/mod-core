//go:build integration

package service_test

// rotating_cipher_integration_test.go proves the plan's headline success
// criterion against a real, ephemeral Postgres database: reading a value
// whose blob carries a non-active key version returns the correct plaintext
// AND leaves the stored blob re-encrypted under the active key, and a second
// read finds it already current. Unit tests with fakes (rotating_cipher_test.go)
// cannot establish this — the compare-and-swap, the separate write-back
// transaction, and the row-level persistence are exactly the parts a fake
// elides.
//
// Covered, against a real database:
//
//   - a standard rotation: write under version 1 through the RotatingCipher
//     (the same encrypt path api/service's Create methods use), rotate
//     directly through the Phase 1 queries (RetireActiveFieldCryptoKey then
//     InsertActiveFieldCryptoKey — the mandatory order), reload, read and
//     assert the plaintext, re-read the raw stored blob and assert its
//     version prefix now decodes to the new active version, then read a
//     second time and assert no further write occurred (natural_persons /
//     corporations updated_at unchanged) — for both natural_persons.ssn and
//     corporations.ein, table-driven, since each column carries its own
//     blobColumn descriptor and its own CAS query;
//   - the grace-window expiry path: a retired key whose decryptable_until has
//     already passed is not loaded, so a read of a blob still carrying that
//     version fails loudly rather than returning empty or wrong plaintext;
//   - the compromised-key branch, the one policy branch whose security value
//     depends on it actually behaving differently from the standard case: a
//     working write handle persists the replacement and the read succeeds; a
//     nil write handle fails the read; and a write-back that genuinely cannot
//     commit (a competing row lock that outlasts the write-back's own
//     SET LOCAL lock_timeout) also fails the read;
//   - the standard-rotation counterpart of that last sub-case: the identical
//     competing-row-lock technique, against a standard (non-compromised)
//     rotation, must NOT fail the read — the tolerate-on-failure branch lets
//     it through with the stored blob left unchanged. This is the one
//     real-SQL pin for compromised_at's false case (the CASE WHEN compromised
//     THEN now() ELSE NULL END in RetireActiveFieldCryptoKey, and the facade
//     adapter's mapping of it) — every unit test exercising this branch
//     builds its fieldcrypto.KeyRecord by hand with CompromisedAt left nil,
//     so none of them actually exercises that SQL or the adapter mapping.
//
// Structural precedent: api/internal/fieldcrypto/generate_integration_test.go
// and api/authz/setup/grant_table_integration_test.go establish the TestMain
// / prerequisite-check / host-resolution / shadow-DB-reset pattern this file
// follows, including their load-bearing adaptations (own-migrations-directory
// resolution via runtime.Caller, and "localhost"-first Postgres host
// resolution verified by a real TCP dial). Unlike either precedent, this
// suite also seeds real entity → legal_entity → natural_person/corporation
// rows (the same three-insert sequence api/authz/setup's seedNaturalPerson /
// seedCorporation helpers use) so the CAS write-back has a genuine row to
// update.
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
//	   go test -tags=integration -p 1 -count=1 -v ./service/...)
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
//	cd mod-core/api && go test -tags=integration -p 1 -v ./service/...
//
// The -p 1 flag matches the sibling suites' convention: TestMain drops and
// recreates the shadow database, so concurrent runs against the same Postgres
// host would conflict.

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
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
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/moduleforge/core-api/fieldcrypto"
	"github.com/moduleforge/core-api/internal/authztest"
	"github.com/moduleforge/core-api/service"
	coredb "github.com/moduleforge/core-model/db"
)

// ---------------------------------------------------------------------------
// Package-level state
// ---------------------------------------------------------------------------

// pool is the connection pool for the core_rotate_on_read_verify_dev shadow
// database. Initialized by TestMain; closed after all tests complete. It also
// doubles as the RotatingCipher write-back handle for every "working write
// handle" test case — the write-back's own short transactions run on the same
// pool a production process would give it.
var pool *pgxpool.Pool

// coreQ wraps pool with mod-core's own sqlc-generated queries — the same
// queries the field crypto key store and the api/service seed paths use in
// production — so both key bootstrap/rotation and entity seeding exercise the
// real query surface rather than a hand-rolled mirror.
var coreQ *coredb.Queries

// coreRotateOnReadVerifyDB is this suite's dedicated shadow database, distinct
// from core_field_key_verify_dev (internal/fieldcrypto), core_grant_verify_dev
// (authz/setup), and any other module's shadow DB on the same shared Postgres
// host.
const coreRotateOnReadVerifyDB = "core_rotate_on_read_verify_dev"

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

// ---------------------------------------------------------------------------
// TestMain
// ---------------------------------------------------------------------------

func TestMain(m *testing.M) {
	if err := checkIntegrationPrerequisites(); err != nil {
		fmt.Fprintf(os.Stderr, "integration: skipping mod-core rotate-on-read tests — %v\n", err)
		os.Exit(0) // exit 0 so `go test` reports skipped, not failed.
	}

	pgHost := resolvePostgresHost()
	if !postgresReachable(pgHost, 2*time.Second) {
		fmt.Fprintf(os.Stderr,
			"integration: skipping mod-core rotate-on-read tests — postgres not reachable at %s:%s\n",
			pgHost, pgPort)
		if os.Getenv("CORE_DEV_PG_HOST") != "" {
			os.Exit(1) // explicitly configured: a silent skip would hide a red run.
		}
		os.Exit(0)
	}

	if err := resetCoreRotateOnReadVerifyDB(pgHost); err != nil {
		fmt.Fprintf(os.Stderr, "integration: shadow DB reset failed: %v\n", err)
		os.Exit(1)
	}

	ctx := context.Background()
	dsn := fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable", pgUser, pgPassword, pgHost, pgPort, coreRotateOnReadVerifyDB)
	p, err := pgxpool.New(ctx, dsn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "integration: open pool: %v\n", err)
		os.Exit(1)
	}
	pool = p
	coreQ = coredb.New(pool)

	// Type-level authorization suite (type_level_authz_integration_test.go):
	// stand-in grant/group tables, then the id-collision fixture.
	if err := authztest.SetupSchema(ctx, pool); err != nil {
		fmt.Fprintf(os.Stderr, "integration: authz stand-in schema: %v\n", err)
		pool.Close()
		os.Exit(1)
	}
	if err := seedTypeLevelFixture(); err != nil {
		fmt.Fprintf(os.Stderr, "integration: type-level authz fixture: %v\n", err)
		pool.Close()
		os.Exit(1)
	}

	code := m.Run()
	pool.Close()
	os.Exit(code)
}

// checkIntegrationPrerequisites verifies that docker and goose are available
// and the shared Postgres container is running.
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
// candidate with an actual TCP dial before returning it.
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
// credentials — only that something is listening.
func postgresReachable(host string, timeout time.Duration) bool {
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, pgPort), timeout)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// resetCoreRotateOnReadVerifyDB drops and recreates core_rotate_on_read_verify_dev,
// then applies mod-core's own migrations (model/migrations, the 1-99 range)
// via goose. Idempotent: safe to re-run on every test binary invocation, and
// what makes running this suite twice in a row against the same Postgres host
// pass both times.
func resetCoreRotateOnReadVerifyDB(pgHost string) error {
	ctx := context.Background()
	adminURL := fmt.Sprintf("postgres://%s:%s@%s:%s/postgres?sslmode=disable", pgUser, pgPassword, pgHost, pgPort)

	conn, err := pgx.Connect(ctx, adminURL)
	if err != nil {
		return fmt.Errorf("connect admin: %w", err)
	}
	defer conn.Close(ctx) //nolint:errcheck // best-effort close on an admin connection we're done with

	for _, stmt := range []string{
		"DROP DATABASE IF EXISTS " + coreRotateOnReadVerifyDB,
		"CREATE DATABASE " + coreRotateOnReadVerifyDB,
	} {
		if _, err := conn.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("%s: %w", stmt, err)
		}
	}

	dsn := fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable", pgUser, pgPassword, pgHost, pgPort, coreRotateOnReadVerifyDB)
	cmd := exec.Command("goose", "-dir", migrationsDir(), "postgres", dsn, "up") //nolint:gosec // fixed args/resolved paths, not user input
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("goose up: %w\n%s", err, out)
	}
	return nil
}

// migrationsDir resolves mod-core's own model/migrations directory relative
// to this source file's location, so the test does not depend on a
// hand-typed absolute path tied to one machine's checkout layout. This file
// lives at <repo>/api/service/rotating_cipher_integration_test.go; migrations
// live at <repo>/model/migrations.
func migrationsDir() string {
	_, thisFile, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(thisFile), "..", "..", "model", "migrations")
}

// ---------------------------------------------------------------------------
// field_crypto_keys helpers
// ---------------------------------------------------------------------------

// unsetFieldKeyEnv clears CORE_FIELD_KEY_HEX for the duration of the test so
// an inherited value cannot interfere with bootstrap against a freshly
// truncated key table.
func unsetFieldKeyEnv(t *testing.T) {
	t.Helper()
	const name = "CORE_FIELD_KEY_HEX"
	prior, had := os.LookupEnv(name)
	if err := os.Unsetenv(name); err != nil {
		t.Fatalf("os.Unsetenv(%q): %v", name, err)
	}
	t.Cleanup(func() {
		if had {
			os.Setenv(name, prior) //nolint:errcheck // best-effort restore in test cleanup
			return
		}
		os.Unsetenv(name) //nolint:errcheck // best-effort restore in test cleanup
	})
}

// resetFieldCryptoKeys empties field_crypto_keys and restarts the identity
// sequence so every test that bootstraps a cipher starts from "nothing
// persisted yet" with a predictable version 1, independent of test order or
// a prior run's leftover state — the invariant Requirement 6 (leave the
// database clean, idempotent re-run) depends on.
func resetFieldCryptoKeys(t *testing.T, ctx context.Context) {
	t.Helper()
	if _, err := pool.Exec(ctx, "TRUNCATE TABLE field_crypto_keys RESTART IDENTITY"); err != nil {
		t.Fatalf("truncate field_crypto_keys: %v", err)
	}
}

// bootstrapCipher resets field_crypto_keys to empty, then constructs a real,
// store-backed Cipher against it exactly as production composition does
// (fieldcrypto.NewFromEnvOrGenerate(ctx, coredb.New(pool))). The freshly
// truncated table guarantees the bootstrapped key is version 1.
func bootstrapCipher(t *testing.T, ctx context.Context) *fieldcrypto.Cipher {
	t.Helper()
	unsetFieldKeyEnv(t)
	resetFieldCryptoKeys(t, ctx)
	c, err := fieldcrypto.NewFromEnvOrGenerate(ctx, coreQ)
	if err != nil {
		t.Fatalf("bootstrapCipher: NewFromEnvOrGenerate: %v", err)
	}
	return c
}

// randomKeyBytes returns 32 bytes of fresh random key material, the shape
// InsertActiveFieldCryptoKey requires.
func randomKeyBytes(t *testing.T) []byte {
	t.Helper()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("generate key material: %v", err)
	}
	return key
}

// rotateFieldCryptoKey performs the rotation transaction the admin endpoint
// will own: RetireActiveFieldCryptoKey then InsertActiveFieldCryptoKey, in
// that mandatory order (plan/notes/key-store-schema-design.md#rotation-transaction),
// in a single transaction. graceDays nil means no expiry (NULL
// decryptable_until); a non-nil value is resolved to an absolute deadline by
// the database clock, exactly as the real rotation endpoint will do it.
func rotateFieldCryptoKey(t *testing.T, ctx context.Context, graceDays *int32, compromised bool) (retiredVersion, newVersion int32) {
	t.Helper()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin rotation: %v", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op once committed

	q := coredb.New(tx)

	var grace pgtype.Int4
	if graceDays != nil {
		grace = pgtype.Int4{Int32: *graceDays, Valid: true}
	}

	retired, err := q.RetireActiveFieldCryptoKey(ctx, coredb.RetireActiveFieldCryptoKeyParams{
		GraceDays:   grace,
		Compromised: compromised,
	})
	if err != nil {
		t.Fatalf("retire active field crypto key: %v", err)
	}
	retiredVersion = retired.Version

	row, err := q.InsertActiveFieldCryptoKey(ctx, randomKeyBytes(t))
	if err != nil {
		t.Fatalf("insert replacement field crypto key: %v", err)
	}
	newVersion = row.Version

	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit rotation: %v", err)
	}
	return retiredVersion, newVersion
}

// ---------------------------------------------------------------------------
// Entity seed and cleanup helpers
// ---------------------------------------------------------------------------

// deleteEntity removes every row a seed helper below may have created for
// entityID, in child-to-parent order (natural_persons/corporations, then
// legal_entities, then entities — legal_entities.entity_id references entities
// ON DELETE RESTRICT). Registered via t.Cleanup so a test leaves the database
// exactly as clean as it found it, which is what makes running this suite
// twice in a row against the same database pass both times.
func deleteEntity(t *testing.T, ctx context.Context, entityID int64) {
	t.Helper()
	for _, stmt := range []string{
		"DELETE FROM natural_persons WHERE entity_id = $1",
		"DELETE FROM corporations WHERE entity_id = $1",
		"DELETE FROM legal_entities WHERE entity_id = $1",
		"DELETE FROM entities WHERE id = $1",
	} {
		if _, err := pool.Exec(ctx, stmt, entityID); err != nil {
			t.Errorf("cleanup %q for entity %d: %v", stmt, entityID, err)
		}
	}
}

// seedNaturalPersonWithSSN inserts entity -> legal_entity -> natural_person,
// storing ssnBlob as the already-encrypted ssn column value (the same shape
// api/service.NaturalPersonService.createNaturalPersonInTx writes), and
// returns the entity's internal ID. Registers cleanup.
func seedNaturalPersonWithSSN(t *testing.T, ctx context.Context, ssnBlob []byte) int64 {
	t.Helper()
	typ, err := coreQ.GetTypeBySlug(ctx, "natural_person")
	if err != nil {
		t.Fatalf("seedNaturalPersonWithSSN: resolve type: %v", err)
	}
	ent, err := coreQ.CreateEntity(ctx, typ.ID)
	if err != nil {
		t.Fatalf("seedNaturalPersonWithSSN: create entity: %v", err)
	}
	t.Cleanup(func() { deleteEntity(t, context.Background(), ent.ID) })

	if _, err := coreQ.CreateLegalEntity(ctx, ent.ID); err != nil {
		t.Fatalf("seedNaturalPersonWithSSN: create legal_entity: %v", err)
	}
	if _, err := coreQ.CreateNaturalPerson(ctx, coredb.CreateNaturalPersonParams{
		EntityID:   ent.ID,
		GivenName:  pgtype.Text{String: "Rotation", Valid: true},
		FamilyName: pgtype.Text{String: "Tester", Valid: true},
		Ssn:        ssnBlob,
	}); err != nil {
		t.Fatalf("seedNaturalPersonWithSSN: create natural_person: %v", err)
	}
	return ent.ID
}

// seedCorporationWithEIN is the corporations.ein equivalent of
// seedNaturalPersonWithSSN.
func seedCorporationWithEIN(t *testing.T, ctx context.Context, einBlob []byte) int64 {
	t.Helper()
	typ, err := coreQ.GetTypeBySlug(ctx, "corporation")
	if err != nil {
		t.Fatalf("seedCorporationWithEIN: resolve type: %v", err)
	}
	ent, err := coreQ.CreateEntity(ctx, typ.ID)
	if err != nil {
		t.Fatalf("seedCorporationWithEIN: create entity: %v", err)
	}
	t.Cleanup(func() { deleteEntity(t, context.Background(), ent.ID) })

	if _, err := coreQ.CreateLegalEntity(ctx, ent.ID); err != nil {
		t.Fatalf("seedCorporationWithEIN: create legal_entity: %v", err)
	}
	if _, err := coreQ.CreateCorporation(ctx, coredb.CreateCorporationParams{
		EntityID:  ent.ID,
		LegalName: "Rotation Test Corp",
		Ein:       einBlob,
	}); err != nil {
		t.Fatalf("seedCorporationWithEIN: create corporation: %v", err)
	}
	return ent.ID
}

// ---------------------------------------------------------------------------
// Raw row readers — bypass the cipher/service layer entirely, so an assertion
// against them proves what is actually on disk rather than what the cipher
// claims is on disk.
// ---------------------------------------------------------------------------

func rawNaturalPersonSSN(t *testing.T, ctx context.Context, entityID int64) []byte {
	t.Helper()
	var b []byte
	if err := pool.QueryRow(ctx, "SELECT ssn FROM natural_persons WHERE entity_id = $1", entityID).Scan(&b); err != nil {
		t.Fatalf("fetch stored ssn for entity %d: %v", entityID, err)
	}
	return b
}

func rawCorporationEIN(t *testing.T, ctx context.Context, entityID int64) []byte {
	t.Helper()
	var b []byte
	if err := pool.QueryRow(ctx, "SELECT ein FROM corporations WHERE entity_id = $1", entityID).Scan(&b); err != nil {
		t.Fatalf("fetch stored ein for entity %d: %v", entityID, err)
	}
	return b
}

func naturalPersonUpdatedAt(t *testing.T, ctx context.Context, entityID int64) time.Time {
	t.Helper()
	var ts time.Time
	if err := pool.QueryRow(ctx, "SELECT updated_at FROM natural_persons WHERE entity_id = $1", entityID).Scan(&ts); err != nil {
		t.Fatalf("fetch natural_persons.updated_at for entity %d: %v", entityID, err)
	}
	return ts
}

func corporationUpdatedAt(t *testing.T, ctx context.Context, entityID int64) time.Time {
	t.Helper()
	var ts time.Time
	if err := pool.QueryRow(ctx, "SELECT updated_at FROM corporations WHERE entity_id = $1", entityID).Scan(&ts); err != nil {
		t.Fatalf("fetch corporations.updated_at for entity %d: %v", entityID, err)
	}
	return ts
}

// ---------------------------------------------------------------------------
// Column descriptor — mirrors api/service/rotating_cipher.go's own
// blobColumn split, so the standard-rotation test below runs identically over
// both encrypted columns rather than hand-copying its body twice.
// ---------------------------------------------------------------------------

type rotationColumn struct {
	name      string
	plaintext string
	seed      func(t *testing.T, ctx context.Context, blob []byte) int64
	decrypt   func(ctx context.Context, rc *service.RotatingCipher, entityID int64, blob []byte) (string, error)
	rawBlob   func(t *testing.T, ctx context.Context, entityID int64) []byte
	updatedAt func(t *testing.T, ctx context.Context, entityID int64) time.Time
}

var rotationColumns = []rotationColumn{
	{
		name:      "natural_persons.ssn",
		plaintext: "123-45-6789",
		seed:      seedNaturalPersonWithSSN,
		decrypt: func(ctx context.Context, rc *service.RotatingCipher, entityID int64, blob []byte) (string, error) {
			return rc.DecryptSSN(ctx, entityID, blob)
		},
		rawBlob:   rawNaturalPersonSSN,
		updatedAt: naturalPersonUpdatedAt,
	},
	{
		name:      "corporations.ein",
		plaintext: "12-3456789",
		seed:      seedCorporationWithEIN,
		decrypt: func(ctx context.Context, rc *service.RotatingCipher, entityID int64, blob []byte) (string, error) {
			return rc.DecryptEIN(ctx, entityID, blob)
		},
		rawBlob:   rawCorporationEIN,
		updatedAt: corporationUpdatedAt,
	},
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestRotateOnRead_StandardRotationPersistsAndIsIdempotent is the plan's
// headline success criterion, over both encrypted columns: a blob written
// under a retired standard key is decrypted correctly on read AND the stored
// blob is genuinely re-encrypted under the active key, and a second read
// leaves the row untouched because there is nothing left to rotate.
func TestRotateOnRead_StandardRotationPersistsAndIsIdempotent(t *testing.T) {
	for _, col := range rotationColumns {
		col := col
		t.Run(col.name, func(t *testing.T) {
			ctx := context.Background()
			cipher := bootstrapCipher(t, ctx)
			rc := service.NewRotatingCipher(cipher, pool, nil)

			blob, err := cipher.Encrypt(ctx, col.plaintext)
			if err != nil {
				t.Fatalf("encrypt %s: %v", col.name, err)
			}
			if v, verr := fieldcrypto.BlobVersion(blob); verr != nil || v != 1 {
				t.Fatalf("bootstrap blob for %s carries version %d (err %v), want 1", col.name, v, verr)
			}

			entityID := col.seed(t, ctx, blob)

			graceDays := int32(30)
			_, newVersion := rotateFieldCryptoKey(t, ctx, &graceDays, false)
			if err := cipher.Reload(ctx); err != nil {
				t.Fatalf("Reload after standard rotation: %v", err)
			}

			// First read: correct plaintext, and the stored blob is genuinely
			// re-encrypted under the new active key — checked straight from the
			// database, not through the cipher.
			got, err := col.decrypt(ctx, rc, entityID, blob)
			if err != nil {
				t.Fatalf("%s: first read: unexpected error: %v", col.name, err)
			}
			if got != col.plaintext {
				t.Errorf("%s: first read plaintext = %q, want %q", col.name, got, col.plaintext)
			}

			stored := col.rawBlob(t, ctx, entityID)
			if bytes.Equal(stored, blob) {
				t.Fatalf("%s: stored blob is unchanged after a rotation that should have persisted a replacement", col.name)
			}
			if v, verr := fieldcrypto.BlobVersion(stored); verr != nil || v != uint32(newVersion) {
				t.Errorf("%s: stored blob carries version %d (err %v), want the new active version %d", col.name, v, verr, newVersion)
			}

			// Second read: plaintext is still correct, and — since the blob is
			// already current — no further write occurs. natural_persons/
			// corporations.updated_at is the row-level witness: it only moves on
			// an actual UPDATE, and the rotate-on-read fast path issues none once
			// nothing needs rotating.
			updatedBefore := col.updatedAt(t, ctx, entityID)
			got2, err := col.decrypt(ctx, rc, entityID, stored)
			if err != nil {
				t.Fatalf("%s: second read: unexpected error: %v", col.name, err)
			}
			if got2 != col.plaintext {
				t.Errorf("%s: second read plaintext = %q, want %q", col.name, got2, col.plaintext)
			}
			updatedAfter := col.updatedAt(t, ctx, entityID)
			if !updatedBefore.Equal(updatedAfter) {
				t.Errorf("%s: second read wrote to the row (updated_at %v -> %v); a blob already under the active key must not be rewritten",
					col.name, updatedBefore, updatedAfter)
			}

			stillStored := col.rawBlob(t, ctx, entityID)
			if !bytes.Equal(stillStored, stored) {
				t.Errorf("%s: second read changed the stored blob bytes even though rotation was not needed", col.name)
			}
		})
	}
}

// TestRotateOnRead_GraceWindowExpiredFailsRead proves the other half of the
// grace-expiry design (plan/notes/key-store-schema-design.md#grace-expiry-semantics):
// once a retired key's decryptable_until has passed, the cipher does not load
// it, so a read of a blob still carrying that version fails loudly rather than
// returning empty or wrong plaintext.
func TestRotateOnRead_GraceWindowExpiredFailsRead(t *testing.T) {
	ctx := context.Background()
	col := rotationColumns[0] // natural_persons.ssn; the branch under test is column-agnostic.
	cipher := bootstrapCipher(t, ctx)

	blob, err := cipher.Encrypt(ctx, col.plaintext)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	entityID := col.seed(t, ctx, blob)

	graceDays := int32(30)
	rotateFieldCryptoKey(t, ctx, &graceDays, false)

	// Back-date the retirement and its window so the deadline has already
	// passed, rather than making the test wait one out — the same technique
	// TestFieldKeyRealExpiredGraceWindowStopsDecrypting uses. Both columns
	// move: field_crypto_keys_grace_after_retirement forbids a deadline
	// earlier than the retirement it belongs to.
	if _, err := pool.Exec(ctx, `
		UPDATE field_crypto_keys
		SET retired_at = now() - INTERVAL '1 hour',
		    decryptable_until = now() - INTERVAL '1 second'
		WHERE version = 1`); err != nil {
		t.Fatalf("expire the grace window: %v", err)
	}

	if err := cipher.Reload(ctx); err != nil {
		t.Fatalf("Reload after expiring the grace window: %v", err)
	}

	rc := service.NewRotatingCipher(cipher, pool, nil)
	got, err := col.decrypt(ctx, rc, entityID, blob)
	if err == nil {
		t.Fatalf("expected the read to fail loudly for a blob under an expired grace-window key; got plaintext %q", got)
	}
	if got != "" {
		t.Errorf("plaintext = %q, want %q on a failed read", got, "")
	}
}

// TestRotateOnRead_CompromisedKeyReadRequiresWorkingWriteHandle is the one
// policy branch whose security value depends on it actually behaving
// differently from the standard case: a blob written under a key retired as
// compromised must only be readable when the replacement can be durably
// persisted.
func TestRotateOnRead_CompromisedKeyReadRequiresWorkingWriteHandle(t *testing.T) {
	ctx := context.Background()
	col := rotationColumns[0] // natural_persons.ssn; the branch under test is column-agnostic.
	cipher := bootstrapCipher(t, ctx)

	// Three independent rows, all encrypted under the same version-1 key,
	// each consumed by exactly one of the three sub-cases below so a prior
	// sub-case's write-back cannot influence a later one.
	blobFor := func() []byte {
		b, err := cipher.Encrypt(ctx, col.plaintext)
		if err != nil {
			t.Fatalf("encrypt: %v", err)
		}
		return b
	}
	blobWorking := blobFor()
	entityWorking := col.seed(t, ctx, blobWorking)
	blobNil := blobFor()
	entityNil := col.seed(t, ctx, blobNil)
	blobBlocked := blobFor()
	entityBlocked := col.seed(t, ctx, blobBlocked)

	// Retire version 1 as compromised: no grace_period_days (NULL
	// decryptable_until), per the schema and API design's recommendation to
	// maximize the window to re-encrypt away from a leaked key.
	rotateFieldCryptoKey(t, ctx, nil, true)
	if err := cipher.Reload(ctx); err != nil {
		t.Fatalf("Reload after compromised rotation: %v", err)
	}

	t.Run("working write handle persists and the read succeeds", func(t *testing.T) {
		rc := service.NewRotatingCipher(cipher, pool, nil)
		got, err := col.decrypt(ctx, rc, entityWorking, blobWorking)
		if err != nil {
			t.Fatalf("unexpected error with a working write handle: %v", err)
		}
		if got != col.plaintext {
			t.Errorf("plaintext = %q, want %q", got, col.plaintext)
		}
		stored := col.rawBlob(t, ctx, entityWorking)
		if bytes.Equal(stored, blobWorking) {
			t.Error("compromised-key write-back did not persist a replacement blob")
		}
		if v, verr := fieldcrypto.BlobVersion(stored); verr != nil || v == 1 {
			t.Errorf("stored blob still carries version %d (err %v); it must move off the compromised version 1", v, verr)
		}
	})

	t.Run("nil write handle fails the read", func(t *testing.T) {
		rc := service.NewRotatingCipher(cipher, nil, nil)
		got, err := col.decrypt(ctx, rc, entityNil, blobNil)
		if err == nil {
			t.Fatal("expected the read to fail: a compromised-key rotation cannot be persisted with no write handle")
		}
		if got != "" {
			t.Errorf("plaintext = %q, want %q on a failed read", got, "")
		}
		stored := col.rawBlob(t, ctx, entityNil)
		if !bytes.Equal(stored, blobNil) {
			t.Error("a failed compromised-key read must not have altered the stored blob")
		}
	})

	t.Run("write-back that cannot commit fails the read", func(t *testing.T) {
		// Hold a competing row lock, in a separate uncommitted transaction, on
		// exactly the row the write-back will try to update. The write-back's
		// own SET LOCAL lock_timeout = '250ms' (rotating_cipher.go) bounds how
		// long it waits on that lock before failing — the same lock-ordering
		// hazard the design note documents, reproduced here for real rather
		// than simulated with a fake querier.
		lockTx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatalf("begin competing lock transaction: %v", err)
		}
		defer func() {
			if rerr := lockTx.Rollback(ctx); rerr != nil && !errors.Is(rerr, pgx.ErrTxClosed) {
				t.Errorf("release competing row lock: %v", rerr)
			}
		}()
		if _, err := lockTx.Exec(ctx, "SELECT ssn FROM natural_persons WHERE entity_id = $1 FOR UPDATE", entityBlocked); err != nil {
			t.Fatalf("acquire competing row lock: %v", err)
		}

		rc := service.NewRotatingCipher(cipher, pool, nil)
		got, err := col.decrypt(ctx, rc, entityBlocked, blobBlocked)
		if err == nil {
			t.Fatal("expected the read to fail when the write-back cannot commit (blocked behind a competing row lock)")
		}
		if got != "" {
			t.Errorf("plaintext = %q, want %q on a failed read", got, "")
		}

		stored := col.rawBlob(t, ctx, entityBlocked)
		if !bytes.Equal(stored, blobBlocked) {
			t.Error("a write-back that could not commit must not have altered the stored blob")
		}
	})
}

// TestRotateOnRead_StandardRotationWriteBackBlockedByLockTolerated pins the
// standard-rotation counterpart of
// TestRotateOnRead_CompromisedKeyReadRequiresWorkingWriteHandle's
// "write-back that cannot commit fails the read" sub-case above: the
// identical competing-row-lock technique, but against a standard
// (non-compromised) rotation, must NOT fail the read. Under a standard
// rotation a write-back that cannot commit is a tolerated miss
// (decryptRotating's default branch in rotating_cipher.go) — the plaintext
// the reader already decrypted is still valid, and the row is left for a
// later read to retry the rotation.
//
// Every rotating_cipher_test.go unit test exercising this tolerate-on-failure
// branch builds its fieldcrypto.KeyRecord by hand with CompromisedAt left
// nil, so none of them ever exercises the real SQL that derives that column
// (compromised_at = CASE WHEN compromised THEN now() ELSE NULL END in
// RetireActiveFieldCryptoKey) or the facade adapter's mapping of it for the
// false case. This test closes that gap against a real database, real
// RetireActiveFieldCryptoKey, and the real facade adapter — mirroring the
// compromised sub-case's fixture and locking technique exactly, so the only
// variable that differs is the compromised flag.
func TestRotateOnRead_StandardRotationWriteBackBlockedByLockTolerated(t *testing.T) {
	ctx := context.Background()
	col := rotationColumns[0] // natural_persons.ssn; the branch under test is column-agnostic.
	cipher := bootstrapCipher(t, ctx)

	blob, err := cipher.Encrypt(ctx, col.plaintext)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	entityID := col.seed(t, ctx, blob)

	// Standard rotation: compromised=false, so rot.MustPersist will be false
	// and a lost write-back is a tolerated miss rather than a failed read —
	// the opposite of the compromised sub-case this mirrors.
	graceDays := int32(30)
	rotateFieldCryptoKey(t, ctx, &graceDays, false)
	if err := cipher.Reload(ctx); err != nil {
		t.Fatalf("Reload after standard rotation: %v", err)
	}

	// Hold a competing row lock, in a separate uncommitted transaction, on
	// exactly the row the write-back will try to update. The write-back's own
	// SET LOCAL lock_timeout = '250ms' (rotating_cipher.go) bounds how long it
	// waits on that lock before failing — the same technique the compromised
	// suite's blocked-lock sub-case above uses, reproduced here against a
	// standard-rotated key.
	lockTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin competing lock transaction: %v", err)
	}
	defer func() {
		if rerr := lockTx.Rollback(ctx); rerr != nil && !errors.Is(rerr, pgx.ErrTxClosed) {
			t.Errorf("release competing row lock: %v", rerr)
		}
	}()
	if _, err := lockTx.Exec(ctx, "SELECT ssn FROM natural_persons WHERE entity_id = $1 FOR UPDATE", entityID); err != nil {
		t.Fatalf("acquire competing row lock: %v", err)
	}

	rc := service.NewRotatingCipher(cipher, pool, nil)
	got, err := col.decrypt(ctx, rc, entityID, blob)
	if err != nil {
		t.Fatalf("standard rotation read must tolerate a write-back that cannot commit, not fail: %v", err)
	}
	if got != col.plaintext {
		t.Errorf("plaintext = %q, want %q", got, col.plaintext)
	}

	// The competing lock is still held at this point (released only by the
	// deferred rollback above, after this test function returns), so the
	// write-back's compare-and-swap cannot have committed — the stored blob
	// must be exactly what was written before the rotation.
	stored := col.rawBlob(t, ctx, entityID)
	if !bytes.Equal(stored, blob) {
		t.Error("a write-back blocked by a competing lock must not have altered the stored blob")
	}
}
