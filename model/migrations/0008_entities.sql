-- +goose Up

-- pgcrypto is the single Postgres-specific dependency; provides gen_random_uuid().
CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE entities (
  id                    BIGSERIAL PRIMARY KEY,
  uuid                  UUID UNIQUE NOT NULL DEFAULT gen_random_uuid(),
  fundamental_type_id   BIGINT NOT NULL REFERENCES types(id),
  created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
  archived_at           TIMESTAMPTZ
);

CREATE INDEX entities_fundamental_type_id_idx ON entities(fundamental_type_id);
CREATE INDEX entities_archived_at_idx ON entities(archived_at) WHERE archived_at IS NOT NULL;

-- Enforce that fundamental_type_id must reference a concrete type.
-- +goose StatementBegin
CREATE FUNCTION entities_check_concrete_type() RETURNS TRIGGER AS $$
DECLARE
  v_concrete   BOOLEAN;
  v_deprecated TIMESTAMPTZ;
  v_slug       TEXT;
BEGIN
  SELECT concrete, deprecated_at, slug INTO v_concrete, v_deprecated, v_slug
    FROM types WHERE id = NEW.fundamental_type_id;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'entities: fundamental_type_id % does not reference a known type', NEW.fundamental_type_id;
  END IF;
  IF NOT v_concrete THEN
    RAISE EXCEPTION 'entities: fundamental_type_id must reference a concrete type; % is abstract', NEW.fundamental_type_id;
  END IF;
  -- Creating an entity of a deprecated type is rejected, INSERT only: a no-op
  -- UPDATE OF fundamental_type_id on an existing row of a since-deprecated type
  -- must keep working (real changes are blocked by entities_immutable_type).
  -- SQLSTATE 'MF001' is a stable contract; Go callers match it via
  -- types.IsDeprecatedTypeError (api/types/deprecated.go).
  IF TG_OP = 'INSERT' AND v_deprecated IS NOT NULL THEN
    RAISE EXCEPTION 'entities: type % is deprecated; new entities of it cannot be created', v_slug
      USING ERRCODE = 'MF001';
  END IF;
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

-- Both triggers below are scoped to fundamental_type_id changes only.
-- The concrete-check validates that the (possibly unchanged) new value references a concrete type.
-- The immutable-check raises if the value actually changed (i.e., the column was modified).
CREATE TRIGGER entities_fundamental_type_concrete_check
  BEFORE INSERT OR UPDATE OF fundamental_type_id ON entities
  FOR EACH ROW EXECUTE FUNCTION entities_check_concrete_type();

-- Enforce that fundamental_type_id is immutable after insert.
-- +goose StatementBegin
CREATE FUNCTION entities_immutable_type() RETURNS TRIGGER AS $$
BEGIN
  IF OLD.fundamental_type_id IS DISTINCT FROM NEW.fundamental_type_id THEN
    RAISE EXCEPTION 'entities: fundamental_type_id is immutable after insert';
  END IF;
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER entities_fundamental_type_immutable
  BEFORE UPDATE OF fundamental_type_id ON entities
  FOR EACH ROW EXECUTE FUNCTION entities_immutable_type();

CREATE TRIGGER entities_set_updated_at
  BEFORE UPDATE ON entities
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- ---------------------------------------------------------------------------
-- Type entities
--
-- Every row of types is represented by exactly one entity whose fundamental
-- type is the sentinel type 'type'. grants.target_id and
-- authz_target_group_members.member_id can therefore point at a type. The
-- sentinel's own entity is of type 'type' (self-referential).
--
-- This lives here, not in 0002_types.sql, because the entities table does not
-- exist until above, and because the sentinel must be inserted AFTER the
-- 0003-0007 seeds so that their types.id values (1-5) stay unchanged.
-- ---------------------------------------------------------------------------

