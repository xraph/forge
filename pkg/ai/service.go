package ai

import (
	"context"
	"fmt"
	"time"

	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/logger"
)

// AIService integrates the AI manager with the Forge service lifecycle
type AIService struct {
	*AIManager
	container common.Container
}

// NewAIService creates a new AI service
func NewAIService(logger common.Logger, metrics common.Metrics, config common.ConfigManager) common.Service {
	manager := NewAIManager(logger, metrics, config)
	return &AIService{
		AIManager: manager,
	}
}

// NewAIServiceWithContainer creates a new AI service with container access
func NewAIServiceWithContainer(container common.Container, logger common.Logger, metrics common.Metrics, config common.ConfigManager) *AIService {
	manager := NewAIManager(logger, metrics, config)
	return &AIService{
		AIManager: manager,
		container: container,
	}
}

// Name returns the service name
func (s *AIService) Name() string {
	return "ai-service"
}

// Dependencies returns the service dependencies
func (s *AIService) Dependencies() []string {
	return []string{
		"config-manager",
		"logger",
		"metrics-collector",
		"database-manager",  // For model storage and agent state
		"event-bus",         // For agent communication
		"streaming-manager", // For real-time agent updates
		"health-checker",    // For health monitoring
		"cache-manager",     // For inference caching
	}
}

// OnStart starts the AI service and initializes default agents
func (s *AIService) OnStart(ctx context.Context) error {
	// OnStart the AI manager
	if err := s.AIManager.OnStart(ctx); err != nil {
		return err
	}

	// Initialize default agents if container is available
	if s.container != nil {
		if err := s.initializeDefaultAgents(ctx); err != nil {
			if s.logger != nil {
				s.logger.Warn("failed to initialize some default agents",
					logger.Error(err),
				)
			}
			// Don't fail the service start if some agents fail to initialize
		}
	}

	return nil
}

// OnStop stops the AI service
func (s *AIService) OnStop(ctx context.Context) error {
	return s.AIManager.OnStop(ctx)
}

// OnHealthCheck performs health checks on the AI service
func (s *AIService) OnHealthCheck(ctx context.Context) error {
	return s.AIManager.OnHealthCheck(ctx)
}

// initializeDefaultAgents initializes default AI agents
func (s *AIService) initializeDefaultAgents(ctx context.Context) error {
	// Initialize optimization agent
	if err := s.initializeOptimizationAgent(ctx); err != nil {
		if s.logger != nil {
			s.logger.Warn("failed to initialize optimization agent", logger.Error(err))
		}
	}

	// Initialize anomaly detection agent
	if err := s.initializeAnomalyDetectionAgent(ctx); err != nil {
		if s.logger != nil {
			s.logger.Warn("failed to initialize anomaly detection agent", logger.Error(err))
		}
	}

	// Initialize load balancer agent
	if err := s.initializeLoadBalancerAgent(ctx); err != nil {
		if s.logger != nil {
			s.logger.Warn("failed to initialize load balancer agent", logger.Error(err))
		}
	}

	// Initialize cache optimization agent
	if err := s.initializeCacheAgent(ctx); err != nil {
		if s.logger != nil {
			s.logger.Warn("failed to initialize cache agent", logger.Error(err))
		}
	}

	// Initialize security monitoring agent
	if err := s.initializeSecurityAgent(ctx); err != nil {
		if s.logger != nil {
			s.logger.Warn("failed to initialize security agent", logger.Error(err))
		}
	}

	// Initialize resource management agent
	if err := s.initializeResourceAgent(ctx); err != nil {
		if s.logger != nil {
			s.logger.Warn("failed to initialize resource agent", logger.Error(err))
		}
	}

	// Initialize predictor agent
	if err := s.initializePredictorAgent(ctx); err != nil {
		if s.logger != nil {
			s.logger.Warn("failed to initialize predictor agent", logger.Error(err))
		}
	}

	return nil
}

// initializeOptimizationAgent initializes the performance optimization agent
func (s *AIService) initializeOptimizationAgent(ctx context.Context) error {
	agent := NewOptimizationAgent("optimization-agent", "Performance Optimization Agent")
	return s.RegisterAgent(agent)
}

// initializeAnomalyDetectionAgent initializes the anomaly detection agent
func (s *AIService) initializeAnomalyDetectionAgent(ctx context.Context) error {
	agent := NewAnomalyDetectionAgent("anomaly-agent", "Anomaly Detection Agent")
	return s.RegisterAgent(agent)
}

// initializeLoadBalancerAgent initializes the load balancer agent
func (s *AIService) initializeLoadBalancerAgent(ctx context.Context) error {
	agent := NewLoadBalancerAgent("loadbalancer-agent", "Intelligent Load Balancer Agent")
	return s.RegisterAgent(agent)
}

