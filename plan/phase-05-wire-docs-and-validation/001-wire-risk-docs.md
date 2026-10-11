# Document Wire Risks and Helper Usage

> **Ephemeral plan document.** This file is ephemeral and will be removed after the implementation is complete. Project documents must be updated or created to capture the context and information contained herein. Link only from other plan documents; do not link to this document from non-plan documents.

## Purpose and scope

Record every wire-visible change in the project docs that describe the HTTP API ([common rule 4 and 5 in section 4](../waves/http-error-precedence-conformance/common-conformance-instructions.md)). Architecture docs are handled by the doc-updates phase; this task covers API-facing docs.

## Requirements

- Update `api/openapi.fragment.yaml` (status codes, 403 for missing, 401, 400 shapes, 422/409 if any), `README.md`, and `AGENTS.md` (the httpapi row) with: the order, 404 to 403 on `/v1/apps/{uuid}` for ALL callers (admins included) and on entity update/archive misses (HTTP and in-process), 401 instead of 400 for unauthenticated bad bodies/UUIDs, unknown fields rejected, 1 MiB cap (4 KiB for field-crypto), empty-body behavior, sudo-only identities now authenticated, new detail codes.
- Add the authorizer note: `AuthorizeResolve` issues an extra `Authorize(ctx, op, &0)` on every masked miss; implementations must deny target 0, and audit logs will show these calls.
- Name downstream clients that may notice (core-gui `useApiError`, mod-users in-process callers, composing apps' GUIs).

## Validation

- Docs contain each listed wire change; links resolve; no link from these project docs into `plan/`.

## Metadata

architectural_impact: false

## Status

- Outcome: succeeded (2026-10-11).
- `api/openapi.fragment.yaml`: new "Error precedence" section in `info.description` (all seven wire changes, authorizer target-0 note, downstream clients); 403-for-missing notes and 404 narrowed to race-only on entity PUT/DELETE; field-crypto body-validation/4 KiB notes; BadRequest/Unauthorized/Conflict response text updated. YAML parses.
- `README.md` and `AGENTS.md` (`httpapi/` row): wire changes and authorizer note added.
- Validation: all changes present; no `plan/` links in the three docs. The fragment has no `/apps` paths, so apps behavior is described in prose only.