-- 1. FK for types.entity_id (the column is added in 0002_types.sql). No
--    ON DELETE action: neither table is ever hard-deleted from.
ALTER TABLE types
  ADD CONSTRAINT types_entity_id_fkey
  FOREIGN KEY (entity_id) REFERENCES entities(id);

-- 2. The sentinel. It is a second root next to 'entity', not a child of it, so
--    type entities do not satisfy type_is_or_descends_from(..., 'entity').
--    Inserted before the creation trigger exists; its entity is created by the
--    seeding step below.
INSERT INTO types (slug, parent_id, concrete, name, description)
VALUES (
  'type',
  NULL,
  true,
  'Type',
  'Sentinel root type. Every type is represented by exactly one entity of this type, including this type itself.'
);

-- 3. The type-entity creation routine, shared by the AFTER INSERT trigger and
--    the seeding step below.
--
--    SECURITY DEFINER: in managed mode a module owner role registers types
--    with INSERT INTO types but holds only SELECT, REFERENCES on entities, so
--    an invoker-rights routine would fail with "permission denied". The
--    search_path is pinned so the unqualified names below cannot be hijacked.
--    It inserts into entities directly, never through core_create_entity,
--    because a managed app may shadow that function in its private schema.
-- +goose StatementBegin
CREATE FUNCTION types_create_entity_for(p_type_id BIGINT) RETURNS BIGINT
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = public, pg_temp
AS $$
DECLARE
  v_sentinel_id BIGINT;
  v_entity_id   BIGINT;
BEGIN
  SELECT id INTO v_sentinel_id FROM public.types WHERE slug = 'type';
  IF NOT FOUND THEN
    RAISE EXCEPTION 'types: sentinel type ''type'' is not registered';
  END IF;

  INSERT INTO public.entities (fundamental_type_id)
  VALUES (v_sentinel_id)
  RETURNING id INTO v_entity_id;

  UPDATE public.types
     SET entity_id = v_entity_id
   WHERE id = p_type_id AND entity_id IS NULL;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'types: type % does not exist or already has an entity', p_type_id;
  END IF;

  RETURN v_entity_id;
END;
$$;
-- +goose StatementEnd

-- Not a trigger function, so it must not be callable by arbitrary roles.
REVOKE EXECUTE ON FUNCTION types_create_entity_for(BIGINT) FROM PUBLIC;

-- 4. Create entities for every type that predates the trigger (the six types
--    present at this point, sentinel included), in types.id order.
-- +goose StatementBegin
DO $$
DECLARE
  r RECORD;
BEGIN
  FOR r IN SELECT id FROM types WHERE entity_id IS NULL ORDER BY id LOOP
    PERFORM types_create_entity_for(r.id);
  END LOOP;
END $$;
-- +goose StatementEnd

-- Search path and definer notes for the type-entity section below. `public` is
-- hard-coded: it is the pinned search_path (`public, pg_temp`) and the schema
-- qualifier on every relation these functions touch, so a temporary table or a
-- same-named relation in another schema cannot shadow `types` or `entities`.
-- The three deferred-check and creation trigger functions are SECURITY DEFINER
-- because the deferred checks fire at commit as the session role, which in
-- managed mode may lack SELECT on public.types. Their EXECUTE is revoked from
-- PUBLIC; triggers do not need EXECUTE at fire time. The two guards that only
-- raise (types_reject_mutation, entities_reject_type_entity_archive) stay
-- SECURITY INVOKER with a pinned search_path.

-- 5. AFTER INSERT trigger: a type's entity is created by the type's own
--    insert. AFTER, not BEFORE: a BEFORE INSERT row trigger fires even when
--    INSERT ... ON CONFLICT (slug) DO NOTHING then skips the row, which would
--    orphan an entity. An AFTER row trigger fires only for inserted rows.
--
--    The trigger function is itself SECURITY DEFINER (it is the one the
--    inserting role reaches, and that role has no EXECUTE on the routine).
-- +goose StatementBegin
CREATE FUNCTION types_create_entity_trigger() RETURNS TRIGGER
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = public, pg_temp
AS $$
BEGIN
  IF NEW.entity_id IS NOT NULL THEN
    RAISE EXCEPTION 'types.entity_id is assigned by the creation trigger; callers may not supply it';
  END IF;
  PERFORM public.types_create_entity_for(NEW.id);
  RETURN NULL;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER types_create_entity
  AFTER INSERT ON types
  FOR EACH ROW EXECUTE FUNCTION types_create_entity_trigger();

