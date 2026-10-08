//go:build integration

package httpapi_test

// field_crypto_keys_integration_test.go proves, against a real, ephemeral
// Postgres database, the concurrency claim the rotation endpoint's design
// rests on (plan/notes/key-store-schema-design.md#rotation-transaction):
// mod-core deliberately does not take a pg_advisory_xact_lock around
// rotation, relying instead on the field_crypto_keys_one_active partial
// unique index and ordinary row-lock re-evaluation to arbitrate two
// concurrent rotations. Unit tests with a fake querier (field_crypto_keys_test.go)
// cannot establish this — a fake has no row locks and no transaction
// isolation to race against; only a real database serializes two concurrent
// UPDATEs against the same row and lets the loser observe the winner's
// committed state.
//
// Covered, against a real database and a real *httptest.ResponseRecorder
// dispatch through FieldCryptoKeyHandler:
//
//   - the headline case: two goroutines fire POST /field-crypto-keys/rotations
//     at the same instant (synchronized via a shared channel, not run in
//     sequence) — exactly one 201, one 409, never two 201s, never two 409s,
//     never a 500; afterward exactly one active key exists and the table
//     grew by exactly one row (the loser's INSERT never ran);
//   - the sequential happy path the concurrency case is contrasted against:
//     a single rotation returns 201, retires the previously-active version
//     with the expected decryptable_until, and creates a new active version;
//   - the unique-key_bytes rejection: rotating with an explicit key_hex equal
//     to a key already on file returns 409, not 500;
//   - the compromised rotation's effect on the retired row (compromised_at
//     stamped, decryptable_until left NULL), and that the same request
//     combined with grace_period_days is rejected 400 before any transaction
//     runs.
//
// Structural precedent: api/authz/setup/grant_table_integration_test.go,
// api/internal/fieldcrypto/generate_integration_test.go, and
// api/service/rotating_cipher_integration_test.go establish the TestMain /
// prerequisite-check / host-resolution / shadow-DB-reset pattern this file
// follows, including their load-bearing adaptations (own-migrations-directory
// resolution via runtime.Caller, and "localhost"-first Postgres host
// resolution verified by a real TCP dial — empirically wrong on the machine
// this task was implemented on, where "localhost" reaches an unrelated
// host-native Postgres with no matching role; CORE_DEV_PG_HOST always wins
// when set, and always needs to be set in that environment). Unlike any of
// the three, this suite drives the real HTTP handler
// (httpapi.FieldCryptoKeyHandler) end to end rather than the query layer or
// a service directly, since the property under test — two concurrent HTTP
// requests resolving to one 201 and one 409 — lives in the handler's
// transaction, not below it.
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
//	   go test -tags=integration -p 1 -count=1 -v ./httpapi/...)
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
//	cd mod-core/api && go test -tags=integration -p 1 -v ./httpapi/...
//
// The -p 1 flag matches the sibling suites' convention: TestMain drops and
// recreates the shadow database, so concurrent runs against the same Postgres
// host would conflict.

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/moduleforge/core-api/apiresp"
	"github.com/moduleforge/core-api/authz"
	"github.com/moduleforge/core-api/httpapi"
	"github.com/moduleforge/core-api/internal/authztest"
	"github.com/moduleforge/core-api/observer"
	"github.com/moduleforge/core-api/opctx"
	coredb "github.com/moduleforge/core-model/db"
)

// ---------------------------------------------------------------------------
// Package-level state
// ---------------------------------------------------------------------------

// pool is the connection pool for the core_field_crypto_rotation_verify_dev
// shadow database. Initialized by TestMain; closed after all tests complete.
// It also doubles as the FieldCryptoKeyHandler's txhelper.DB: *pgxpool.Pool
// satisfies that interface directly via its own BeginTx method, exactly as a
// production composition root would hand it in.
var pool *pgxpool.Pool

// coreQ wraps pool with mod-core's own sqlc-generated queries — the same
// coredb.Querier a production FieldCryptoKeyHandler is constructed with.
var coreQ *coredb.Queries

