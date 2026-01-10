package llm

import (
	"time"
)

// StreamEventType represents the type of streaming event sent to clients.
type StreamEventType string

const (
	// Thinking events - for extended thinking/reasoning.
	EventThinkingStart StreamEventType = "thinking_start"
	EventThinkingDelta StreamEventType = "thinking_delta"
	EventThinkingEnd   StreamEventType = "thinking_end"

	// Content events - for regular text content.
	EventContentStart StreamEventType = "content_start"
	EventContentDelta StreamEventType = "content_delta"
	EventContentEnd   StreamEventType = "content_end"

	// Tool use events - for function/tool calls.
	EventToolUseStart StreamEventType = "tool_use_start"
	EventToolUseDelta StreamEventType = "tool_use_delta"
	EventToolUseEnd   StreamEventType = "tool_use_end"

	// Tool result events - for tool execution results.
	EventToolResultStart StreamEventType = "tool_result_start"
	EventToolResultDelta StreamEventType = "tool_result_delta"
	EventToolResultEnd   StreamEventType = "tool_result_end"

	// UI Part events - for progressive UI component rendering.
	EventUIPartStart StreamEventType = "ui_part_start"
	EventUIPartDelta StreamEventType = "ui_part_delta"
	EventUIPartEnd   StreamEventType = "ui_part_end"

	// Control events.
	EventError StreamEventType = "error"
	EventDone  StreamEventType = "done"
)

// ClientStreamEvent is the unified event structure sent to clients via SSE.
// This is the typed event format that frontends consume for rendering.
// Note: This is different from StreamEvent in streaming.go which is for internal use.
type ClientStreamEvent struct {
	// Type is the event type (thinking_start, content_delta, etc.)
	Type StreamEventType `json:"type"`

	// Delta is the incremental text content for delta events
	Delta string `json:"delta,omitempty"`

	// Index is the monotonic index for React key stability
	// Increments across ALL delta events in a single execution
	Index int64 `json:"index,omitempty"`

	// ExecutionID uniquely identifies this streaming session
	ExecutionID string `json:"executionId"`

	// Timestamp is when this event was generated
	Timestamp time.Time `json:"timestamp"`

	// Tool use specific fields
	ToolID   string `json:"toolId,omitempty"`
	ToolName string `json:"toolName,omitempty"`

	// Error specific fields
	Error string `json:"error,omitempty"`
	Code  string `json:"code,omitempty"`

	// Usage information (included in done event)
	Usage *StreamUsage `json:"usage,omitempty"`

	// Model information
	Model    string `json:"model,omitempty"`
	Provider string `json:"provider,omitempty"`

	// UI Part specific fields - for progressive UI component rendering
	PartID   string `json:"partId,omitempty"`
	PartType string `json:"partType,omitempty"`
	Section  string `json:"section,omitempty"`
	PartData any    `json:"partData,omitempty"`
}

// StreamUsage contains token usage information for the stream.
type StreamUsage struct {
	InputTokens  int `json:"inputTokens"`
	OutputTokens int `json:"outputTokens"`
	TotalTokens  int `json:"totalTokens"`
}

// BlockType represents the type of content block being streamed.
type BlockType string

const (
	BlockTypeThinking BlockType = "thinking"
	BlockTypeText     BlockType = "text"
	BlockTypeToolUse  BlockType = "tool_use"
)

// BlockState represents the state of a content block.
type BlockState string

const (
	BlockStateStart BlockState = "start"
	BlockStateDelta BlockState = "delta"
	BlockStateStop  BlockState = "stop"
)

// StreamErrorCode represents standard error codes for streaming.
type StreamErrorCode string

const (
	ErrCodeRateLimit       StreamErrorCode = "rate_limit"
	ErrCodeOverloaded      StreamErrorCode = "overloaded"
	ErrCodeInvalidAPIKey   StreamErrorCode = "invalid_api_key"
	ErrCodeContextLength   StreamErrorCode = "context_length_exceeded"
	ErrCodeInternalError   StreamErrorCode = "internal_error"
	ErrCodeCancelled       StreamErrorCode = "cancelled"
	ErrCodeTimeout         StreamErrorCode = "timeout"
	ErrCodeInvalidRequest  StreamErrorCode = "invalid_request"
	ErrCodeModelNotFound   StreamErrorCode = "model_not_found"
	ErrCodeProviderError   StreamErrorCode = "provider_error"
	ErrCodeConnectionError StreamErrorCode = "connection_error"
)

// NewClientStreamEvent creates a new ClientStreamEvent with the given type and execution ID.
func NewClientStreamEvent(eventType StreamEventType, executionID string) ClientStreamEvent {
	return ClientStreamEvent{
		Type:        eventType,
		ExecutionID: executionID,
		Timestamp:   time.Now(),
	}
}

