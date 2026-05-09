// registry.go
package contract

import (
	"fmt"
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
	r.mu.RLock()
	defer r.mu.RUnlock()
	g, ok := r.mergedGraphs[contributor]
	if !ok {
		return nil, false
	}
	if route == "" {
		if len(g) > 0 {
			return &g[0], true
		}
		return nil, false
	}
	for i := range g {
		if g[i].Route == route {
			return &g[i], true
		}
	}
	return nil, false
}
