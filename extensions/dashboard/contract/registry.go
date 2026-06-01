// registry.go
package contract

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
)

// RemoteEndpoint describes how to reach a contract contributor that lives in
// another service. Slice (m) introduced this so the dispatcher's
// forwarding layer knows where to send envelopes for a contributor whose
// handlers are out-of-process.
type RemoteEndpoint struct {
	// BaseURL is the upstream service's root, including any path prefix
	// (e.g. https://svc.internal:8443 or /proxied/svc). The forwarding
	// client appends "/_forge/contract/dispatch" for envelope POSTs and
	// "/_forge/contract/manifest" for manifest fetches.
	BaseURL string

	// APIKey, when non-empty, is sent as Authorization: Bearer <key> on
	// every forwarded envelope so the upstream can authenticate the
	// dashboard. Inbound user headers (Authorization, Cookie) are still
	// forwarded so the upstream sees the end-user identity too — the
	// API key authenticates the dashboard itself; user identity flows in
	// parallel.
	APIKey string

	// Client overrides the http.Client used to talk to this remote.
	// nil = a default client with a 10s timeout.
	Client *http.Client
}

// Registry holds all registered contributor manifests and provides
// lookup by (contributor, intent, version) plus highest-active-version queries
// for negotiation. It also stores per-contributor merged graphs reflecting any
// cross-contributor slot extensions applied at registration time.
type Registry interface {
	Register(m *ContractManifest) error
	Contributor(name string) (*ContractManifest, bool)
	Intent(contributor, intent string, version int) (Intent, bool)
	HighestVersion(contributor, intent string) (int, bool)
	All() []*ContractManifest
	MergedGraph(contributor, route string) (*GraphNode, bool)
	// MatchRoute is MergedGraph plus :name-style placeholder matching. On a
	// match the returned map carries the extracted segment values keyed by
	// placeholder name. Exact route matches return an empty (non-nil) map.
	// Slice (j) added this for deep-link detail routes; MergedGraph remains
	// for callers that don't care about params.
	MatchRoute(contributor, route string) (*GraphNode, map[string]string, bool)

	// RegisterRemote records a contributor whose handlers live in another
	// service. The manifest is registered identically to a local one so the
	// graph endpoint and capabilities listing work uniformly; the endpoint
	// is what the dispatcher's forwarding layer reads to know where to send
	// envelopes. Slice (m) added this.
	RegisterRemote(m *ContractManifest, endpoint RemoteEndpoint) error

	// IsRemote reports whether the named contributor was registered via
	// RegisterRemote.
	IsRemote(contributor string) bool

	// Remote returns the upstream endpoint for a contributor previously
	// registered via RegisterRemote. ok is false for local contributors.
	Remote(contributor string) (RemoteEndpoint, bool)

	// Unregister removes a contributor and all its intents + merged graph.
	// Used by discovery loops to clean up offline remotes; safe to call
	// for unknown names.
	Unregister(contributor string)
}

// NewRegistry returns an empty registry.
func NewRegistry() Registry {
	return &registry{
		contributors: map[string]*ContractManifest{},
		intents:      map[intentKey]Intent{},
		highest:      map[string]int{},
		mergedGraphs: map[string][]GraphNode{},
		remotes:      map[string]RemoteEndpoint{},
	}
}

type intentKey struct {
	contributor string
	intent      string
	version     int
}

type registry struct {
	mu           sync.RWMutex
	contributors map[string]*ContractManifest
	intents      map[intentKey]Intent
	highest      map[string]int         // "contributor:intent" -> highest active version
	mergedGraphs map[string][]GraphNode // contributor name -> deep-copied graph with extensions applied
	remotes      map[string]RemoteEndpoint
}

func (r *registry) Register(m *ContractManifest) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.registerLocked(m)
}

