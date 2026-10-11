package httpapi

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/moduleforge/core-api/apiresp"
	"github.com/moduleforge/core-api/service"
)

// createNaturalPersonRequest is the body for POST /entities/natural-persons.
type createNaturalPersonRequest struct {
	GivenName  string `json:"given_name"`
	FamilyName string `json:"family_name"`
	SSN        string `json:"ssn,omitempty"` // optional plaintext; "" means not recorded
}

// createNaturalPerson handles POST /entities/natural-persons (admin only).
func (h *handlers) createNaturalPerson(w http.ResponseWriter, r *http.Request) {
	var req createNaturalPersonRequest
	if !apiresp.DecodeJSON(w, r, &req) {
		return
	}

	// Stateless blank-name checks run before the service's AuthorizeType so an
	// unauthorized caller sees the 400, not a 403 (401 -> 400 -> 403 -> 404).
	if strings.TrimSpace(req.GivenName) == "" {
		apiresp.WriteError(w, r, apiresp.InvalidInput(apiresp.FieldError{
			Field:   "given_name",
			Code:    "natural_persons.given_name_required",
			Message: "given_name is required",
		}))
		return
	}
	if strings.TrimSpace(req.FamilyName) == "" {
		apiresp.WriteError(w, r, apiresp.InvalidInput(apiresp.FieldError{
			Field:   "family_name",
			Code:    "natural_persons.family_name_required",
			Message: "family_name is required",
		}))
		return
	}

	in := service.CreateNaturalPersonInput{
		GivenName:  req.GivenName,
		FamilyName: req.FamilyName,
		SSN:        req.SSN,
	}

	// The service manages its own transaction internally via txhelper.Run.
	np, entityUUID, err := h.d.Services.NaturalPerson.Create(r.Context(), h.d.Services.Querier(), in)
	if err != nil {
		apiresp.WriteError(w, r, err)
		return
	}

	resp := map[string]any{
		"uuid":        entityUUID.String(),
		"kind":        "natural_person",
		"given_name":  np.GivenName.String,
		"family_name": np.FamilyName.String,
	}
	// Admin callers always see the tax_id they just wrote, if one was supplied.
	if req.SSN != "" {
		resp["tax_id"] = req.SSN
		resp["tax_id_type"] = "SSN"
	}
	apiresp.WriteJSON(w, http.StatusCreated, resp)
}

// getNaturalPerson handles GET /entities/natural-persons/{uuid}.
func (h *handlers) getNaturalPerson(w http.ResponseWriter, r *http.Request) {
	entityUUID, err := apiresp.ParseUUIDParam("uuid", chi.URLParam(r, "uuid"))
	if err != nil {
		apiresp.WriteError(w, r, err)
		return
	}

	profile, err := h.d.Services.NaturalPerson.GetByEntityUUID(r.Context(), h.d.Services.Querier(), entityUUID)
	if err != nil {
		apiresp.WriteError(w, r, err)
		return
	}

	apiresp.WriteJSON(w, http.StatusOK, profileResponse(profile))
}

// updateNaturalPersonRequest is the body for PUT /entities/natural-persons/{uuid}.
// SSN uses three-state semantics: nil (field absent) = unchanged, pointer-to-""
// = clear, non-empty pointer = set.
type updateNaturalPersonRequest struct {
	GivenName  *string `json:"given_name"`
	FamilyName *string `json:"family_name"`
	SSN        *string `json:"ssn"` // nil = unchanged, "" = clear, else set
}

// updateNaturalPerson handles PUT /entities/natural-persons/{uuid}.
func (h *handlers) updateNaturalPerson(w http.ResponseWriter, r *http.Request) {
	entityUUID, err := apiresp.ParseUUIDParam("uuid", chi.URLParam(r, "uuid"))
	if err != nil {
		apiresp.WriteError(w, r, err)
		return
	}

	var req updateNaturalPersonRequest
	if !apiresp.DecodeJSON(w, r, &req) {
		return
	}

	in := service.UpdateNaturalPersonInput{
		GivenName:  req.GivenName,
		FamilyName: req.FamilyName,
		SSN:        req.SSN,
	}

	if err := h.d.Services.NaturalPerson.UpdateByEntityUUID(
		r.Context(),
		h.d.Services.Querier(),
		entityUUID,
		in,
	); err != nil {
		apiresp.WriteError(w, r, err)
		return
	}

	profile, err := h.d.Services.NaturalPerson.GetByEntityUUID(r.Context(), h.d.Services.Querier(), entityUUID)
	if err != nil {
		apiresp.WriteError(w, r, err)
		return
	}

	apiresp.WriteJSON(w, http.StatusOK, profileResponse(profile))
}
