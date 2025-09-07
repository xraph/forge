package transport

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge/pkg/common"
)

// DefaultTransportManager implements TransportManager
type DefaultTransportManager struct {
	factories  map[string]TransportFactory
	transports map[string]Transport
	mu         sync.RWMutex
}

// NewTransportManager creates a new transport manager
func NewTransportManager() TransportManager {
	return &DefaultTransportManager{
		factories:  make(map[string]TransportFactory),
		transports: make(map[string]Transport),
	}
}

// RegisterFactory registers a transport factory
func (tm *DefaultTransportManager) RegisterFactory(factory TransportFactory) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if factory == nil {
		return fmt.Errorf("factory cannot be nil")
	}

	name := factory.Name()
	if name == "" {
		return fmt.Errorf("factory name cannot be empty")
	}

	if _, exists := tm.factories[name]; exists {
		return fmt.Errorf("factory with name %s already registered", name)
	}

	tm.factories[name] = factory
	return nil
}

// CreateTransport creates a transport instance
func (tm *DefaultTransportManager) CreateTransport(config TransportConfig) (Transport, error) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if config.Type == "" {
		return nil, fmt.Errorf("transport type is required")
	}

	factory, exists := tm.factories[config.Type]
	if !exists {
		return nil, fmt.Errorf("no factory registered for transport type: %s", config.Type)
	}

	// Validate configuration
	if err := factory.ValidateConfig(config); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	// Create transport
	transport, err := factory.Create(config, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create transport: %w", err)
	}

	// Store transport for management
	transportKey := fmt.Sprintf("%s_%s_%d", config.Type, config.Address, config.Port)
	tm.transports[transportKey] = transport

	return transport, nil
}

// GetTransport returns a transport by name
func (tm *DefaultTransportManager) GetTransport(name string) (Transport, error) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	transport, exists := tm.transports[name]
	if !exists {
		return nil, fmt.Errorf("transport not found: %s", name)
	}

	return transport, nil
}

// GetFactories returns all registered factories
func (tm *DefaultTransportManager) GetFactories() map[string]TransportFactory {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	factories := make(map[string]TransportFactory)
	for name, factory := range tm.factories {
		factories[name] = factory
	}

	return factories
}

// Close closes all transports
func (tm *DefaultTransportManager) Close(ctx context.Context) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	var errors []error

	for name, transport := range tm.transports {
		if err := transport.Close(); err != nil {
			errors = append(errors, fmt.Errorf("failed to close transport %s: %w", name, err))
		}
	}

	tm.transports = make(map[string]Transport)

	if len(errors) > 0 {
		return fmt.Errorf("errors closing transports: %v", errors)
	}

	return nil
}

// RegisterDefaultFactories registers the default transport factories
func RegisterDefaultFactories(manager TransportManager, logger common.Logger, metrics common.Metrics) error {
	// Register HTTP factory
	httpFactory := NewHTTPTransportFactory(logger, metrics)
	if err := manager.RegisterFactory(httpFactory); err != nil {
		return fmt.Errorf("failed to register HTTP factory: %w", err)
	}

	// Register gRPC factory
	grpcFactory := NewGRPCTransportFactory(logger, metrics)
	if err := manager.RegisterFactory(grpcFactory); err != nil {
		return fmt.Errorf("failed to register gRPC factory: %w", err)
	}

	// Register TCP factory
	tcpFactory := NewTCPTransportFactory(logger, metrics)
	if err := manager.RegisterFactory(tcpFactory); err != nil {
		return fmt.Errorf("failed to register TCP factory: %w", err)
	}

	return nil
}

// DefaultConnectionPool implements ConnectionPool
type DefaultConnectionPool struct {
	connections map[string]Connection
	stats       ConnectionPoolStats
	mu          sync.RWMutex
}

// NewConnectionPool creates a new connection pool
func NewConnectionPool() ConnectionPool {
	return &DefaultConnectionPool{
		connections: make(map[string]Connection),
		stats:       ConnectionPoolStats{},
	}
}

// Get gets a connection to a peer
func (cp *DefaultConnectionPool) Get(ctx context.Context, peerID string) (Connection, error) {
	cp.mu.RLock()
	conn, exists := cp.connections[peerID]
	cp.mu.RUnlock()

	if exists && conn.IsConnected() {
		cp.mu.Lock()
		cp.stats.HitRate = float64(cp.stats.HitRate+1) / float64(cp.stats.HitRate+cp.stats.MissRate+1)
		cp.mu.Unlock()
		return conn, nil
	}

	cp.mu.Lock()
	cp.stats.MissRate = float64(cp.stats.MissRate+1) / float64(cp.stats.HitRate+cp.stats.MissRate+1)
	cp.mu.Unlock()

	return nil, fmt.Errorf("connection not found or disconnected: %s", peerID)
}

