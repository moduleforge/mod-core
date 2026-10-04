package observer_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/moduleforge/core-api/observer"
)

// -- test helpers --

// errObserver always returns a fixed error from Observe; ObserveAfterCommit
// is a no-op (errors are no longer returned by that method).
type errObserver struct {
	observeErr error
}

func (o *errObserver) Observe(_ context.Context, _ pgx.Tx, _, _ string, _ *int64, _, _ any) error {
	return o.observeErr
}

func (o *errObserver) ObserveAfterCommit(_ context.Context, _, _ string, _ *int64, _ any) {}

// countObserver records how many times Observe / ObserveAfterCommit were
// called and optionally introduces a small delay to exercise parallelism.
type countObserver struct {
	observeCount     atomic.Int32
	afterCommitCount atomic.Int32
	delay            time.Duration
}

func (o *countObserver) Observe(_ context.Context, _ pgx.Tx, _, _ string, _ *int64, _, _ any) error {
	if o.delay > 0 {
		time.Sleep(o.delay)
	}
	o.observeCount.Add(1)
	return nil
}

func (o *countObserver) ObserveAfterCommit(_ context.Context, _, _ string, _ *int64, _ any) {
	if o.delay > 0 {
		time.Sleep(o.delay)
	}
	o.afterCommitCount.Add(1)
}

// logObserver records calls in a mutex-protected slice for test assertions.
type logObserver struct {
	mu  sync.Mutex
	ops []string
	err error
}

func (o *logObserver) Observe(_ context.Context, _ pgx.Tx, op, _ string, _ *int64, _, _ any) error {
	o.mu.Lock()
	o.ops = append(o.ops, op)
	o.mu.Unlock()
	return o.err
}

func (o *logObserver) ObserveAfterCommit(_ context.Context, op, _ string, _ *int64, _ any) {
	o.mu.Lock()
	o.ops = append(o.ops, op)
	o.mu.Unlock()
}

// newLogBuf returns a slog.Logger that writes to the returned buffer.
func newLogBuf() (*slog.Logger, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	return slog.New(slog.NewTextHandler(buf, nil)), buf
}

// -- tests --

func TestEmptyGroup_Noop(t *testing.T) {
	g := observer.NewObserverGroup()
	ctx := context.Background()

	if err := g.Observe(ctx, nil, "create", "foo", nil, nil, nil); err != nil {
		t.Fatalf("Observe on empty group: %v", err)
	}
	if err := g.MustObserve(ctx, nil, "create", "foo", nil, nil, nil); err != nil {
		t.Fatalf("MustObserve on empty group: %v", err)
	}
	if err := g.MayObserve(ctx, nil, "create", "foo", nil, nil, nil); err != nil {
		t.Fatalf("MayObserve on empty group: %v", err)
	}
	// ObserveAfterCommit on empty group must not panic.
	g.ObserveAfterCommit(ctx, "create", "foo", nil, nil)
}

func TestGroup_CallsAllObserversInParallel(t *testing.T) {
	// Each observer sleeps 20 ms. If they ran sequentially, total wall time
	// would exceed 60 ms. Running in parallel it should be < 50 ms.
	const delay = 20 * time.Millisecond
	const n = 3

	observers := make([]observer.MutationObserver, n)
	counters := make([]*countObserver, n)
	for i := range observers {
		co := &countObserver{delay: delay}
		counters[i] = co
		observers[i] = co
	}

	g := observer.NewObserverGroup(observers...)
	ctx := context.Background()

	start := time.Now()
	if err := g.Observe(ctx, nil, "create", "foo", nil, nil, nil); err != nil {
		t.Fatalf("Observe: %v", err)
	}
	elapsed := time.Since(start)

	// Verify all were called exactly once.
	for i, co := range counters {
		if got := co.observeCount.Load(); got != 1 {
			t.Errorf("observer[%d]: expected 1 call, got %d", i, got)
		}
	}

	// Parallelism check: should complete in well under 3x the delay.
	if elapsed > 2*delay*time.Duration(n) {
		t.Errorf("likely sequential execution: elapsed %v with %d observers at %v each", elapsed, n, delay)
	}
}

func TestObserve_PolicyPropagate_DefaultReturnsError(t *testing.T) {
	sentinel := errors.New("observe error")
	g := observer.NewObserverGroup(
		&errObserver{observeErr: sentinel},
		observer.NoopObserver{},
	)
	// PolicyPropagate is the default; no WithPolicy call needed.
	err := g.Observe(context.Background(), nil, "update", "foo", nil, nil, nil)
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel error, got: %v", err)
	}
}

func TestObserve_PolicySwallow_ReturnsNil(t *testing.T) {
	logger, buf := newLogBuf()
	sentinel := errors.New("swallowed error")
	g := observer.NewObserverGroup(
		&errObserver{observeErr: sentinel},
	).WithPolicy(observer.PolicySwallow).WithLogger(logger)

	err := g.Observe(context.Background(), nil, "update", "foo", nil, nil, nil)
	if err != nil {
		t.Fatalf("expected nil from PolicySwallow, got: %v", err)
	}
	if !bytes.Contains(buf.Bytes(), []byte("swallowed")) {
		t.Errorf("expected swallowed error to be logged; log output: %s", buf.String())
	}
}

