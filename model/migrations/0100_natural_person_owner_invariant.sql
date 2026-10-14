-- +goose Up

-- Structural invariant: a natural_person entity (or any descendant type) is
-- always owned by itself -- owner_id = id -- and can never be owned by another
-- entity.
--
-- Why: the authorization "own arm" (entities.owner_id = actor) confers every
-- operation on the owned entity, including credential-control operations such
-- as registering SSH keys and assuming the account. 0013_entity_ownership.sql
-- only DEFAULTS owner_id to the entity's own id when the caller leaves it NULL
-- and makes owner_id immutable afterwards; it does not reject an explicit
-- non-self owner on INSERT (core_create_entity_with_owner, or a raw INSERT).
-- That left a latent route to owning another person. This migration closes it.
--
-- Scope: natural_person and its descendants only. service_account keeps the
-- 0013 behaviour (default-to-self when NULL, explicit owner accepted); that is
-- intentionally NOT changed here.
--
-- SQLSTATE 'MF002' is a stable contract; Go callers match it via
-- types.IsNaturalPersonOwnerError (api/types/owner.go).

-- 1. Validate existing rows BEFORE installing the trigger. Rewriting
--    ownership silently would change authorization outcomes, so a database
--    that already violates the invariant fails the migration loudly and must
--    be repaired deliberately by an operator (see the exception hint).
-- +goose StatementBegin
DO $$
DECLARE
  v_bad BIGINT;
BEGIN
  SELECT count(*) INTO v_bad
    FROM public.entities e
   WHERE public.type_is_or_descends_from(e.fundamental_type_id, 'natural_person')
     AND e.owner_id IS DISTINCT FROM e.id;
  IF v_bad > 0 THEN
    RAISE EXCEPTION 'entities: % natural_person entit(y/ies) have owner_id other than their own id; repair them before applying this migration', v_bad
      USING ERRCODE = 'MF002',
            HINT = 'List them: SELECT id, uuid, owner_id FROM entities WHERE type_is_or_descends_from(fundamental_type_id, ''natural_person'') AND owner_id IS DISTINCT FROM id; owner_id is immutable, so disable trigger entities_owner_immutable in a maintenance transaction to repair.';
  END IF;
END;
$$;
-- +goose StatementEnd

-- 2. The invariant itself.
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

-- +goose Down

DROP TRIGGER IF EXISTS entities_owner_self_natural_person ON entities;
DROP FUNCTION IF EXISTS entities_enforce_natural_person_self_owner();