// NewThinkingStartEvent creates a thinking_start event.
func NewThinkingStartEvent(executionID string) ClientStreamEvent {
	return NewClientStreamEvent(EventThinkingStart, executionID)
}

// NewThinkingDeltaEvent creates a thinking_delta event with content.
func NewThinkingDeltaEvent(executionID string, delta string, index int64) ClientStreamEvent {
	event := NewClientStreamEvent(EventThinkingDelta, executionID)
	event.Delta = delta
	event.Index = index

	return event
}

// NewThinkingEndEvent creates a thinking_end event.
func NewThinkingEndEvent(executionID string) ClientStreamEvent {
	return NewClientStreamEvent(EventThinkingEnd, executionID)
}

// NewContentStartEvent creates a content_start event.
func NewContentStartEvent(executionID string) ClientStreamEvent {
	return NewClientStreamEvent(EventContentStart, executionID)
}

// NewContentDeltaEvent creates a content_delta event with content.
func NewContentDeltaEvent(executionID string, delta string, index int64) ClientStreamEvent {
	event := NewClientStreamEvent(EventContentDelta, executionID)
	event.Delta = delta
	event.Index = index

	return event
}

// NewContentEndEvent creates a content_end event.
func NewContentEndEvent(executionID string) ClientStreamEvent {
	return NewClientStreamEvent(EventContentEnd, executionID)
}

// NewToolUseStartEvent creates a tool_use_start event.
func NewToolUseStartEvent(executionID, toolID, toolName string) ClientStreamEvent {
	event := NewClientStreamEvent(EventToolUseStart, executionID)
	event.ToolID = toolID
	event.ToolName = toolName

	return event
}

// NewToolUseDeltaEvent creates a tool_use_delta event with partial JSON.
func NewToolUseDeltaEvent(executionID, toolID string, delta string, index int64) ClientStreamEvent {
	event := NewClientStreamEvent(EventToolUseDelta, executionID)
	event.ToolID = toolID
	event.Delta = delta
	event.Index = index

	return event
}

// NewToolUseEndEvent creates a tool_use_end event.
func NewToolUseEndEvent(executionID, toolID string) ClientStreamEvent {
	event := NewClientStreamEvent(EventToolUseEnd, executionID)
	event.ToolID = toolID

	return event
}

// NewToolResultStartEvent creates a tool_result_start event.
func NewToolResultStartEvent(executionID, toolID, toolName string) ClientStreamEvent {
	event := NewClientStreamEvent(EventToolResultStart, executionID)
	event.ToolID = toolID
	event.ToolName = toolName

	return event
}

// NewToolResultDeltaEvent creates a tool_result_delta event with result content.
func NewToolResultDeltaEvent(executionID, toolID string, delta string, index int64) ClientStreamEvent {
	event := NewClientStreamEvent(EventToolResultDelta, executionID)
	event.ToolID = toolID
	event.Delta = delta
	event.Index = index

	return event
}

// NewToolResultEndEvent creates a tool_result_end event.
func NewToolResultEndEvent(executionID, toolID string) ClientStreamEvent {
	event := NewClientStreamEvent(EventToolResultEnd, executionID)
	event.ToolID = toolID

	return event
}

// NewErrorEvent creates an error event.
func NewErrorEvent(executionID string, code StreamErrorCode, message string) ClientStreamEvent {
	event := NewClientStreamEvent(EventError, executionID)
	event.Code = string(code)
	event.Error = message

	return event
}

// NewDoneEvent creates a done event with optional usage information.
func NewDoneEvent(executionID string, usage *StreamUsage) ClientStreamEvent {
	event := NewClientStreamEvent(EventDone, executionID)
	event.Usage = usage

	return event
}

// WithModel adds model information to the event.
func (e ClientStreamEvent) WithModel(model, provider string) ClientStreamEvent {
	e.Model = model
	e.Provider = provider

	return e
}

// NewUIPartStartEvent creates a ui_part_start event for progressive UI rendering.
func NewUIPartStartEvent(executionID, partID, partType string) ClientStreamEvent {
	event := NewClientStreamEvent(EventUIPartStart, executionID)
	event.PartID = partID
	event.PartType = partType

	return event
}

// NewUIPartDeltaEvent creates a ui_part_delta event with section data.
func NewUIPartDeltaEvent(executionID, partID, section string, data any, index int64) ClientStreamEvent {
	event := NewClientStreamEvent(EventUIPartDelta, executionID)
	event.PartID = partID
	event.Section = section
	event.PartData = data
	event.Index = index

	return event
}

// NewUIPartEndEvent creates a ui_part_end event.
func NewUIPartEndEvent(executionID, partID string) ClientStreamEvent {
	event := NewClientStreamEvent(EventUIPartEnd, executionID)
	event.PartID = partID

	return event
}

// WithPartData adds part data to a UI part event.
func (e ClientStreamEvent) WithPartData(data any) ClientStreamEvent {
	e.PartData = data

	return e
}
