package middleware

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/xraph/forge/v0/pkg/ai"
	"github.com/xraph/forge/v0/pkg/common"
	"github.com/xraph/forge/v0/pkg/logger"
)

// IntelligentRateLimitConfig contains configuration for AI-powered rate limiting
type IntelligentRateLimitConfig struct {
	BaseLimit           int64         `yaml:"base_limit" default:"1000"`          // Base requests per window
	WindowDuration      time.Duration `yaml:"window_duration" default:"1m"`       // Time window
	BurstMultiplier     float64       `yaml:"burst_multiplier" default:"2.0"`     // Burst allowance multiplier
	LearningEnabled     bool          `yaml:"learning_enabled" default:"true"`    // Enable learning from patterns
	AdaptiveEnabled     bool          `yaml:"adaptive_enabled" default:"true"`    // Enable adaptive limits
	MinLimit            int64         `yaml:"min_limit" default:"100"`            // Minimum limit
	MaxLimit            int64         `yaml:"max_limit" default:"10000"`          // Maximum limit
	UserBehaviorWeight  float64       `yaml:"user_behavior_weight" default:"0.3"` // Weight for user behavior
	SystemLoadWeight    float64       `yaml:"system_load_weight" default:"0.4"`   // Weight for system load
	HistoricalWeight    float64       `yaml:"historical_weight" default:"0.3"`    // Weight for historical data
	ReputationThreshold float64       `yaml:"reputation_threshold" default:"0.7"` // Trust threshold for users
	AnomalyThreshold    float64       `yaml:"anomaly_threshold" default:"3.0"`    // Standard deviations for anomaly
}

// IntelligentRateLimitStats contains statistics for intelligent rate limiting
type IntelligentRateLimitStats struct {
	TotalRequests    int64                    `json:"total_requests"`
	RateLimitedCount int64                    `json:"rate_limited_count"`
	AdaptiveChanges  int64                    `json:"adaptive_changes"`
	UserLimits       map[string]RateLimitInfo `json:"user_limits"`
	EndpointLimits   map[string]RateLimitInfo `json:"endpoint_limits"`
	AverageLatency   time.Duration            `json:"average_latency"`
	FalsePositives   int64                    `json:"false_positives"`
	TruePositives    int64                    `json:"true_positives"`
	LearningMetrics  LearningMetrics          `json:"learning_metrics"`
	LastUpdated      time.Time                `json:"last_updated"`
}

// RateLimitInfo contains rate limit information for a user or endpoint
type RateLimitInfo struct {
	CurrentLimit    int64     `json:"current_limit"`
	RequestCount    int64     `json:"request_count"`
	WindowStart     time.Time `json:"window_start"`
	ReputationScore float64   `json:"reputation_score"`
	IsBlocked       bool      `json:"is_blocked"`
	BlockedUntil    time.Time `json:"blocked_until"`
	LastActivity    time.Time `json:"last_activity"`
	PatternScore    float64   `json:"pattern_score"`
}

// LearningMetrics contains metrics about the learning process
type LearningMetrics struct {
	ModelAccuracy      float64   `json:"model_accuracy"`
	PredictionCount    int64     `json:"prediction_count"`
	FeedbackReceived   int64     `json:"feedback_received"`
	LastModelUpdate    time.Time `json:"last_model_update"`
	TrainingDataPoints int64     `json:"training_data_points"`
}

// UserBehaviorPattern represents user behavior patterns
type UserBehaviorPattern struct {
	UserID             string    `json:"user_id"`
	TypicalRequestRate float64   `json:"typical_request_rate"`
	PeakRequestRate    float64   `json:"peak_request_rate"`
	RequestVariability float64   `json:"request_variability"`
	PreferredEndpoints []string  `json:"preferred_endpoints"`
	RequestTiming      []int     `json:"request_timing"` // Hours of activity
	ReputationScore    float64   `json:"reputation_score"`
	AnomalyScore       float64   `json:"anomaly_score"`
	LastAnalysis       time.Time `json:"last_analysis"`
}

