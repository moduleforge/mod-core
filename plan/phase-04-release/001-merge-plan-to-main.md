# Merge Plan Branch To Main

## Purpose and scope

Merge the `plan/error-precedence-foundation` branch into mod-core `main` using the standard plan merge flow (the manager-level plan-merge skill, e.g. `finalize-completed-plan-documents` followed by the standard merge), then confirm the aggregate mod-core checkout at `/Users/zane/playground/moduleforge/mod-core` is on the new `main`. This is a manager-executed mechanics task. Local only: no push (task 002).

## Requirements

1. Precondition: phases 1 to 3 are done and reviewed; the plan has no open findings; `cd api && go build ./... && go test ./...` and `cd gui && bun run test && bun run typecheck` pass on the plan branch.
2. Concurrent plan `type-scoped-grants` is `working` in the same repo (worktree `worktrees/plan/type-scoped-grants`). Expect textual conflicts only in shared docs (for example `AGENTS.md`); resolve by preserving both sides. Do not merge or modify the other plan's branch.
3. Follow the standard plan merge flow; do not hand-roll git history rewrites. Session-specific plan docs are deleted by the finalize step, per the plan documents handling protocol.
4. After merge, in the aggregate checkout confirm: current branch is `main`, `git log -1` is the plan merge, working tree is clean, and `git worktree list` no longer needs the plan worktree (clean it up via the standard flow).
5. After merge the manager removes followup `ggLy` from `plan/followups.yaml` (closed by phase 2 task 001) via the flow-mcp followups tool.
6. Record the resulting `main` commit SHA in the task report; later tasks use it.

## Validation

```sh
cd /Users/zane/playground/moduleforge/mod-core
git branch --show-current                  # main
git status --short                         # empty
git log --oneline -3
git merge-base --is-ancestor plan/error-precedence-foundation HEAD && echo merged
cd api && go build ./... && go test ./...
```

Acceptance: `main` contains the plan commits; build and tests pass on `main`; `ggLy` is gone from followups.

## References

- Project Plan Document Standards (plan documents handling protocol).
