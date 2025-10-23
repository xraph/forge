package middleware

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sync"
	"time"

	"github.com/xraph/forge"
	ai "github.com/xraph/forge/extensions/ai/internal"
	"github.com/xraph/forge/internal/logger"
)

// AdaptiveLoadBalanceConfig contains configuration for AI-powered load balancing
type AdaptiveLoadBalanceConfig struct {
	Algorithm           string        `yaml:"algorithm" default:"ai_weighted"`        // Load balancing algorithm
	HealthCheckEnabled  bool          `yaml:"health_check_enabled" default:"true"`    // Enable health checks
	HealthCheckInterval time.Duration `yaml:"health_check_interval" default:"30s"`    // Health check interval
	HealthCheckTimeout  time.Duration `yaml:"health_check_timeout" default:"5s"`      // Health check timeout
	LearningEnabled     bool          `yaml:"learning_enabled" default:"true"`        // Enable learning from performance
	AdaptiveEnabled     bool          `yaml:"adaptive_enabled" default:"true"`        // Enable adaptive weight adjustment
	WeightAdjustment    float64       `yaml:"weight_adjustment" default:"0.1"`        // Weight adjustment factor
	MinWeight           float64       `yaml:"min_weight" default:"0.1"`               // Minimum weight for a backend
	MaxWeight           float64       `yaml:"max_weight" default:"2.0"`               // Maximum weight for a backend
	ResponseTimeWeight  float64       `yaml:"response_time_weight" default:"0.4"`     // Weight for response time factor
	ErrorRateWeight     float64       `yaml:"error_rate_weight" default:"0.3"`        // Weight for error rate factor
	CapacityWeight      float64       `yaml:"capacity_weight" default:"0.3"`          // Weight for capacity factor
	PredictionWindow    time.Duration `yaml:"prediction_window" default:"5m"`         // Window for performance prediction
	StickySessions      bool          `yaml:"sticky_sessions" default:"false"`        // Enable sticky sessions
	SessionCookie       string        `yaml:"session_cookie" default:"forge_session"` // Session cookie name
}

// BackendServer represents a backend server
type BackendServer struct {
	ID                string                 `json:"id"`
	URL               *url.URL               `json:"url"`
	Weight            float64                `json:"weight"`
	BaseWeight        float64                `json:"base_weight"`
	IsHealthy         bool                   `json:"is_healthy"`
	LastHealthCheck   time.Time              `json:"last_health_check"`
	ResponseTimes     []time.Duration        `json:"-"` // Recent response times
	ErrorCount        int64                  `json:"error_count"`
	RequestCount      int64                  `json:"request_count"`
	ActiveConnections int64                  `json:"active_connections"`
	Capacity          float64                `json:"capacity"`       // Server capacity (0.0-1.0)
	LoadScore         float64                `json:"load_score"`     // Current load score
	PredictedLoad     float64                `json:"predicted_load"` // Predicted load
	Metadata          map[string]interface{} `json:"metadata"`
	Proxy             *httputil.ReverseProxy `json:"-"`
	mu                sync.RWMutex           `json:"-"`
}

// LoadBalancingStats contains statistics for load balancing
type LoadBalancingStats struct {
	TotalRequests      int64                    `json:"total_requests"`
	BackendStats       map[string]*BackendStats `json:"backend_stats"`
	AlgorithmChanges   int64                    `json:"algorithm_changes"`
	WeightAdjustments  int64                    `json:"weight_adjustments"`
	HealthChecksFailed int64                    `json:"health_checks_failed"`
	AverageLatency     time.Duration            `json:"average_latency"`
	LoadDistribution   map[string]float64       `json:"load_distribution"`
	PredictionAccuracy float64                  `json:"prediction_accuracy"`
	LastUpdated        time.Time                `json:"last_updated"`
}

// BackendStats contains statistics for a backend server
type BackendStats struct {
	RequestCount   int64         `json:"request_count"`
	ErrorCount     int64         `json:"error_count"`
	AverageLatency time.Duration `json:"average_latency"`
	CurrentWeight  float64       `json:"current_weight"`
	HealthStatus   string        `json:"health_status"`
	LoadPercentage float64       `json:"load_percentage"`
	LastUsed       time.Time     `json:"last_used"`
}