// IntelligentRateLimit implements AI-powered rate limiting middleware
type IntelligentRateLimit struct {
	config         IntelligentRateLimitConfig
	agent          ai.AIAgent
	logger         common.Logger
	metrics        common.Metrics
	userLimits     map[string]*RateLimitInfo
	endpointLimits map[string]*RateLimitInfo
	userPatterns   map[string]*UserBehaviorPattern
	stats          IntelligentRateLimitStats
	systemLoad     *SystemLoadMonitor
	mu             sync.RWMutex
	started        bool
}

// SystemLoadMonitor monitors system load for adaptive rate limiting
type SystemLoadMonitor struct {
	CPUUsage    float64   `json:"cpu_usage"`
	MemoryUsage float64   `json:"memory_usage"`
	RequestLoad float64   `json:"request_load"`
	ErrorRate   float64   `json:"error_rate"`
	LastUpdate  time.Time `json:"last_update"`
}

// NewIntelligentRateLimit creates a new intelligent rate limiting middleware
func NewIntelligentRateLimit(config IntelligentRateLimitConfig, logger common.Logger, metrics common.Metrics) (*IntelligentRateLimit, error) {
	// Create learning agent for rate limiting
	capabilities := []ai.Capability{
		{
			Name:        "predict_user_behavior",
			Description: "Predict user behavior patterns for rate limiting",
			Metadata: map[string]interface{}{
				"type":     "prediction",
				"accuracy": 0.85,
			},
		},
		{
			Name:        "detect_anomalies",
			Description: "Detect anomalous request patterns",
			Metadata: map[string]interface{}{
				"type":      "anomaly_detection",
				"threshold": config.AnomalyThreshold,
			},
		},
		{
			Name:        "adaptive_limits",
			Description: "Dynamically adjust rate limits based on system conditions",
			Metadata: map[string]interface{}{
				"type":      "optimization",
				"min_limit": config.MinLimit,
				"max_limit": config.MaxLimit,
			},
		},
	}

	agent := ai.NewBaseAgent("intelligent-rate-limiter", "Intelligent Rate Limiter", ai.AgentTypeOptimization, capabilities)

	return &IntelligentRateLimit{
		config:         config,
		agent:          agent,
		logger:         logger,
		metrics:        metrics,
		userLimits:     make(map[string]*RateLimitInfo),
		endpointLimits: make(map[string]*RateLimitInfo),
		userPatterns:   make(map[string]*UserBehaviorPattern),
		systemLoad:     &SystemLoadMonitor{},
		stats: IntelligentRateLimitStats{
			UserLimits:     make(map[string]RateLimitInfo),
			EndpointLimits: make(map[string]RateLimitInfo),
			LastUpdated:    time.Now(),
		},
	}, nil
}

// Name returns the middleware name
func (irl *IntelligentRateLimit) Name() string {
	return "intelligent-rate-limit"
}

// Type returns the middleware type
func (irl *IntelligentRateLimit) Type() ai.AIMiddlewareType {
	return ai.AIMiddlewareTypeRateLimit
}

