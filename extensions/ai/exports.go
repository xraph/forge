package ai

import (
	"github.com/xraph/forge/extensions/ai/internal"
	"github.com/xraph/forge/extensions/ai/llm"
	"github.com/xraph/forge/extensions/ai/models"
)

// LLM types
type (
	LLMProvider        = llm.LLMProvider
	LLMManager         = llm.LLMManager
	LLMManagerConfig   = llm.LLMManagerConfig
	LLMUsage           = llm.LLMUsage
	ChatRequest        = llm.ChatRequest
	ChatResponse       = llm.ChatResponse
	CompletionRequest  = llm.CompletionRequest
	CompletionResponse = llm.CompletionResponse
	EmbeddingRequest   = llm.EmbeddingRequest
	EmbeddingResponse  = llm.EmbeddingResponse
	ChatMessage        = llm.ChatMessage
	Tool               = llm.Tool
	ToolCall           = llm.ToolCall
)

// Model types
type (
	Model       = models.Model
	ModelInput  = models.ModelInput
	ModelOutput = models.ModelOutput
	ModelInfo   = models.ModelInfo
	ModelConfig = models.ModelConfig
)

// Core AI types
type (
	AI                = internal.AI
	AIConfig          = internal.AIConfig
	AIAgent           = internal.AIAgent
	BaseAgent         = internal.BaseAgent
	AgentType         = internal.AgentType
	AgentConfig       = internal.AgentConfig
	AgentInput        = internal.AgentInput
	AgentOutput       = internal.AgentOutput
	AgentAction       = internal.AgentAction
	AgentHealth       = internal.AgentHealth
	AgentHealthStatus = internal.AgentHealthStatus
	AgentStats        = internal.AgentStats
	AgentFeedback     = internal.AgentFeedback
	AgentRequest      = internal.AgentRequest
	AgentResponse     = internal.AgentResponse
	Capability        = internal.Capability
	LearningMetrics   = internal.LearningMetrics
)

// Middleware types
type (
	AIMiddlewareType   = internal.AIMiddlewareType
	AIMiddlewareConfig = internal.AIMiddlewareConfig
	AIMiddlewareStats  = internal.AIMiddlewareStats
)

// Note: Storage, Factory, and Team types are already defined in the ai package
// and don't need to be re-exported here. They are already public types.

// Agent type constants
const (
	AgentTypeOptimization     = internal.AgentTypeOptimization
	AgentTypeAnomalyDetection = internal.AgentTypeAnomalyDetection
	AgentTypeLoadBalancer     = internal.AgentTypeLoadBalancer
	AgentTypeCacheManager     = internal.AgentTypeCacheManager
	AgentTypeJobScheduler     = internal.AgentTypeJobScheduler
	AgentTypeSecurityMonitor  = internal.AgentTypeSecurityMonitor
	AgentTypeResourceManager  = internal.AgentTypeResourceManager
	AgentTypePredictor        = internal.AgentTypePredictor
)

// Agent health status constants
const (
	AgentHealthStatusHealthy   = internal.AgentHealthStatusHealthy
	AgentHealthStatusUnhealthy = internal.AgentHealthStatusUnhealthy
	AgentHealthStatusDegraded  = internal.AgentHealthStatusDegraded
	AgentHealthStatusUnknown   = internal.AgentHealthStatusUnknown
)

// Middleware type constants
const (
	AIMiddlewareTypeLoadBalance          = internal.AIMiddlewareTypeLoadBalance
	AIMiddlewareTypeAnomalyDetection     = internal.AIMiddlewareTypeAnomalyDetection
	AIMiddlewareTypeRateLimit            = internal.AIMiddlewareTypeRateLimit
	AIMiddlewareTypeResponseOptimization = internal.AIMiddlewareTypeResponseOptimization
	AIMiddlewareTypePersonalization      = internal.AIMiddlewareTypePersonalization
	AIMiddlewareTypeOptimization         = internal.AIMiddlewareTypeOptimization
	AIMiddlewareTypeSecurity             = internal.AIMiddlewareTypeSecurity
)

// Constructor exports
var (
	NewBaseAgent = internal.NewBaseAgent
)
