package streaming

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/logger"
	"github.com/xraph/forge/pkg/streaming/middleware"
)

// TestConfig contains configuration for streaming tests
type TestConfig struct {
	ServerAddr        string        `yaml:"server_addr" default:"localhost:8080"`
	TestTimeout       time.Duration `yaml:"test_timeout" default:"30s"`
	ConnectionTimeout time.Duration `yaml:"connection_timeout" default:"5s"`
	MessageTimeout    time.Duration `yaml:"message_timeout" default:"1s"`
	MaxConnections    int           `yaml:"max_connections" default:"100"`
	MaxMessages       int           `yaml:"max_messages" default:"1000"`
	EnableLogging     bool          `yaml:"enable_logging" default:"true"`
	EnableMetrics     bool          `yaml:"enable_metrics" default:"true"`
	CleanupTimeout    time.Duration `yaml:"cleanup_timeout" default:"5s"`
	RetryAttempts     int           `yaml:"retry_attempts" default:"3"`
	RetryDelay        time.Duration `yaml:"retry_delay" default:"100ms"`
}

// DefaultTestConfig returns default test configuration
func DefaultTestConfig() TestConfig {
	return TestConfig{
		ServerAddr:        "localhost:8080",
		TestTimeout:       30 * time.Second,
		ConnectionTimeout: 5 * time.Second,
		MessageTimeout:    1 * time.Second,
		MaxConnections:    100,
		MaxMessages:       1000,
		EnableLogging:     true,
		EnableMetrics:     true,
		CleanupTimeout:    5 * time.Second,
		RetryAttempts:     3,
		RetryDelay:        100 * time.Millisecond,
	}
}

// TestServer provides a test server for streaming tests
type TestServer struct {
	server           *httptest.Server
	streamingManager StreamingManager
	config           TestConfig
	logger           common.Logger
	metrics          common.Metrics
	mu               sync.RWMutex
	running          bool
	connections      map[string]*TestConnection
	rooms            map[string]*TestRoom
	cleanup          []func()
}

// NewTestServer creates a new test server
func NewTestServer(config TestConfig, l common.Logger, metrics common.Metrics) *TestServer {
	if l == nil {
		l = logger.NewNoopLogger()
	}
	if metrics == nil {
		metrics = &testMetrics{}
	}

	return &TestServer{
		config:      config,
		logger:      l,
		metrics:     metrics,
		connections: make(map[string]*TestConnection),
		rooms:       make(map[string]*TestRoom),
		cleanup:     make([]func(), 0),
	}
}

// Start starts the test server
func (ts *TestServer) Start() error {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	if ts.running {
		return fmt.Errorf("test server already running")
	}

	// Create streaming manager
	managerConfig := DefaultStreamingConfig()
	ts.streamingManager = NewStreamingManager(managerConfig, ts.logger, ts.metrics)

	// Create HTTP server
	mux := http.NewServeMux()

	// Add WebSocket endpoint
	mux.HandleFunc("/ws", ts.handleWebSocket)

	// Add SSE endpoint
	mux.HandleFunc("/sse", ts.handleSSE)

	// Add metrics endpoint
	mux.HandleFunc("/metrics", ts.handleMetrics)

	// Add health endpoint
	mux.HandleFunc("/health", ts.handleHealth)

	ts.server = httptest.NewServer(mux)
	ts.running = true

	return nil
}

// Stop stops the test server
func (ts *TestServer) Stop() error {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	if !ts.running {
		return fmt.Errorf("test server not running")
	}

	// Stop all connections
	for _, conn := range ts.connections {
		conn.Close()
	}

	// Run cleanup functions
	for _, cleanup := range ts.cleanup {
		cleanup()
	}

	// Stop server
	ts.server.Close()
	ts.running = false

	return nil
}

// URL returns the server URL
func (ts *TestServer) URL() string {
	if ts.server == nil {
		return ""
	}
	return ts.server.URL
}

// WebSocketURL returns the WebSocket URL
func (ts *TestServer) WebSocketURL() string {
	if ts.server == nil {
		return ""
	}
	return "ws" + ts.server.URL[4:] + "/ws"
}

// SSEURL returns the SSE URL
func (ts *TestServer) SSEURL() string {
	if ts.server == nil {
		return ""
	}
	return ts.server.URL + "/sse"
}

