package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/moduleforge/core-api/apiresp"
	"github.com/moduleforge/core-api/display"
)

// displayNameResponse is the response body for
// GET /entities/{uuid}/display-name. DisplayName is a *string so an
// unavailable render serializes as JSON null rather than being omitted —
// callers need to distinguish "no name available" from "field absent".
type displayNameResponse struct {
	UUID        uuid.UUID `json:"uuid"`
	DisplayName *string   `json:"display_name"`
}

// getDisplayName handles GET /entities/{uuid}/display-name — resolves an
// entity UUID to its human-readable name via the configured display
// service.
//
// This endpoint requires real "read" authorization on the target entity —
// the same gate as every other single-entity read in this package, with the
// same masked 403 for a nonexistent or unauthorized UUID (the resolve step
// and the authorization check are indistinguishable to the caller). A 200
// response with a null display_name is the deliberate graceful-fallback
// contract for a readable entity with no registered renderer, or for a
// deployment with no display service wired at all — it is not a missing
// error case.
func (h *handlers) getDisplayName(w http.ResponseWriter, r *http.Request) {
	// The 401 gate is the router-level authz.RequireAuth; the real
	// authorization gate is the service's az.Authorize call below.
	entityUUID, err := apiresp.ParseUUIDParam("uuid", chi.URLParam(r, "uuid"))
	if err != nil {
		apiresp.WriteError(w, r, err)
		return
	}

	// A deployment with no display service wired is a valid state. The
	// response is constant across every UUID, so it discloses nothing about
	// any entity.
	if h.d.Display == nil {
		apiresp.WriteJSON(w, http.StatusOK, displayNameResponse{UUID: entityUUID})
		return
	}

	name, available, err := h.d.Display.RenderField(r.Context(), h.d.Services.Querier(), entityUUID, display.FieldName)
	if err != nil {
		// Pass the service's error through untouched — it is already an
		// apiresp-classifiable sentinel in the cases that matter (403 for a
		// masked miss or an authorization denial, 401 for no effective
		// actor). Never downgrade it into a 200/null body.
		apiresp.WriteError(w, r, err)
		return
	}

	resp := displayNameResponse{UUID: entityUUID}
	if available {
		resp.DisplayName = &name
	}
	apiresp.WriteJSON(w, http.StatusOK, resp)
}