// coreFieldCryptoRotationVerifyDB is this suite's dedicated shadow database,
// distinct from core_grant_verify_dev (authz/setup), core_field_key_verify_dev
// (internal/fieldcrypto), core_rotate_on_read_verify_dev (service), and any
// other module's shadow DB on the same shared Postgres host.
const coreFieldCryptoRotationVerifyDB = "core_field_crypto_rotation_verify_dev"

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
		fmt.Fprintf(os.Stderr, "integration: skipping mod-core field-crypto rotation tests — %v\n", err)
		os.Exit(0) // exit 0 so `go test` reports skipped, not failed.
	}

	pgHost := resolvePostgresHost()
	if !postgresReachable(pgHost, 2*time.Second) {
		fmt.Fprintf(os.Stderr,
			"integration: skipping mod-core field-crypto rotation tests — postgres not reachable at %s:%s\n",
			pgHost, pgPort)
		if os.Getenv("CORE_DEV_PG_HOST") != "" {
			os.Exit(1) // explicitly configured: a silent skip would hide a red run.
		}
		os.Exit(0)
	}

	if err := resetCoreFieldCryptoRotationVerifyDB(pgHost); err != nil {
		fmt.Fprintf(os.Stderr, "integration: shadow DB reset failed: %v\n", err)
		os.Exit(1)
	}

	ctx := context.Background()
	dsn := fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable", pgUser, pgPassword, pgHost, pgPort, coreFieldCryptoRotationVerifyDB)
	p, err := pgxpool.New(ctx, dsn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "integration: open pool: %v\n", err)
		os.Exit(1)
	}
	pool = p
	coreQ = coredb.New(pool)

	// Type-level authorization suite (apps_type_level_authz_integration_test.go):
	// stand-in grant/group tables, then the deterministic ownership fixture.
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

// resetCoreFieldCryptoRotationVerifyDB drops and recreates
// core_field_crypto_rotation_verify_dev, then applies mod-core's own
// migrations (model/migrations, the 1-99 range) via goose. Idempotent: safe
// to re-run on every test binary invocation, and what makes running this
// suite twice in a row against the same Postgres host pass both times.
func resetCoreFieldCryptoRotationVerifyDB(pgHost string) error {
	ctx := context.Background()
	adminURL := fmt.Sprintf("postgres://%s:%s@%s:%s/postgres?sslmode=disable", pgUser, pgPassword, pgHost, pgPort)

	conn, err := pgx.Connect(ctx, adminURL)
	if err != nil {
		return fmt.Errorf("connect admin: %w", err)
	}
	defer conn.Close(ctx) //nolint:errcheck // best-effort close on an admin connection we're done with

	for _, stmt := range []string{
		"DROP DATABASE IF EXISTS " + coreFieldCryptoRotationVerifyDB,
		"CREATE DATABASE " + coreFieldCryptoRotationVerifyDB,
	} {
		if _, err := conn.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("%s: %w", stmt, err)
		}
	}

	dsn := fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable", pgUser, pgPassword, pgHost, pgPort, coreFieldCryptoRotationVerifyDB)
	cmd := exec.Command("goose", "-dir", migrationsDir(), "postgres", dsn, "up") //nolint:gosec // fixed args/resolved paths, not user input
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("goose up: %w\n%s", err, out)
	}
	return nil
}

// migrationsDir resolves mod-core's own model/migrations directory relative
// to this source file's location, so the test does not depend on a
// hand-typed absolute path tied to one machine's checkout layout. This file
// lives at <repo>/api/httpapi/field_crypto_keys_integration_test.go;
// migrations live at <repo>/model/migrations.
func migrationsDir() string {
	_, thisFile, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(thisFile), "..", "..", "model", "migrations")
}

// ---------------------------------------------------------------------------
// Authorizer test double
// ---------------------------------------------------------------------------

// wildcardAuthorizer grants every operation, exactly the "wildcard manage
// grant" actor the task doc's Assumptions call for. This suite is about
// concurrency and persistence, not about the authorization gate — task 001's
// unit tests (field_crypto_keys_test.go) already cover the 401/403 paths in
// depth against a fake.
type wildcardAuthorizer struct{}

func (wildcardAuthorizer) Authorize(context.Context, string, *int64) error { return nil }

var _ authz.Authorizer = wildcardAuthorizer{}

// ---------------------------------------------------------------------------
// Handler / request helpers
// ---------------------------------------------------------------------------

// newRotationRouter wires a real FieldCryptoKeyHandler over the shared pool
// and coreQ — the same coredb.Querier and txhelper.DB a production
// composition root hands it — with a wildcard authorizer, a no-op observer
// group (NewObserverGroup() with no observers is a valid no-op per
// api/observer's own doc comment), and no cipher: this suite never asserts
// on Cipher.Reload, so a nil cipher (which the handler explicitly guards
// with `if h.cipher != nil`) keeps the harness free of an unrelated
// dependency.
func newRotationRouter(t *testing.T) chi.Router {
	t.Helper()
	h := httpapi.NewFieldCryptoKeyHandler(pool, coreQ, wildcardAuthorizer{}, observer.NewObserverGroup(), nil)
	r := chi.NewRouter()
	httpapi.RegisterFieldCryptoKeyRoutes(r, h)
	return r
}

