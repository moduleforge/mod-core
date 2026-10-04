// Package observer defines the MutationObserver interface and the ObserverGroup
// concrete type for fan-out dispatch to multiple observers. Membership is not
// fixed at construction: an app can attach its own observer to the generated
// group later via ObserverGroup.Add (see its doc comment for the semantics).
//
// # Interfaces vs concrete types
//
// Service methods receive *ObserverGroup (not the bare MutationObserver
// interface) so they can choose Observe / MustObserve / MayObserve per call
// site without sacrificing the flexibility of a per-group default policy.
//
// # Logger injection
//
// Functions that must log errors (MayObserve, ObserveAfterCommit) accept an
// optional *slog.Logger via ObserverGroup.WithLogger. When nil (the default),
// slog.Default() is used at each call site. This keeps the zero-value
// ObserverGroup valid without any initialisation, while still allowing test
// code to capture log output by passing in a custom logger.
package observer

import (
	"context"
	"log/slog"
	"sync"

	"github.com/jackc/pgx/v5"
	"golang.org/x/sync/errgroup"
)

// MutationObserver is implemented by audit-module, outbox-module,
// cache-invalidator, search-index-sync, and any other consumer that wants
// to react to entity mutations. Implementations return errors honestly;
// the decision to swallow or propagate is made by the wrapping ObserverGroup
// based on its configured Policy and the call variant (Observe / MustObserve
// / MayObserve) the service method uses.
//
// An implementation may implement only one of the two methods (the other
// becoming a no-op) if it only cares about one phase.
type MutationObserver interface {
	// Observe runs inside the operation's transaction. All in-tx observers on
	// one ObserverGroup are invoked serially, in attach order, on the shared
	// tx, so an implementation may issue queries on it freely but must not
	// spawn concurrent use of the tx itself (pgx rejects it with "conn busy").
	// The error return is
	// passed to the calling ObserverGroup, which decides whether to
	// propagate or log-and-swallow based on its configured Policy and the
	// call variant in use.
	Observe(
		ctx context.Context,
		tx pgx.Tx,
		op string,
		resource string,
		targetEntityID *int64,
		before, after any,
	) error

	// ObserveAfterCommit runs after the tx has successfully committed.
	// Errors are logged by the caller (ObserverGroup); they do not unwind
	// the already-committed operation. Post-commit observers re-fetch from
	// the DB if they need before-state.
	ObserveAfterCommit(
		ctx context.Context,
		op string,
		resource string,
		targetEntityID *int64,
		after any,
	)
}

// NoopObserver is a MutationObserver that does nothing. Useful in tests that
// need a concrete observer but do not care about its behaviour. An empty
// ObserverGroup (NewObserverGroup()) is also a valid no-op.
type NoopObserver struct{}

// NewNoopObserver returns a NoopObserver value. Provided as a constructor
// function so the generated wiring can reference it via a constructor symbol
// rather than a struct literal.
func NewNoopObserver() NoopObserver { return NoopObserver{} }

var _ MutationObserver = NoopObserver{}

func (NoopObserver) Observe(_ context.Context, _ pgx.Tx, _, _ string, _ *int64, _, _ any) error {
	return nil
}

func (NoopObserver) ObserveAfterCommit(_ context.Context, _, _ string, _ *int64, _ any) {}

// Policy controls how an ObserverGroup handles in-tx observation errors
// when service methods call Observe (the default-policy variant).
//
// PolicyPropagate: the first non-nil error from any wrapped observer is
// returned, aborting the operation and rolling back the transaction.
//
// PolicySwallow: errors are logged per-observer; the group returns nil.
//
// Service methods can override the configured policy on a per-call basis by
// calling MustObserve (always propagate) or MayObserve (always swallow).
type Policy int

const (
	PolicyPropagate Policy = iota // default: fail loud
	PolicySwallow                 // best-effort: log and continue
)

