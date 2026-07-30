package router

import (
	"errors"
	"sync"

	"github.com/xraph/forge/internal/shared"
	"github.com/xraph/go-utils/di"
)

// errContextDetached is returned by a detached syncContext. It only surfaces to
// an interceptor goroutine whose result has already been discarded, so it never
// reaches a handler.
var errContextDetached = errors.New("interceptor context detached: fan-out already resolved")

// contextGuard serializes access to one forge context across a fan-out of
// interceptor goroutines, and can be detached once the fan-out's decision is
// made so stragglers stop touching the context altogether.
type contextGuard struct {
	mu       sync.Mutex
	detached bool
}

// detach makes every syncContext sharing this guard inert. Callers must detach
// before returning from a fan-out that leaves goroutines running: the forge
// context is pooled and reused by the next request, so a straggler writing to
// it after the handler returns corrupts unrelated in-flight requests.
func (g *contextGuard) detach() {
	g.mu.Lock()
	g.detached = true
	g.mu.Unlock()
}

// embeddedContext exists purely so syncContext can embed the context interface.
// Embedding Context directly would create a field named "Context", which
// shadows the interface's own Context() method and stops the wrapper from
// satisfying the interface. Embedding through an alias keeps the method.
type embeddedContext = Context

// syncContext serializes the context operations that mutate state shared by
// every interceptor running under Parallel/ParallelAny.
//
// The underlying forge context carries a plain map for values and lazily
// creates its DI scope on first use, with no synchronization of its own —
// it is built for one goroutine per request. Handing the same context to N
// interceptor goroutines therefore races, and an unsynchronized map write is
// a fatal runtime error that recover() cannot catch, so it takes the process
// down rather than just failing the request. Racing the lazy scope init also
// orphans a scope that is never ended.
//
// Only the state-mutating surface is wrapped. Reads of immutable request data
// (Param, Query, Header, Request, Response) pass through untouched, as do the
// response writers — interceptors are expected to allow/block and enrich, not
// to write responses, and a mutex here would not make concurrent response
// writes correct anyway.
type syncContext struct {
	embeddedContext // everything not overridden below passes straight through

	guard *contextGuard
}

// newSyncContext wraps ctx so it can be shared across interceptor goroutines.
// Every wrapper produced for one fan-out must share a single guard.
func newSyncContext(ctx Context, guard *contextGuard) Context {
	return &syncContext{embeddedContext: ctx, guard: guard}
}

// --- values map ---

func (c *syncContext) Set(key string, value any) {
	c.guard.mu.Lock()
	defer c.guard.mu.Unlock()

	if c.guard.detached {
		return
	}

	c.embeddedContext.Set(key, value)
}

func (c *syncContext) Get(key string) any {
	c.guard.mu.Lock()
	defer c.guard.mu.Unlock()

	if c.guard.detached {
		return nil
	}

	return c.embeddedContext.Get(key)
}

func (c *syncContext) MustGet(key string) any {
	c.guard.mu.Lock()
	defer c.guard.mu.Unlock()

	if c.guard.detached {
		return nil
	}

	return c.embeddedContext.MustGet(key)
}

// --- lazily created DI scope ---

func (c *syncContext) Scope() di.Scope {
	c.guard.mu.Lock()
	defer c.guard.mu.Unlock()

	if c.guard.detached {
		return nil
	}

	return c.embeddedContext.Scope()
}

func (c *syncContext) Resolve(name string) (any, error) {
	c.guard.mu.Lock()
	defer c.guard.mu.Unlock()

	if c.guard.detached {
		return nil, errContextDetached
	}

	return c.embeddedContext.Resolve(name)
}

func (c *syncContext) Must(name string) any {
	c.guard.mu.Lock()
	defer c.guard.mu.Unlock()

	if c.guard.detached {
		return nil
	}

	return c.embeddedContext.Must(name)
}

// --- lazily loaded session ---

func (c *syncContext) Session() (shared.Session, error) {
	c.guard.mu.Lock()
	defer c.guard.mu.Unlock()

	if c.guard.detached {
		return nil, errContextDetached
	}

	return c.embeddedContext.Session()
}

func (c *syncContext) SetSession(session shared.Session) {
	c.guard.mu.Lock()
	defer c.guard.mu.Unlock()

	if c.guard.detached {
		return
	}

	c.embeddedContext.SetSession(session)
}

func (c *syncContext) SaveSession() error {
	c.guard.mu.Lock()
	defer c.guard.mu.Unlock()

	if c.guard.detached {
		return errContextDetached
	}

	return c.embeddedContext.SaveSession()
}

func (c *syncContext) DestroySession() error {
	c.guard.mu.Lock()
	defer c.guard.mu.Unlock()

	if c.guard.detached {
		return errContextDetached
	}

	return c.embeddedContext.DestroySession()
}

func (c *syncContext) GetSessionValue(key string) (any, bool) {
	c.guard.mu.Lock()
	defer c.guard.mu.Unlock()

	if c.guard.detached {
		return nil, false
	}

	return c.embeddedContext.GetSessionValue(key)
}

func (c *syncContext) SetSessionValue(key string, value any) {
	c.guard.mu.Lock()
	defer c.guard.mu.Unlock()

	if c.guard.detached {
		return
	}

	c.embeddedContext.SetSessionValue(key, value)
}

func (c *syncContext) DeleteSessionValue(key string) {
	c.guard.mu.Lock()
	defer c.guard.mu.Unlock()

	if c.guard.detached {
		return
	}

	c.embeddedContext.DeleteSessionValue(key)
}