// handleWebSocket handles WebSocket connections
func (ts *TestServer) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		ts.logger.Error("failed to upgrade WebSocket connection", logger.Error(err))
		return
	}

	// Create test connection
	testConn := NewTestConnection("websocket", conn, ts.logger)

	ts.mu.Lock()
	ts.connections[testConn.ID()] = testConn
	ts.mu.Unlock()

	// Handle connection
	go ts.handleConnection(testConn)
}

// handleSSE handles Server-Sent Events
func (ts *TestServer) handleSSE(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	// Create test connection
	testConn := NewTestSSEConnection(w, ts.logger)

	ts.mu.Lock()
	ts.connections[testConn.ID()] = testConn
	ts.mu.Unlock()

	// Handle connection
	go ts.handleConnection(testConn)
}

// handleMetrics handles metrics endpoint
func (ts *TestServer) handleMetrics(w http.ResponseWriter, r *http.Request) {
	// Return test metrics
	fmt.Fprintf(w, "# Test metrics\n")
	fmt.Fprintf(w, "test_connections_total %d\n", len(ts.connections))
	fmt.Fprintf(w, "test_rooms_total %d\n", len(ts.rooms))
}

// handleHealth handles health endpoint
func (ts *TestServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `{"status": "healthy", "connections": %d, "rooms": %d}`, len(ts.connections), len(ts.rooms))
}

// handleConnection handles a test connection
func (ts *TestServer) handleConnection(conn *TestConnection) {
	defer func() {
		ts.mu.Lock()
		delete(ts.connections, conn.ID())
		ts.mu.Unlock()
		conn.Close()
	}()

	for {
		select {
		case message := <-conn.Messages():
			ts.handleMessage(conn, message)
		case <-conn.Done():
			return
		}
	}
}

// handleMessage handles a message from a connection
func (ts *TestServer) handleMessage(conn *TestConnection, message *Message) {
	// Echo the message back
	response := &Message{
		ID:        fmt.Sprintf("echo-%s", message.ID),
		Type:      MessageTypeText,
		RoomID:    message.RoomID,
		From:      "server",
		To:        message.From,
		Data:      map[string]interface{}{"echo": message.Data},
		Timestamp: time.Now(),
	}

	conn.Send(response)
}

// AddCleanup adds a cleanup function
func (ts *TestServer) AddCleanup(cleanup func()) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.cleanup = append(ts.cleanup, cleanup)
}

// GetConnection returns a test connection by ID
func (ts *TestServer) GetConnection(id string) (*TestConnection, bool) {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	conn, exists := ts.connections[id]
	return conn, exists
}

// GetConnections returns all test connections
func (ts *TestServer) GetConnections() []*TestConnection {
	ts.mu.RLock()
	defer ts.mu.RUnlock()

	connections := make([]*TestConnection, 0, len(ts.connections))
	for _, conn := range ts.connections {
		connections = append(connections, conn)
	}
	return connections
}

// TestConnection represents a test connection
type TestConnection struct {
	id        string
	protocol  string
	wsConn    *websocket.Conn
	sseWriter http.ResponseWriter
	messages  chan *Message
	done      chan struct{}
	logger    common.Logger
	mu        sync.RWMutex
	closed    bool
	metadata  map[string]interface{}
	stats     ConnectionStats
}

// NewTestConnection creates a new test WebSocket connection
func NewTestConnection(protocol string, wsConn *websocket.Conn, logger common.Logger) *TestConnection {
	conn := &TestConnection{
		id:       fmt.Sprintf("test-conn-%d", time.Now().UnixNano()),
		protocol: protocol,
		wsConn:   wsConn,
		messages: make(chan *Message, 100),
		done:     make(chan struct{}),
		logger:   logger,
		metadata: make(map[string]interface{}),
		stats:    ConnectionStats{ConnectedAt: time.Now()},
	}

	// Start reading messages
	go conn.readMessages()

	return conn
}

// NewTestSSEConnection creates a new test SSE connection
func NewTestSSEConnection(w http.ResponseWriter, logger common.Logger) *TestConnection {
	conn := &TestConnection{
		id:        fmt.Sprintf("test-sse-%d", time.Now().UnixNano()),
		protocol:  "sse",
		sseWriter: w,
		messages:  make(chan *Message, 100),
		done:      make(chan struct{}),
		logger:    logger,
		metadata:  make(map[string]interface{}),
		stats:     ConnectionStats{ConnectedAt: time.Now()},
	}

	return conn
}