// LoadBalancingDecision represents a load balancing decision
type LoadBalancingDecision struct {
	SelectedBackend  *BackendServer     `json:"selected_backend"`
	Algorithm        string             `json:"algorithm"`
	Confidence       float64            `json:"confidence"`
	Factors          map[string]float64 `json:"factors"`
	PredictedLatency time.Duration      `json:"predicted_latency"`
	Reason           string             `json:"reason"`
	Timestamp        time.Time          `json:"timestamp"`
}

// AdaptiveLoadBalance implements AI-powered adaptive load balancing
type AdaptiveLoadBalance struct {
	config       AdaptiveLoadBalanceConfig
	agent        ai.AIAgent
	logger       forge.Logger
	metrics      forge.Metrics
	backends     []*BackendServer
	stats        LoadBalancingStats
	sessionStore map[string]string // session_id -> backend_id
	currentIndex int               // For round-robin fallback
	mu           sync.RWMutex
	started      bool
}

// NewAdaptiveLoadBalance creates a new adaptive load balancing middleware
func NewAdaptiveLoadBalance(config AdaptiveLoadBalanceConfig, backends []string, logger logger.Logger, metrics forge.Metrics) (*AdaptiveLoadBalance, error) {
	// Create load balancing agent
	capabilities := []ai.Capability{
		{
			Name:        "predict_backend_performance",
			Description: "Predict backend server performance metrics",
			Metadata: map[string]interface{}{
				"type":              "prediction",
				"accuracy":          0.85,
				"prediction_window": config.PredictionWindow,
			},
		},
		{
			Name:        "optimize_weights",
			Description: "Optimize backend server weights based on performance",
			Metadata: map[string]interface{}{
				"type":       "optimization",
				"min_weight": config.MinWeight,
				"max_weight": config.MaxWeight,
			},
		},
		{
			Name:        "select_optimal_backend",
			Description: "Select optimal backend server for request routing",
			Metadata: map[string]interface{}{
				"type":      "decision_making",
				"algorithm": config.Algorithm,
			},
		},
	}

	agent := ai.NewBaseAgent("adaptive-load-balancer", "Adaptive Load Balancer", ai.AgentTypeLoadBalancer, capabilities)

	// Initialize backend servers
	backendServers := make([]*BackendServer, 0, len(backends))
	for i, backendURL := range backends {
		parsedURL, err := url.Parse(backendURL)
		if err != nil {
			return nil, fmt.Errorf("invalid backend URL %s: %w", backendURL, err)
		}

		backend := &BackendServer{
			ID:            fmt.Sprintf("backend-%d", i),
			URL:           parsedURL,
			Weight:        1.0,
			BaseWeight:    1.0,
			IsHealthy:     true,
			ResponseTimes: make([]time.Duration, 0, 100),
			Capacity:      1.0,
			Metadata:      make(map[string]interface{}),
			Proxy:         httputil.NewSingleHostReverseProxy(parsedURL),
		}

		// Configure proxy error handler
		backend.Proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
			backend.mu.Lock()
			backend.ErrorCount++
			backend.IsHealthy = false
			backend.mu.Unlock()

			if logger != nil {
				logger.Error("backend proxy error: " + err.Error())
			}

			w.WriteHeader(http.StatusBadGateway)
		}

		backendServers = append(backendServers, backend)
	}

	return &AdaptiveLoadBalance{
		config:       config,
		agent:        agent,
		logger:       logger,
		metrics:      metrics,
		backends:     backendServers,
		sessionStore: make(map[string]string),
		stats: LoadBalancingStats{
			BackendStats:     make(map[string]*BackendStats),
			LoadDistribution: make(map[string]float64),
			LastUpdated:      time.Now(),
		},
	}, nil
}

// Name returns the middleware name
func (alb *AdaptiveLoadBalance) Name() string {
	return "adaptive-load-balance"
}

// Type returns the middleware type
func (alb *AdaptiveLoadBalance) Type() ai.AIMiddlewareType {
	return ai.AIMiddlewareTypeLoadBalance
}