REVOKE EXECUTE ON FUNCTION types_create_entity_trigger() FROM PUBLIC;

-- 6. Deferred commit-time invariants (bidirectional pairing).
--
--    (a) every types row has a non-NULL entity_id. The trigger's NEW is the
--        pre-update snapshot (entity_id still NULL), so re-select by id.
-- +goose StatementBegin
CREATE FUNCTION types_check_entity_id_set() RETURNS TRIGGER
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = public, pg_temp
AS $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM public.types WHERE id = NEW.id AND entity_id IS NOT NULL) THEN
    RAISE EXCEPTION 'types: type % has no entity at commit', NEW.id;
  END IF;
  RETURN NULL;
END;
$$;
-- +goose StatementEnd

CREATE CONSTRAINT TRIGGER types_entity_id_required
  AFTER INSERT ON types
  DEFERRABLE INITIALLY DEFERRED
  FOR EACH ROW EXECUTE FUNCTION types_check_entity_id_set();

REVOKE EXECUTE ON FUNCTION types_check_entity_id_set() FROM PUBLIC;

--    (b) every entity of the sentinel type is referenced by a types.entity_id.
--        This rejects sentinel-typed entities created by anything other than
--        types_create_entity_for.
-- +goose StatementBegin
CREATE FUNCTION entities_check_type_entity_referenced() RETURNS TRIGGER
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = public, pg_temp
AS $$
BEGIN
  IF EXISTS (SELECT 1 FROM public.types s WHERE s.slug = 'type' AND s.id = NEW.fundamental_type_id)
     AND NOT EXISTS (SELECT 1 FROM public.types t WHERE t.entity_id = NEW.id)
  THEN
    RAISE EXCEPTION 'entities: entity % has the sentinel type ''type'' but represents no type', NEW.id;
  END IF;
  RETURN NULL;
END;
$$;
-- +goose StatementEnd

CREATE CONSTRAINT TRIGGER entities_type_entity_referenced
  AFTER INSERT ON entities
  DEFERRABLE INITIALLY DEFERRED
  FOR EACH ROW EXECUTE FUNCTION entities_check_type_entity_referenced();

REVOKE EXECUTE ON FUNCTION entities_check_type_entity_referenced() FROM PUBLIC;

-- 7. Archive guard: types are append-only and deprecation is their only
--    lifecycle, so their entities may never be archived. BEFORE UPDATE OF
--    archived_at, like the other immutability guards in this file. Row
--    triggers for one event fire in alphabetical name order (see the note on
--    entities_owner_self_default in 0013_entity_ownership.sql); this guard
--    only raises, so its position relative to the others is not load-bearing.
-- +goose StatementBegin
CREATE FUNCTION entities_reject_type_entity_archive() RETURNS TRIGGER
LANGUAGE plpgsql
SET search_path = public, pg_temp
AS $$
BEGIN
  IF OLD.archived_at IS NULL
     AND NEW.archived_at IS NOT NULL
     AND EXISTS (SELECT 1 FROM public.types s WHERE s.slug = 'type' AND s.id = NEW.fundamental_type_id)
  THEN
    RAISE EXCEPTION 'entities: type entities cannot be archived';
  END IF;
  RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER entities_type_entity_no_archive
  BEFORE UPDATE OF archived_at ON entities
  FOR EACH ROW EXECUTE FUNCTION entities_reject_type_entity_archive();