// ID returns the connection ID
func (tc *TestConnection) ID() string {
	return tc.id
}

// Protocol returns the connection protocol
func (tc *TestConnection) Protocol() string {
	return tc.protocol
}

// Send sends a message
func (tc *TestConnection) Send(message *Message) error {
	tc.mu.RLock()
	defer tc.mu.RUnlock()

	if tc.closed {
		return fmt.Errorf("connection closed")
	}

	switch tc.protocol {
	case "websocket":
		return tc.wsConn.WriteJSON(message)
	case "sse":
		data := fmt.Sprintf("data: %s\n\n", message.Data)
		_, err := tc.sseWriter.Write([]byte(data))
		return err
	default:
		return fmt.Errorf("unsupported protocol: %s", tc.protocol)
	}
}

// Messages returns the messages channel
func (tc *TestConnection) Messages() <-chan *Message {
	return tc.messages
}

// Done returns the done channel
func (tc *TestConnection) Done() <-chan struct{} {
	return tc.done
}

// Close closes the connection
func (tc *TestConnection) Close() error {
	tc.mu.Lock()
	defer tc.mu.Unlock()

	if tc.closed {
		return nil
	}

	tc.closed = true
	close(tc.done)

	if tc.wsConn != nil {
		return tc.wsConn.Close()
	}

	return nil
}

// readMessages reads messages from WebSocket
func (tc *TestConnection) readMessages() {
	if tc.wsConn == nil {
		return
	}

	defer close(tc.messages)

	for {
		var message Message
		err := tc.wsConn.ReadJSON(&message)
		if err != nil {
			if tc.logger != nil {
				tc.logger.Error("failed to read message", logger.Error(err))
			}
			return
		}

		select {
		case tc.messages <- &message:
		case <-tc.done:
			return
		}
	}
}

// SetMetadata sets connection metadata
func (tc *TestConnection) SetMetadata(key string, value interface{}) {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	tc.metadata[key] = value
}

// GetMetadata gets connection metadata
func (tc *TestConnection) GetMetadata(key string) (interface{}, bool) {
	tc.mu.RLock()
	defer tc.mu.RUnlock()
	value, exists := tc.metadata[key]
	return value, exists
}

// GetStats returns connection statistics
func (tc *TestConnection) GetStats() ConnectionStats {
	tc.mu.RLock()
	defer tc.mu.RUnlock()
	return tc.stats
}

// TestRoom represents a test room
type TestRoom struct {
	id          string
	connections map[string]*TestConnection
	messages    []*Message
	logger      common.Logger
	mu          sync.RWMutex
}

// NewTestRoom creates a new test room
func NewTestRoom(id string, logger common.Logger) *TestRoom {
	return &TestRoom{
		id:          id,
		connections: make(map[string]*TestConnection),
		messages:    make([]*Message, 0),
		logger:      logger,
	}
}

// ID returns the room ID
func (tr *TestRoom) ID() string {
	return tr.id
}

// AddConnection adds a connection to the room
func (tr *TestRoom) AddConnection(conn *TestConnection) {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	tr.connections[conn.ID()] = conn
}

// RemoveConnection removes a connection from the room
func (tr *TestRoom) RemoveConnection(connID string) {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	delete(tr.connections, connID)
}

// Broadcast broadcasts a message to all connections in the room
func (tr *TestRoom) Broadcast(message *Message) {
	tr.mu.RLock()
	defer tr.mu.RUnlock()

	tr.messages = append(tr.messages, message)

	for _, conn := range tr.connections {
		go func(c *TestConnection) {
			if err := c.Send(message); err != nil && tr.logger != nil {
				tr.logger.Error("failed to send message to connection",
					logger.String("connection_id", c.ID()),
					logger.Error(err),
				)
			}
		}(conn)
	}
}

// GetConnections returns all connections in the room
func (tr *TestRoom) GetConnections() []*TestConnection {
	tr.mu.RLock()
	defer tr.mu.RUnlock()

	connections := make([]*TestConnection, 0, len(tr.connections))
	for _, conn := range tr.connections {
		connections = append(connections, conn)
	}
	return connections
}