// initializeCacheAgent initializes the cache optimization agent
func (s *AIService) initializeCacheAgent(ctx context.Context) error {
	agent := NewCacheOptimizationAgent("cache-agent", "Cache Optimization Agent")
	return s.RegisterAgent(agent)
}

// initializeSecurityAgent initializes the security monitoring agent
func (s *AIService) initializeSecurityAgent(ctx context.Context) error {
	agent := NewSecurityMonitoringAgent("security-agent", "Security Monitoring Agent")
	return s.RegisterAgent(agent)
}

// initializeResourceAgent initializes the resource management agent
func (s *AIService) initializeResourceAgent(ctx context.Context) error {
	agent := NewResourceManagementAgent("resource-agent", "Resource Management Agent")
	return s.RegisterAgent(agent)
}

// initializePredictorAgent initializes the predictor agent
func (s *AIService) initializePredictorAgent(ctx context.Context) error {
	agent := NewPredictiveAnalyticsAgent("predictor-agent", "Predictive Analytics Agent")
	return s.RegisterAgent(agent)
}

// GetAIManager returns the underlying AI manager
func (s *AIService) GetAIManager() *AIManager {
	return s.AIManager
}

// ProcessIntelligentRequest processes a request with intelligent routing
func (s *AIService) ProcessIntelligentRequest(ctx context.Context, requestType string, data interface{}) (*AgentOutput, error) {
	request := AgentRequest{
		ID:      generateRequestID(),
		Type:    requestType,
		Data:    data,
		Context: extractContextFromRequest(ctx),
	}

	response, err := s.ProcessAgentRequest(ctx, request)
	if err != nil {
		return nil, err
	}

	return &response.Output, nil
}

// OptimizePerformance requests performance optimization
func (s *AIService) OptimizePerformance(ctx context.Context, component string, metrics map[string]float64) (*AgentOutput, error) {
	return s.ProcessIntelligentRequest(ctx, "performance-optimization", map[string]interface{}{
		"component": component,
		"metrics":   metrics,
	})
}

// DetectAnomalies requests anomaly detection
func (s *AIService) DetectAnomalies(ctx context.Context, data []DataPoint) (*AgentOutput, error) {
	return s.ProcessIntelligentRequest(ctx, "anomaly-detection", map[string]interface{}{
		"data": data,
	})
}

// OptimizeLoadBalancing requests load balancing optimization
func (s *AIService) OptimizeLoadBalancing(ctx context.Context, currentLoad LoadMetrics) (*AgentOutput, error) {
	return s.ProcessIntelligentRequest(ctx, "load-balancing", map[string]interface{}{
		"current_load": currentLoad,
	})
}

// OptimizeCache requests cache optimization
func (s *AIService) OptimizeCache(ctx context.Context, cacheStats CacheStats) (*AgentOutput, error) {
	return s.ProcessIntelligentRequest(ctx, "cache-optimization", map[string]interface{}{
		"cache_stats": cacheStats,
	})
}

// MonitorSecurity requests security monitoring
func (s *AIService) MonitorSecurity(ctx context.Context, securityEvents []SecurityEvent) (*AgentOutput, error) {
	return s.ProcessIntelligentRequest(ctx, "security-monitoring", map[string]interface{}{
		"security_events": securityEvents,
	})
}

// OptimizeResources requests resource optimization
func (s *AIService) OptimizeResources(ctx context.Context, resourceUsage ResourceUsage) (*AgentOutput, error) {
	return s.ProcessIntelligentRequest(ctx, "resource-optimization", map[string]interface{}{
		"resource_usage": resourceUsage,
	})
}

// PredictTrends requests predictive analytics
func (s *AIService) PredictTrends(ctx context.Context, historicalData []DataPoint, horizon time.Duration) (*AgentOutput, error) {
	return s.ProcessIntelligentRequest(ctx, "predictive-analytics", map[string]interface{}{
		"historical_data": historicalData,
		"horizon":         horizon,
	})
}

// Supporting types and functions

// DataPoint represents a data point for analysis
type DataPoint struct {
	Timestamp time.Time              `json:"timestamp"`
	Value     float64                `json:"value"`
	Labels    map[string]string      `json:"labels"`
	Metadata  map[string]interface{} `json:"metadata"`
}

// LoadMetrics represents load balancing metrics
type LoadMetrics struct {
	TotalRequests     int64                    `json:"total_requests"`
	RequestsPerSecond float64                  `json:"requests_per_second"`
	ServerLoads       map[string]float64       `json:"server_loads"`
	ResponseTimes     map[string]time.Duration `json:"response_times"`
	ErrorRates        map[string]float64       `json:"error_rates"`
}