// Initialize initializes the middleware
func (alb *AdaptiveLoadBalance) Initialize(ctx context.Context, config ai.AIMiddlewareConfig) error {
	alb.mu.Lock()
	defer alb.mu.Unlock()

	if alb.started {
		return fmt.Errorf("adaptive load balance middleware already initialized")
	}

	// Initialize the AI agent
	agentConfig := ai.AgentConfig{
		MaxConcurrency:  20,
		Timeout:         10 * time.Second,
		LearningEnabled: alb.config.LearningEnabled,
		AutoApply:       alb.config.AdaptiveEnabled,
		Logger:          alb.logger,
		Metrics:         alb.metrics,
	}

	if err := alb.agent.Initialize(ctx, agentConfig); err != nil {
		return fmt.Errorf("failed to initialize load balance agent: %w", err)
	}

	if err := alb.agent.Start(ctx); err != nil {
		return fmt.Errorf("failed to start load balance agent: %w", err)
	}

	// Initialize backend statistics
	for _, backend := range alb.backends {
		alb.stats.BackendStats[backend.ID] = &BackendStats{
			HealthStatus: "healthy",
			LastUsed:     time.Now(),
		}
	}

	// Start background tasks
	if alb.config.HealthCheckEnabled {
		go alb.healthCheckLoop(ctx)
	}
	go alb.performanceMonitorLoop(ctx)
	go alb.weightOptimizationLoop(ctx)

	alb.started = true

	if alb.logger != nil {
		alb.logger.Info("adaptive load balance middleware initialized",
			logger.String("algorithm", alb.config.Algorithm),
			logger.Int("backends", len(alb.backends)),
			logger.Bool("learning_enabled", alb.config.LearningEnabled),
		)
	}

	return nil
}

// Process processes HTTP requests with adaptive load balancing
func (alb *AdaptiveLoadBalance) Process(ctx context.Context, req *http.Request, resp http.ResponseWriter, next http.HandlerFunc) error {
	start := time.Now()

	// Select optimal backend using AI
	decision, err := alb.selectOptimalBackend(ctx, req)
	if err != nil {
		if alb.logger != nil {
			alb.logger.Error("failed to select backend", logger.Error(err))
		}
		return err
	}

	backend := decision.SelectedBackend
	if backend == nil {
		return fmt.Errorf("no healthy backend available")
	}

	// Update backend connection count
	backend.mu.Lock()
	backend.ActiveConnections++
	backend.mu.Unlock()

	// Update session mapping if sticky sessions are enabled
	if alb.config.StickySessions {
		sessionID := alb.extractSessionID(req)
		if sessionID != "" {
			alb.mu.Lock()
			alb.sessionStore[sessionID] = backend.ID
			alb.mu.Unlock()
		}
	}

	// Proxy request to selected backend
	backend.Proxy.ServeHTTP(resp, req)

	// Record performance metrics
	latency := time.Since(start)
	alb.recordBackendPerformance(backend, latency, resp.Header().Get("Status") != "")

	// Learn from request if enabled
	if alb.config.LearningEnabled {
		go alb.learnFromRequest(ctx, decision, latency, resp)
	}

	// Update statistics
	alb.updateStats(backend, latency)

	return nil
}

// selectOptimalBackend selects the optimal backend using AI
func (alb *AdaptiveLoadBalance) selectOptimalBackend(ctx context.Context, req *http.Request) (*LoadBalancingDecision, error) {
	// Check for sticky session first
	if alb.config.StickySessions {
		if backend := alb.getStickyBackend(req); backend != nil && backend.IsHealthy {
			return &LoadBalancingDecision{
				SelectedBackend: backend,
				Algorithm:       "sticky_session",
				Confidence:      1.0,
				Reason:          "sticky session routing",
				Timestamp:       time.Now(),
			}, nil
		}
	}

	// Get healthy backends
	healthyBackends := alb.getHealthyBackends()
	if len(healthyBackends) == 0 {
		return nil, fmt.Errorf("no healthy backends available")
	}

	// Use AI to select optimal backend
	input := ai.AgentInput{
		Type: "select_optimal_backend",
		Data: map[string]interface{}{
			"backends":  healthyBackends,
			"request":   alb.extractRequestFeatures(req),
			"algorithm": alb.config.Algorithm,
			"weights":   alb.getBackendWeights(),
		},
		Context: map[string]interface{}{
			"selection_criteria": map[string]interface{}{
				"response_time_weight": alb.config.ResponseTimeWeight,
				"error_rate_weight":    alb.config.ErrorRateWeight,
				"capacity_weight":      alb.config.CapacityWeight,
			},
		},
		RequestID: fmt.Sprintf("select-%d", time.Now().UnixNano()),
		Timestamp: time.Now(),
	}

	output, err := alb.agent.Process(ctx, input)
	if err != nil {
		// Fallback to simple round-robin
		return alb.roundRobinSelection(), nil
	}

	// Parse AI output
	if selectionData, ok := output.Data.(map[string]interface{}); ok {
		if backendID, exists := selectionData["backend_id"].(string); exists {
			if backend := alb.getBackendByID(backendID); backend != nil {
				factors := make(map[string]float64)
				if factorData, ok := selectionData["factors"].(map[string]interface{}); ok {
					for k, v := range factorData {
						if score, ok := v.(float64); ok {
							factors[k] = score
						}
					}
				}

				predictedLatency := time.Millisecond * 100 // Default
				if latency, exists := selectionData["predicted_latency"].(float64); exists {
					predictedLatency = time.Duration(latency) * time.Millisecond
				}

				return &LoadBalancingDecision{
					SelectedBackend:  backend,
					Algorithm:        alb.config.Algorithm,
					Confidence:       output.Confidence,
					Factors:          factors,
					PredictedLatency: predictedLatency,
					Reason:           output.Explanation,
					Timestamp:        time.Now(),
				}, nil
			}
		}
	}

	// Fallback to round-robin
	return alb.roundRobinSelection(), nil
}

