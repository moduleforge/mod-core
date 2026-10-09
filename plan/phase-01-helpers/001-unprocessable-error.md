# Unprocessable Error

## Purpose and scope

Add the 422 sentinel and constructor to `api/apiresp`, wired into the existing `classify` and `publicMessage` mapping (no second mapper). Survey: `.flow/error-order-helpers.md` finding I and "Recommended helpers" item 3 (no 422 exists anywhere in the ecosystem today). Additive only.

## Requirements

Exact final API (frozen; see `plan/notes/helper-api-final-signatures.md`):

```go
var ErrUnprocessable = errors.New("unprocessable")
func Unprocessable(details ...FieldError) error
```

1. `api/apiresp/errors.go`: add `ErrUnprocessable` to the sentinel `var (...)` block, with doc comment (semantically invalid body after authorization, e.g. a referenced value that does not exist or lies outside a registry; reserved code `unprocessable_entity`, status 422).
2. New file `api/apiresp/unprocessable.go` modelled on `conflict.go`: unexported `unprocessableError` carrying `[]FieldError`, `Error()` returns `ErrUnprocessable.Error()`, `Unwrap()` returns `ErrUnprocessable`, `fieldDetails()` implements the existing `fieldDetailCarrier`. `Unprocessable(details ...FieldError) error` returns it. Zero details is valid and omits `error.details`.
3. `api/apiresp/writer.go`: add a `classify` case `errors.Is(err, ErrUnprocessable)` returning `("unprocessable_entity", http.StatusUnprocessableEntity)` (place it before the `default`; ordering relative to the others is irrelevant unless an error wraps two sentinels, in which case keep existing precedence unchanged), and a `publicMessage` case returning `"the request references a value that is not valid"`. Update the `WriteError`/`classify` doc comments only if they enumerate sentinels.
4. `api/service/errors.go`: add `var ErrUnprocessable = apiresp.ErrUnprocessable` with a doc comment, matching the other aliases.
5. If `api/openapi.fragment.yaml` documents the error envelope codes or a 4xx response set, add `unprocessable_entity`/422 consistently; if it does not, leave it unchanged.
6. Do not touch `WriteActionRequired` (its statuses stay 403/409/503).

Tests in `api/apiresp/` (follow the style of `conflict_test.go` and `apiresp_test.go`):

- `errors.Is(Unprocessable(), ErrUnprocessable)` and a `fmt.Errorf("x: %w", ErrUnprocessable)` wrap both classify to `unprocessable_entity` / 422.
- `WriteError` with `Unprocessable(FieldError{...})` yields status 422, envelope code `unprocessable_entity`, the public message above, and `details` present; with no details the `details` key is absent from the JSON.
- The error text of the underlying error is never echoed.
- A table check that every pre-existing sentinel still maps exactly as before (regression guard).

## Validation

Run from the plan worktree:

```sh
cd api && gofmt -l apiresp service && go build ./... && go vet ./... && go test ./apiresp/... ./service/... && go test ./...
grep -n "unprocessable_entity" apiresp/writer.go
git diff --stat   # additive: no deleted exported identifiers
```

Acceptance: all commands succeed; `gofmt -l` prints nothing; the diff removes no existing exported symbol.

## Metadata

architectural_impact: true

## References

- `.flow/error-order-helpers.md` (main checkout): finding I, item 3.
- `api/apiresp/conflict.go`, `api/apiresp/invalidinput.go` (`fieldDetailCarrier`), `api/apiresp/writer.go`.
- `plan/notes/helper-api-final-signatures.md`.
