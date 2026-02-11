package gateway

import (
	"net/http"
	"sync"
)

// RequestHook is called before a request is proxied.
// Return a non-nil error to reject the request.
type RequestHook func(r *http.Request, route *Route) error

// ResponseHook is called after a response is received from upstream.
type ResponseHook func(resp *http.Response, route *Route)

// ErrorHook is called when an upstream error occurs.
type ErrorHook func(err error, route *Route, w http.ResponseWriter)

// RouteChangeHook is called when the route table changes.
type RouteChangeHook func(event RouteEvent)

// UpstreamHealthHook is called when upstream health changes.
type UpstreamHealthHook func(event UpstreamHealthEvent)

// CircuitBreakHook is called when a circuit breaker state changes.
type CircuitBreakHook func(targetID string, from, to CircuitState)

// HookEngine manages gateway hooks.
type HookEngine struct {
	mu sync.RWMutex

	onRequest        []RequestHook
	onResponse       []ResponseHook
	onError          []ErrorHook
	onRouteChange    []RouteChangeHook
	onUpstreamHealth []UpstreamHealthHook
	onCircuitBreak   []CircuitBreakHook
}

// NewHookEngine creates a new hook engine.
func NewHookEngine() *HookEngine {
	return &HookEngine{}
}

// OnRequest registers a request hook.
func (he *HookEngine) OnRequest(fn RequestHook) {
	he.mu.Lock()
	defer he.mu.Unlock()

	he.onRequest = append(he.onRequest, fn)
}

// OnResponse registers a response hook.
func (he *HookEngine) OnResponse(fn ResponseHook) {
	he.mu.Lock()
	defer he.mu.Unlock()

	he.onResponse = append(he.onResponse, fn)
}

// OnError registers an error hook.
func (he *HookEngine) OnError(fn ErrorHook) {
	he.mu.Lock()
	defer he.mu.Unlock()

	he.onError = append(he.onError, fn)
}

// OnRouteChange registers a route change hook.
func (he *HookEngine) OnRouteChange(fn RouteChangeHook) {
	he.mu.Lock()
	defer he.mu.Unlock()

	he.onRouteChange = append(he.onRouteChange, fn)
}

// OnUpstreamHealth registers an upstream health hook.
func (he *HookEngine) OnUpstreamHealth(fn UpstreamHealthHook) {
	he.mu.Lock()
	defer he.mu.Unlock()

	he.onUpstreamHealth = append(he.onUpstreamHealth, fn)
}

// OnCircuitBreak registers a circuit break hook.
func (he *HookEngine) OnCircuitBreak(fn CircuitBreakHook) {
	he.mu.Lock()
	defer he.mu.Unlock()

	he.onCircuitBreak = append(he.onCircuitBreak, fn)
}

// RunOnRequest runs all request hooks. Returns first error encountered.
func (he *HookEngine) RunOnRequest(r *http.Request, route *Route) error {
	he.mu.RLock()
	hooks := make([]RequestHook, len(he.onRequest))
	copy(hooks, he.onRequest)
	he.mu.RUnlock()

	for _, fn := range hooks {
		if err := fn(r, route); err != nil {
			return err
		}
	}

	return nil
}

// RunOnResponse runs all response hooks.
func (he *HookEngine) RunOnResponse(resp *http.Response, route *Route) {
	he.mu.RLock()
	hooks := make([]ResponseHook, len(he.onResponse))
	copy(hooks, he.onResponse)
	he.mu.RUnlock()

	for _, fn := range hooks {
		fn(resp, route)
	}
}

// RunOnError runs all error hooks.
func (he *HookEngine) RunOnError(err error, route *Route, w http.ResponseWriter) {
	he.mu.RLock()
	hooks := make([]ErrorHook, len(he.onError))
	copy(hooks, he.onError)
	he.mu.RUnlock()

	for _, fn := range hooks {
		fn(err, route, w)
	}
}

// RunOnRouteChange runs all route change hooks.
func (he *HookEngine) RunOnRouteChange(event RouteEvent) {
	he.mu.RLock()
	hooks := make([]RouteChangeHook, len(he.onRouteChange))
	copy(hooks, he.onRouteChange)
	he.mu.RUnlock()

	for _, fn := range hooks {
		go fn(event)
	}
}

// RunOnUpstreamHealth runs all upstream health hooks.
func (he *HookEngine) RunOnUpstreamHealth(event UpstreamHealthEvent) {
	he.mu.RLock()
	hooks := make([]UpstreamHealthHook, len(he.onUpstreamHealth))
	copy(hooks, he.onUpstreamHealth)
	he.mu.RUnlock()

	for _, fn := range hooks {
		go fn(event)
	}
}

// RunOnCircuitBreak runs all circuit break hooks.
func (he *HookEngine) RunOnCircuitBreak(targetID string, from, to CircuitState) {
	he.mu.RLock()
	hooks := make([]CircuitBreakHook, len(he.onCircuitBreak))
	copy(hooks, he.onCircuitBreak)
	he.mu.RUnlock()

	for _, fn := range hooks {
		go fn(targetID, from, to)
	}
}