// getStickyBackend gets the backend for sticky session
func (alb *AdaptiveLoadBalance) getStickyBackend(req *http.Request) *BackendServer {
	sessionID := alb.extractSessionID(req)
	if sessionID == "" {
		return nil
	}

	alb.mu.RLock()
	backendID, exists := alb.sessionStore[sessionID]
	alb.mu.RUnlock()

	if !exists {
		return nil
	}

	return alb.getBackendByID(backendID)
}

// extractSessionID extracts session ID from request
func (alb *AdaptiveLoadBalance) extractSessionID(req *http.Request) string {
	// Try cookie first
	if cookie, err := req.Cookie(alb.config.SessionCookie); err == nil {
		return cookie.Value
	}

	// Try header
	return req.Header.Get("X-Session-ID")
}

// getHealthyBackends returns list of healthy backends
func (alb *AdaptiveLoadBalance) getHealthyBackends() []*BackendServer {
	alb.mu.RLock()
	defer alb.mu.RUnlock()

	healthy := make([]*BackendServer, 0, len(alb.backends))
	for _, backend := range alb.backends {
		backend.mu.RLock()
		isHealthy := backend.IsHealthy
		backend.mu.RUnlock()

		if isHealthy {
			healthy = append(healthy, backend)
		}
	}

	return healthy
}

// getBackendWeights returns current backend weights
func (alb *AdaptiveLoadBalance) getBackendWeights() map[string]float64 {
	weights := make(map[string]float64)
	for _, backend := range alb.backends {
		backend.mu.RLock()
		weights[backend.ID] = backend.Weight
		backend.mu.RUnlock()
	}
	return weights
}

// getBackendByID finds backend by ID
func (alb *AdaptiveLoadBalance) getBackendByID(backendID string) *BackendServer {
	for _, backend := range alb.backends {
		if backend.ID == backendID {
			return backend
		}
	}
	return nil
}

// roundRobinSelection provides fallback round-robin selection
func (alb *AdaptiveLoadBalance) roundRobinSelection() *LoadBalancingDecision {
	healthyBackends := alb.getHealthyBackends()
	if len(healthyBackends) == 0 {
		return nil
	}

	alb.mu.Lock()
	backend := healthyBackends[alb.currentIndex%len(healthyBackends)]
	alb.currentIndex++
	alb.mu.Unlock()

	return &LoadBalancingDecision{
		SelectedBackend: backend,
		Algorithm:       "round_robin_fallback",
		Confidence:      0.8,
		Reason:          "AI selection failed, using round-robin fallback",
		Timestamp:       time.Now(),
	}
}

// extractRequestFeatures extracts features from HTTP request for AI analysis
func (alb *AdaptiveLoadBalance) extractRequestFeatures(req *http.Request) map[string]interface{} {
	return map[string]interface{}{
		"method":         req.Method,
		"path":           req.URL.Path,
		"content_length": req.ContentLength,
		"user_agent":     req.UserAgent(),
		"remote_addr":    req.RemoteAddr,
		"headers":        len(req.Header),
		"timestamp":      time.Now().Unix(),
	}
}

