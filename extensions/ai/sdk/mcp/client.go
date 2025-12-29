package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"
)

// Client is an MCP client that connects to MCP servers.
type Client struct {
	transport       Transport
	capabilities    Capability
	serverInfo      *Implementation
	requestID       int64
	pendingRequests map[int64]chan *Message
	subscriptions   map[string][]func(ResourceContent)
	onNotification  func(Method, json.RawMessage)
	mu              sync.RWMutex
	connected       bool
	ctx             context.Context
	cancel          context.CancelFunc
}

// Transport represents a communication transport for MCP.
type Transport interface {
	Send(msg *Message) error
	Receive() (*Message, error)
	Close() error
}

// ClientConfig configures the MCP client.
type ClientConfig struct {
	ClientInfo     Implementation
	Capabilities   Capability
	OnNotification func(Method, json.RawMessage)
}

// NewClient creates a new MCP client.
func NewClient(transport Transport, config ClientConfig) *Client {
	ctx, cancel := context.WithCancel(context.Background())
	return &Client{
		transport:       transport,
		capabilities:    config.Capabilities,
		pendingRequests: make(map[int64]chan *Message),
		subscriptions:   make(map[string][]func(ResourceContent)),
		onNotification:  config.OnNotification,
		ctx:             ctx,
		cancel:          cancel,
	}
}

// Connect initializes the connection with the MCP server.
func (c *Client) Connect(ctx context.Context, clientInfo Implementation) error {
	// Start message receiver
	go c.receiveLoop()

	// Send initialize request
	resp, err := c.request(ctx, MethodInitialize, InitializeRequest{
		ProtocolVersion: ProtocolVersion,
		Capabilities:    c.capabilities,
		ClientInfo:      clientInfo,
	})
	if err != nil {
		return fmt.Errorf("initialize failed: %w", err)
	}

	var initResp InitializeResponse
	if err := json.Unmarshal(resp.Result, &initResp); err != nil {
		return fmt.Errorf("parse initialize response: %w", err)
	}

	c.mu.Lock()
	c.serverInfo = &initResp.ServerInfo
	c.connected = true
	c.mu.Unlock()

	return nil
}

// Close closes the client connection.
func (c *Client) Close() error {
	c.cancel()
	c.mu.Lock()
	c.connected = false
	c.mu.Unlock()
	return c.transport.Close()
}

// IsConnected returns whether the client is connected.
func (c *Client) IsConnected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.connected
}

// ServerInfo returns the connected server's information.
func (c *Client) ServerInfo() *Implementation {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.serverInfo
}

// ListResources lists available resources.
func (c *Client) ListResources(ctx context.Context) ([]Resource, error) {
	resp, err := c.request(ctx, MethodListResources, ListRequest{})
	if err != nil {
		return nil, err
	}

	var result ListResourcesResponse
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, err
	}

	return result.Resources, nil
}

// ReadResource reads a resource by URI.
func (c *Client) ReadResource(ctx context.Context, uri string) ([]ResourceContent, error) {
	resp, err := c.request(ctx, MethodReadResource, ReadResourceRequest{URI: uri})
	if err != nil {
		return nil, err
	}

	var result ReadResourceResponse
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, err
	}

	return result.Contents, nil
}

// SubscribeResource subscribes to resource updates.
func (c *Client) SubscribeResource(ctx context.Context, uri string, handler func(ResourceContent)) error {
	_, err := c.request(ctx, MethodSubscribeResource, SubscribeRequest{URI: uri})
	if err != nil {
		return err
	}

	c.mu.Lock()
	c.subscriptions[uri] = append(c.subscriptions[uri], handler)
	c.mu.Unlock()

	return nil
}

// UnsubscribeResource unsubscribes from resource updates.
func (c *Client) UnsubscribeResource(ctx context.Context, uri string) error {
	_, err := c.request(ctx, MethodUnsubscribeResource, UnsubscribeRequest{URI: uri})
	if err != nil {
		return err
	}

	c.mu.Lock()
	delete(c.subscriptions, uri)
	c.mu.Unlock()

	return nil
}

// ListTools lists available tools.
func (c *Client) ListTools(ctx context.Context) ([]Tool, error) {
	resp, err := c.request(ctx, MethodListTools, ListRequest{})
	if err != nil {
		return nil, err
	}

	var result ListToolsResponse
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, err
	}

	return result.Tools, nil
}

