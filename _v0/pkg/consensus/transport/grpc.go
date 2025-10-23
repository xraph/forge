package transport

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/peer"

	"github.com/xraph/forge/v0/pkg/common"
	"github.com/xraph/forge/v0/pkg/logger"
)

// GRPCTransport implements Transport using gRPC
type GRPCTransport struct {
	config      TransportConfig
	logger      common.Logger
	metrics     common.Metrics
	server      *grpc.Server
	listener    net.Listener
	peers       map[string]*GRPCPeer
	clients     map[string]*grpc.ClientConn
	messageChan chan IncomingMessage
	stopCh      chan struct{}
	stats       TransportStats
	startTime   time.Time
	mu          sync.RWMutex
	peersmu     sync.RWMutex
	started     bool
}

// GRPCPeer represents a gRPC peer
type GRPCPeer struct {
	Info   PeerInfo
	Client ConsensusServiceClient
	Conn   *grpc.ClientConn
	mu     sync.RWMutex
}

// GRPCTransportFactory creates gRPC transport instances
type GRPCTransportFactory struct {
	logger  common.Logger
	metrics common.Metrics
}

// NewGRPCTransportFactory creates a new gRPC transport factory
func NewGRPCTransportFactory(logger common.Logger, metrics common.Metrics) *GRPCTransportFactory {
	return &GRPCTransportFactory{
		logger:  logger,
		metrics: metrics,
	}
}

