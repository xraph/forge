// registry.go
package contract

import (
	"fmt"
	"strings"
	"sync"
)

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
}

// NewRegistry returns an empty registry.
func NewRegistry() Registry {
	return &registry{
		contributors: map[string]*ContractManifest{},
		intents:      map[intentKey]Intent{},
		highest:      map[string]int{},
		mergedGraphs: map[string][]GraphNode{},
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
	highest      map[string]int            // "contributor:intent" -> highest active version
	mergedGraphs map[string][]GraphNode    // contributor name -> deep-copied graph with extensions applied
}

func (r *registry) Register(m *ContractManifest) error {
	if m == nil {
		return fmt.Errorf("nil manifest")
	}
	name := m.Contributor.Name
	if name == "" {
		return fmt.Errorf("manifest missing contributor.name")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
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
	r.mergedGraphs[name] = deepCopyGraph(m.Graph)
	for _, ext := range m.Extends {
		targetGraph, ok := r.mergedGraphs[ext.Target.Contributor]
		if !ok {
			return fmt.Errorf("contributor %q: extension target %q not registered", name, ext.Target.Contributor)
		}
		if err := applyExtension(targetGraph, ext.Target, ext.Slot, ext.Add); err != nil {
			return fmt.Errorf("contributor %q: %w", name, err)
		}
	}
	return nil
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
