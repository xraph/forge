package llm

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/xraph/forge/internal/errors"
)

// StreamingClient handles streaming responses from LLM providers.
type StreamingClient struct {
	client   *http.Client
	buffers  map[string]*StreamBuffer
	handlers map[string]StreamHandler
	mu       sync.RWMutex
}

// StreamHandler handles streaming events.
type StreamHandler interface {
	OnData(event StreamEvent) error
	OnError(err error)
	OnComplete()
}

// StreamEvent represents a streaming event.
type StreamEvent struct {
	Type      string         `json:"type"` // chat, completion, embedding, error, done
	ID        string         `json:"id"`
	Timestamp time.Time      `json:"timestamp"`
	Data      any            `json:"data"`
	Metadata  map[string]any `json:"metadata"`
	RequestID string         `json:"request_id"`
	Provider  string         `json:"provider"`
}

// StreamBuffer buffers streaming data.
type StreamBuffer struct {
	data      []byte
	events    []StreamEvent
	completed bool
	error     error
	mu        sync.RWMutex
}

// ChatStreamClient handles chat streaming.
type ChatStreamClient struct {
	*StreamingClient

	session *ChatSession
}

// CompletionStreamClient handles completion streaming.
type CompletionStreamClient struct {
	*StreamingClient

	currentText strings.Builder
}

// NewStreamingClient creates a new streaming client.
func NewStreamingClient() *StreamingClient {
	return &StreamingClient{
		client: &http.Client{
			Timeout: 60 * time.Second,
		},
		buffers:  make(map[string]*StreamBuffer),
		handlers: make(map[string]StreamHandler),
	}
}

// NewChatStreamClient creates a new chat streaming client.
func NewChatStreamClient(session *ChatSession) *ChatStreamClient {
	return &ChatStreamClient{
		StreamingClient: NewStreamingClient(),
		session:         session,
	}
}

// NewCompletionStreamClient creates a new completion streaming client.
func NewCompletionStreamClient() *CompletionStreamClient {
	return &CompletionStreamClient{
		StreamingClient: NewStreamingClient(),
		currentText:     strings.Builder{},
	}
}

// RegisterHandler registers a stream handler.
func (sc *StreamingClient) RegisterHandler(id string, handler StreamHandler) {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	sc.handlers[id] = handler
}

// UnregisterHandler unregisters a stream handler.
func (sc *StreamingClient) UnregisterHandler(id string) {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	delete(sc.handlers, id)
}

// StartStream starts a streaming request.
func (sc *StreamingClient) StartStream(ctx context.Context, request *http.Request, requestID string) error {
	sc.mu.Lock()

	buffer := &StreamBuffer{
		data:   make([]byte, 0),
		events: make([]StreamEvent, 0),
	}
	sc.buffers[requestID] = buffer
	sc.mu.Unlock()

	response, err := sc.client.Do(request.WithContext(ctx))
	if err != nil {
		sc.notifyError(requestID, err)

		return err
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		err := fmt.Errorf("HTTP %d: %s", response.StatusCode, response.Status)
		sc.notifyError(requestID, err)

		return err
	}

	return sc.processStream(ctx, response.Body, requestID)
}

// processStream processes the streaming response.
func (sc *StreamingClient) processStream(ctx context.Context, reader io.Reader, requestID string) error {
	scanner := bufio.NewScanner(reader)

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		line := scanner.Text()
		if line == "" {
			continue
		}

		// Handle Server-Sent Events format
		if after, ok := strings.CutPrefix(line, "data: "); ok {
			data := after

			if data == "[DONE]" {
				sc.notifyComplete(requestID)

				break
			}

			event, err := sc.parseStreamData(data, requestID)
			if err != nil {
				sc.notifyError(requestID, err)

				continue
			}

			sc.addEvent(requestID, event)
			sc.notifyData(requestID, event)
		}
	}

	if err := scanner.Err(); err != nil {
		sc.notifyError(requestID, err)

		return err
	}

	return nil
}

