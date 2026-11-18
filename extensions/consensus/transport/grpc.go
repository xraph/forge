package transport

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/errors"
	"github.com/xraph/forge/extensions/consensus/internal"
	"google.golang.org/grpc"
	"google.golang.org/grpc/backoff"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

const (
	// grpcServiceName is the hand written gRPC service the transport serves.
	// There is no .proto file: its one method carries the wire envelope bytes
	// through envelopeCodec, so every transport shares one message format.
	grpcServiceName = "forge.consensus.raft.v1.Transport"
	// grpcSendMethod is the full name of the unary call that delivers one
	// message.
	grpcSendMethod = "/" + grpcServiceName + "/Send"

	// grpcCodecName becomes the content-subtype of every call
	// (application/grpc+raft-envelope).
	grpcCodecName = "raft-envelope"

	// grpcMinPingInterval is the shortest client keepalive interval the
	// server tolerates. gRPC clients never ping more often than every 10s, so
	// this only cuts off misbehaving peers.
	grpcMinPingInterval = 5 * time.Second

	// grpcGracefulStopTimeout bounds how long Stop waits for in-flight calls
	// before it closes connections outright.
	grpcGracefulStopTimeout = time.Second

	// grpcMaxHeaderListSize caps request metadata. Raft calls only carry the
	// standard gRPC headers.
	grpcMaxHeaderListSize = 16 << 10
)

