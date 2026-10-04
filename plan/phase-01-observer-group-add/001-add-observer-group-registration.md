# Add Observer Group Registration

## Purpose and scope

Add a concurrency-safe `(*ObserverGroup).Add(observers ...MutationObserver)` to `api/observer/observer.go`, so an app can attach its own `MutationObserver` to the one mfgen-generated group from a startup hook. Today the only option is to build a nesting group and re-point each consumer. Scope covers:

- the `observer` package code;
- its unit tests;
- its package, type and method doc comments;
- the `observer/` row and the observer-policy convention bullet in `AGENTS.md`.

No mfgen change and no manifest key. No standard skill covers this, so implement it directly as Go library work.

The full design, with rationale and the investigated current state, is in [the observer group add design note](../notes/observer-group-add-design.md). Read it first. Its "Required semantics" section is normative for this task.

## Requirements

1. **`Add` method.** `func (g *ObserverGroup) Add(observers ...MutationObserver)`:
   - Appends the given observers after the existing members, preserving argument order. Successive calls append in call order.
   - With zero arguments, does nothing.
   - Panics with a message that names `observer.ObserverGroup.Add` when any argument is a nil interface value.
   - Panics with a message that names `observer.ObserverGroup.Add` when any argument is the group itself or a view sharing its member set (a direct self-nesting cycle). Deeper cycles are not detected, and the doc comment says so.
   - Validates every argument before mutating, so a panicking call adds nothing.
   - Works on the zero value (`&ObserverGroup{}`) with no prior initialization.
2. **Shared membership across views.** `WithPolicy` and `WithLogger` return views that share a single member set with the group they derive from. Chains (`g.WithPolicy(p).WithLogger(l)`) flatten to that same set. `Add` on any view is visible to the root group and every other view, and `Add` on the root is visible to every view. Policy and logger stay per-view, exactly as today. A suggested shape is in the design note: an unexported `src *ObserverGroup` plus a `root()` helper, with the lock and slice held only on the root. Any shape that meets these requirements and requirement 6 is acceptable.
3. **Concurrency.**
   - `Add` is safe concurrently with itself and with `Observe`, `MustObserve`, `MayObserve` and `ObserveAfterCommit`.
   - Each dispatch takes one snapshot of the member slice (under a read lock or an atomic load) and iterates it outside any lock. The lock is never held while an observer runs.
   - `Add` is copy-on-write: it allocates a new slice and never appends into a backing array that an earlier snapshot may still be reading.
4. **Constructor copies its input.** `NewObserverGroup(observers...)` copies the variadic slice, so the group never aliases a caller-owned array. Its behavior is otherwise unchanged: the default is `PolicyPropagate`, and empty input gives a valid no-op group.
5. **Dispatch behavior unchanged.**
   - Serial in-tx dispatch in attach order with a non-nil tx. Under propagate, the first error stops dispatch.
   - Parallel in-tx dispatch with a nil tx.
   - Parallel `ObserveAfterCommit` that waits for every member.
   - The empty-group fast path (a length check with no goroutines) stays.
   - Every existing test in `api/observer/observer_test.go` passes unmodified.
6. **No lock copying.** Remove every by-value struct copy (`g2 := *g`) from `WithPolicy` and `WithLogger`. `go vet ./...` (copylocks) passes.
7. **Doc comments.**
   - `Add`'s doc comment states the dispatch interaction from the design note's "Dispatch interaction" section:
     - added observers run last in serial in-tx dispatch;
     - under propagate, an earlier member's error skips them, and their own `Observe` error rolls back the mutation, so a post-commit-only observer should return nil from `Observe`;
     - they run in parallel with the others for a nil tx and for `ObserveAfterCommit`, and `ObserveAfterCommit` waits for all members;
     - an in-flight dispatch keeps its snapshot;
     - an `Add` between a mutation's in-tx and post-commit dispatch can yield `ObserveAfterCommit` without a matching `Observe`;
     - the intended call site is an app startup hook, before the server serves traffic.
   - Update the `ObserverGroup` type comment and the package doc so neither still implies membership is fixed at construction.
   - Update the `WithPolicy` and `WithLogger` comments to say the returned view shares membership.
