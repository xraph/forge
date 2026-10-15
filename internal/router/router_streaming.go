package router

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/gobwas/ws/wsutil"
	forge_http "github.com/xraph/go-utils/http"
	logger "github.com/xraph/go-utils/log"

	"github.com/xraph/forge/internal/shared"
)

// WebSocket registers a WebSocket handler.
func (r *router) WebSocket(path string, handler WebSocketHandler, opts ...RouteOption) error {
	// Convert WebSocketHandler to http.Handler
	httpHandler := func(w http.ResponseWriter, req *http.Request) {
		req, leave := r.drain.enter(req)
		defer leave()

		// Upgrade to WebSocket (validates Origin first)
		conn, err := upgradeToWebSocket(w, req, r.webSocketOrigins)
		if err != nil {
			if errors.Is(err, errOriginNotAllowed) {
				if r.logger != nil {
					r.logger.Warn("websocket upgrade rejected: origin not allowed")
				}

				http.Error(w, "Forbidden", http.StatusForbidden)

				return
			}

			if r.logger != nil {
				r.logger.Error("failed to upgrade websocket connection")
			}

			// ws.UpgradeHTTP has already written its own error response to the
			// client, so do not write a second status line here.
			return
		}

		shared.RouteMatchFromContext(req.Context()).Establish(http.StatusSwitchingProtocols)

		handle := r.openStream(shared.KindWebSocket, shared.RouteMatchFromContext(req.Context()).PatternOrEmpty())

		var handlerErr error
		defer closeStream(handle, &handlerErr)

		// Create WebSocket connection wrapper
		connID := generateConnectionID()

		wsConn := newWSConnection(connID, conn, req.Context())
		wsConn.handle = handle

		defer wsConn.Close()

		// Shutdown does not track a hijacked connection, so a drain tells the
		// client itself and closes the socket, which also unblocks a handler
		// waiting in Read.
		if r.drain != nil {
			stop := context.AfterFunc(r.drain.ctx, wsConn.goAway)
			defer stop()
		}

		// Create context. Cleanup releases the DI scope and any multipart temp
		// files; without it every connection leaks them for process life.
		ctx := newContext(w, req, r.container)
		defer ctx.(forge_http.ContextWithClean).Cleanup()

		// Call handler
		if err := handler(ctx, wsConn); err != nil {
			handlerErr = clientCloseAsEOF(err)

			if r.logger != nil {
				r.logger.Error("websocket handler error")
			}
		}
	}

	// Add route type marker for AsyncAPI
	optsWithType := append([]RouteOption{WithRouteKind(KindWebSocket)}, opts...)

	// Register as normal route
	return r.register(http.MethodGet, path, httpHandler, optsWithType...)
}

// EventStream registers a Server-Sent Events handler.
func (r *router) EventStream(path string, handler SSEHandler, opts ...RouteOption) error {
	// Build config to check for method override
	routeConfig := &RouteConfig{}
	for _, opt := range opts {
		opt.Apply(routeConfig)
	}

	// Use GET as default, allow override
	method := http.MethodGet
	if routeConfig.Method != "" {
		method = routeConfig.Method
	}

	// Convert SSEHandler to http.Handler
	httpHandler := func(w http.ResponseWriter, req *http.Request) {
		req, leave := r.drain.enter(req)
		defer leave()

		// Create SSE stream
		config := DefaultStreamConfig()

		stream, err := newSSEStream(w, req, config.RetryInterval)
		if err != nil {
			if r.logger != nil {
				r.logger.Error("failed to create SSE stream")
			}

			http.Error(w, "Streaming not supported", http.StatusInternalServerError)

			return
		}

		shared.RouteMatchFromContext(req.Context()).Establish(http.StatusOK)

		handle := r.openStream(shared.KindSSE, shared.RouteMatchFromContext(req.Context()).PatternOrEmpty())
		stream.handle = handle

		var handlerErr error
		defer closeStream(handle, &handlerErr)

		// Deferred after closeStream so it runs first: once the stream is
		// closed, a goroutine still calling Send gets an error and counts
		// nothing, so no Sent can follow the handle's Closed.
		defer stream.Close()

		// Create context. Cleanup releases the DI scope and any multipart temp
		// files; long-lived streams are exactly where leaking them hurts most.
		ctx := newContext(w, req, r.container)
		defer ctx.(forge_http.ContextWithClean).Cleanup()

		// A route with a log configured replays the client's gap and then hands
		// the handler a stream that records what it sends. Without one, the
		// handler gets the raw stream and the route behaves exactly as before.
		handlerStream := Stream(stream)

		if routeConfig.EventLog != nil && routeConfig.EventLogChannel != nil {
			channel := routeConfig.EventLogChannel(ctx)

			// Deliberately used whether or not the replay succeeded: a broken
			// log costs resumability, never the stream. Returning here would
			// close a 200 with no body, and the client would reconnect from the
			// same position into the same failure — an unbreakable loop for any
			// persistently failing shared log, which is exactly the
			// multi-instance deployment this feature exists to support.
			handlerStream, err = resumable(stream, routeConfig.EventLog, channel, routeConfig.EventLogAuthoritative, r.logger)
			if err != nil {
				// Logged with the error value, because a loop nobody can name
				// the cause of is a loop nobody can fix.
				if r.logger != nil {
					r.logger.Error("SSE replay failed",
						logger.String("error", err.Error()),
					)
				}

				// Best effort. If this write fails too the connection is gone
				// anyway, and the client's grace window settles it.
				_ = stream.SendJSON(EventGap, GapPayload{Reason: "unresumable"})
			}
		}

		// Call handler
		if err := handler(ctx, handlerStream); err != nil {
			handlerErr = err

			if r.logger != nil {
				r.logger.Error("SSE handler error")
			}
		}
	}

	// Add route type marker for AsyncAPI
	optsWithType := append([]RouteOption{WithRouteKind(KindSSE)}, opts...)

	// Register with configurable method
	return r.register(method, path, httpHandler, optsWithType...)
}

