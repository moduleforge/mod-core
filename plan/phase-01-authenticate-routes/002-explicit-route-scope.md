# Set Explicit Authenticated Scope on Core Router Route

> **Ephemeral plan document.** This file is ephemeral and will be removed after the implementation is complete. Project documents must be updated or created to capture the context and information contained herein. Link only from other plan documents; do not link to this document from non-plan documents.

## Purpose and scope

mfgen treats an empty route `scope` as public (mfgen followup VFHF), and mod-core's `NewRouter` manifest route has none. Set it explicitly so a composing app's generated server mounts its `requireAuth` in front of `/v1/entities/*` before any inline check is removed. Work on mfgen itself is out of scope and not planned; the VFHF fix removes the underlying hazard for every module. Standing rules: [common section 4 and section 7](../waves/http-error-precedence-conformance/common-conformance-instructions.md).

## Requirements

- In `moduleforge.module.yaml`, add `scope: authenticated` to the `provides.routes` entry with `mountFromModule: corehttpapi.NewRouter` (the `/v1` entry with `innerMount: true`). The apps and field-crypto entries already carry `scope: authenticated`: confirm, do not change.
- Update the route comment to say why the scope is explicit (empty scope is public in mfgen; reference followup VFHF by title only in prose, not in a plan link).
- Check `docs/mf-standards` manifest docs and README/AGENTS mentions of the manifest routes and keep them consistent.

## Validation

- `grep -n 'scope' moduleforge.module.yaml` shows `authenticated` on all three route entries.
- The manifest still parses (run the repo's manifest/verification target if one exists, otherwise a YAML parse).
- `make test` unaffected.

## Metadata

architectural_impact: false
