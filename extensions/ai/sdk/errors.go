package sdk

import "errors"

// SDK error types provide typed errors for better error handling and debugging.
// Use errors.Is() to check for specific error types.

// Agent-related errors.
var (
	// ErrAgentNil is returned when a nil agent is provided.
	ErrAgentNil = errors.New("agent is nil")

	// ErrAgentIDRequired is returned when an agent ID is required but not provided.
	ErrAgentIDRequired = errors.New("agent ID is required")

	// ErrAgentNotFound is returned when an agent is not found in the registry.
	ErrAgentNotFound = errors.New("agent not found")

	// ErrAgentAlreadyRegistered is returned when trying to register an agent that already exists.
	ErrAgentAlreadyRegistered = errors.New("agent already registered")

	// ErrNoAgentsAvailable is returned when no agents are available for routing.
	ErrNoAgentsAvailable = errors.New("no agents available")
)

// Tool-related errors.
var (
	// ErrToolNil is returned when a nil tool is provided.
	ErrToolNil = errors.New("tool is nil")

	// ErrToolNotFound is returned when a tool is not found.
	ErrToolNotFound = errors.New("tool not found")

	// ErrToolAlreadyRegistered is returned when trying to register a tool that already exists.
	ErrToolAlreadyRegistered = errors.New("tool already registered")

	// ErrInvalidToolName is returned when an invalid tool name is provided.
	ErrInvalidToolName = errors.New("invalid tool name")

	// ErrToolExecutionFailed is returned when a tool execution fails.
	ErrToolExecutionFailed = errors.New("tool execution failed")

	// ErrToolTimeout is returned when a tool execution times out.
	ErrToolTimeout = errors.New("tool execution timed out")
)

// Handoff-related errors.
var (
	// ErrInvalidHandoffTarget is returned when an invalid handoff target is specified.
	ErrInvalidHandoffTarget = errors.New("invalid handoff target")

	// ErrEmptyHandoffChain is returned when an empty handoff chain is provided.
	ErrEmptyHandoffChain = errors.New("empty handoff chain")

	// ErrHandoffChainTooLong is returned when a handoff chain exceeds the maximum depth.
	ErrHandoffChainTooLong = errors.New("handoff chain exceeds maximum depth")

	// ErrHandoffFailed is returned when a handoff operation fails.
	ErrHandoffFailed = errors.New("handoff failed")

	// ErrCircularHandoff is returned when a circular handoff is detected.
	ErrCircularHandoff = errors.New("circular handoff detected")
)

// Workflow-related errors.
var (
	// ErrWorkflowNil is returned when a nil workflow is provided.
	ErrWorkflowNil = errors.New("workflow is nil")

	// ErrWorkflowNotFound is returned when a workflow is not found.
	ErrWorkflowNotFound = errors.New("workflow not found")

	// ErrWorkflowCycleDetected is returned when a cycle is detected in a workflow.
	ErrWorkflowCycleDetected = errors.New("workflow cycle detected")

	// ErrWorkflowNodeNotFound is returned when a workflow node is not found.
	ErrWorkflowNodeNotFound = errors.New("workflow node not found")

	// ErrWorkflowNoEntryPoint is returned when a workflow has no entry point.
	ErrWorkflowNoEntryPoint = errors.New("workflow has no entry point")

	// ErrWorkflowAlreadyRunning is returned when trying to start a workflow that's already running.
	ErrWorkflowAlreadyRunning = errors.New("workflow is already running")

	// ErrWorkflowNotRunning is returned when trying to operate on a workflow that's not running.
	ErrWorkflowNotRunning = errors.New("workflow is not running")

	// ErrWorkflowExecutionFailed is returned when workflow execution fails.
	ErrWorkflowExecutionFailed = errors.New("workflow execution failed")
)