// registerLocked is the merge body shared by Register and RegisterRemote.
// Caller must hold r.mu for writes.
func (r *registry) registerLocked(m *ContractManifest) error {
	if m == nil {
		return fmt.Errorf("nil manifest")
	}
	name := m.Contributor.Name
	if name == "" {
		return fmt.Errorf("manifest missing contributor.name")
	}
	if _, exists := r.contributors[name]; exists {
		return fmt.Errorf("contributor %q already registered", name)
	}
	for _, in := range m.Intents {
		k := intentKey{name, in.Name, in.Version}
		if _, dup := r.intents[k]; dup {
			return fmt.Errorf("contributor %q intent %q version %d declared twice", name, in.Name, in.Version)
		}
		r.intents[k] = in
		hk := name + ":" + in.Name
		if in.Deprecated == nil {
			if r.highest[hk] < in.Version {
				r.highest[hk] = in.Version
			}
		} else if _, hasHigher := r.highest[hk]; !hasHigher {
			// only set if no active version has been seen yet; deprecated falls back
			r.highest[hk] = in.Version
		}
	}
	// Validate the manifest's own graph against depth/cycle/slot-accept rules.
	if err := checkDepth(m.Graph, 0); err != nil {
		return fmt.Errorf("contributor %q: %w", name, err)
	}
	if err := checkCycle(m.Graph, map[string]bool{}); err != nil {
		return fmt.Errorf("contributor %q: %w", name, err)
	}
	if err := validateGraphSlots(m.Graph); err != nil {
		return fmt.Errorf("contributor %q: %w", name, err)
	}
	r.contributors[name] = m
	// Compute merged graphs: deep-copy own graph, then apply this manifest's
	// extends against ALL contributors (including ones registered earlier).
	// QueryRef indirections (data: queries.health) are flattened into
	// {intent, params, kind} here so the wire envelope the React shell
	// receives always has node.data.intent populated AND node.data.kind set.
	// Without this the shell's useContractQuery falls back to intent="" and
	// the dashboard hits "intent  not registered" the moment a page tries
	// to load data; without kind, metric.counter blindly subscribes to
	// query intents and gets rejected with "intent not a subscription".
	intentKinds := intentKindMap(m.Intents)
	r.mergedGraphs[name] = deepCopyGraph(m.Graph)
	resolveDataBindings(r.mergedGraphs[name], m.Queries, intentKinds)
	// Note: top-level routes stay unprefixed in mergedGraphs. The
	// /@<slug> URL namespace is purely a wire/display concern — the
	// navigation handler and apps.list project the prefix onto NavItem
	// Href and AppInfo.Home, and the React shell strips it back off
	// before issuing the graph query. Keeping mergedGraphs unprefixed
	// means MatchRoute remains per-contributor (no global routing) and
	// graph fixtures in tests stay readable.
	for _, ext := range m.Extends {
		targetGraph, ok := r.mergedGraphs[ext.Target.Contributor]
		if !ok {
			return fmt.Errorf("contributor %q: extension target %q not registered", name, ext.Target.Contributor)
		}
		// Deep-copy + resolve ext.Add before applying so the source
		// manifest's nodes aren't mutated and any QueryRefs in the
		// extension are flattened against THIS manifest's Queries (ext.Add
		// originates from m, not from the target contributor). Same goes
		// for kind stamping: the intents declared in m, not the target's,
		// are what ext.Add can reference.
		adds := deepCopyGraph(ext.Add)
		resolveDataBindings(adds, m.Queries, intentKinds)
		if err := applyExtension(targetGraph, ext.Target, ext.Slot, adds); err != nil {
			return fmt.Errorf("contributor %q: %w", name, err)
		}
	}
	return nil
}

// intentKindMap collects a name → kind lookup table for a manifest's
// declared intents, used during graph merge to stamp DataBinding.Kind.
// When two intent versions share a name, the highest active version
// wins; the resolver only needs the kind, which is invariant across
// versions of the same intent in the current model.
func intentKindMap(intents []Intent) map[string]IntentKind {
	if len(intents) == 0 {
		return nil
	}
	out := make(map[string]IntentKind, len(intents))
	for _, in := range intents {
		out[in.Name] = in.Kind
	}
	return out
}

