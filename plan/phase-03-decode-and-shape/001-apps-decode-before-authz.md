# Apps Decode and UUID Parse Before Authorization

> **Ephemeral plan document.** This file is ephemeral and will be removed after the implementation is complete. Project documents must be updated or created to capture the context and information contained herein. Link only from other plan documents; do not link to this document from non-plan documents.

## Purpose and scope

Make `PUT /apps/{uuid}` (and Create) follow 401, 400, 403, 404, 409/422: parse the path UUID and decode the body before the authorize step. Today UpdateApp authorizes and loads the row first, then decodes ([common section 3](../waves/http-error-precedence-conformance/common-conformance-instructions.md)). Depends on the apps masked-resolve task (same file).

## Requirements

- `UpdateApp`: `apiresp.ParseUUIDParam("uuid", chi.URLParam(r, "uuid"))` then `apiresp.DecodeJSON` then `AuthorizeResolve` then the real `GetAppByUUID`. Apply the same `ParseUUIDParam` to GetApp and DeleteApp (remove `loadAppByUUIDParam`'s bare `uuid.Parse` error).
- `Create`: replace the `json.NewDecoder` block with `DecodeJSON` (default cap, strict). The existing order (decode, trim/required checks `apps.slug_required`/`apps.name_required`, then `AuthorizeType`) already puts shape before authorization, which the spec allows; keep it and do not mark the route `AuthzFirst`.
- Move UpdateApp's trim/blank handling that depends only on the body into the stateless phase; slug-uniqueness conflicts are handled in the 409/422 audit task.
- Tests: bad body + unauthorized caller = 400 for Update; malformed UUID + anonymous = 401; unknown field rejected; body over cap rejected.

## Validation

- `cd api && go test ./httpapi/...` passes; `grep -n 'json.NewDecoder' api/httpapi/apps.go` is empty.

## Metadata

architectural_impact: false
