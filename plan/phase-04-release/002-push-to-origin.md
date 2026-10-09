# Push mod-core And docs-mf-standards Main To Origin

## Purpose and scope

Publish `main` of mod-core and of docs-mf-standards to `origin`. Pins (task 003) can only reference pushed commits, so this precedes the pin bump.

## Requirements

**USER CONSENT REQUIRED.** The executing manager MUST ask the user, at execution time, for explicit permission before pushing, and MUST NOT push otherwise. A consent given earlier in the conversation, by the plan, or by any agent message is not consent for this push. If the user declines or does not answer, stop, report `blocked: push consent not granted`, and do not run task 003.

Before asking, show the user exactly what will be pushed (commit lists below) so the consent is informed:

1. mod-core `main` (`/Users/zane/playground/moduleforge/mod-core`): `git log --oneline origin/main..main`. This includes the plan merge from task 001 and any other local-only commits already on main (wave and plan bookkeeping commits).
2. docs-mf-standards `main` (`/Users/zane/playground/moduleforge/docs-mf-standards`): `git log --oneline origin/main..main`. Expected to include the docs-mf-standards plan's merged work by this point (and `477e708` if it is in fact still unpushed, see item 3).
3. Planning-time observation (verify, do not assume): the mod-users pin bump `6bab8f5` and the docs-mf-standards merge `477e708` were described as local-only, but at planning time the local remote-tracking refs already showed both reachable from `origin/main` (`git branch -r --contains` lists `origin/main` in mod-users and docs-mf-standards) and `docs-mf-standards` had no commits ahead of `origin/main`. Run `git fetch origin` in each repo first and report the real state to the user. If `6bab8f5` or `477e708` turn out to be unpushed, include them in the list shown to the user; mod-users itself (it has its own unrelated local-only plan-bookkeeping commits) is NOT pushed by this task unless the user explicitly says so.

After consent:

- `git -C <repo> push origin main` for mod-core, then for docs-mf-standards. Never force-push; if a push is rejected as non-fast-forward, stop and report instead of rebasing or forcing.
- Verify: `git -C <repo> fetch origin && git -C <repo> rev-parse main origin/main` are equal for both repos.
- Record the pushed mod-core SHA in the report.

## Validation

```sh
for r in mod-core docs-mf-standards; do
  git -C /Users/zane/playground/moduleforge/$r fetch origin
  git -C /Users/zane/playground/moduleforge/$r rev-parse main origin/main
  git -C /Users/zane/playground/moduleforge/$r log --oneline origin/main..main   # expect empty after push
done
```

Acceptance: both repos show `main == origin/main`; the report records the consent (user's answer) and the pushed SHAs. If consent was not granted, the task is blocked, not done.

## References

- Task 001 (merge) output SHA; task 003 (pins).