// GRPCTransport implements gRPC-based network transport.
//
// Each message is one unary call to forge.consensus.raft.v1.Transport/Send.
// The request is the JSON wire envelope, carried as raw bytes by a custom
// codec instead of protobuf, so the receiver checks the protocol version and
// payload exactly as the TCP and HTTP transports do. The receiver queues the
// message and answers with an empty message; Raft responses travel back as
// separate messages and are matched to their request by RequestID. Each node
// keeps one client connection per peer and reuses it for every call.
type GRPCTransport struct {
	id       string
	address  string
	port     int
	config   GRPCTransportConfig
	server   *grpc.Server
	listener *connTrackingListener
	peers    map[string]*grpcPeerConn
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

// grpcPeerConn holds the client connection to one peer. The connection is
// created on the first Send and shared by every later call.
type grpcPeerConn struct {
	nodeID  string
	address string
	port    int

	connMu  sync.Mutex
	conn    *grpc.ClientConn
	removed bool
}

// GRPCTransportConfig contains gRPC transport configuration.
type GRPCTransportConfig struct {
	NodeID string
	// Address to listen on. Empty means 127.0.0.1; set it explicitly (for
	// example to 0.0.0.0) to accept peers from other hosts.
	Address    string
	Port       int
	BufferSize int
	// MaxConnections caps concurrent inbound connections. Defaults to 128.
	MaxConnections int
	// ConnectionTimeout bounds connecting to a peer, and the handshake of an
	// inbound connection.
	ConnectionTimeout time.Duration
	// RequestTimeout bounds one Send when the caller's context has no
	// earlier deadline.
	RequestTimeout time.Duration
	// KeepAliveTime is how often an idle connection is pinged, and
	// KeepAliveTimeout how long a ping may go unanswered before the
	// connection is closed.
	KeepAliveTime    time.Duration
	KeepAliveTimeout time.Duration
	// IdleTimeout closes an inbound connection that has carried no call for
	// this long. The sender reconnects on its next message.
	IdleTimeout time.Duration
	// MaxConcurrentStreams caps concurrent calls on one inbound connection.
	// Defaults to 256.
	MaxConcurrentStreams uint32

	// MaxMessageSize is the largest message accepted or sent, in bytes.
	// Defaults to DefaultMaxMessageSize.
	MaxMessageSize int

	// TLSConfig enables TLS for both the server and outbound connections. It
	// must hold this node's certificate and the CA that signs its peers;
	// TLSManager.GetTLSConfig builds one.
	TLSConfig *tls.Config
	// RequirePeerIdentity rejects any message whose From is not named by the
	// caller's client certificate, and verifies that each dialed peer's
	// certificate names that peer. It requires TLSConfig with ClientAuth set
	// to tls.RequireAndVerifyClientCert, and every node certificate must carry
	// its node ID as a DNS subject alternative name.
	RequirePeerIdentity bool
}

// NewGRPCTransport creates a new gRPC transport.
func NewGRPCTransport(config GRPCTransportConfig, logger forge.Logger) *GRPCTransport {
	if config.Address == "" {
		config.Address = DefaultBindAddress
	}

	if config.BufferSize == 0 {
		config.BufferSize = 100
	}

	if config.MaxConnections <= 0 {
		config.MaxConnections = 128
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

	if config.IdleTimeout == 0 {
		config.IdleTimeout = 2 * time.Minute
	}

	if config.MaxConcurrentStreams == 0 {
		config.MaxConcurrentStreams = 256
	}

	if config.MaxMessageSize <= 0 {
		config.MaxMessageSize = DefaultMaxMessageSize
	}

	return &GRPCTransport{
		id:      config.NodeID,
		address: config.Address,
		port:    config.Port,
		config:  config,
		peers:   make(map[string]*grpcPeerConn),
		inbox:   make(chan internal.Message, config.BufferSize),
		logger:  logger,
	}
}

// Start starts the gRPC server. The listener is opened before Start returns,
// so a port that cannot be bound is reported to the caller.
func (gt *GRPCTransport) Start(ctx context.Context) error {
	gt.mu.Lock()
	defer gt.mu.Unlock()

	if gt.started {
		return internal.ErrAlreadyStarted
	}

	if err := checkIdentityTLS(gt.config.TLSConfig, gt.config.RequirePeerIdentity); err != nil {
		return fmt.Errorf("grpc transport: %w", err)
	}

	var listenConfig net.ListenConfig

	listener, err := listenConfig.Listen(ctx, "tcp", net.JoinHostPort(gt.address, strconv.Itoa(gt.port)))
	if err != nil {
		return fmt.Errorf("failed to start gRPC listener: %w", err)
	}

	if addr, ok := listener.Addr().(*net.TCPAddr); ok && gt.port == 0 {
		gt.port = addr.Port
	}

	// The cap counts raw connections, so a client stuck in the TLS handshake
	// holds a slot too.
	tracked := newConnTrackingListener(newLimitListener(listener, gt.config.MaxConnections))

	// The lifecycle context is not derived from ctx's cancellation: callers
	// commonly pass a startup context that ends before the transport stops.
	lifecycle, cancel := context.WithCancel(context.WithoutCancel(ctx))

	server := grpc.NewServer(gt.serverOptions()...)
	server.RegisterService(&grpcServiceDesc, &grpcReceiver{transport: gt, ctx: lifecycle})

	gt.ctx, gt.cancel = lifecycle, cancel
	gt.listener = tracked
	gt.server = server

	gt.wg.Go(func() {
		if err := server.Serve(tracked); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			gt.logger.Error("gRPC server error",
				forge.F("error", err),
			)
		}
	})

	gt.started = true

	gt.logger.Info("gRPC transport started",
		forge.F("node_id", gt.id),
		forge.F("address", tracked.Addr().String()),
		forge.F("tls", gt.config.TLSConfig != nil),
		forge.F("peer_identity", gt.config.RequirePeerIdentity),
	)

	return nil
}

func (gt *GRPCTransport) serverOptions() []grpc.ServerOption {
	creds := insecure.NewCredentials()
	if gt.config.TLSConfig != nil {
		creds = credentials.NewTLS(gt.config.TLSConfig.Clone())
	}

	return []grpc.ServerOption{
		grpc.Creds(creds),
		grpc.ForceServerCodec(envelopeCodec{}),
		grpc.MaxRecvMsgSize(gt.config.MaxMessageSize),
		grpc.MaxSendMsgSize(gt.config.MaxMessageSize),
		grpc.MaxConcurrentStreams(gt.config.MaxConcurrentStreams),
		grpc.MaxHeaderListSize(grpcMaxHeaderListSize),
		grpc.ConnectionTimeout(gt.config.ConnectionTimeout),
		grpc.KeepaliveParams(keepalive.ServerParameters{
			MaxConnectionIdle: gt.config.IdleTimeout,
			Time:              gt.config.KeepAliveTime,
			Timeout:           gt.config.KeepAliveTimeout,
		}),
		// Peers ping idle connections. The default policy would close a
		// connection whose client pings more often than every 5 minutes.
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
			MinTime:             grpcMinPingInterval,
			PermitWithoutStream: true,
		}),
	}
}