// recordBackendPerformance records performance metrics for a backend
func (alb *AdaptiveLoadBalance) recordBackendPerformance(backend *BackendServer, latency time.Duration, success bool) {
	backend.mu.Lock()
	defer backend.mu.Unlock()

	backend.RequestCount++
	backend.ActiveConnections--

	if !success {
		backend.ErrorCount++
	}

	// Update response times (keep last 100)
	backend.ResponseTimes = append(backend.ResponseTimes, latency)
	if len(backend.ResponseTimes) > 100 {
		backend.ResponseTimes = backend.ResponseTimes[1:]
	}

	// Calculate load score
	backend.LoadScore = alb.calculateLoadScore(backend)
}

// calculateLoadScore calculates load score for a backend
func (alb *AdaptiveLoadBalance) calculateLoadScore(backend *BackendServer) float64 {
	if backend.RequestCount == 0 {
		return 0.0
	}

	// Calculate average response time
	var avgResponseTime time.Duration
	if len(backend.ResponseTimes) > 0 {
		var total time.Duration
		for _, rt := range backend.ResponseTimes {
			total += rt
		}
		avgResponseTime = total / time.Duration(len(backend.ResponseTimes))
	}

	// Calculate error rate
	errorRate := float64(backend.ErrorCount) / float64(backend.RequestCount)

	// Calculate capacity utilization
	capacityUtil := float64(backend.ActiveConnections) / (backend.Capacity * 100)

	// Weighted load score
	responseTimeScore := math.Min(avgResponseTime.Seconds()/10.0, 1.0) // Normalize to 0-1
	errorRateScore := math.Min(errorRate*10, 1.0)                      // Normalize to 0-1
	capacityScore := math.Min(capacityUtil, 1.0)

	loadScore := (responseTimeScore * alb.config.ResponseTimeWeight) +
		(errorRateScore * alb.config.ErrorRateWeight) +
		(capacityScore * alb.config.CapacityWeight)

	return math.Min(loadScore, 1.0)
}

// healthCheckLoop performs periodic health checks
func (alb *AdaptiveLoadBalance) healthCheckLoop(ctx context.Context) {
	ticker := time.NewTicker(alb.config.HealthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			alb.performHealthChecks()
		}
	}
}

// performHealthChecks checks health of all backends
func (alb *AdaptiveLoadBalance) performHealthChecks() {
	for _, backend := range alb.backends {
		go alb.checkBackendHealth(backend)
	}
}

// checkBackendHealth checks health of a single backend
func (alb *AdaptiveLoadBalance) checkBackendHealth(backend *BackendServer) {
	start := time.Now()

	// Create health check URL
	healthURL := *backend.URL
	healthURL.Path = "/health"

	// Create HTTP client with timeout
	client := &http.Client{
		Timeout: alb.config.HealthCheckTimeout,
	}

	// Perform health check
	resp, err := client.Get(healthURL.String())
	latency := time.Since(start)

	backend.mu.Lock()
	backend.LastHealthCheck = time.Now()

	if err != nil || resp.StatusCode >= 400 {
		backend.IsHealthy = false
		if alb.stats.BackendStats[backend.ID] != nil {
			alb.stats.BackendStats[backend.ID].HealthStatus = "unhealthy"
		}
		alb.mu.Lock()
		alb.stats.HealthChecksFailed++
		alb.mu.Unlock()

		if alb.logger != nil {
			alb.logger.Warn("backend health check failed",
				logger.String("backend_id", backend.ID),
				logger.String("url", backend.URL.String()),
				logger.Duration("latency", latency),
				logger.Error(err),
			)
		}
	} else {
		backend.IsHealthy = true
		if alb.stats.BackendStats[backend.ID] != nil {
			alb.stats.BackendStats[backend.ID].HealthStatus = "healthy"
		}
	}

	backend.mu.Unlock()

	if resp != nil {
		resp.Body.Close()
	}
}

// performanceMonitorLoop monitors backend performance
func (alb *AdaptiveLoadBalance) performanceMonitorLoop(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			alb.updatePerformanceMetrics(ctx)
		}
	}
}

