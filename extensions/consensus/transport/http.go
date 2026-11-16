package transport

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/errors"
	"github.com/xraph/forge/extensions/consensus/internal"
)

// HTTPTransport implements HTTP-based network transport.
//
// Each message is POSTed to /raft/message as the JSON wire envelope. The
// receiver only queues the message and answers 200; Raft responses travel back
// as separate messages.
type HTTPTransport struct {
	id       string
	address  string
	port     int
	config   HTTPTransportConfig
	server   *http.Server
	listener net.Listener
	peers    map[string]*httpPeer
	peersMu  sync.RWMutex
	inbox    chan internal.Message
	logger   forge.Logger

	// Lifecycle
	started bool
	ctx     context.Context //nolint:containedctx // lifecycle context: created in Start, cancelled in Stop
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	mu      sync.RWMutex
}

// httpPeer represents an HTTP peer.
type httpPeer struct {
	nodeID  string
	baseURL string
	client  *http.Client
}

// HTTPTransportConfig contains HTTP transport configuration.
type HTTPTransportConfig struct {
	NodeID string
	// Address to listen on. Empty means 127.0.0.1; set it explicitly (for
	// example to 0.0.0.0) to accept peers from other hosts.
	Address             string
	Port                int
	BufferSize          int
	RequestTimeout      time.Duration
	IdleConnTimeout     time.Duration
	MaxIdleConns        int
	MaxIdleConnsPerHost int

	// MaxMessageSize is the largest request body accepted or sent, in bytes.
	// Defaults to DefaultMaxMessageSize.
	MaxMessageSize int
	// MaxConnections caps concurrent inbound connections. Defaults to 128.
	MaxConnections int

	// TLSConfig switches the transport to HTTPS for both the server and the
	// client. It must hold this node's certificate and the CA that signs its
	// peers; TLSManager.GetTLSConfig builds one.
	TLSConfig *tls.Config
	// RequirePeerIdentity rejects any message whose From is not named by the
	// caller's client certificate, and verifies that each peer's server
	// certificate names that peer. It requires TLSConfig with ClientAuth set
	// to tls.RequireAndVerifyClientCert, and every node certificate must carry
	// its node ID as a DNS subject alternative name.
	RequirePeerIdentity bool
}

// NewHTTPTransport creates a new HTTP transport.
func NewHTTPTransport(config HTTPTransportConfig, logger forge.Logger) *HTTPTransport {
	if config.Address == "" {
		config.Address = DefaultBindAddress
	}

	if config.BufferSize == 0 {
		config.BufferSize = 100
	}

	if config.RequestTimeout == 0 {
		config.RequestTimeout = 30 * time.Second
	}

	if config.IdleConnTimeout == 0 {
		config.IdleConnTimeout = 90 * time.Second
	}

	if config.MaxIdleConns == 0 {
		config.MaxIdleConns = 100
	}

	if config.MaxIdleConnsPerHost == 0 {
		config.MaxIdleConnsPerHost = 10
	}

	if config.MaxMessageSize <= 0 {
		config.MaxMessageSize = DefaultMaxMessageSize
	}

	if config.MaxConnections <= 0 {
		config.MaxConnections = 128
	}

	return &HTTPTransport{
		id:      config.NodeID,
		address: config.Address,
		port:    config.Port,
		config:  config,
		peers:   make(map[string]*httpPeer),
		inbox:   make(chan internal.Message, config.BufferSize),
		logger:  logger,
	}
}

// newClient builds the HTTP client used to reach one peer.
func (ht *HTTPTransport) newClient(peerID string) *http.Client {
	return &http.Client{
		Timeout: ht.config.RequestTimeout,
		Transport: &http.Transport{
			MaxIdleConns:        ht.config.MaxIdleConns,
			MaxIdleConnsPerHost: ht.config.MaxIdleConnsPerHost,
			IdleConnTimeout:     ht.config.IdleConnTimeout,
			TLSClientConfig:     clientTLSFor(ht.config.TLSConfig, peerID, ht.config.RequirePeerIdentity),
		},
	}
}