// adminRotationRequest builds an authenticated POST /field-crypto-keys/rotations
// request carrying body as its JSON payload (or no body at all when body is
// empty, the recommended zero-request invocation).
func adminRotationRequest(body string) *http.Request {
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(http.MethodPost, "/field-crypto-keys/rotations", nil)
	} else {
		req = httptest.NewRequest(http.MethodPost, "/field-crypto-keys/rotations", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	}
	return req.WithContext(opctx.WithActor(req.Context(), 1))
}

func decodeRotationBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response body %q: %v", rec.Body.String(), err)
	}
	return body
}

func decodeRotationErrorDetails(t *testing.T, rec *httptest.ResponseRecorder) []apiresp.FieldError {
	t.Helper()
	var env apiresp.Envelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode error envelope %q: %v", rec.Body.String(), err)
	}
	return env.Error.Details
}

// ---------------------------------------------------------------------------
// field_crypto_keys table helpers
// ---------------------------------------------------------------------------

// resetFieldCryptoKeys empties field_crypto_keys and restarts the identity
// sequence so every test starts from "nothing persisted yet" with predictable
// version numbers, independent of test order or a prior run's leftover
// state — the invariant Requirement 6 (leave the database clean, idempotent
// re-run) depends on.
func resetFieldCryptoKeys(t *testing.T, ctx context.Context) {
	t.Helper()
	if _, err := pool.Exec(ctx, "TRUNCATE TABLE field_crypto_keys RESTART IDENTITY"); err != nil {
		t.Fatalf("truncate field_crypto_keys: %v", err)
	}
}

// seedActiveKey inserts material as the sole active key row (no other row
// present, since every test resets the table first) and returns its version.
func seedActiveKey(t *testing.T, ctx context.Context, material []byte) int32 {
	t.Helper()
	var version int32
	if err := pool.QueryRow(ctx,
		"INSERT INTO field_crypto_keys (key_bytes) VALUES ($1) RETURNING version",
		material,
	).Scan(&version); err != nil {
		t.Fatalf("seed active field crypto key: %v", err)
	}
	return version
}

// countFieldCryptoKeys returns the total row count.
func countFieldCryptoKeys(t *testing.T, ctx context.Context) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM field_crypto_keys").Scan(&n); err != nil {
		t.Fatalf("count field_crypto_keys: %v", err)
	}
	return n
}

// countActiveFieldCryptoKeys returns the count of rows with retired_at IS
// NULL — the one-active-key invariant the concurrency case exists to prove.
func countActiveFieldCryptoKeys(t *testing.T, ctx context.Context) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM field_crypto_keys WHERE retired_at IS NULL").Scan(&n); err != nil {
		t.Fatalf("count active field_crypto_keys: %v", err)
	}
	return n
}