// Stop stops the server and closes every inbound and outbound connection.
// In-flight calls get a short grace period, so Stop returns promptly even
// when a peer stops responding.
func (gt *GRPCTransport) Stop(ctx context.Context) error {
	gt.mu.Lock()

	if !gt.started {
		gt.mu.Unlock()

		return internal.ErrNotStarted
	}

	gt.started = false
	server, listener, cancel := gt.server, gt.listener, gt.cancel
	gt.server, gt.listener = nil, nil
	gt.mu.Unlock()

	if cancel != nil {
		cancel()
	}

	gt.peersMu.RLock()

	for _, peer := range gt.peers {
		peer.connMu.Lock()
		peer.closeLocked(gt.logger)
		peer.connMu.Unlock()
	}

	gt.peersMu.RUnlock()

	if server != nil {
		stopServer(ctx, server, listener)
	}

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

// stopServer drains the server, and closes it outright once the grace period
// or ctx runs out. grpc.Server waits for every handshake in progress before
// it stops, even in Stop, so the raw connections are closed first: a client
// that connects and then stays silent would otherwise hold Stop for the whole
// ConnectionTimeout.
func stopServer(ctx context.Context, server *grpc.Server, listener *connTrackingListener) {
	drained := make(chan struct{})

	go func() {
		server.GracefulStop()
		close(drained)
	}()

	timer := time.NewTimer(grpcGracefulStopTimeout)
	defer timer.Stop()

	select {
	case <-drained:
		return
	case <-timer.C:
	case <-ctx.Done():
	}

	listener.closeAll()
	server.Stop()

	select {
	case <-drained:
	case <-ctx.Done():
	}
}

// Send delivers a message to a peer. It returns once the peer has queued the
// message, or with the peer's reason for refusing it: ErrProtocolVersion for a
// peer on another wire protocol version, and ErrMessageTooLarge for a message
// over either side's limit.
func (gt *GRPCTransport) Send(ctx context.Context, target string, message any) error {
	data, _, err := encodeMessage(message)
	if err != nil {
		return err
	}

	if len(data) > gt.config.MaxMessageSize {
		return fmt.Errorf("%w: %d bytes, limit %d", ErrMessageTooLarge, len(data), gt.config.MaxMessageSize)
	}

	gt.peersMu.RLock()
	peer, exists := gt.peers[target]
	gt.peersMu.RUnlock()

	if !exists {
		return fmt.Errorf("peer %s not found", target)
	}

	conn, lifecycle, err := gt.clientFor(peer)
	if err != nil {
		return err
	}

	callCtx, cancel := context.WithTimeout(ctx, gt.config.RequestTimeout)
	defer cancel()

	stop := context.AfterFunc(lifecycle, cancel)
	defer stop()

	if err := conn.Invoke(callCtx, grpcSendMethod, &envelopeFrame{data: data}, &envelopeFrame{}); err != nil {
		return sendError(target, err)
	}

	return nil
}

// sendError maps the status a peer answered with onto the errors the other
// transports return.
func sendError(target string, err error) error {
	st, ok := status.FromError(err)
	if !ok {
		return fmt.Errorf("failed to send to %s: %w", target, err)
	}

	switch st.Code() {
	case codes.FailedPrecondition:
		return fmt.Errorf("%w: peer %s refused the message: %s", ErrProtocolVersion, target, st.Message())
	case codes.ResourceExhausted:
		return fmt.Errorf("%w: peer %s refused the message: %s", ErrMessageTooLarge, target, st.Message())
	default:
		return fmt.Errorf("failed to send to %s: %w", target, err)
	}
}

// clientFor returns the peer's client connection, creating it on first use,
// and the lifecycle context the call must not outlive.
func (gt *GRPCTransport) clientFor(peer *grpcPeerConn) (*grpc.ClientConn, context.Context, error) {
	peer.connMu.Lock()
	defer peer.connMu.Unlock()

	// Stop clears started under mu before it closes peer connections, so
	// holding mu.RLock here means it cannot miss a connection created now.
	gt.mu.RLock()
	defer gt.mu.RUnlock()

	if !gt.started || gt.ctx == nil {
		return nil, nil, internal.ErrNotStarted
	}

	if peer.removed {
		return nil, nil, fmt.Errorf("peer %s not found", peer.nodeID)
	}

	if peer.conn != nil {
		return peer.conn, gt.ctx, nil
	}

	target := net.JoinHostPort(peer.address, strconv.Itoa(peer.port))

	conn, err := grpc.NewClient("passthrough:///"+target, gt.dialOptions(peer.nodeID)...)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create gRPC client for peer %s: %w", peer.nodeID, err)
	}

	peer.conn = conn

	gt.logger.Info("created gRPC client for peer",
		forge.F("peer_id", peer.nodeID),
		forge.F("address", target),
	)

	return conn, gt.ctx, nil
}

