package transport

import (
	"context"
	"crypto/tls"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/logger"
)

// TCPTransport implements Transport using TCP
type TCPTransport struct {
	config      TransportConfig
	logger      common.Logger
	metrics     common.Metrics
	listener    net.Listener
	peers       map[string]*TCPPeer
	connections map[string]*TCPConnection
	messageChan chan IncomingMessage
	stopCh      chan struct{}
	stats       TransportStats
	startTime   time.Time
	mu          sync.RWMutex
	peersmu     sync.RWMutex
	connmu      sync.RWMutex
	started     bool
}

// TCPPeer represents a TCP peer
type TCPPeer struct {
	Info PeerInfo
	mu   sync.RWMutex
}

// TCPConnection represents a TCP connection
type TCPConnection struct {
	conn       net.Conn
	peerID     string
	remoteAddr string
	lastUsed   time.Time
	mu         sync.RWMutex
}

// TCPTransportFactory creates TCP transport instances
type TCPTransportFactory struct {
	logger  common.Logger
	metrics common.Metrics
}

// NewTCPTransportFactory creates a new TCP transport factory
func NewTCPTransportFactory(logger common.Logger, metrics common.Metrics) *TCPTransportFactory {
	return &TCPTransportFactory{
		logger:  logger,
		metrics: metrics,
	}
}

