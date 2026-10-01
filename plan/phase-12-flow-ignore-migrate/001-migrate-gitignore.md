# Migrate Gitignore To Single Flow Entry

## Purpose and scope

Wave 2 of wave plan `flow-ignore-canonicalization` (plan-group `migrate-moduleforge`) for project `mod-core` (class `both`, branch `main`). Make `.gitignore` contain a single `.flow/` entry and untrack any tracked `.flow` content, in one commit, inside this plan's task worktree.

## Requirements

1. Work only inside the task worktree; never touch the main checkout.
2. In `.gitignore`, remove every flow-related ignore line and comment block that the single entry supersedes, and add exactly one `.flow/` line preceded by the comment `# Flow per-machine runtime state; durable state lives in plan/ and flow/.`. Keep every unrelated line untouched; create `.gitignore` if absent. Current flow-related lines per the inventory:
  - line 9: `# Runtime working dirs flow writes into the project under management.`
  - line 10: `# .flow/* (not .flow/) so that !.flow/plans negation can take effect.`
  - line 17: `.flow/*`
  - line 18: `!.flow/plans`
  - line 19: `!.flow/project-analysis.json`
  - line 20: `!.flow/what-next-cache.json`
   Also inspect the comment lines adjacent to these (and any comment that only explains the removed `.flow/*` / negation scheme) and remove those that become meaningless; leave comments that describe unrelated rules.
3. Tracked `.flow` paths per the inventory (3):
  - `.flow/binding.md`
  - `.flow/project-analysis.json`
  - `.flow/what-next-cache.json`
   If any are listed (or `git ls-files .flow` is non-empty in the worktree), run `git rm -r --cached .flow` (files stay on disk) and include the removals in the same commit.
4. Commit only `.gitignore` and the `.flow` untracking, with message `chore: ignore .flow/ via a single canonical entry` (add a line noting `git rm --cached` of `.flow` if applicable), following the project's normal commit conventions. Do not change any other file.

## Validation

- `git check-ignore -v .flow/x` reports a match on `.gitignore` for the `.flow/` line.
- `git ls-files .flow` is empty.
- `git diff <base>..HEAD --stat` lists only `.gitignore` (and removed `.flow/*` paths); no other file changed.
- If the project has fast checks relevant to `.gitignore` (none expected), they still pass.

## Assumptions

- The Flow release containing the `.flow/` enforcement (wave 1) is installed.
- The main checkout's uncommitted work is untouched because the task runs in an isolated worktree.
- At close-out (`worktree-merge.sh` into the working branch), a main checkout with TRACKED uncommitted modifications is refused by the merge. Inventory dirty state for this project: `dirty(1)`. If that includes tracked changes, the owner may need to commit/stash or use the submit_pr strategy.

## References

- Wave plan: `flow-ignore-canonicalization` (lead project `sdlcforge/flow`), wave `wave-2-migration`, plan-group `migrate-moduleforge`.
- Inventory: `/Users/zane/playground/.flow/flow-ignore-migration-inventory.md` (row `moduleforge/mod-core`).
