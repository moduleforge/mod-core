// Package authz_test confirms that the Authorizer interface is implementable
// from outside the authz package and that the interface signature compiles as
// specified.
//
// There is no behavior to test on the interface itself; these tests exist to
// catch any future accidental mutation of the interface signature and to serve
// as a compilation-time contract check.
package authz_test

import (
	"context"
	"errors"
	"testing"

	"github.com/moduleforge/core-api/apiresp"
	"github.com/moduleforge/core-api/authz"
	"github.com/moduleforge/core-api/opctx"
)

// stubAuthorizer is a minimal, outside-the-package implementation of authz.Authorizer.
// It is used only to confirm that the interface can be satisfied by consumer code.
type stubAuthorizer struct {
	// denyOperation, if non-empty, causes Authorize to return an error for that operation.
	denyOperation string
}

// Authorize implements authz.Authorizer.
func (s *stubAuthorizer) Authorize(_ context.Context, operation string, _ *int64) error {
	if s.denyOperation != "" && operation == s.denyOperation {
		return errors.New("authz: denied")
	}
	return nil
}

// Compile-time assertion: *stubAuthorizer must satisfy authz.Authorizer.
var _ authz.Authorizer = (*stubAuthorizer)(nil)

// TestStubAuthorizerAllows verifies the stub permits operations that are not denied.
func TestStubAuthorizerAllows(t *testing.T) {
	a := &stubAuthorizer{denyOperation: "delete"}
	id := int64(1)

	if err := a.Authorize(context.Background(), "read", &id); err != nil {
		t.Fatalf("expected nil error for allowed operation, got: %v", err)
	}
}

// TestStubAuthorizerDenies verifies the stub rejects the configured operation.
func TestStubAuthorizerDenies(t *testing.T) {
	a := &stubAuthorizer{denyOperation: "delete"}
	id := int64(1)

	if err := a.Authorize(context.Background(), "delete", &id); err == nil {
		t.Fatal("expected error for denied operation, got nil")
	}
}

// TestAuthorizerWithNilTarget confirms the interface handles a nil target
// (e.g. create / list / search) without panicking.
func TestAuthorizerWithNilTarget(t *testing.T) {
	a := &stubAuthorizer{}

	if err := a.Authorize(context.Background(), "create", nil); err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
}

// TestRequireAuthenticated covers the three effective-actor states
// RequireAuthenticated must distinguish.
func TestRequireAuthenticated(t *testing.T) {
	t.Run("sudo actor set", func(t *testing.T) {
		ctx := opctx.WithActor(context.Background(), 1)
		ctx = opctx.WithSudoActor(ctx, 2)

		if err := authz.RequireAuthenticated(ctx); err != nil {
			t.Fatalf("expected nil error, got: %v", err)
		}
	})

	t.Run("real actor only", func(t *testing.T) {
		ctx := opctx.WithActor(context.Background(), 1)

		if err := authz.RequireAuthenticated(ctx); err != nil {
			t.Fatalf("expected nil error, got: %v", err)
		}
	})

	t.Run("neither set", func(t *testing.T) {
		err := authz.RequireAuthenticated(context.Background())

		if err == nil {
			t.Fatal("expected non-nil error, got nil")
		}
		if !errors.Is(err, apiresp.ErrUnauthenticated) {
			t.Fatalf("expected apiresp.ErrUnauthenticated, got: %v", err)
		}
		if errors.Is(err, apiresp.ErrForbidden) {
			t.Fatalf("expected error not to match apiresp.ErrForbidden, got: %v", err)
		}
	})
}

// authzCall records one Authorize or AuthorizeType invocation. hasTarget is
// false when Authorize received a nil target.
type authzCall struct {
	operation string
	hasTarget bool
	target    int64
}