// CallTool calls a tool with the given arguments.
func (c *Client) CallTool(ctx context.Context, name string, args map[string]any) (*CallToolResponse, error) {
	resp, err := c.request(ctx, MethodCallTool, CallToolRequest{
		Name:      name,
		Arguments: args,
	})
	if err != nil {
		return nil, err
	}

	var result CallToolResponse
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// ListPrompts lists available prompts.
func (c *Client) ListPrompts(ctx context.Context) ([]Prompt, error) {
	resp, err := c.request(ctx, MethodListPrompts, ListRequest{})
	if err != nil {
		return nil, err
	}

	var result ListPromptsResponse
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, err
	}

	return result.Prompts, nil
}

// GetPrompt retrieves a prompt by name with arguments.
func (c *Client) GetPrompt(ctx context.Context, name string, args map[string]any) (*GetPromptResponse, error) {
	resp, err := c.request(ctx, MethodGetPrompt, GetPromptRequest{
		Name:      name,
		Arguments: args,
	})
	if err != nil {
		return nil, err
	}

	var result GetPromptResponse
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// SetLogLevel sets the server's log level.
func (c *Client) SetLogLevel(ctx context.Context, level LogLevel) error {
	_, err := c.request(ctx, MethodSetLogLevel, SetLogLevelRequest{Level: level})
	return err
}

// request sends a request and waits for a response.
func (c *Client) request(ctx context.Context, method Method, params interface{}) (*Message, error) {
	id := atomic.AddInt64(&c.requestID, 1)

	msg, err := NewMessage(method, id, params)
	if err != nil {
		return nil, err
	}

	// Create response channel
	respChan := make(chan *Message, 1)
	c.mu.Lock()
	c.pendingRequests[id] = respChan
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		delete(c.pendingRequests, id)
		c.mu.Unlock()
	}()

	// Send request
	if err := c.transport.Send(msg); err != nil {
		return nil, err
	}

	// Wait for response
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case resp := <-respChan:
		if resp.Error != nil {
			return nil, fmt.Errorf("MCP error %d: %s", resp.Error.Code, resp.Error.Message)
		}
		return resp, nil
	}
}

// receiveLoop processes incoming messages.
func (c *Client) receiveLoop() {
	for {
		select {
		case <-c.ctx.Done():
			return
		default:
		}

		msg, err := c.transport.Receive()
		if err != nil {
			if err == io.EOF {
				return
			}
			continue
		}

		c.handleMessage(msg)
	}
}

// handleMessage processes an incoming message.
func (c *Client) handleMessage(msg *Message) {
	// Check if it's a response to a pending request
	if msg.ID != nil && len(msg.ID) > 0 {
		var id int64
		if err := json.Unmarshal(msg.ID, &id); err == nil {
			c.mu.RLock()
			respChan, ok := c.pendingRequests[id]
			c.mu.RUnlock()

			if ok {
				select {
				case respChan <- msg:
				default:
				}
				return
			}
		}
	}

	// Handle notifications
	if msg.Method != "" && c.onNotification != nil {
		c.onNotification(msg.Method, msg.Params)
	}

	// Handle resource update notifications
	if msg.Method == MethodResourceUpdated {
		var notif ResourceUpdatedNotification
		if err := json.Unmarshal(msg.Params, &notif); err == nil {
			c.handleResourceUpdate(notif.URI)
		}
	}
}

// handleResourceUpdate handles resource update notifications.
func (c *Client) handleResourceUpdate(uri string) {
	c.mu.RLock()
	handlers := c.subscriptions[uri]
	c.mu.RUnlock()

	if len(handlers) == 0 {
		return
	}

	// Read updated resource
	contents, err := c.ReadResource(c.ctx, uri)
	if err != nil {
		return
	}

	// Notify handlers
	for _, handler := range handlers {
		for _, content := range contents {
			handler(content)
		}
	}
}

// StdioTransport implements Transport over stdin/stdout.
type StdioTransport struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
	mu     sync.Mutex
}

// NewStdioTransport creates a new stdio transport.
func NewStdioTransport(cmd *exec.Cmd) (*StdioTransport, error) {
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	return &StdioTransport{
		cmd:    cmd,
		stdin:  stdin,
		stdout: bufio.NewReader(stdout),
	}, nil
}

// Send implements Transport.
func (t *StdioTransport) Send(msg *Message) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	// Write header
	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(data))
	if _, err := t.stdin.Write([]byte(header)); err != nil {
		return err
	}

	// Write body
	_, err = t.stdin.Write(data)
	return err
}

// Receive implements Transport.
func (t *StdioTransport) Receive() (*Message, error) {
	// Read header
	var contentLength int
	for {
		line, err := t.stdout.ReadString('\n')
		if err != nil {
			return nil, err
		}

		line = line[:len(line)-1] // Remove \n
		if len(line) > 0 && line[len(line)-1] == '\r' {
			line = line[:len(line)-1] // Remove \r
		}

		if line == "" {
			break
		}

		if _, err := fmt.Sscanf(line, "Content-Length: %d", &contentLength); err == nil {
			continue
		}
	}

	// Read body
	body := make([]byte, contentLength)
	if _, err := io.ReadFull(t.stdout, body); err != nil {
		return nil, err
	}

	var msg Message
	if err := json.Unmarshal(body, &msg); err != nil {
		return nil, err
	}

	return &msg, nil
}

