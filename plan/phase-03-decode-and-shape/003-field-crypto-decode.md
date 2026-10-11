# Field Crypto Keys Decode and Ordering

> **Ephemeral plan document.** This file is ephemeral and will be removed after the implementation is complete. Project documents must be updated or created to capture the context and information contained herein. Link only from other plan documents; do not link to this document from non-plan documents.

## Purpose and scope

`httpapi/field_crypto_keys.go` has a bespoke decoder (`decodeFieldCryptoKeyBody`, 4 KiB cap, strict, empty body allowed, raw bytes returned for `fieldCryptoKeyBodyHasKey`) and authorizes (`manage`, nil target) BEFORE decoding. Bring it onto `DecodeJSON` without losing its semantics ([common section 3 usage notes on empty bodies](../waves/http-error-precedence-conformance/common-conformance-instructions.md)).

## Requirements

- Decide and record the ordering: the spec's only authz-before-shape exception is `AuthorizeType`; a nil-target `manage` authorize is not it. Default decision: reorder so shape validation (decode, `{version}` param, stateless field checks) runs before the authorize on Rotate/SetGrace/MarkCompromised-style routes, UNLESS the audit finds a concrete reason (the existing comment says the authorize is the sole gate on post-commit `Cipher.Reload`; that is still satisfied by reordering since decode has no side effects). If you keep authz-first for any route, say why in the code comment and the order test (`AuthzFirst: true`) and flag it in the report for the manager.
- Replace `decodeFieldCryptoKeyBody` with `DecodeJSON(..., WithMaxBytes(maxFieldCryptoKeyBodyBytes))` (keep the 4 KiB cap and existing detail codes where they exist). Preserve: an EMPTY body decodes to the zero request on routes where that is the documented default (Rotate); and the absent-vs-null distinction used by `fieldCryptoKeyBodyHasKey` (read once, restore `r.Body`, or decode into a type that records key presence).
- Replace the `{version}` parser's ad-hoc 400 with the same code (`field_crypto_keys.version_invalid`) placed before authorization.
- Tests: unknown member rejected; oversize rejected; empty body still a standard rotation; absent vs null grace still distinguished; anonymous bad body = 401; unauthorized bad body = 400 (or 403 if kept authz-first and flagged).

## Validation

- `cd api && go test ./httpapi/...` passes, including `field_crypto_keys_test.go`; integration tests in `field_crypto_keys_integration_test.go` pass.
- `grep -n 'decodeFieldCryptoKeyBody' api/httpapi` empty or only the thin shim.

## Metadata

architectural_impact: false
