package router

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"
	"github.com/quic-go/webtransport-go"
	forge_http "github.com/xraph/go-utils/http"
	logger "github.com/xraph/go-utils/log"

	"github.com/xraph/forge/internal/shared"
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

		if !isWebTransportProtocol(req.Proto) {
			http.Error(w, "WebTransport protocol required", http.StatusBadRequest)

			return
		}

		// Upgrade to WebTransport
		session, err := r.upgradeToWebTransport(w, req)
		if err != nil {
			if r.logger != nil {
				r.logger.Error("failed to upgrade webtransport session",
					logger.String("error", err.Error()),
				)
			}

			http.Error(w, "Failed to upgrade connection", http.StatusInternalServerError)

			return
		}

		shared.RouteMatchFromContext(req.Context()).Establish(http.StatusOK)

		handle := r.openStream(shared.KindWebTransport, shared.RouteMatchFromContext(req.Context()).PatternOrEmpty())

		var handlerErr error
		defer closeStream(handle, &handlerErr)

		// Create WebTransport session wrapper
		sessionID := generateConnectionID()

		r.mu.RLock()
		maxDatagramSize := r.webTransportConfig.MaxDatagramFrameSize
		r.mu.RUnlock()

		wtSession := newWebTransportSession(sessionID, session, req.Context())
		wtSession.handle = handle
		wtSession.maxDatagramSize = maxDatagramSize

		defer wtSession.Close()

		// Create context. Cleanup releases the DI scope and any multipart temp
		// files; long-lived sessions are exactly where leaking them hurts most.
		ctx := newContext(w, req, r.container)
		defer ctx.(forge_http.ContextWithClean).Cleanup()

		// Call handler
		if err := handler(ctx, wtSession); err != nil {
			handlerErr = err

			if r.logger != nil {
				r.logger.Error("webtransport handler error",
					logger.String("error", err.Error()),
				)
			}
		}
	}

	// Add route type marker for AsyncAPI
	optsWithType := append([]RouteOption{WithRouteKind(KindWebTransport)}, opts...)

	// Register as CONNECT route
	return r.register(http.MethodConnect, path, httpHandler, optsWithType...)
}

// isWebTransportProtocol reports whether proto is an Extended CONNECT
// :protocol that opens a WebTransport session. Draft 15 renamed "webtransport"
// to "webtransport-h3". webtransport-go clients send the new name, and older
// clients and browsers still send the old one, so both are accepted.
func isWebTransportProtocol(proto string) bool {
	return proto == "webtransport-h3" || proto == "webtransport"
}

// upgradeToWebTransport upgrades an HTTP/3 request to a WebTransport session.
// It needs the server StartHTTP3 runs, because only that server knows the
// QUIC connection the request arrived on.
func (r *router) upgradeToWebTransport(w http.ResponseWriter, req *http.Request) (*webtransport.Session, error) {
	hw, err := newWebTransportWriter(w)
	if err != nil {
		return nil, err
	}

	r.mu.RLock()
	server := r.webTransportServer
	r.mu.RUnlock()

	if server == nil {
		return nil, errors.New("webtransport: HTTP/3 server not running, call StartHTTP3")
	}

	session, err := server.Upgrade(hw, req)
	if err != nil {
		return nil, fmt.Errorf("failed to upgrade: %w", err)
	}

	return session, nil
}

// h3ResponseWriter is what webtransport-go's Upgrade type-asserts its writer
// to, besides http.Flusher.
type h3ResponseWriter interface {
	http3.Settingser
	http3.HTTPStreamer
}

// webTransportWriter gives Upgrade the HTTP/3 writer's own methods while the
// status still goes through every wrapper above it.
//
// Upgrade never sees the raw HTTP/3 writer in forge. The router wraps it in a
// before-write writer, and the tracing and metrics middleware wrap that in
// status recorders. None of them has the HTTP/3 methods, and Upgrade used to
// panic on the type assertion. They all have Unwrap, so we walk down to the
// HTTP/3 writer and keep the outermost one for the 200. Writing the status at
// the top is what lets before-write hooks run and status recorders see it.
type webTransportWriter struct {
	http.ResponseWriter

	h3 h3ResponseWriter
}

func newWebTransportWriter(w http.ResponseWriter) (*webTransportWriter, error) {
	for inner := w; inner != nil; {
		if h3, ok := inner.(h3ResponseWriter); ok {
			return &webTransportWriter{ResponseWriter: w, h3: h3}, nil
		}

		u, ok := inner.(interface{ Unwrap() http.ResponseWriter })
		if !ok {
			break
		}

		inner = u.Unwrap()
	}

	return nil, errors.New("webtransport: request did not arrive over HTTP/3")
}

func (w *webTransportWriter) ReceivedSettings() <-chan struct{} { return w.h3.ReceivedSettings() }

func (w *webTransportWriter) Settings() *http3.Settings { return w.h3.Settings() }

func (w *webTransportWriter) HTTPStream() *http3.Stream { return w.h3.HTTPStream() }

// Flush sends the 200 down the whole chain. The client can't use the session
// until it has the response.
func (w *webTransportWriter) Flush() {
	_ = http.NewResponseController(w.ResponseWriter).Flush()
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
		)
	}

	return nil
}