// Initialize initializes the middleware
func (irl *IntelligentRateLimit) Initialize(ctx context.Context, config ai.AIMiddlewareConfig) error {
	irl.mu.Lock()
	defer irl.mu.Unlock()

	if irl.started {
		return fmt.Errorf("intelligent rate limit middleware already initialized")
	}

	// Initialize the AI agent
	agentConfig := ai.AgentConfig{
		MaxConcurrency:  10,
		Timeout:         30 * time.Second,
		LearningEnabled: irl.config.LearningEnabled,
		AutoApply:       irl.config.AdaptiveEnabled,
		Logger:          irl.logger,
		Metrics:         irl.metrics,
	}

	if err := irl.agent.Initialize(ctx, agentConfig); err != nil {
		return fmt.Errorf("failed to initialize rate limit agent: %w", err)
	}

	if err := irl.agent.Start(ctx); err != nil {
		return fmt.Errorf("failed to start rate limit agent: %w", err)
	}

	// Start background monitoring
	go irl.monitorSystemLoad(ctx)
	go irl.analyzeUserPatterns(ctx)
	go irl.updateAdaptiveLimits(ctx)

	irl.started = true

	if irl.logger != nil {
		irl.logger.Info("intelligent rate limit middleware initialized",
			logger.Int64("base_limit", irl.config.BaseLimit),
			logger.Bool("learning_enabled", irl.config.LearningEnabled),
			logger.Bool("adaptive_enabled", irl.config.AdaptiveEnabled),
		)
	}

	return nil
}

// Process processes HTTP requests with intelligent rate limiting
func (irl *IntelligentRateLimit) Process(ctx context.Context, req *http.Request, resp http.ResponseWriter, next http.HandlerFunc) error {
	start := time.Now()

	// Extract user identification
	userID := irl.extractUserID(req)
	endpoint := irl.extractEndpoint(req)

	// Update statistics
	irl.updateRequestStats()

	// Check rate limits
	allowed, reason, waitTime := irl.checkRateLimit(ctx, userID, endpoint, req)
	if !allowed {
		irl.handleRateLimitExceeded(resp, reason, waitTime)
		irl.updateRateLimitStats(userID, endpoint, false)
		return nil
	}

	// Process request
	next.ServeHTTP(resp, req)

	// Record successful request
	irl.recordRequest(userID, endpoint, time.Since(start))
	irl.updateRateLimitStats(userID, endpoint, true)

	// Learn from request patterns if enabled
	if irl.config.LearningEnabled {
		go irl.learnFromRequest(ctx, userID, endpoint, req, resp)
	}

	return nil
}

// checkRateLimit checks if the request should be rate limited
func (irl *IntelligentRateLimit) checkRateLimit(ctx context.Context, userID, endpoint string, req *http.Request) (bool, string, time.Duration) {
	irl.mu.Lock()
	defer irl.mu.Unlock()

	// Get or create user limit info
	userLimit := irl.getUserLimitInfo(userID)
	endpointLimit := irl.getEndpointLimitInfo(endpoint)

	// Reset window if needed
	now := time.Now()
	if now.Sub(userLimit.WindowStart) >= irl.config.WindowDuration {
		userLimit.RequestCount = 0
		userLimit.WindowStart = now
	}
	if now.Sub(endpointLimit.WindowStart) >= irl.config.WindowDuration {
		endpointLimit.RequestCount = 0
		endpointLimit.WindowStart = now
	}

	// Check if user is currently blocked
	if userLimit.IsBlocked && now.Before(userLimit.BlockedUntil) {
		waitTime := userLimit.BlockedUntil.Sub(now)
		return false, "user_blocked", waitTime
	}

	// Calculate dynamic limits based on AI predictions
	userDynamicLimit := irl.calculateDynamicLimit(userID, userLimit)
	endpointDynamicLimit := irl.calculateDynamicLimit(endpoint, endpointLimit)

	// Apply the most restrictive limit
	effectiveLimit := userDynamicLimit
	if endpointDynamicLimit < effectiveLimit {
		effectiveLimit = endpointDynamicLimit
	}

	// Check limits
	if userLimit.RequestCount >= effectiveLimit {
		// Check if this might be anomalous behavior
		if irl.isAnomalousRequest(ctx, userID, req) {
			// Block user temporarily
			userLimit.IsBlocked = true
			userLimit.BlockedUntil = now.Add(time.Duration(effectiveLimit) * time.Second)
			return false, "anomalous_behavior", userLimit.BlockedUntil.Sub(now)
		}
		return false, "rate_limit_exceeded", irl.config.WindowDuration
	}

	// Allow request
	userLimit.RequestCount++
	userLimit.LastActivity = now
	endpointLimit.RequestCount++
	endpointLimit.LastActivity = now

	return true, "", 0
}

