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

	Version    string `json:"version,omitempty"`
	Configured bool   `json:"configured"`
	Message    string `json:"message,omitempty"`
}

// ContributorStatus is what the host reports about one contributor: the
// version a plugin's `requires` range is matched against, and whether the
// contributor has everything it needs to serve.
//
// This mirrors dashboard.DashboardStatus, which is the interface extension
// authors implement. It is redeclared here because package dashboard already
// imports this package, so importing it back would be a cycle. The dashboard
// extension converts between the two at the wiring site.
type ContributorStatus struct {
	Version    string
	Configured bool
	Message    string
}

// ContributorStatusFunc looks up a contributor's status by name. ok is false
// when nothing registered a status for that contributor, in which case the
// handler applies the permissive default (empty version, configured).
type ContributorStatusFunc func(contributor string) (ContributorStatus, bool)

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

// NewCapabilitiesHandler returns the GET /capabilities handler. status may be
// nil, which reports every contributor with the permissive default.
func NewCapabilitiesHandler(reg contract.Registry, shellEnvelopes []string, status ContributorStatusFunc) http.Handler {
	return &capabilitiesHandler{reg: reg, shellEnvelopes: shellEnvelopes, status: status}
}

type capabilitiesHandler struct {
	reg            contract.Registry
	shellEnvelopes []string
	status         ContributorStatusFunc
}

func (h *capabilitiesHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET required", http.StatusMethodNotAllowed)
		return
	}
	resp := CapabilitiesResponse{ShellEnvelopes: h.shellEnvelopes}
	for _, m := range h.reg.All() {
		c := ContributorCapability{Name: m.Contributor.Name, Envelopes: m.Contributor.Envelope.Supports}
		// Default to configured. A contributor that never registered a
		// status — the dashboard's own core contributor, any remote — is
		// reported as ready, not as needing setup. bool's zero value is the
		// wrong way round here, so it is set explicitly.
		st := ContributorStatus{Configured: true}
		if h.status != nil {
			if s, ok := h.status(m.Contributor.Name); ok {
				st = s
			}
		}
		c.Version, c.Configured, c.Message = st.Version, st.Configured, st.Message
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
