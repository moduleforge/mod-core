-- name: CreateEntity :one
SELECT id, uuid, fundamental_type_id, created_at, updated_at, archived_at, owner_id
FROM core_create_entity(sqlc.arg(fundamental_type_id));

-- name: CreateEntityWithOwner :one
SELECT id, uuid, fundamental_type_id, created_at, updated_at, archived_at, owner_id
FROM core_create_entity_with_owner(sqlc.arg(fundamental_type_id), sqlc.narg(owner_id));

-- name: GetEntityByUUID :one
SELECT
  e.id, e.uuid, e.fundamental_type_id,
  t.slug AS fundamental_type_slug,
  e.created_at, e.updated_at, e.archived_at
FROM entities e
JOIN types t ON e.fundamental_type_id = t.id
WHERE e.uuid = $1;

-- name: GetEntityByID :one
SELECT
  e.id, e.uuid, e.fundamental_type_id,
  t.slug AS fundamental_type_slug,
  e.created_at, e.updated_at, e.archived_at
FROM entities e
JOIN types t ON e.fundamental_type_id = t.id
WHERE e.id = $1;

-- name: ArchiveEntity :exec
SELECT core_archive_entity(sqlc.arg(uuid));

-- name: UnarchiveEntity :exec
SELECT core_unarchive_entity(sqlc.arg(uuid));