// GetMessages returns all messages sent to the room
func (tr *TestRoom) GetMessages() []*Message {
	tr.mu.RLock()
	defer tr.mu.RUnlock()

	messages := make([]*Message, len(tr.messages))
	copy(messages, tr.messages)
	return messages
}

// TestClient represents a test client
type TestClient struct {
	id         string
	connection *TestConnection
	logger     common.Logger
	config     TestConfig
	received   []*Message
	mu         sync.RWMutex
}

// NewTestClient creates a new test client
func NewTestClient(id string, logger common.Logger, config TestConfig) *TestClient {
	return &TestClient{
		id:       id,
		logger:   logger,
		config:   config,
		received: make([]*Message, 0),
	}
}

// Connect connects to a test server
func (tc *TestClient) Connect(serverURL string) error {
	wsURL := "ws" + serverURL[4:] + "/ws"

	dialer := websocket.Dialer{
		HandshakeTimeout: tc.config.ConnectionTimeout,
	}

	conn, _, err := dialer.Dial(wsURL, nil)
	if err != nil {
		return fmt.Errorf("failed to connect to WebSocket: %w", err)
	}

	tc.connection = NewTestConnection("websocket", conn, tc.logger)

	// Start receiving messages
	go tc.receiveMessages()

	return nil
}

// Send sends a message
func (tc *TestClient) Send(message *Message) error {
	if tc.connection == nil {
		return fmt.Errorf("not connected")
	}
	return tc.connection.Send(message)
}

// Receive receives a message with timeout
func (tc *TestClient) Receive(timeout time.Duration) (*Message, error) {
	if tc.connection == nil {
		return nil, fmt.Errorf("not connected")
	}

	select {
	case message := <-tc.connection.Messages():
		return message, nil
	case <-time.After(timeout):
		return nil, fmt.Errorf("timeout waiting for message")
	}
}

// GetReceived returns all received messages
func (tc *TestClient) GetReceived() []*Message {
	tc.mu.RLock()
	defer tc.mu.RUnlock()

	messages := make([]*Message, len(tc.received))
	copy(messages, tc.received)
	return messages
}

// Close closes the client connection
func (tc *TestClient) Close() error {
	if tc.connection == nil {
		return nil
	}
	return tc.connection.Close()
}

// receiveMessages receives messages in the background
func (tc *TestClient) receiveMessages() {
	for message := range tc.connection.Messages() {
		tc.mu.Lock()
		tc.received = append(tc.received, message)
		tc.mu.Unlock()
	}
}

// TestHelper provides helper functions for testing
type TestHelper struct {
	t      *testing.T
	config TestConfig
	logger common.Logger
}

// NewTestHelper creates a new test helper
func NewTestHelper(t *testing.T, config TestConfig, logger common.Logger) *TestHelper {
	return &TestHelper{
		t:      t,
		config: config,
		logger: logger,
	}
}

// AssertMessageReceived asserts that a message was received
func (th *TestHelper) AssertMessageReceived(client *TestClient, expectedType MessageType, timeout time.Duration) *Message {
	th.t.Helper()

	message, err := client.Receive(timeout)
	if err != nil {
		th.t.Fatalf("expected message but got error: %v", err)
	}

	if message.Type != expectedType {
		th.t.Fatalf("expected message type %s but got %s", expectedType, message.Type)
	}

	return message
}

// AssertConnectionCount asserts the number of connections
func (th *TestHelper) AssertConnectionCount(server *TestServer, expected int) {
	th.t.Helper()

	connections := server.GetConnections()
	if len(connections) != expected {
		th.t.Fatalf("expected %d connections but got %d", expected, len(connections))
	}
}

// AssertMessageCount asserts the number of messages received
func (th *TestHelper) AssertMessageCount(client *TestClient, expected int) {
	th.t.Helper()

	received := client.GetReceived()
	if len(received) != expected {
		th.t.Fatalf("expected %d messages but got %d", expected, len(received))
	}
}

// CreateTestMessage creates a test message
func (th *TestHelper) CreateTestMessage(messageType MessageType, roomID, from, to string, data interface{}) *Message {
	return &Message{
		ID:        fmt.Sprintf("test-msg-%d", time.Now().UnixNano()),
		Type:      messageType,
		RoomID:    roomID,
		From:      from,
		To:        to,
		Data:      data,
		Timestamp: time.Now(),
		Priority:  PriorityNormal,
	}
}

