# TypeAuthorizer Contract Wording

## Purpose and scope

Update mod-core's authorization contract text for type-scoped grants.

The current `TypeAuthorizer` doc says the capability "answers type-scoped authority only (today, wildcard grants) and never consults entity ownership or entity-targeted grants". Under variant 1a, the last clause is wrong: a type grant **is** an entity-targeted grant, on the type's entity. The original intent was "never treat `typeID` as an `entities.id`". It was never "never read grants".

This task rewrites the code doc comments and the architecture doc section to state the new contract precisely. It also folds in followup `ggLy`.

Standard implementation task: follow the `implement-task` procedure. Documentation-only: no behaviour change.

Files in scope:

- `api/authz/authz.go` (doc comments only)
- `docs/architecture/type-level-authorization.md`

Out of scope:

- AGENTS.md, README.md and `model/README.md` (phase 3),
- `docs/mf-standards/architecture/authorization-design.md`, a submodule edited by the mod-authz slice.

## Requirements

1. **`api/authz/authz.go`, `TypeAuthorizer` doc comment.** State that the capability answers type-scoped authority:
   - (a) wildcard grants (`target_id IS NULL`), and
   - (b) grants whose target is the **exact** type's entity (`types.entity_id` for `typeID`), held directly or through target-group membership, by the actor or any actor group in its chain.

   It must also state that the capability:
   - never consults entity ownership,
   - never walks to parent or child types (a grant on a parent type does not cover its subtypes),
   - never treats `typeID` as an `entities.id`.

   Keep: "typeID is a types.id, never an entities.id" and "Callers should not invoke it directly; use AuthorizeType".
2. **`Authorizer` doc comment** (the target paragraph, about `authz.go:45-59`):
   - Keep the target rule.
   - Add one sentence: a type's own entity (`types.entity_id`) is a legitimate `entities.id` target for entity-level operations on the type entity itself, for example `read` or `grant`. It is never a stand-in for the type id in a type-level `create` or `list` check.
   - Keep the note that `authorization-design.md` is being corrected. That correction is now delivered by the mod-authz slice of this same change.
3. **`AuthorizeType` doc comment.** The fallback paragraph must say that the nil-target fallback answers from wildcard grants only. So an `Authorizer` that lacks the capability, or hides it behind a decorator, denies type-grant holders. It fails closed: a type grant takes effect only through an implementation that provides `TypeAuthorizer`.
4. **`docs/architecture/type-level-authorization.md`:**
   - **"Implementing the capability":** replace "(today, wildcard grants) and never consults entity ownership or entity-targeted grants" with the contract in requirement 1. Name the lookup (`types.entity_id` of exactly `typeID`).
   - **"The fallback rule":** add the fail-closed consequence in requirement 3.
   - **Add a short section on type entities:**
     - every type has one entity of the sentinel type `type`, created with the type;
     - grants and target groups reference it through `grants.target_id`;
     - a type grant confers only the type-level abilities checked through `AuthorizeType`, for example `create` and `list`, and **no access to existing instances** of the type;
     - type entities and instance entities cannot share a target group, which the mod-authz slice enforces.

     Link to the dedicated type-entity architecture doc if phase 3 creates one. Otherwise keep the section self-contained.
   - **"mod-core call sites":** the sentence "A non-wildcard actor who owns, or holds a targeted grant over, the entity whose id equals the type id is denied" is no longer accurate as written, because that entity is now usually the type's own type entity. Restate the guarantee as: call sites never pass the type id as an entity target. A grant on an unrelated entity whose id happens to equal the type id confers nothing. A grant on the type's own entity is honoured only through `AuthorizeType`.
   - **Followup `ggLy`:** in "The fallback rule" and "Adopting the helper in a module", soften or source the claims about peer modules (the production Authorizer's nil-target behaviour, which modules carry local helpers, and wildcard holders being unaffected). Either attribute them to the authorization-design contract, or reword them as requirements on implementations rather than statements of fact about other repos.
   - **The `authorization-design.md` paragraph at the end:** keep it, but reword it to say that the correction is tracked in the mod-authz slice of the type-scoped-grants change. Do not name plan or task identifiers.
5. **Style.** Follow the existing tone and wrap width of each file. Doc comments stay gofmt-clean.

## Validation

- `cd api && make lint` passes (gofmt and vet). `cd api && make test` passes.
- `git diff --stat` touches only `api/authz/authz.go` and `docs/architecture/type-level-authorization.md`, excluding `plan/`.
- `git diff -- api/authz/authz.go` shows changes only inside comment lines: no code or signature changes.
- `grep -n "never consults entity ownership or entity-targeted grants\|today, wildcard grants" api/authz/authz.go docs/architecture/type-level-authorization.md` returns nothing.
- `grep -n "entity_id" docs/architecture/type-level-authorization.md` shows the exact-type lookup named.
- Manual read: the doc states "no instance access" and "no subtype inheritance" explicitly.

## Assumptions

- The contract wording describes the post-change behaviour that mod-users' `AuthorizeType` arm implements in its own slice. Until that lands, the production Authorizer is still wildcard-only. The wording is phrased as the capability's contract, which is accurate for any conforming implementation.
- The federated request's Q1 (optional instance semantics) is not adopted at this point. If a later phase adopts it, that phase owns the documented warning.

## References

- [Type entity design note](../notes/type-entity-design.md): the sibling contract.
- `api/authz/authz.go:40-106`: the current doc comments.
- `docs/architecture/type-level-authorization.md`: the current architecture doc.
- Followup `ggLy` (in `plan/followups.yaml` at the project root): the unverifiable peer claims.
- Architect report `mod-authz/.flow/research/qWTH-architect-report.md` §1.6 and §2.1 "Risk": the original rationale for the rewording.
