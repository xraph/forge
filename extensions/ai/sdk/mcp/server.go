package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
)

// Server is an MCP server implementation.
type Server struct {
	info         Implementation
	capabilities Capability
	resources    *ResourceRegistry
	tools        *ToolRegistry
	prompts      *PromptRegistry
	transport    ServerTransport
	logLevel     LogLevel
	onLog        func(LogNotification)
	mu           sync.RWMutex
	initialized  bool
	ctx          context.Context
	cancel       context.CancelFunc
}

// ServerTransport represents a server-side transport.
type ServerTransport interface {
	Send(msg *Message) error
	Receive() (*Message, error)
	Close() error
}

// ServerConfig configures the MCP server.
type ServerConfig struct {
	Info         Implementation
	Capabilities Capability
	OnLog        func(LogNotification)
}

// NewServer creates a new MCP server.
func NewServer(transport ServerTransport, config ServerConfig) *Server {
	ctx, cancel := context.WithCancel(context.Background())

	return &Server{
		info:         config.Info,
		capabilities: config.Capabilities,
		resources:    NewResourceRegistry(),
		tools:        NewToolRegistry(),
		prompts:      NewPromptRegistry(),
		transport:    transport,
		logLevel:     LogLevelInfo,
		onLog:        config.OnLog,
		ctx:          ctx,
		cancel:       cancel,
	}
}

// Start starts the server message processing loop.
func (s *Server) Start() error {
	for {
		select {
		case <-s.ctx.Done():
			return nil
		default:
		}

		msg, err := s.transport.Receive()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}

			continue
		}

		resp := s.handleMessage(msg)
		if resp != nil {
			if err := s.transport.Send(resp); err != nil {
				continue
			}
		}
	}
}

// Stop stops the server.
func (s *Server) Stop() error {
	s.cancel()

	return s.transport.Close()
}

// handleMessage processes an incoming message.
func (s *Server) handleMessage(msg *Message) *Message {
	switch msg.Method {
	case MethodInitialize:
		return s.handleInitialize(msg)
	case MethodShutdown:
		return s.handleShutdown(msg)
	case MethodListResources:
		return s.handleListResources(msg)
	case MethodReadResource:
		return s.handleReadResource(msg)
	case MethodSubscribeResource:
		return s.handleSubscribeResource(msg)
	case MethodUnsubscribeResource:
		return s.handleUnsubscribeResource(msg)
	case MethodListTools:
		return s.handleListTools(msg)
	case MethodCallTool:
		return s.handleCallTool(msg)
	case MethodListPrompts:
		return s.handleListPrompts(msg)
	case MethodGetPrompt:
		return s.handleGetPrompt(msg)
	case MethodSetLogLevel:
		return s.handleSetLogLevel(msg)
	default:
		return NewErrorResponse(msg.ID, ErrorCodeMethodNotFound, "method not found")
	}
}

func (s *Server) handleInitialize(msg *Message) *Message {
	var req InitializeRequest
	if err := json.Unmarshal(msg.Params, &req); err != nil {
		return NewErrorResponse(msg.ID, ErrorCodeInvalidParams, err.Error())
	}

	s.mu.Lock()
	s.initialized = true
	s.mu.Unlock()

	resp, _ := NewResponse(msg.ID, InitializeResponse{
		ProtocolVersion: ProtocolVersion,
		Capabilities:    s.capabilities,
		ServerInfo:      s.info,
	})

	return resp
}

func (s *Server) handleShutdown(msg *Message) *Message {
	s.cancel()

	resp, _ := NewResponse(msg.ID, nil)

	return resp
}

func (s *Server) handleListResources(msg *Message) *Message {
	resources := s.resources.List()
	resp, _ := NewResponse(msg.ID, ListResourcesResponse{
		Resources: resources,
	})

	return resp
}

func (s *Server) handleReadResource(msg *Message) *Message {
	var req ReadResourceRequest
	if err := json.Unmarshal(msg.Params, &req); err != nil {
		return NewErrorResponse(msg.ID, ErrorCodeInvalidParams, err.Error())
	}

	content, err := s.resources.Read(s.ctx, req.URI)
	if err != nil {
		return NewErrorResponse(msg.ID, ErrorCodeResourceNotFound, err.Error())
	}

	resp, _ := NewResponse(msg.ID, ReadResourceResponse{
		Contents: []ResourceContent{*content},
	})

	return resp
}

func (s *Server) handleSubscribeResource(msg *Message) *Message {
	var req SubscribeRequest
	if err := json.Unmarshal(msg.Params, &req); err != nil {
		return NewErrorResponse(msg.ID, ErrorCodeInvalidParams, err.Error())
	}

	if err := s.resources.Subscribe(req.URI); err != nil {
		return NewErrorResponse(msg.ID, ErrorCodeResourceNotFound, err.Error())
	}

	resp, _ := NewResponse(msg.ID, nil)

	return resp
}

func (s *Server) handleUnsubscribeResource(msg *Message) *Message {
	var req UnsubscribeRequest
	if err := json.Unmarshal(msg.Params, &req); err != nil {
		return NewErrorResponse(msg.ID, ErrorCodeInvalidParams, err.Error())
	}

	s.resources.Unsubscribe(req.URI)

	resp, _ := NewResponse(msg.ID, nil)

	return resp
}

func (s *Server) handleListTools(msg *Message) *Message {
	tools := s.tools.List()
	resp, _ := NewResponse(msg.ID, ListToolsResponse{
		Tools: tools,
	})

	return resp
}

