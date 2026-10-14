//go:build integration

package fieldcrypto_test

// generate_integration_test.go exercises the multi-key cipher against a real,
// ephemeral Postgres database carrying the replacement field_crypto_keys
// schema (model/migrations/0001_baseline.sql), proving the properties
// only a real database can demonstrate:
//
//   - concurrent first-boot callers converge on exactly one persisted key, now
//     arbitrated by the field_crypto_keys_one_active partial unique index
//     rather than the id = 1 singleton it replaced;
//   - a rotation performed by one process is picked up by another through
//     Reload, with the retired key still decrypting its old blobs and the new
//     active key taking over encryption;
//   - a retired key past decryptable_until stops being loaded at all;
//   - the octet_length(key_bytes) = 32 CHECK and the one-active unique index
//     genuinely reject the rows the application must never write.
//
// The KeyStore below is hand-written over a raw pgx pool rather than built on
// the generated sqlc model package. That is deliberate and load-bearing:
// api/internal/fieldcrypto must stay free of any generated-model dependency,
// tests included, which is what lets the api/fieldcrypto façade own that
// import and keeps the module manifest's cipher service block unchanged. The
// SQL carries the same filter and the same two guards as
// model/queries/field_crypto_keys.sql's ListUsableFieldCryptoKeys and
// InsertInitialFieldCryptoKey.
//
// Structural precedent: api/authz/setup/grant_table_integration_test.go
// establishes the TestMain / prerequisite-check / host-resolution /
// shadow-DB-reset pattern this file follows, including its own load-bearing
// adaptations (own-migrations-directory resolution via runtime.Caller, and
// "localhost"-first Postgres host resolution verified by a real TCP dial).
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
//	   go test -tags=integration -p 1 -count=1 -v ./internal/fieldcrypto/...)
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
//	cd mod-core/api && go test -tags=integration -p 1 -v ./internal/fieldcrypto/...
//
// The -p 1 flag matches the sibling suites' convention: TestMain drops and
// recreates the shadow database, so concurrent runs against the same Postgres
// host would conflict.

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/moduleforge/core-api/internal/fieldcrypto"
)

// ---------------------------------------------------------------------------
// Package-level state
// ---------------------------------------------------------------------------

// pool is the connection pool for the core_field_key_verify_dev shadow
// database. Initialized by TestMain; closed after all tests complete.
var pool *pgxpool.Pool

// coreFieldKeyVerifyDB is this suite's dedicated shadow database, distinct
// from core_grant_verify_dev (authz/setup) and any other module's shadow DB
// on the same shared Postgres host.
const coreFieldKeyVerifyDB = "core_field_key_verify_dev"

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
		fmt.Fprintf(os.Stderr, "integration: skipping mod-core field-key tests — %v\n", err)
		os.Exit(0) // exit 0 so `go test` reports skipped, not failed.
	}

	pgHost := resolvePostgresHost()
	if !postgresReachable(pgHost, 2*time.Second) {
		fmt.Fprintf(os.Stderr,
			"integration: skipping mod-core field-key tests — postgres not reachable at %s:%s\n",
			pgHost, pgPort)
		if os.Getenv("CORE_DEV_PG_HOST") != "" {
			os.Exit(1) // explicitly configured: a silent skip would hide a red run.
		}
		os.Exit(0)
	}

	if err := resetCoreFieldKeyVerifyDB(pgHost); err != nil {
		fmt.Fprintf(os.Stderr, "integration: shadow DB reset failed: %v\n", err)
		os.Exit(1)
	}

	ctx := context.Background()
	dsn := fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable", pgUser, pgPassword, pgHost, pgPort, coreFieldKeyVerifyDB)
	p, err := pgxpool.New(ctx, dsn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "integration: open pool: %v\n", err)
		os.Exit(1)
	}
	pool = p

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