// plainStub is an Authorizer WITHOUT AuthorizeType. It simulates an
// entity-semantic implementation: it allows only when a non-nil target equals
// allowTarget (as if the actor owned the entity with that id), or when the
// target is nil and allowNil is set.
type plainStub struct {
	allowTarget int64
	allowNil    bool
	err         error // returned instead of the allow/deny decision when set
	calls       []authzCall
}

func (s *plainStub) Authorize(_ context.Context, operation string, target *int64) error {
	c := authzCall{operation: operation}
	if target != nil {
		c.hasTarget = true
		c.target = *target
	}
	s.calls = append(s.calls, c)
	if s.err != nil {
		return s.err
	}
	if target == nil {
		if s.allowNil {
			return nil
		}
		return apiresp.ErrForbidden
	}
	if *target == s.allowTarget {
		return nil
	}
	return apiresp.ErrForbidden
}

// typedStub implements both Authorizer and TypeAuthorizer.
type typedStub struct {
	typeErr      error // result of AuthorizeType
	authorizeErr error // result of Authorize
	typeCalls    []authzCall
	authzCalls   []authzCall
}

func (s *typedStub) Authorize(_ context.Context, operation string, target *int64) error {
	c := authzCall{operation: operation}
	if target != nil {
		c.hasTarget = true
		c.target = *target
	}
	s.authzCalls = append(s.authzCalls, c)
	return s.authorizeErr
}

func (s *typedStub) AuthorizeType(_ context.Context, operation string, typeID int64) error {
	s.typeCalls = append(s.typeCalls, authzCall{operation: operation, hasTarget: true, target: typeID})
	return s.typeErr
}

// hidingDecorator wraps a type-aware authorizer but exposes only Authorize,
// as a decorating authorizer that does not forward AuthorizeType would.
type hidingDecorator struct {
	inner *typedStub
}

func (d hidingDecorator) Authorize(ctx context.Context, operation string, target *int64) error {
	return d.inner.Authorize(ctx, operation, target)
}

// Compile-time assertions.
var (
	_ authz.Authorizer     = (*plainStub)(nil)
	_ authz.Authorizer     = (*typedStub)(nil)
	_ authz.TypeAuthorizer = (*typedStub)(nil)
	_ authz.Authorizer     = hidingDecorator{}
)

func TestAuthorizeType_PlainAuthorizerNeverReceivesTypeIDAsTarget(t *testing.T) {
	const typeID = int64(7)
	// Owns entity 7 == typeID: would be a bypass if &typeID were passed.
	stub := &plainStub{allowTarget: typeID}

	err := authz.AuthorizeType(context.Background(), stub, "create", typeID)

	if !errors.Is(err, apiresp.ErrForbidden) {
		t.Fatalf("expected ErrForbidden, got: %v", err)
	}
	if len(stub.calls) != 1 {
		t.Fatalf("expected exactly 1 Authorize call, got %d: %+v", len(stub.calls), stub.calls)
	}
	if got := stub.calls[0]; got.operation != "create" || got.hasTarget {
		t.Fatalf("expected call (create, nil), got %+v", got)
	}
}

func TestAuthorizeType_PlainAuthorizerAllowOnNil(t *testing.T) {
	stub := &plainStub{allowNil: true}

	if err := authz.AuthorizeType(context.Background(), stub, "create", 7); err != nil {
		t.Fatalf("expected nil, got: %v", err)
	}
	if len(stub.calls) != 1 || stub.calls[0].hasTarget || stub.calls[0].operation != "create" {
		t.Fatalf("expected single (create, nil) call, got %+v", stub.calls)
	}
}

func TestAuthorizeType_TypeAwareDenyIsFinal(t *testing.T) {
	stub := &typedStub{typeErr: apiresp.ErrForbidden} // Authorize allows everything

	err := authz.AuthorizeType(context.Background(), stub, "create", 7)

	if !errors.Is(err, apiresp.ErrForbidden) {
		t.Fatalf("expected ErrForbidden, got: %v", err)
	}
	if len(stub.typeCalls) != 1 || stub.typeCalls[0].operation != "create" || stub.typeCalls[0].target != 7 {
		t.Fatalf("expected one AuthorizeType(create, 7) call, got %+v", stub.typeCalls)
	}
	if len(stub.authzCalls) != 0 {
		t.Fatalf("Authorize must never be called after a type-aware deny, got %+v", stub.authzCalls)
	}
}

