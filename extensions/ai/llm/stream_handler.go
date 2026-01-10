package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/google/uuid"
	"github.com/xraph/forge/errors"
)

// ClientStreamHandler manages a single streaming session and transforms
// ChatStreamEvent to typed ClientStreamEvent for client consumption.
type ClientStreamHandler struct {
	// ExecutionID uniquely identifies this streaming session
	ExecutionID string

	// tokenIndex is a monotonic counter for React keys
	// Increments with every delta event
	tokenIndex int64

	// currentBlockType tracks whether we're in thinking, text, or tool_use
	currentBlockType BlockType

	// currentToolID tracks the current tool being called
	currentToolID string

	// model and provider information
	model    string
	provider string

	// events channel for sending typed events
	events chan ClientStreamEvent

	// done channel for signaling completion
	done chan struct{}

	// isDone tracks if the stream has been completed (to prevent double-close)
	isDone bool

	// ctx for cancellation
	ctx    context.Context
	cancel context.CancelFunc

	// mu protects state changes
	mu sync.Mutex

	// blockStarted tracks if we've sent a start event for the current block
	blockStarted bool

	// thinkingStarted tracks if we've sent thinking_start
	thinkingStarted bool

	// contentStarted tracks if we've sent content_start
	contentStarted bool

	// onEvent is a callback for each typed event
	onEvent func(ClientStreamEvent) error
}

// ClientStreamHandlerConfig contains configuration for creating a ClientStreamHandler.
type ClientStreamHandlerConfig struct {
	Model    string
	Provider string
	OnEvent  func(ClientStreamEvent) error
	Context  context.Context
}

// NewClientStreamHandler creates a new ClientStreamHandler for managing a streaming session.
func NewClientStreamHandler(config ClientStreamHandlerConfig) *ClientStreamHandler {
	ctx := config.Context
	if ctx == nil {
		ctx = context.Background()
	}

	ctx, cancel := context.WithCancel(ctx)

	return &ClientStreamHandler{
		ExecutionID: uuid.New().String(),
		model:       config.Model,
		provider:    config.Provider,
		events:      make(chan ClientStreamEvent, 100),
		done:        make(chan struct{}),
		ctx:         ctx,
		cancel:      cancel,
		onEvent:     config.OnEvent,
	}
}

// GetExecutionID returns the execution ID for this stream.
func (h *ClientStreamHandler) GetExecutionID() string {
	return h.ExecutionID
}

// GetIndex returns the current token index.
func (h *ClientStreamHandler) GetIndex() int64 {
	return atomic.LoadInt64(&h.tokenIndex)
}

// nextIndex increments and returns the next token index.
func (h *ClientStreamHandler) nextIndex() int64 {
	return atomic.AddInt64(&h.tokenIndex, 1) - 1
}

// Cancel cancels the streaming session.
func (h *ClientStreamHandler) Cancel() {
	h.cancel()
}

// Done returns a channel that's closed when the stream is complete.
func (h *ClientStreamHandler) Done() <-chan struct{} {
	return h.done
}

// Events returns the channel for receiving typed events.
func (h *ClientStreamHandler) Events() <-chan ClientStreamEvent {
	return h.events
}

// HandleChatStreamEvent transforms a ChatStreamEvent into typed ClientStreamEvents.
// This is the main entry point for processing provider events.
func (h *ClientStreamHandler) HandleChatStreamEvent(event ChatStreamEvent) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Check for cancellation
	select {
	case <-h.ctx.Done():
		return h.ctx.Err()
	default:
	}

	// Handle error events
	if event.Error != "" {
		return h.sendError(MapErrorCode(event.Error), event.Error)
	}

	// Handle done event
	if event.Type == "done" {
		return h.handleDone(event)
	}

	// Process content blocks from choices
	if len(event.Choices) > 0 {
		choice := event.Choices[0]

		// Handle delta content (streaming)
		if choice.Delta != nil {
			return h.handleDelta(choice, event)
		}

		// Handle message content (non-streaming fallback)
		if choice.Message.Content != "" {
			return h.handleMessage(choice, event)
		}
	}

	return nil
}