// Create creates a new TCP transport
func (f *TCPTransportFactory) Create(config TransportConfig) (Transport, error) {
	if config.Type != TransportTypeTCP {
		return nil, fmt.Errorf("invalid transport type: %s", config.Type)
	}

	// Set defaults
	if config.Port == 0 {
		config.Port = 8081
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
	if config.KeepAliveTime == 0 {
		config.KeepAliveTime = 30 * time.Second
	}

	transport := &TCPTransport{
		config:      config,
		logger:      f.logger,
		metrics:     f.metrics,
		peers:       make(map[string]*TCPPeer),
		connections: make(map[string]*TCPConnection),
		messageChan: make(chan IncomingMessage, config.BufferSize),
		stopCh:      make(chan struct{}),
		startTime:   time.Now(),
		stats:       TransportStats{},
	}

	return transport, nil
}

// Name returns the factory name
func (f *TCPTransportFactory) Name() string {
	return TransportTypeTCP
}

// Version returns the factory version
func (f *TCPTransportFactory) Version() string {
	return "1.0.0"
}

// ValidateConfig validates the configuration
func (f *TCPTransportFactory) ValidateConfig(config TransportConfig) error {
	if config.Type != TransportTypeTCP {
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

// Start starts the TCP transport
func (tt *TCPTransport) Start(ctx context.Context) error {
	tt.mu.Lock()
	defer tt.mu.Unlock()

	if tt.started {
		return fmt.Errorf("transport already started")
	}

	// Create listener
	address := fmt.Sprintf("%s:%d", tt.config.Address, tt.config.Port)
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", address, err)
	}

	// Add TLS if enabled
	if tt.config.EnableTLS && tt.config.TLSConfig != nil {
		cert, err := tls.LoadX509KeyPair(tt.config.TLSConfig.CertFile, tt.config.TLSConfig.KeyFile)
		if err != nil {
			return fmt.Errorf("failed to load TLS certificate: %w", err)
		}

		tlsConfig := &tls.Config{
			Certificates: []tls.Certificate{cert},
			ServerName:   tt.config.TLSConfig.ServerName,
		}

		listener = tls.NewListener(listener, tlsConfig)
	}

	tt.listener = listener

	// OnStart accepting connections
	go tt.acceptConnections()

	// OnStart connection cleanup routine
	go tt.cleanupConnections()

	tt.started = true

	if tt.logger != nil {
		tt.logger.Info("TCP transport started",
			logger.String("address", address),
			logger.Bool("tls_enabled", tt.config.EnableTLS),
		)
	}

	if tt.metrics != nil {
		tt.metrics.Counter("forge.consensus.transport.tcp.started").Inc()
	}

	return nil
}

// Stop stops the TCP transport
func (tt *TCPTransport) Stop(ctx context.Context) error {
	tt.mu.Lock()
	defer tt.mu.Unlock()

	if !tt.started {
		return nil
	}

	// Signal stop
	close(tt.stopCh)

	// Close listener
	if tt.listener != nil {
		tt.listener.Close()
	}

	// Close all connections
	tt.connmu.Lock()
	for _, conn := range tt.connections {
		conn.mu.Lock()
		if conn.conn != nil {
			conn.conn.Close()
		}
		conn.mu.Unlock()
	}
	tt.connections = make(map[string]*TCPConnection)
	tt.connmu.Unlock()

	// Close message channel
	close(tt.messageChan)

	tt.started = false

	if tt.logger != nil {
		tt.logger.Info("TCP transport stopped")
	}

	if tt.metrics != nil {
		tt.metrics.Counter("forge.consensus.transport.tcp.stopped").Inc()
	}

	return nil
}

// Send sends a message to a peer
func (tt *TCPTransport) Send(ctx context.Context, target string, message Message) error {
	tt.peersmu.RLock()
	peer, exists := tt.peers[target]
	tt.peersmu.RUnlock()

	if !exists {
		return NewTransportError(ErrCodePeerNotFound, fmt.Sprintf("peer not found: %s", target))
	}

	// Get or create connection
	conn, err := tt.getConnection(target, peer.Info.Address)
	if err != nil {
		return err
	}

	// Serialize message
	data, err := json.Marshal(message)
	if err != nil {
		return NewTransportError(ErrCodeSerializationError, "failed to serialize message").WithCause(err)
	}

	// Check message size
	if len(data) > tt.config.MaxMessageSize {
		return NewTransportError(ErrCodeMessageTooLarge, fmt.Sprintf("message too large: %d bytes", len(data)))
	}

	// Send message with length prefix
	start := time.Now()
	err = tt.sendMessage(conn, data)
	latency := time.Since(start)

	// Update statistics
	tt.mu.Lock()
	tt.stats.MessagesSent++
	tt.stats.BytesSent += int64(len(data))
	if err != nil {
		tt.stats.ErrorCount++
	}
	tt.mu.Unlock()

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
		// Remove failed connection
		tt.removeConnection(target)
		return NewTransportError(ErrCodeNetworkError, fmt.Sprintf("failed to send message to %s", target)).WithCause(err)
	}

	if tt.metrics != nil {
		tt.metrics.Counter("forge.consensus.transport.tcp.messages_sent").Inc()
		tt.metrics.Histogram("forge.consensus.transport.tcp.send_latency").Observe(latency.Seconds())
	}

	return nil
}

func (tt *TCPTransport) GetAddress(nodeID string) (string, error) {
	tt.peersmu.Lock()
	defer tt.peersmu.Unlock()

	add := tt.connections[nodeID].remoteAddr
	if len(add) > 0 {
		return add, nil
	}

	add = tt.peers[nodeID].Info.Address
	if len(add) > 0 {
		return add, nil
	}

	return "", errors.New("no address found")
}

// Receive receives messages from peers
func (tt *TCPTransport) Receive(ctx context.Context) (<-chan IncomingMessage, error) {
	return tt.messageChan, nil
}

// AddPeer adds a peer to the transport
func (tt *TCPTransport) AddPeer(peerID, address string) error {
	tt.peersmu.Lock()
	defer tt.peersmu.Unlock()

	if _, exists := tt.peers[peerID]; exists {
		return fmt.Errorf("peer already exists: %s", peerID)
	}

	peer := &TCPPeer{
		Info: PeerInfo{
			ID:       peerID,
			Address:  address,
			Status:   PeerStatusConnecting,
			LastSeen: time.Now(),
			Metadata: make(map[string]interface{}),
		},
	}

	tt.peers[peerID] = peer

	// Test connection
	go tt.testPeerConnection(peer)

	if tt.logger != nil {
		tt.logger.Info("TCP peer added",
			logger.String("peer_id", peerID),
			logger.String("address", address),
		)
	}

	if tt.metrics != nil {
		tt.metrics.Counter("forge.consensus.transport.tcp.peers_added").Inc()
		tt.metrics.Gauge("forge.consensus.transport.tcp.connected_peers").Set(float64(len(tt.peers)))
	}

	return nil
}

// RemovePeer removes a peer from the transport
func (tt *TCPTransport) RemovePeer(peerID string) error {
	tt.peersmu.Lock()
	defer tt.peersmu.Unlock()

	_, exists := tt.peers[peerID]
	if !exists {
		return fmt.Errorf("peer not found: %s", peerID)
	}

	delete(tt.peers, peerID)

	// Remove connection
	tt.removeConnection(peerID)

	if tt.logger != nil {
		tt.logger.Info("TCP peer removed",
			logger.String("peer_id", peerID),
		)
	}

	if tt.metrics != nil {
		tt.metrics.Counter("forge.consensus.transport.tcp.peers_removed").Inc()
		tt.metrics.Gauge("forge.consensus.transport.tcp.connected_peers").Set(float64(len(tt.peers)))
	}

	return nil
}

// GetPeers returns all peers
func (tt *TCPTransport) GetPeers() []PeerInfo {
	tt.peersmu.RLock()
	defer tt.peersmu.RUnlock()

	peers := make([]PeerInfo, 0, len(tt.peers))
	for _, peer := range tt.peers {
		peer.mu.RLock()
		peers = append(peers, peer.Info)
		peer.mu.RUnlock()
	}

	return peers
}

// LocalAddress returns the local address
func (tt *TCPTransport) LocalAddress() string {
	if tt.listener != nil {
		return tt.listener.Addr().String()
	}
	return fmt.Sprintf("%s:%d", tt.config.Address, tt.config.Port)
}

// HealthCheck performs a health check
func (tt *TCPTransport) HealthCheck(ctx context.Context) error {
	tt.mu.RLock()
	defer tt.mu.RUnlock()

	if !tt.started {
		return fmt.Errorf("transport not started")
	}

	// Check listener
	if tt.listener == nil {
		return fmt.Errorf("listener not initialized")
	}

	// Check error rate
	if tt.stats.MessagesSent > 0 {
		errorRate := float64(tt.stats.ErrorCount) / float64(tt.stats.MessagesSent)
		if errorRate > 0.1 { // 10% error rate threshold
			return fmt.Errorf("high error rate: %.2f%%", errorRate*100)
		}
	}

	return nil
}

// GetStats returns transport statistics
func (tt *TCPTransport) GetStats() TransportStats {
	tt.mu.RLock()
	defer tt.mu.RUnlock()

	stats := tt.stats
	stats.Uptime = time.Since(tt.startTime)
	stats.ConnectedPeers = len(tt.peers)

	return stats
}

// Close closes the transport
func (tt *TCPTransport) Close() error {
	return tt.Stop(context.Background())
}

// Internal methods

// acceptConnections accepts incoming connections
func (tt *TCPTransport) acceptConnections() {
	for {
		select {
		case <-tt.stopCh:
			return
		default:
			conn, err := tt.listener.Accept()
			if err != nil {
				if tt.logger != nil {
					tt.logger.Error("failed to accept connection", logger.Error(err))
				}
				continue
			}

			go tt.handleConnection(conn)
		}
	}
}

// handleConnection handles an incoming connection
func (tt *TCPTransport) handleConnection(conn net.Conn) {
	defer conn.Close()

	// Set read timeout
	conn.SetReadDeadline(time.Now().Add(tt.config.Timeout))

	for {
		select {
		case <-tt.stopCh:
			return
		default:
			// Read message
			message, err := tt.readMessage(conn)
			if err != nil {
				if err != io.EOF {
					if tt.logger != nil {
						tt.logger.Error("failed to read message", logger.Error(err))
					}
				}
				return
			}

			// Extract peer info
			peerInfo := PeerInfo{
				ID:      message.From,
				Address: conn.RemoteAddr().String(),
				Status:  PeerStatusConnected,
			}

			// Send to message channel
			select {
			case tt.messageChan <- IncomingMessage{
				Message: message,
				Peer:    peerInfo,
			}:
				// Update statistics
				tt.mu.Lock()
				tt.stats.MessagesReceived++
				tt.stats.BytesReceived += int64(MessageSize(message))
				tt.mu.Unlock()

				if tt.metrics != nil {
					tt.metrics.Counter("forge.consensus.transport.tcp.messages_received").Inc()
				}
			default:
				if tt.logger != nil {
					tt.logger.Warn("message buffer full, dropping message")
				}
			}

			// Reset read timeout
			conn.SetReadDeadline(time.Now().Add(tt.config.Timeout))
		}
	}
}

// getConnection gets or creates a connection to a peer
func (tt *TCPTransport) getConnection(peerID, address string) (*TCPConnection, error) {
	tt.connmu.RLock()
	conn, exists := tt.connections[peerID]
	tt.connmu.RUnlock()

	if exists {
		conn.mu.Lock()
		if conn.conn != nil {
			conn.lastUsed = time.Now()
			conn.mu.Unlock()
			return conn, nil
		}
		conn.mu.Unlock()
	}

	// Create new connection
	return tt.createConnection(peerID, address)
}

// createConnection creates a new connection to a peer
func (tt *TCPTransport) createConnection(peerID, address string) (*TCPConnection, error) {
	// Connect to peer
	dialer := &net.Dialer{
		Timeout:   tt.config.Timeout,
		KeepAlive: tt.config.KeepAliveTime,
	}

	var conn net.Conn
	var err error

	if tt.config.EnableTLS && tt.config.TLSConfig != nil {
		tlsConfig := &tls.Config{
			ServerName:         tt.config.TLSConfig.ServerName,
			InsecureSkipVerify: tt.config.TLSConfig.SkipVerify,
		}
		conn, err = tls.DialWithDialer(dialer, "tcp", address, tlsConfig)
	} else {
		conn, err = dialer.Dial("tcp", address)
	}

	if err != nil {
		return nil, NewTransportError(ErrCodeConnectionFailed, fmt.Sprintf("failed to connect to %s", address)).WithCause(err)
	}

	// Set keepalive
	if tcpConn, ok := conn.(*net.TCPConn); ok && tt.config.KeepAlive {
		tcpConn.SetKeepAlive(true)
		tcpConn.SetKeepAlivePeriod(tt.config.KeepAliveTime)
	}

	tcpConn := &TCPConnection{
		conn:       conn,
		peerID:     peerID,
		remoteAddr: address,
		lastUsed:   time.Now(),
	}

	tt.connmu.Lock()
	tt.connections[peerID] = tcpConn
	tt.connmu.Unlock()

	return tcpConn, nil
}

// removeConnection removes a connection
func (tt *TCPTransport) removeConnection(peerID string) {
	tt.connmu.Lock()
	defer tt.connmu.Unlock()

	if conn, exists := tt.connections[peerID]; exists {
		conn.mu.Lock()
		if conn.conn != nil {
			conn.conn.Close()
		}
		conn.mu.Unlock()
		delete(tt.connections, peerID)
	}
}

// sendMessage sends a message over a connection
func (tt *TCPTransport) sendMessage(conn *TCPConnection, data []byte) error {
	conn.mu.Lock()
	defer conn.mu.Unlock()

	if conn.conn == nil {
		return fmt.Errorf("connection closed")
	}

	// Set write timeout
	conn.conn.SetWriteDeadline(time.Now().Add(tt.config.Timeout))

	// Write message length
	length := uint32(len(data))
	if err := binary.Write(conn.conn, binary.BigEndian, length); err != nil {
		return err
	}

	// Write message data
	_, err := conn.conn.Write(data)
	return err
}

// readMessage reads a message from a connection
func (tt *TCPTransport) readMessage(conn net.Conn) (Message, error) {
	var message Message

	// Read message length
	var length uint32
	if err := binary.Read(conn, binary.BigEndian, &length); err != nil {
		return message, err
	}

	// Check message size
	if int(length) > tt.config.MaxMessageSize {
		return message, NewTransportError(ErrCodeMessageTooLarge, fmt.Sprintf("message too large: %d bytes", length))
	}

	// Read message data
	data := make([]byte, length)
	if _, err := io.ReadFull(conn, data); err != nil {
		return message, err
	}

	// Deserialize message
	if err := json.Unmarshal(data, &message); err != nil {
		return message, NewTransportError(ErrCodeSerializationError, "failed to deserialize message").WithCause(err)
	}

	return message, nil
}

// cleanupConnections cleans up idle connections
func (tt *TCPTransport) cleanupConnections() {
	ticker := time.NewTicker(tt.config.KeepAliveTime)
	defer ticker.Stop()

	for {
		select {
		case <-tt.stopCh:
			return
		case <-ticker.C:
			tt.connmu.Lock()
			now := time.Now()
			for peerID, conn := range tt.connections {
				conn.mu.RLock()
				if now.Sub(conn.lastUsed) > tt.config.KeepAliveTime*2 {
					conn.mu.RUnlock()
					if tt.logger != nil {
						tt.logger.Debug("closing idle connection",
							logger.String("peer_id", peerID),
							logger.Duration("idle_time", now.Sub(conn.lastUsed)),
						)
					}
					conn.mu.Lock()
					if conn.conn != nil {
						conn.conn.Close()
					}
					conn.mu.Unlock()
					delete(tt.connections, peerID)
				} else {
					conn.mu.RUnlock()
				}
			}
			tt.connmu.Unlock()
		}
	}
}

// testPeerConnection tests a peer connection
func (tt *TCPTransport) testPeerConnection(peer *TCPPeer) {
	// Create test message
	message := Message{
		Type:      MessageTypeHeartbeat,
		From:      tt.LocalAddress(),
		To:        peer.Info.ID,
		Data:      []byte("ping"),
		Timestamp: time.Now(),
		ID:        GenerateMessageID(),
	}

	ctx, cancel := context.WithTimeout(context.Background(), tt.config.Timeout)
	defer cancel()

	err := tt.Send(ctx, peer.Info.ID, message)

	peer.mu.Lock()
	if err != nil {
		peer.Info.Status = PeerStatusError
	} else {
		peer.Info.Status = PeerStatusConnected
		peer.Info.LastSeen = time.Now()
	}
	peer.mu.Unlock()
}