// waitForBlockedRotationWaiters polls pg_stat_activity until want backends
// are shown blocked on a lock while running RetireActiveFieldCryptoKey's
// UPDATE (identified by the sqlc-generated query's leading `-- name:` comment
// line, which pg_stat_activity.query reports verbatim), or fails the test if
// timeout elapses first. Used to confirm two real rotation requests are
// genuinely queued as waiters on the same contended row lock before that
// lock is released — see the comment in the concurrency test itself for why
// this confirmation, not just a channel-synchronized launch, is what makes
// the race deterministic.
func waitForBlockedRotationWaiters(t *testing.T, ctx context.Context, want int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		var n int
		if err := pool.QueryRow(ctx, `
			SELECT count(*) FROM pg_stat_activity
			WHERE wait_event_type = 'Lock'
			  AND query ILIKE '%RetireActiveFieldCryptoKey%'`,
		).Scan(&n); err != nil {
			t.Fatalf("poll pg_stat_activity for blocked rotation waiters: %v", err)
		}
		if n >= want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out after %s waiting for %d rotation requests to block on the retire-step row lock; saw %d", timeout, want, n)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// testKeyMaterial returns 32 non-zero bytes distinguishable by seed —
// mirrors field_crypto_keys_test.go's helper of the same shape, duplicated
// here rather than shared because the two files build in mutually exclusive
// package sets (httpapi vs. httpapi_test under the integration tag).
func testKeyMaterial(seed byte) []byte {
	material := make([]byte, 32)
	for i := range material {
		material[i] = seed + byte(i)
	}
	if material[0] == 0 {
		material[0] = 1
	}
	return material
}

// parseRotationTimestamp parses an RFC3339 timestamp string decoded from a
// JSON response field, failing the test loudly if v is not a string or does
// not parse — a response field that carries a lifecycle timestamp must
// always be one.
func parseRotationTimestamp(t *testing.T, v any) time.Time {
	t.Helper()
	s, ok := v.(string)
	if !ok {
		t.Fatalf("timestamp field: got %T (%v), want a string", v, v)
	}
	ts, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("parse timestamp %q: %v", s, err)
	}
	return ts
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestFieldCryptoKeyRotate_Sequential_HappyPath is the baseline the
// concurrency case is contrasted against: a single rotation returns 201,
// retires the previously-active version with the expected decryptable_until,
// and creates a new active version.
func TestFieldCryptoKeyRotate_Sequential_HappyPath(t *testing.T) {
	ctx := context.Background()
	resetFieldCryptoKeys(t, ctx)
	seedActiveKey(t, ctx, testKeyMaterial(1))

	router := newRotationRouter(t)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, adminRotationRequest(`{"grace_period_days":14}`))

	if rec.Code != http.StatusCreated {
		t.Fatalf("status: got %d, want %d — body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	body := decodeRotationBody(t, rec)
	retired, _ := body["retired"].(map[string]any)
	active, _ := body["active"].(map[string]any)

	if retired["version"] != float64(1) {
		t.Errorf("retired version: got %v, want 1", retired["version"])
	}
	if active["version"] != float64(2) {
		t.Errorf("active version: got %v, want 2", active["version"])
	}
	if retired["compromised_at"] != nil {
		t.Errorf("retired compromised_at: got %v, want null", retired["compromised_at"])
	}

	until := parseRotationTimestamp(t, retired["decryptable_until"])
	lower := time.Now().Add(13 * 24 * time.Hour)
	upper := time.Now().Add(15 * 24 * time.Hour)
	if until.Before(lower) || until.After(upper) {
		t.Errorf("decryptable_until: got %v, want ~14 days out (between %v and %v)", until, lower, upper)
	}

	if n := countFieldCryptoKeys(t, ctx); n != 2 {
		t.Fatalf("row count: got %d, want 2", n)
	}
	if n := countActiveFieldCryptoKeys(t, ctx); n != 1 {
		t.Errorf("active row count: got %d, want 1", n)
	}

	// The persisted retired row must carry the same resolved deadline the
	// response reported — the response is read back from the database inside
	// the same transaction, so this is really an internal-consistency check.
	var persistedUntil time.Time
	if err := pool.QueryRow(ctx, "SELECT decryptable_until FROM field_crypto_keys WHERE version = 1").Scan(&persistedUntil); err != nil {
		t.Fatalf("read back persisted decryptable_until: %v", err)
	}
	if persistedUntil.Before(lower) || persistedUntil.After(upper) {
		t.Errorf("persisted decryptable_until: got %v, want ~14 days out", persistedUntil)
	}
}

// TestFieldCryptoKeyRotate_ConcurrentRotations_OneWinsOneConflicts is the
// headline case this task exists to prove: two simultaneous rotation
// requests against the same handler and the same real database produce
// exactly one 201 and one 409 — never two 201s, never two 409s, never a
// 500 — and the table never ends up with two active keys.
func TestFieldCryptoKeyRotate_ConcurrentRotations_OneWinsOneConflicts(t *testing.T) {
	ctx := context.Background()
	resetFieldCryptoKeys(t, ctx)
	seedActiveKey(t, ctx, testKeyMaterial(1))

	router := newRotationRouter(t)

	// Gatekeeper: take the exact row lock RetireActiveFieldCryptoKey's UPDATE
	// needs, and hold it open until both real rotation requests below are
	// confirmed genuinely queued as waiters on it. A bare "release two
	// goroutines from the same channel" is not, by itself, enough to force a
	// genuine race here: each rotation's full HTTP-to-commit round trip
	// against a local Postgres is sub-millisecond, so without an artificial
	// point of contention the two requests reliably run start-to-finish one
	// after the other, and the second one simply rotates the first one's
	// brand-new active key — two clean 201s, never a 409, which is what a
	// first attempt at this test (channel-gate only, no forced contention)
	// actually produced. Holding, then releasing, the contended lock
	// ourselves reproduces the same DB-level contention two truly
	// simultaneous rotations would hit, deterministically rather than as a
	// function of scheduler luck.
	gateTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin gatekeeper transaction: %v", err)
	}
	if _, err := gateTx.Exec(ctx, "SELECT version FROM field_crypto_keys WHERE retired_at IS NULL FOR UPDATE"); err != nil {
		t.Fatalf("gatekeeper: lock the active row: %v", err)
	}

	// Distinct key material per goroutine: whichever loses must lose at the
	// RetireActiveFieldCryptoKey step (the active row is already retired by
	// the winner by the time it runs, so its WHERE retired_at IS NULL matches
	// nothing) rather than at the InsertActiveFieldCryptoKey unique-material
	// check — the property this test asserts is the retire-step race, not a
	// coincidental material collision.
	bodies := []string{
		fmt.Sprintf(`{"key_hex":%q}`, hex.EncodeToString(testKeyMaterial(101))),
		fmt.Sprintf(`{"key_hex":%q}`, hex.EncodeToString(testKeyMaterial(102))),
	}

	const goroutines = 2
	release := make(chan struct{}) // closed once, unblocking both goroutines at the same instant
	var (
		wg    sync.WaitGroup
		mu    sync.Mutex
		codes []int
		bodys []string
	)
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(i int) {
			defer wg.Done()
			req := adminRotationRequest(bodies[i])
			rec := httptest.NewRecorder()
			<-release // wait for the starting gate, so both fire together
			router.ServeHTTP(rec, req)
			mu.Lock()
			codes = append(codes, rec.Code)
			bodys = append(bodys, rec.Body.String())
			mu.Unlock()
		}(i)
	}
	close(release) // the starting gate: both goroutines proceed at the same instant

	// The visible synchronization point: nothing proceeds past this line
	// until both real rotation requests are provably contending for the same
	// row lock — i.e. genuinely concurrent, not merely dispatched together.
	waitForBlockedRotationWaiters(t, ctx, goroutines, 5*time.Second)

	if err := gateTx.Rollback(ctx); err != nil {
		t.Fatalf("release gatekeeper lock: %v", err)
	}

	wg.Wait()

	var got201, got409, gotOther int
	for i, code := range codes {
		switch code {
		case http.StatusCreated:
			got201++
		case http.StatusConflict:
			got409++
		default:
			gotOther++
			t.Errorf("goroutine %d: unexpected status %d — body: %s", i, code, bodys[i])
		}
	}
	if got201 != 1 {
		t.Errorf("201 responses: got %d, want exactly 1 — codes: %v", got201, codes)
	}
	if got409 != 1 {
		t.Errorf("409 responses: got %d, want exactly 1 — codes: %v", got409, codes)
	}
	if gotOther != 0 {
		t.Errorf("unexpected-status responses: got %d, want 0 — codes: %v", gotOther, codes)
	}

	// The one-active-key invariant: never two active keys, no matter how the
	// race resolved.
	if n := countActiveFieldCryptoKeys(t, ctx); n != 1 {
		t.Fatalf("active row count after concurrent rotations: got %d, want exactly 1", n)
	}

	// The version count increased by exactly one: the loser's INSERT never
	// ran. Started at 1 seeded row; exactly one winning rotation adds one row.
	if n := countFieldCryptoKeys(t, ctx); n != 2 {
		t.Fatalf("row count after concurrent rotations: got %d, want 2 (1 seed + 1 winner; the loser's INSERT must never have run)", n)
	}

	// The 409 must be the genuine no-active-key conflict this race produces,
	// not a coincidental key-material collision (the two goroutines used
	// distinct material precisely to rule that out).
	for i, code := range codes {
		if code != http.StatusConflict {
			continue
		}
		var env apiresp.Envelope
		if err := json.Unmarshal([]byte(bodys[i]), &env); err != nil {
			t.Fatalf("decode 409 envelope: %v", err)
		}
		if len(env.Error.Details) != 1 || env.Error.Details[0].Code != "field_crypto_keys.no_active_key" {
			t.Errorf("409 details: got %+v, want the no_active_key conflict", env.Error.Details)
		}
	}
}

