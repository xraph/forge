package transport

import (
	"context"
	"crypto/tls"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/consensus/internal"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
)

// GRPCTransport implements gRPC-based network transport
type GRPCTransport struct {
	id        string
	address   string
	port      int
	server    *grpc.Server
	peers     map[string]*grpcPeerConn
	peersMu   sync.RWMutex
	inbox     chan internal.Message
	logger    forge.Logger
	tlsConfig *tls.Config

	// Lifecycle
	started bool
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	mu      sync.RWMutex
}

// grpcPeerConn represents a connection to a peer
type grpcPeerConn struct {
	nodeID   string
	address  string
	port     int
	conn     *grpc.ClientConn
	connMu   sync.RWMutex
	lastUsed time.Time
}

// GRPCTransportConfig contains gRPC transport configuration
type GRPCTransportConfig struct {
	NodeID               string
	Address              string
	Port                 int
	BufferSize           int
	MaxConnections       int
	ConnectionTimeout    time.Duration
	RequestTimeout       time.Duration
	KeepAliveTime        time.Duration
	KeepAliveTimeout     time.Duration
	MaxConcurrentStreams uint32
	TLSConfig            *tls.Config
}

// NewGRPCTransport creates a new gRPC transport
func NewGRPCTransport(config GRPCTransportConfig, logger forge.Logger) *GRPCTransport {
	if config.BufferSize == 0 {
		config.BufferSize = 100
	}
	if config.MaxConnections == 0 {
		config.MaxConnections = 100
	}
	if config.ConnectionTimeout == 0 {
		config.ConnectionTimeout = 10 * time.Second
	}
	if config.RequestTimeout == 0 {
		config.RequestTimeout = 30 * time.Second
	}
	if config.KeepAliveTime == 0 {
		config.KeepAliveTime = 30 * time.Second
	}
	if config.KeepAliveTimeout == 0 {
		config.KeepAliveTimeout = 10 * time.Second
	}

	return &GRPCTransport{
		id:        config.NodeID,
		address:   config.Address,
		port:      config.Port,
		peers:     make(map[string]*grpcPeerConn),
		inbox:     make(chan internal.Message, config.BufferSize),
		logger:    logger,
		tlsConfig: config.TLSConfig,
	}
}

// Start starts the gRPC transport
func (gt *GRPCTransport) Start(ctx context.Context) error {
	gt.mu.Lock()
	defer gt.mu.Unlock()

	if gt.started {
		return internal.ErrAlreadyStarted
	}

	gt.ctx, gt.cancel = context.WithCancel(ctx)
	gt.started = true

	// Create gRPC server with options
	serverOpts := []grpc.ServerOption{
		grpc.KeepaliveParams(keepalive.ServerParameters{
			Time:    30 * time.Second,
			Timeout: 10 * time.Second,
		}),
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
			MinTime:             10 * time.Second,
			PermitWithoutStream: true,
		}),
	}

	// Add TLS credentials if configured
	if gt.tlsConfig != nil {
		creds := credentials.NewTLS(gt.tlsConfig)
		serverOpts = append(serverOpts, grpc.Creds(creds))
		gt.logger.Info("gRPC transport using TLS")
	} else {
		gt.logger.Warn("gRPC transport running without TLS - NOT RECOMMENDED FOR PRODUCTION")
	}

	gt.server = grpc.NewServer(serverOpts...)

	// TODO: Register gRPC service implementation
	// consensuspb.RegisterConsensusServiceServer(gt.server, gt)

	// Start server goroutine
	gt.wg.Add(1)
	go gt.runServer()

	gt.logger.Info("gRPC transport started",
		forge.F("node_id", gt.id),
		forge.F("address", gt.address),
		forge.F("port", gt.port),
	)

	return nil
}

// Stop stops the gRPC transport
func (gt *GRPCTransport) Stop(ctx context.Context) error {
	gt.mu.Lock()
	if !gt.started {
		gt.mu.Unlock()
		return internal.ErrNotStarted
	}
	gt.mu.Unlock()

	if gt.cancel != nil {
		gt.cancel()
	}

	// Graceful shutdown of gRPC server
	if gt.server != nil {
		gt.server.GracefulStop()
	}

	// Close all peer connections
	gt.peersMu.Lock()
	for _, peer := range gt.peers {
		peer.connMu.Lock()
		if peer.conn != nil {
			peer.conn.Close()
		}
		peer.connMu.Unlock()
	}
	gt.peers = make(map[string]*grpcPeerConn)
	gt.peersMu.Unlock()

	// Wait for goroutines
	done := make(chan struct{})
	go func() {
		gt.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		gt.logger.Info("gRPC transport stopped")
	case <-ctx.Done():
		gt.logger.Warn("gRPC transport stop timed out")
	}

	return nil
}