// RegisterRemote registers a contributor whose handlers live in another
// service. The manifest is merged identically to a local Register call so
// the graph endpoint and capabilities listing surface the remote uniformly;
// the endpoint is recorded separately for the dispatcher's forwarding
// layer to look up at dispatch time. Slice (m) added this.
func (r *registry) RegisterRemote(m *ContractManifest, endpoint RemoteEndpoint) error {
	if m == nil {
		return fmt.Errorf("authsome/contract: nil remote manifest")
	}
	if endpoint.BaseURL == "" {
		return fmt.Errorf("authsome/contract: RemoteEndpoint.BaseURL is required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.registerLocked(m); err != nil {
		return err
	}
	r.remotes[m.Contributor.Name] = endpoint
	return nil
}

// IsRemote reports whether the named contributor was registered via
// RegisterRemote.
func (r *registry) IsRemote(contributor string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.remotes[contributor]
	return ok
}

// Remote returns the endpoint for a contributor previously registered via
// RegisterRemote.
func (r *registry) Remote(contributor string) (RemoteEndpoint, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ep, ok := r.remotes[contributor]
	return ep, ok
}

// Unregister removes a contributor and all derived state (intents, highest
// version map, merged graph, remote endpoint). Used by discovery loops when
// a remote goes offline. Safe for unknown names.
func (r *registry) Unregister(contributor string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.contributors, contributor)
	delete(r.mergedGraphs, contributor)
	delete(r.remotes, contributor)
	for k := range r.intents {
		if k.contributor == contributor {
			delete(r.intents, k)
		}
	}
	prefix := contributor + ":"
	for hk := range r.highest {
		if strings.HasPrefix(hk, prefix) {
			delete(r.highest, hk)
		}
	}
}

func (r *registry) Contributor(name string) (*ContractManifest, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	m, ok := r.contributors[name]
	return m, ok
}

func (r *registry) Intent(contributor, intent string, version int) (Intent, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	in, ok := r.intents[intentKey{contributor, intent, version}]
	return in, ok
}

func (r *registry) HighestVersion(contributor, intent string) (int, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	v, ok := r.highest[contributor+":"+intent]
	return v, ok
}

func (r *registry) All() []*ContractManifest {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*ContractManifest, 0, len(r.contributors))
	for _, m := range r.contributors {
		out = append(out, m)
	}
	return out
}

// MergedGraph returns the merged graph for a contributor (with all extensions
// applied), or false if the contributor is not registered.
// If route is empty, the first top-level node is returned. Otherwise the
// top-level node with a matching Route is returned.
func (r *registry) MergedGraph(contributor, route string) (*GraphNode, bool) {
	n, _, ok := r.MatchRoute(contributor, route)
	return n, ok
}

// MatchRoute performs the same lookup as MergedGraph plus :name-style
// placeholder matching. Exact matches win over param matches. The returned
// map is non-nil on a successful match (empty for exact, populated for
// param-route matches).
func (r *registry) MatchRoute(contributor, route string) (*GraphNode, map[string]string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	g, ok := r.mergedGraphs[contributor]
	if !ok {
		return nil, nil, false
	}
	if route == "" {
		if len(g) > 0 {
			return &g[0], map[string]string{}, true
		}
		return nil, nil, false
	}
	// Pass 1: exact match wins.
	for i := range g {
		if g[i].Route == route {
			return &g[i], map[string]string{}, true
		}
	}
	// Pass 2: pattern match against routes that contain :placeholders.
	for i := range g {
		if !strings.Contains(g[i].Route, ":") {
			continue
		}
		if params, matched := matchRoutePattern(g[i].Route, route); matched {
			return &g[i], params, true
		}
	}
	return nil, nil, false
}

// matchRoutePattern matches a request URL against a route template containing
// :name placeholders. Returns the extracted name->value map and true on match.
// Both inputs are matched segment-by-segment after splitting on '/'; segment
// counts must agree and each :name segment captures exactly one URL segment.
//
// /traces/:id matches /traces/abc123 (params: id=abc123).
// /traces/:id does not match /traces or /traces/abc/extra.
func matchRoutePattern(pattern, path string) (map[string]string, bool) {
	pSegs := splitRoute(pattern)
	uSegs := splitRoute(path)
	if len(pSegs) != len(uSegs) {
		return nil, false
	}
	out := map[string]string{}
	for i, ps := range pSegs {
		us := uSegs[i]
		if len(ps) > 0 && ps[0] == ':' {
			if us == "" {
				return nil, false
			}
			out[ps[1:]] = us
			continue
		}
		if ps != us {
			return nil, false
		}
	}
	return out, true
}

// splitRoute breaks a route on '/' and drops empty leading/trailing segments
// so /a/b and a/b/ both yield [a b].
func splitRoute(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, "/")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p == "" {
			continue
		}
		out = append(out, p)
	}
	return out
}
