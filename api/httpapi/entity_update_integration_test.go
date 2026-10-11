//go:build integration

package httpapi_test

// entity_update_integration_test.go drives the three entity PUT routes through
// the real router, real services and the real Postgres schema (TestMain in
// field_crypto_keys_integration_test.go): a valid but untrimmed value from an
// authorized caller is a 200 with the trimmed value persisted, an unauthorized
// caller still gets 403 for a valid body and 400 for a blank one, and a
// service-account label change dispatches the in-tx 'update' observer event
// carrying the before and after labels.
//
// Run recipe is in the header of field_crypto_keys_integration_test.go.

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/moduleforge/core-api/apiresp"
	"github.com/moduleforge/core-api/entity"
	"github.com/moduleforge/core-api/fieldcrypto"
	"github.com/moduleforge/core-api/httpapi"
	"github.com/moduleforge/core-api/observer"
	"github.com/moduleforge/core-api/opctx"
	"github.com/moduleforge/core-api/service"
	"github.com/moduleforge/core-api/types"
)

const (
	updateAllowedActor int64 = 7001
	updateDeniedActor  int64 = 7002
)

// updateAuthorizer allows updateAllowedActor on any real target and denies
// everyone else, and every caller on the target-0 masked-miss probe.
type updateAuthorizer struct{}

func (updateAuthorizer) Authorize(ctx context.Context, _ string, target *int64) error {
	if target != nil && *target == 0 {
		return apiresp.ErrForbidden
	}
	if id, ok := opctx.EffectiveActorEntityID(ctx); ok && id == updateAllowedActor {
		return nil
	}
	return apiresp.ErrForbidden
}

func (updateAuthorizer) AuthorizeType(ctx context.Context, op string, _ int64) error {
	return updateAuthorizer{}.Authorize(ctx, op, nil)
}

// updateObserver records the in-tx and post-commit events.
type updateObserver struct {
	mu        sync.Mutex
	inTx      []updateEvent
	afterComm []updateEvent
}

type updateEvent struct {
	op, resource  string
	before, after any
}

func (o *updateObserver) Observe(_ context.Context, _ pgx.Tx, op, resource string, _ *int64, before, after any) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.inTx = append(o.inTx, updateEvent{op, resource, before, after})
	return nil
}

func (o *updateObserver) ObserveAfterCommit(_ context.Context, op, resource string, _ *int64, after any) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.afterComm = append(o.afterComm, updateEvent{op: op, resource: resource, after: after})
}

func newEntityUpdateRouter(t *testing.T, obs *observer.ObserverGroup) http.Handler {
	t.Helper()
	typeResolver, err := types.New(context.Background(), coreQ)
	if err != nil {
		t.Fatalf("types.New: %v", err)
	}
	cipher, err := fieldcrypto.NewFromKey(bytes.Repeat([]byte{9}, 32))
	if err != nil {
		t.Fatalf("cipher: %v", err)
	}
	svcs := service.New(coreQ, pool, updateAuthorizer{}, obs, cipher, entity.NewResolver(), typeResolver)
	return httpapi.NewRouter(httpapi.NewDeps(svcs, slog.New(slog.NewTextHandler(io.Discard, nil))))
}

func updateReq(h http.Handler, method, path, body string, actor int64) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(opctx.WithActor(req.Context(), actor))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestEntityUpdateRoutes_Integration_UntrimmedValuesStoredTrimmed(t *testing.T) {
	uobs := &updateObserver{}
	h := newEntityUpdateRouter(t, observer.NewObserverGroup(uobs))

	create := func(path, body string) string {
		t.Helper()
		rec := updateReq(h, http.MethodPost, path, body, updateAllowedActor)
		if rec.Code != http.StatusCreated {
			t.Fatalf("create %s: got %d - %s", path, rec.Code, rec.Body.String())
		}
		var out struct {
			UUID string `json:"uuid"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode create: %v", err)
		}
		return out.UUID
	}
	npUUID := create("/entities/natural-persons", `{"given_name":"Ada","family_name":"Lovelace"}`)
	corpUUID := create("/entities/corporations", `{"legal_name":"Acme","jurisdiction":"DE"}`)
	saUUID := create("/entities/service-accounts", `{"label":"old"}`)

	cases := []struct {
		name, path, valid, blank, want string
	}{
		{"natural person", "/entities/natural-persons/" + npUUID, `{"given_name":"  Grace  "}`, `{"family_name":"   "}`, `"Grace"`},
		{"corporation", "/entities/corporations/" + corpUUID, `{"legal_name":"  Acme Corp  "}`, `{"legal_name":" "}`, `"Acme Corp"`},
		{"service account", "/entities/service-accounts/" + saUUID, `{"label":"  new  "}`, `{"label":"\t"}`, `"new"`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if rec := updateReq(h, http.MethodPut, c.path, c.valid, updateDeniedActor); rec.Code != http.StatusForbidden {
				t.Errorf("unauthorized+valid: got %d, want 403 - %s", rec.Code, rec.Body.String())
			}
			if rec := updateReq(h, http.MethodPut, c.path, c.blank, updateDeniedActor); rec.Code != http.StatusBadRequest {
				t.Errorf("unauthorized+blank: got %d, want 400 - %s", rec.Code, rec.Body.String())
			}
			if rec := updateReq(h, http.MethodPut, c.path, c.blank, updateAllowedActor); rec.Code != http.StatusBadRequest {
				t.Errorf("authorized+blank: got %d, want 400 - %s", rec.Code, rec.Body.String())
			}
			rec := updateReq(h, http.MethodPut, c.path, c.valid, updateAllowedActor)
			if rec.Code != http.StatusOK {
				t.Fatalf("authorized+valid: got %d, want 200 - %s", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), c.want) {
				t.Errorf("response missing trimmed value %s: %s", c.want, rec.Body.String())
			}
		})
	}

	// The service-account update dispatched the audit events.
	uobs.mu.Lock()
	defer uobs.mu.Unlock()
	var inTx, post *updateEvent
	for i := range uobs.inTx {
		if e := &uobs.inTx[i]; e.op == "update" && e.resource == "service_account" {
			inTx = e
		}
	}
	for i := range uobs.afterComm {
		if e := &uobs.afterComm[i]; e.op == "update" && e.resource == "service_account" {
			post = e
		}
	}
	if inTx == nil || post == nil {
		t.Fatalf("service_account update events: in-tx=%v post-commit=%v, want both", inTx, post)
	}
	if b, _ := inTx.before.(map[string]any); b["label"] != "old" {
		t.Errorf("before: %+v, want label old", inTx.before)
	}
	if a, _ := inTx.after.(map[string]any); a["label"] != "new" {
		t.Errorf("after: %+v, want label new", inTx.after)
	}
}