func (s *Server) handleCallTool(msg *Message) *Message {
	var req CallToolRequest
	if err := json.Unmarshal(msg.Params, &req); err != nil {
		return NewErrorResponse(msg.ID, ErrorCodeInvalidParams, err.Error())
	}

	result, err := s.tools.Call(s.ctx, req.Name, req.Arguments)
	if err != nil {
		resp, _ := NewResponse(msg.ID, CallToolResponse{
			Content: []ToolResultContent{{
				Type: "text",
				Text: err.Error(),
			}},
			IsError: true,
		})

		return resp
	}

	resp, _ := NewResponse(msg.ID, CallToolResponse{
		Content: result,
	})

	return resp
}

func (s *Server) handleListPrompts(msg *Message) *Message {
	prompts := s.prompts.List()
	resp, _ := NewResponse(msg.ID, ListPromptsResponse{
		Prompts: prompts,
	})

	return resp
}

func (s *Server) handleGetPrompt(msg *Message) *Message {
	var req GetPromptRequest
	if err := json.Unmarshal(msg.Params, &req); err != nil {
		return NewErrorResponse(msg.ID, ErrorCodeInvalidParams, err.Error())
	}

	result, err := s.prompts.Get(s.ctx, req.Name, req.Arguments)
	if err != nil {
		return NewErrorResponse(msg.ID, ErrorCodeInvalidParams, err.Error())
	}

	resp, _ := NewResponse(msg.ID, result)

	return resp
}

func (s *Server) handleSetLogLevel(msg *Message) *Message {
	var req SetLogLevelRequest
	if err := json.Unmarshal(msg.Params, &req); err != nil {
		return NewErrorResponse(msg.ID, ErrorCodeInvalidParams, err.Error())
	}

	s.mu.Lock()
	s.logLevel = req.Level
	s.mu.Unlock()

	resp, _ := NewResponse(msg.ID, nil)

	return resp
}

// RegisterResource registers a resource with the server.
func (s *Server) RegisterResource(resource Resource, handler ResourceHandler) {
	s.resources.Register(resource, handler)
}

// RegisterTool registers a tool with the server.
func (s *Server) RegisterTool(tool Tool, handler ToolHandler) {
	s.tools.Register(tool, handler)
}

// RegisterPrompt registers a prompt with the server.
func (s *Server) RegisterPrompt(prompt Prompt, handler PromptHandler) {
	s.prompts.Register(prompt, handler)
}

// NotifyResourceUpdated sends a resource updated notification.
func (s *Server) NotifyResourceUpdated(uri string) error {
	msg, err := NewMessage(MethodResourceUpdated, nil, ResourceUpdatedNotification{URI: uri})
	if err != nil {
		return err
	}

	return s.transport.Send(msg)
}

// NotifyResourceListChanged sends a resource list changed notification.
func (s *Server) NotifyResourceListChanged() error {
	msg, err := NewMessage(MethodResourceListChanged, nil, nil)
	if err != nil {
		return err
	}

	return s.transport.Send(msg)
}

// NotifyToolListChanged sends a tool list changed notification.
func (s *Server) NotifyToolListChanged() error {
	msg, err := NewMessage(MethodToolListChanged, nil, nil)
	if err != nil {
		return err
	}

	return s.transport.Send(msg)
}

// Log sends a log notification.
func (s *Server) Log(level LogLevel, logger string, data any) error {
	s.mu.RLock()
	currentLevel := s.logLevel
	s.mu.RUnlock()

	// Check if level is at or above current log level
	if !shouldLog(level, currentLevel) {
		return nil
	}

	notif := LogNotification{
		Level:  level,
		Logger: logger,
		Data:   data,
	}

	if s.onLog != nil {
		s.onLog(notif)
	}

	msg, err := NewMessage(MethodLog, nil, notif)
	if err != nil {
		return err
	}

	return s.transport.Send(msg)
}

func shouldLog(level, threshold LogLevel) bool {
	levels := map[LogLevel]int{
		LogLevelDebug:     0,
		LogLevelInfo:      1,
		LogLevelNotice:    2,
		LogLevelWarning:   3,
		LogLevelError:     4,
		LogLevelCritical:  5,
		LogLevelAlert:     6,
		LogLevelEmergency: 7,
	}

	return levels[level] >= levels[threshold]
}

// StdioServerTransport implements ServerTransport over stdin/stdout.
type StdioServerTransport struct {
	reader *bufio.Reader
	writer io.Writer
	mu     sync.Mutex
}

// NewStdioServerTransport creates a new stdio server transport.
func NewStdioServerTransport() *StdioServerTransport {
	return &StdioServerTransport{
		reader: bufio.NewReader(os.Stdin),
		writer: os.Stdout,
	}
}

// Send implements ServerTransport.
func (t *StdioServerTransport) Send(msg *Message) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	// Write header
	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(data))
	if _, err := t.writer.Write([]byte(header)); err != nil {
		return err
	}

	// Write body
	_, err = t.writer.Write(data)

	return err
}

// Receive implements ServerTransport.
func (t *StdioServerTransport) Receive() (*Message, error) {
	// Read header
	var contentLength int

	for {
		line, err := t.reader.ReadString('\n')
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
	if _, err := io.ReadFull(t.reader, body); err != nil {
		return nil, err
	}

	var msg Message
	if err := json.Unmarshal(body, &msg); err != nil {
		return nil, err
	}

	return &msg, nil
}

// Close implements ServerTransport.
func (t *StdioServerTransport) Close() error {
	return nil // Stdin/stdout don't need to be closed
}

// ServeStdio starts an MCP server over stdio.
func ServeStdio(config ServerConfig) error {
	transport := NewStdioServerTransport()
	server := NewServer(transport, config)

	return server.Start()
}
