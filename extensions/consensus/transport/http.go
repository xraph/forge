package transport

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/consensus/internal"
)

// HTTPTransport implements HTTP-based network transport.
type HTTPTransport struct {
	id      string
	address string
	port    int
	server  *http.Server
	client  *http.Client
	peers   map[string]*httpPeer
	peersMu sync.RWMutex
	inbox   chan internal.Message
	logger  forge.Logger

	// Lifecycle
	started bool
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	mu      sync.RWMutex
}

// httpPeer represents an HTTP peer.
type httpPeer struct {
	nodeID  string
	baseURL string
}

// HTTPTransportConfig contains HTTP transport configuration.
type HTTPTransportConfig struct {
	NodeID              string
	Address             string
	Port                int
	BufferSize          int
	RequestTimeout      time.Duration
	IdleConnTimeout     time.Duration
	MaxIdleConns        int
	MaxIdleConnsPerHost int
}

// NewHTTPTransport creates a new HTTP transport.
func NewHTTPTransport(config HTTPTransportConfig, logger forge.Logger) *HTTPTransport {
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

	// Create HTTP client with connection pooling
	client := &http.Client{
		Timeout: config.RequestTimeout,
		Transport: &http.Transport{
			MaxIdleConns:        config.MaxIdleConns,
			MaxIdleConnsPerHost: config.MaxIdleConnsPerHost,
			IdleConnTimeout:     config.IdleConnTimeout,
		},
	}

	return &HTTPTransport{
		id:      config.NodeID,
		address: config.Address,
		port:    config.Port,
		client:  client,
		peers:   make(map[string]*httpPeer),
		inbox:   make(chan internal.Message, config.BufferSize),
		logger:  logger,
	}
}

// Start starts the HTTP transport.
func (ht *HTTPTransport) Start(ctx context.Context) error {
	ht.mu.Lock()
	defer ht.mu.Unlock()

	if ht.started {
		return internal.ErrAlreadyStarted
	}

	ht.ctx, ht.cancel = context.WithCancel(ctx)

	// Create HTTP server
	mux := http.NewServeMux()
	mux.HandleFunc("/raft/message", ht.handleMessage)
	mux.HandleFunc("/health", ht.handleHealth)

	ht.server = &http.Server{
		Addr:    fmt.Sprintf("%s:%d", ht.address, ht.port),
		Handler: mux,
	}

	// Start server in goroutine

	ht.wg.Go(func() {

		if err := ht.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			ht.logger.Error("HTTP server error",
				forge.F("error", err),
			)
		}
	})

	ht.started = true

	ht.logger.Info("HTTP transport started",
		forge.F("node_id", ht.id),
		forge.F("address", ht.server.Addr),
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

	ht.mu.Unlock()

	if ht.cancel != nil {
		ht.cancel()
	}

	// Graceful shutdown of HTTP server
	if ht.server != nil {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := ht.server.Shutdown(shutdownCtx); err != nil {
			ht.logger.Error("failed to shutdown HTTP server",
				forge.F("error", err),
			)
		}
	}

	// Wait for goroutines
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

	// Marshal message to JSON
	data, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	// Create HTTP request
	url := peer.baseURL + "/raft/message"

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Node-Id", ht.id)

	// Send request
	resp, err := ht.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)

		return fmt.Errorf("HTTP error %d: %s", resp.StatusCode, string(body))
	}

	ht.logger.Debug("sent HTTP message",
		forge.F("target", target),
		forge.F("url", url),
	)

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

	ht.peers[nodeID] = &httpPeer{
		nodeID:  nodeID,
		baseURL: fmt.Sprintf("http://%s:%d", address, port),
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

	if _, exists := ht.peers[nodeID]; !exists {
		return internal.ErrNodeNotFound
	}

	delete(ht.peers, nodeID)

	ht.logger.Info("peer removed from HTTP transport",
		forge.F("node_id", ht.id),
		forge.F("peer_id", nodeID),
	)

	return nil
}

// GetAddress returns the local address.
func (ht *HTTPTransport) GetAddress() string {
	return fmt.Sprintf("%s:%d", ht.address, ht.port)
}

// handleMessage handles incoming Raft messages.
func (ht *HTTPTransport) handleMessage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)

		return
	}

	// Read body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)

		return
	}
	defer r.Body.Close()

	// Unmarshal message
	var msg internal.Message
	if err := json.Unmarshal(body, &msg); err != nil {
		http.Error(w, "failed to unmarshal message", http.StatusBadRequest)

		return
	}

	// Send to inbox
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
	json.NewEncoder(w).Encode(map[string]any{
		"status":  "healthy",
		"node_id": ht.id,
	})
}