// StartHTTP3 starts an HTTP/3 server for WebTransport support. It binds the
// UDP socket before it returns, so a bad or busy address is reported here,
// and serves in the background.
func (r *router) StartHTTP3(addr string, tlsConfig *tls.Config) error {
	r.mu.RLock()
	enabled, config, running := r.webTransportEnabled, r.webTransportConfig, r.webTransportServer != nil
	r.mu.RUnlock()

	if !enabled {
		return errors.New("webtransport not enabled")
	}

	if tlsConfig == nil {
		return errors.New("TLS config required for HTTP/3")
	}

	if running {
		return errors.New("HTTP/3 server already running")
	}

	// Every WebTransport session needs datagrams, so they're on whatever the
	// deprecated EnableDatagrams says, and webtransport-go would turn them on
	// anyway. Its clients also refuse a server without stream resets with
	// partial delivery.
	//
	// The stream limits are QUIC's, and a server allows one session per
	// connection, so they cap the session too. webtransport.Server.Config
	// would set per-session limits instead, but they only hold when the
	// client sends WebTransport flow control settings of its own, and
	// turning them on lets one connection carry several sessions.
	quicConfig := &quic.Config{
		MaxIdleTimeout:                   time.Duration(config.MaxIdleTimeout) * time.Millisecond,
		KeepAlivePeriod:                  time.Duration(config.KeepAliveInterval) * time.Millisecond,
		EnableDatagrams:                  true,
		MaxIncomingStreams:               quicStreamLimit(config.MaxBidiStreams, h3BidiStreams),
		MaxIncomingUniStreams:            quicStreamLimit(config.MaxUniStreams, h3UniStreams),
		MaxStreamReceiveWindow:           config.StreamReceiveWindow,
		MaxConnectionReceiveWindow:       config.ConnectionReceiveWindow,
		EnableStreamResetPartialDelivery: true,
	}

	// One webtransport.Server for the life of the HTTP/3 server. It accepts
	// the QUIC connections itself, so it knows the connection behind each
	// CONNECT when Upgrade runs. It also calls ConfigureHTTP3Server on h3,
	// which advertises WebTransport in the HTTP/3 SETTINGS and puts the QUIC
	// connection into every request's context. A server built per request has
	// neither, and every upgrade failed.
	server := &webtransport.Server{
		H3: &http3.Server{
			Addr:       addr,
			TLSConfig:  http3.ConfigureTLSConfig(tlsConfig),
			QUICConfig: quicConfig,
			Handler:    r,
		},
		CheckOrigin: checkWebTransportOrigin(config.AllowedOrigins),
	}

	conn, err := (&net.ListenConfig{}).ListenPacket(context.Background(), "udp", addr)
	if err != nil {
		return fmt.Errorf("HTTP/3 listen on %s: %w", addr, err)
	}

	r.mu.Lock()
	r.webTransportServer = server
	r.http3Conn = conn
	r.mu.Unlock()

	if r.logger != nil {
		r.logger.Info("starting HTTP/3 server for webtransport",
			logger.String("address", conn.LocalAddr().String()),
		)
	}

	go func() {
		err := server.Serve(conn)

		r.mu.RLock()
		stopped := r.webTransportServer != server
		r.mu.RUnlock()

		if err != nil && !stopped && r.logger != nil {
			r.logger.Error("HTTP/3 server error",
				logger.String("error", err.Error()),
			)
		}
	}()

	return nil
}

// The streams HTTP/3 takes from a client's QUIC stream limits before
// WebTransport gets any. The CONNECT request that carries the session is
// bidirectional. The control stream and the QPACK encoder and decoder streams
// are unidirectional (RFC 9114 section 6.2, RFC 9204 section 4.2).
const (
	h3BidiStreams = 1
	h3UniStreams  = 3
)

// quicStreamLimit turns a WebTransportConfig stream limit into the QUIC one,
// with room for the streams HTTP/3 needs. Zero means the default of 100 and a
// negative value allows no WebTransport streams, the way quic-go reads its
// own limits.
func quicStreamLimit(limit, reserved int64) int64 {
	const (
		defaultLimit = 100
		maxLimit     = 1 << 60
	)

	switch {
	case limit == 0:
		limit = defaultLimit
	case limit < 0:
		limit = 0
	}

	return min(limit, maxLimit-reserved) + reserved
}

// http3Addr returns the address the HTTP/3 server listens on, or nil when it
// isn't running.
func (r *router) http3Addr() net.Addr {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if r.http3Conn == nil {
		return nil
	}

	return r.http3Conn.LocalAddr()
}

// StopHTTP3 stops the HTTP/3 server. It closes every WebTransport session and
// QUIC connection, then the UDP socket.
func (r *router) StopHTTP3() error {
	r.mu.Lock()
	server, conn := r.webTransportServer, r.http3Conn
	r.webTransportServer, r.http3Conn = nil, nil
	r.mu.Unlock()

	if server == nil {
		return nil
	}

	if r.logger != nil {
		r.logger.Info("stopping HTTP/3 server")
	}

	err := server.Close()

	if cerr := conn.Close(); cerr != nil && !errors.Is(cerr, net.ErrClosed) {
		err = errors.Join(err, cerr)
	}

	return err
}

// checkWebTransportOrigin returns an origin checker function.
//
// An empty allowedOrigins list means same-origin only. It previously meant
// "allow everything", which left the default configuration open to cross-site
// hijacking — browsers send cookies with WebTransport upgrades but do not apply
// CORS to them. Pass "*" to opt back into allowing any origin.
//
// Delegates to the shared checker in origin.go so WebTransport and WebSocket
// enforce identical rules, including label-boundary wildcard matching.
func checkWebTransportOrigin(allowedOrigins []string) func(r *http.Request) bool {
	return func(r *http.Request) bool {
		return requestOriginAllowed(r, allowedOrigins)
	}
}
