-- +goose Up

-- 1. Nullable, self-referential ownership column.
--    No ON DELETE clause (default NO ACTION); entities are soft-deleted via
--    archived_at, so there is no hard-DELETE path for the RI action to guard.
ALTER TABLE entities
  ADD COLUMN owner_id BIGINT REFERENCES entities(id);

-- Supports the generic own-arm lookup (e.owner_id = p_actor_entity_id).
CREATE INDEX entities_owner_id_idx ON entities(owner_id) WHERE owner_id IS NOT NULL;

-- 2. "Owns itself" defaulting. Fires BEFORE INSERT only. NEW.id is already
--    populated here: the BIGSERIAL DEFAULT nextval() is evaluated when the
--    candidate tuple is formed, before row-level BEFORE INSERT triggers run.
-- +goose StatementBegin
CREATE FUNCTION entities_owner_default_self() RETURNS TRIGGER AS $$
BEGIN
  IF NEW.owner_id IS NULL
     AND ( type_is_or_descends_from(NEW.fundamental_type_id, 'natural_person')
        OR type_is_or_descends_from(NEW.fundamental_type_id, 'service_account') )
  THEN
    NEW.owner_id := NEW.id;
  END IF;
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

-- Trigger NAME is chosen deliberately: 'entities_owner_self_default' sorts
-- alphabetically AFTER the existing 'entities_fundamental_type_concrete_check'
-- ('...f...' < '...o...'), and Postgres fires same-event row triggers in
-- alphabetical name order, so fundamental_type_id is validated first.
CREATE TRIGGER entities_owner_self_default
  BEFORE INSERT ON entities
  FOR EACH ROW EXECUTE FUNCTION entities_owner_default_self();

-- 3. Backfill pre-existing natural_person / service_account rows. This MUST run
--    BEFORE the owner-immutability trigger is created (step 4); otherwise the
--    NULL -> id transition trips that guard. (Corporations / authz groups stay NULL.)
UPDATE entities e
   SET owner_id = e.id
 WHERE e.owner_id IS NULL
   AND ( type_is_or_descends_from(e.fundamental_type_id, 'natural_person')
      OR type_is_or_descends_from(e.fundamental_type_id, 'service_account') );

-- 4. Immutability guard (created AFTER the backfill). BEFORE UPDATE OF owner_id
--    mirrors the existing entities_fundamental_type_immutable pattern (0008):
--    it only fires when owner_id is named in the UPDATE's SET list, and uses
--    IS DISTINCT FROM so a no-op re-set of the same value is allowed.
-- +goose StatementBegin
CREATE FUNCTION entities_immutable_owner() RETURNS TRIGGER AS $$
BEGIN
  IF OLD.owner_id IS DISTINCT FROM NEW.owner_id THEN
    RAISE EXCEPTION 'entities: owner_id is immutable after insert';
  END IF;
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER entities_owner_immutable
  BEFORE UPDATE OF owner_id ON entities
  FOR EACH ROW EXECUTE FUNCTION entities_immutable_owner();

-- 5. Entity-write function seam (design item A2, managed-mode access).
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
-- p_owner_id -- entities_owner_self_default (above, this migration) fills
-- it in for natural_person / service_account types. STRICT would
-- short-circuit to NULL instead of running the INSERT.
--
-- These functions reference entities.owner_id (added by step 1, above), so
-- they must be defined here -- in the migration that completes entities'
-- write-relevant schema -- rather than in 0008_entities.sql, which predates
-- owner_id and would fail CREATE FUNCTION with "column owner_id does not
-- exist" if these were placed there instead.

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

-- Reverse order overall: drop the four write functions (step 5, added
-- last), then the two triggers, then the two functions, then the index,
-- then the column (steps 4 through 1, undone last-to-first).
DROP FUNCTION IF EXISTS core_unarchive_entity(UUID);
DROP FUNCTION IF EXISTS core_archive_entity(UUID);
DROP FUNCTION IF EXISTS core_create_entity_with_owner(BIGINT, BIGINT);
DROP FUNCTION IF EXISTS core_create_entity(BIGINT);
DROP TRIGGER IF EXISTS entities_owner_immutable ON entities;
DROP TRIGGER IF EXISTS entities_owner_self_default ON entities;
DROP FUNCTION IF EXISTS entities_immutable_owner();
DROP FUNCTION IF EXISTS entities_owner_default_self();
DROP INDEX IF EXISTS entities_owner_id_idx;
ALTER TABLE entities DROP COLUMN IF EXISTS owner_id;
