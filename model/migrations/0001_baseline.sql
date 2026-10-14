-- +goose Up

-- mod-core baseline schema.
--
-- Collapsed on 2026-10-14 from migrations 0001-0017, 0099 and 0100 while
-- pre-release. Databases that recorded those versions in
-- goose_db_version_core must be dropped and recreated; there is no in-place
-- upgrade path from the old chain. New migrations append as 0002..0099.

-- Helper functions used across the schema.

-- set_updated_at() — generic trigger function that stamps updated_at = now()
-- on any table that has an updated_at column. Applied via BEFORE UPDATE triggers.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION set_updated_at() RETURNS TRIGGER AS $$
BEGIN
  NEW.updated_at = now();
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TABLE types (
  id             BIGSERIAL PRIMARY KEY,
  slug           TEXT UNIQUE NOT NULL,
  parent_id      BIGINT REFERENCES types(id) CHECK (parent_id IS DISTINCT FROM id),
  concrete       BOOLEAN NOT NULL,
  name           TEXT NOT NULL,
  description    TEXT,
  created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
  deprecated_at  TIMESTAMPTZ,
  -- The entity that represents this type (type-scoped grants target it through
  -- grants.target_id). Nullable at the DDL level: the entities table does not
  -- exist yet, so the FK to entities(id) is added further down, where the
  -- entities table is created; that section also installs the AFTER INSERT trigger that creates and links the entity
  -- and the deferred invariant that makes the column non-NULL at commit.
  entity_id      BIGINT UNIQUE
);

CREATE INDEX types_parent_id_idx ON types(parent_id);

-- Append-only enforcement: rows may never be DELETEd, and may only be UPDATEd
-- to set/unset deprecated_at, with one further exception: entity_id may be set
-- exactly once (NULL -> non-NULL) to an entity whose fundamental type is the
-- sentinel type 'type' (see "Type entities" below). Setting deprecated_at on the
-- sentinel row itself is rejected, because every later type registration
-- creates an entity of the sentinel type.
--
-- The body references the entities table, which does not exist yet. plpgsql
-- resolves relations at run time, so the function is definable here.
-- +goose StatementBegin
CREATE FUNCTION types_reject_mutation() RETURNS TRIGGER
LANGUAGE plpgsql
SET search_path = public, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'types rows are append-only; DELETE is not permitted';
  END IF;
  IF OLD.id IS DISTINCT FROM NEW.id
     OR OLD.slug IS DISTINCT FROM NEW.slug
     OR OLD.parent_id IS DISTINCT FROM NEW.parent_id
     OR OLD.concrete IS DISTINCT FROM NEW.concrete
     OR OLD.name IS DISTINCT FROM NEW.name
     OR OLD.description IS DISTINCT FROM NEW.description
     OR OLD.created_at IS DISTINCT FROM NEW.created_at
  THEN
    RAISE EXCEPTION 'types rows are append-only; only deprecated_at may be updated';
  END IF;
  IF OLD.entity_id IS DISTINCT FROM NEW.entity_id THEN
    IF OLD.entity_id IS NOT NULL OR NEW.entity_id IS NULL THEN
      RAISE EXCEPTION 'types.entity_id is immutable once set';
    END IF;
    IF NOT EXISTS (
      SELECT 1
        FROM public.entities e
        JOIN public.types s ON s.id = e.fundamental_type_id AND s.slug = 'type'
       WHERE e.id = NEW.entity_id
    ) THEN
      RAISE EXCEPTION 'types.entity_id must reference an entity of the sentinel type ''type''';
    END IF;
  END IF;
  IF OLD.slug = 'type'
     AND NEW.deprecated_at IS NOT NULL
     AND OLD.deprecated_at IS DISTINCT FROM NEW.deprecated_at
  THEN
    RAISE EXCEPTION 'the sentinel type ''type'' cannot be deprecated';
  END IF;
  RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER types_no_delete
  BEFORE DELETE ON types
  FOR EACH ROW EXECUTE FUNCTION types_reject_mutation();

CREATE TRIGGER types_append_only_update
  BEFORE UPDATE ON types
  FOR EACH ROW EXECUTE FUNCTION types_reject_mutation();

