package transport

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/logger"
)

// HTTPTransport implements Transport using HTTP
type HTTPTransport struct {
	config      TransportConfig
	logger      common.Logger
	metrics     common.Metrics
	server      *http.Server
	client      *http.Client
	peers       map[string]*HTTPPeer
	messageChan chan IncomingMessage
	stopCh      chan struct{}
	stats       TransportStats
	startTime   time.Time
	mu          sync.RWMutex
	peersmu     sync.RWMutex
	started     bool
}

// HTTPPeer represents an HTTP peer
type HTTPPeer struct {
	Info   PeerInfo
	Client *http.Client
	mu     sync.RWMutex
}

// HTTPTransportFactory creates HTTP transport instances
type HTTPTransportFactory struct {
	logger  common.Logger
	metrics common.Metrics
}

// NewHTTPTransportFactory creates a new HTTP transport factory
func NewHTTPTransportFactory(logger common.Logger, metrics common.Metrics) *HTTPTransportFactory {
	return &HTTPTransportFactory{
		logger:  logger,
		metrics: metrics,
	}
}

// Create creates a new HTTP transport
func (f *HTTPTransportFactory) Create(config TransportConfig) (Transport, error) {
	if config.Type != TransportTypeHTTP {
		return nil, fmt.Errorf("invalid transport type: %s", config.Type)
	}

	// Set defaults
	if config.Port == 0 {
		config.Port = 8080
	}
	if config.Timeout == 0 {
		config.Timeout = 5 * time.Second
	}
	if config.MaxMessageSize == 0 {
		config.MaxMessageSize = 1024 * 1024 // 1MB
	}
	if config.BufferSize == 0 {
		config.BufferSize = 1000
	}
	if config.MaxConnections == 0 {
		config.MaxConnections = 100
	}

	// Create HTTP client
	client := &http.Client{
		Timeout: config.Timeout,
		Transport: &http.Transport{
			MaxIdleConns:        config.MaxConnections,
			MaxIdleConnsPerHost: 10,
			IdleConnTimeout:     config.KeepAliveTime,
			DisableKeepAlives:   !config.KeepAlive,
		},
	}

	// Add TLS if enabled
	if config.EnableTLS && config.TLSConfig != nil {
		tlsConfig := &tls.Config{
			ServerName:         config.TLSConfig.ServerName,
			InsecureSkipVerify: config.TLSConfig.SkipVerify,
		}

		if config.TLSConfig.CertFile != "" && config.TLSConfig.KeyFile != "" {
			cert, err := tls.LoadX509KeyPair(config.TLSConfig.CertFile, config.TLSConfig.KeyFile)
			if err != nil {
				return nil, fmt.Errorf("failed to load TLS certificate: %w", err)
			}
			tlsConfig.Certificates = []tls.Certificate{cert}
		}

		client.Transport.(*http.Transport).TLSClientConfig = tlsConfig
	}

	transport := &HTTPTransport{
		config:      config,
		logger:      f.logger,
		metrics:     f.metrics,
		client:      client,
		peers:       make(map[string]*HTTPPeer),
		messageChan: make(chan IncomingMessage, config.BufferSize),
		stopCh:      make(chan struct{}),
		startTime:   time.Now(),
		stats:       TransportStats{},
	}

	return transport, nil
}

// Name returns the factory name
func (f *HTTPTransportFactory) Name() string {
	return TransportTypeHTTP
}

// Version returns the factory version
func (f *HTTPTransportFactory) Version() string {
	return "1.0.0"
}

// ValidateConfig validates the configuration
func (f *HTTPTransportFactory) ValidateConfig(config TransportConfig) error {
	if config.Type != TransportTypeHTTP {
		return fmt.Errorf("invalid transport type: %s", config.Type)
	}

	if config.Address == "" {
		return fmt.Errorf("address is required")
	}

	if config.Port <= 0 || config.Port > 65535 {
		return fmt.Errorf("invalid port: %d", config.Port)
	}

	if config.EnableTLS && config.TLSConfig == nil {
		return fmt.Errorf("TLS config is required when TLS is enabled")
	}

	return nil
}