8. **Tests** in `api/observer/observer_test.go` (package `observer_test`), covering at least:
   - `Add` on a constructed group: an added observer receives `Observe` and `ObserveAfterCommit`.
   - `Add` on a zero-value `&observer.ObserverGroup{}`.
   - Attach order: with a non-nil `fakeTx`, constructor members run before added members, and added members run in `Add` argument and call order. Record order with an order-capturing observer.
   - Propagate short-circuit: an earlier member's error means the added observer's `Observe` is not called.
   - View sharing:
     - `Add` on the root is seen by a previously derived `WithPolicy`/`WithLogger` view;
     - `Add` on a view is seen by the root;
     - a derived view keeps its own policy and logger.
   - Panics on a nil argument, on adding the group itself, and on adding a view of the group. Each panicking call leaves membership unchanged.
   - `NewObserverGroup` does not alias the caller's slice. Mutating the caller's slice after construction does not change dispatch.
   - A race test: concurrent `Add` calls run alongside concurrent `Observe` (nil tx), `MayObserve` and `ObserveAfterCommit` calls. After all goroutines finish, a final dispatch reaches every added observer. Run it under `-race`.
9. **`AGENTS.md`.**
   - Correct the `observer/` row in "Key types and packages". It currently says the group "fans out to N observers in parallel", which is already wrong for in-tx dispatch. It should say in-tx dispatch is serial in attach order on a shared tx, while `ObserveAfterCommit` (and nil-tx in-tx dispatch) runs in parallel. Mention `Add` for app-level registration from a startup hook, with views sharing membership.
   - Extend the "Observer policy" convention bullet with one sentence: app-owned observers join the generated group via `Add` in a startup hook, not by building a nesting group.

## Validation

- `cd api && go build ./...` succeeds.
- `cd api && go test ./...` passes, including every pre-existing observer test unmodified.
- `cd api && go test -race -count=3 ./observer/...` passes with no race reports.
- `cd api && make lint` passes (`go vet ./...` with no copylocks finding, and `gofmt -l .` is empty).
- `grep -n 'g2 := \*g' api/observer/observer.go` returns nothing.
- `grep -n 'func (g \*ObserverGroup) Add' api/observer/observer.go` returns exactly one match.
- `grep -n 'in parallel with configurable' AGENTS.md` returns nothing (the stale `observer/` row text is replaced), and `grep -n 'Add' AGENTS.md` shows the new registration guidance.
- Manual review: `Add`'s doc comment covers every point in Requirement 7.

## Metadata

architectural_impact: true

## Assumptions

- Only one production caller of `NewObserverGroup` exists per app: the mfgen-generated root. It passes the group pointer to every consumer, so mutating it in place reaches all holders. Confirmed against `mfgen/internal/codegen/main_gen.go` and flow-accounts' generated `cmd/server/main.go`.
- mfgen `startupHooks` run after all services are constructed and before the server serves, so they are the intended `Add` call site. No mfgen change is part of this plan.
- Sibling repos (`docs/mf-standards` submodule, flow-accounts) are not edited here. Their updates were filed as followups.
- A consumer repo that copies an `ObserverGroup` by value would start failing `go vet` copylocks. No such copy was found (consumers use `*ObserverGroup` and `&observer.ObserverGroup{}`). If one turns up, report it rather than weakening the lock design.

## References

- [Observer group add design note](../notes/observer-group-add-design.md): the decision, the normative semantics and the dispatch-interaction text.
- `api/observer/observer.go`: current `ObserverGroup`, `NewObserverGroup`, `WithPolicy`, `WithLogger`, `dispatch` and `ObserveAfterCommit`.
- `api/observer/observer_test.go`: the existing helpers (`countObserver`, `logObserver`, `errObserver`, `fakeTx`, `newLogBuf`) to reuse.
- `AGENTS.md` ("Key types and packages" `observer/` row, and the "Conventions" observer-policy bullet).

## Checkpoint hints

- After `observer.go` implements `Add`, shared views and the constructor copy, with build, vet and existing tests green.
- After the new tests, including the race test, pass.
- After the `AGENTS.md` updates.

## Status

- Outcome: succeeded (2026-10-04).
- Validation: build, `go test ./...`, `go test -race -count=3 ./observer/...`, `make lint` all pass; grep checks pass.
- Files: `api/observer/observer.go`, `api/observer/observer_test.go`, `AGENTS.md`.
