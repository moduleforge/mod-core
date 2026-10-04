# Observer Group Add Design

## Purpose and scope

Records the investigation and design decision for app-level `ObserverGroup` registration: why a concurrency-safe `ObserverGroup.Add` was chosen over an mfgen manifest key, and the exact semantics the implementation must have.

## Current state (investigated)

- `api/observer/observer.go`: `ObserverGroup` holds `observers []MutationObserver`, `policy`, `logger`. `NewObserverGroup(observers...)` stores the caller's slice as-is (aliased). There is no way to add a member after construction.
- `WithPolicy` / `WithLogger` copy the struct by value (`g2 := *g`) and return a pointer to the copy.
- Dispatch: in-tx variants (`Observe`, `MustObserve`, `MayObserve`) run members serially in attach order when the tx is non-nil (a `pgx.Tx` refuses concurrent use). Under `PolicyPropagate` the first error stops the loop, so later members are skipped. With a nil tx they run in parallel (errgroup for propagate, WaitGroup for swallow). `ObserveAfterCommit` always runs members in parallel and waits for all.
- The zero value `&observer.ObserverGroup{}` is used directly by mod-users tests (`api/internal/service/ssh_keys_test.go`, `user_accounts_upgrade_test.go`), so the zero value must stay valid.
- The only `WithPolicy` user in the ecosystem is a mod-tokens test; no production code derives views today.
- mfgen (`mfgen/internal/codegen/main_gen.go`, `buildObserverGroup`) emits one `observer.NewObserverGroup(...)` call from every module's `provides.observers`, in module declaration order, and passes the resulting `*ObserverGroup` pointer to every service constructor.
- mfgen apps already have `startupHooks` in `moduleforge.app.yaml`. They run once, in order, after every infra/service/handler is constructed and before the server starts serving.
- flow-accounts' `appsetup.RegisterNotices` (in its `cmd/server/appsetup/notices.go`) works around the gap with `observer.NewObserverGroup(group, notices)` and then re-points each holder (`billing.Obs = combined`, `release.SetObserverGroup(combined)`). Every other holder of the root group never sees the app observer.

## Decision

Add a concurrency-safe `(*ObserverGroup).Add(observers ...MutationObserver)` to mod-core. No mfgen change.

Rationale:

- Because mfgen already hands the same `*ObserverGroup` pointer to every consumer, mutating that one group in place reaches every holder at once. That removes the per-consumer re-pointing that the workaround needs.
- Startup hooks already give an app a point after construction and before serving, and that is the right moment to call `Add`. A manifest key (`app.observers` or similar) would need schema, resolver and codegen changes in mfgen, plus a spec change in docs-mf-standards, and it would solve nothing more. It is not warranted.

## Required semantics

1. **Shared membership across views.** `WithPolicy` and `WithLogger` must return a view that shares its member set with the group it was derived from. Today they copy a frozen slice, so a module that stored `og.WithPolicy(...)` at construction time would silently miss observers an app `Add`s to the root group later. Shared membership closes that gap. A chain of views (`g.WithPolicy(p).WithLogger(l)`) shares the same single member set. Calling `Add` on any view adds to the shared set. Existing behavior does not change, because nothing could mutate membership before.
   - Suggested shape (not mandated): an unexported `src *ObserverGroup` field. A derived view sets it to the root group, so chains flatten. A `root()` helper returns `g.src` when non-nil and `g` otherwise. The mutex and member slice live only on the root, and views are built field-by-field, never by `*g` struct copy.
2. **Zero value stays valid.** `&ObserverGroup{}` dispatches as an empty group and accepts `Add` without prior initialization.
3. **Concurrency.** `Add` is safe to call concurrently with itself and with every dispatch method (`go test -race` clean). Dispatch takes a snapshot of the member slice under a read lock (or an atomic load) and iterates outside the lock. `Add` is copy-on-write: it builds a new slice and never appends into a backing array a snapshot may still be reading. `NewObserverGroup` copies its variadic input so the group never aliases the caller's slice.
4. **Ordering.** Added observers go after the existing members, in argument order and then call order. This is the "attach order" that serial in-tx dispatch follows.
5. **Programmer-error guards.** `Add` panics, with a message naming `observer.ObserverGroup.Add`, when:
   - an argument is a nil interface, because calling it would panic later at dispatch, far from the cause;
   - an argument is the group itself or any view sharing its member set, because that would recurse forever.

   Deeper cycles (A in B, B in A) are not detected, and the doc comment says so.
6. **`go vet` copylocks.** No by-value copy of a struct that holds a lock may remain anywhere in `api/` (in particular, `WithPolicy` and `WithLogger` must not use `g2 := *g`).

## Dispatch interaction (to be documented on `Add`)

- **In-tx (non-nil tx): serial.** Added observers run after earlier members, on the shared tx. Under `PolicyPropagate` (or `MustObserve`), an error from an earlier member stops dispatch before an added observer runs, and the tx rolls back. An error from an added observer's own `Observe` rolls back the whole mutation. So an observer that only cares about post-commit work should make `Observe` a no-op that returns nil.
- **In-tx (nil tx): parallel** with every other member.
- **Post-commit: parallel.** `ObserveAfterCommit` runs added observers concurrently with every other member and waits for all of them before returning. A slow post-commit observer delays the service method's return, so long work should be handed off asynchronously (flow-accounts' notice observer already does this).
- **Timing.** Register before serving, which means in a startup hook. A dispatch already in flight keeps the snapshot it took. An `Add` that lands between one mutation's in-tx dispatch and its post-commit dispatch can make the new observer see `ObserveAfterCommit` without a matching `Observe`. `Add` is safe at runtime but intended for startup.

## Out of scope (filed as followups)

- docs-mf-standards: `manifest-spec.md` §7 and `architecture/state-management-design.md` still say in-tx dispatch is parallel via errgroup, and neither mentions app-level registration. Those docs are in a separate repo (the `docs/mf-standards` submodule here).
- flow-accounts: replace the `RegisterNotices` nesting workaround with `group.Add(notices)` once this lands.