// Start starts the HTTP transport
func (ht *HTTPTransport) Start(ctx context.Context) error {
	ht.mu.Lock()
	defer ht.mu.Unlock()

	if ht.started {
		return fmt.Errorf("transport already started")
	}

	// Create HTTP server
	mux := http.NewServeMux()
	mux.HandleFunc("/consensus/message", ht.handleMessage)
	mux.HandleFunc("/consensus/health", ht.handleHealth)

	address := fmt.Sprintf("%s:%d", ht.config.Address, ht.config.Port)
	ht.server = &http.Server{
		Addr:         address,
		Handler:      mux,
		ReadTimeout:  ht.config.Timeout,
		WriteTimeout: ht.config.Timeout,
		IdleTimeout:  ht.config.KeepAliveTime,
	}

	// OnStart server
	go func() {
		var err error
		if ht.config.EnableTLS && ht.config.TLSConfig != nil {
			err = ht.server.ListenAndServeTLS(ht.config.TLSConfig.CertFile, ht.config.TLSConfig.KeyFile)
		} else {
			err = ht.server.ListenAndServe()
		}

		if err != nil && err != http.ErrServerClosed {
			if ht.logger != nil {
				ht.logger.Error("HTTP server error", logger.Error(err))
			}
		}
	}()

	ht.started = true

	if ht.logger != nil {
		ht.logger.Info("HTTP transport started",
			logger.String("address", address),
			logger.Bool("tls_enabled", ht.config.EnableTLS),
		)
	}

	if ht.metrics != nil {
		ht.metrics.Counter("forge.consensus.transport.http.started").Inc()
	}

	return nil
}

// Stop stops the HTTP transport
func (ht *HTTPTransport) Stop(ctx context.Context) error {
	ht.mu.Lock()
	defer ht.mu.Unlock()

	if !ht.started {
		return nil
	}

	// Signal stop
	close(ht.stopCh)

	// Shutdown server
	if ht.server != nil {
		shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		ht.server.Shutdown(shutdownCtx)
	}

	// Close message channel
	close(ht.messageChan)

	ht.started = false

	if ht.logger != nil {
		ht.logger.Info("HTTP transport stopped")
	}

	if ht.metrics != nil {
		ht.metrics.Counter("forge.consensus.transport.http.stopped").Inc()
	}

	return nil
}