// Send sends a message to a peer
func (gt *GRPCTransport) Send(ctx context.Context, target string, message interface{}) error {
	peer, err := gt.getPeerConn(target)
	if err != nil {
		return fmt.Errorf("failed to get peer connection: %w", err)
	}

	msg, ok := message.(internal.Message)
	if !ok {
		return fmt.Errorf("invalid message type")
	}

	// TODO: Convert to protobuf and send via gRPC
	// For now, log that we would send
	gt.logger.Debug("would send gRPC message",
		forge.F("target", target),
		forge.F("type", msg.Type),
	)

	peer.lastUsed = time.Now()

	return nil
}

// Receive returns a channel for receiving messages
func (gt *GRPCTransport) Receive() <-chan internal.Message {
	return gt.inbox
}

// AddPeer adds a peer to the transport
func (gt *GRPCTransport) AddPeer(nodeID, address string, port int) error {
	gt.peersMu.Lock()
	defer gt.peersMu.Unlock()

	if _, exists := gt.peers[nodeID]; exists {
		return internal.ErrPeerExists
	}

	gt.peers[nodeID] = &grpcPeerConn{
		nodeID:  nodeID,
		address: address,
		port:    port,
	}

	gt.logger.Info("peer added to gRPC transport",
		forge.F("node_id", gt.id),
		forge.F("peer_id", nodeID),
		forge.F("address", address),
		forge.F("port", port),
	)

	return nil
}

// RemovePeer removes a peer from the transport
func (gt *GRPCTransport) RemovePeer(nodeID string) error {
	gt.peersMu.Lock()
	defer gt.peersMu.Unlock()

	peer, exists := gt.peers[nodeID]
	if !exists {
		return internal.ErrNodeNotFound
	}

	// Close connection
	peer.connMu.Lock()
	if peer.conn != nil {
		peer.conn.Close()
	}
	peer.connMu.Unlock()

	delete(gt.peers, nodeID)

	gt.logger.Info("peer removed from gRPC transport",
		forge.F("node_id", gt.id),
		forge.F("peer_id", nodeID),
	)

	return nil
}

// GetAddress returns the local address
func (gt *GRPCTransport) GetAddress() string {
	return fmt.Sprintf("%s:%d", gt.address, gt.port)
}

// runServer runs the gRPC server
func (gt *GRPCTransport) runServer() {
	defer gt.wg.Done()

	// TODO: Implement actual server listen
	// For now, just wait for context cancellation
	<-gt.ctx.Done()
}

// getPeerConn gets or creates a connection to a peer
func (gt *GRPCTransport) getPeerConn(nodeID string) (*grpcPeerConn, error) {
	gt.peersMu.RLock()
	peer, exists := gt.peers[nodeID]
	gt.peersMu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("peer %s not found", nodeID)
	}

	peer.connMu.Lock()
	defer peer.connMu.Unlock()

	// Create connection if not exists or closed
	if peer.conn == nil {
		target := fmt.Sprintf("%s:%d", peer.address, peer.port)

		dialOpts := []grpc.DialOption{
			grpc.WithKeepaliveParams(keepalive.ClientParameters{
				Time:                30 * time.Second,
				Timeout:             10 * time.Second,
				PermitWithoutStream: true,
			}),
		}

		// Add TLS credentials if configured
		if gt.tlsConfig != nil {
			creds := credentials.NewTLS(gt.tlsConfig)
			dialOpts = append(dialOpts, grpc.WithTransportCredentials(creds))
		} else {
			dialOpts = append(dialOpts, grpc.WithTransportCredentials(insecure.NewCredentials()))
		}

		ctx, cancel := context.WithTimeout(gt.ctx, 10*time.Second)
		defer cancel()

		conn, err := grpc.DialContext(ctx, target, dialOpts...)
		if err != nil {
			return nil, fmt.Errorf("failed to dial peer: %w", err)
		}

		peer.conn = conn
		gt.logger.Info("connected to peer",
			forge.F("peer_id", nodeID),
			forge.F("address", target),
		)
	}

	return peer, nil
}
