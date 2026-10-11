package httpapi

// field_crypto_keys_order_test.go drives every /field-crypto-keys route
// through apiresp/ordertest to pin the HTTP error-precedence order. All four
// routes authorize with a nil target ("manage"), not AuthorizeType, so none is
// AuthzFirst: stateless shape checks (400) precede the 403. The routes have no
// entity id; version-bearing routes use a retired version as ExistingPath and
// an absent version as MissingPath, while List and Rotate use one path for
// both.

import (
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/moduleforge/core-api/apiresp/ordertest"
	"github.com/moduleforge/core-api/fieldcrypto"
	"github.com/moduleforge/core-api/observer"
	coredb "github.com/moduleforge/core-model/db"
)

// newFCKOrderRouter composes RegisterFieldCryptoKeyRoutes on a shared /v1
// router with a public peer route. Fresh state per call: version 1 retired,
// version 2 active.
func newFCKOrderRouter(t *testing.T) http.Handler {
	t.Helper()
	q := newFCKFakeQuerier()
	q.seedKey(testKeyMaterial(1), true)
	q.seedKey(testKeyMaterial(40), false)
	cipher, err := fieldcrypto.NewFromKey(testKeyMaterial(40))
	if err != nil {
		t.Fatalf("build cipher: %v", err)
	}
	h := NewFieldCryptoKeyHandler(&fakeAppsDB{tx: &fakeAppsTx{}}, q, orderAdminOnlyAuthz{}, observer.NewObserverGroup(), cipher)
	h.newQuerier = func(_ pgx.Tx) coredb.Querier { return q }

	r := chi.NewRouter()
	r.Route("/v1", func(r chi.Router) {
		RegisterFieldCryptoKeyRoutes(r, h)
		r.Get("/peer", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	})
	return r
}

func TestFieldCryptoKeysErrorOrder(t *testing.T) {
	const (
		retiredPath = "/v1/field-crypto-keys/1/"
		activePath  = "/v1/field-crypto-keys/2/"
		absentPath  = "/v1/field-crypto-keys/99/"
	)
	admin, stranger := ordertest.Actor(orderAdminID), ordertest.Actor(orderStrangerID)

	// Each Run gets a fresh router: Rotate mutates the key table.
	ordertest.Run(t, ordertest.Route{
		Name:                    "GET /field-crypto-keys",
		Handler:                 newFCKOrderRouter(t),
		Method:                  http.MethodGet,
		ExistingPath:            "/v1/field-crypto-keys",
		MissingPath:             "/v1/field-crypto-keys",
		Authorized:              admin,
		Unauthorized:            stranger,
		AuthorizedMissingStatus: http.StatusOK,
	})

	for _, bad := range []struct{ name, body string }{
		{"malformed", `{"compromised":`},
		{"unknown member", `{"compromize":true}`},
		{"grace with compromised", `{"compromised":true,"grace_period_days":30}`},
		{"grace too small", `{"grace_period_days":0}`},
		{"key_hex invalid", `{"key_hex":"zzzz"}`},
	} {
		ordertest.Run(t, ordertest.Route{
			Name:                    "POST /field-crypto-keys/rotations " + bad.name,
			Handler:                 newFCKOrderRouter(t),
			Method:                  http.MethodPost,
			ExistingPath:            "/v1/field-crypto-keys/rotations",
			MissingPath:             "/v1/field-crypto-keys/rotations",
			GoodBody:                []byte(`{}`),
			BadBody:                 []byte(bad.body),
			Authorized:              admin,
			Unauthorized:            stranger,
			AuthorizedMissingStatus: http.StatusCreated,
			Late: []ordertest.Late{
				// Material of version 1 is already on file (key_bytes UNIQUE).
				{Name: "key-material-in-use", Body: []byte(`{"key_hex":"` + hexKeyMaterial(1) + `"}`), WantStatus: http.StatusConflict},
			},
		})
	}

	ordertest.Run(t, ordertest.Route{
		Name:                    "POST /field-crypto-keys/{version}/mark-compromised",
		Handler:                 newFCKOrderRouter(t),
		Method:                  http.MethodPost,
		ExistingPath:            retiredPath + "mark-compromised",
		MissingPath:             absentPath + "mark-compromised",
		Authorized:              admin,
		Unauthorized:            stranger,
		AuthorizedMissingStatus: http.StatusNotFound,
		Late: []ordertest.Late{
			{Name: "version-active", Path: activePath + "mark-compromised", WantStatus: http.StatusConflict},
		},
	})

	for _, bad := range []struct{ name, body string }{
		{"malformed", `{"grace_period_days":`},
		{"unknown member", `{"grace_days":5}`},
		{"missing key", `{}`},
		{"out of range", `{"grace_period_days":40000}`},
	} {
		ordertest.Run(t, ordertest.Route{
			Name:                    "PUT /field-crypto-keys/{version}/grace " + bad.name,
			Handler:                 newFCKOrderRouter(t),
			Method:                  http.MethodPut,
			ExistingPath:            retiredPath + "grace",
			MissingPath:             absentPath + "grace",
			GoodBody:                []byte(`{"grace_period_days":30}`),
			BadBody:                 []byte(bad.body),
			Authorized:              admin,
			Unauthorized:            stranger,
			AuthorizedMissingStatus: http.StatusNotFound,
			Late: []ordertest.Late{
				{Name: "version-active", Path: activePath + "grace", Body: []byte(`{"grace_period_days":30}`), WantStatus: http.StatusConflict},
			},
		})
	}
}

// A non-numeric {version} is a path-shape 400 (no body for ordertest's BadBody
// to carry): anonymous is 401 first, an authenticated caller who would be
// denied still sees 400, never 403.
func TestFieldCryptoKeysVersionParamOrder(t *testing.T) {
	router := newFCKOrderRouter(t)
	for _, tc := range []struct{ method, path, body string }{
		{http.MethodPost, "/v1/field-crypto-keys/abc/mark-compromised", ""},
		{http.MethodPut, "/v1/field-crypto-keys/abc/grace", `{"grace_period_days":30}`},
	} {
		for _, who := range []struct {
			name string
			id   ordertest.Identity
			want int
		}{
			{"anonymous", ordertest.Anonymous, http.StatusUnauthorized},
			{"stranger", ordertest.Actor(orderStrangerID), http.StatusBadRequest},
			{"admin", ordertest.Actor(orderAdminID), http.StatusBadRequest},
		} {
			t.Run(tc.method+" "+tc.path+"/"+who.name, func(t *testing.T) {
				req := httptest.NewRequest(tc.method, tc.path, bytesReaderOrNil(tc.body))
				req = req.WithContext(who.id(req.Context()))
				rec := httptest.NewRecorder()
				router.ServeHTTP(rec, req)
				if rec.Code != who.want {
					t.Errorf("got %d, want %d — %s", rec.Code, who.want, rec.Body.String())
				}
			})
		}
	}
}

// The 401 gate is scoped to the field-crypto group: the peer route on the
// same /v1 router stays public.
func TestFieldCryptoKeysRouteGroupDoesNotGatePeers(t *testing.T) {
	router := newFCKOrderRouter(t)
	peer := httptest.NewRecorder()
	router.ServeHTTP(peer, httptest.NewRequest(http.MethodGet, "/v1/peer", nil))
	if peer.Code != http.StatusOK {
		t.Errorf("anonymous peer route: got %d, want 200", peer.Code)
	}
	keys := httptest.NewRecorder()
	router.ServeHTTP(keys, httptest.NewRequest(http.MethodGet, "/v1/field-crypto-keys", nil))
	if keys.Code != http.StatusUnauthorized {
		t.Errorf("anonymous /field-crypto-keys: got %d, want 401", keys.Code)
	}
}

func hexKeyMaterial(seed byte) string { return hex.EncodeToString(testKeyMaterial(seed)) }

func bytesReaderOrNil(body string) io.Reader {
	if body == "" {
		return nil
	}
	return strings.NewReader(body)
}