// Send sends a message to a peer
func (ht *HTTPTransport) Send(ctx context.Context, target string, message Message) error {
	ht.peersmu.RLock()
	peer, exists := ht.peers[target]
	ht.peersmu.RUnlock()

	if !exists {
		return NewTransportError(ErrCodePeerNotFound, fmt.Sprintf("peer not found: %s", target))
	}

	// Serialize message
	data, err := json.Marshal(message)
	if err != nil {
		return NewTransportError(ErrCodeSerializationError, "failed to serialize message").WithCause(err)
	}

	// Check message size
	if len(data) > ht.config.MaxMessageSize {
		return NewTransportError(ErrCodeMessageTooLarge, fmt.Sprintf("message too large: %d bytes", len(data)))
	}

	// Create HTTP request
	url := fmt.Sprintf("http://%s/consensus/message", peer.Info.Address)
	if ht.config.EnableTLS {
		url = fmt.Sprintf("https://%s/consensus/message", peer.Info.Address)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(data))
	if err != nil {
		return NewTransportError(ErrCodeNetworkError, "failed to create request").WithCause(err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Consensus-From", message.From)
	req.Header.Set("X-Consensus-To", message.To)
	req.Header.Set("X-Consensus-Type", string(message.Type))
	req.Header.Set("X-Message-ID", message.ID)

	// Send request
	peer.mu.RLock()
	client := peer.Client
	peer.mu.RUnlock()

	start := time.Now()
	resp, err := client.Do(req)
	latency := time.Since(start)

	// Update statistics
	ht.mu.Lock()
	ht.stats.MessagesSent++
	ht.stats.BytesSent += int64(len(data))
	if err != nil {
		ht.stats.ErrorCount++
	}
	ht.mu.Unlock()

	// Update peer status
	peer.mu.Lock()
	if err != nil {
		peer.Info.Status = PeerStatusError
	} else {
		peer.Info.Status = PeerStatusConnected
		peer.Info.LastSeen = time.Now()
	}
	peer.mu.Unlock()

	if err != nil {
		return NewTransportError(ErrCodeNetworkError, fmt.Sprintf("failed to send message to %s", target)).WithCause(err)
	}
	defer resp.Body.Close()

	// Check response
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return NewTransportError(ErrCodeNetworkError, fmt.Sprintf("HTTP error %d: %s", resp.StatusCode, string(body)))
	}

	if ht.metrics != nil {
		ht.metrics.Counter("forge.consensus.transport.http.messages_sent").Inc()
		ht.metrics.Histogram("forge.consensus.transport.http.send_latency").Observe(latency.Seconds())
	}

	return nil
}

// Receive receives messages from peers
func (ht *HTTPTransport) Receive(ctx context.Context) (<-chan IncomingMessage, error) {
	return ht.messageChan, nil
}

// AddPeer adds a peer to the transport
func (ht *HTTPTransport) AddPeer(peerID, address string) error {
	ht.peersmu.Lock()
	defer ht.peersmu.Unlock()

	if _, exists := ht.peers[peerID]; exists {
		return fmt.Errorf("peer already exists: %s", peerID)
	}

	// Create client for peer
	client := &http.Client{
		Timeout: ht.config.Timeout,
		Transport: &http.Transport{
			MaxIdleConns:        10,
			MaxIdleConnsPerHost: 5,
			IdleConnTimeout:     ht.config.KeepAliveTime,
			DisableKeepAlives:   !ht.config.KeepAlive,
		},
	}

	// Add TLS if enabled
	if ht.config.EnableTLS && ht.config.TLSConfig != nil {
		tlsConfig := &tls.Config{
			ServerName:         ht.config.TLSConfig.ServerName,
			InsecureSkipVerify: ht.config.TLSConfig.SkipVerify,
		}
		client.Transport.(*http.Transport).TLSClientConfig = tlsConfig
	}

	peer := &HTTPPeer{
		Info: PeerInfo{
			ID:       peerID,
			Address:  address,
			Status:   PeerStatusConnecting,
			LastSeen: time.Now(),
			Metadata: make(map[string]interface{}),
		},
		Client: client,
	}

	ht.peers[peerID] = peer

	// Test connection
	go ht.testPeerConnection(peer)

	if ht.logger != nil {
		ht.logger.Info("HTTP peer added",
			logger.String("peer_id", peerID),
			logger.String("address", address),
		)
	}

	if ht.metrics != nil {
		ht.metrics.Counter("forge.consensus.transport.http.peers_added").Inc()
		ht.metrics.Gauge("forge.consensus.transport.http.connected_peers").Set(float64(len(ht.peers)))
	}

	return nil
}

// RemovePeer removes a peer from the transport
func (ht *HTTPTransport) RemovePeer(peerID string) error {
	ht.peersmu.Lock()
	defer ht.peersmu.Unlock()

	_, exists := ht.peers[peerID]
	if !exists {
		return fmt.Errorf("peer not found: %s", peerID)
	}

	delete(ht.peers, peerID)

	if ht.logger != nil {
		ht.logger.Info("HTTP peer removed",
			logger.String("peer_id", peerID),
		)
	}

	if ht.metrics != nil {
		ht.metrics.Counter("forge.consensus.transport.http.peers_removed").Inc()
		ht.metrics.Gauge("forge.consensus.transport.http.connected_peers").Set(float64(len(ht.peers)))
	}

	return nil
}

// GetPeers returns all peers
func (ht *HTTPTransport) GetPeers() []PeerInfo {
	ht.peersmu.RLock()
	defer ht.peersmu.RUnlock()

	peers := make([]PeerInfo, 0, len(ht.peers))
	for _, peer := range ht.peers {
		peer.mu.RLock()
		peers = append(peers, peer.Info)
		peer.mu.RUnlock()
	}

	return peers
}

// LocalAddress returns the local address
func (ht *HTTPTransport) LocalAddress() string {
	return fmt.Sprintf("%s:%d", ht.config.Address, ht.config.Port)
}

// HealthCheck performs a health check
func (ht *HTTPTransport) HealthCheck(ctx context.Context) error {
	ht.mu.RLock()
	defer ht.mu.RUnlock()

	if !ht.started {
		return fmt.Errorf("transport not started")
	}

	// Check server
	if ht.server == nil {
		return fmt.Errorf("HTTP server not initialized")
	}

	// Check error rate
	if ht.stats.MessagesSent > 0 {
		errorRate := float64(ht.stats.ErrorCount) / float64(ht.stats.MessagesSent)
		if errorRate > 0.1 { // 10% error rate threshold
			return fmt.Errorf("high error rate: %.2f%%", errorRate*100)
		}
	}

	return nil
}

// GetStats returns transport statistics
func (ht *HTTPTransport) GetStats() TransportStats {
	ht.mu.RLock()
	defer ht.mu.RUnlock()

	stats := ht.stats
	stats.Uptime = time.Since(ht.startTime)
	stats.ConnectedPeers = len(ht.peers)

	return stats
}

func (ht *HTTPTransport) GetAddress(nodeID string) (string, error) {
	ht.mu.RLock()
	defer ht.mu.RUnlock()

	if _, exists := ht.peers[nodeID]; !exists {
		return "", fmt.Errorf("peer not found: %s", nodeID)
	}

	peer := ht.peers[nodeID]
	peer.mu.RLock()
	defer peer.mu.RUnlock()

	return peer.Info.Address, nil
}

// Close closes the transport
func (ht *HTTPTransport) Close() error {
	return ht.Stop(context.Background())
}

// HTTP handlers

// handleMessage handles incoming messages
func (ht *HTTPTransport) handleMessage(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Read message data
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return
	}

	// Check message size
	if len(body) > ht.config.MaxMessageSize {
		http.Error(w, "Message too large", http.StatusRequestEntityTooLarge)
		return
	}

	// Deserialize message
	var message Message
	if err := json.Unmarshal(body, &message); err != nil {
		http.Error(w, "Failed to parse message", http.StatusBadRequest)
		return
	}

	// Validate message
	if err := ValidateMessage(message); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Extract peer info
	peerInfo := PeerInfo{
		ID:      message.From,
		Address: r.RemoteAddr,
		Status:  PeerStatusConnected,
	}

	// Send to message channel
	select {
	case ht.messageChan <- IncomingMessage{
		Message: message,
		Peer:    peerInfo,
	}:
		// Update statistics
		ht.mu.Lock()
		ht.stats.MessagesReceived++
		ht.stats.BytesReceived += int64(len(body))
		ht.mu.Unlock()

		if ht.metrics != nil {
			ht.metrics.Counter("forge.consensus.transport.http.messages_received").Inc()
		}

		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	default:
		http.Error(w, "Message buffer full", http.StatusServiceUnavailable)
	}
}