// Create creates a new gRPC transport
func (f *GRPCTransportFactory) Create(config TransportConfig) (Transport, error) {
	if config.Type != TransportTypeGRPC {
		return nil, fmt.Errorf("invalid transport type: %s", config.Type)
	}

	// Set defaults
	if config.Port == 0 {
		config.Port = 8090
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

	transport := &GRPCTransport{
		config:      config,
		logger:      f.logger,
		metrics:     f.metrics,
		peers:       make(map[string]*GRPCPeer),
		clients:     make(map[string]*grpc.ClientConn),
		messageChan: make(chan IncomingMessage, config.BufferSize),
		stopCh:      make(chan struct{}),
		startTime:   time.Now(),
		stats:       TransportStats{},
	}

	return transport, nil
}

// Name returns the factory name
func (f *GRPCTransportFactory) Name() string {
	return TransportTypeGRPC
}

// Version returns the factory version
func (f *GRPCTransportFactory) Version() string {
	return "1.0.0"
}

// ValidateConfig validates the configuration
func (f *GRPCTransportFactory) ValidateConfig(config TransportConfig) error {
	if config.Type != TransportTypeGRPC {
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

// Start starts the gRPC transport
func (gt *GRPCTransport) Start(ctx context.Context) error {
	gt.mu.Lock()
	defer gt.mu.Unlock()

	if gt.started {
		return fmt.Errorf("transport already started")
	}

	// Create listener
	address := fmt.Sprintf("%s:%d", gt.config.Address, gt.config.Port)
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", address, err)
	}

	gt.listener = listener

	// Create server options
	var serverOpts []grpc.ServerOption

	// Add TLS if enabled
	if gt.config.EnableTLS && gt.config.TLSConfig != nil {
		cert, err := tls.LoadX509KeyPair(gt.config.TLSConfig.CertFile, gt.config.TLSConfig.KeyFile)
		if err != nil {
			return fmt.Errorf("failed to load TLS certificate: %w", err)
		}

		creds := credentials.NewTLS(&tls.Config{
			Certificates: []tls.Certificate{cert},
			ServerName:   gt.config.TLSConfig.ServerName,
		})

		serverOpts = append(serverOpts, grpc.Creds(creds))
	}

	// Add keepalive parameters
	if gt.config.KeepAlive {
		keepaliveParams := keepalive.ServerParameters{
			MaxConnectionIdle:     gt.config.KeepAliveTime,
			MaxConnectionAge:      gt.config.KeepAliveTime * 2,
			MaxConnectionAgeGrace: gt.config.KeepAliveTime,
			Time:                  gt.config.KeepAliveTime,
			Timeout:               gt.config.Timeout,
		}
		serverOpts = append(serverOpts, grpc.KeepaliveParams(keepaliveParams))
	}

	// Add message size limits
	serverOpts = append(serverOpts,
		grpc.MaxRecvMsgSize(gt.config.MaxMessageSize),
		grpc.MaxSendMsgSize(gt.config.MaxMessageSize),
	)

	// Create gRPC server
	gt.server = grpc.NewServer(serverOpts...)

	// Register consensus service
	RegisterConsensusServiceServer(gt.server, gt)

	// Start server
	go func() {
		if err := gt.server.Serve(listener); err != nil {
			if gt.logger != nil {
				gt.logger.Error("gRPC server error", logger.Error(err))
			}
		}
	}()

	gt.started = true

	if gt.logger != nil {
		gt.logger.Info("gRPC transport started",
			logger.String("address", address),
			logger.Bool("tls_enabled", gt.config.EnableTLS),
		)
	}

	if gt.metrics != nil {
		gt.metrics.Counter("forge.consensus.transport.grpc.started").Inc()
	}

	return nil
}

func (gt *GRPCTransport) GetAddress(nodeID string) (string, error) {
	gt.peersmu.RLock()
	defer gt.peersmu.RUnlock()

	if _, exists := gt.peers[nodeID]; !exists {
		return "", NewTransportError(ErrCodePeerNotFound, fmt.Sprintf("peer not found: %s", nodeID))
	}

	return gt.peers[nodeID].Info.Address, nil
}

// Stop stops the gRPC transport
func (gt *GRPCTransport) Stop(ctx context.Context) error {
	gt.mu.Lock()
	defer gt.mu.Unlock()

	if !gt.started {
		return nil
	}

	// Signal stop
	close(gt.stopCh)

	// Stop server
	if gt.server != nil {
		gt.server.GracefulStop()
	}

	// Close listener
	if gt.listener != nil {
		gt.listener.Close()
	}

	// Close client connections
	for _, client := range gt.clients {
		client.Close()
	}

	// Close message channel
	close(gt.messageChan)

	gt.started = false

	if gt.logger != nil {
		gt.logger.Info("gRPC transport stopped")
	}

	if gt.metrics != nil {
		gt.metrics.Counter("forge.consensus.transport.grpc.stopped").Inc()
	}

	return nil
}

// Send sends a message to a peer
func (gt *GRPCTransport) Send(ctx context.Context, target string, message Message) error {
	gt.peersmu.RLock()
	peer, exists := gt.peers[target]
	gt.peersmu.RUnlock()

	if !exists {
		return NewTransportError(ErrCodePeerNotFound, fmt.Sprintf("peer not found: %s", target))
	}

	// Convert message to gRPC message
	grpcMsg := &ConsensusMessage{
		Type:      string(message.Type),
		From:      message.From,
		To:        message.To,
		Data:      message.Data,
		Metadata:  convertMetadata(message.Metadata),
		Timestamp: message.Timestamp.Unix(),
		Id:        message.ID,
	}

	// Send message
	ctx, cancel := context.WithTimeout(ctx, gt.config.Timeout)
	defer cancel()

	peer.mu.RLock()
	client := peer.Client
	peer.mu.RUnlock()

	start := time.Now()
	response, err := client.SendMessage(ctx, grpcMsg)
	latency := time.Since(start)

	// Update statistics
	gt.mu.Lock()
	gt.stats.MessagesSent++
	gt.stats.BytesSent += int64(len(message.Data))
	if err != nil {
		gt.stats.ErrorCount++
	}
	gt.mu.Unlock()

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
		return NewTransportError(ErrCodeNetworkError, fmt.Sprintf("failed to send message to %s", target)).
			WithCause(err)
	}

	if !response.Success {
		return NewTransportError(ErrCodeInvalidMessage, response.Error)
	}

	if gt.metrics != nil {
		gt.metrics.Counter("forge.consensus.transport.grpc.messages_sent").Inc()
		gt.metrics.Histogram("forge.consensus.transport.grpc.send_latency").Observe(latency.Seconds())
	}

	return nil
}

// Receive receives messages from peers
func (gt *GRPCTransport) Receive(ctx context.Context) (<-chan IncomingMessage, error) {
	return gt.messageChan, nil
}

// AddPeer adds a peer to the transport
func (gt *GRPCTransport) AddPeer(peerID, address string) error {
	gt.peersmu.Lock()
	defer gt.peersmu.Unlock()

	if _, exists := gt.peers[peerID]; exists {
		return fmt.Errorf("peer already exists: %s", peerID)
	}

	// Create client connection
	conn, err := gt.createClientConnection(address)
	if err != nil {
		return fmt.Errorf("failed to create connection to %s: %w", address, err)
	}

	client := NewConsensusServiceClient(conn)

	peer := &GRPCPeer{
		Info: PeerInfo{
			ID:       peerID,
			Address:  address,
			Status:   PeerStatusConnecting,
			LastSeen: time.Now(),
			Metadata: make(map[string]interface{}),
		},
		Client: client,
		Conn:   conn,
	}

	gt.peers[peerID] = peer
	gt.clients[peerID] = conn

	// Test connection
	go gt.testPeerConnection(peer)

	if gt.logger != nil {
		gt.logger.Info("peer added",
			logger.String("peer_id", peerID),
			logger.String("address", address),
		)
	}

	if gt.metrics != nil {
		gt.metrics.Counter("forge.consensus.transport.grpc.peers_added").Inc()
		gt.metrics.Gauge("forge.consensus.transport.grpc.connected_peers").Set(float64(len(gt.peers)))
	}

	return nil
}

// RemovePeer removes a peer from the transport
func (gt *GRPCTransport) RemovePeer(peerID string) error {
	gt.peersmu.Lock()
	defer gt.peersmu.Unlock()

	peer, exists := gt.peers[peerID]
	if !exists {
		return fmt.Errorf("peer not found: %s", peerID)
	}

	// Close connection
	if peer.Conn != nil {
		peer.Conn.Close()
	}

	delete(gt.peers, peerID)
	delete(gt.clients, peerID)

	if gt.logger != nil {
		gt.logger.Info("peer removed",
			logger.String("peer_id", peerID),
		)
	}

	if gt.metrics != nil {
		gt.metrics.Counter("forge.consensus.transport.grpc.peers_removed").Inc()
		gt.metrics.Gauge("forge.consensus.transport.grpc.connected_peers").Set(float64(len(gt.peers)))
	}

	return nil
}

// GetPeers returns all peers
func (gt *GRPCTransport) GetPeers() []PeerInfo {
	gt.peersmu.RLock()
	defer gt.peersmu.RUnlock()

	peers := make([]PeerInfo, 0, len(gt.peers))
	for _, peer := range gt.peers {
		peer.mu.RLock()
		peers = append(peers, peer.Info)
		peer.mu.RUnlock()
	}

	return peers
}

// LocalAddress returns the local address
func (gt *GRPCTransport) LocalAddress() string {
	if gt.listener != nil {
		return gt.listener.Addr().String()
	}
	return fmt.Sprintf("%s:%d", gt.config.Address, gt.config.Port)
}

// HealthCheck performs a health check
func (gt *GRPCTransport) HealthCheck(ctx context.Context) error {
	gt.mu.RLock()
	defer gt.mu.RUnlock()

	if !gt.started {
		return fmt.Errorf("transport not started")
	}

	// Check server
	if gt.server == nil {
		return fmt.Errorf("gRPC server not initialized")
	}

	// Check listener
	if gt.listener == nil {
		return fmt.Errorf("listener not initialized")
	}

	// Check error rate
	if gt.stats.MessagesSent > 0 {
		errorRate := float64(gt.stats.ErrorCount) / float64(gt.stats.MessagesSent)
		if errorRate > 0.1 { // 10% error rate threshold
			return fmt.Errorf("high error rate: %.2f%%", errorRate*100)
		}
	}

	return nil
}

// GetStats returns transport statistics
func (gt *GRPCTransport) GetStats() TransportStats {
	gt.mu.RLock()
	defer gt.mu.RUnlock()

	stats := gt.stats
	stats.Uptime = time.Since(gt.startTime)
	stats.ConnectedPeers = len(gt.peers)

	return stats
}

// Close closes the transport
func (gt *GRPCTransport) Close() error {
	return gt.Stop(context.Background())
}

// gRPC service implementation

// SendMessage handles incoming messages
func (gt *GRPCTransport) SendMessage(ctx context.Context, req *ConsensusMessage) (*MessageResponse, error) {
	// Get peer info
	var peerInfo PeerInfo
	if p, ok := peer.FromContext(ctx); ok {
		peerInfo = PeerInfo{
			ID:      req.From,
			Address: p.Addr.String(),
			Status:  PeerStatusConnected,
		}
	}

	// Convert gRPC message to internal message
	message := Message{
		Type:      MessageType(req.Type),
		From:      req.From,
		To:        req.To,
		Data:      req.Data,
		Metadata:  convertFromGRPCMetadata(req.Metadata),
		Timestamp: time.Unix(req.Timestamp, 0),
		ID:        req.Id,
	}

	// Validate message
	if err := ValidateMessage(message); err != nil {
		return &MessageResponse{
			Success: false,
			Error:   err.Error(),
		}, nil
	}

	// Send to message channel
	select {
	case gt.messageChan <- IncomingMessage{
		Message: message,
		Peer:    peerInfo,
	}:
		// Update statistics
		gt.mu.Lock()
		gt.stats.MessagesReceived++
		gt.stats.BytesReceived += int64(len(message.Data))
		gt.mu.Unlock()

		if gt.metrics != nil {
			gt.metrics.Counter("forge.consensus.transport.grpc.messages_received").Inc()
		}

		return &MessageResponse{Success: true}, nil
	default:
		return &MessageResponse{
			Success: false,
			Error:   "message buffer full",
		}, nil
	}
}

// createClientConnection creates a client connection
func (gt *GRPCTransport) createClientConnection(address string) (*grpc.ClientConn, error) {
	var dialOpts []grpc.DialOption

	// Add TLS if enabled
	if gt.config.EnableTLS && gt.config.TLSConfig != nil {
		creds := credentials.NewTLS(&tls.Config{
			ServerName:         gt.config.TLSConfig.ServerName,
			InsecureSkipVerify: gt.config.TLSConfig.SkipVerify,
		})
		dialOpts = append(dialOpts, grpc.WithTransportCredentials(creds))
	} else {
		dialOpts = append(dialOpts, grpc.WithInsecure())
	}

	// Add keepalive parameters
	if gt.config.KeepAlive {
		keepaliveParams := keepalive.ClientParameters{
			Time:                gt.config.KeepAliveTime,
			Timeout:             gt.config.Timeout,
			PermitWithoutStream: true,
		}
		dialOpts = append(dialOpts, grpc.WithKeepaliveParams(keepaliveParams))
	}

	// Add message size limits
	dialOpts = append(dialOpts,
		grpc.WithDefaultCallOptions(
			grpc.MaxCallRecvMsgSize(gt.config.MaxMessageSize),
			grpc.MaxCallSendMsgSize(gt.config.MaxMessageSize),
		),
	)

	// Create connection
	ctx, cancel := context.WithTimeout(context.Background(), gt.config.Timeout)
	defer cancel()

	return grpc.DialContext(ctx, address, dialOpts...)
}

// testPeerConnection tests a peer connection
func (gt *GRPCTransport) testPeerConnection(peer *GRPCPeer) {
	ctx, cancel := context.WithTimeout(context.Background(), gt.config.Timeout)
	defer cancel()

	// Send ping message
	pingMsg := &ConsensusMessage{
		Type:      string(MessageTypeHeartbeat),
		From:      gt.LocalAddress(),
		To:        peer.Info.ID,
		Data:      []byte("ping"),
		Timestamp: time.Now().Unix(),
		Id:        GenerateMessageID(),
	}

	peer.mu.RLock()
	client := peer.Client
	peer.mu.RUnlock()

	_, err := client.SendMessage(ctx, pingMsg)

	peer.mu.Lock()
	if err != nil {
		peer.Info.Status = PeerStatusError
	} else {
		peer.Info.Status = PeerStatusConnected
		peer.Info.LastSeen = time.Now()
	}
	peer.mu.Unlock()
}

// Helper functions

// convertMetadata converts internal metadata to gRPC metadata
func convertMetadata(metadata map[string]interface{}) map[string]string {
	result := make(map[string]string)
	for k, v := range metadata {
		if s, ok := v.(string); ok {
			result[k] = s
		} else {
			result[k] = fmt.Sprintf("%v", v)
		}
	}
	return result
}

// convertFromGRPCMetadata converts gRPC metadata to internal metadata
func convertFromGRPCMetadata(metadata map[string]string) map[string]interface{} {
	result := make(map[string]interface{})
	for k, v := range metadata {
		result[k] = v
	}
	return result
}

// Generated gRPC service definitions would go here
// For brevity, I'm showing the interface definitions

// ConsensusServiceServer is the server API for ConsensusService
type ConsensusServiceServer interface {
	SendMessage(context.Context, *ConsensusMessage) (*MessageResponse, error)
}

// ConsensusServiceClient is the client API for ConsensusService
type ConsensusServiceClient interface {
	SendMessage(ctx context.Context, in *ConsensusMessage, opts ...grpc.CallOption) (*MessageResponse, error)
}

// ConsensusMessage represents a gRPC message
type ConsensusMessage struct {
	Type      string            `protobuf:"bytes,1,opt,name=type,proto3" json:"type,omitempty"`
	From      string            `protobuf:"bytes,2,opt,name=from,proto3" json:"from,omitempty"`
	To        string            `protobuf:"bytes,3,opt,name=to,proto3" json:"to,omitempty"`
	Data      []byte            `protobuf:"bytes,4,opt,name=data,proto3" json:"data,omitempty"`
	Metadata  map[string]string `protobuf:"bytes,5,rep,name=metadata,proto3" json:"metadata,omitempty"`
	Timestamp int64             `protobuf:"varint,6,opt,name=timestamp,proto3" json:"timestamp,omitempty"`
	Id        string            `protobuf:"bytes,7,opt,name=id,proto3" json:"id,omitempty"`
}

// MessageResponse represents a gRPC message response
type MessageResponse struct {
	Success bool   `protobuf:"varint,1,opt,name=success,proto3" json:"success,omitempty"`
	Error   string `protobuf:"bytes,2,opt,name=error,proto3" json:"error,omitempty"`
}

// RegisterConsensusServiceServer registers the service
func RegisterConsensusServiceServer(s *grpc.Server, srv ConsensusServiceServer) {
	// Implementation would register the service with gRPC server
}

// NewConsensusServiceClient creates a new client
func NewConsensusServiceClient(cc *grpc.ClientConn) ConsensusServiceClient {
	// Implementation would create a new gRPC client
	return nil
}