// Close implements Transport.
func (t *StdioTransport) Close() error {
	t.stdin.Close()
	return t.cmd.Wait()
}

// MCPClientManager manages multiple MCP clients.
type MCPClientManager struct {
	clients map[string]*Client
	mu      sync.RWMutex
}

// NewMCPClientManager creates a new client manager.
func NewMCPClientManager() *MCPClientManager {
	return &MCPClientManager{
		clients: make(map[string]*Client),
	}
}

// AddClient adds a client with the given name.
func (m *MCPClientManager) AddClient(name string, client *Client) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.clients[name] = client
}

// GetClient returns a client by name.
func (m *MCPClientManager) GetClient(name string) (*Client, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	client, ok := m.clients[name]
	return client, ok
}

// RemoveClient removes a client by name.
func (m *MCPClientManager) RemoveClient(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if client, ok := m.clients[name]; ok {
		client.Close()
		delete(m.clients, name)
	}
}

// ListClients returns all client names.
func (m *MCPClientManager) ListClients() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	names := make([]string, 0, len(m.clients))
	for name := range m.clients {
		names = append(names, name)
	}
	return names
}

// Close closes all clients.
func (m *MCPClientManager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, client := range m.clients {
		client.Close()
	}
	m.clients = make(map[string]*Client)
	return nil
}

// AggregatedResources returns resources from all connected clients.
func (m *MCPClientManager) AggregatedResources(ctx context.Context) (map[string][]Resource, error) {
	m.mu.RLock()
	clients := make(map[string]*Client)
	for k, v := range m.clients {
		clients[k] = v
	}
	m.mu.RUnlock()

	results := make(map[string][]Resource)
	var mu sync.Mutex
	var wg sync.WaitGroup

	for name, client := range clients {
		wg.Add(1)
		go func(n string, c *Client) {
			defer wg.Done()
			resources, err := c.ListResources(ctx)
			if err != nil {
				return
			}
			mu.Lock()
			results[n] = resources
			mu.Unlock()
		}(name, client)
	}

	wg.Wait()
	return results, nil
}

// AggregatedTools returns tools from all connected clients.
func (m *MCPClientManager) AggregatedTools(ctx context.Context) (map[string][]Tool, error) {
	m.mu.RLock()
	clients := make(map[string]*Client)
	for k, v := range m.clients {
		clients[k] = v
	}
	m.mu.RUnlock()

	results := make(map[string][]Tool)
	var mu sync.Mutex
	var wg sync.WaitGroup

	for name, client := range clients {
		wg.Add(1)
		go func(n string, c *Client) {
			defer wg.Done()
			tools, err := c.ListTools(ctx)
			if err != nil {
				return
			}
			mu.Lock()
			results[n] = tools
			mu.Unlock()
		}(name, client)
	}

	wg.Wait()
	return results, nil
}

// ConnectStdio connects to an MCP server via stdio.
func ConnectStdio(ctx context.Context, command string, args []string, clientInfo Implementation) (*Client, error) {
	cmd := exec.CommandContext(ctx, command, args...)

	transport, err := NewStdioTransport(cmd)
	if err != nil {
		return nil, err
	}

	client := NewClient(transport, ClientConfig{
		ClientInfo: clientInfo,
	})

	if err := client.Connect(ctx, clientInfo); err != nil {
		transport.Close()
		return nil, err
	}

	return client, nil
}

// MCPToolAdapter adapts MCP tools for use with the SDK tool system.
type MCPToolAdapter struct {
	client   *Client
	toolName string
}

// NewMCPToolAdapter creates a new MCP tool adapter.
func NewMCPToolAdapter(client *Client, toolName string) *MCPToolAdapter {
	return &MCPToolAdapter{
		client:   client,
		toolName: toolName,
	}
}

// Call calls the MCP tool.
func (a *MCPToolAdapter) Call(ctx context.Context, args map[string]any) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	resp, err := a.client.CallTool(ctx, a.toolName, args)
	if err != nil {
		return "", err
	}

	if resp.IsError {
		if len(resp.Content) > 0 && resp.Content[0].Text != "" {
			return "", fmt.Errorf("tool error: %s", resp.Content[0].Text)
		}
		return "", fmt.Errorf("tool execution failed")
	}

	// Extract text content
	var result string
	for _, content := range resp.Content {
		if content.Type == "text" {
			result += content.Text
		}
	}

	return result, nil
}