// handleDelta processes streaming delta content.
func (h *ClientStreamHandler) handleDelta(choice ChatChoice, event ChatStreamEvent) error {
	delta := choice.Delta

	// Determine block type based on content
	blockType := h.determineBlockType(choice, event)

	// Handle block type transitions
	if err := h.handleBlockTransition(blockType); err != nil {
		return err
	}

	// Handle content delta
	if delta.Content != "" {
		return h.sendContentDelta(blockType, delta.Content)
	}

	// Handle tool calls
	if len(delta.ToolCalls) > 0 {
		return h.handleToolCallDelta(delta.ToolCalls)
	}

	// Handle finish reason
	if choice.FinishReason != "" {
		return h.handleFinishReason(choice.FinishReason)
	}

	return nil
}

// handleMessage processes non-streaming message content.
func (h *ClientStreamHandler) handleMessage(choice ChatChoice, event ChatStreamEvent) error {
	content := choice.Message.Content

	// Send content_start
	if !h.contentStarted {
		if err := h.sendEvent(NewContentStartEvent(h.ExecutionID)); err != nil {
			return err
		}

		h.contentStarted = true
	}

	// Send content as a single delta
	if err := h.sendEvent(NewContentDeltaEvent(h.ExecutionID, content, h.nextIndex())); err != nil {
		return err
	}

	// Send content_end
	if err := h.sendEvent(NewContentEndEvent(h.ExecutionID)); err != nil {
		return err
	}

	// Handle tool calls if present
	if len(choice.Message.ToolCalls) > 0 {
		for _, tc := range choice.Message.ToolCalls {
			if err := h.handleToolCall(tc); err != nil {
				return err
			}
		}
	}

	return nil
}

// determineBlockType determines the block type from the event.
func (h *ClientStreamHandler) determineBlockType(choice ChatChoice, event ChatStreamEvent) BlockType {
	// Check BlockType field if available
	if event.BlockType != "" {
		switch event.BlockType {
		case string(BlockTypeThinking):
			return BlockTypeThinking
		case string(BlockTypeToolUse):
			return BlockTypeToolUse
		default:
			return BlockTypeText
		}
	}

	// Check for tool calls
	if choice.Delta != nil && len(choice.Delta.ToolCalls) > 0 {
		return BlockTypeToolUse
	}

	// Default to text
	return BlockTypeText
}

// handleBlockTransition handles transitions between block types.
func (h *ClientStreamHandler) handleBlockTransition(newBlockType BlockType) error {
	// Same block type, no transition needed
	if h.currentBlockType == newBlockType && h.blockStarted {
		return nil
	}

	// End previous block if needed
	if h.blockStarted {
		if err := h.endCurrentBlock(); err != nil {
			return err
		}
	}

	// Start new block
	h.currentBlockType = newBlockType
	h.blockStarted = true

	switch newBlockType {
	case BlockTypeThinking:
		h.thinkingStarted = true

		return h.sendEvent(NewThinkingStartEvent(h.ExecutionID))
	case BlockTypeText:
		h.contentStarted = true

		return h.sendEvent(NewContentStartEvent(h.ExecutionID))
	case BlockTypeToolUse:
		// Tool use start is sent when we get the tool ID
		return nil
	}

	return nil
}

// endCurrentBlock sends the end event for the current block.
func (h *ClientStreamHandler) endCurrentBlock() error {
	if !h.blockStarted {
		return nil
	}

	h.blockStarted = false

	switch h.currentBlockType {
	case BlockTypeThinking:
		return h.sendEvent(NewThinkingEndEvent(h.ExecutionID))
	case BlockTypeText:
		return h.sendEvent(NewContentEndEvent(h.ExecutionID))
	case BlockTypeToolUse:
		if h.currentToolID != "" {
			if err := h.sendEvent(NewToolUseEndEvent(h.ExecutionID, h.currentToolID)); err != nil {
				return err
			}

			h.currentToolID = ""
		}
	}

	return nil
}

