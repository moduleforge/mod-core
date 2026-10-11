package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/moduleforge/core-api/apiresp"
)

// getEntity handles GET /entities/{uuid} — resolves any entity UUID to its
// profile, picking the appropriate subtype automatically.
func (h *handlers) getEntity(w http.ResponseWriter, r *http.Request) {
	entityUUID, err := uuid.Parse(chi.URLParam(r, "uuid"))
	if err != nil {
		apiresp.WriteError(w, r, apiresp.ErrInvalidInput)
		return
	}

	profile, err := h.d.Services.Entity.ResolveProfile(r.Context(), h.d.Services.Querier(), entityUUID)
	if err != nil {
		apiresp.WriteError(w, r, err)
		return
	}

	apiresp.WriteJSON(w, http.StatusOK, profileResponse(profile))
}

// archiveEntity handles DELETE /entities/{uuid} (admin only) — soft-deletes
// an entity by setting archived_at.
func (h *handlers) archiveEntity(w http.ResponseWriter, r *http.Request) {
	entityUUID, err := uuid.Parse(chi.URLParam(r, "uuid"))
	if err != nil {
		apiresp.WriteError(w, r, apiresp.ErrInvalidInput)
		return
	}

	if err := h.d.Services.Entity.Archive(r.Context(), h.d.Services.Querier(), entityUUID); err != nil {
		apiresp.WriteError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
