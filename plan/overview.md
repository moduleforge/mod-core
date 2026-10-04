# Observer Group Registration

## Purpose and scope

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

## Current status

Phase 1 starts first. There are no preconditions beyond a clean `api/` build. Sibling-repo work is out of scope and was filed as followups: the stale observer docs in docs-mf-standards, and flow-accounts dropping its nesting workaround.

## Overview

### Phase 1: ObserverGroup app-level registration (`observer-group-add`)

- `001-add-observer-group-registration` (sonnet-med) implements `Add`, shared-membership views, the constructor copy, doc comments, tests and the `AGENTS.md` updates. The normative semantics are in the [design note](./notes/observer-group-add-design.md).

This is a single task with no parallelism inside the phase.

### Phase 2: Documentation updates (`doc-updates`)

- `001-update-architecture-docs` (sonnet-high) runs architect-backend's review of `AGENTS.md`, `README.md` and `docs/architecture/` against the landed change. It must not edit the `docs/mf-standards` submodule.

Phase 2 depends on phase 1.