// handleHealth handles health check requests
func (ht *HTTPTransport) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	if err := ht.HealthCheck(ctx); err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	response := map[string]interface{}{
		"status": "healthy",
		"stats":  ht.GetStats(),
	}

	json.NewEncoder(w).Encode(response)
}

// testPeerConnection tests a peer connection
func (ht *HTTPTransport) testPeerConnection(peer *HTTPPeer) {
	url := fmt.Sprintf("http://%s/consensus/health", peer.Info.Address)
	if ht.config.EnableTLS {
		url = fmt.Sprintf("https://%s/consensus/health", peer.Info.Address)
	}

	ctx, cancel := context.WithTimeout(context.Background(), ht.config.Timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		peer.mu.Lock()
		peer.Info.Status = PeerStatusError
		peer.mu.Unlock()
		return
	}

	peer.mu.RLock()
	client := peer.Client
	peer.mu.RUnlock()

	resp, err := client.Do(req)
	if err != nil {
		peer.mu.Lock()
		peer.Info.Status = PeerStatusError
		peer.mu.Unlock()
		return
	}
	defer resp.Body.Close()

	peer.mu.Lock()
	if resp.StatusCode == http.StatusOK {
		peer.Info.Status = PeerStatusConnected
		peer.Info.LastSeen = time.Now()
	} else {
		peer.Info.Status = PeerStatusError
	}
	peer.mu.Unlock()
}
