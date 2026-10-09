-- +goose Up

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
  -- exist yet, so the FK to entities(id) is added in 0008_entities.sql, which
  -- also installs the AFTER INSERT trigger that creates and links the entity
  -- and the deferred invariant that makes the column non-NULL at commit.
  entity_id      BIGINT UNIQUE
);

CREATE INDEX types_parent_id_idx ON types(parent_id);

-- Append-only enforcement: rows may never be DELETEd, and may only be UPDATEd
-- to set/unset deprecated_at, with one further exception: entity_id may be set
-- exactly once (NULL -> non-NULL) to an entity whose fundamental type is the
-- sentinel type 'type' (see 0008_entities.sql). Setting deprecated_at on the
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
  SELECT id, slug, parent_id FROM types WHERE id = p_type_id
  UNION ALL
  SELECT t.id, t.slug, t.parent_id
  FROM types t JOIN walk w ON t.id = w.parent_id
)
SELECT EXISTS (SELECT 1 FROM walk WHERE slug = p_target_slug);
$$ LANGUAGE sql STABLE;
-- +goose StatementEnd