// resetCoreFieldKeyVerifyDB drops and recreates core_field_key_verify_dev,
// then applies mod-core's own migrations (model/migrations, the 1-99 range)
// via goose. Idempotent: safe to re-run on every test binary invocation.
func resetCoreFieldKeyVerifyDB(pgHost string) error {
	ctx := context.Background()
	adminURL := fmt.Sprintf("postgres://%s:%s@%s:%s/postgres?sslmode=disable", pgUser, pgPassword, pgHost, pgPort)

	conn, err := pgx.Connect(ctx, adminURL)
	if err != nil {
		return fmt.Errorf("connect admin: %w", err)
	}
	defer conn.Close(ctx) //nolint:errcheck // best-effort close on an admin connection we're done with

	for _, stmt := range []string{
		"DROP DATABASE IF EXISTS " + coreFieldKeyVerifyDB,
		"CREATE DATABASE " + coreFieldKeyVerifyDB,
	} {
		if _, err := conn.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("%s: %w", stmt, err)
		}
	}

	dsn := fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable", pgUser, pgPassword, pgHost, pgPort, coreFieldKeyVerifyDB)
	cmd := exec.Command("goose", "-dir", migrationsDir(), "postgres", dsn, "up") //nolint:gosec // fixed args/resolved paths, not user input
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("goose up: %w\n%s", err, out)
	}
	return nil
}

// migrationsDir resolves mod-core's own model/migrations directory relative
// to this source file's location, so the test does not depend on a
// hand-typed absolute path tied to one machine's checkout layout.
func migrationsDir() string {
	_, thisFile, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "model", "migrations")
}

// ---------------------------------------------------------------------------
// A KeyStore over raw SQL
// ---------------------------------------------------------------------------

// pgQuerier is satisfied by both *pgxpool.Pool and pgx.Tx.
type pgQuerier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// pgKeyStore implements fieldcrypto.KeyStore over raw SQL — hand-written
// rather than generated, for the reason the file-level comment gives.
type pgKeyStore struct{ db pgQuerier }

// listUsableSQL carries the same filter as ListUsableFieldCryptoKeys: the
// active key plus every retired key still inside its grace window.
const listUsableSQL = `
SELECT version, key_bytes, retired_at, decryptable_until, compromised_at
FROM field_crypto_keys
WHERE retired_at IS NULL OR decryptable_until IS NULL OR decryptable_until > now()
ORDER BY version`

// insertInitialSQL carries the same two guards as
// InsertInitialFieldCryptoKey. The WHERE NOT EXISTS covers a committed
// non-empty table; the untargeted ON CONFLICT DO NOTHING covers the
// concurrent first-boot race against the one-active partial unique index.
// Either way, no rows back means someone else established the key material.
const insertInitialSQL = `
INSERT INTO field_crypto_keys (key_bytes)
SELECT $1::BYTEA
WHERE NOT EXISTS (SELECT 1 FROM field_crypto_keys)
ON CONFLICT DO NOTHING
RETURNING version, key_bytes, retired_at, decryptable_until, compromised_at`