// parseStreamData parses streaming data into an event.
func (sc *StreamingClient) parseStreamData(data, requestID string) (StreamEvent, error) {
	var rawEvent map[string]any
	if err := json.Unmarshal([]byte(data), &rawEvent); err != nil {
		return StreamEvent{}, fmt.Errorf("failed to parse stream data: %w", err)
	}

	event := StreamEvent{
		Timestamp: time.Now(),
		RequestID: requestID,
		Metadata:  make(map[string]any),
	}

	// Determine event type based on content
	if choices, ok := rawEvent["choices"].([]any); ok && len(choices) > 0 {
		choice := choices[0].(map[string]any)

		if delta, ok := choice["delta"].(map[string]any); ok {
			if content, ok := delta["content"].(string); ok {
				event.Type = "chat"
				event.Data = ChatStreamEvent{
					Type: "message",
					Choices: []ChatChoice{{
						Delta: &ChatMessage{
							Role:    "assistant",
							Content: content,
						},
					}},
				}
			} else if toolCalls, ok := delta["tool_calls"]; ok {
				event.Type = "chat"
				event.Data = ChatStreamEvent{
					Type: "tool_call",
					Choices: []ChatChoice{{
						Delta: &ChatMessage{
							ToolCalls: parseToolCalls(toolCalls),
						},
					}},
				}
			}
		} else if text, ok := choice["text"].(string); ok {
			event.Type = "completion"
			event.Data = CompletionStreamEvent{
				Type: "text",
				Choices: []CompletionChoice{{
					Text: text,
				}},
			}
		}
	}

	// Extract metadata
	if id, ok := rawEvent["id"].(string); ok {
		event.ID = id
	}

	if model, ok := rawEvent["model"].(string); ok {
		event.Metadata["model"] = model
	}

	if created, ok := rawEvent["created"].(float64); ok {
		event.Metadata["created"] = int64(created)
	}

	return event, nil
}

// parseToolCalls parses tool calls from raw data.
func parseToolCalls(rawToolCalls any) []ToolCall {
	if toolCallsArray, ok := rawToolCalls.([]any); ok {
		toolCalls := make([]ToolCall, 0, len(toolCallsArray))

		for _, rawToolCall := range toolCallsArray {
			if toolCallMap, ok := rawToolCall.(map[string]any); ok {
				toolCall := ToolCall{}

				if id, ok := toolCallMap["id"].(string); ok {
					toolCall.ID = id
				}

				if tcType, ok := toolCallMap["type"].(string); ok {
					toolCall.Type = tcType
				}

				if function, ok := toolCallMap["function"].(map[string]any); ok {
					toolCall.Function = &FunctionCall{}
					if name, ok := function["name"].(string); ok {
						toolCall.Function.Name = name
					}

					if args, ok := function["arguments"].(string); ok {
						toolCall.Function.Arguments = args
					}
				}

				toolCalls = append(toolCalls, toolCall)
			}
		}

		return toolCalls
	}

	return nil
}

// addEvent adds an event to the buffer.
func (sc *StreamingClient) addEvent(requestID string, event StreamEvent) {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	if buffer, exists := sc.buffers[requestID]; exists {
		buffer.mu.Lock()
		buffer.events = append(buffer.events, event)
		buffer.mu.Unlock()
	}
}

// notifyData notifies handlers of new data.
func (sc *StreamingClient) notifyData(requestID string, event StreamEvent) {
	sc.mu.RLock()

	handlers := make(map[string]StreamHandler)
	maps.Copy(handlers, sc.handlers)

	sc.mu.RUnlock()

	for _, handler := range handlers {
		if err := handler.OnData(event); err != nil {
			handler.OnError(err)
		}
	}
}

// notifyError notifies handlers of errors.
func (sc *StreamingClient) notifyError(requestID string, err error) {
	sc.mu.Lock()

	if buffer, exists := sc.buffers[requestID]; exists {
		buffer.mu.Lock()
		buffer.error = err
		buffer.mu.Unlock()
	}

	sc.mu.Unlock()

	sc.mu.RLock()

	handlers := make(map[string]StreamHandler)
	maps.Copy(handlers, sc.handlers)

	sc.mu.RUnlock()

	for _, handler := range handlers {
		handler.OnError(err)
	}
}