// WaitForCondition waits for a condition to be true
func (th *TestHelper) WaitForCondition(condition func() bool, timeout time.Duration, message string) {
	th.t.Helper()

	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	timeoutCh := time.After(timeout)

	for {
		select {
		case <-ticker.C:
			if condition() {
				return
			}
		case <-timeoutCh:
			th.t.Fatalf("timeout waiting for condition: %s", message)
		}
	}
}

// LoadTestConfig contains configuration for load testing
type LoadTestConfig struct {
	NumClients      int           `yaml:"num_clients" default:"10"`
	NumMessages     int           `yaml:"num_messages" default:"100"`
	MessageInterval time.Duration `yaml:"message_interval" default:"100ms"`
	TestDuration    time.Duration `yaml:"test_duration" default:"30s"`
	RampUpDuration  time.Duration `yaml:"ramp_up_duration" default:"5s"`
	Concurrent      bool          `yaml:"concurrent" default:"true"`
}

// LoadTester provides load testing functionality
type LoadTester struct {
	config  LoadTestConfig
	logger  common.Logger
	metrics common.Metrics
	clients []*TestClient
	results *LoadTestResults
	mu      sync.RWMutex
}

// LoadTestResults contains load test results
type LoadTestResults struct {
	TotalClients     int                      `json:"total_clients"`
	TotalMessages    int                      `json:"total_messages"`
	MessagesSent     int                      `json:"messages_sent"`
	MessagesReceived int                      `json:"messages_received"`
	Errors           int                      `json:"errors"`
	Duration         time.Duration            `json:"duration"`
	Throughput       float64                  `json:"throughput"`
	ErrorRate        float64                  `json:"error_rate"`
	AverageLatency   time.Duration            `json:"average_latency"`
	MinLatency       time.Duration            `json:"min_latency"`
	MaxLatency       time.Duration            `json:"max_latency"`
	ConnectionErrors int                      `json:"connection_errors"`
	MessageErrors    int                      `json:"message_errors"`
	Percentiles      map[string]time.Duration `json:"percentiles"`
}

// NewLoadTester creates a new load tester
func NewLoadTester(config LoadTestConfig, logger common.Logger, metrics common.Metrics) *LoadTester {
	return &LoadTester{
		config:  config,
		logger:  logger,
		metrics: metrics,
		clients: make([]*TestClient, 0),
		results: &LoadTestResults{},
	}
}

// Run runs the load test
func (lt *LoadTester) Run(serverURL string) (*LoadTestResults, error) {
	startTime := time.Now()

	// Create clients
	for i := 0; i < lt.config.NumClients; i++ {
		client := NewTestClient(fmt.Sprintf("load-client-%d", i), lt.logger, DefaultTestConfig())
		lt.clients = append(lt.clients, client)
	}

	// Connect clients
	var wg sync.WaitGroup
	errors := make(chan error, lt.config.NumClients)

	for _, client := range lt.clients {
		wg.Add(1)
		go func(c *TestClient) {
			defer wg.Done()
			if err := c.Connect(serverURL); err != nil {
				errors <- err
			}
		}(client)
	}

	wg.Wait()
	close(errors)

	// Count connection errors
	connectionErrors := 0
	for err := range errors {
		if err != nil {
			connectionErrors++
			if lt.logger != nil {
				lt.logger.Error("client connection failed", logger.Error(err))
			}
		}
	}

	// Run test
	if lt.config.Concurrent {
		lt.runConcurrentTest()
	} else {
		lt.runSequentialTest()
	}

	// Calculate results
	duration := time.Since(startTime)
	lt.results.Duration = duration
	lt.results.TotalClients = len(lt.clients)
	lt.results.ConnectionErrors = connectionErrors
	lt.results.Throughput = float64(lt.results.MessagesSent) / duration.Seconds()

	if lt.results.MessagesSent > 0 {
		lt.results.ErrorRate = float64(lt.results.Errors) / float64(lt.results.MessagesSent)
	}

	// Cleanup
	for _, client := range lt.clients {
		client.Close()
	}

	return lt.results, nil
}

// runConcurrentTest runs concurrent load test
func (lt *LoadTester) runConcurrentTest() {
	var wg sync.WaitGroup

	for i, client := range lt.clients {
		wg.Add(1)
		go func(clientID int, c *TestClient) {
			defer wg.Done()
			lt.runClientTest(clientID, c)
		}(i, client)
	}

	wg.Wait()
}