// updatePerformanceMetrics updates performance metrics
func (alb *AdaptiveLoadBalance) updatePerformanceMetrics(ctx context.Context) {
	for _, backend := range alb.backends {
		stats := alb.stats.BackendStats[backend.ID]
		if stats == nil {
			continue
		}

		backend.mu.RLock()
		stats.RequestCount = backend.RequestCount
		stats.ErrorCount = backend.ErrorCount
		stats.CurrentWeight = backend.Weight
		if len(backend.ResponseTimes) > 0 {
			var total time.Duration
			for _, rt := range backend.ResponseTimes {
				total += rt
			}
			stats.AverageLatency = total / time.Duration(len(backend.ResponseTimes))
		}
		backend.mu.RUnlock()

		// Calculate load percentage
		if alb.stats.TotalRequests > 0 {
			stats.LoadPercentage = float64(stats.RequestCount) / float64(alb.stats.TotalRequests) * 100
			alb.stats.LoadDistribution[backend.ID] = stats.LoadPercentage
		}
	}

	alb.stats.LastUpdated = time.Now()
}

// weightOptimizationLoop optimizes backend weights using AI
func (alb *AdaptiveLoadBalance) weightOptimizationLoop(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if alb.config.AdaptiveEnabled {
				alb.optimizeWeights(ctx)
			}
		}
	}
}

// optimizeWeights optimizes backend weights using AI
func (alb *AdaptiveLoadBalance) optimizeWeights(ctx context.Context) {
	// Prepare performance data
	performanceData := make(map[string]interface{})
	for _, backend := range alb.backends {
		backend.mu.RLock()
		performanceData[backend.ID] = map[string]interface{}{
			"current_weight":     backend.Weight,
			"load_score":         backend.LoadScore,
			"request_count":      backend.RequestCount,
			"error_count":        backend.ErrorCount,
			"active_connections": backend.ActiveConnections,
			"is_healthy":         backend.IsHealthy,
		}
		backend.mu.RUnlock()
	}

	input := ai.AgentInput{
		Type: "optimize_weights",
		Data: map[string]interface{}{
			"performance_data": performanceData,
			"current_weights":  alb.getBackendWeights(),
			"constraints": map[string]interface{}{
				"min_weight": alb.config.MinWeight,
				"max_weight": alb.config.MaxWeight,
			},
		},
		Context: map[string]interface{}{
			"optimization_goal": "minimize_latency_and_errors",
		},
		RequestID: fmt.Sprintf("optimize-%d", time.Now().UnixNano()),
		Timestamp: time.Now(),
	}

	output, err := alb.agent.Process(ctx, input)
	if err != nil {
		if alb.logger != nil {
			alb.logger.Error("failed to optimize weights", logger.Error(err))
		}
		return
	}

	// Apply weight optimizations
	if optimizedWeights, ok := output.Data.(map[string]interface{}); ok {
		alb.applyWeightOptimizations(optimizedWeights, output.Confidence)
	}
}

// applyWeightOptimizations applies AI-recommended weight optimizations
func (alb *AdaptiveLoadBalance) applyWeightOptimizations(optimizedWeights map[string]interface{}, confidence float64) {
	if confidence < 0.7 {
		return // Only apply high-confidence optimizations
	}

	adjustmentsMade := 0
	for backendID, weightValue := range optimizedWeights {
		if weight, ok := weightValue.(float64); ok {
			if backend := alb.getBackendByID(backendID); backend != nil {
				backend.mu.Lock()
				oldWeight := backend.Weight

				// Apply gradual adjustment
				adjustment := (weight - oldWeight) * alb.config.WeightAdjustment
				newWeight := oldWeight + adjustment

				// Apply bounds
				if newWeight < alb.config.MinWeight {
					newWeight = alb.config.MinWeight
				}
				if newWeight > alb.config.MaxWeight {
					newWeight = alb.config.MaxWeight
				}

				if math.Abs(newWeight-oldWeight) > 0.01 {
					backend.Weight = newWeight
					adjustmentsMade++
				}

				backend.mu.Unlock()
			}
		}
	}

	if adjustmentsMade > 0 {
		alb.mu.Lock()
		alb.stats.WeightAdjustments++
		alb.mu.Unlock()

		if alb.logger != nil {
			alb.logger.Debug("applied weight optimizations",
				logger.Int("adjustments_made", adjustmentsMade),
				logger.Float64("confidence", confidence),
			)
		}
	}
}