-- type_is_or_descends_from(p_type_id, p_target_slug) — returns TRUE if the
-- type identified by p_type_id is, or is a descendant of, the type with slug
-- p_target_slug. Used by subtype BEFORE INSERT triggers to assert ancestry.
-- Placed here (after CREATE TABLE types) so the SQL-language function can
-- resolve the types relation at parse time.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION type_is_or_descends_from(p_type_id BIGINT, p_target_slug TEXT)
RETURNS BOOLEAN AS $$
WITH RECURSIVE walk AS (
  SELECT id, slug, parent_id FROM public.types WHERE id = p_type_id
  UNION ALL
  SELECT t.id, t.slug, t.parent_id
  FROM public.types t JOIN walk w ON t.id = w.parent_id
)
SELECT EXISTS (SELECT 1 FROM walk WHERE slug = p_target_slug);
$$ LANGUAGE sql STABLE
SET search_path = public, pg_temp;
-- +goose StatementEnd

-- Seed: abstract root type 'entity'. Every object in the system is an Entity.
INSERT INTO types (slug, parent_id, concrete, name, description)
VALUES (
  'entity',
  NULL,
  false,
  'Entity',
  'Abstract root type. Every object in the system is an Entity.'
);

-- Seed: abstract type 'legal_entity', child of 'entity'.
-- +goose StatementBegin
DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM types WHERE slug = 'entity') THEN
    RAISE EXCEPTION 'seed for legal_entity requires parent slug ''entity'' to exist';
  END IF;
END $$;
-- +goose StatementEnd

INSERT INTO types (slug, parent_id, concrete, name, description)
SELECT
  'legal_entity',
  id,
  false,
  'Legal Entity',
  'Abstract type for entities with legal standing (natural persons and corporations).'
FROM types WHERE slug = 'entity';

-- Seed: concrete type 'natural_person', child of 'legal_entity'.
-- +goose StatementBegin
DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM types WHERE slug = 'legal_entity') THEN
    RAISE EXCEPTION 'seed for natural_person requires parent slug ''legal_entity'' to exist';
  END IF;
END $$;
-- +goose StatementEnd

INSERT INTO types (slug, parent_id, concrete, name, description)
SELECT
  'natural_person',
  id,
  true,
  'Natural Person',
  'A human individual with legal standing.'
FROM types WHERE slug = 'legal_entity';

-- Seed: concrete type 'corporation', child of 'legal_entity'.
-- +goose StatementBegin
DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM types WHERE slug = 'legal_entity') THEN
    RAISE EXCEPTION 'seed for corporation requires parent slug ''legal_entity'' to exist';
  END IF;
END $$;
-- +goose StatementEnd

INSERT INTO types (slug, parent_id, concrete, name, description)
SELECT
  'corporation',
  id,
  true,
  'Corporation',
  'A legal entity organized as a corporation or company.'
FROM types WHERE slug = 'legal_entity';

-- Seed: concrete type 'service_account', child of 'entity' (not a legal entity).
-- +goose StatementBegin
DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM types WHERE slug = 'entity') THEN
    RAISE EXCEPTION 'seed for service_account requires parent slug ''entity'' to exist';
  END IF;
END $$;
-- +goose StatementEnd

INSERT INTO types (slug, parent_id, concrete, name, description)
SELECT
  'service_account',
  id,
  true,
  'Service Account',
  'A non-human principal used by automated services and integrations.'
FROM types WHERE slug = 'entity';

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
CREATE FUNCTION entities_check_concrete_type() RETURNS TRIGGER
LANGUAGE plpgsql
SET search_path = public, pg_temp
AS $$
DECLARE
  v_concrete   BOOLEAN;
  v_deprecated TIMESTAMPTZ;
  v_slug       TEXT;
BEGIN
  SELECT concrete, deprecated_at, slug INTO v_concrete, v_deprecated, v_slug
    FROM public.types WHERE id = NEW.fundamental_type_id;
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
$$;
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
-- This lives here, not next to the types table, because the entities table
-- does not exist until above, and because the sentinel must be inserted AFTER
-- the entity, legal_entity, natural_person, corporation and service_account
-- seeds so that their types.id values (1-5) stay unchanged.
-- ---------------------------------------------------------------------------

-- 1. FK for types.entity_id (the column is declared on the types table). No
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
-- SECURITY INVOKER with a pinned search_path. The same pinning and
-- qualification covers entities_check_concrete_type (above) and
-- type_is_or_descends_from (see the note there on its search_path).

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
--    entities_owner_self_default in the entity ownership section below); this guard
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