// ObserverGroup wraps N MutationObserver implementations and exposes the
// three-variant in-tx call surface plus the post-commit fan-out.
//
// Service methods take *ObserverGroup (not the bare MutationObserver
// interface) so they can choose between Observe / MustObserve / MayObserve
// per call.
//
// In-tx call variants dispatch to wrapped observers serially, in attach order,
// when given a tx (one pgx.Tx cannot be used concurrently); with a nil tx and
// for ObserveAfterCommit they dispatch in parallel.
// No inter-observer dependencies are supported (deliberate simplification).
// No per-observer policy split within a single group; if you need that,
// decompose into multiple groups.
//
// Membership may grow after construction via Add, which is safe for
// concurrent use. Views returned by WithPolicy and WithLogger share one
// member set with the group they derive from, so an Add through any of them
// is visible to all. An ObserverGroup must not be copied by value after first
// use; always hold it by pointer.
//
// An empty group (NewObserverGroup() with no arguments, or the zero value
// &ObserverGroup{}) is valid and is a no-op for all methods.
type ObserverGroup struct {
	// src is the root group this view derives from; nil on a root group.
	// Views always point at the root, so chains flatten.
	src *ObserverGroup

	// mu and observers are used only on the root group (see root()).
	mu        sync.RWMutex
	observers []MutationObserver

	policy Policy
	logger *slog.Logger
}

// NewObserverGroup constructs an ObserverGroup with PolicyPropagate as the
// default policy. Use WithPolicy to override.
//
// The variadic slice is copied, so the group never aliases a caller-owned
// array. Empty input is valid and produces a no-op group; service code does
// not need nil checks.
func NewObserverGroup(observers ...MutationObserver) *ObserverGroup {
	cp := make([]MutationObserver, len(observers))
	copy(cp, observers)
	return &ObserverGroup{observers: cp}
}

// root returns the group that owns the member set: the group itself, or the
// group a view was derived from.
func (g *ObserverGroup) root() *ObserverGroup {
	if g.src != nil {
		return g.src
	}
	return g
}

// snapshot returns the current member slice. The returned slice is never
// mutated (Add is copy-on-write), so callers iterate it without any lock.
func (g *ObserverGroup) snapshot() []MutationObserver {
	r := g.root()
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.observers
}

