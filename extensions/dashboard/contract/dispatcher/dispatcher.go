package dispatcher

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/xraph/forge/extensions/dashboard/contract"
	"github.com/xraph/forge/extensions/dashboard/contract/transport"
)

// Dispatcher is the concrete implementation of transport.Dispatcher and
// transport.SubscriptionSource (Subscribe lives in subscription.go).
// Contributors register handlers indexed by (contributor, intent, version);
// dispatch is a map lookup + the handler call wrapped in metrics emission
// and canonical error mapping.
type Dispatcher struct {
	metrics MetricsEmitter

	mu            sync.RWMutex
	handlers      map[handlerKey]Handler
	subscriptions map[handlerKey]SubscriptionHandler
}

type handlerKey struct {
	Contributor string
	Intent      string
	Version     int
}

// New returns a fresh dispatcher. Pass NoopMetricsEmitter{} for tests / dev;
// slice (b) provides a Prometheus-backed implementation.
func New(metrics MetricsEmitter) *Dispatcher {
	if metrics == nil {
		metrics = NoopMetricsEmitter{}
	}
	return &Dispatcher{
		metrics:       metrics,
		handlers:      map[handlerKey]Handler{},
		subscriptions: map[handlerKey]SubscriptionHandler{},
	}
}

// Register binds a query/command handler to a (contributor, intent, version)
// key. Returns an error on duplicate registration.
func (d *Dispatcher) Register(contributor, intent string, version int, h Handler) error {
	if h == nil {
		return fmt.Errorf("dispatcher: nil handler for %s/%s@%d", contributor, intent, version)
	}
	k := handlerKey{contributor, intent, version}
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, exists := d.handlers[k]; exists {
		return fmt.Errorf("dispatcher: handler %s/%s@%d already registered", contributor, intent, version)
	}
	d.handlers[k] = h
	return nil
}

// Dispatch implements transport.Dispatcher.
func (d *Dispatcher) Dispatch(ctx context.Context, req contract.Request, p contract.Principal) (json.RawMessage, contract.ResponseMeta, error) {
	k := handlerKey{req.Contributor, req.Intent, req.IntentVersion}
	d.mu.RLock()
	h, ok := d.handlers[k]
	d.mu.RUnlock()
	if !ok {
		err := &contract.Error{Code: contract.CodeNotFound, Message: fmt.Sprintf("handler %s/%s@%d not registered", req.Contributor, req.Intent, req.IntentVersion)}
		d.metrics.RecordDispatch(ctx, req.Contributor, req.Intent, req.IntentVersion, req.Kind, 0, err.Code)
		return nil, contract.ResponseMeta{}, err
	}

	t0 := time.Now()
	res, handlerErr := h(ctx, req.Payload, req.Params, p)
	latency := time.Since(t0)

	wireErr := mapDispatchError(handlerErr)
	errCode := contract.ErrorCode("")
	if wireErr != nil {
		var ce *contract.Error
		if errors.As(wireErr, &ce) {
			errCode = ce.Code
		}
	}
	d.metrics.RecordDispatch(ctx, req.Contributor, req.Intent, req.IntentVersion, req.Kind, latency, errCode)

	if wireErr != nil {
		return nil, contract.ResponseMeta{}, wireErr
	}
	if res == nil {
		// Allow nil result to mean {data: null} explicitly.
		return nil, contract.ResponseMeta{IntentVersion: req.IntentVersion}, nil
	}
	meta := contract.ResponseMeta{IntentVersion: req.IntentVersion}
	if len(res.ExtraInvalidates) > 0 {
		meta.Invalidates = append(meta.Invalidates, res.ExtraInvalidates...)
	}
	if res.CacheOverride != nil {
		meta.CacheControl = res.CacheOverride
	}
	return res.Data, meta, nil
}

// mapDispatchError converts a handler error into the canonical wire error
// shape. *contract.Error is preserved verbatim. context.Canceled becomes
// CodeUnavailable+Retryable. Any other error is wrapped as CodeInternal,
// with the original chained for server-side logging.
func mapDispatchError(err error) error {
	if err == nil {
		return nil
	}
	var ce *contract.Error
	if errors.As(err, &ce) {
		return ce
	}
	if errors.Is(err, context.Canceled) {
		return &contract.Error{Code: contract.CodeUnavailable, Message: "request cancelled", Retryable: true}
	}
	log.Printf("dispatcher: unmapped handler error: %v", err)
	return &contract.Error{Code: contract.CodeInternal, Message: "internal error"}
}

// Compile-time check that the dispatcher satisfies the transport interface.
// The Subscribe half lands in subscription.go (Phase 2).
var _ transport.Dispatcher = (*Dispatcher)(nil)
