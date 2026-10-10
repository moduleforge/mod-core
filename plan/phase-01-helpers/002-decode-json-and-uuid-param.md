# Decode JSON And UUID Param

## Purpose and scope

Add `apiresp.DecodeJSON` (size cap, unknown-field rejection, trailing-data rejection, optional `Validator` hook) and `apiresp.ParseUUIDParam`. Survey: `.flow/error-order-helpers.md` finding 2 and 3 (about 35 copy-pasted decode blocks, inconsistent caps and codes; security-relevant gaps in mod-core httpapi, mod-authz and mod-users) and "Recommended helpers" item 2. Includes ParseUUIDParam because the survey shows the same parse and `invalid_uuid` error hand-written in mod-workflows (`parseUUIDParam`/`invalidUUIDErr`), mod-tasks (inline) and mod-core `apps.go`. Do not migrate any existing call site (wave 2). Additive only.

## Requirements

Exact final API (frozen; see `plan/notes/helper-api-final-signatures.md`), new file `api/apiresp/decode.go` (and `api/apiresp/uuidparam.go` for the second helper):

```go
type Validator interface{ Validate() []FieldError }
const DefaultMaxBodyBytes = 1 << 20
type DecodeOption func(*decodeConfig)
func WithMaxBytes(n int64) DecodeOption
func WithCodePrefix(p string) DecodeOption
func WithAllowUnknownFields() DecodeOption
func DecodeJSON(w http.ResponseWriter, r *http.Request, dst any, opts ...DecodeOption) bool
func ParseUUIDParam(name, value string) (uuid.UUID, error)
```

`DecodeJSON` behavior:

1. Wraps `r.Body` with `http.MaxBytesReader(w, r.Body, max)` (default `DefaultMaxBodyBytes`; `WithMaxBytes(n)` with `n <= 0` keeps the default).
2. Uses `json.NewDecoder`; calls `DisallowUnknownFields()` unless `WithAllowUnknownFields()` is given.
3. Decodes into `dst`, then rejects trailing data (a second `Decode` must return `io.EOF`).
4. If `dst` implements `Validator`, calls `Validate()` after a successful decode and treats a non-empty result as a failure carrying those `FieldError`s (the Validator hook is for stateless checks only; document that).
5. On ANY failure it writes the response itself with `WriteError(w, r, InvalidInput(...))` (status 400) and returns `false`; on success returns `true` and writes nothing. It never touches a database or service.
6. Failure detail codes: `<prefix>.invalid_json_body` for malformed JSON, empty body, unknown field, trailing data and wrong types; `<prefix>.body_too_large` when `*http.MaxBytesError` is hit; with no prefix the codes are unprefixed `invalid_json_body` and `body_too_large`. `Field` is empty for body-level errors (or the offending field name when `json.UnmarshalTypeError.Field` / the unknown-field message identifies it). `Message` must be client-safe and must not echo raw decoder internals beyond a short fixed description plus (optionally) the field name.
7. Validator failures are returned as the validator's own `FieldError`s unmodified.
8. A nil `r.Body` or nil `dst` must not panic (treat nil body as empty body failure; nil/non-pointer `dst` is a programmer error, so return `false` after writing a 500 via `WriteError(w, r, fmt.Errorf(...))` rather than panic).

`ParseUUIDParam(name, value)`: `uuid.Parse(value)`; on failure return `InvalidInput(FieldError{Field: name, Code: "invalid_uuid", Message: "must be a valid UUID"})`. Document that callers pass `chi.URLParam(r, name)` and do the parse before any authorization or DB access only when rule stage 2 (400) applies; this helper does not touch the request.

Tests (`api/apiresp/decode_test.go`, `uuidparam_test.go`): well-formed body decodes; malformed JSON, empty body, unknown field (rejected by default, accepted with `WithAllowUnknownFields`), trailing data, wrong type, oversized body (`WithMaxBytes(16)`; also default cap boundary at `DefaultMaxBodyBytes+1` using a small override rather than allocating 1 MiB where possible), prefix applied to codes, Validator failures surface verbatim, Validator not called when decode fails, response is a 400 `invalid_input` envelope and nothing is written on success, ParseUUIDParam valid/invalid/empty.

## Validation

```sh
cd api && gofmt -l apiresp && go build ./... && go vet ./... && go test ./apiresp/... && go test ./...
grep -n "func DecodeJSON\|func ParseUUIDParam\|func WithMaxBytes\|func WithCodePrefix\|func WithAllowUnknownFields" apiresp/*.go
```

Acceptance: commands succeed; the five greps match exactly the frozen signatures; `go list -deps ./apiresp | grep -c go-chi` is 0 (no chi dependency); no existing exported symbol removed.

## Metadata

architectural_impact: true

## References

- `.flow/error-order-helpers.md` (main checkout): findings 2 and 3, item 2.
- Existing blocks to mirror semantics of: mod-tasks `tasks.go:165-172`, mod-workflows `response.go` (`maxJSONBodyBytes`), mod-users `internal/server/respond.go:50`.
- `plan/notes/helper-api-final-signatures.md`.

## Status

Outcome: succeeded (2026-10-10). Added `api/apiresp/decode.go`, `api/apiresp/uuidparam.go` and tests. gofmt, build, vet, `go test ./...` green; no chi dependency in `apiresp`. Unknown-field name is deliberately not echoed in `Field` (client-safe message).