// learnFromRequest learns from request outcome
func (alb *AdaptiveLoadBalance) learnFromRequest(ctx context.Context, decision *LoadBalancingDecision, actualLatency time.Duration, resp http.ResponseWriter) {
	// Create feedback based on actual vs predicted performance
	success := resp.Header().Get("Status") == "" // No error status
	latencyDiff := math.Abs(actualLatency.Seconds() - decision.PredictedLatency.Seconds())

	feedback := ai.AgentFeedback{
		ActionID: fmt.Sprintf("routing-%s-%d", decision.SelectedBackend.ID, time.Now().UnixNano()),
		Success:  success && latencyDiff < 1.0, // Success if no errors and latency prediction was close
		Outcome: map[string]interface{}{
			"actual_latency":    actualLatency.Seconds(),
			"predicted_latency": decision.PredictedLatency.Seconds(),
			"latency_diff":      latencyDiff,
			"backend_id":        decision.SelectedBackend.ID,
		},
		Metrics: map[string]float64{
			"prediction_accuracy": 1.0 - math.Min(latencyDiff, 1.0),
			"routing_success":     map[bool]float64{true: 1.0, false: 0.0}[success],
		},
		Context: map[string]interface{}{
			"algorithm":  decision.Algorithm,
			"confidence": decision.Confidence,
		},
		Timestamp: time.Now(),
	}

	if err := alb.agent.Learn(ctx, feedback); err != nil {
		if alb.logger != nil {
			alb.logger.Error("failed to learn from request", logger.Error(err))
		}
	}
}

// updateStats updates load balancing statistics
func (alb *AdaptiveLoadBalance) updateStats(backend *BackendServer, latency time.Duration) {
	alb.mu.Lock()
	defer alb.mu.Unlock()

	alb.stats.TotalRequests++

	// Update average latency
	if alb.stats.TotalRequests == 1 {
		alb.stats.AverageLatency = latency
	} else {
		// Moving average
		alb.stats.AverageLatency = time.Duration(
			(int64(alb.stats.AverageLatency)*int64(alb.stats.TotalRequests-1) + int64(latency)) / int64(alb.stats.TotalRequests),
		)
	}

	// Update backend stats
	if stats := alb.stats.BackendStats[backend.ID]; stats != nil {
		stats.LastUsed = time.Now()
	}

	if alb.metrics != nil {
		alb.metrics.Counter("forge.ai.load_balance_total").Inc()
		alb.metrics.Histogram("forge.ai.load_balance_latency").Observe(latency.Seconds())
		alb.metrics.Counter("forge.ai.backend_requests", "backend_id", backend.ID).Inc()
	}
}

// GetStats returns middleware statistics
func (alb *AdaptiveLoadBalance) GetStats() ai.AIMiddlewareStats {
	alb.mu.RLock()
	defer alb.mu.RUnlock()

	backendHealthCounts := make(map[string]int)
	for _, backend := range alb.backends {
		backend.mu.RLock()
		if backend.IsHealthy {
			backendHealthCounts["healthy"]++
		} else {
			backendHealthCounts["unhealthy"]++
		}
		backend.mu.RUnlock()
	}

	return ai.AIMiddlewareStats{
		Name:            alb.Name(),
		Type:            string(alb.Type()),
		RequestsTotal:   alb.stats.TotalRequests,
		RequestsBlocked: 0, // Load balancer doesn't block requests
		AverageLatency:  alb.stats.AverageLatency,
		LearningEnabled: alb.config.LearningEnabled,
		AdaptiveChanges: alb.stats.WeightAdjustments,
		LastUpdated:     alb.stats.LastUpdated,
		CustomMetrics: map[string]interface{}{
			"backends_total":       len(alb.backends),
			"backends_healthy":     backendHealthCounts["healthy"],
			"backends_unhealthy":   backendHealthCounts["unhealthy"],
			"algorithm":            alb.config.Algorithm,
			"sticky_sessions":      alb.config.StickySessions,
			"health_checks_failed": alb.stats.HealthChecksFailed,
			"load_distribution":    alb.stats.LoadDistribution,
			"prediction_accuracy":  alb.stats.PredictionAccuracy,
		},
	}
}