// runSequentialTest runs sequential load test
func (lt *LoadTester) runSequentialTest() {
	for i, client := range lt.clients {
		lt.runClientTest(i, client)
	}
}

// runClientTest runs test for a single client
func (lt *LoadTester) runClientTest(clientID int, client *TestClient) {
	for i := 0; i < lt.config.NumMessages; i++ {
		message := &Message{
			ID:        fmt.Sprintf("load-msg-%d-%d", clientID, i),
			Type:      MessageTypeText,
			RoomID:    "load-test-room",
			From:      client.id,
			Data:      fmt.Sprintf("Load test message %d from client %d", i, clientID),
			Timestamp: time.Now(),
			Priority:  PriorityNormal,
		}

		startTime := time.Now()
		err := client.Send(message)
		latency := time.Since(startTime)

		lt.mu.Lock()
		if err != nil {
			lt.results.Errors++
			lt.results.MessageErrors++
		} else {
			lt.results.MessagesSent++
		}

		// Update latency stats
		if lt.results.AverageLatency == 0 {
			lt.results.AverageLatency = latency
			lt.results.MinLatency = latency
			lt.results.MaxLatency = latency
		} else {
			lt.results.AverageLatency = (lt.results.AverageLatency + latency) / 2
			if latency < lt.results.MinLatency {
				lt.results.MinLatency = latency
			}
			if latency > lt.results.MaxLatency {
				lt.results.MaxLatency = latency
			}
		}
		lt.mu.Unlock()

		time.Sleep(lt.config.MessageInterval)
	}
}

// GetResults returns the load test results
func (lt *LoadTester) GetResults() *LoadTestResults {
	lt.mu.RLock()
	defer lt.mu.RUnlock()
	return lt.results
}

// Mock implementations for testing

// testMetrics implements common.Metrics for testing
type testMetrics struct{}

func (tm *testMetrics) Name() string {
	// TODO implement me
	panic("implement me")
}

func (tm *testMetrics) Dependencies() []string {
	// TODO implement me
	panic("implement me")
}

func (tm *testMetrics) OnStart(ctx context.Context) error {
	// TODO implement me
	panic("implement me")
}

func (tm *testMetrics) OnStop(ctx context.Context) error {
	// TODO implement me
	panic("implement me")
}

func (tm *testMetrics) OnHealthCheck(ctx context.Context) error {
	// TODO implement me
	panic("implement me")
}

func (tm *testMetrics) RegisterCollector(collector common.CustomCollector) error {
	// TODO implement me
	panic("implement me")
}

func (tm *testMetrics) UnregisterCollector(name string) error {
	// TODO implement me
	panic("implement me")
}

func (tm *testMetrics) GetCollectors() []common.CustomCollector {
	// TODO implement me
	panic("implement me")
}

func (tm *testMetrics) GetMetrics() map[string]interface{} {
	// TODO implement me
	panic("implement me")
}

func (tm *testMetrics) GetMetricsByType(metricType common.MetricType) map[string]interface{} {
	// TODO implement me
	panic("implement me")
}

func (tm *testMetrics) GetMetricsByTag(tagKey, tagValue string) map[string]interface{} {
	// TODO implement me
	panic("implement me")
}

func (tm *testMetrics) Export(format common.ExportFormat) ([]byte, error) {
	// TODO implement me
	panic("implement me")
}

func (tm *testMetrics) ExportToFile(format common.ExportFormat, filename string) error {
	// TODO implement me
	panic("implement me")
}

func (tm *testMetrics) Reset() error {
	// TODO implement me
	panic("implement me")
}

func (tm *testMetrics) ResetMetric(name string) error {
	// TODO implement me
	panic("implement me")
}

func (tm *testMetrics) GetStats() common.CollectorStats {
	// TODO implement me
	panic("implement me")
}

func (tm *testMetrics) Counter(name string, tags ...string) common.Counter { return &testCounter{} }
func (tm *testMetrics) Gauge(name string, tags ...string) common.Gauge     { return &testGauge{} }
func (tm *testMetrics) Histogram(name string, tags ...string) common.Histogram {
	return &testHistogram{}
}
func (tm *testMetrics) Timer(name string, tags ...string) common.Timer { return &testTimer{} }