// notifyComplete notifies handlers of completion.
func (sc *StreamingClient) notifyComplete(requestID string) {
	sc.mu.Lock()

	if buffer, exists := sc.buffers[requestID]; exists {
		buffer.mu.Lock()
		buffer.completed = true
		buffer.mu.Unlock()
	}

	sc.mu.Unlock()

	sc.mu.RLock()

	handlers := make(map[string]StreamHandler)
	maps.Copy(handlers, sc.handlers)

	sc.mu.RUnlock()

	for _, handler := range handlers {
		handler.OnComplete()
	}
}

// GetEvents returns all events for a request.
func (sc *StreamingClient) GetEvents(requestID string) ([]StreamEvent, error) {
	sc.mu.RLock()
	buffer, exists := sc.buffers[requestID]
	sc.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("no buffer found for request %s", requestID)
	}

	buffer.mu.RLock()
	defer buffer.mu.RUnlock()

	events := make([]StreamEvent, len(buffer.events))
	copy(events, buffer.events)

	return events, nil
}

// IsComplete checks if streaming is complete.
func (sc *StreamingClient) IsComplete(requestID string) bool {
	sc.mu.RLock()
	buffer, exists := sc.buffers[requestID]
	sc.mu.RUnlock()

	if !exists {
		return false
	}

	buffer.mu.RLock()
	defer buffer.mu.RUnlock()

	return buffer.completed
}

// GetError returns any error for a request.
func (sc *StreamingClient) GetError(requestID string) error {
	sc.mu.RLock()
	buffer, exists := sc.buffers[requestID]
	sc.mu.RUnlock()

	if !exists {
		return fmt.Errorf("no buffer found for request %s", requestID)
	}

	buffer.mu.RLock()
	defer buffer.mu.RUnlock()

	return buffer.error
}

// CleanupBuffer removes a request buffer.
func (sc *StreamingClient) CleanupBuffer(requestID string) {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	delete(sc.buffers, requestID)
}

// StreamChatWithSession streams chat with session management.
func (csc *ChatStreamClient) StreamChatWithSession(ctx context.Context, message string, handler ChatStreamHandler) error {
	// Add user message to session
	csc.session.AddMessage(ChatMessage{
		Role:    "user",
		Content: message,
	})

	// Create streaming request
	request := csc.session.ToRequest()
	request.Stream = true

	// Set up response accumulation
	responseBuilder := strings.Builder{}

	// Create wrapper handler
	wrapperHandler := &chatSessionHandler{
		originalHandler: handler,
		session:         csc.session,
		responseBuilder: &responseBuilder,
	}

	requestID := request.RequestID
	if requestID == "" {
		requestID = fmt.Sprintf("chat_%d", time.Now().UnixNano())
	}

	csc.RegisterHandler(requestID, wrapperHandler)
	defer csc.UnregisterHandler(requestID)

	// Convert to HTTP request (this would be implemented by specific providers)
	httpReq, err := csc.createHTTPRequest(ctx, request)
	if err != nil {
		return err
	}

	return csc.StartStream(ctx, httpReq, requestID)
}

// StreamCompletion streams text completion.
func (csc *CompletionStreamClient) StreamCompletion(ctx context.Context, request CompletionRequest, handler CompletionStreamHandler) error {
	request.Stream = true

	// Create wrapper handler
	wrapperHandler := &completionHandler{
		originalHandler: handler,
		textBuilder:     &csc.currentText,
	}

	requestID := request.RequestID
	if requestID == "" {
		requestID = fmt.Sprintf("completion_%d", time.Now().UnixNano())
	}

	csc.RegisterHandler(requestID, wrapperHandler)
	defer csc.UnregisterHandler(requestID)

	// Convert to HTTP request (this would be implemented by specific providers)
	httpReq, err := csc.createHTTPRequest(ctx, request)
	if err != nil {
		return err
	}

	return csc.StartStream(ctx, httpReq, requestID)
}