// Put returns a connection to the pool
func (cp *DefaultConnectionPool) Put(peerID string, conn Connection) error {
	cp.mu.Lock()
	defer cp.mu.Unlock()

	if conn == nil {
		return fmt.Errorf("connection cannot be nil")
	}

	cp.connections[peerID] = conn
	cp.stats.TotalConnections = len(cp.connections)

	if conn.IsConnected() {
		cp.stats.ActiveConnections++
	} else {
		cp.stats.IdleConnections++
	}

	return nil
}

// Remove removes a connection from the pool
func (cp *DefaultConnectionPool) Remove(peerID string) error {
	cp.mu.Lock()
	defer cp.mu.Unlock()

	conn, exists := cp.connections[peerID]
	if !exists {
		return fmt.Errorf("connection not found: %s", peerID)
	}

	if conn.IsConnected() {
		cp.stats.ActiveConnections--
	} else {
		cp.stats.IdleConnections--
	}

	delete(cp.connections, peerID)
	cp.stats.TotalConnections = len(cp.connections)

	return conn.Close()
}

// Close closes all connections
func (cp *DefaultConnectionPool) Close() error {
	cp.mu.Lock()
	defer cp.mu.Unlock()

	var errors []error

	for peerID, conn := range cp.connections {
		if err := conn.Close(); err != nil {
			errors = append(errors, fmt.Errorf("failed to close connection to %s: %w", peerID, err))
		}
	}

	cp.connections = make(map[string]Connection)
	cp.stats = ConnectionPoolStats{}

	if len(errors) > 0 {
		return fmt.Errorf("errors closing connections: %v", errors)
	}

	return nil
}

// Size returns the number of connections
func (cp *DefaultConnectionPool) Size() int {
	cp.mu.RLock()
	defer cp.mu.RUnlock()
	return len(cp.connections)
}

// Stats returns pool statistics
func (cp *DefaultConnectionPool) Stats() ConnectionPoolStats {
	cp.mu.RLock()
	defer cp.mu.RUnlock()
	return cp.stats
}

// JSONSerializer implements Serializer for JSON
type JSONSerializer struct{}

// NewJSONSerializer creates a new JSON serializer
func NewJSONSerializer() Serializer {
	return &JSONSerializer{}
}

// Serialize serializes a message to JSON
func (s *JSONSerializer) Serialize(message Message) ([]byte, error) {
	return json.Marshal(message)
}

// Deserialize deserializes a message from JSON
func (s *JSONSerializer) Deserialize(data []byte) (Message, error) {
	var message Message
	err := json.Unmarshal(data, &message)
	return message, err
}

// ContentType returns the content type
func (s *JSONSerializer) ContentType() string {
	return "application/json"
}

// JSONCodec implements Codec for JSON
type JSONCodec struct{}

// NewJSONCodec creates a new JSON codec
func NewJSONCodec() Codec {
	return &JSONCodec{}
}

// Encode encodes data to JSON
func (c *JSONCodec) Encode(data interface{}) ([]byte, error) {
	return json.Marshal(data)
}

// Decode decodes data from JSON
func (c *JSONCodec) Decode(data []byte, target interface{}) error {
	return json.Unmarshal(data, target)
}

// ContentType returns the content type
func (c *JSONCodec) ContentType() string {
	return "application/json"
}

// CreateTransportWithDefaults Helper function to create a transport with all default factories registered
func CreateTransportWithDefaults(config TransportConfig, logger common.Logger, metrics common.Metrics) (Transport, error) {
	manager := NewTransportManager()

	// Register default factories
	if err := RegisterDefaultFactories(manager, logger, metrics); err != nil {
		return nil, fmt.Errorf("failed to register default factories: %w", err)
	}

	// Create transport
	return manager.CreateTransport(config)
}

// NewTransportConfig Helper function to create a transport configuration
func NewTransportConfig(transportType, address string, port int) TransportConfig {
	return TransportConfig{
		Type:           transportType,
		Address:        address,
		Port:           port,
		Timeout:        5 * time.Second,
		MaxMessageSize: 1024 * 1024, // 1MB
		BufferSize:     1000,
		MaxConnections: 100,
		KeepAlive:      true,
		KeepAliveTime:  30 * time.Second,
		Options:        make(map[string]interface{}),
	}
}

// NewTLSTransportConfig Helper function to create a TLS transport configuration
func NewTLSTransportConfig(transportType, address string, port int, certFile, keyFile string) TransportConfig {
	config := NewTransportConfig(transportType, address, port)
	config.EnableTLS = true
	config.TLSConfig = &TLSConfig{
		CertFile: certFile,
		KeyFile:  keyFile,
	}
	return config
}
