# Entity Handlers to DecodeJSON and ParseUUIDParam

> **Ephemeral plan document.** This file is ephemeral and will be removed after the implementation is complete. Project documents must be updated or created to capture the context and information contained herein. Link only from other plan documents; do not link to this document from non-plan documents.

## Purpose and scope

Convert the natural-person, corporation, and service-account create/update handlers, plus entities and display handlers, to the shared stateless-shape helpers so malformed bodies and UUIDs return structured 400s before authorization ([common section 3](../waves/http-error-precedence-conformance/common-conformance-instructions.md)). Parallel-eligible with the apps decode task (disjoint files).

## Requirements

- `natural_persons.go`, `corporations.go`, `service_accounts.go`: replace each `json.NewDecoder(r.Body).Decode` with `apiresp.DecodeJSON`; replace each `uuid.Parse(chi.URLParam(...))` + bare `ErrInvalidInput` in these files plus `entities.go` and `display.go` with `apiresp.ParseUUIDParam("uuid", ...)`.
- Order in update handlers: `ParseUUIDParam`, then `DecodeJSON`, then the service call (which authorizes via the masked resolve). Keep three-state pointer semantics for `ein` etc. (decode targets unchanged).
- No existing success shape or error code outside `invalid_input` details changes; record: unknown fields now rejected, 1 MiB cap, `invalid_json_body`/`invalid_uuid` detail codes.
- `writeCoreServiceErr` is not in mod-core (it is in mod-users); there are no pass-through writers in `httpapi` to delete: confirm via grep and note it in the report.
- Tests: malformed body and malformed UUID give 400 with the new detail codes; unknown field gives 400; existing tests that expected a bare `invalid_input` are updated.

## Validation

- `grep -rn 'json.NewDecoder' api/httpapi` shows only the field-crypto helper (handled by its own task); `cd api && go test ./...` passes.

## Metadata

architectural_impact: false