func (gt *GRPCTransport) dialOptions(peerID string) []grpc.DialOption {
	creds := insecure.NewCredentials()
	if gt.config.TLSConfig != nil {
		creds = credentials.NewTLS(clientTLSFor(gt.config.TLSConfig, peerID, gt.config.RequirePeerIdentity))
	}

	return []grpc.DialOption{
		grpc.WithTransportCredentials(creds),
		grpc.WithDefaultCallOptions(
			grpc.ForceCodec(envelopeCodec{}),
			grpc.MaxCallSendMsgSize(gt.config.MaxMessageSize),
			grpc.MaxCallRecvMsgSize(gt.config.MaxMessageSize),
		),
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:                gt.config.KeepAliveTime,
			Timeout:             gt.config.KeepAliveTimeout,
			PermitWithoutStream: true,
		}),
		// The default backoff grows to two minutes, which would leave a
		// restarted peer unreachable long after it is back. Raft retries on
		// every heartbeat, so reconnect attempts stay frequent.
		grpc.WithConnectParams(grpc.ConnectParams{
			Backoff: backoff.Config{
				BaseDelay:  100 * time.Millisecond,
				Multiplier: 1.6,
				Jitter:     0.2,
				MaxDelay:   time.Second,
			},
			MinConnectTimeout: gt.config.ConnectionTimeout,
		}),
	}
}

// Receive returns a channel for receiving messages.
func (gt *GRPCTransport) Receive() <-chan internal.Message {
	return gt.inbox
}

// AddPeer adds a peer to the transport.
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

// RemovePeer removes a peer from the transport and closes its connection.
func (gt *GRPCTransport) RemovePeer(nodeID string) error {
	gt.peersMu.Lock()
	defer gt.peersMu.Unlock()

	peer, exists := gt.peers[nodeID]
	if !exists {
		return internal.ErrNodeNotFound
	}

	peer.connMu.Lock()
	peer.removed = true
	peer.closeLocked(gt.logger)
	peer.connMu.Unlock()

	delete(gt.peers, nodeID)

	gt.logger.Info("peer removed from gRPC transport",
		forge.F("node_id", gt.id),
		forge.F("peer_id", nodeID),
	)

	return nil
}

// GetAddress returns the local address.
func (gt *GRPCTransport) GetAddress() string {
	gt.mu.RLock()
	defer gt.mu.RUnlock()

	return net.JoinHostPort(gt.address, strconv.Itoa(gt.port))
}

// closeLocked closes the client connection. peer.connMu is held.
func (p *grpcPeerConn) closeLocked(logger forge.Logger) {
	if p.conn == nil {
		return
	}

	if err := p.conn.Close(); err != nil {
		logger.Debug("failed to close gRPC client connection",
			forge.F("peer_id", p.nodeID),
			forge.F("error", err),
		)
	}

	p.conn = nil
}

// connTrackingListener records the connections it accepts so Stop can close
// those gRPC does not own yet, such as ones still in the TLS handshake.
type connTrackingListener struct {
	net.Listener

	mu     sync.Mutex
	conns  map[*trackedConn]struct{}
	closed bool
}

func newConnTrackingListener(l net.Listener) *connTrackingListener {
	return &connTrackingListener{Listener: l, conns: make(map[*trackedConn]struct{})}
}

func (l *connTrackingListener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	if l.closed {
		_ = conn.Close()

		return nil, net.ErrClosed
	}

	tc := &trackedConn{Conn: conn, owner: l}
	l.conns[tc] = struct{}{}

	return tc, nil
}

// closeAll closes the listener and every connection it accepted.
func (l *connTrackingListener) closeAll() {
	l.mu.Lock()
	l.closed = true
	conns := make([]*trackedConn, 0, len(l.conns))

	for c := range l.conns {
		conns = append(conns, c)
	}

	l.mu.Unlock()

	_ = l.Close()

	for _, c := range conns {
		_ = c.Close()
	}
}

func (l *connTrackingListener) forget(c *trackedConn) {
	l.mu.Lock()
	delete(l.conns, c)
	l.mu.Unlock()
}

// trackedConn removes itself from its listener when closed.
type trackedConn struct {
	net.Conn

	owner *connTrackingListener
	once  sync.Once
}