// Add attaches observers to the group after its existing members, preserving
// argument order; successive calls append in call order. With no arguments it
// does nothing. It works on the zero value and is safe to call concurrently
// with itself and with every dispatch method. Because WithPolicy and
// WithLogger views share membership, Add through any view (or the root) is
// visible to all of them.
//
// Add panics, with a message naming observer.ObserverGroup.Add, if any
// argument is a nil interface value, or is the group itself or a view sharing
// its member set (a direct self-nesting cycle that would recurse forever).
// Deeper cycles (A contains B, B contains A) are not detected. Every argument
// is validated before anything is added, so a panicking call adds nothing.
//
// Dispatch interaction:
//   - With a non-nil tx, in-tx dispatch is serial in attach order, so added
//     observers run last. Under PolicyPropagate (or MustObserve) an error from
//     an earlier member skips them and the tx rolls back. An error from an
//     added observer's own Observe rolls back the whole mutation, so an
//     observer that only cares about post-commit work should make Observe a
//     no-op that returns nil.
//   - With a nil tx, and for ObserveAfterCommit, added observers run in
//     parallel with every other member; ObserveAfterCommit waits for all
//     members before returning, so long post-commit work should be handed off
//     asynchronously.
//   - A dispatch already in flight keeps the member snapshot it took. An Add
//     that lands between one mutation's in-tx dispatch and its post-commit
//     dispatch can make the new observer see ObserveAfterCommit without a
//     matching Observe.
//
// The intended call site is an application startup hook, before the server
// serves traffic; Add is safe at runtime but not intended for it.
func (g *ObserverGroup) Add(observers ...MutationObserver) {
	if len(observers) == 0 {
		return
	}
	r := g.root()
	for _, o := range observers {
		if o == nil {
			panic("observer.ObserverGroup.Add: nil observer")
		}
		if og, ok := o.(*ObserverGroup); ok && og != nil && og.root() == r {
			panic("observer.ObserverGroup.Add: cannot add a group to itself or to a view of itself")
		}
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	next := make([]MutationObserver, len(r.observers), len(r.observers)+len(observers))
	copy(next, r.observers)
	r.observers = append(next, observers...)
}

// WithPolicy returns a view of g with the given default policy set. The view
// shares membership with g (and with every other view of it), so Add through
// either is visible to both; only the policy (and logger) are per-view.
// Builder-style: safely chainable on a shared base group.
func (g *ObserverGroup) WithPolicy(p Policy) *ObserverGroup {
	return &ObserverGroup{src: g.root(), policy: p, logger: g.logger}
}

// WithLogger returns a view of g with the given logger set for swallowed-error
// messages. If not called, slog.Default() is used. Useful in tests that
// capture log output. The view shares membership with g (and with every other
// view of it), so Add through either is visible to both; only the policy and
// logger are per-view.
func (g *ObserverGroup) WithLogger(l *slog.Logger) *ObserverGroup {
	return &ObserverGroup{src: g.root(), policy: g.policy, logger: l}
}

func (g *ObserverGroup) log() *slog.Logger {
	if g.logger != nil {
		return g.logger
	}
	return slog.Default()
}

// Observe runs all wrapped observers using the group's configured
// default policy.
func (g *ObserverGroup) Observe(
	ctx context.Context, tx pgx.Tx,
	op, resource string, targetEntityID *int64,
	before, after any,
) error {
	return g.dispatch(ctx, tx, op, resource, targetEntityID, before, after, g.policy)
}

// MustObserve runs all wrapped observers and always propagates
// the first error, regardless of the group's configured policy.
func (g *ObserverGroup) MustObserve(
	ctx context.Context, tx pgx.Tx,
	op, resource string, targetEntityID *int64,
	before, after any,
) error {
	return g.dispatch(ctx, tx, op, resource, targetEntityID, before, after, PolicyPropagate)
}

// MayObserve runs all wrapped observers and always logs and
// swallows errors, regardless of the group's configured policy.
// Always returns nil.
func (g *ObserverGroup) MayObserve(
	ctx context.Context, tx pgx.Tx,
	op, resource string, targetEntityID *int64,
	before, after any,
) error {
	return g.dispatch(ctx, tx, op, resource, targetEntityID, before, after, PolicySwallow)
}

// dispatch is the shared implementation for in-tx fan-out. With a non-nil tx
// observers run serially in attach order; with a nil tx they run in parallel.
// Error handling is governed by policy.
func (g *ObserverGroup) dispatch(
	ctx context.Context, tx pgx.Tx,
	op, resource string, targetEntityID *int64,
	before, after any,
	policy Policy,
) error {
	observers := g.snapshot()
	if len(observers) == 0 {
		return nil
	}

	// A pgx.Tx wraps a single connection and refuses concurrent use ("conn
	// busy"), so observers sharing a real tx run serially in attach order.
	// With a nil tx there is nothing to share and the parallel path is kept.
	if tx != nil {
		for _, obs := range observers {
			err := obs.Observe(ctx, tx, op, resource, targetEntityID, before, after)
			if err == nil {
				continue
			}
			if policy == PolicyPropagate {
				return err
			}
			g.log().ErrorContext(ctx, "observer: in-tx observe error (swallowed)",
				"op", op, "resource", resource, "error", err)
		}
		return nil
	}

	if policy == PolicyPropagate {
		eg, egCtx := errgroup.WithContext(ctx)
		for _, obs := range observers {
			obs := obs
			eg.Go(func() error {
				return obs.Observe(egCtx, tx, op, resource, targetEntityID, before, after)
			})
		}
		return eg.Wait()
	}

	// PolicySwallow: run all, log errors, return nil.
	var wg sync.WaitGroup
	for _, obs := range observers {
		obs := obs
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := obs.Observe(ctx, tx, op, resource, targetEntityID, before, after); err != nil {
				g.log().ErrorContext(ctx, "observer: in-tx observe error (swallowed)",
					"op", op, "resource", resource, "error", err)
			}
		}()
	}
	wg.Wait()
	return nil
}

// ObserveAfterCommit runs all wrapped observers after a successful
// commit. There are no Must/May variants because the transaction has already
// committed. Post-commit observers re-fetch from the DB if they need
// before-state.
func (g *ObserverGroup) ObserveAfterCommit(
	ctx context.Context,
	op, resource string, targetEntityID *int64,
	after any,
) {
	observers := g.snapshot()
	if len(observers) == 0 {
		return
	}

	var wg sync.WaitGroup
	for _, obs := range observers {
		obs := obs
		wg.Add(1)
		go func() {
			defer wg.Done()
			obs.ObserveAfterCommit(ctx, op, resource, targetEntityID, after)
		}()
	}
	wg.Wait()
}
