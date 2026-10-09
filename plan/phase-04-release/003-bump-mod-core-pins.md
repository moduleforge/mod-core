# Record New mod-core Pin In Four Lockfile Repos

## Purpose and scope

After the push (task 002), record the new pushed mod-core commit in `versions.lock.yaml` for the four lockfile-carrying repos: mod-users, app-mftodo, app-mfgit, app-mfmanager. Local builds use the relative go.mod replace; pins matter only for CI and fresh clones, so they must name a pushed commit. Each repo gets its own small commit made in a separate simple worktree; never edit the main checkouts.

## Requirements

1. Precondition: task 002 done (mod-core `main == origin/main`). If it is not, halt.
2. For each repo R in `mod-users app-mftodo app-mfgit app-mfmanager` (independent, may run in parallel): create a simple worktree of R from its current `main` (the manager's standard simple-worktree flow), then from inside that worktree run
   `MODULEFORGE_SIBLINGS_DIR=/Users/zane/playground/moduleforge make pins.update REPOS="mod-core"`
   (the script `scripts/update-pins.sh` rewrites SHAs from each pinned sibling's current `origin/main`, never local HEAD, which is why the push must come first). Do not pass other repos in `REPOS`.
3. Verify the diff in each worktree changes only the `mod-core` entry of `versions.lock.yaml` to the pushed SHA from task 002; if the script changes anything else, revert the extra changes and note it.
4. Commit in each worktree as one small commit (message like `pin mod-core to <short-sha>`), merge it to that repo's `main` via the standard simple-worktree merge, and remove the worktree. Do not push these repos unless the user explicitly asks; report that the pin commits are local-only.
5. Note: mod-users has a local-only pin bump `6bab8f5` already on `main`; base the new worktree on current `main` so it builds on it.

## Validation

```sh
SHA=$(git -C /Users/zane/playground/moduleforge/mod-core rev-parse origin/main)
for r in mod-users app-mftodo app-mfgit app-mfmanager; do
  grep -n "mod-core" -A3 /Users/zane/playground/moduleforge/$r/versions.lock.yaml | grep -c "$SHA"
  git -C /Users/zane/playground/moduleforge/$r status --short   # empty
  git -C /Users/zane/playground/moduleforge/$r log --oneline -1
done
git -C /Users/zane/playground/moduleforge/mod-core branch -r --contains "$SHA"   # contains origin/main
```

Acceptance: each of the four lockfiles pins the pushed mod-core SHA; each main checkout is clean with a separate pin commit; no main checkout was edited directly.

## References

- `scripts/update-pins.sh` and the `pins.update` target in each repo's Makefile.
- docs-mf-standards `versions-lockfile.md` (lists the four lockfile carriers; corrected by the docs-mf-standards plan).
