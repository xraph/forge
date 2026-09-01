package router

import (
	"errors"
	"net/http"

	forge_http "github.com/xraph/go-utils/http"
	logger "github.com/xraph/go-utils/log"
)

// WebSocket registers a WebSocket handler.
func (r *router) WebSocket(path string, handler WebSocketHandler, opts ...RouteOption) error {
	// Convert WebSocketHandler to http.Handler
	httpHandler := func(w http.ResponseWriter, req *http.Request) {
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

		// Create WebSocket connection wrapper
		connID := generateConnectionID()

		wsConn := newWSConnection(connID, conn, req.Context())
		defer wsConn.Close()

		// Create context. Cleanup releases the DI scope and any multipart temp
		// files; without it every connection leaks them for process life.
		ctx := forge_http.NewContext(w, req, r.container)
		defer ctx.(forge_http.ContextWithClean).Cleanup()

		// Call handler
		if err := handler(ctx, wsConn); err != nil {
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
		defer stream.Close()

		// Create context. Cleanup releases the DI scope and any multipart temp
		// files; long-lived streams are exactly where leaking them hurts most.
		ctx := forge_http.NewContext(w, req, r.container)
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
		// Set SSE headers
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no") // Disable nginx buffering

		// Create context. Cleanup releases the DI scope and any multipart temp
		// files; long-lived streams are exactly where leaking them hurts most.
		ctx := forge_http.NewContext(w, req, r.container)
		defer ctx.(forge_http.ContextWithClean).Cleanup()

		// Call handler - user can now use ctx.WriteSSE()
		if err := handler(ctx); err != nil {
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