// Start starts the HTTP transport. The listener is opened before Start
// returns, so a port that cannot be bound is reported to the caller.
func (ht *HTTPTransport) Start(ctx context.Context) error {
	ht.mu.Lock()
	defer ht.mu.Unlock()

	if ht.started {
		return internal.ErrAlreadyStarted
	}

	if err := checkIdentityTLS(ht.config.TLSConfig, ht.config.RequirePeerIdentity); err != nil {
		return fmt.Errorf("http transport: %w", err)
	}

	var listenConfig net.ListenConfig

	listener, err := listenConfig.Listen(ctx, "tcp", net.JoinHostPort(ht.address, strconv.Itoa(ht.port)))
	if err != nil {
		return fmt.Errorf("failed to start HTTP listener: %w", err)
	}

	if addr, ok := listener.Addr().(*net.TCPAddr); ok && ht.port == 0 {
		ht.port = addr.Port
	}

	listener = newLimitListener(listener, ht.config.MaxConnections)

	if ht.config.TLSConfig != nil {
		listener = tls.NewListener(listener, ht.config.TLSConfig.Clone())
	}

	ht.ctx, ht.cancel = context.WithCancel(context.WithoutCancel(ctx))
	ht.listener = listener

	mux := http.NewServeMux()
	mux.HandleFunc("/raft/message", ht.handleMessage)
	mux.HandleFunc("/health", ht.handleHealth)

	// Peers give up on a request after the client timeout, so a request that
	// takes longer than that to arrive comes from a caller that has gone.
	ht.server = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: ht.config.RequestTimeout,
		ReadTimeout:       ht.config.RequestTimeout,
		WriteTimeout:      ht.config.RequestTimeout,
		IdleTimeout:       ht.config.IdleConnTimeout,
		MaxHeaderBytes:    16 << 10,
	}

	server := ht.server

	ht.wg.Go(func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			ht.logger.Error("HTTP server error",
				forge.F("error", err),
			)
		}
	})

	ht.started = true

	ht.logger.Info("HTTP transport started",
		forge.F("node_id", ht.id),
		forge.F("address", listener.Addr().String()),
		forge.F("tls", ht.config.TLSConfig != nil),
		forge.F("peer_identity", ht.config.RequirePeerIdentity),
	)

	return nil
}

// Stop stops the HTTP transport.
func (ht *HTTPTransport) Stop(ctx context.Context) error {
	ht.mu.Lock()

	if !ht.started {
		ht.mu.Unlock()

		return internal.ErrNotStarted
	}

	ht.started = false
	ht.mu.Unlock()

	if ht.cancel != nil {
		ht.cancel()
	}

	if ht.server != nil {
		shutdownCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()

		if err := ht.server.Shutdown(shutdownCtx); err != nil {
			ht.logger.Warn("HTTP server did not shut down cleanly, closing it",
				forge.F("error", err),
			)

			_ = ht.server.Close()
		}
	}

	ht.peersMu.RLock()

	for _, peer := range ht.peers {
		peer.client.CloseIdleConnections()
	}

	ht.peersMu.RUnlock()

	done := make(chan struct{})

	go func() {
		ht.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		ht.logger.Info("HTTP transport stopped")
	case <-ctx.Done():
		ht.logger.Warn("HTTP transport stop timed out")
	}

	return nil
}

// Send sends a message to a peer.
func (ht *HTTPTransport) Send(ctx context.Context, target string, message any) error {
	ht.peersMu.RLock()
	peer, exists := ht.peers[target]
	ht.peersMu.RUnlock()

	if !exists {
		return fmt.Errorf("peer %s not found", target)
	}

	data, _, err := encodeMessage(message)
	if err != nil {
		return err
	}

	if len(data) > ht.config.MaxMessageSize {
		return fmt.Errorf("%w: %d bytes, limit %d", ErrMessageTooLarge, len(data), ht.config.MaxMessageSize)
	}

	url := peer.baseURL + "/raft/message"

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := peer.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))

		return fmt.Errorf("HTTP error %d: %s", resp.StatusCode, string(body))
	}

	// Drain so the connection can be reused.
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1024))

	return nil
}

// Receive returns a channel for receiving messages.
func (ht *HTTPTransport) Receive() <-chan internal.Message {
	return ht.inbox
}