// calculateDynamicLimit calculates dynamic rate limit using AI
func (irl *IntelligentRateLimit) calculateDynamicLimit(identifier string, limitInfo *RateLimitInfo) int64 {
	baseLimit := irl.config.BaseLimit

	if !irl.config.AdaptiveEnabled {
		return baseLimit
	}

	// Factor in reputation score
	reputationFactor := limitInfo.ReputationScore
	if reputationFactor < irl.config.ReputationThreshold {
		reputationFactor = 0.5 // Reduce limit for low reputation users
	}

	// Factor in system load
	systemLoadFactor := 1.0
	if irl.systemLoad.CPUUsage > 0.8 || irl.systemLoad.MemoryUsage > 0.8 {
		systemLoadFactor = 0.5 // Reduce limits under high load
	}

	// Calculate adaptive limit
	adaptiveLimit := float64(baseLimit) * reputationFactor * systemLoadFactor

	// Apply bounds
	if adaptiveLimit < float64(irl.config.MinLimit) {
		adaptiveLimit = float64(irl.config.MinLimit)
	}
	if adaptiveLimit > float64(irl.config.MaxLimit) {
		adaptiveLimit = float64(irl.config.MaxLimit)
	}

	return int64(adaptiveLimit)
}

// isAnomalousRequest determines if a request is anomalous using AI
func (irl *IntelligentRateLimit) isAnomalousRequest(ctx context.Context, userID string, req *http.Request) bool {
	if !irl.config.LearningEnabled {
		return false
	}

	// Get user pattern
	pattern, exists := irl.userPatterns[userID]
	if !exists {
		return false // No pattern data yet
	}

	// Create input for anomaly detection
	input := ai.AgentInput{
		Type: "detect_anomalies",
		Data: map[string]interface{}{
			"user_id":      userID,
			"request_rate": pattern.TypicalRequestRate,
			"current_time": time.Now().Hour(),
			"endpoint":     irl.extractEndpoint(req),
			"user_agent":   req.UserAgent(),
			"ip_address":   irl.extractClientIP(req),
		},
		Context: map[string]interface{}{
			"threshold": irl.config.AnomalyThreshold,
		},
		RequestID: fmt.Sprintf("anomaly-%s-%d", userID, time.Now().UnixNano()),
		Timestamp: time.Now(),
	}

	output, err := irl.agent.Process(ctx, input)
	if err != nil {
		if irl.logger != nil {
			irl.logger.Error("failed to detect anomalies",
				logger.String("user_id", userID),
				logger.Error(err),
			)
		}
		return false
	}

	// Check if anomaly detected
	if anomalyData, ok := output.Data.(map[string]interface{}); ok {
		if isAnomaly, exists := anomalyData["is_anomaly"].(bool); exists {
			return isAnomaly && output.Confidence > 0.7
		}
	}

	return false
}

// getUserLimitInfo gets or creates user limit info
func (irl *IntelligentRateLimit) getUserLimitInfo(userID string) *RateLimitInfo {
	if limitInfo, exists := irl.userLimits[userID]; exists {
		return limitInfo
	}

	limitInfo := &RateLimitInfo{
		CurrentLimit:    irl.config.BaseLimit,
		RequestCount:    0,
		WindowStart:     time.Now(),
		ReputationScore: 0.8, // Default reputation
		IsBlocked:       false,
		LastActivity:    time.Now(),
		PatternScore:    0.5,
	}

	irl.userLimits[userID] = limitInfo
	return limitInfo
}

