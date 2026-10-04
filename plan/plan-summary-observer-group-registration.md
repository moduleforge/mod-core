# Plan Summary: observer-group-registration

## What was planned and why

Resolves followup `cuNY` (enhancement, from flow-accounts), "App-level ObserverGroup registration". Today an `observer.ObserverGroup` cannot gain members after `NewObserverGroup`, and mfgen's generated root builds the group only from modules' `provides.observers`. An app therefore cannot attach its own `MutationObserver` to the one generated group. flow-accounts works around this in `appsetup.RegisterNotices` by building a second group that nests the root group plus its observer, and re-pointing each consumer by hand.

This plan adds a concurrency-safe `(*ObserverGroup).Add` to mod-core's `api/observer` package. An app startup hook can then attach an observer to the generated group in place, and every holder of that group sees it. The plan also documents how added observers interact with serial in-tx and parallel post-commit dispatch.

**Must change**

- `api/observer/observer.go`:
  - new `Add`;
  - `WithPolicy`/`WithLogger` views share membership with their root instead of freezing a copy;
  - `NewObserverGroup` copies its input;
  - doc comments updated.
- `api/observer/observer_test.go`: new tests, including a `-race` test.
- `AGENTS.md`: observer row and convention.

**Must not change**

- Existing dispatch semantics: serial in-tx in attach order with a non-nil tx, parallel with a nil tx, parallel post-commit that waits for all members, and per-call policy variants.
- The validity of the zero value `&ObserverGroup{}`.
- `NewObserverGroup`'s signature.
- mfgen and the manifest schema. A manifest key was considered and rejected as unwarranted. The rationale is in the [observer group add design note](./notes/observer-group-add-design.md).

**Success criteria**

- An app can call `group.Add(obs)` from a startup hook, and `obs` then receives `Observe` and `ObserveAfterCommit` from every service holding the generated group, including any derived `WithPolicy`/`WithLogger` view.
- `go test -race ./observer/...`, `go test ./...` and `make lint` (vet, including copylocks, plus gofmt) pass in `api/`.

### Phase 1: ObserverGroup app-level registration (`observer-group-add`)

- `001-add-observer-group-registration` (sonnet-med) implements `Add`, shared-membership views, the constructor copy, doc comments, tests and the `AGENTS.md` updates. The normative semantics are in the [design note](./notes/observer-group-add-design.md).

This is a single task with no parallelism inside the phase.

### Phase 2: Documentation updates (`doc-updates`)

- `001-update-architecture-docs` (sonnet-high) runs architect-backend's review of `AGENTS.md`, `README.md` and `docs/architecture/` against the landed change. It must not edit the `docs/mf-standards` submodule.

Phase 2 depends on phase 1.

## What shipped

### Phase 01 — ObserverGroup App-Level Registration

1. **Add Concurrency-Safe ObserverGroup.Add** (`001-add-observer-group-registration.md`, tier `sonnet-med`) — Added a concurrency-safe copy-on-write ObserverGroup.Add. WithPolicy and WithLogger views now share one root member set with no lock copying, and NewObserverGroup copies its input. Added 8 tests including a race test, and corrected the AGENTS.md observer row and convention bullet. Dispatch behavior is unchanged.
   Commit `6ab446d`, merged at `8970c62b9d37765c0171961433878682ec6df9d4`.

### Phase 02 — Documentation Updates

1. **Update Architecture Docs** (`001-update-architecture-docs.md`, tier `sonnet-high`) — Architecture docs were already accurate after phase 1. Added one small AGENTS.md sentence on observer trust and startup registration and recorded the review in the task doc Status.
   Commit `56f8f7c`, merged at `47fc6079b61aef1bf61eaae901f93ab91e2dc71f`.

## Key decisions

_No `## Why this shape` section is recorded in `plan/overview.md`, so this plan's cross-task rationale was never written down. Per-task outcomes are under "What shipped" above._

## Findings

- **`mldB`** — **Observer dispatch goroutines lack recover** — promoted — ref: `mldB` — 2026-10-04

- **`SAUP`** — **Four root docs unreachable from README** — promoted — ref: `SAUP` — 2026-10-04

- **`MoCc`** — **ObserverGroup.Add accepts typed-nil, cycles** — promoted — ref: `MoCc` — 2026-10-04

- **`5cqV`** — **ObserverGroup.Add has no post-startup seal** — promoted — ref: `5cqV` — 2026-10-04

## Final Task State

# TODO

## Purpose and scope

Tracking document for the active plan.

## Tasks

### Phase 01 — ObserverGroup App-Level Registration

- [x] [001-add-observer-group-registration.md](./phase-01-observer-group-add/001-add-observer-group-registration.md) — tier `sonnet-med` · branch `plan/observer-group-registration-01-001` · commit `6ab446d` · merge `8970c62b9d37765c0171961433878682ec6df9d4`

### Phase 02 — Documentation Updates

- [x] [001-update-architecture-docs.md](./phase-02-doc-updates/001-update-architecture-docs.md) — tier `sonnet-high` · branch `plan/observer-group-registration-02-001` · commit `56f8f7c` · merge `47fc6079b61aef1bf61eaae901f93ab91e2dc71f`