func (s pgKeyStore) LoadUsableKeys(ctx context.Context) ([]fieldcrypto.KeyRecord, error) {
	rows, err := s.db.Query(ctx, listUsableSQL)
	if err != nil {
		return nil, fmt.Errorf("list usable field crypto keys: %w", err)
	}
	defer rows.Close()

	var records []fieldcrypto.KeyRecord
	for rows.Next() {
		rec, err := scanKeyRecord(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list usable field crypto keys: %w", err)
	}
	return records, nil
}

func (s pgKeyStore) InsertInitialKey(ctx context.Context, keyBytes []byte) (fieldcrypto.KeyRecord, error) {
	// pgx surfaces a zero-row :one query as pgx.ErrNoRows, which is exactly
	// the signal the cipher's lost-race branch keys on.
	return scanKeyRecord(s.db.QueryRow(ctx, insertInitialSQL, keyBytes))
}

// rowScanner is the common shape of pgx.Row and pgx.Rows.
type rowScanner interface{ Scan(dest ...any) error }

func scanKeyRecord(row rowScanner) (fieldcrypto.KeyRecord, error) {
	var (
		version int32
		rec     fieldcrypto.KeyRecord
	)
	if err := row.Scan(&version, &rec.KeyBytes, &rec.RetiredAt, &rec.DecryptableUntil, &rec.CompromisedAt); err != nil {
		return fieldcrypto.KeyRecord{}, err
	}
	if version <= 0 {
		return fieldcrypto.KeyRecord{}, fmt.Errorf("corrupt field_crypto_keys row: version %d", version)
	}
	rec.Version = uint32(version)
	return rec, nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// resetKeys empties field_crypto_keys and restarts the identity sequence so
// each test starts from a known "nothing persisted yet" state with predictable
// version numbers, independent of test order.
func resetKeys(t *testing.T, ctx context.Context) {
	t.Helper()
	if _, err := pool.Exec(ctx, "TRUNCATE TABLE field_crypto_keys RESTART IDENTITY"); err != nil {
		t.Fatalf("truncate field_crypto_keys: %v", err)
	}
}

// rotate performs the rotation transaction the admin endpoint will own:
// retire the active key (resolving its grace deadline and compromise flag
// against the database clock), then insert the replacement — in that mandatory
// order, since the one-active partial unique index is checked immediately.
func rotate(t *testing.T, ctx context.Context, newKey []byte, graceDays *int32, compromised bool) {
	t.Helper()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin rotation: %v", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op once committed

	var retiredVersion int32
	err = tx.QueryRow(ctx, `
		UPDATE field_crypto_keys
		SET retired_at = now(),
		    decryptable_until = CASE WHEN $1::INT IS NULL THEN NULL ELSE now() + $1::INT * INTERVAL '1 day' END,
		    compromised_at = CASE WHEN $2::BOOLEAN THEN now() ELSE NULL END
		WHERE retired_at IS NULL
		RETURNING version`, graceDays, compromised).Scan(&retiredVersion)
	if err != nil {
		t.Fatalf("retire active key: %v", err)
	}

	var newVersion int32
	if err := tx.QueryRow(ctx,
		`INSERT INTO field_crypto_keys (key_bytes) VALUES ($1::BYTEA) RETURNING version`,
		newKey).Scan(&newVersion); err != nil {
		t.Fatalf("insert replacement key: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit rotation: %v", err)
	}
}

func countKeys(t *testing.T, ctx context.Context) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM field_crypto_keys").Scan(&n); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	return n
}

func activeKeyBytes(t *testing.T, ctx context.Context) []byte {
	t.Helper()
	var key []byte
	if err := pool.QueryRow(ctx, "SELECT key_bytes FROM field_crypto_keys WHERE retired_at IS NULL").Scan(&key); err != nil {
		t.Fatalf("fetch active key_bytes: %v", err)
	}
	return key
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestFieldKeyRealBootstrapRoundTrip proves the round trip through the real
// table: a first call against an empty table generates and persists version 1,
// and a second call against the same database adopts that persisted key rather
// than generating another.
func TestFieldKeyRealBootstrapRoundTrip(t *testing.T) {
	ctx := context.Background()
	unsetEnv(t, "CORE_FIELD_KEY_HEX")
	resetKeys(t, ctx)

	store := pgKeyStore{db: pool}

	first, err := fieldcrypto.NewFromEnvOrGenerate(ctx, store)
	if err != nil {
		t.Fatalf("first NewFromEnvOrGenerate: %v", err)
	}
	second, err := fieldcrypto.NewFromEnvOrGenerate(ctx, store)
	if err != nil {
		t.Fatalf("second NewFromEnvOrGenerate: %v", err)
	}

	const plaintext = "round-trip-through-real-table"
	blob, err := first.Encrypt(ctx, plaintext)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if got := blobVersionOf(t, blob); got != 1 {
		t.Errorf("bootstrapped blob carries version %d, want 1", got)
	}
	got, err := second.Decrypt(ctx, blob)
	if err != nil {
		t.Fatalf("second.Decrypt(first's blob): %v (the second call did not adopt the persisted key)", err)
	}
	if got != plaintext {
		t.Errorf("second.Decrypt(first's blob) = %q, want %q", got, plaintext)
	}

	if n := countKeys(t, ctx); n != 1 {
		t.Errorf("field_crypto_keys row count = %d, want 1", n)
	}
}

// TestFieldKeyRealConcurrentRace is the test that exercises the DB-level
// convergence guarantee under real concurrency: N goroutines call
// NewFromEnvOrGenerate against an empty table. Every goroutine must succeed,
// exactly one row must exist afterward, and every returned Cipher must have
// converged on that row's key. The arbiter is now the
// field_crypto_keys_one_active partial unique index rather than the id = 1
// singleton it replaced.
func TestFieldKeyRealConcurrentRace(t *testing.T) {
	ctx := context.Background()
	unsetEnv(t, "CORE_FIELD_KEY_HEX")
	resetKeys(t, ctx)

	const goroutines = 8
	ciphers := make([]*fieldcrypto.Cipher, goroutines)

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(i int) {
			defer wg.Done()
			c, err := fieldcrypto.NewFromEnvOrGenerate(ctx, pgKeyStore{db: pool})
			if err != nil {
				t.Errorf("goroutine %d: NewFromEnvOrGenerate: %v", i, err)
				return
			}
			ciphers[i] = c
		}(i)
	}
	wg.Wait()

	for i, c := range ciphers {
		if c == nil {
			t.Fatalf("goroutine %d did not produce a Cipher (see earlier error)", i)
		}
	}
	if n := countKeys(t, ctx); n != 1 {
		t.Fatalf("field_crypto_keys row count after the concurrent race = %d, want exactly 1", n)
	}

	// Cross-check every goroutine's Cipher against the one persisted row.
	persisted := activeKeyBytes(t, ctx)
	for i, c := range ciphers {
		plaintext := fmt.Sprintf("probe-from-goroutine-%d", i)
		blob, err := c.Encrypt(ctx, plaintext)
		if err != nil {
			t.Fatalf("goroutine %d cipher Encrypt: %v", i, err)
		}
		got, err := openUnder(t, persisted, blob)
		if err != nil {
			t.Errorf("goroutine %d: its Cipher did not converge on the winning key: %v", i, err)
			continue
		}
		if got != plaintext {
			t.Errorf("goroutine %d: blob decrypts to %q, want %q", i, got, plaintext)
		}
	}
}

// TestFieldKeyRealRotationPickedUpByReload is the multi-key property the whole
// plan exists for: a rotation committed by one actor is picked up by an
// already-running cipher through Reload, the retired key keeps decrypting its
// old blobs, and new writes move to the replacement version.
func TestFieldKeyRealRotationPickedUpByReload(t *testing.T) {
	ctx := context.Background()
	unsetEnv(t, "CORE_FIELD_KEY_HEX")
	resetKeys(t, ctx)

	c, err := fieldcrypto.NewFromEnvOrGenerate(ctx, pgKeyStore{db: pool})
	if err != nil {
		t.Fatalf("NewFromEnvOrGenerate: %v", err)
	}
	// Long TTL and rate limit: only the explicit Reload may refresh the set.
	fieldcrypto.SetReloadTuningForTest(c, time.Hour, time.Hour)

	const plaintext = "written-before-the-rotation"
	oldBlob, err := c.Encrypt(ctx, plaintext)
	if err != nil {
		t.Fatalf("Encrypt before rotation: %v", err)
	}

	graceDays := int32(30)
	rotate(t, ctx, testKey(21), &graceDays, false)

	if err := c.Reload(ctx); err != nil {
		t.Fatalf("Reload after rotation: %v", err)
	}

	newBlob, err := c.Encrypt(ctx, plaintext)
	if err != nil {
		t.Fatalf("Encrypt after rotation: %v", err)
	}
	if blobVersionOf(t, newBlob) != 2 {
		t.Errorf("post-rotation Encrypt used version %d, want 2", blobVersionOf(t, newBlob))
	}

	got, rot, err := c.DecryptWithRotation(ctx, oldBlob)
	if err != nil {
		t.Fatalf("DecryptWithRotation of a pre-rotation blob: %v", err)
	}
	if got != plaintext {
		t.Errorf("plaintext = %q, want %q", got, plaintext)
	}
	if !rot.Needed() || rot.FromVersion != 1 || rot.ToVersion != 2 {
		t.Errorf("Rotation = %+v, want a needed 1→2 rotation", rot)
	}
	if rot.MustPersist {
		t.Error("MustPersist is true after a standard rotation")
	}
}

// TestFieldKeyRealCompromisedRotationMustPersist proves compromised_at reaches
// the caller as policy rather than as a column: a blob written under a key
// retired as compromised comes back with MustPersist set.
func TestFieldKeyRealCompromisedRotationMustPersist(t *testing.T) {
	ctx := context.Background()
	unsetEnv(t, "CORE_FIELD_KEY_HEX")
	resetKeys(t, ctx)

	c, err := fieldcrypto.NewFromEnvOrGenerate(ctx, pgKeyStore{db: pool})
	if err != nil {
		t.Fatalf("NewFromEnvOrGenerate: %v", err)
	}
	fieldcrypto.SetReloadTuningForTest(c, time.Hour, time.Hour)

	const plaintext = "written-under-a-leaked-key"
	oldBlob, err := c.Encrypt(ctx, plaintext)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	// A compromised rotation leaves decryptable_until NULL: maximum
	// opportunity to re-encrypt away from the leaked key.
	rotate(t, ctx, testKey(22), nil, true)

	if err := c.Reload(ctx); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	got, rot, err := c.DecryptWithRotation(ctx, oldBlob)
	if err != nil {
		t.Fatalf("DecryptWithRotation: %v", err)
	}
	if got != plaintext {
		t.Errorf("plaintext = %q, want %q", got, plaintext)
	}
	if !rot.MustPersist {
		t.Error("MustPersist is false for a blob written under a key retired as compromised")
	}
}

// TestFieldKeyRealExpiredGraceWindowStopsDecrypting proves the SQL load filter
// half of the grace-expiry design: once decryptable_until is in the past the
// key is not loaded at all, so a blob still carrying its version fails loudly.
func TestFieldKeyRealExpiredGraceWindowStopsDecrypting(t *testing.T) {
	ctx := context.Background()
	unsetEnv(t, "CORE_FIELD_KEY_HEX")
	resetKeys(t, ctx)

	c, err := fieldcrypto.NewFromEnvOrGenerate(ctx, pgKeyStore{db: pool})
	if err != nil {
		t.Fatalf("NewFromEnvOrGenerate: %v", err)
	}

	oldBlob, err := c.Encrypt(ctx, "written-before-the-window-closed")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	graceDays := int32(30)
	rotate(t, ctx, testKey(23), &graceDays, false)
	// Back-date the retirement and its window so the deadline has already
	// passed, rather than making the test wait one out. Both columns move:
	// field_crypto_keys_grace_after_retirement forbids a deadline earlier
	// than the retirement it belongs to.
	if _, err := pool.Exec(ctx, `
		UPDATE field_crypto_keys
		SET retired_at = now() - INTERVAL '1 hour',
		    decryptable_until = now() - INTERVAL '1 second'
		WHERE version = 1`); err != nil {
		t.Fatalf("expire the grace window: %v", err)
	}

	fresh, err := fieldcrypto.NewFromEnvOrGenerate(ctx, pgKeyStore{db: pool})
	if err != nil {
		t.Fatalf("NewFromEnvOrGenerate after expiry: %v", err)
	}
	fieldcrypto.SetReloadTuningForTest(fresh, time.Hour, time.Hour)

	if got, err := fresh.Decrypt(ctx, oldBlob); err == nil {
		t.Errorf("Decrypt of a blob under an expired key = %q, want an error", got)
	}
}

// TestFieldKeyRealSchemaGuards proves the schema-level defenses reject rows
// the application must never write, independent of the Go-level checks: a key
// that is not 32 bytes, and a second active key.
func TestFieldKeyRealSchemaGuards(t *testing.T) {
	ctx := context.Background()
	resetKeys(t, ctx)

	t.Run("key length CHECK", func(t *testing.T) {
		shortKey := make([]byte, 16) // one CHECK (octet_length = 32) short
		_, err := pool.Exec(ctx, "INSERT INTO field_crypto_keys (key_bytes) VALUES ($1)", shortKey)
		if err == nil {
			t.Fatal("expected the octet_length(key_bytes) = 32 CHECK to reject a 16-byte key")
		}
		assertSQLState(t, err, "23514") // check_violation
		if n := countKeys(t, ctx); n != 0 {
			t.Errorf("rejected insert left %d rows behind", n)
		}
	})

	t.Run("one-active unique index", func(t *testing.T) {
		resetKeys(t, ctx)
		if _, err := pool.Exec(ctx, "INSERT INTO field_crypto_keys (key_bytes) VALUES ($1)", testKey(31)); err != nil {
			t.Fatalf("insert the first active key: %v", err)
		}
		_, err := pool.Exec(ctx, "INSERT INTO field_crypto_keys (key_bytes) VALUES ($1)", testKey(32))
		if err == nil {
			t.Fatal("expected field_crypto_keys_one_active to reject a second active key")
		}
		assertSQLState(t, err, "23505") // unique_violation
		if n := countKeys(t, ctx); n != 1 {
			t.Errorf("row count = %d, want 1", n)
		}
	})
}

func assertSQLState(t *testing.T, err error, want string) {
	t.Helper()
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		t.Fatalf("expected a *pgconn.PgError, got: %v", err)
	}
	if pgErr.Code != want {
		t.Errorf("expected SQLSTATE %s, got %s: %v", want, pgErr.Code, err)
	}
}
