# mod-core — next steps

This file tracks pending manual verification and deferred work for `mod-core`. Architecture decisions made across the cross-cutting framework rounds and the SQL access-function refactor are documented in `docs/mf-standards/architecture/` and indexed by `plan/report.phase-1-sql-access-fn-handoff.md` at the user-components root.

## Pending manual verification (needs live stack / DB)

- **`make dev.start` end-to-end smoke** — bring up the full stack, log in, edit profile at `/profile`, reload to confirm persistence; create a user via admin; edit an existing user.
- **`goose status` against a freshly-migrated local DB** — confirm migrations applied in order: core (`0001_baseline.sql`, range `0001–0099`) → mod-audit (`0400_audit_log.sql`) → mod-users (`0100–0104`) → mod-tags (`0200–0299`) → mod-contacts (`0300–0399`) → mod-authz (`0500–0506`).
- **Audit log smoke** — after a profile edit, verify `SELECT op, resource, actor_entity_id FROM audit_log ORDER BY at DESC LIMIT 5` shows `op='update'`, `resource='natural_person'`, populated `actor_entity_id`, populated `before`/`after`. (Note: column is `actor_entity_id`, not `actor_user_id` — schema generalized in Phase 3.)
- **Access function inlining spot-check** — VERIFIED in Phase 2.5 against the Phase 2 schema (authz tables + tags table). EXPLAIN ANALYZE on `accessible_tag_ids_for_actor(1, ARRAY[1,2,3,4,5,6,7]::int[])` showed the CTE body inlined directly (no "Function Scan" node). Retained here for future re-verification after Phase 3 access-table migration.
- **Tax-id round-trip** — verify `Cipher.Decrypt` on an empty-but-non-NULL bytea cleanly yields `""` against live Postgres. Inspect raw column (`SELECT encode(ssn, 'hex') FROM natural_persons WHERE entity_id = <id>;`) to confirm the plaintext SSN bytes are NOT present.

## Pre-existing code-level gaps

- `EntityService.GetByID` returns `ErrNotFound` unconditionally — no caller uses it today; sqlc query was never added. Add when needed.
- Entity forms (`<NaturalPersonForm>`, `<CorporationForm>`, `<ServiceAccountForm>`) exist in core-gui but the admin user-edit page in mod-users renders its own inline form. Consolidation deferred ("leave admin pages alone — we can consolidate later").

## Tax-id encryption — future hardening

- **Key rotation.** No mechanism for rotating the AES-256-GCM key. A future hardening step would embed a key-id prefix in the blob and introduce a re-encryption migration.
- **AAD binding.** Ciphertext is not bound to the row's primary key, so a ciphertext could in principle be copied between rows. A source comment documents this as future work.

## Phase 1 (SQL access-fn) — Phase 1 followups

These surfaced during the round and are not blocking; capture before context-clear:

- **`GrantTableGenerator` + slug-list maintenance.** `mod-users/api/cmd/server/main.go` hardcodes the slug list passed to `setup.ApplyFuncs`; `GrantTableGenerator.GenerateForResource` hardcodes per-slug bodies. Adding a new entity type requires updating both. Acceptable today; defer a registration helper until a real new-entity-type scenario.
- **`EntityResolver.AllowNotFound` is unused.** No resource has opted into 404 transparency. Reasonable default for a privacy-conservative system; flag if a UI starts relying on 404 to distinguish missing vs. forbidden.

## Phase 3: trigger-maintained access tables

If indirect-grant fan-out (e.g. group-of-users grants on org-of-entities) creates measurable contention or perf issues at scale, per-resource `*_access` tables (e.g. `tag_access(actor_entity_id, tag_entity_id, can_read)`) materialise the grants for fast index-scan JOIN. Triggers on `grants` keep access tables in sync. The function bodies become simple `SELECT entity_id FROM <resource>_access WHERE actor_entity_id = $1 AND can_read` queries — still inlinable.

This is a Phase 3 evolution path; the Phase 2 generator is sufficient for the foreseeable simple-grant case.

## Mechanical CRUD (architect Proposal 6)

The architect's evaluation across rounds 4–5 suggested that several CRUD operations could be mechanized once the standard service-method shape stabilised:

- **Fully mechanizable today:** `Delete` (archive) for any registered Entity. The sequence `Authorize → txhelper.Run(ArchiveEntity → Observe) → ObserveAfterCommit` is purely entity-level with no per-type variation.
- **Mostly mechanizable:** `Get` (entity-level fields only); `List` (entity-level filter by type slug, archived status, time range) once Phase D's per-resource queries stabilise.
- **Partially mechanizable:** `Create` and `Update` have skeleton consistency but per-type variation (validation, encryption, multi-table writes, sub-type dispatch in CTI).
- **Out of scope:** Tags (two-principal model), contacts (not entities).

A speculative `EntityDescriptor` registry sketch is in the round-1 handoff report. Not in scope for any current phase; revisit when the service-method shape has been stable across a few new modules and the mechanical-vs-handcrafted boundary becomes obvious.

## Component workbench (Ladle)

See `stories-next.md` at this module's root for the deferred Ladle / Storybook follow-ups (story coverage gaps, Storybook migration path, a11y / theme / MSW addons, visual-regression options).
