# Update Architecture Docs

> **Ephemeral plan document.** This file is ephemeral and will be removed after the implementation is complete. Project documents must be updated or created to capture the context and information contained herein. Link only from other plan documents; do not link to this document from non-plan documents.

## Purpose and scope

Update architecture and spec docs to reflect the handler-conformance changes made by this plan (masked resolve in services, route-level authentication, decode-before-authorization, target-0 authorizer call).

## Requirements

- Implementation task docs that surfaced the architectural implications (plain code text, not links): `plan/phase-01-authenticate-routes/003-mount-require-auth.md`, `plan/phase-02-existence-leaks/001-apps-masked-resolve.md`, `plan/phase-02-existence-leaks/002-service-masked-resolve.md`.
- Files to review: `docs/architecture/type-level-authorization.md`, `docs/architecture/type-entities.md`, `AGENTS.md`, `README.md`, and any `docs/*-spec.md` if present; mention how the httpapi and service layers order authn, shape, authz, and lookup, and the authorizer's target-0 contract.
- role_doc: plugins/flow/roles/architect-backend.md
- Procedure: plugins/flow/task-procedures/update-architecture-docs/SKILL.md
- Write durable content into the project docs; never link the plan task docs listed above (they are ephemeral).

## Validation

- Each named doc was reviewed and updated where behavior is described; no new link from a non-plan doc into `plan/`.