// SSE registers a Server-Sent Events handler with automatic header setup.
// This is a convenience method that automatically sets SSE headers and uses
// the standard Handler signature. For low-level control, use EventStream instead.
func (r *router) SSE(path string, handler Handler, opts ...RouteOption) error {
	// Build config to check for method override
	routeConfig := &RouteConfig{}
	for _, opt := range opts {
		opt.Apply(routeConfig)
	}

	// Use GET as default, allow override
	method := http.MethodGet
	if routeConfig.Method != "" {
		method = routeConfig.Method
	}

	// Wrap handler to set SSE headers automatically
	wrappedHandler := func(w http.ResponseWriter, req *http.Request) {
		req, leave := r.drain.enter(req)
		defer leave()

		// Set SSE headers
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no") // Disable nginx buffering

		// Create context. Cleanup releases the DI scope and any multipart temp
		// files; long-lived streams are exactly where leaking them hurts most.
		ctx := newContext(w, req, r.container)
		defer ctx.(forge_http.ContextWithClean).Cleanup()

		shared.RouteMatchFromContext(req.Context()).Establish(http.StatusOK)

		handle := r.openStream(shared.KindSSE, shared.RouteMatchFromContext(req.Context()).PatternOrEmpty())

		var handlerErr error
		defer closeStream(handle, &handlerErr)

		// Call handler - user can now use ctx.WriteSSE()
		if err := handler(ctx); err != nil {
			handlerErr = err

			if r.logger != nil {
				r.logger.Error("SSE handler error")
			}
		}
	}

	// Add route type marker for AsyncAPI
	optsWithType := append([]RouteOption{WithRouteKind(KindSSE)}, opts...)

	// Register with configurable method
	return r.register(method, path, wrappedHandler, optsWithType...)
}

// closeStream closes a stream's observer handle with the handler's error. Defer
// it directly, so its recover sees a handler panic: the handle then closes with
// the panic as its error, and the panic carries on to the router's recovery. A
// panic with an error value is wrapped, so observers can match it with
// errors.Is (http.ErrAbortHandler, for one).
func closeStream(handle shared.StreamHandle, handlerErr *error) {
	if p := recover(); p != nil {
		if err, ok := p.(error); ok {
			handle.Closed(fmt.Errorf("panic: %w", err))
		} else {
			handle.Closed(fmt.Errorf("panic: %v", p))
		}

		panic(p)
	}

	handle.Closed(*handlerErr)
}

// clientCloseAsEOF reports a WebSocket close frame from the client as io.EOF.
// gobwas returns wsutil.ClosedError when the client closes cleanly, and a
// handler that returns its Read error passes that on. To an observer it means
// the same as a dropped connection: the client went away.
func clientCloseAsEOF(err error) error {
	var closed wsutil.ClosedError
	if errors.As(err, &closed) {
		return io.EOF
	}

	return err
}
