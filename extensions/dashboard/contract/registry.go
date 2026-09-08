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
// for negotiation.
type Registry interface {
	Register(m *ContractManifest) error
	Contributor(name string) (*ContractManifest, bool)
	Intent(contributor, intent string, version int) (Intent, bool)
	HighestVersion(contributor, intent string) (int, bool)
	All() []*ContractManifest

	// RegisterRemote records a contributor whose handlers live in another
	// service. The manifest is registered identically to a local one so the
	// capabilities listing works uniformly; the endpoint is what the
	// dispatcher's forwarding layer reads to know where to send envelopes.
	// Slice (m) added this.
	RegisterRemote(m *ContractManifest, endpoint RemoteEndpoint) error

	// IsRemote reports whether the named contributor was registered via
	// RegisterRemote.
	IsRemote(contributor string) bool

	// Remote returns the upstream endpoint for a contributor previously
	// registered via RegisterRemote. ok is false for local contributors.
	Remote(contributor string) (RemoteEndpoint, bool)

	// Unregister removes a contributor and all its intents.
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
	highest      map[string]int // "contributor:intent" -> highest active version
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
	r.contributors[name] = m
	return nil
}

// RegisterRemote registers a contributor whose handlers live in another
// service. The manifest is registered identically to a local Register call
// so the capabilities listing surfaces the remote uniformly; the endpoint
// is recorded separately for the dispatcher's forwarding layer to look up
// at dispatch time. Slice (m) added this.
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

// Unregister removes a contributor and all derived state (intents,
// highest version map, remote endpoint). Used by discovery loops when a
// remote goes offline. Safe for unknown names.
func (r *registry) Unregister(contributor string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.contributors, contributor)
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
