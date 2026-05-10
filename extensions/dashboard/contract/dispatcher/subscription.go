package dispatcher

import (
	"context"
	"fmt"

	"github.com/xraph/forge/extensions/dashboard/contract"
	"github.com/xraph/forge/extensions/dashboard/contract/transport"
)

// RegisterSubscription binds a subscription handler to (contributor, intent, version).
func (d *Dispatcher) RegisterSubscription(contributor, intent string, version int, h SubscriptionHandler) error {
	if h == nil {
		return fmt.Errorf("dispatcher: nil subscription handler for %s/%s@%d", contributor, intent, version)
	}
	k := handlerKey{contributor, intent, version}
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, exists := d.subscriptions[k]; exists {
		return fmt.Errorf("dispatcher: subscription %s/%s@%d already registered", contributor, intent, version)
	}
	d.subscriptions[k] = h
	return nil
}

// Subscribe implements transport.SubscriptionSource. The broker calls this on
// each subscribe-control message; the dispatcher routes to the registered handler.
// Params from YAML (map[string]contract.ParamSource) are flattened into a
// runtime map[string]any using the From string when set, the literal Value otherwise.
func (d *Dispatcher) Subscribe(ctx context.Context, p contract.Principal, contributor string, intent contract.Intent, params map[string]contract.ParamSource) (<-chan contract.StreamEvent, func(), error) {
	k := handlerKey{contributor, intent.Name, intent.Version}
	d.mu.RLock()
	h, ok := d.subscriptions[k]
	d.mu.RUnlock()
	if !ok {
		return nil, nil, &contract.Error{Code: contract.CodeNotFound, Message: fmt.Sprintf("subscription %s/%s@%d not registered", contributor, intent.Name, intent.Version)}
	}
	flat := flattenParams(params)
	return h(ctx, flat, p)
}

func flattenParams(in map[string]contract.ParamSource) map[string]any {
	out := make(map[string]any, len(in))
	for k, src := range in {
		if src.From != "" {
			out[k] = src.From // resolution happens caller-side; the handler sees the bound value if any
			continue
		}
		out[k] = src.Value
	}
	return out
}

// Compile-time check that Subscribe satisfies the broker's source interface.
var _ transport.SubscriptionSource = (*Dispatcher)(nil)