// AddPeer adds a peer to the transport.
func (ht *HTTPTransport) AddPeer(nodeID, address string, port int) error {
	ht.peersMu.Lock()
	defer ht.peersMu.Unlock()

	if _, exists := ht.peers[nodeID]; exists {
		return internal.ErrPeerExists
	}

	scheme := "http"
	if ht.config.TLSConfig != nil {
		scheme = "https"
	}

	ht.peers[nodeID] = &httpPeer{
		nodeID:  nodeID,
		baseURL: scheme + "://" + net.JoinHostPort(address, strconv.Itoa(port)),
		client:  ht.newClient(nodeID),
	}

	ht.logger.Info("peer added to HTTP transport",
		forge.F("node_id", ht.id),
		forge.F("peer_id", nodeID),
		forge.F("base_url", ht.peers[nodeID].baseURL),
	)

	return nil
}

// RemovePeer removes a peer from the transport.
func (ht *HTTPTransport) RemovePeer(nodeID string) error {
	ht.peersMu.Lock()
	defer ht.peersMu.Unlock()

	peer, exists := ht.peers[nodeID]
	if !exists {
		return internal.ErrNodeNotFound
	}

	peer.client.CloseIdleConnections()
	delete(ht.peers, nodeID)

	ht.logger.Info("peer removed from HTTP transport",
		forge.F("node_id", ht.id),
		forge.F("peer_id", nodeID),
	)

	return nil
}

// GetAddress returns the local address.
func (ht *HTTPTransport) GetAddress() string {
	return net.JoinHostPort(ht.address, strconv.Itoa(ht.port))
}

// handleMessage handles incoming Raft messages.
func (ht *HTTPTransport) handleMessage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)

		return
	}

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, int64(ht.config.MaxMessageSize)))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			http.Error(w, "message too large", http.StatusRequestEntityTooLarge)

			return
		}

		http.Error(w, "failed to read body", http.StatusBadRequest)

		return
	}

	msg, err := decodeMessage(body)
	if err != nil {
		http.Error(w, "invalid message", http.StatusBadRequest)

		return
	}

	if ht.config.RequirePeerIdentity {
		if err := verifySender(r.TLS, msg.From); err != nil {
			ht.logger.Warn("rejecting message with unverified sender",
				forge.F("remote", r.RemoteAddr),
				forge.F("error", err),
			)
			http.Error(w, "sender does not match client certificate", http.StatusForbidden)

			return
		}
	}

	select {
	case ht.inbox <- msg:
		ht.logger.Debug("received HTTP message",
			forge.F("from", msg.From),
			forge.F("type", msg.Type),
		)
		w.WriteHeader(http.StatusOK)
	case <-ht.ctx.Done():
		http.Error(w, "server shutting down", http.StatusServiceUnavailable)
	default:
		http.Error(w, "inbox full", http.StatusTooManyRequests)
	}
}

// handleHealth handles health check requests.
func (ht *HTTPTransport) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	// The status line is already sent, so a failed body write can only be logged.
	if err := json.NewEncoder(w).Encode(map[string]any{
		"status":  "healthy",
		"node_id": ht.id,
	}); err != nil {
		ht.logger.Debug("failed to write health response", forge.F("error", err))
	}
}

// limitListener caps the number of open accepted connections. Accept blocks
// while the cap is reached, so excess clients wait in the kernel backlog
// instead of each costing a goroutine and buffers.
type limitListener struct {
	net.Listener

	sem       chan struct{}
	closeOnce sync.Once
	done      chan struct{}
}

func newLimitListener(l net.Listener, n int) *limitListener {
	return &limitListener{Listener: l, sem: make(chan struct{}, n), done: make(chan struct{})}
}

func (l *limitListener) Accept() (net.Conn, error) {
	select {
	case l.sem <- struct{}{}:
	case <-l.done:
		return nil, net.ErrClosed
	}

	conn, err := l.Listener.Accept()
	if err != nil {
		<-l.sem

		return nil, err
	}

	return &limitConn{Conn: conn, release: func() { <-l.sem }}, nil
}

func (l *limitListener) Close() error {
	l.closeOnce.Do(func() { close(l.done) })

	return l.Listener.Close()
}

type limitConn struct {
	net.Conn

	once    sync.Once
	release func()
}

func (c *limitConn) Close() error {
	err := c.Conn.Close()
	c.once.Do(c.release)

	return err
}
