# Fix Type-Level Authorization Doc Claims

## Purpose and scope

Apply the verified replacement wording from `.flow/peer-auth-findings.md` section C (finding ggLy) to `docs/architecture/type-level-authorization.md`. This closes followup `ggLy` ("Type-level auth doc claims about peers"). The manager removes the followup from `plan/followups.yaml` after the plan merges; this task does not edit `followups.yaml`.

## Requirements

Edit only `docs/architecture/type-level-authorization.md`:

1. Section "The fallback rule": make the sentence read "So an `Authorizer` that lacks the capability, hides it behind a decorator, or is a test stub still fails closed rather than matching an unrelated entity, provided the implementation honors that contract." (adds the proviso).
2. Section "Adopting the helper in a module", opening paragraph: keep the examples (mod-users, mod-authz, mod-workflows) and append: "Note the local helpers differ in edge-case guards (mod-authz does not reject a nil `az` or `typeID <= 0`; mod-users does not reject a nil `az`), so switching also adopts the core guards."
3. The alias bullet ("Replace the local interface with an alias ...") must say it applies only to mod-users' exported `localAuthz.TypeAuthorizer` (which has a compile-time assertion); the mod-authz and mod-workflows interfaces are unexported (`typeAuthorizer`), so for them the step is to delete the local interface and call `authz.AuthorizeType`. Keep "The method signatures must stay identical so structural satisfaction holds."
4. Replace "the local helpers and the core helper have the same method set, so they interoperate" with "the local helpers and the core helper share the same `AuthorizeType` method signature, so they interoperate."
5. Section "mod-core call sites": after "Wildcard `manage` and `create` holders are unaffected" add " (a non-positive type id is denied for everyone)".
6. Preserve all other text, the table, and the typed-nil paragraph. No frontmatter. Follow markdown design/style standards (ATX headings, existing line-wrap style).

## Validation

```sh
cd /path/to/plan/worktree
grep -n "same method set" docs/architecture/type-level-authorization.md   # expect no output
grep -n "same \`AuthorizeType\` method signature" docs/architecture/type-level-authorization.md   # expect 1 match
grep -n "localAuthz.TypeAuthorizer" docs/architecture/type-level-authorization.md   # expect >= 1
grep -n "does not reject a nil" docs/architecture/type-level-authorization.md   # expect 1
git diff --stat   # only this one doc changed
```

Acceptance: the greps behave as stated; the diff touches only `docs/architecture/type-level-authorization.md`.

## References

- `.flow/peer-auth-findings.md` (main checkout) section C, "Proposed wording".
- Followup `ggLy` in the main checkout's `plan/followups.yaml` (removed by the manager post-merge).
