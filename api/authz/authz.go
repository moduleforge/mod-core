// Package authz defines the Authorizer interface consumed by all service
// methods in the system, plus RequireAuthenticated, a contract-adjacent
// authentication-only helper that needs no implementation.
//
// Authorizer is intentionally narrow: one method, called pre-op, on every
// operation including reads. The implementation is consumer-supplied; this
// package defines the contract, plus RequireAuthenticated below.
//
// Type-level checks (admin-gated create, type-scoped list) are a separate
// question from entity-level ones and must not be expressed through Authorize's
// target. They go through AuthorizeType, which uses the optional TypeAuthorizer
// capability when the Authorizer provides it and otherwise fails closed to a
// nil-target Authorize call.
//
// The acting user's identity is resolved from ctx by the implementation using
// the opctx package (ActorEntityID, SudoActorEntityID). operation and target
// are explicit parameters because they differ per service call and context
// values survive call boundaries poorly when they are method-specific.
//
// A non-nil error aborts the calling service method immediately. Implementations
// should return distinguishable error values so HTTP handlers can map
// "not authenticated" to 401 and "authenticated but not allowed" to 403.
package authz

import (
	"context"

	"github.com/moduleforge/core-api/apiresp"
	"github.com/moduleforge/core-api/opctx"
)

// Authorizer gates every operation in the system. A single Authorizer
// implementation is wired per application at the composition root; peer modules
// receive it via their service constructors.
//
// operation is a stable, lower-case verb string describing the operation:
//
//	"read", "list", "create", "update", "delete"
//
// "search" is not a separate operation — searches authorize as "list" (same
// security question: may this actor discover this resource?). Domain-specific
// verbs (e.g. "assume", "login", "grant", "revoke") are permitted where the
// standard set is insufficient.
//
// target is always an entities.id: the internal entity ID of the object being
// acted on (or the parent entity for dependent data, such as a legal entity's
// contacts), or nil when no specific target exists yet (create / list /
// search). The Authorizer operates *before* any retrieval or instantiation of
// the target; it never needs more than the ID. For an entity target, if policy
// needs the target's resource type, the implementation looks it up by ID.
//
// target must never be a types.id. Passing one is a caller bug that an
// implementation cannot detect, because entities.id and types.id are both
// int64 and both sequences start at 1: an entity-semantic implementation will
// match the value against an unrelated entity, which is an authorization
// bypass. Type-level checks go through AuthorizeType, and implementations that
// can answer them implement TypeAuthorizer. A type's own entity
// (types.entity_id) is a legitimate entities.id target for entity-level
// operations on the type entity itself, for example "read" or "grant". It is
// never a stand-in for the type id in a type-level "create" or "list" check.
// (The ecosystem call-shape table in
// docs/mf-standards/architecture/authorization-design.md currently says
// otherwise; the mod-authz slice of the type-scoped-grants change delivers the
// correction.)
//
// The Authorizer resolves the effective actor from ctx via the opctx accessors.
// When SudoActorEntityID is set on ctx, the Authorizer must treat that ID as
// the effective actor for all policy checks (the admin's own identity is not the
// subject of the request for the duration of an assume session).
type Authorizer interface {
	Authorize(ctx context.Context, operation string, target *int64) error
}

// TypeAuthorizer is an optional capability of an Authorizer: it answers
// type-level questions ("may the effective actor create/list resources of type
// typeID?") distinctly from entity-level ones. typeID is a types.id, never an
// entities.id. It answers type-scoped authority only:
//
//   - wildcard grants (target_id IS NULL), and
//   - grants whose target is the exact type's entity (types.entity_id for
//     typeID), held directly or through target-group membership, by the actor
//     or any actor group in its chain.
//
// It never consults entity ownership. It never walks to parent or child types:
// a grant on a parent type does not cover its subtypes. It never treats typeID
// as an entities.id. Callers should not invoke it directly; use AuthorizeType.
type TypeAuthorizer interface {
	AuthorizeType(ctx context.Context, operation string, typeID int64) error
}

// AuthorizeType performs a type-level authorization check against az. It is the
// one call every module should use for type-level create/list checks.
//
// It fails closed with apiresp.ErrForbidden when az is nil or typeID is not
// positive, without calling az. If az implements TypeAuthorizer, that result
// is returned unchanged and is final (a deny is never retried through
// Authorize). Otherwise it calls az.Authorize(ctx, operation, nil): a nil
// target is the core contract's "no specific target", which implementations
// answer from wildcard grants only, so decorators, foreign authorizers, and
// stubs that hide or lack AuthorizeType still fail closed. The fallback
// therefore denies type-grant holders: a type grant takes effect only through
// an implementation that provides TypeAuthorizer.
//
// AuthorizeType never passes typeID, or any non-nil target, to Authorize:
// typeID is a types.id and Authorize's target is an entities.id, so doing so
// would match an unrelated entity.
//
// The nil check detects only an untyped-nil Authorizer. A typed-nil pointer
// held in the interface is not detected, and the TypeAuthorizer method call
// may panic on the nil receiver. That fails closed (a panic is never an
// allow), but Authorizers must be constructed non-nil.
func AuthorizeType(ctx context.Context, az Authorizer, operation string, typeID int64) error {
	if az == nil || typeID <= 0 {
		return apiresp.ErrForbidden
	}
	if ta, ok := az.(TypeAuthorizer); ok {
		return ta.AuthorizeType(ctx, operation, typeID)
	}
	return az.Authorize(ctx, operation, nil)
}

// OpResolver resolves an operation slug to the set of operation IDs that
// satisfy it (the reverse-implies / satisfied-by closure). Peer modules that
// issue list queries against access functions use this to compute the op_ids
// slice without importing the full authz-api module.
//
// The returned slice is owned by the resolver; callers MUST NOT mutate it.
// Returns nil and an error for unknown slugs.
//
// OperationRegistry in authz-api implements this interface. Inject it at the
// composition root (users-module main.go) and pass it down to every service
// constructor that issues list queries.
type OpResolver interface {
	SatisfiedBy(slug string) ([]int32, error)
}

// RequireAuthenticated is an authentication-only check — it answers "is there
// an effective actor on this context at all?", not "may this actor perform
// this operation?"; the latter question stays with Authorizer.Authorize. It
// resolves the effective actor via opctx.EffectiveActorEntityID, so a
// sudo-assumed identity counts as authenticated. It returns
// apiresp.ErrUnauthenticated — which HTTP handlers map to 401 — when no
// effective actor is present, and nil otherwise. It is a free function rather
// than an Authorizer method so callers need no Authorizer instance and no
// existing implementation needs to change.
func RequireAuthenticated(ctx context.Context) error {
	if _, ok := opctx.EffectiveActorEntityID(ctx); !ok {
		return apiresp.ErrUnauthenticated
	}
	return nil
}
