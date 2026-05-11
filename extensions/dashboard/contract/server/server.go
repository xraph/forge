// Package server exposes the two HTTP endpoints a non-dashboard service
// needs to advertise itself as a contract contributor that other dashboards
// can discover + dispatch into.
//
// The dashboard extension already serves /api/dashboard/v1 for its own
// React shell. A service that doesn't host the dashboard — but wants to
// contribute intents into one — uses this helper to mount the equivalent
// surface at /_forge/contract/{manifest,dispatch}. Slice (m) introduced it
// so multiple microservices can feed a single dashboard, mirroring the
// legacy templ-based dashboard's remote contributor model.
package server

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/xraph/forge/extensions/dashboard/contract"
	"github.com/xraph/forge/extensions/dashboard/contract/transport"
)

// DefaultPrefix is the path the dashboard's RemoteContractContributor
// client expects to find a peer's contract endpoints under.
const DefaultPrefix = "/_forge/contract"

// Server bundles the manifest endpoint and the dispatch endpoint into a
// single http.Handler. Mount it on any net/http mux (or forge.Router via
// the wrapper helper below). The dispatch endpoint reuses the contract's
// existing transport.NewHandler so request semantics — envelope parsing,
// CSRF, idempotency, kind/capability matching, warden checks, audit —
// stay identical between a dashboard's own /api/dashboard/v1 and a
// remote service's /_forge/contract/dispatch.
type Server struct {
	reg      contract.Registry
	dispatch http.Handler
	prefix   string
}

// Option configures a Server.
type Option func(*Server)

// WithPrefix overrides DefaultPrefix. Useful when the service mounts the
// contract endpoints under a custom path (proxy compatibility, etc.).
func WithPrefix(p string) Option {
	return func(s *Server) {
		s.prefix = strings.TrimRight(p, "/")
		if s.prefix == "" {
			s.prefix = DefaultPrefix
		}
	}
}

// New returns a Server configured to serve the registry + dispatcher
// passed in. The supplied audit emitter is plumbed through to the
// dispatch handler; pass contract.NoopAuditEmitter{} when not needed.
func New(
	reg contract.Registry,
	wreg contract.WardenRegistry,
	disp transport.Dispatcher,
	audit contract.AuditEmitter,
	opts ...Option,
) *Server {
	s := &Server{
		reg:      reg,
		dispatch: transport.NewHandler(reg, wreg, disp, audit),
		prefix:   DefaultPrefix,
	}
	for _, o := range opts {
		o(s)
	}
	return s
}

// Prefix returns the configured URL prefix (DefaultPrefix unless overridden).
func (s *Server) Prefix() string { return s.prefix }

// ManifestPath returns the absolute path to the manifest endpoint.
func (s *Server) ManifestPath() string { return s.prefix + "/manifest" }

// DispatchPath returns the absolute path to the dispatch endpoint.
func (s *Server) DispatchPath() string { return s.prefix + "/dispatch" }

// ServeHTTP routes inbound requests to the manifest or dispatch handler
// based on the suffix beneath Prefix(). 404 for anything else.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	switch path {
	case s.ManifestPath():
		s.handleManifest(w, r)
	case s.DispatchPath():
		s.dispatch.ServeHTTP(w, r)
	default:
		http.NotFound(w, r)
	}
}

// HandleManifest is exported so callers wiring the manifest endpoint
// onto a custom router (without going through ServeHTTP) can mount it
// directly.
func (s *Server) HandleManifest(w http.ResponseWriter, r *http.Request) {
	s.handleManifest(w, r)
}

// HandleDispatch exposes the dispatch handler for the same reason.
func (s *Server) HandleDispatch(w http.ResponseWriter, r *http.Request) {
	s.dispatch.ServeHTTP(w, r)
}

func (s *Server) handleManifest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET required", http.StatusMethodNotAllowed)
		return
	}
	contributor := r.URL.Query().Get("contributor")
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache")
	if contributor != "" {
		m, ok := s.reg.Contributor(contributor)
		if !ok {
			http.Error(w, "contributor not registered", http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(m)
		return
	}
	// No ?contributor: when the service hosts exactly one contributor,
	// return that manifest directly so simple single-contributor services
	// match the legacy /_forge/dashboard/manifest contract. With multiple
	// contributors, return the catalog so callers can pick.
	all := s.reg.All()
	switch len(all) {
	case 0:
		http.Error(w, "no contributors registered", http.StatusNotFound)
	case 1:
		_ = json.NewEncoder(w).Encode(all[0])
	default:
		_ = json.NewEncoder(w).Encode(struct {
			Manifests []*contract.ContractManifest `json:"manifests"`
		}{Manifests: all})
	}
}
