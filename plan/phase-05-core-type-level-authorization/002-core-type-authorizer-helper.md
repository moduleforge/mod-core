# Core Type Authorizer Helper

## Purpose and scope

**Security task.** Add the optional `TypeAuthorizer` capability interface and the `AuthorizeType(ctx, az, operation, typeID)` helper to `core-api/authz` (`api/authz/authz.go`), and correct the `Authorizer` contract's doc comment so that it states, without ambiguity, that target is an `entities.id` and must never be a `types.id`. Task 003 migrates mod-core's own four type-id call sites onto this helper. Implement with the standard `implement-task` procedure, test-first.

The design contract is [core type authorizer design](../notes/core-type-authorizer-design.md). Follow it, and halt and report if it proves wrong.

Scope: `api/authz/authz.go` and `api/authz/authz_test.go` only. Do not touch call sites (task 003), `api/authz/setup/`, any other module, or the `docs/mf-standards` submodule. Work only on a task branch cut from the plan branch, and never push.

## Requirements

1. **Red first.** In `api/authz/authz_test.go` (package `authz`), write the helper tests before the helper exists, so the first run fails to compile. Record that in Status/notes. Use local stubs:
   - (a) **Plain authorizer, no `AuthorizeType`.** A recording stub whose `Authorize` returns nil only when `target != nil && *target == typeID` (it simulates owning entity N where N equals the type id) and `ErrForbidden` otherwise. `AuthorizeType(ctx, stub, "create", typeID)` returns `ErrForbidden`, and the stub saw exactly one call, `("create", nil)`, and **no** call with a non-nil target.
   - (b) **Plain authorizer, allow on nil.** A stub that allows `("create", nil)` → the helper returns nil (the fallback honors wildcard-style policy).
   - (c) **Type-aware deny is final.** A stub that implements both methods: `AuthorizeType` returns `ErrForbidden`, and `Authorize` allows everything. The helper returns `ErrForbidden`, `AuthorizeType` was called exactly once with `("create", typeID)`, and `Authorize` was **never** called.
   - (d) **Type-aware allow.** `AuthorizeType` returns nil, and `Authorize` denies everything → nil.
   - (e) **Error passthrough.** `AuthorizeType` returns an arbitrary sentinel → `errors.Is(err, sentinel)`, and `Authorize` was not called. The same holds on the fallback path when `Authorize(ctx, op, nil)` returns a sentinel.
   - (f) **Fail closed.** `typeID` of `0` and `-1` → `ErrForbidden` (`errors.Is` with `apiresp.ErrForbidden`), with neither stub method called. A nil `Authorizer` interface → `ErrForbidden`, with no panic.
   - (g) **Hidden by a decorator.** A wrapper struct that holds a type-aware stub but only exposes `Authorize` takes the nil fallback (`("create", nil)` reaches the inner `Authorize`), never `&typeID`.
   - A compile-time check in the test file that a stub with the right method satisfies `TypeAuthorizer`.
2. **Implementation** in `api/authz/authz.go`:
   - `type TypeAuthorizer interface { AuthorizeType(ctx context.Context, operation string, typeID int64) error }`, with a doc comment: optional capability; typeID is a `types.id`, never an `entities.id`; answers type-scoped authority only (today, wildcard grants); never consults entity ownership or entity-targeted grants.
   - `func AuthorizeType(ctx context.Context, az Authorizer, operation string, typeID int64) error`, with exactly the semantics in the design note: nil `az` → `apiresp.ErrForbidden`; `typeID <= 0` → `apiresp.ErrForbidden`; capability present → its result, final; otherwise `az.Authorize(ctx, operation, nil)`. **It must never pass `&typeID` (or any non-nil target) to `Authorize`.** Its doc comment explains why the fallback is nil (the core contract's "no specific target", answered from wildcard grants only, so decorators, foreign authorizers, and stubs that hide or lack the method still fail closed), and that this is the one call every module should use for type-level `create`/`list`.
3. **Contract doc correction** (the `Authorizer` interface comment and the package doc):
   - target is always an `entities.id` (the object acted on, or the parent entity for dependent data such as a legal entity's contacts), or nil when no specific target exists.
   - Passing a `types.id` is a caller bug that an implementation cannot detect, because both are `int64`. An entity-semantic implementation matches it against an unrelated entity, which is an authorization bypass.
   - Type-level checks go through `AuthorizeType`, and implementations that can answer them implement `TypeAuthorizer`.
   - Replace the "If policy needs the target's resource type, the implementation looks it up by ID" sentence only if it now reads ambiguously. It stays true for entity targets.
   - Note in one sentence that the ecosystem call-shape table in `docs/mf-standards/architecture/authorization-design.md` currently says otherwise and is being corrected. Do not edit the submodule.
4. **No other change.** The `Authorizer` interface method set, `OpResolver`, and `RequireAuthenticated` are unchanged. No new imports beyond what `authz.go` already uses (`context`, `apiresp`, `opctx`).

## Validation

- Status/notes record the red run (compile failure or failing test names) from requirement 1.
- `cd api && go build ./... && go vet ./... && go test ./authz/...` pass. `cd api && go test ./...` passes.
- `cd api && make lint` is clean.
- `grep -n 'func AuthorizeType\|type TypeAuthorizer' api/authz/authz.go` shows both.
- Inspect `AuthorizeType`'s body by reading it: the only `Authorize(` call in it passes a literal `nil` target.
- `go doc ./authz` (from `api/`) shows the corrected `Authorizer` comment, `TypeAuthorizer`, and `AuthorizeType`.
- `git diff --stat` (excluding `plan/`) touches only `api/authz/authz.go` and `api/authz/authz_test.go`.

## Metadata

architectural_impact: true

## Assumptions

- The mod-users slice may or may not have landed. Nothing here depends on it. mod-users' `*Authorizer.AuthorizeType` has the identical signature, so it will satisfy `TypeAuthorizer` structurally once it exists.
- `apiresp.ErrForbidden` is the canonical forbidden sentinel that every module matches with `errors.Is`.

## References

- [core type authorizer design](../notes/core-type-authorizer-design.md): the decision, the semantics, and how other slices adopt it later.
- `api/authz/authz.go`: the current contract and package doc.
- `api/authz/authz_test.go`: the existing `stubAuthorizer` pattern.
- mod-users `api/internal/authz/authz.go` and `api/localAuthz/authz.go` (read-only): the production Authorizer, and where `AuthorizeType`/`localAuthz.TypeAuthorizer` land in the mod-users slice.
- mod-authz `api/service` local `authorizeType` helper (read-only, may not exist yet): the same pattern as a local copy.