func TestAuthorizeType_TypeAwareAllow(t *testing.T) {
	stub := &typedStub{authorizeErr: apiresp.ErrForbidden} // Authorize denies everything

	if err := authz.AuthorizeType(context.Background(), stub, "list", 3); err != nil {
		t.Fatalf("expected nil, got: %v", err)
	}
	if len(stub.typeCalls) != 1 || len(stub.authzCalls) != 0 {
		t.Fatalf("expected one type call and no Authorize calls, got %+v / %+v", stub.typeCalls, stub.authzCalls)
	}
}

func TestAuthorizeType_ErrorPassthrough(t *testing.T) {
	sentinel := errors.New("backend unavailable")

	t.Run("type-aware", func(t *testing.T) {
		stub := &typedStub{typeErr: sentinel}
		err := authz.AuthorizeType(context.Background(), stub, "create", 7)
		if !errors.Is(err, sentinel) {
			t.Fatalf("expected sentinel, got: %v", err)
		}
		if len(stub.authzCalls) != 0 {
			t.Fatalf("Authorize must not be called, got %+v", stub.authzCalls)
		}
	})

	t.Run("fallback", func(t *testing.T) {
		stub := &plainStub{err: sentinel}
		err := authz.AuthorizeType(context.Background(), stub, "create", 7)
		if !errors.Is(err, sentinel) {
			t.Fatalf("expected sentinel, got: %v", err)
		}
		if len(stub.calls) != 1 || stub.calls[0].hasTarget {
			t.Fatalf("expected single nil-target call, got %+v", stub.calls)
		}
	})
}

func TestAuthorizeType_FailClosed(t *testing.T) {
	for _, typeID := range []int64{0, -1} {
		t.Run("typeID", func(t *testing.T) {
			plain := &plainStub{allowNil: true, allowTarget: typeID}
			typed := &typedStub{}

			if err := authz.AuthorizeType(context.Background(), plain, "create", typeID); !errors.Is(err, apiresp.ErrForbidden) {
				t.Fatalf("plain, typeID %d: expected ErrForbidden, got: %v", typeID, err)
			}
			if err := authz.AuthorizeType(context.Background(), typed, "create", typeID); !errors.Is(err, apiresp.ErrForbidden) {
				t.Fatalf("typed, typeID %d: expected ErrForbidden, got: %v", typeID, err)
			}
			if len(plain.calls) != 0 || len(typed.typeCalls) != 0 || len(typed.authzCalls) != 0 {
				t.Fatalf("no stub method may be called for typeID %d", typeID)
			}
		})
	}

	t.Run("nil authorizer", func(t *testing.T) {
		err := authz.AuthorizeType(context.Background(), nil, "create", 7)
		if !errors.Is(err, apiresp.ErrForbidden) {
			t.Fatalf("expected ErrForbidden, got: %v", err)
		}
	})
}

func TestAuthorizeType_HiddenByDecorator(t *testing.T) {
	inner := &typedStub{typeErr: errors.New("must not be reached")}
	dec := hidingDecorator{inner: inner}

	if err := authz.AuthorizeType(context.Background(), dec, "create", 7); err != nil {
		t.Fatalf("expected nil from inner Authorize, got: %v", err)
	}
	if len(inner.typeCalls) != 0 {
		t.Fatalf("AuthorizeType must not be reached through a hiding decorator, got %+v", inner.typeCalls)
	}
	if len(inner.authzCalls) != 1 || inner.authzCalls[0].hasTarget || inner.authzCalls[0].operation != "create" {
		t.Fatalf("expected single (create, nil) call, got %+v", inner.authzCalls)
	}
}
