# Observer Registration

## Purpose and scope

Documents how `MutationObserver` implementations get into an `observer.ObserverGroup`, how
registration interacts with dispatch, and the trust model that follows from it. Intended for an
application author wiring observers in a startup hook, for a module author shipping an observer,
and for a reviewer assessing what code can see and veto mutations.

Not covered here: the `MutationObserver` / `ObserverGroup` API itself (see the `observer/` row of
[AGENTS.md](../../AGENTS.md#key-types-and-packages) and the doc comments in
`api/observer/observer.go`) and the module manifest specification (see
[docs/mf-standards/manifest-spec.md](../mf-standards/manifest-spec.md)).

## How observers get into a group

Membership is not fixed at construction. There are two routes in:

- **Module-provided observers.** Modules declare observers in their manifest; mfgen assembles them
  into the generated `*ObserverGroup` when it constructs the application, so they are members from
  the start.
- **App-level registration.** The composing application attaches its own observer to the generated
  group with `(*ObserverGroup).Add`, called from a startup hook. `Add` appends after existing
  members in argument order; successive calls append in call order. It works on the zero value, is
  safe for concurrent use, and panics (adding nothing) on a nil observer or on the group itself or
  a view of it.

`WithPolicy` and `WithLogger` return views that share one member set with the group they derive
from. An `Add` through any view or the root is visible to all of them; only the policy and logger
are per-view.

## Interaction with dispatch

- **In-tx dispatch is serial, in attach order, on the shared tx.** Observers added via `Add` run
  last. Under `PolicyPropagate` (or `MustObserve`), an error from an earlier member skips them and
  the transaction rolls back; an error from an added observer's `Observe` rolls back the whole
  mutation. An observer that only cares about post-commit work should make `Observe` a no-op
  returning `nil`.
- **Parallel dispatch** applies with a nil tx and for `ObserveAfterCommit`. `ObserveAfterCommit`
  waits for every member before returning, so long post-commit work should be handed off
  asynchronously.
- **Snapshots.** A dispatch already in flight keeps the member snapshot it took. An `Add` that
  lands between a mutation's in-tx dispatch and its post-commit dispatch can therefore give the new
  observer an `ObserveAfterCommit` without a matching `Observe`.
- **Panics in parallel goroutines are recovered.** Under propagate, a panic becomes a returned
  error; under swallow, and in `ObserveAfterCommit`, it is logged. (In the serial in-tx path with a
  non-nil tx, no recovery is installed.)

## Trust model

Observers are fully trusted in-process code. Every observer receives each mutation's `before` and
`after` payloads; under in-tx dispatch it receives the live `pgx.Tx`; and it can veto the
operation by returning an error.

Nothing seals the group after startup. `Add` is safe to call at runtime and is supported, but it
is not intended for that: any holder of the group pointer can add an observer at any time.

Guidance: register observers in startup hooks, and register only code you control.

## Late (post-startup) registration

Legitimate reasons to call `Add` after startup include:

- lazy module initialization;
- observers enabled per feature or tenant;
- temporary operational diagnostics;
- tests;
- plugin or hot-reload loading;
- one-off backfill jobs.

Most of these have a static substitute: a permanent observer that gates on runtime state, or that
delegates to a swappable handler. Prefer the static form where it fits.

There is currently no `Remove`, so a late-added observer cannot be detached.

## Future direction

Sealing the group after startup, mediating late-stage `Add`, and an app-scoped `Remove` are open
design questions, not implemented. See
[Observer registration seal, mediated late-stage Add, and app-scoped Remove](../project-roadmap.md#observer-registration-seal-mediated-late-stage-add-and-app-scoped-remove).