// Generation-related errors.
var (
	// ErrGenerationFailed is returned when text generation fails.
	ErrGenerationFailed = errors.New("generation failed")

	// ErrGenerationTimeout is returned when generation times out.
	ErrGenerationTimeout = errors.New("generation timed out")

	// ErrPromptRequired is returned when a prompt is required but not provided.
	ErrPromptRequired = errors.New("prompt is required")

	// ErrPromptTooLong is returned when a prompt exceeds the maximum length.
	ErrPromptTooLong = errors.New("prompt exceeds maximum length")

	// ErrInvalidPromptTemplate is returned when a prompt template is invalid.
	ErrInvalidPromptTemplate = errors.New("invalid prompt template")

	// ErrPromptRenderFailed is returned when prompt template rendering fails.
	ErrPromptRenderFailed = errors.New("prompt template rendering failed")
)

// Streaming-related errors.
var (
	// ErrStreamingNotSupported is returned when streaming is not supported.
	ErrStreamingNotSupported = errors.New("streaming not supported")

	// ErrStreamClosed is returned when operating on a closed stream.
	ErrStreamClosed = errors.New("stream is closed")

	// ErrStreamTimeout is returned when streaming times out.
	ErrStreamTimeout = errors.New("stream timed out")
)

// Memory-related errors.
var (
	// ErrMemoryNotFound is returned when a memory entry is not found.
	ErrMemoryNotFound = errors.New("memory not found")

	// ErrMemoryStoreFailed is returned when storing memory fails.
	ErrMemoryStoreFailed = errors.New("failed to store memory")

	// ErrMemoryRetrieveFailed is returned when retrieving memory fails.
	ErrMemoryRetrieveFailed = errors.New("failed to retrieve memory")
)

// RAG-related errors.
var (
	// ErrRAGIndexFailed is returned when RAG indexing fails.
	ErrRAGIndexFailed = errors.New("RAG indexing failed")

	// ErrRAGRetrievalFailed is returned when RAG retrieval fails.
	ErrRAGRetrievalFailed = errors.New("RAG retrieval failed")

	// ErrRAGNoResults is returned when RAG retrieval returns no results.
	ErrRAGNoResults = errors.New("no RAG results found")

	// ErrDocumentTooLarge is returned when a document exceeds the maximum size.
	ErrDocumentTooLarge = errors.New("document exceeds maximum size")
)

// Guardrail-related errors.
var (
	// ErrGuardrailViolation is returned when a guardrail is violated.
	ErrGuardrailViolation = errors.New("guardrail violation")

	// ErrInputBlocked is returned when input is blocked by guardrails.
	ErrInputBlocked = errors.New("input blocked by guardrails")

	// ErrOutputBlocked is returned when output is blocked by guardrails.
	ErrOutputBlocked = errors.New("output blocked by guardrails")
)

// State-related errors.
var (
	// ErrStateNotFound is returned when agent state is not found.
	ErrStateNotFound = errors.New("state not found")

	// ErrStateSaveFailed is returned when saving state fails.
	ErrStateSaveFailed = errors.New("failed to save state")

	// ErrStateLoadFailed is returned when loading state fails.
	ErrStateLoadFailed = errors.New("failed to load state")

	// ErrInvalidState is returned when state is invalid.
	ErrInvalidState = errors.New("invalid state")
)

// Cache-related errors.
var (
	// ErrCacheMiss is returned when a cache lookup misses.
	ErrCacheMiss = errors.New("cache miss")

	// ErrCacheStoreFailed is returned when storing in cache fails.
	ErrCacheStoreFailed = errors.New("failed to store in cache")
)

// Cost-related errors.
var (
	// ErrBudgetExceeded is returned when the cost budget is exceeded.
	ErrBudgetExceeded = errors.New("budget exceeded")

	// ErrCostTrackingFailed is returned when cost tracking fails.
	ErrCostTrackingFailed = errors.New("cost tracking failed")
)

// Plugin-related errors.
var (
	// ErrPluginNotFound is returned when a plugin is not found.
	ErrPluginNotFound = errors.New("plugin not found")

	// ErrPluginLoadFailed is returned when loading a plugin fails.
	ErrPluginLoadFailed = errors.New("failed to load plugin")

	// ErrPluginInitFailed is returned when plugin initialization fails.
	ErrPluginInitFailed = errors.New("plugin initialization failed")
)