// createHTTPRequest creates an HTTP request for streaming (placeholder implementation).
func (csc *ChatStreamClient) createHTTPRequest(ctx context.Context, request ChatRequest) (*http.Request, error) {
	// This would be implemented by specific providers (OpenAI, Anthropic, etc.)
	return nil, errors.New("createHTTPRequest not implemented for chat")
}

func (csc *CompletionStreamClient) createHTTPRequest(ctx context.Context, request CompletionRequest) (*http.Request, error) {
	// This would be implemented by specific providers (OpenAI, Anthropic, etc.)
	return nil, errors.New("createHTTPRequest not implemented for completion")
}

// chatSessionHandler handles chat streaming with session management.
type chatSessionHandler struct {
	originalHandler ChatStreamHandler
	session         *ChatSession
	responseBuilder *strings.Builder
}

func (h *chatSessionHandler) OnData(event StreamEvent) error {
	if chatEvent, ok := event.Data.(ChatStreamEvent); ok {
		// Accumulate assistant response
		if len(chatEvent.Choices) > 0 && chatEvent.Choices[0].Delta != nil {
			if chatEvent.Choices[0].Delta.Content != "" {
				h.responseBuilder.WriteString(chatEvent.Choices[0].Delta.Content)
			}
		}

		return h.originalHandler(chatEvent)
	}

	return nil
}

func (h *chatSessionHandler) OnError(err error) {
	// Could implement error handling for chat
}

func (h *chatSessionHandler) OnComplete() {
	// Add assistant response to session
	if h.responseBuilder.Len() > 0 {
		h.session.AddMessage(ChatMessage{
			Role:    "assistant",
			Content: h.responseBuilder.String(),
		})
	}
}

// completionHandler handles completion streaming.
type completionHandler struct {
	originalHandler CompletionStreamHandler
	textBuilder     *strings.Builder
}

func (h *completionHandler) OnData(event StreamEvent) error {
	if completionEvent, ok := event.Data.(CompletionStreamEvent); ok {
		// Accumulate text
		if len(completionEvent.Choices) > 0 {
			h.textBuilder.WriteString(completionEvent.Choices[0].Text)
		}

		return h.originalHandler(completionEvent)
	}

	return nil
}

func (h *completionHandler) OnError(err error) {
	// Could implement error handling for completion
}

func (h *completionHandler) OnComplete() {
	// Completion finished
}

// StreamingManager manages multiple streaming requests.
type StreamingManager struct {
	clients map[string]*StreamingClient
	mu      sync.RWMutex
}

// NewStreamingManager creates a new streaming manager.
func NewStreamingManager() *StreamingManager {
	return &StreamingManager{
		clients: make(map[string]*StreamingClient),
	}
}

// CreateChatStream creates a new chat streaming client.
func (sm *StreamingManager) CreateChatStream(sessionID string, session *ChatSession) *ChatStreamClient {
	client := NewChatStreamClient(session)

	sm.mu.Lock()
	sm.clients[sessionID] = client.StreamingClient
	sm.mu.Unlock()

	return client
}

// CreateCompletionStream creates a new completion streaming client.
func (sm *StreamingManager) CreateCompletionStream(requestID string) *CompletionStreamClient {
	client := NewCompletionStreamClient()

	sm.mu.Lock()
	sm.clients[requestID] = client.StreamingClient
	sm.mu.Unlock()

	return client
}

// GetClient returns a streaming client by ID.
func (sm *StreamingManager) GetClient(id string) (*StreamingClient, bool) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	client, exists := sm.clients[id]

	return client, exists
}

// RemoveClient removes a streaming client.
func (sm *StreamingManager) RemoveClient(id string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	delete(sm.clients, id)
}

// CleanupExpired removes expired streaming clients.
func (sm *StreamingManager) CleanupExpired(maxAge time.Duration) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	// In a real implementation, you'd track creation times and clean up old clients
	// For now, this is a placeholder
}

// GetActiveStreams returns the number of active streams.
func (sm *StreamingManager) GetActiveStreams() int {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	return len(sm.clients)
}