func (c *trackedConn) Close() error {
	err := c.Conn.Close()
	c.once.Do(func() { c.owner.forget(c) })

	return err
}

// grpcEnvelopeServer is the handler type of grpcServiceDesc.
type grpcEnvelopeServer interface {
	deliver(ctx context.Context, data []byte) error
}

// grpcServiceDesc describes the transport's service by hand, in place of the
// code protoc would generate.
var grpcServiceDesc = grpc.ServiceDesc{
	ServiceName: grpcServiceName,
	HandlerType: (*grpcEnvelopeServer)(nil),
	Methods: []grpc.MethodDesc{{
		MethodName: "Send",
		Handler:    grpcSendHandler,
	}},
	Streams:  []grpc.StreamDesc{},
	Metadata: "extensions/consensus/transport/grpc.go",
}

// grpcSendHandler decodes the request frame and hands it to the receiver. Its
// signature is the one gRPC requires of a unary method handler.
func grpcSendHandler(srv any, ctx context.Context, dec func(any) error, _ grpc.UnaryServerInterceptor) (any, error) {
	var in envelopeFrame
	if err := dec(&in); err != nil {
		return nil, err
	}

	server, ok := srv.(grpcEnvelopeServer)
	if !ok {
		return nil, status.Errorf(codes.Internal, "unexpected gRPC service implementation %T", srv)
	}

	if err := server.deliver(ctx, in.data); err != nil {
		return nil, err
	}

	return &envelopeFrame{}, nil
}

// grpcReceiver serves the Send method for one run of the transport. It holds
// that run's lifecycle context, so a restarted transport never mixes the two.
type grpcReceiver struct {
	transport *GRPCTransport
	ctx       context.Context //nolint:containedctx // lifecycle context of the server this receiver is registered on
}

// deliver decodes one envelope, checks its sender and queues it. The status
// code tells the sender why a message was refused.
func (r *grpcReceiver) deliver(ctx context.Context, data []byte) error {
	gt := r.transport
	remote := "unknown"

	p, hasPeer := peer.FromContext(ctx)
	if hasPeer && p.Addr != nil {
		remote = p.Addr.String()
	}

	msg, err := decodeMessage(data)
	if err != nil {
		if errors.Is(err, ErrProtocolVersion) {
			// The sender's Send returns this, naming both versions.
			gt.logger.Error("rejecting message from a peer on another wire protocol version",
				forge.F("remote", remote),
				forge.F("error", err),
			)

			return status.Error(codes.FailedPrecondition, err.Error())
		}

		gt.logger.Warn("rejecting malformed message",
			forge.F("remote", remote),
			forge.F("error", err),
		)

		return status.Error(codes.InvalidArgument, "invalid message")
	}

	if gt.config.RequirePeerIdentity {
		var state *tls.ConnectionState

		if hasPeer {
			if info, ok := p.AuthInfo.(credentials.TLSInfo); ok {
				state = &info.State
			}
		}

		if err := verifySender(state, msg.From); err != nil {
			gt.logger.Warn("rejecting message with unverified sender",
				forge.F("remote", remote),
				forge.F("error", err),
			)

			return status.Error(codes.PermissionDenied, "sender does not match client certificate")
		}
	}

	select {
	case gt.inbox <- msg:
		gt.logger.Debug("received gRPC message",
			forge.F("from", msg.From),
			forge.F("type", msg.Type),
		)

		return nil
	case <-r.ctx.Done():
		return status.Error(codes.Unavailable, "transport shutting down")
	default:
		return status.Error(codes.Unavailable, "inbox full")
	}
}

// envelopeFrame is the request and response message of the Send method: the
// encoded wire envelope, or nothing in a reply.
type envelopeFrame struct {
	data []byte
}

// envelopeCodec passes envelope bytes through gRPC unchanged. gRPC hands
// Unmarshal a slice it no longer uses, so the frame may keep it.
type envelopeCodec struct{}

func (envelopeCodec) Marshal(v any) ([]byte, error) {
	frame, ok := v.(*envelopeFrame)
	if !ok {
		return nil, fmt.Errorf("raft envelope codec cannot marshal %T", v)
	}

	return frame.data, nil
}

func (envelopeCodec) Unmarshal(data []byte, v any) error {
	frame, ok := v.(*envelopeFrame)
	if !ok {
		return fmt.Errorf("raft envelope codec cannot unmarshal into %T", v)
	}

	frame.data = data

	return nil
}

func (envelopeCodec) Name() string {
	return grpcCodecName
}