// CacheStats represents cache statistics
type CacheStats struct {
	HitRate        float64              `json:"hit_rate"`
	MissRate       float64              `json:"miss_rate"`
	EvictionRate   float64              `json:"eviction_rate"`
	Size           int64                `json:"size"`
	MaxSize        int64                `json:"max_size"`
	KeyFrequencies map[string]int64     `json:"key_frequencies"`
	KeyAccessTimes map[string]time.Time `json:"key_access_times"`
}

// SecurityEvent represents a security event
type SecurityEvent struct {
	ID        string                 `json:"id"`
	Type      string                 `json:"type"`
	Severity  string                 `json:"severity"`
	Source    string                 `json:"source"`
	Target    string                 `json:"target"`
	Message   string                 `json:"message"`
	Details   map[string]interface{} `json:"details"`
	Timestamp time.Time              `json:"timestamp"`
}

// ResourceUsage represents resource usage metrics
type ResourceUsage struct {
	CPU       float64 `json:"cpu"`
	Memory    float64 `json:"memory"`
	Disk      float64 `json:"disk"`
	Network   float64 `json:"network"`
	Processes int     `json:"processes"`
	Threads   int     `json:"threads"`
}

// generateRequestID generates a unique request ID
func generateRequestID() string {
	return fmt.Sprintf("req-%d", time.Now().UnixNano())
}

// extractContextFromRequest extracts context information from the request
func extractContextFromRequest(ctx context.Context) map[string]interface{} {
	contextMap := make(map[string]interface{})

	// Extract common context values
	if userID := ctx.Value("user_id"); userID != nil {
		contextMap["user_id"] = userID
	}

	if requestID := ctx.Value("request_id"); requestID != nil {
		contextMap["request_id"] = requestID
	}

	if traceID := ctx.Value("trace_id"); traceID != nil {
		contextMap["trace_id"] = traceID
	}

	if sessionID := ctx.Value("session_id"); sessionID != nil {
		contextMap["session_id"] = sessionID
	}

	contextMap["timestamp"] = time.Now()

	return contextMap
}

// RegisterDefaultAgents registers default AI agents for common use cases
func RegisterDefaultAgents(container common.Container) error {
	// Get AI service
	aiService, err := container.ResolveNamed("ai-service")
	if err != nil {
		return err
	}

	aiSvc, ok := aiService.(*AIService)
	if !ok {
		return fmt.Errorf("ai-service is not of type *AIService")
	}

	// Register optimization agent
	optimizationAgent := NewOptimizationAgent("default-optimization", "Default Performance Optimization Agent")
	if err := aiSvc.RegisterAgent(optimizationAgent); err != nil {
		return fmt.Errorf("failed to register optimization agent: %w", err)
	}

	// Register anomaly detection agent
	anomalyAgent := NewAnomalyDetectionAgent("default-anomaly", "Default Anomaly Detection Agent")
	if err := aiSvc.RegisterAgent(anomalyAgent); err != nil {
		return fmt.Errorf("failed to register anomaly detection agent: %w", err)
	}

	// Register load balancer agent
	loadBalancerAgent := NewLoadBalancerAgent("default-loadbalancer", "Default Load Balancer Agent")
	if err := aiSvc.RegisterAgent(loadBalancerAgent); err != nil {
		return fmt.Errorf("failed to register load balancer agent: %w", err)
	}

	// Register cache agent
	cacheAgent := NewCacheOptimizationAgent("default-cache", "Default Cache Optimization Agent")
	if err := aiSvc.RegisterAgent(cacheAgent); err != nil {
		return fmt.Errorf("failed to register cache agent: %w", err)
	}

	// Register security agent
	securityAgent := NewSecurityMonitoringAgent("default-security", "Default Security Monitoring Agent")
	if err := aiSvc.RegisterAgent(securityAgent); err != nil {
		return fmt.Errorf("failed to register security agent: %w", err)
	}

	// Register resource agent
	resourceAgent := NewResourceManagementAgent("default-resource", "Default Resource Management Agent")
	if err := aiSvc.RegisterAgent(resourceAgent); err != nil {
		return fmt.Errorf("failed to register resource agent: %w", err)
	}

	// Register predictor agent
	predictorAgent := NewPredictiveAnalyticsAgent("default-predictor", "Default Predictive Analytics Agent")
	if err := aiSvc.RegisterAgent(predictorAgent); err != nil {
		return fmt.Errorf("failed to register predictor agent: %w", err)
	}

	return nil
}
