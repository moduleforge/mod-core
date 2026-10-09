-- name: GetTypeBySlug :one
SELECT id, slug, parent_id, concrete, name, description, created_at, deprecated_at, entity_id
FROM types
WHERE slug = $1;

-- name: GetTypeByID :one
SELECT id, slug, parent_id, concrete, name, description, created_at, deprecated_at, entity_id
FROM types
WHERE id = $1;

-- name: ListAllTypes :many
SELECT id, slug, parent_id, concrete, name, description, created_at, deprecated_at, entity_id
FROM types;