// TestFieldCryptoKeyRotate_409_KeyMaterialAlreadyOnFile covers the
// unique-key_bytes rejection: rotating with an explicit key_hex equal to a
// key already on file returns a 409 rather than a 500 — the guard that stops
// an operator re-introducing a key previously retired as compromised.
func TestFieldCryptoKeyRotate_409_KeyMaterialAlreadyOnFile(t *testing.T) {
	ctx := context.Background()
	resetFieldCryptoKeys(t, ctx)
	inUse := testKeyMaterial(1)
	seedActiveKey(t, ctx, inUse)

	router := newRotationRouter(t)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, adminRotationRequest(fmt.Sprintf(`{"key_hex":%q}`, hex.EncodeToString(inUse))))

	if rec.Code != http.StatusConflict {
		t.Fatalf("status: got %d, want %d — body: %s", rec.Code, http.StatusConflict, rec.Body.String())
	}
	details := decodeRotationErrorDetails(t, rec)
	if len(details) != 1 || details[0].Field != "key_hex" {
		t.Errorf("details: got %+v, want one field error naming key_hex", details)
	}

	// Requirement 2's mandatory retire-then-insert order means the rejected
	// INSERT still leaves the retire half of the transaction rolled back —
	// the whole transaction fails together.
	if n := countFieldCryptoKeys(t, ctx); n != 1 {
		t.Errorf("row count: got %d, want 1 (the whole transaction must have rolled back)", n)
	}
	if n := countActiveFieldCryptoKeys(t, ctx); n != 1 {
		t.Errorf("active row count: got %d, want 1", n)
	}
}