func TestMustObserve_OverridesSwallowPolicy(t *testing.T) {
	sentinel := errors.New("must-observe error")
	// Group is configured to swallow, but MustObserve overrides it.
	g := observer.NewObserverGroup(
		&errObserver{observeErr: sentinel},
	).WithPolicy(observer.PolicySwallow)

	err := g.MustObserve(context.Background(), nil, "delete", "foo", nil, nil, nil)
	if !errors.Is(err, sentinel) {
		t.Fatalf("MustObserve should propagate error; got: %v", err)
	}
}

func TestMayObserve_OverridesPropagatePolicy(t *testing.T) {
	logger, buf := newLogBuf()
	sentinel := errors.New("may-observe error")
	// Group is configured to propagate, but MayObserve overrides it.
	g := observer.NewObserverGroup(
		&errObserver{observeErr: sentinel},
	).WithLogger(logger)
	// Default policy is PolicyPropagate.

	err := g.MayObserve(context.Background(), nil, "delete", "foo", nil, nil, nil)
	if err != nil {
		t.Fatalf("MayObserve should swallow error; got: %v", err)
	}
	if !bytes.Contains(buf.Bytes(), []byte("may-observe error")) {
		t.Errorf("expected error to be logged; log output: %s", buf.String())
	}
}

func TestObserveAfterCommit_CallsAllObservers(t *testing.T) {
	co1 := &countObserver{}
	co2 := &countObserver{}
	g := observer.NewObserverGroup(co1, co2)

	g.ObserveAfterCommit(context.Background(), "create", "foo", nil, nil)

	if got := co1.afterCommitCount.Load(); got != 1 {
		t.Errorf("co1: expected 1 post-commit call, got %d", got)
	}
	if got := co2.afterCommitCount.Load(); got != 1 {
		t.Errorf("co2: expected 1 post-commit call, got %d", got)
	}
}

func TestNoopObserver(t *testing.T) {
	var obs observer.MutationObserver = observer.NoopObserver{}
	ctx := context.Background()

	if err := obs.Observe(ctx, nil, "create", "foo", nil, nil, nil); err != nil {
		t.Fatalf("NoopObserver.Observe: %v", err)
	}
	// Must not panic.
	obs.ObserveAfterCommit(ctx, "create", "foo", nil, nil)
}

// fakeTx detects overlapping use: embedded nil pgx.Tx, only Exec is implemented.
type fakeTx struct {
	pgx.Tx
	inUse atomic.Bool
}

func (f *fakeTx) Exec(_ context.Context, _ string, _ ...any) (pgconn.CommandTag, error) {
	if !f.inUse.CompareAndSwap(false, true) {
		return pgconn.CommandTag{}, errors.New("conn busy")
	}
	time.Sleep(20 * time.Millisecond)
	f.inUse.Store(false)
	return pgconn.CommandTag{}, nil
}

type queryObserver struct{ calls atomic.Int32 }

func (o *queryObserver) Observe(ctx context.Context, tx pgx.Tx, _, _ string, _ *int64, _, _ any) error {
	o.calls.Add(1)
	_, err := tx.Exec(ctx, "select 1")
	return err
}

func (o *queryObserver) ObserveAfterCommit(_ context.Context, _, _ string, _ *int64, _ any) {}

func TestObserve_SharedTx_SerializedNoConnBusy(t *testing.T) {
	for _, policy := range []observer.Policy{observer.PolicyPropagate, observer.PolicySwallow} {
		logger, buf := newLogBuf()
		a, b := &queryObserver{}, &queryObserver{}
		g := observer.NewObserverGroup(a, b).WithPolicy(policy).WithLogger(logger)
		if err := g.Observe(context.Background(), &fakeTx{}, "delete", "foo", nil, nil, nil); err != nil {
			t.Fatalf("policy %d: Observe: %v", policy, err)
		}
		if a.calls.Load() != 1 || b.calls.Load() != 1 {
			t.Errorf("policy %d: expected both observers called once", policy)
		}
		if bytes.Contains(buf.Bytes(), []byte("conn busy")) {
			t.Errorf("policy %d: concurrent tx use detected: %s", policy, buf.String())
		}
	}
}

func TestObserve_SharedTx_PropagateStopsAtFirstError(t *testing.T) {
	sentinel := errors.New("boom")
	after := &countObserver{}
	g := observer.NewObserverGroup(&errObserver{observeErr: sentinel}, after)
	err := g.Observe(context.Background(), &fakeTx{}, "update", "foo", nil, nil, nil)
	if !errors.Is(err, sentinel) || after.observeCount.Load() != 0 {
		t.Fatalf("expected sentinel and later observer skipped; err=%v", err)
	}
}
