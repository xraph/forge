package router

import (
	"net/http"

	forge_http "github.com/xraph/go-utils/http"
)

// WebSocket registers a WebSocket handler.
func (r *router) WebSocket(path string, handler WebSocketHandler, opts ...RouteOption) error {
	// Convert WebSocketHandler to http.Handler
	httpHandler := func(w http.ResponseWriter, req *http.Request) {
		// Upgrade to WebSocket
		conn, err := upgradeToWebSocket(w, req)
		if err != nil {
			if r.logger != nil {
				r.logger.Error("failed to upgrade websocket connection")
			}

			http.Error(w, "Failed to upgrade connection", http.StatusInternalServerError)

			return
		}

		// Create WebSocket connection wrapper
		connID := generateConnectionID()

		wsConn := newWSConnection(connID, conn, req.Context())
		defer wsConn.Close()

		// Create context
		ctx := forge_http.NewContext(w, req, r.container)

		// Call handler
		if err := handler(ctx, wsConn); err != nil {
			if r.logger != nil {
				r.logger.Error("websocket handler error")
			}
		}
	}

	// Add route type marker for AsyncAPI
	optsWithType := append([]RouteOption{WithMetadata("route.type", "websocket")}, opts...)

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

		// Create context
		ctx := forge_http.NewContext(w, req, r.container)

		// Call handler
		if err := handler(ctx, stream); err != nil {
			if r.logger != nil {
				r.logger.Error("SSE handler error")
			}
		}
	}

	// Add route type marker for AsyncAPI
	optsWithType := append([]RouteOption{WithMetadata("route.type", "sse")}, opts...)

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

		// Create context
		ctx := forge_http.NewContext(w, req, r.container)

		// Call handler - user can now use ctx.WriteSSE()
		if err := handler(ctx); err != nil {
			if r.logger != nil {
				r.logger.Error("SSE handler error")
			}
		}
	}

	// Add route type marker for AsyncAPI
	optsWithType := append([]RouteOption{WithMetadata("route.type", "sse")}, opts...)

	// Register with configurable method
	return r.register(method, path, wrappedHandler, optsWithType...)
}