// TestFieldCryptoKeyRotate_CompromisedEffects covers the compromised
// rotation's effect on the retired row end to end: compromised: true stamps
// compromised_at and leaves decryptable_until NULL.
func TestFieldCryptoKeyRotate_CompromisedEffects(t *testing.T) {
	ctx := context.Background()
	resetFieldCryptoKeys(t, ctx)
	seedActiveKey(t, ctx, testKeyMaterial(1))

	router := newRotationRouter(t)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, adminRotationRequest(`{"compromised":true}`))

	if rec.Code != http.StatusCreated {
		t.Fatalf("status: got %d, want %d — body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	retired, _ := decodeRotationBody(t, rec)["retired"].(map[string]any)
	if retired["compromised_at"] == nil {
		t.Error("compromised_at: got null, want a timestamp")
	}
	if retired["decryptable_until"] != nil {
		t.Errorf("decryptable_until: got %v, want null — a compromised rotation maximizes the re-encrypt window", retired["decryptable_until"])
	}

	var (
		persistedCompromisedAt *time.Time
		persistedUntil         *time.Time
	)
	if err := pool.QueryRow(ctx,
		"SELECT compromised_at, decryptable_until FROM field_crypto_keys WHERE version = 1",
	).Scan(&persistedCompromisedAt, &persistedUntil); err != nil {
		t.Fatalf("read back retired row: %v", err)
	}
	if persistedCompromisedAt == nil {
		t.Error("persisted compromised_at: got nil, want a timestamp")
	}
	if persistedUntil != nil {
		t.Errorf("persisted decryptable_until: got %v, want nil", *persistedUntil)
	}
}

// TestFieldCryptoKeyRotate_400_CompromisedWithGrace covers the other half of
// requirement 5: compromised combined with grace_period_days is rejected
// with 400 before any transaction runs.
func TestFieldCryptoKeyRotate_400_CompromisedWithGrace(t *testing.T) {
	ctx := context.Background()
	resetFieldCryptoKeys(t, ctx)
	seedActiveKey(t, ctx, testKeyMaterial(1))

	router := newRotationRouter(t)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, adminRotationRequest(`{"compromised":true,"grace_period_days":30}`))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want %d — body: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	details := decodeRotationErrorDetails(t, rec)
	if len(details) != 1 || details[0].Field != "grace_period_days" {
		t.Errorf("details: got %+v, want one field error naming grace_period_days", details)
	}

	// No transaction ran at all: still exactly the one seeded active row,
	// untouched.
	if n := countFieldCryptoKeys(t, ctx); n != 1 {
		t.Errorf("row count: got %d, want 1 (rejected before any transaction ran)", n)
	}
	var compromisedAt *time.Time
	if err := pool.QueryRow(ctx, "SELECT compromised_at FROM field_crypto_keys WHERE version = 1").Scan(&compromisedAt); err != nil {
		t.Fatalf("read back seeded row: %v", err)
	}
	if compromisedAt != nil {
		t.Error("the rejected request still stamped compromised_at on the seeded row")
	}
}