-- legal_entities is a pure FK-anchor table: no kind, no display_name, no
-- synthetic id, no timestamps (those live on entities). Any entity whose
-- fundamental type descends from 'legal_entity' may have a row here.
CREATE TABLE legal_entities (
  entity_id BIGINT PRIMARY KEY REFERENCES entities(id) ON DELETE RESTRICT
);

-- Enforce that only entities whose fundamental type descends from 'legal_entity'
-- may be inserted into this table.
-- +goose StatementBegin
CREATE FUNCTION legal_entities_check_type() RETURNS TRIGGER AS $$
DECLARE
  v_type_id BIGINT;
BEGIN
  SELECT fundamental_type_id INTO v_type_id FROM entities WHERE id = NEW.entity_id;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'legal_entities: entity_id % does not reference a known entity', NEW.entity_id;
  END IF;
  IF NOT type_is_or_descends_from(v_type_id, 'legal_entity') THEN
    RAISE EXCEPTION 'legal_entities: entity % fundamental type does not descend from legal_entity', NEW.entity_id;
  END IF;
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER legal_entities_type_check
  BEFORE INSERT ON legal_entities
  FOR EACH ROW EXECUTE FUNCTION legal_entities_check_type();

CREATE TABLE natural_persons (
  id          BIGSERIAL PRIMARY KEY,
  entity_id   BIGINT NOT NULL UNIQUE REFERENCES legal_entities(entity_id) ON DELETE RESTRICT,
  given_name  TEXT,
  family_name TEXT,
  -- ssn holds an AES-256-GCM blob (version || nonce || ciphertext || tag)
  -- produced by the application. NULL means no SSN is recorded. The
  -- database never sees plaintext.
  ssn         BYTEA,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Consumers should prefer joining through entity_id rather than the synthetic id.

-- Enforce that the entity's fundamental type is exactly 'natural_person'.
-- +goose StatementBegin
CREATE FUNCTION natural_persons_check_type() RETURNS TRIGGER AS $$
DECLARE
  v_type_id BIGINT;
BEGIN
  SELECT fundamental_type_id INTO v_type_id FROM entities WHERE id = NEW.entity_id;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'natural_persons: entity_id % does not reference a known entity', NEW.entity_id;
  END IF;
  IF NOT type_is_or_descends_from(v_type_id, 'natural_person') THEN
    RAISE EXCEPTION 'natural_persons: entity % fundamental type is not natural_person', NEW.entity_id;
  END IF;
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER natural_persons_type_check
  BEFORE INSERT ON natural_persons
  FOR EACH ROW EXECUTE FUNCTION natural_persons_check_type();

CREATE TRIGGER natural_persons_set_updated_at
  BEFORE UPDATE ON natural_persons
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE corporations (
  id           BIGSERIAL PRIMARY KEY,
  entity_id    BIGINT NOT NULL UNIQUE REFERENCES legal_entities(entity_id) ON DELETE RESTRICT,
  legal_name   TEXT NOT NULL,
  jurisdiction TEXT,
  -- ein holds an AES-256-GCM blob (version || nonce || ciphertext || tag)
  -- produced by the application. NULL means no EIN is recorded.
  ein          BYTEA,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Consumers should prefer joining through entity_id rather than the synthetic id.

-- Enforce that the entity's fundamental type is exactly 'corporation'.
-- +goose StatementBegin
CREATE FUNCTION corporations_check_type() RETURNS TRIGGER AS $$
DECLARE
  v_type_id BIGINT;
BEGIN
  SELECT fundamental_type_id INTO v_type_id FROM entities WHERE id = NEW.entity_id;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'corporations: entity_id % does not reference a known entity', NEW.entity_id;
  END IF;
  IF NOT type_is_or_descends_from(v_type_id, 'corporation') THEN
    RAISE EXCEPTION 'corporations: entity % fundamental type is not corporation', NEW.entity_id;
  END IF;
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER corporations_type_check
  BEFORE INSERT ON corporations
  FOR EACH ROW EXECUTE FUNCTION corporations_check_type();

CREATE TRIGGER corporations_set_updated_at
  BEFORE UPDATE ON corporations
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE service_accounts (
  id          BIGSERIAL PRIMARY KEY,
  entity_id   BIGINT NOT NULL UNIQUE REFERENCES entities(id) ON DELETE RESTRICT,
  label       TEXT NOT NULL,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Enforce that the entity's fundamental type is exactly 'service_account'.
-- +goose StatementBegin
CREATE FUNCTION service_accounts_check_type() RETURNS TRIGGER AS $$
DECLARE
  v_type_id BIGINT;
BEGIN
  SELECT fundamental_type_id INTO v_type_id FROM entities WHERE id = NEW.entity_id;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'service_accounts: entity_id % does not reference a known entity', NEW.entity_id;
  END IF;
  IF NOT type_is_or_descends_from(v_type_id, 'service_account') THEN
    RAISE EXCEPTION 'service_accounts: entity % fundamental type is not service_account', NEW.entity_id;
  END IF;
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER service_accounts_type_check
  BEFORE INSERT ON service_accounts
  FOR EACH ROW EXECUTE FUNCTION service_accounts_check_type();

CREATE TRIGGER service_accounts_set_updated_at
  BEFORE UPDATE ON service_accounts
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

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

-- 3. Immutability guard. BEFORE UPDATE OF owner_id
--    mirrors the existing entities_fundamental_type_immutable pattern above:
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

-- 4. Entity-write function seam (design item A2, managed-mode access).
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
-- p_owner_id -- entities_owner_self_default (above) fills
-- it in for natural_person / service_account types. STRICT would
-- short-circuit to NULL instead of running the INSERT.
--
-- These functions reference entities.owner_id (added by step 1, above), so
-- they must be defined after that column is added, which is why they sit
-- here rather than next to the entities table definition, where the
-- CREATE FUNCTION would fail with "column owner_id does not exist".

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

-- Seed: concrete type 'app', child of 'entity' (not a legal entity).
-- +goose StatementBegin
DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM types WHERE slug = 'entity') THEN
    RAISE EXCEPTION 'seed for app requires parent slug ''entity'' to exist';
  END IF;
END $$;
-- +goose StatementEnd

INSERT INTO types (slug, parent_id, concrete, name, description)
SELECT
  'app',
  id,
  true,
  'App',
  'A registered application principal that consumes moduleforge module APIs.'
FROM types WHERE slug = 'entity';

-- apps is a pure FK-anchor table, keyed on entities.id (per the change
-- request: apps.id becomes a FK to entities.id, not a separate surrogate
-- key). No uuid, no timestamps, no archived_at: those all live on entities
-- (see the entities table above). The app's UUID is entities.uuid; archival is
-- entities.archived_at.
--
-- apps has mutable domain columns (slug, name), like corporations and
-- service_accounts and unlike legal_entities, so it carries its own
-- updated_at and set_updated_at trigger: entities.updated_at only changes on
-- writes to the entities table itself, which an app slug/name edit never
-- touches.
CREATE TABLE apps (
  id          BIGINT PRIMARY KEY REFERENCES entities(id) ON DELETE RESTRICT,
  slug        TEXT NOT NULL UNIQUE,
  name        TEXT NOT NULL,
  updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Enforce that only entities whose fundamental type descends from 'app'
-- may be inserted into this table.
-- +goose StatementBegin
CREATE FUNCTION apps_check_type() RETURNS TRIGGER AS $$
DECLARE
  v_type_id BIGINT;
BEGIN
  SELECT fundamental_type_id INTO v_type_id FROM entities WHERE id = NEW.id;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'apps: id % does not reference a known entity', NEW.id;
  END IF;
  IF NOT type_is_or_descends_from(v_type_id, 'app') THEN
    RAISE EXCEPTION 'apps: entity % fundamental type does not descend from app', NEW.id;
  END IF;
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER apps_type_check
  BEFORE INSERT ON apps
  FOR EACH ROW EXECUTE FUNCTION apps_check_type();

CREATE TRIGGER apps_set_updated_at
  BEFORE UPDATE ON apps
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- field_crypto_keys holds every AES-256-GCM field-encryption key this
-- database has ever used: exactly one active key (retired_at IS NULL),
-- which all new encryption uses, plus an indefinite number of retired,
-- decrypt-only keys kept so blobs written under them stay readable until
-- re-encrypt-on-read has moved them onto the active key.
--
-- version is the value carried in each stored blob's version prefix
-- (version || nonce || ciphertext || tag) and bound into the AEAD as
-- additional authenticated data, so a blob names the exact row that can
-- decrypt it. GENERATED ALWAYS AS IDENTITY because no application code may
-- ever choose a version number: reusing one would silently make ciphertext
-- undecryptable. Gaps are expected and harmless — a lost first-boot race
-- consumes a value without inserting a row.
--
-- Exactly one active key is a database-level invariant, enforced by the
-- field_crypto_keys_one_active partial unique index below. That index also
-- replaces the id = 1 CHECK the previous single-row shape of this table
-- used to converge concurrent first-boot INSERTs on one winner: the
-- bootstrap INSERT is always an active-key INSERT, so concurrent
-- bootstraps race on that index exactly as they used to race on the
-- primary key, and the loser adopts the winner's key rather than
-- generating one of its own.
--
-- Deliberately not modeled as an entity (no FK to entities.id): this is
-- bootstrap/operational data unrelated to the domain entity hierarchy, the
-- same reasoning that keeps goose_db_version_core a bare table. It
-- likewise gets no accessible_*_ids_for_actor access function
-- (see the access function stubs below) — there is no actor-scoped read path
-- into key material.
CREATE TABLE field_crypto_keys (
  -- Wire version tag. Starts at 1; 0 is never issued, so application code
  -- may treat a 0 version prefix as a malformed blob.
  version           INTEGER PRIMARY KEY GENERATED ALWAYS AS IDENTITY,
  -- 32 bytes = AES-256. UNIQUE so an operator cannot re-introduce key
  -- material already on file — notably a key previously retired as
  -- compromised — under a fresh version number.
  key_bytes         BYTEA NOT NULL UNIQUE CHECK (octet_length(key_bytes) = 32),
  created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
  -- NULL = this is the active key, the one all new encryption uses. Set
  -- when a rotation demotes it to decrypt-only. Never cleared: a retired
  -- key is never re-activated.
  retired_at        TIMESTAMPTZ,
  -- End of the decrypt grace window for a retired key, resolved to an
  -- absolute instant from the operator-supplied grace period in days,
  -- against the database clock, at the moment of retirement. NULL = no
  -- expiry, and NULL is the default: a retired key stays usable for
  -- decryption indefinitely unless an operator deliberately sets a
  -- deadline. Once this instant passes the key is no longer loaded by the
  -- cipher and any blob still carrying its version fails loudly rather
  -- than decrypting.
  decryptable_until TIMESTAMPTZ,
  -- NULL = not known to be compromised. When set, re-encrypt-on-read must
  -- fail the read rather than return plaintext whenever it cannot persist
  -- the re-encrypted blob. Set either at rotation time or afterwards, when
  -- a key previously believed safe turns out not to be.
  compromised_at    TIMESTAMPTZ,
  -- A grace deadline and a compromise flag both describe a key that is no
  -- longer the active encryption key. Refusing either on the active row
  -- makes "we know this key leaked but we are still encrypting under it"
  -- unrepresentable: marking the active key compromised is a rotation, not
  -- a flag update.
  CONSTRAINT field_crypto_keys_retired_only_flags CHECK (
    (decryptable_until IS NULL AND compromised_at IS NULL) OR retired_at IS NOT NULL
  ),
  CONSTRAINT field_crypto_keys_grace_after_retirement CHECK (
    decryptable_until IS NULL OR decryptable_until >= retired_at
  )
);

-- At most one active key. Unique over a constant-true expression,
-- restricted to the rows where retired_at IS NULL, so a second active row
-- is a unique violation rather than a state the application has to
-- police. Also the first-boot convergence arbiter (see the table comment).
CREATE UNIQUE INDEX field_crypto_keys_one_active
  ON field_crypto_keys ((retired_at IS NULL))
  WHERE retired_at IS NULL;

-- retired_at and compromised_at are self-timestamping, but a grace-window
-- extension writes only decryptable_until; updated_at is what records when
-- that happened. Same set_updated_at() helper every other mutable table
-- uses.
CREATE TRIGGER field_crypto_keys_set_updated_at
  BEFORE UPDATE ON field_crypto_keys
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- No secondary indexes: this table gains one row per rotation and is read
-- in full at process start.

--
-- Access function stubs for core-module entity resources.
--
-- These define the function signatures that list/search queries JOIN against
-- for row-level scoping. The bodies here are stubs that return the empty set.
-- At application startup, the chosen Authorizer implementation calls
-- core-module/api/authz/setup.ApplyFuncs(...) which replaces these bodies via
-- CREATE OR REPLACE FUNCTION with the real policy.
--
-- Signatures take a second parameter p_op_ids INT[] carrying the
-- satisfied-by closure for the requested operation.
--
-- See docs/mf-standards/architecture/authorization-design.md "Row-level scoping".

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION accessible_natural_person_ids_for_actor(p_actor_entity_id BIGINT, p_op_ids INT[])
RETURNS TABLE(entity_id BIGINT) LANGUAGE sql STABLE AS $$
    SELECT 0::BIGINT AS entity_id WHERE FALSE
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION accessible_corporation_ids_for_actor(p_actor_entity_id BIGINT, p_op_ids INT[])
RETURNS TABLE(entity_id BIGINT) LANGUAGE sql STABLE AS $$
    SELECT 0::BIGINT AS entity_id WHERE FALSE
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION accessible_service_account_ids_for_actor(p_actor_entity_id BIGINT, p_op_ids INT[])
RETURNS TABLE(entity_id BIGINT) LANGUAGE sql STABLE AS $$
    SELECT 0::BIGINT AS entity_id WHERE FALSE
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION accessible_legal_entity_ids_for_actor(p_actor_entity_id BIGINT, p_op_ids INT[])
RETURNS TABLE(entity_id BIGINT) LANGUAGE sql STABLE AS $$
    SELECT 0::BIGINT AS entity_id WHERE FALSE
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION accessible_authz_actor_group_ids_for_actor(p_actor_entity_id BIGINT, p_op_ids INT[])
RETURNS TABLE(entity_id BIGINT) LANGUAGE sql STABLE AS $$
    SELECT 0::BIGINT AS entity_id WHERE FALSE
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION accessible_authz_target_group_ids_for_actor(p_actor_entity_id BIGINT, p_op_ids INT[])
RETURNS TABLE(entity_id BIGINT) LANGUAGE sql STABLE AS $$
    SELECT 0::BIGINT AS entity_id WHERE FALSE
$$;
-- +goose StatementEnd

-- Structural invariant: a natural_person entity (or any descendant type) is
-- always owned by itself -- owner_id = id -- and can never be owned by another
-- entity.
--
-- Why: the authorization "own arm" (entities.owner_id = actor) confers every
-- operation on the owned entity, including credential-control operations such
-- as registering SSH keys and assuming the account. The entity ownership
-- section above only DEFAULTS owner_id to the entity's own id when the caller leaves it NULL
-- and makes owner_id immutable afterwards; it does not reject an explicit
-- non-self owner on INSERT (core_create_entity_with_owner, or a raw INSERT).
-- That would leave a latent route to owning another person; this trigger closes it.
--
-- Scope: natural_person and its descendants only. service_account keeps the
-- entity ownership behaviour (default-to-self when NULL, explicit owner
-- accepted); that is intentionally NOT enforced here.
--
-- SQLSTATE 'MF002' is a stable contract; Go callers match it via
-- types.IsNaturalPersonOwnerError (api/types/owner.go).

-- The invariant itself.
-- +goose StatementBegin
CREATE FUNCTION entities_enforce_natural_person_self_owner() RETURNS TRIGGER
LANGUAGE plpgsql
SET search_path = public, pg_temp
AS $$
BEGIN
  IF public.type_is_or_descends_from(NEW.fundamental_type_id, 'natural_person')
     AND NEW.owner_id IS DISTINCT FROM NEW.id
  THEN
    RAISE EXCEPTION 'entities: a natural_person entity must be owned by itself (owner_id = id); it cannot be owned by another entity'
      USING ERRCODE = 'MF002';
  END IF;
  RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- Name sorts AFTER entities_owner_self_default ('...self_d...' < '...self_n...'),
-- and Postgres fires same-event row triggers in name order, so on INSERT the
-- NULL -> id defaulting has already happened when this check runs. On UPDATE,
-- entities_owner_immutable ('...immutable' < '...self_...') still fires first
-- and rejects any owner_id change with its own message; this trigger is the
-- independent backstop (and also covers a type change, which
-- entities_fundamental_type_immutable already forbids).
CREATE TRIGGER entities_owner_self_natural_person
  BEFORE INSERT OR UPDATE OF owner_id, fundamental_type_id ON entities
  FOR EACH ROW EXECUTE FUNCTION entities_enforce_natural_person_self_owner();
