-- +goose Up
--
-- Entity-write function seam (design item A2, managed-mode access).
--
-- These four SECURITY INVOKER functions are verbatim wrappers around today's
-- entity-write queries (model/queries/entities.sql). The wrapper itself
-- changes nothing for a standalone deployment: SECURITY INVOKER (the
-- default, so not stated explicitly below) runs the body as the calling
-- role against unqualified, and therefore unchanged-resolution, table names.
--
-- The reason to introduce the wrapper at all is managed mode: MFManager
-- generates a SECURITY DEFINER function of the same name in a managed app's
-- private schema. Because that schema resolves ahead of `public` in the
-- managed app's search path, an unqualified call to e.g. core_create_entity
-- resolves to the platform-owned SECURITY DEFINER body instead of this one
-- -- without the caller (or this function) changing at all. See
-- app-mfmanager/docs/architecture/managed-app-foundation-access.md.
--
-- Not STRICT: core_create_entity_with_owner legitimately receives a NULL
-- p_owner_id -- entities_owner_self_default (migration
-- 0013_entity_ownership.sql) fills it in for natural_person /
-- service_account types. STRICT would short-circuit to NULL instead of
-- running the INSERT.

-- +goose StatementBegin
CREATE FUNCTION core_create_entity(p_fundamental_type_id BIGINT) RETURNS entities
LANGUAGE sql VOLATILE AS $$
    INSERT INTO entities (fundamental_type_id)
    VALUES (p_fundamental_type_id)
    RETURNING id, uuid, fundamental_type_id, created_at, updated_at, archived_at, owner_id;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION core_create_entity_with_owner(p_fundamental_type_id BIGINT, p_owner_id BIGINT) RETURNS entities
LANGUAGE sql VOLATILE AS $$
    INSERT INTO entities (fundamental_type_id, owner_id)
    VALUES (p_fundamental_type_id, p_owner_id)
    RETURNING id, uuid, fundamental_type_id, created_at, updated_at, archived_at, owner_id;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION core_archive_entity(p_uuid UUID) RETURNS void
LANGUAGE sql VOLATILE AS $$
    UPDATE entities
    SET archived_at = now()
    WHERE uuid = p_uuid AND archived_at IS NULL;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION core_unarchive_entity(p_uuid UUID) RETURNS void
LANGUAGE sql VOLATILE AS $$
    UPDATE entities
    SET archived_at = NULL
    WHERE uuid = p_uuid AND archived_at IS NOT NULL;
$$;
-- +goose StatementEnd

-- +goose Down

DROP FUNCTION IF EXISTS core_unarchive_entity(UUID);
DROP FUNCTION IF EXISTS core_archive_entity(UUID);
DROP FUNCTION IF EXISTS core_create_entity_with_owner(BIGINT, BIGINT);
DROP FUNCTION IF EXISTS core_create_entity(BIGINT);
