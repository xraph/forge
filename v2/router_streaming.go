package forge

import (
	"net/http"
)

// WebSocket registers a WebSocket handler
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
		ctx := &ctx{
			request:   req,
			response:  w,
			container: r.container,
		}

		// Call handler
		if err := handler(ctx, wsConn); err != nil {
			if r.logger != nil {
				r.logger.Error("websocket handler error")
			}
		}
	}

	// Register as normal route
	return r.register(http.MethodGet, path, httpHandler, opts...)
}

// EventStream registers a Server-Sent Events handler
func (r *router) EventStream(path string, handler SSEHandler, opts ...RouteOption) error {
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
		ctx := &ctx{
			request:   req,
			response:  w,
			container: r.container,
		}

		// Call handler
		if err := handler(ctx, stream); err != nil {
			if r.logger != nil {
				r.logger.Error("SSE handler error")
			}
		}
	}

	// Register as normal route
	return r.register(http.MethodGet, path, httpHandler, opts...)
}
