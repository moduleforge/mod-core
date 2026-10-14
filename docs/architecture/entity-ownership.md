# Entity ownership

`entities.owner_id` (defined in `0001_baseline.sql`) names the entity that owns a row. The authorization
"own arm" treats `owner_id = actor` as satisfying **every** operation on the owned entity, so ownership is
authority, not metadata.

## Rules

| Rule | Mechanism |
| --- | --- |
| `owner_id` defaults to the entity's own id for `natural_person` and `service_account` (and descendants) when the insert leaves it NULL | `BEFORE INSERT` trigger `entities_owner_self_default` |
| `owner_id` is immutable after insert | `BEFORE UPDATE OF owner_id` trigger `entities_owner_immutable` |
| **A `natural_person` (or descendant) is always owned by itself; no other owner is accepted** | `BEFORE INSERT OR UPDATE OF owner_id, fundamental_type_id` trigger `entities_owner_self_natural_person` raising SQLSTATE `MF002` |
| Other types (corporation, tasks, repos, ...) take whatever owner the creator passes, including NULL | no rule |

## The natural_person invariant

Why: the own arm confers credential-control authority (for example registering SSH keys, or assuming the
account) over whatever the actor owns. If a person could be owned by another entity, that owner would gain
standing control of a human's credentials. Without the invariant, the default trigger only fills a NULL owner; an explicit
non-self owner supplied to `core_create_entity_with_owner` (`CreateEntityWithOwner`) or a raw `INSERT` would be
accepted. No code path in the platform does that, so the route is latent; the invariant makes it structurally
unreachable.

Behaviour:

- `INSERT` of a natural_person with `owner_id` NULL or its own id succeeds (self-owned).
- `INSERT` with any other owner fails with SQLSTATE `MF002` (`types.IsNaturalPersonOwnerError`).
- `UPDATE` that changes `owner_id` is rejected. `entities_owner_immutable` fires first (alphabetical trigger
  order) with its own message; `entities_owner_self_natural_person` is the independent backstop.
- Go callers that pass an explicit owner should use `service.CreateEntityWithOwner`, which maps `MF002` to an
  error wrapping `service.ErrInvalidInput` with fixed text (the raw SQL error is not exposed).
- Creating a natural_person never needs an owner: use `CreateEntity`.

### service_account

`service_account` is deliberately **not** covered by the invariant. It gets the same default-to-self treatment
as natural_person (`entities_owner_self_default`), but an
explicit non-self owner is still accepted. Whether service accounts should be handled alongside natural_person
is under separate review; do not extend `entities_owner_self_natural_person` to them without that decision.

## Rollout

`0001_baseline.sql` creates `entities_owner_self_natural_person` together with the `entities` table, so the trigger
exists before any row does and no existing-row scan is needed. The pre-release migration history was collapsed
into that baseline; there is no separate migration for the invariant and no Down section. A database that
recorded the old versions in `goose_db_version_core` must be dropped and recreated (see the reset section of
`model/README.md`); it cannot be upgraded in place.
