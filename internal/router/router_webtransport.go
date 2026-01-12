package router

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"
	"github.com/quic-go/webtransport-go"
	forge_http "github.com/xraph/go-utils/http"
	logger "github.com/xraph/go-utils/log"
)

// WebTransport registers a WebTransport handler.
func (r *router) WebTransport(path string, handler WebTransportHandler, opts ...RouteOption) error {
	// WebTransport requires HTTP/3, which needs to be configured separately
	// Store the handler for later use when HTTP/3 server is started

	// Convert WebTransportHandler to http.Handler
	httpHandler := func(w http.ResponseWriter, req *http.Request) {
		// Check if this is a WebTransport request
		if req.Method != http.MethodConnect {
			http.Error(w, "WebTransport requires CONNECT method", http.StatusMethodNotAllowed)

			return
		}

		if req.Proto != "webtransport" {
			http.Error(w, "WebTransport protocol required", http.StatusBadRequest)

			return
		}

		// Upgrade to WebTransport
		session, err := upgradeToWebTransport(w, req, r.webTransportConfig)
		if err != nil {
			if r.logger != nil {
				r.logger.Error("failed to upgrade webtransport session",
					logger.String("error", err.Error()),
				)
			}

			http.Error(w, "Failed to upgrade connection", http.StatusInternalServerError)

			return
		}

		// Create WebTransport session wrapper
		sessionID := generateConnectionID()

		wtSession := newWebTransportSession(sessionID, session, req.Context())
		defer wtSession.Close()

		// Create context
		ctx := forge_http.NewContext(w, req, r.container)

		// Call handler
		if err := handler(ctx, wtSession); err != nil {
			if r.logger != nil {
				r.logger.Error("webtransport handler error",
					logger.String("error", err.Error()),
				)
			}
		}
	}

	// Add route type marker for AsyncAPI
	optsWithType := append([]RouteOption{WithMetadata("route.type", "webtransport")}, opts...)

	// Register as CONNECT route
	return r.register(http.MethodConnect, path, httpHandler, optsWithType...)
}

// upgradeToWebTransport upgrades an HTTP request to WebTransport.
func upgradeToWebTransport(w http.ResponseWriter, r *http.Request, config WebTransportConfig) (*webtransport.Session, error) {
	// Create a WebTransport server
	server := &webtransport.Server{
		CheckOrigin: func(r *http.Request) bool {
			// TODO: Add origin checking
			return true
		},
	}

	// Upgrade to WebTransport session
	session, err := server.Upgrade(w, r)
	if err != nil {
		return nil, fmt.Errorf("failed to upgrade: %w", err)
	}

	return session, nil
}

// EnableWebTransport configures the router to support WebTransport.
func (r *router) EnableWebTransport(config WebTransportConfig) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.webTransportEnabled = true
	r.webTransportConfig = config

	if r.logger != nil {
		r.logger.Info("webtransport support enabled",
			logger.Int64("max_bidi_streams", config.MaxBidiStreams),
			logger.Int64("max_uni_streams", config.MaxUniStreams),
			logger.Bool("enable_datagrams", config.EnableDatagrams),
		)
	}

	return nil
}

// StartHTTP3 starts an HTTP/3 server for WebTransport support.
func (r *router) StartHTTP3(addr string, tlsConfig *tls.Config) error {
	if !r.webTransportEnabled {
		return errors.New("webtransport not enabled")
	}

	if tlsConfig == nil {
		return errors.New("TLS config required for HTTP/3")
	}

	// Configure QUIC
	quicConfig := &quic.Config{
		MaxIdleTimeout:  time.Duration(r.webTransportConfig.MaxIdleTimeout) * time.Millisecond,
		KeepAlivePeriod: time.Duration(r.webTransportConfig.KeepAliveInterval) * time.Millisecond,
		EnableDatagrams: r.webTransportConfig.EnableDatagrams,
		MaxIncomingStreams: r.webTransportConfig.MaxBidiStreams +
			r.webTransportConfig.MaxUniStreams,
		MaxStreamReceiveWindow:     r.webTransportConfig.StreamReceiveWindow,
		MaxConnectionReceiveWindow: r.webTransportConfig.ConnectionReceiveWindow,
	}

	// Create HTTP/3 server
	server := &http3.Server{
		Addr:       addr,
		TLSConfig:  tlsConfig,
		QUICConfig: quicConfig,
		Handler:    r,
	}

	r.mu.Lock()
	r.http3Server = server
	r.mu.Unlock()

	if r.logger != nil {
		r.logger.Info("starting HTTP/3 server for webtransport",
			logger.String("address", addr),
		)
	}

	// Start server in background
	go func() {
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			if r.logger != nil {
				r.logger.Error("HTTP/3 server error",
					logger.String("error", err.Error()),
				)
			}
		}
	}()

	return nil
}

// StopHTTP3 stops the HTTP/3 server.
func (r *router) StopHTTP3() error {
	r.mu.Lock()
	serverInterface := r.http3Server
	r.mu.Unlock()

	if serverInterface == nil {
		return nil
	}

	if r.logger != nil {
		r.logger.Info("stopping HTTP/3 server")
	}

	// Type assert to get the actual server
	if server, ok := serverInterface.(*http3.Server); ok {
		return server.Close()
	}

	return nil
}
