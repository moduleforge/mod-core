# Migrate mod-core to a single .flow/ ignore entry

## Purpose and scope

This is wave 2 (wave-2-migration) of the wave plan `flow-ignore-canonicalization`, plan-group `migrate-moduleforge`. Flow now treats `.flow/` as fully git-ignored; this plan migrates the single project `mod-core` accordingly. One project, one task.

## Overview

Replace every flow-related ignore line in `mod-core`'s `.gitignore` (inventory: 9:# Runtime working dirs flow writes into the project under management.; 10:# .flow/* (not .flow/) so that !.flow/plans negation can take effect.; 17:.flow/*; 18:!.flow/plans; 19:!.flow/project-analysis.json; 20:!.flow/what-next-cache.json) with one `.flow/` entry, and untrack any tracked `.flow` content with `git rm -r --cached .flow` (inventory: 3: .flow/binding.md .flow/project-analysis.json .flow/what-next-cache.json). Files stay on disk. One commit touches only `.gitignore` and the `.flow` untracking.

## Phases

| Phase | Slug | Task | Tier |
|---|---|---|---|
| 12 | flow-ignore-migrate | migrate-gitignore | sonnet-low |

## Risks

The main checkout is `dirty(1)`. If that includes tracked modifications, close-out (`worktree-merge.sh` into the working branch) is refused by the merge; the owner may need to commit or stash first, or use the submit_pr strategy. The task itself is unaffected because it runs in an isolated worktree.