// getEndpointLimitInfo gets or creates endpoint limit info
func (irl *IntelligentRateLimit) getEndpointLimitInfo(endpoint string) *RateLimitInfo {
	if limitInfo, exists := irl.endpointLimits[endpoint]; exists {
		return limitInfo
	}

	limitInfo := &RateLimitInfo{
		CurrentLimit:    irl.config.BaseLimit,
		RequestCount:    0,
		WindowStart:     time.Now(),
		ReputationScore: 1.0, // Endpoints start with full reputation
		IsBlocked:       false,
		LastActivity:    time.Now(),
		PatternScore:    0.5,
	}

	irl.endpointLimits[endpoint] = limitInfo
	return limitInfo
}

// extractUserID extracts user ID from request
func (irl *IntelligentRateLimit) extractUserID(req *http.Request) string {
	// Try JWT token first
	if authHeader := req.Header.Get("Authorization"); authHeader != "" {
		if strings.HasPrefix(authHeader, "Bearer ") {
			// In a real implementation, you'd decode the JWT
			return fmt.Sprintf("jwt:%s", authHeader[7:20]) // Use part of token as ID
		}
	}

	// Try API key
	if apiKey := req.Header.Get("X-API-Key"); apiKey != "" {
		return fmt.Sprintf("api:%s", apiKey)
	}

	// Fallback to IP address
	return fmt.Sprintf("ip:%s", irl.extractClientIP(req))
}

// extractEndpoint extracts endpoint identifier from request
func (irl *IntelligentRateLimit) extractEndpoint(req *http.Request) string {
	return fmt.Sprintf("%s:%s", req.Method, req.URL.Path)
}

// extractClientIP extracts client IP address
func (irl *IntelligentRateLimit) extractClientIP(req *http.Request) string {
	// Check X-Forwarded-For header
	if xff := req.Header.Get("X-Forwarded-For"); xff != "" {
		ips := strings.Split(xff, ",")
		return strings.TrimSpace(ips[0])
	}

	// Check X-Real-IP header
	if xri := req.Header.Get("X-Real-IP"); xri != "" {
		return strings.TrimSpace(xri)
	}

	// Use RemoteAddr
	ip := req.RemoteAddr
	if colon := strings.LastIndex(ip, ":"); colon != -1 {
		ip = ip[:colon]
	}
	return ip
}

// handleRateLimitExceeded handles rate limit exceeded responses
func (irl *IntelligentRateLimit) handleRateLimitExceeded(resp http.ResponseWriter, reason string, waitTime time.Duration) {
	resp.Header().Set("Content-Type", "application/json")
	resp.Header().Set("X-Rate-Limit-Reason", reason)
	resp.Header().Set("X-Rate-Limit-Retry-After", fmt.Sprintf("%.0f", waitTime.Seconds()))
	resp.WriteHeader(http.StatusTooManyRequests)

	response := map[string]interface{}{
		"error": map[string]interface{}{
			"code":        "RATE_LIMIT_EXCEEDED",
			"message":     "Rate limit exceeded",
			"reason":      reason,
			"retry_after": waitTime.Seconds(),
		},
		"timestamp": time.Now().Format(time.RFC3339),
	}

	resp.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(resp).Encode(response); err != nil {
		if irl.logger != nil {
			irl.logger.Error("failed to write rate limit response", logger.Error(err))
		}
	}
}

// recordRequest records a successful request
func (irl *IntelligentRateLimit) recordRequest(userID, endpoint string, latency time.Duration) {
	// Update user pattern data
	if pattern, exists := irl.userPatterns[userID]; exists {
		pattern.LastAnalysis = time.Now()
		// Update request timing
		hour := time.Now().Hour()
		if hour < len(pattern.RequestTiming) {
			pattern.RequestTiming[hour]++
		}
	}
}

// updateRequestStats updates request statistics
func (irl *IntelligentRateLimit) updateRequestStats() {
	irl.mu.Lock()
	defer irl.mu.Unlock()

	irl.stats.TotalRequests++
	irl.stats.LastUpdated = time.Now()
}

