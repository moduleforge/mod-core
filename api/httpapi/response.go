// Package httpapi exposes a mountable chi subrouter serving core entity routes.
// Consumers call NewRouter(deps) and mount the returned router under their
// preferred path prefix (typically /v1).
package httpapi

import (
	"time"

	"github.com/moduleforge/core-api/service"
	"github.com/moduleforge/core-api/types"
	coredb "github.com/moduleforge/core-model/db"
)

// profileResponse converts a service.Profile into a JSON-serialisable map.
// Used by entity-CRUD handlers that return the resolved Profile after a
// mutation or lookup. The "kind" JSON key carries the fundamental_type_slug.
// Tax-id fields are included when non-empty; access-control is enforced by
// the Authorizer before this function is reached.
func profileResponse(p service.Profile) map[string]any {
	resp := map[string]any{
		"kind": p.Kind,
	}

	if p.Entity.Uuid != (coredb.GetEntityByUUIDRow{}).Uuid {
		resp["uuid"] = p.Entity.Uuid.String()
	}

	switch p.Kind {
	case "natural_person":
		if np := p.NaturalPerson; np != nil {
			resp["given_name"] = np.GivenName.String
			resp["family_name"] = np.FamilyName.String
		}
	case "corporation":
		if corp := p.Corporation; corp != nil {
			resp["legal_name"] = corp.LegalName
			resp["jurisdiction"] = corp.Jurisdiction.String
		}
	case "service_account":
		if sa := p.ServiceAccount; sa != nil {
			resp["label"] = sa.Label
		}
	case types.SentinelSlug:
		// Internal ids (types.id, parent id, entity id) are never exposed.
		if ti := p.Type; ti != nil {
			resp["type_slug"] = ti.Slug
			resp["type_name"] = ti.Name
			resp["type_description"] = ti.Description
			resp["concrete"] = ti.Concrete
			if ti.DeprecatedAt != nil {
				resp["deprecated_at"] = ti.DeprecatedAt.UTC().Format(time.RFC3339)
			}
		}
	}

	// Include tax_id / tax_id_type when present. Authorization has already
	// been enforced by the Authorizer before this point, so any caller that
	// reaches here is permitted to see the full profile.
	if p.TaxID != "" {
		resp["tax_id"] = p.TaxID
		resp["tax_id_type"] = p.TaxIDType
	}

	return resp
}
