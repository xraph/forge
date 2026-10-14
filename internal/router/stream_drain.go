package router

import (
	"context"
	"net/http"
	"sync"
)

// streamDrain ends a router's long-lived streams when the server shuts down.
//
// http.Server.Shutdown waits for every active request to return, and an event
// stream never returns on its own, so without this a graceful shutdown sits out
// its whole timeout. Hijacked WebSocket connections are worse off: Shutdown
// neither waits for them nor tells them to stop, so the process exits under
// them. Plain requests are deliberately left alone and still finish normally.
//
// One drain is shared by a router and all of its groups.
type streamDrain struct {
	ctx    context.Context //nolint:containedctx // the drain signal every stream handler watches
	cancel context.CancelFunc

	mu     sync.Mutex
	active int
	idle   chan struct{} // closed when active reaches zero while someone waits
}

func newStreamDrain() *streamDrain {
	ctx, cancel := context.WithCancel(context.Background())

	return &streamDrain{ctx: ctx, cancel: cancel}
}

// enter registers a stream handler. It returns the request to hand the handler,
// whose context also ends when the drain starts, and a function the handler
// must defer. A stream that opens after the drain started ends straight away.
func (d *streamDrain) enter(req *http.Request) (*http.Request, func()) {
	if d == nil {
		return req, func() {}
	}

	d.mu.Lock()
	d.active++
	d.mu.Unlock()

	ctx, cancel := context.WithCancel(req.Context())
	stop := context.AfterFunc(d.ctx, cancel)

	return req.WithContext(ctx), func() {
		stop()
		cancel()
		d.leave()
	}
}

func (d *streamDrain) leave() {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.active--
	if d.active == 0 && d.idle != nil {
		close(d.idle)
		d.idle = nil
	}
}

// drain ends every open stream and waits for their handlers to return, or for
// ctx to end, whichever comes first.
func (d *streamDrain) drain(ctx context.Context) error {
	if d == nil {
		return nil
	}

	d.cancel()

	d.mu.Lock()
	if d.active == 0 {
		d.mu.Unlock()

		return nil
	}

	if d.idle == nil {
		d.idle = make(chan struct{})
	}

	idle := d.idle
	d.mu.Unlock()

	select {
	case <-idle:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// DrainStreams ends every SSE stream and WebSocket connection this router
// serves, and waits until their handlers have returned or ctx ends.
//
// Stream handlers see their request context cancelled; a WebSocket client is
// sent a 1001 (going away) close frame first, so it can reconnect elsewhere.
// Plain requests are not touched. Call it when a graceful shutdown starts,
// alongside http.Server.Shutdown: it is what lets Shutdown finish while
// streams are open.
//
// The drain is one-shot. Every stream opened on this router after it was
// called ends immediately, so serving again after a graceful shutdown needs a
// new router.
func (r *router) DrainStreams(ctx context.Context) error {
	return r.drain.drain(ctx)
}