// updateRateLimitStats updates rate limit statistics
func (irl *IntelligentRateLimit) updateRateLimitStats(userID, endpoint string, allowed bool) {
	irl.mu.Lock()
	defer irl.mu.Unlock()

	if !allowed {
		irl.stats.RateLimitedCount++
	}

	// Update user and endpoint stats
	if userLimit, exists := irl.userLimits[userID]; exists {
		irl.stats.UserLimits[userID] = *userLimit
	}
	if endpointLimit, exists := irl.endpointLimits[endpoint]; exists {
		irl.stats.EndpointLimits[endpoint] = *endpointLimit
	}

	if irl.metrics != nil {
		irl.metrics.Counter("forge.ai.rate_limit_total").Inc()
		if !allowed {
			irl.metrics.Counter("forge.ai.rate_limit_exceeded").Inc()
		}
	}
}

// monitorSystemLoad monitors system load for adaptive rate limiting
func (irl *IntelligentRateLimit) monitorSystemLoad(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			irl.updateSystemLoad()
		}
	}
}

// updateSystemLoad updates system load metrics
func (irl *IntelligentRateLimit) updateSystemLoad() {
	// In a real implementation, you'd get actual system metrics
	// For now, simulate system load
	irl.mu.Lock()
	defer irl.mu.Unlock()

	irl.systemLoad.LastUpdate = time.Now()
	// Simulate varying load
	irl.systemLoad.CPUUsage = 0.3 + (float64(time.Now().Unix()%60) / 100.0)
	irl.systemLoad.MemoryUsage = 0.4 + (float64(time.Now().Unix()%40) / 200.0)
	irl.systemLoad.RequestLoad = float64(irl.stats.TotalRequests) / 1000.0
}

// analyzeUserPatterns analyzes user behavior patterns
func (irl *IntelligentRateLimit) analyzeUserPatterns(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			irl.updateUserPatterns(ctx)
		}
	}
}

// updateUserPatterns updates user behavior patterns using AI
func (irl *IntelligentRateLimit) updateUserPatterns(ctx context.Context) {
	irl.mu.RLock()
	users := make([]string, 0, len(irl.userLimits))
	for userID := range irl.userLimits {
		users = append(users, userID)
	}
	irl.mu.RUnlock()

	for _, userID := range users {
		if _, exists := irl.userPatterns[userID]; !exists {
			irl.userPatterns[userID] = &UserBehaviorPattern{
				UserID:          userID,
				RequestTiming:   make([]int, 24),
				ReputationScore: 0.8,
				LastAnalysis:    time.Now(),
			}
		}

		// Analyze pattern using AI agent
		input := ai.AgentInput{
			Type: "predict_user_behavior",
			Data: map[string]interface{}{
				"user_id":      userID,
				"request_data": irl.userLimits[userID],
			},
			Context: map[string]interface{}{
				"analysis_type": "pattern_update",
			},
			RequestID: fmt.Sprintf("pattern-%s-%d", userID, time.Now().UnixNano()),
			Timestamp: time.Now(),
		}

		output, err := irl.agent.Process(ctx, input)
		if err != nil {
			if irl.logger != nil {
				irl.logger.Error("failed to analyze user pattern",
					logger.String("user_id", userID),
					logger.Error(err),
				)
			}
			continue
		}

		// Update pattern based on AI output
		if patternData, ok := output.Data.(map[string]interface{}); ok {
			pattern := irl.userPatterns[userID]
			if rate, exists := patternData["typical_rate"].(float64); exists {
				pattern.TypicalRequestRate = rate
			}
			if reputation, exists := patternData["reputation"].(float64); exists {
				pattern.ReputationScore = reputation
			}
			pattern.LastAnalysis = time.Now()
		}
	}
}

// updateAdaptiveLimits updates adaptive rate limits based on AI analysis
func (irl *IntelligentRateLimit) updateAdaptiveLimits(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if irl.config.AdaptiveEnabled {
				irl.adjustAdaptiveLimits(ctx)
			}
		}
	}
}

