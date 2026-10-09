# Helpers

## Goals

Add the additive Go and GUI helpers (422 error, `DecodeJSON`, `ParseUUIDParam`, `authz.RequireAuth`, `AuthorizeResolve`, `ordertest`, GUI `unprocessable_entity` routing) with unit tests, so wave-2 conformance plans have a stable API. First phase because docs and wave 2 depend on the exact names.

## Inputs

The survey `.flow/error-order-helpers.md`; existing `apiresp`, `authz`, `entity`, `opctx` packages.

## Outputs

New exported symbols listed in `plan/notes/helper-api-final-signatures.md`; passing `go test ./...` and gui tests.
