// capabilities.go
package transport

import (
	"encoding/json"
	"net/http"

	"github.com/xraph/forge/extensions/dashboard/contract"
)

// CapabilitiesResponse is the wire shape for GET /capabilities.
type CapabilitiesResponse struct {
	ShellEnvelopes []string                `json:"shellEnvelopes"`
	Contributors   []ContributorCapability `json:"contributors"`
}

// ContributorCapability is one contributor's negotiable surface.
type ContributorCapability struct {
	Name      string             `json:"name"`
	Envelopes []string           `json:"envelopes"`
	Intents   []IntentCapability `json:"intents"`
}

// IntentCapability summarises one intent's available versions.
type IntentCapability struct {
	Name     string                `json:"name"`
	Versions []IntentVersionStatus `json:"versions"`
}

// IntentVersionStatus reports a single version + lifecycle status.
type IntentVersionStatus struct {
	N           int    `json:"n"`
	Status      string `json:"status"` // active | deprecated
	RemoveAfter string `json:"removeAfter,omitempty"`
}

// NewCapabilitiesHandler returns the GET /capabilities handler.
func NewCapabilitiesHandler(reg contract.Registry, shellEnvelopes []string) http.Handler {
	return &capabilitiesHandler{reg: reg, shellEnvelopes: shellEnvelopes}
}

type capabilitiesHandler struct {
	reg            contract.Registry
	shellEnvelopes []string
}

func (h *capabilitiesHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET required", http.StatusMethodNotAllowed)
		return
	}
	resp := CapabilitiesResponse{ShellEnvelopes: h.shellEnvelopes}
	for _, m := range h.reg.All() {
		c := ContributorCapability{Name: m.Contributor.Name, Envelopes: m.Contributor.Envelope.Supports}
		// Group intents by name; collect versions.
		byName := map[string][]IntentVersionStatus{}
		for _, in := range m.Intents {
			s := IntentVersionStatus{N: in.Version, Status: "active"}
			if in.Deprecated != nil {
				s.Status = "deprecated"
				s.RemoveAfter = in.Deprecated.RemoveAfter
			}
			byName[in.Name] = append(byName[in.Name], s)
		}
		for name, versions := range byName {
			c.Intents = append(c.Intents, IntentCapability{Name: name, Versions: versions})
		}
		resp.Contributors = append(resp.Contributors, c)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}