// Configuration-related errors.
var (
	// ErrInvalidConfig is returned when configuration is invalid.
	ErrInvalidConfig = errors.New("invalid configuration")

	// ErrMissingConfig is returned when required configuration is missing.
	ErrMissingConfig = errors.New("missing required configuration")
)

// LLM-related errors.
var (
	// ErrLLMNotAvailable is returned when the LLM is not available.
	ErrLLMNotAvailable = errors.New("LLM not available")

	// ErrLLMRequestFailed is returned when an LLM request fails.
	ErrLLMRequestFailed = errors.New("LLM request failed")

	// ErrProviderNotFound is returned when an LLM provider is not found.
	ErrProviderNotFound = errors.New("provider not found")

	// ErrModelNotSupported is returned when a model is not supported.
	ErrModelNotSupported = errors.New("model not supported")

	// ErrRateLimited is returned when rate limited by the LLM provider.
	ErrRateLimited = errors.New("rate limited")

	// ErrContextLengthExceeded is returned when context length is exceeded.
	ErrContextLengthExceeded = errors.New("context length exceeded")
)

// Multimodal-related errors.
var (
	// ErrUnsupportedContentType is returned when a content type is not supported.
	ErrUnsupportedContentType = errors.New("unsupported content type")

	// ErrInvalidImageFormat is returned when an image format is invalid.
	ErrInvalidImageFormat = errors.New("invalid image format")

	// ErrInvalidAudioFormat is returned when an audio format is invalid.
	ErrInvalidAudioFormat = errors.New("invalid audio format")

	// ErrContentTooLarge is returned when content exceeds size limits.
	ErrContentTooLarge = errors.New("content too large")
)

// Batch-related errors.
var (
	// ErrBatchEmpty is returned when a batch is empty.
	ErrBatchEmpty = errors.New("batch is empty")

	// ErrBatchTooLarge is returned when a batch exceeds size limits.
	ErrBatchTooLarge = errors.New("batch too large")

	// ErrBatchProcessingFailed is returned when batch processing fails.
	ErrBatchProcessingFailed = errors.New("batch processing failed")
)

// Resilience-related errors.
var (
	// ErrCircuitOpen is returned when a circuit breaker is open.
	ErrCircuitOpen = errors.New("circuit breaker is open")

	// ErrMaxRetriesExceeded is returned when maximum retries are exceeded.
	ErrMaxRetriesExceeded = errors.New("maximum retries exceeded")

	// ErrAllFallbacksFailed is returned when all fallbacks fail.
	ErrAllFallbacksFailed = errors.New("all fallbacks failed")
)

// IsRetryable returns true if the error is retryable.
func IsRetryable(err error) bool {
	if err == nil {
		return false
	}

	retryableErrors := []error{
		ErrLLMRequestFailed,
		ErrRateLimited,
		ErrStreamTimeout,
		ErrGenerationTimeout,
		ErrToolTimeout,
	}

	for _, retryable := range retryableErrors {
		if errors.Is(err, retryable) {
			return true
		}
	}

	return false
}

// IsUserError returns true if the error is caused by invalid user input.
func IsUserError(err error) bool {
	if err == nil {
		return false
	}

	userErrors := []error{
		ErrPromptRequired,
		ErrPromptTooLong,
		ErrInvalidPromptTemplate,
		ErrGuardrailViolation,
		ErrInputBlocked,
		ErrInvalidConfig,
		ErrMissingConfig,
		ErrUnsupportedContentType,
		ErrInvalidImageFormat,
		ErrInvalidAudioFormat,
		ErrContentTooLarge,
	}

	for _, userErr := range userErrors {
		if errors.Is(err, userErr) {
			return true
		}
	}

	return false
}

// IsResourceError returns true if the error is related to resource limits.
func IsResourceError(err error) bool {
	if err == nil {
		return false
	}

	resourceErrors := []error{
		ErrBudgetExceeded,
		ErrRateLimited,
		ErrContextLengthExceeded,
		ErrDocumentTooLarge,
		ErrBatchTooLarge,
	}

	for _, resourceErr := range resourceErrors {
		if errors.Is(err, resourceErr) {
			return true
		}
	}

	return false
}
