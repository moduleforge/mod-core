# Bump docs/mf-standards Submodule Pin In mod-core

## Purpose and scope

Move mod-core's `docs/mf-standards` submodule gitlink to a pushed docs-mf-standards commit that contains `architecture/http-error-precedence.md` (at least `477e708`), so the links to `docs/mf-standards/architecture/http-error-precedence.md` added in `AGENTS.md`, `README.md` and `docs/architecture/display-name-http.md` resolve in the released mod-core. The gitlink is currently `2c005cfd57a390a9ffb939f8efaab82006beb407`, which lacks that file. This task was added at execution time on the user's decision (followup KJLW, option A). It runs after task 002 (docs-mf-standards `main` is pushed, so the gitlink names a commit fresh clones can fetch) and before task 003 (which pins the pushed mod-core SHA, so that SHA must include this bump).

Because this adds a new commit to mod-core `main` after task 002's push, mod-core `main` must be pushed a second time before task 003. That second push needs its own explicit user consent (see Requirements 5).

## Requirements

1. Preconditions: task 002 done (docs-mf-standards `main == origin/main`), and mod-core `main` is clean. If either fails, halt.
2. Determine the target commit: `git -C /Users/zane/playground/moduleforge/docs-mf-standards rev-parse origin/main` (the pushed tip, which contains the plan's merged work). Confirm `architecture/http-error-precedence.md` exists at that commit and that it is a descendant of the current gitlink (`git merge-base --is-ancestor 2c005cfd57a390a9ffb939f8efaab82006beb407 <target>`). If not, halt and report.
3. Bump the submodule in a simple worktree of mod-core based on current `main` (the manager's standard simple-worktree flow; never edit the main checkout directly). In that worktree, initialize the submodule if needed, fetch the target commit into it (from the local docs-mf-standards clone or from origin), check it out at the target commit, and stage only the gitlink change (`docs/mf-standards`). Do not edit any file inside the submodule. Verify `git diff --cached --stat` shows only the `docs/mf-standards` gitlink, and that `docs/mf-standards/architecture/http-error-precedence.md` now exists in the worktree.
4. Commit as one small commit (message like `bump docs/mf-standards to <short-sha>`), merge it to mod-core `main` via the standard simple-worktree merge, and remove the worktree.
5. **USER CONSENT REQUIRED for the push.** The executing manager MUST ask the user, at execution time, for explicit permission before pushing mod-core `main` a second time, showing `git log --oneline origin/main..main` first. Earlier consent (including task 002's) does not cover this push. If consent is not granted, stop and report `blocked: push consent not granted`; task 003 must not run. After consent: `git -C /Users/zane/playground/moduleforge/mod-core push origin main`, never force-push, and stop on a non-fast-forward rejection. Record the new pushed mod-core SHA in the report; task 003 pins that SHA.

## Validation

```sh
R=/Users/zane/playground/moduleforge/mod-core
git -C $R ls-tree main docs/mf-standards          # gitlink is the new commit, not 2c005cf
git -C $R submodule status docs/mf-standards
git -C $R show main:docs/mf-standards >/dev/null 2>&1 || true
git -C /Users/zane/playground/moduleforge/docs-mf-standards cat-file -e "$(git -C $R ls-tree main docs/mf-standards | awk '{print $3}'):architecture/http-error-precedence.md" && echo "doc present at pinned commit"
git -C $R fetch origin && git -C $R rev-parse main origin/main   # equal after the push
```

Acceptance: mod-core `main`'s `docs/mf-standards` gitlink points at a pushed docs-mf-standards commit containing `architecture/http-error-precedence.md`; only the gitlink changed in the commit; mod-core `main == origin/main`; the report records the user's consent answer and the pushed SHA.

## References

- Followup KJLW (dangling `http-error-precedence.md` links) and the plan's `overview.md` Phase 4 section.
- Task 002 (push) and task 003 (pins); docs-mf-standards `architecture/http-error-precedence.md`.