// sendContentDelta sends a delta event based on block type.
func (h *ClientStreamHandler) sendContentDelta(blockType BlockType, content string) error {
	index := h.nextIndex()

	switch blockType {
	case BlockTypeThinking:
		return h.sendEvent(NewThinkingDeltaEvent(h.ExecutionID, content, index))
	case BlockTypeText:
		return h.sendEvent(NewContentDeltaEvent(h.ExecutionID, content, index))
	default:
		return h.sendEvent(NewContentDeltaEvent(h.ExecutionID, content, index))
	}
}

// handleToolCallDelta handles streaming tool call deltas.
func (h *ClientStreamHandler) handleToolCallDelta(toolCalls []ToolCall) error {
	for _, tc := range toolCalls {
		// Start new tool if ID changed
		if tc.ID != "" && tc.ID != h.currentToolID {
			// End previous tool if any
			if h.currentToolID != "" {
				if err := h.sendEvent(NewToolUseEndEvent(h.ExecutionID, h.currentToolID)); err != nil {
					return err
				}
			}

			h.currentToolID = tc.ID

			toolName := ""
			if tc.Function != nil {
				toolName = tc.Function.Name
			}

			if err := h.sendEvent(NewToolUseStartEvent(h.ExecutionID, tc.ID, toolName)); err != nil {
				return err
			}
		}

		// Send tool delta (arguments)
		if tc.Function != nil && tc.Function.Arguments != "" {
			if err := h.sendEvent(NewToolUseDeltaEvent(h.ExecutionID, h.currentToolID, tc.Function.Arguments, h.nextIndex())); err != nil {
				return err
			}
		}
	}

	return nil
}

// handleToolCall handles a complete tool call (non-streaming).
func (h *ClientStreamHandler) handleToolCall(tc ToolCall) error {
	toolName := ""
	args := ""

	if tc.Function != nil {
		toolName = tc.Function.Name
		args = tc.Function.Arguments
	}

	// Send start
	if err := h.sendEvent(NewToolUseStartEvent(h.ExecutionID, tc.ID, toolName)); err != nil {
		return err
	}

	// Send arguments as single delta
	if args != "" {
		if err := h.sendEvent(NewToolUseDeltaEvent(h.ExecutionID, tc.ID, args, h.nextIndex())); err != nil {
			return err
		}
	}

	// Send end
	return h.sendEvent(NewToolUseEndEvent(h.ExecutionID, tc.ID))
}

// handleFinishReason handles the finish reason.
func (h *ClientStreamHandler) handleFinishReason(reason string) error {
	// End current block
	return h.endCurrentBlock()
}

// handleDone handles the done event.
func (h *ClientStreamHandler) handleDone(event ChatStreamEvent) error {
	// End any open blocks
	if err := h.endCurrentBlock(); err != nil {
		return err
	}

	// Build usage info
	var usage *StreamUsage
	if event.Usage != nil {
		usage = &StreamUsage{
			InputTokens:  int(event.Usage.InputTokens),
			OutputTokens: int(event.Usage.OutputTokens),
			TotalTokens:  int(event.Usage.TotalTokens),
		}
	}

	// Send done event
	doneEvent := NewDoneEvent(h.ExecutionID, usage)
	doneEvent.Model = h.model
	doneEvent.Provider = h.provider

	if err := h.sendEvent(doneEvent); err != nil {
		return err
	}

	// Signal completion (only close once to prevent panic)
	if !h.isDone {
		h.isDone = true
		close(h.done)
	}

	return nil
}

// sendError sends an error event.
func (h *ClientStreamHandler) sendError(code StreamErrorCode, message string) error {
	return h.sendEvent(NewErrorEvent(h.ExecutionID, code, message))
}

