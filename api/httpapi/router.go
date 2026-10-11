package httpapi

import (
	"log/slog"

	"github.com/go-chi/chi/v5"

	"github.com/moduleforge/core-api/authz"
	"github.com/moduleforge/core-api/service"
)

// Deps carries the external dependencies that httpapi handlers need.
type Deps struct {
	// Services holds the entity CRUD implementations.
	Services *service.Services
	// Logger is the structured logger for handler-level error messages.
	Logger *slog.Logger
	// Display resolves a human-readable name for an entity, backing
	// GET /entities/{uuid}/display-name. It is nil-safe: a Deps built by
	// NewDeps has a nil Display, and the handler responds with the
	// graceful-unavailable body rather than panicking or erroring.
	Display service.DisplayServicer
}

// NewDeps constructs a Deps value from its components.
//
// NewDeps remains valid: it is declared in moduleforge.module.yaml and
// called by peer repositories' own servers, so its signature must not
// change. A Deps built by NewDeps simply has a nil Display, and
// GET /entities/{uuid}/display-name responds with the graceful
// unavailable (display_name: null) body for every UUID.
func NewDeps(svcs *service.Services, logger *slog.Logger) Deps {
	return Deps{Services: svcs, Logger: logger}
}

// NewDepsWithDisplay constructs a Deps value with a display service wired,
// so GET /entities/{uuid}/display-name resolves real display names. Use
// this constructor instead of NewDeps when the composing app has a
// service.DisplayServicer to wire in; NewDeps remains valid for a
// deployment that does not.
func NewDepsWithDisplay(svcs *service.Services, logger *slog.Logger, dsp service.DisplayServicer) Deps {
	return Deps{Services: svcs, Logger: logger, Display: dsp}
}

type handlers struct {
	d Deps
}

// NewRouter wires /entities/* routes and returns a mountable chi.Router.
// Mount it under any prefix, e.g. r.Mount("/v1", core.NewRouter(deps)).
//
// Core does not expose /self: that endpoint only makes sense for a
// consumer that has a users/authentication layer (which core does not).
// Consumers like users-module own /self and compose a response from
// their own user row plus core's EntityService.GetSelf helper.
func NewRouter(d Deps) chi.Router {
	r := chi.NewRouter()
	h := &handlers{d: d}

	// 401 gate for every route, before any handler: sudo-aware, so an
	// identity carrying only a sudo actor still passes.
	r.Use(authz.RequireAuth)

	r.Route("/entities", func(r chi.Router) {
		r.Post("/natural-persons", h.createNaturalPerson)
		r.Get("/natural-persons/{uuid}", h.getNaturalPerson)
		r.Put("/natural-persons/{uuid}", h.updateNaturalPerson)

		r.Post("/corporations", h.createCorporation)
		r.Get("/corporations/{uuid}", h.getCorporation)
		r.Put("/corporations/{uuid}", h.updateCorporation)

		r.Post("/service-accounts", h.createServiceAccount)
		r.Get("/service-accounts/{uuid}", h.getServiceAccount)
		r.Put("/service-accounts/{uuid}", h.updateServiceAccount)

		// Generic entity routes — must come after typed sub-paths to avoid
		// capturing /natural-persons etc. display-name is registered here,
		// inside the existing /entities group, rather than as a second
		// Register*Routes entry mounted onto /v1 (the pattern apps.go and
		// field_crypto_keys.go use): those own new top-level prefixes that do
		// not overlap this router's mount point, but a second registration of
		// /entities/... into the same /v1 chi group would introduce a
		// competing "entities" trie node alongside the catch-all this router
		// already mounts at "/", risking a routing-precedence change for the
		// existing /entities/* surface.
		r.Get("/{uuid}", h.getEntity)
		r.Get("/{uuid}/display-name", h.getDisplayName)
		r.Delete("/{uuid}", h.archiveEntity)
	})

	return r
}
