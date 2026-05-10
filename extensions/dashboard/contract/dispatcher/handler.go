package dispatcher

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/xraph/forge/extensions/dashboard/contract"
)

// Handler is the foundation function-table handler signature for query and
// command intents. Returning a *contract.Error propagates the canonical code
// to the wire; any other error is wrapped as CodeInternal at dispatch time.
type Handler func(ctx context.Context, payload json.RawMessage, params map[string]any, p contract.Principal) (*Result, error)

// SubscriptionHandler is the function-table handler for subscription intents.
// The handler returns a channel of events, a force-stop function, and an
// optional error. Closing the channel signals end-of-stream; cancelling ctx
// is the canonical way to ask the handler to stop emitting.
type SubscriptionHandler func(ctx context.Context, params map[string]any, p contract.Principal) (<-chan contract.StreamEvent, func(), error)

// Result carries the data payload plus optional response-meta overrides.
// Handlers that don't need to influence meta can return &Result{Data: ...};
// handlers that need to add invalidations or override cache hints set the
// extra fields.
type Result struct {
	// Data is the JSON-encoded response body. May be nil for a {data: null} response.
	Data json.RawMessage
	// ExtraInvalidates is appended to the manifest's declared Invalidates.
	ExtraInvalidates []string
	// CacheOverride, when non-nil, replaces the manifest's declared cache hint.
	CacheOverride *contract.CacheHint
}

// IntentRef is the (intent, version) tuple used as a registration key.
type IntentRef struct {
	Intent  string
	Version int
}

// String formats as "intent@version" — used in error messages and logs.
func (r IntentRef) String() string {
	return fmt.Sprintf("%s@%d", r.Intent, r.Version)
}

// Contributor is layer (b)'s registration shape: a contributor publishes its
// handler and subscription tables, and the dispatcher walks them on Register.
type Contributor interface {
	Name() string
	Handlers() map[IntentRef]Handler
	Subscriptions() map[IntentRef]SubscriptionHandler
}