// sendEvent sends an event through the callback and channel.
func (h *ClientStreamHandler) sendEvent(event ClientStreamEvent) error {
	// Add model/provider info
	if event.Model == "" {
		event.Model = h.model
	}

	if event.Provider == "" {
		event.Provider = h.provider
	}

	// Call the callback if set
	if h.onEvent != nil {
		if err := h.onEvent(event); err != nil {
			return err
		}
	}

	// Send to channel (non-blocking)
	select {
	case h.events <- event:
	default:
		// Channel full, skip (or could log warning)
	}

	return nil
}

// MapErrorCode maps error messages to standard error codes.
func MapErrorCode(errorMsg string) StreamErrorCode {
	lower := strings.ToLower(errorMsg)

	switch {
	case strings.Contains(lower, "rate limit"):
		return ErrCodeRateLimit
	case strings.Contains(lower, "overloaded"):
		return ErrCodeOverloaded
	case strings.Contains(lower, "api key") || strings.Contains(lower, "authentication"):
		return ErrCodeInvalidAPIKey
	case strings.Contains(lower, "context length") || strings.Contains(lower, "token limit"):
		return ErrCodeContextLength
	case strings.Contains(lower, "cancelled") || strings.Contains(lower, "canceled"):
		return ErrCodeCancelled
	case strings.Contains(lower, "timeout"):
		return ErrCodeTimeout
	case strings.Contains(lower, "invalid request") || strings.Contains(lower, "bad request"):
		return ErrCodeInvalidRequest
	case strings.Contains(lower, "model not found"):
		return ErrCodeModelNotFound
	case strings.Contains(lower, "connection"):
		return ErrCodeConnectionError
	default:
		return ErrCodeProviderError
	}
}

// SSEWriter provides helpers for writing SSE events to HTTP responses.
type SSEWriter struct {
	w       http.ResponseWriter
	flusher http.Flusher
}

// NewSSEWriter creates a new SSE writer.
func NewSSEWriter(w http.ResponseWriter) (*SSEWriter, error) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return nil, errors.New("streaming not supported: ResponseWriter does not implement http.Flusher")
	}

	return &SSEWriter{
		w:       w,
		flusher: flusher,
	}, nil
}

// SetHeaders sets the required SSE headers.
func (s *SSEWriter) SetHeaders() {
	s.w.Header().Set("Content-Type", "text/event-stream")
	s.w.Header().Set("Cache-Control", "no-cache")
	s.w.Header().Set("Connection", "keep-alive")
	s.w.Header().Set("X-Accel-Buffering", "no") // Disable nginx buffering
}

// WriteEvent writes a ClientStreamEvent as an SSE data line.
func (s *SSEWriter) WriteEvent(event ClientStreamEvent) error {
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	// SSE format: "data: {json}\n\n"
	if _, err := fmt.Fprintf(s.w, "data: %s\n\n", data); err != nil {
		return fmt.Errorf("failed to write SSE data: %w", err)
	}

	s.flusher.Flush()

	return nil
}

// WriteEventWithType writes an SSE event with a custom event type.
func (s *SSEWriter) WriteEventWithType(eventType string, event ClientStreamEvent) error {
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	// SSE format with event type: "event: type\ndata: {json}\n\n"
	if _, err := fmt.Fprintf(s.w, "event: %s\ndata: %s\n\n", eventType, data); err != nil {
		return fmt.Errorf("failed to write SSE data: %w", err)
	}

	s.flusher.Flush()

	return nil
}

// WriteError writes an error event.
func (s *SSEWriter) WriteError(executionID string, code StreamErrorCode, message string) error {
	return s.WriteEvent(NewErrorEvent(executionID, code, message))
}

// WriteDone writes a done event.
func (s *SSEWriter) WriteDone(executionID string, usage *StreamUsage) error {
	return s.WriteEvent(NewDoneEvent(executionID, usage))
}

// Flush flushes the response writer.
func (s *SSEWriter) Flush() {
	s.flusher.Flush()
}