// testCounter implements core.Counter for testing
type testCounter struct{}

func (tc *testCounter) Dec() {
}

func (tc *testCounter) Get() float64 {
	return 0.0
}

func (tc *testCounter) Reset() {
}

func (tc *testCounter) Inc()              {}
func (tc *testCounter) Add(value float64) {}

// testGauge implements core.Gauge for testing
type testGauge struct{}

func (tg *testGauge) Get() float64 {
	return 0.0
}

func (tg *testGauge) Reset() {
}

func (tg *testGauge) Set(value float64) {}
func (tg *testGauge) Inc()              {}
func (tg *testGauge) Dec()              {}
func (tg *testGauge) Add(value float64) {}

// testHistogram implements core.Histogram for testing
type testHistogram struct{}

func (th *testHistogram) GetBuckets() map[float64]uint64 {
	return map[float64]uint64{}
}

func (th *testHistogram) GetCount() uint64 {
	return 0
}

func (th *testHistogram) GetSum() float64 {
	return 0.0
}

func (th *testHistogram) GetMean() float64 {
	return 0.0
}

func (th *testHistogram) GetPercentile(percentile float64) float64 {
	return 0.0
}

func (th *testHistogram) Reset() {
}

func (th *testHistogram) Observe(value float64) {}

// testTimer implements core.Timer for testing
type testTimer struct{}

func (tt *testTimer) GetCount() uint64 {
	return 0
}

func (tt *testTimer) GetMean() time.Duration {
	return 0
}

func (tt *testTimer) GetPercentile(percentile float64) time.Duration {
	// TODO implement me
	panic("implement me")
}

func (tt *testTimer) GetMin() time.Duration {
	// TODO implement me
	panic("implement me")
}

func (tt *testTimer) GetMax() time.Duration {
	// TODO implement me
	panic("implement me")
}

func (tt *testTimer) Reset() {
	// TODO implement me
	panic("implement me")
}

func (tt *testTimer) Record(duration time.Duration) {}
func (tt *testTimer) Time() func()                  { return func() {} }

// Utility functions for testing

// CreateTestStreamingManager creates a streaming manager for testing
func CreateTestStreamingManager(logger common.Logger, metrics common.Metrics) StreamingManager {
	config := DefaultStreamingConfig()
	config.EnableWebSocket = true
	config.EnableSSE = true
	config.EnableLongPolling = false

	return NewStreamingManager(config, logger, metrics)
}

// CreateTestAuthProvider creates an authentication provider for testing
func CreateTestAuthProvider(logger common.Logger) *SimpleAuthenticationProvider {
	provider := NewSimpleAuthenticationProvider(logger)

	// Add test users
	provider.AddUser("test-token-1", &AuthenticatedUser{
		ID:          "user-1",
		Username:    "testuser1",
		Email:       "test1@example.com",
		Roles:       []string{"user"},
		Permissions: []string{"connect", "send", "receive"},
		Token:       "test-token-1",
		ExpiresAt:   time.Now().Add(24 * time.Hour),
	})

	provider.AddUser("test-token-2", &AuthenticatedUser{
		ID:          "user-2",
		Username:    "testuser2",
		Email:       "test2@example.com",
		Roles:       []string{"user", "moderator"},
		Permissions: []string{"connect", "send", "receive", "moderate"},
		Token:       "test-token-2",
		ExpiresAt:   time.Now().Add(24 * time.Hour),
	})

	return provider
}

// CreateTestMiddleware creates test middleware
func CreateTestMiddleware(logger common.Logger, metrics common.Metrics) (*middleware.AuthMiddleware, *middleware.LoggingMiddleware, *middleware.MetricsMiddleware) {
	// Create auth provider
	authProvider := CreateTestAuthProvider(logger)
	authManager := NewAuthenticationManager(authProvider, DefaultAuthenticationConfig(), logger, metrics)

	// Create middleware
	authMiddleware := middleware.NewAuthMiddleware(authManager, middleware.DefaultAuthMiddlewareConfig(), logger, metrics)
	loggingMiddleware := middleware.NewLoggingMiddleware(logger, metrics, middleware.DefaultLoggingConfig())
	metricsMiddleware := middleware.NewMetricsMiddleware(metrics, logger, middleware.DefaultMetricsConfig())

	return authMiddleware, loggingMiddleware, metricsMiddleware
}
