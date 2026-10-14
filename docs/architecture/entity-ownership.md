# Entity ownership

`entities.owner_id` (added by `0013_entity_ownership.sql`) names the entity that owns a row. The authorization
"own arm" treats `owner_id = actor` as satisfying **every** operation on the owned entity, so ownership is
authority, not metadata.

## Rules

| Rule | Mechanism | Migration |
| --- | --- | --- |
| `owner_id` defaults to the entity's own id for `natural_person` and `service_account` (and descendants) when the insert leaves it NULL | `BEFORE INSERT` trigger `entities_owner_self_default` | `0013` |
| `owner_id` is immutable after insert | `BEFORE UPDATE OF owner_id` trigger `entities_owner_immutable` | `0013` |
| **A `natural_person` (or descendant) is always owned by itself; no other owner is accepted** | `BEFORE INSERT OR UPDATE OF owner_id, fundamental_type_id` trigger `entities_owner_self_natural_person` raising SQLSTATE `MF002` | `0100` |
| Other types (corporation, tasks, repos, ...) take whatever owner the creator passes, including NULL | no rule | |

## The natural_person invariant

Why: the own arm confers credential-control authority (for example registering SSH keys, or assuming the
account) over whatever the actor owns. If a person could be owned by another entity, that owner would gain
standing control of a human's credentials. Before `0100`, `0013` only defaulted a NULL owner; an explicit
non-self owner supplied to `core_create_entity_with_owner` (`CreateEntityWithOwner`) or a raw `INSERT` was
accepted. No code path in the platform did that, so the route was latent; the invariant makes it structurally
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
as natural_person in `0013_entity_ownership.sql` (`entities_owner_default_self`, and its backfill), but an
explicit non-self owner is still accepted. Whether service accounts should be handled alongside natural_person
is under separate review; do not extend `0100` to them without that decision.

## Rollout

`0100_natural_person_owner_invariant.sql` first scans existing rows. If any natural_person entity has an
`owner_id` other than its own id (none should: `0013` backfilled and no creation path sets one), the migration
**fails** with SQLSTATE `MF002` and a hint query, rather than silently rewriting ownership. Repair the rows
deliberately (for example, in a maintenance transaction: `ALTER TABLE entities DISABLE TRIGGER
entities_owner_immutable; UPDATE ... SET owner_id = id ...; ALTER TABLE entities ENABLE TRIGGER
entities_owner_immutable;`) and re-run. The Down migration drops the trigger and function only.
