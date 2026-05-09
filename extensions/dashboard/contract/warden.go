package contract

import (
	"context"
	"fmt"
	"sync"

	dashauth "github.com/xraph/forge/extensions/dashboard/auth"
)

// Warden is the pluggable, data-aware authorization second pass.
// It runs after the YAML boolean Predicate succeeds and may inspect
// intent params (e.g. tenant ownership), claims, or external policy.
type Warden interface {
	Authorize(ctx context.Context, p Principal, a Action) (Decision, error)
}

// Principal is the caller identity passed to Wardens and the predicate engine.
type Principal struct {
	User   *dashauth.UserInfo
	Claims map[string]any
}

// PrincipalFor builds a Principal from a UserInfo, copying claims for safety.
func PrincipalFor(user *dashauth.UserInfo) Principal {
	if user == nil {
		return Principal{}
	}
	claims := make(map[string]any, len(user.Claims))
	for k, v := range user.Claims {
		claims[k] = v
	}
	return Principal{User: user, Claims: claims}
}

// Action is the operation being authorized.
type Action struct {
	Contributor string
	Intent      string
	Kind        IntentKind
	Capability  Capability
	Resource    map[string]any
}

// Decision is the Warden's verdict.
type Decision struct {
	// Allow reports whether access is granted.
	Allow bool
	// Reason is a short, human-readable explanation. Surfaced in audit logs
	// and (optionally) in error responses.
	Reason string
	// Redactions lists JSONPath-like field paths that must be redacted from
	// the response payload even when Allow is true. Empty when no redactions
	// apply.
	Redactions []string
}

// WardenRegistry maps a Warden's declared name to its implementation.
// Manifest validation rejects YAML that references a name not in the registry.
type WardenRegistry interface {
	Register(name string, w Warden) error
	Get(name string) (Warden, bool)
}

// NewWardenRegistry returns an empty in-memory registry.
func NewWardenRegistry() WardenRegistry {
	return &wardenRegistry{wardens: map[string]Warden{}}
}

type wardenRegistry struct {
	mu      sync.RWMutex
	wardens map[string]Warden
}

func (r *wardenRegistry) Register(name string, w Warden) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.wardens[name]; exists {
		return fmt.Errorf("warden %q already registered", name)
	}
	r.wardens[name] = w
	return nil
}

func (r *wardenRegistry) Get(name string) (Warden, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	w, ok := r.wardens[name]
	return w, ok
}