// adjustAdaptiveLimits adjusts rate limits adaptively
func (irl *IntelligentRateLimit) adjustAdaptiveLimits(ctx context.Context) {
	input := ai.AgentInput{
		Type: "adaptive_limits",
		Data: map[string]interface{}{
			"system_load":   irl.systemLoad,
			"user_patterns": irl.userPatterns,
			"stats":         irl.stats,
		},
		Context: map[string]interface{}{
			"adjustment_type": "global_optimization",
		},
		RequestID: fmt.Sprintf("adaptive-%d", time.Now().UnixNano()),
		Timestamp: time.Now(),
	}

	output, err := irl.agent.Process(ctx, input)
	if err != nil {
		if irl.logger != nil {
			irl.logger.Error("failed to adjust adaptive limits", logger.Error(err))
		}
		return
	}

	// Apply adaptive changes
	if adjustments, ok := output.Data.(map[string]interface{}); ok {
		irl.applyAdaptiveAdjustments(adjustments)
	}
}

// applyAdaptiveAdjustments applies adaptive limit adjustments
func (irl *IntelligentRateLimit) applyAdaptiveAdjustments(adjustments map[string]interface{}) {
	irl.mu.Lock()
	defer irl.mu.Unlock()

	if globalMultiplier, exists := adjustments["global_multiplier"].(float64); exists {
		// Apply global multiplier to base limit
		newBaseLimit := int64(float64(irl.config.BaseLimit) * globalMultiplier)
		if newBaseLimit >= irl.config.MinLimit && newBaseLimit <= irl.config.MaxLimit {
			irl.config.BaseLimit = newBaseLimit
			irl.stats.AdaptiveChanges++
		}
	}

	if irl.logger != nil {
		irl.logger.Debug("applied adaptive rate limit adjustments",
			logger.Int64("new_base_limit", irl.config.BaseLimit),
			logger.Int64("adaptive_changes", irl.stats.AdaptiveChanges),
		)
	}
}

// learnFromRequest learns from individual requests
func (irl *IntelligentRateLimit) learnFromRequest(ctx context.Context, userID, endpoint string, req *http.Request, resp http.ResponseWriter) {
	// Create feedback based on request outcome
	feedback := ai.AgentFeedback{
		ActionID: fmt.Sprintf("request-%s-%d", userID, time.Now().UnixNano()),
		Success:  resp.Header().Get("Status") != "429", // Not rate limited
		Context: map[string]interface{}{
			"user_id":  userID,
			"endpoint": endpoint,
			"method":   req.Method,
		},
		Timestamp: time.Now(),
	}

	if err := irl.agent.Learn(ctx, feedback); err != nil {
		if irl.logger != nil {
			irl.logger.Error("failed to learn from request",
				logger.String("user_id", userID),
				logger.Error(err),
			)
		}
	}
}

// GetStats returns middleware statistics
func (irl *IntelligentRateLimit) GetStats() ai.AIMiddlewareStats {
	irl.mu.RLock()
	defer irl.mu.RUnlock()

	return ai.AIMiddlewareStats{
		Name:            irl.Name(),
		Type:            string(irl.Type()),
		RequestsTotal:   irl.stats.TotalRequests,
		RequestsBlocked: irl.stats.RateLimitedCount,
		AverageLatency:  irl.stats.AverageLatency,
		LearningEnabled: irl.config.LearningEnabled,
		AdaptiveChanges: irl.stats.AdaptiveChanges,
		LastUpdated:     irl.stats.LastUpdated,
		CustomMetrics: map[string]interface{}{
			"users_tracked":     len(irl.userLimits),
			"endpoints_tracked": len(irl.endpointLimits),
			"false_positives":   irl.stats.FalsePositives,
			"true_positives":    irl.stats.TruePositives,
			"learning_metrics":  irl.stats.LearningMetrics,
		},
	}
}
