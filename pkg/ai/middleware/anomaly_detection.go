package middleware

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/xraph/forge/pkg/ai"
	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/logger"
)

// AnomalyDetectionConfig contains configuration for anomaly detection
type AnomalyDetectionConfig struct {
	Enabled             bool           `yaml:"enabled" default:"true"`
	SensitivityLevel    string         `yaml:"sensitivity_level" default:"medium"`     // low, medium, high
	DetectionAlgorithm  string         `yaml:"detection_algorithm" default:"ensemble"` // statistical, ml, ensemble
	ThresholdMultiplier float64        `yaml:"threshold_multiplier" default:"3.0"`     // Standard deviations for outlier detection
	LearningPeriod      time.Duration  `yaml:"learning_period" default:"24h"`          // Period to establish baseline
	WindowSize          int            `yaml:"window_size" default:"1000"`             // Size of sliding window for analysis
	MinDataPoints       int            `yaml:"min_data_points" default:"100"`          // Minimum data points before detection
	BlockSuspicious     bool           `yaml:"block_suspicious" default:"false"`       // Block suspicious requests
	LogSuspicious       bool           `yaml:"log_suspicious" default:"true"`          // Log suspicious requests
	NotifyOnAnomaly     bool           `yaml:"notify_on_anomaly" default:"true"`       // Send notifications for anomalies
	FeatureWeights      FeatureWeights `yaml:"feature_weights"`                        // Weights for different features
	ActionConfig        ActionConfig   `yaml:"action_config"`                          // Actions to take on anomalies
}

// FeatureWeights defines weights for different request features
type FeatureWeights struct {
	RequestRate  float64 `yaml:"request_rate" default:"0.25"`  // Request rate anomalies
	ResponseTime float64 `yaml:"response_time" default:"0.20"` // Response time anomalies
	ErrorRate    float64 `yaml:"error_rate" default:"0.25"`    // Error rate anomalies
	PayloadSize  float64 `yaml:"payload_size" default:"0.15"`  // Payload size anomalies
	UserBehavior float64 `yaml:"user_behavior" default:"0.10"` // User behavior anomalies
	Geographic   float64 `yaml:"geographic" default:"0.05"`    // Geographic anomalies
}

// ActionConfig defines actions to take when anomalies are detected
type ActionConfig struct {
	AutoBlock      bool          `yaml:"auto_block" default:"false"`    // Automatically block anomalous IPs
	BlockDuration  time.Duration `yaml:"block_duration" default:"1h"`   // Duration to block IPs
	RateLimit      bool          `yaml:"rate_limit" default:"true"`     // Apply rate limiting to suspicious users
	AlertThreshold float64       `yaml:"alert_threshold" default:"0.8"` // Confidence threshold for alerts
	QuarantineTTL  time.Duration `yaml:"quarantine_ttl" default:"30m"`  // Time to quarantine suspicious requests
}

// RequestFeatures represents features extracted from HTTP request
type RequestFeatures struct {
	Timestamp    time.Time              `json:"timestamp"`
	Method       string                 `json:"method"`
	Path         string                 `json:"path"`
	StatusCode   int                    `json:"status_code"`
	ResponseTime time.Duration          `json:"response_time"`
	PayloadSize  int64                  `json:"payload_size"`
	UserAgent    string                 `json:"user_agent"`
	IPAddress    string                 `json:"ip_address"`
	UserID       string                 `json:"user_id"`
	SessionID    string                 `json:"session_id"`
	Headers      map[string]string      `json:"headers"`
	QueryParams  map[string]string      `json:"query_params"`
	GeoLocation  *GeoLocation           `json:"geo_location"`
	Fingerprint  string                 `json:"fingerprint"`
	Metadata     map[string]interface{} `json:"metadata"`
}

// GeoLocation represents geographic location data
type GeoLocation struct {
	Country   string  `json:"country"`
	Region    string  `json:"region"`
	City      string  `json:"city"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	ISP       string  `json:"isp"`
}

// AnomalyDetectionResult represents the result of anomaly detection
type AnomalyDetectionResult struct {
	IsAnomaly         bool                   `json:"is_anomaly"`
	ConfidenceScore   float64                `json:"confidence_score"`
	AnomalyType       string                 `json:"anomaly_type"`
	AnomalyFeatures   []string               `json:"anomaly_features"`
	Severity          string                 `json:"severity"` // low, medium, high, critical
	RiskScore         float64                `json:"risk_score"`
	RecommendedAction string                 `json:"recommended_action"`
	Explanation       string                 `json:"explanation"`
	Timestamp         time.Time              `json:"timestamp"`
	Metadata          map[string]interface{} `json:"metadata"`
}

// BaselineMetrics represents baseline metrics for normal behavior
type BaselineMetrics struct {
	RequestRate      StatisticalMetrics         `json:"request_rate"`
	ResponseTime     StatisticalMetrics         `json:"response_time"`
	ErrorRate        StatisticalMetrics         `json:"error_rate"`
	PayloadSize      StatisticalMetrics         `json:"payload_size"`
	PathDistribution map[string]float64         `json:"path_distribution"`
	UserAgents       map[string]int             `json:"user_agents"`
	IPGeography      map[string]int             `json:"ip_geography"`
	TimePatterns     map[int]StatisticalMetrics `json:"time_patterns"` // Hour of day patterns
	LastUpdated      time.Time                  `json:"last_updated"`
}

// StatisticalMetrics contains statistical metrics for a feature
type StatisticalMetrics struct {
	Mean         float64   `json:"mean"`
	StandardDev  float64   `json:"standard_dev"`
	Min          float64   `json:"min"`
	Max          float64   `json:"max"`
	Percentile95 float64   `json:"percentile_95"`
	Percentile99 float64   `json:"percentile_99"`
	SampleCount  int64     `json:"sample_count"`
	LastUpdated  time.Time `json:"last_updated"`
}

// AnomalyDetectionStats contains statistics for anomaly detection
type AnomalyDetectionStats struct {
	TotalRequests     int64                  `json:"total_requests"`
	AnomaliesDetected int64                  `json:"anomalies_detected"`
	BlockedRequests   int64                  `json:"blocked_requests"`
	FalsePositives    int64                  `json:"false_positives"`
	TruePositives     int64                  `json:"true_positives"`
	DetectionAccuracy float64                `json:"detection_accuracy"`
	AverageConfidence float64                `json:"average_confidence"`
	AnomalyTypes      map[string]int64       `json:"anomaly_types"`
	TopAnomalousIPs   []string               `json:"top_anomalous_ips"`
	BaselineHealth    string                 `json:"baseline_health"`
	LastModelUpdate   time.Time              `json:"last_model_update"`
	ProcessingLatency time.Duration          `json:"processing_latency"`
	CustomMetrics     map[string]interface{} `json:"custom_metrics"`
}

// AnomalyDetection implements AI-powered anomaly detection middleware
type AnomalyDetection struct {
	config         AnomalyDetectionConfig
	agent          ai.AIAgent
	logger         common.Logger
	metrics        common.Metrics
	baseline       *BaselineMetrics
	requestBuffer  []RequestFeatures
	blockedIPs     map[string]time.Time
	quarantinedIPs map[string]time.Time
	stats          AnomalyDetectionStats
	mu             sync.RWMutex
	started        bool
	learningMode   bool
	lastBaseline   time.Time
}

// NewAnomalyDetection creates a new anomaly detection middleware
func NewAnomalyDetection(config AnomalyDetectionConfig, logger common.Logger, metrics common.Metrics) (*AnomalyDetection, error) {
	// Create anomaly detection agent
	capabilities := []ai.Capability{
		{
			Name:        "detect_statistical_anomalies",
			Description: "Detect statistical anomalies in request patterns",
			Metadata: map[string]interface{}{
				"type":      "statistical_analysis",
				"algorithm": "z_score_isolation_forest",
				"accuracy":  0.90,
			},
		},
		{
			Name:        "analyze_behavioral_patterns",
			Description: "Analyze user behavioral patterns for anomalies",
			Metadata: map[string]interface{}{
				"type":      "behavior_analysis",
				"algorithm": "sequence_analysis",
				"accuracy":  0.85,
			},
		},
		{
			Name:        "classify_anomaly_severity",
			Description: "Classify severity and risk level of detected anomalies",
			Metadata: map[string]interface{}{
				"type":      "classification",
				"algorithm": "risk_scoring",
				"accuracy":  0.88,
			},
		},
		{
			Name:        "update_baseline_model",
			Description: "Update baseline behavior model based on new data",
			Metadata: map[string]interface{}{
				"type":      "model_update",
				"algorithm": "incremental_learning",
			},
		},
	}

	agent := ai.NewBaseAgent("anomaly-detector", "Anomaly Detector", ai.AgentTypeAnomalyDetection, capabilities)

	return &AnomalyDetection{
		config:         config,
		agent:          agent,
		logger:         logger,
		metrics:        metrics,
		baseline:       &BaselineMetrics{},
		requestBuffer:  make([]RequestFeatures, 0, config.WindowSize),
		blockedIPs:     make(map[string]time.Time),
		quarantinedIPs: make(map[string]time.Time),
		stats: AnomalyDetectionStats{
			AnomalyTypes:    make(map[string]int64),
			TopAnomalousIPs: make([]string, 0),
			BaselineHealth:  "initializing",
			CustomMetrics:   make(map[string]interface{}),
		},
		learningMode: true,
	}, nil
}

// Name returns the middleware name
func (ad *AnomalyDetection) Name() string {
	return "anomaly-detection"
}

// Type returns the middleware type
func (ad *AnomalyDetection) Type() ai.AIMiddlewareType {
	return ai.AIMiddlewareTypeAnomalyDetect
}

// Initialize initializes the middleware
func (ad *AnomalyDetection) Initialize(ctx context.Context, config ai.AIMiddlewareConfig) error {
	ad.mu.Lock()
	defer ad.mu.Unlock()

	if ad.started {
		return fmt.Errorf("anomaly detection middleware already initialized")
	}

	// Initialize the AI agent
	agentConfig := ai.AgentConfig{
		MaxConcurrency:  10,
		Timeout:         5 * time.Second,
		LearningEnabled: true,
		AutoApply:       true,
		Logger:          ad.logger,
		Metrics:         ad.metrics,
	}

	if err := ad.agent.Initialize(ctx, agentConfig); err != nil {
		return fmt.Errorf("failed to initialize anomaly detection agent: %w", err)
	}

	if err := ad.agent.Start(ctx); err != nil {
		return fmt.Errorf("failed to start anomaly detection agent: %w", err)
	}

	// Initialize baseline metrics
	ad.initializeBaseline()

	// OnStart background tasks
	go ad.baselineUpdateLoop(ctx)
	go ad.cleanupLoop(ctx)
	go ad.modelRetrainingLoop(ctx)

	ad.started = true

	if ad.logger != nil {
		ad.logger.Info("anomaly detection middleware initialized",
			logger.String("sensitivity", ad.config.SensitivityLevel),
			logger.String("algorithm", ad.config.DetectionAlgorithm),
			logger.Bool("block_suspicious", ad.config.BlockSuspicious),
		)
	}

	return nil
}

// Process processes HTTP requests with anomaly detection
func (ad *AnomalyDetection) Process(ctx context.Context, req *http.Request, resp http.ResponseWriter, next http.HandlerFunc) error {
	if !ad.config.Enabled {
		next.ServeHTTP(resp, req)
		return nil
	}

	start := time.Now()
	clientIP := ad.extractClientIP(req)

	// Check if IP is blocked
	if ad.isBlocked(clientIP) {
		ad.handleBlockedRequest(resp, clientIP)
		return nil
	}

	// Check if IP is quarantined
	if ad.isQuarantined(clientIP) {
		ad.handleQuarantinedRequest(resp, clientIP)
		return nil
	}

	// Extract request features
	features := ad.extractRequestFeatures(req, start)

	// Process request normally
	next.ServeHTTP(resp, req)

	// Complete feature extraction with response data
	features.ResponseTime = time.Since(start)
	if statusCode := ad.getResponseStatus(resp); statusCode > 0 {
		features.StatusCode = statusCode
	}

	// Add to request buffer
	ad.addToBuffer(features)

	// Perform anomaly detection (async to avoid blocking)
	go ad.detectAnomalies(ctx, features)

	// Update statistics
	ad.updateStats(start)

	return nil
}

// extractRequestFeatures extracts features from HTTP request
func (ad *AnomalyDetection) extractRequestFeatures(req *http.Request, startTime time.Time) RequestFeatures {
	headers := make(map[string]string)
	for name, values := range req.Header {
		if len(values) > 0 {
			headers[name] = values[0]
		}
	}

	queryParams := make(map[string]string)
	for name, values := range req.URL.Query() {
		if len(values) > 0 {
			queryParams[name] = values[0]
		}
	}

	// Create request fingerprint
	fingerprint := ad.createRequestFingerprint(req)

	// Extract geo location (simplified - in production, use actual geo service)
	geoLocation := &GeoLocation{
		Country: ad.extractCountryFromIP(ad.extractClientIP(req)),
		ISP:     "unknown",
	}

	return RequestFeatures{
		Timestamp:   startTime,
		Method:      req.Method,
		Path:        req.URL.Path,
		PayloadSize: req.ContentLength,
		UserAgent:   req.UserAgent(),
		IPAddress:   ad.extractClientIP(req),
		UserID:      ad.extractUserID(req),
		SessionID:   ad.extractSessionID(req),
		Headers:     headers,
		QueryParams: queryParams,
		GeoLocation: geoLocation,
		Fingerprint: fingerprint,
		Metadata:    make(map[string]interface{}),
	}
}

// detectAnomalies performs anomaly detection on request features
func (ad *AnomalyDetection) detectAnomalies(ctx context.Context, features RequestFeatures) {
	if ad.learningMode && ad.shouldSkipDetection() {
		ad.addToBaseline(features)
		return
	}

	// Create input for AI agent
	input := ai.AgentInput{
		Type: "detect_statistical_anomalies",
		Data: map[string]interface{}{
			"features":  features,
			"baseline":  ad.baseline,
			"algorithm": ad.config.DetectionAlgorithm,
		},
		Context: map[string]interface{}{
			"sensitivity":          ad.config.SensitivityLevel,
			"threshold_multiplier": ad.config.ThresholdMultiplier,
			"feature_weights":      ad.config.FeatureWeights,
		},
		RequestID: fmt.Sprintf("detect-%s-%d", features.IPAddress, time.Now().UnixNano()),
		Timestamp: time.Now(),
	}

	output, err := ad.agent.Process(ctx, input)
	if err != nil {
		if ad.logger != nil {
			ad.logger.Error("anomaly detection failed",
				logger.String("ip", features.IPAddress),
				logger.Error(err),
			)
		}
		return
	}

	// Parse detection results
	result := ad.parseDetectionResult(output)

	if result.IsAnomaly {
		ad.handleAnomalyDetected(ctx, features, result)
	} else {
		// Add normal behavior to baseline
		ad.addToBaseline(features)
	}
}

// parseDetectionResult parses AI agent output into anomaly detection result
func (ad *AnomalyDetection) parseDetectionResult(output ai.AgentOutput) AnomalyDetectionResult {
	result := AnomalyDetectionResult{
		Timestamp: time.Now(),
		Metadata:  make(map[string]interface{}),
	}

	if resultData, ok := output.Data.(map[string]interface{}); ok {
		result.IsAnomaly = getBool(resultData, "is_anomaly")
		result.ConfidenceScore = getFloat64(resultData, "confidence_score")
		result.AnomalyType = getString(resultData, "anomaly_type")
		result.Severity = getString(resultData, "severity")
		result.RiskScore = getFloat64(resultData, "risk_score")
		result.RecommendedAction = getString(resultData, "recommended_action")

		if features, ok := resultData["anomaly_features"].([]interface{}); ok {
			for _, f := range features {
				if feature, ok := f.(string); ok {
					result.AnomalyFeatures = append(result.AnomalyFeatures, feature)
				}
			}
		}
	}

	result.Explanation = output.Explanation
	return result
}

// handleAnomalyDetected handles detected anomalies
func (ad *AnomalyDetection) handleAnomalyDetected(ctx context.Context, features RequestFeatures, result AnomalyDetectionResult) {
	ad.mu.Lock()
	ad.stats.AnomaliesDetected++
	ad.stats.AnomalyTypes[result.AnomalyType]++
	ad.mu.Unlock()

	if ad.logger != nil && ad.config.LogSuspicious {
		ad.logger.Warn("anomaly detected",
			logger.String("ip", features.IPAddress),
			logger.String("path", features.Path),
			logger.String("type", result.AnomalyType),
			logger.Float64("confidence", result.ConfidenceScore),
			logger.Float64("risk_score", result.RiskScore),
			logger.String("explanation", result.Explanation),
		)
	}

	// Take action based on anomaly severity and configuration
	ad.takeAction(features, result)

	// Send notifications if enabled
	if ad.config.NotifyOnAnomaly && result.ConfidenceScore >= ad.config.ActionConfig.AlertThreshold {
		go ad.sendAnomalyNotification(ctx, features, result)
	}

	if ad.metrics != nil {
		ad.metrics.Counter("forge.ai.anomalies_detected", "type", result.AnomalyType, "severity", result.Severity).Inc()
		ad.metrics.Histogram("forge.ai.anomaly_confidence", "type", result.AnomalyType).Observe(result.ConfidenceScore)
		ad.metrics.Histogram("forge.ai.anomaly_risk_score", "type", result.AnomalyType).Observe(result.RiskScore)
	}
}

// takeAction takes appropriate action based on anomaly detection result
func (ad *AnomalyDetection) takeAction(features RequestFeatures, result AnomalyDetectionResult) {
	switch result.Severity {
	case "critical":
		if ad.config.ActionConfig.AutoBlock {
			ad.blockIP(features.IPAddress, ad.config.ActionConfig.BlockDuration)
		}
	case "high":
		if ad.config.ActionConfig.AutoBlock && result.ConfidenceScore > 0.9 {
			ad.blockIP(features.IPAddress, ad.config.ActionConfig.BlockDuration/2)
		} else {
			ad.quarantineIP(features.IPAddress, ad.config.ActionConfig.QuarantineTTL)
		}
	case "medium":
		if ad.config.ActionConfig.RateLimit {
			ad.quarantineIP(features.IPAddress, ad.config.ActionConfig.QuarantineTTL/2)
		}
	case "low":
		// Just log and monitor
	}
}

// isBlocked checks if an IP is blocked
func (ad *AnomalyDetection) isBlocked(ip string) bool {
	ad.mu.RLock()
	defer ad.mu.RUnlock()

	if blockTime, exists := ad.blockedIPs[ip]; exists {
		if time.Since(blockTime) < ad.config.ActionConfig.BlockDuration {
			return true
		}
		// Remove expired blocks
		delete(ad.blockedIPs, ip)
	}
	return false
}

// isQuarantined checks if an IP is quarantined
func (ad *AnomalyDetection) isQuarantined(ip string) bool {
	ad.mu.RLock()
	defer ad.mu.RUnlock()

	if quarantineTime, exists := ad.quarantinedIPs[ip]; exists {
		if time.Since(quarantineTime) < ad.config.ActionConfig.QuarantineTTL {
			return true
		}
		// Remove expired quarantines
		delete(ad.quarantinedIPs, ip)
	}
	return false
}

// blockIP blocks an IP address
func (ad *AnomalyDetection) blockIP(ip string, duration time.Duration) {
	ad.mu.Lock()
	defer ad.mu.Unlock()

	ad.blockedIPs[ip] = time.Now()
	ad.stats.BlockedRequests++

	if ad.logger != nil {
		ad.logger.Warn("IP blocked due to anomalous behavior",
			logger.String("ip", ip),
			logger.Duration("duration", duration),
		)
	}
}

// quarantineIP quarantines an IP address (rate limiting)
func (ad *AnomalyDetection) quarantineIP(ip string, ttl time.Duration) {
	ad.mu.Lock()
	defer ad.mu.Unlock()

	ad.quarantinedIPs[ip] = time.Now()

	if ad.logger != nil {
		ad.logger.Info("IP quarantined for suspicious behavior",
			logger.String("ip", ip),
			logger.Duration("ttl", ttl),
		)
	}
}

// handleBlockedRequest handles blocked request
func (ad *AnomalyDetection) handleBlockedRequest(resp http.ResponseWriter, ip string) {
	resp.Header().Set("Content-Type", "application/json")
	resp.WriteHeader(http.StatusForbidden)

	response := map[string]interface{}{
		"error": map[string]interface{}{
			"code":    "IP_BLOCKED",
			"message": "Access denied due to suspicious activity",
		},
		"timestamp": time.Now().Format(time.RFC3339),
	}

	json.NewEncoder(resp).Encode(response)
}

// handleQuarantinedRequest handles quarantined request
func (ad *AnomalyDetection) handleQuarantinedRequest(resp http.ResponseWriter, ip string) {
	resp.Header().Set("Content-Type", "application/json")
	resp.Header().Set("X-Rate-Limit-Reason", "quarantined")
	resp.WriteHeader(http.StatusTooManyRequests)

	response := map[string]interface{}{
		"error": map[string]interface{}{
			"code":    "IP_QUARANTINED",
			"message": "Rate limited due to suspicious activity",
		},
		"timestamp": time.Now().Format(time.RFC3339),
	}

	json.NewEncoder(resp).Encode(response)
}

// addToBuffer adds request features to the analysis buffer
func (ad *AnomalyDetection) addToBuffer(features RequestFeatures) {
	ad.mu.Lock()
	defer ad.mu.Unlock()

	ad.requestBuffer = append(ad.requestBuffer, features)
	if len(ad.requestBuffer) > ad.config.WindowSize {
		ad.requestBuffer = ad.requestBuffer[1:] // Remove oldest
	}
}

// shouldSkipDetection checks if detection should be skipped during learning period
func (ad *AnomalyDetection) shouldSkipDetection() bool {
	return ad.learningMode && time.Since(ad.lastBaseline) < ad.config.LearningPeriod
}

// initializeBaseline initializes baseline metrics
func (ad *AnomalyDetection) initializeBaseline() {
	ad.baseline = &BaselineMetrics{
		RequestRate:      StatisticalMetrics{},
		ResponseTime:     StatisticalMetrics{},
		ErrorRate:        StatisticalMetrics{},
		PayloadSize:      StatisticalMetrics{},
		PathDistribution: make(map[string]float64),
		UserAgents:       make(map[string]int),
		IPGeography:      make(map[string]int),
		TimePatterns:     make(map[int]StatisticalMetrics),
		LastUpdated:      time.Now(),
	}

	ad.lastBaseline = time.Now()
}

// addToBaseline adds normal request to baseline
func (ad *AnomalyDetection) addToBaseline(features RequestFeatures) {
	ad.mu.Lock()
	defer ad.mu.Unlock()

	// Update path distribution
	ad.baseline.PathDistribution[features.Path]++

	// Update user agents
	ad.baseline.UserAgents[features.UserAgent]++

	// Update geography
	if features.GeoLocation != nil {
		ad.baseline.IPGeography[features.GeoLocation.Country]++
	}

	ad.baseline.LastUpdated = time.Now()
}

// baselineUpdateLoop periodically updates baseline metrics
func (ad *AnomalyDetection) baselineUpdateLoop(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			ad.updateBaseline(ctx)
		}
	}
}

// updateBaseline updates baseline metrics using AI
func (ad *AnomalyDetection) updateBaseline(ctx context.Context) {
	ad.mu.RLock()
	bufferCopy := make([]RequestFeatures, len(ad.requestBuffer))
	copy(bufferCopy, ad.requestBuffer)
	ad.mu.RUnlock()

	if len(bufferCopy) < ad.config.MinDataPoints {
		return
	}

	input := ai.AgentInput{
		Type: "update_baseline_model",
		Data: map[string]interface{}{
			"request_data":     bufferCopy,
			"current_baseline": ad.baseline,
		},
		Context: map[string]interface{}{
			"update_type": "incremental",
			"window_size": ad.config.WindowSize,
		},
		RequestID: fmt.Sprintf("baseline-%d", time.Now().UnixNano()),
		Timestamp: time.Now(),
	}

	output, err := ad.agent.Process(ctx, input)
	if err != nil {
		if ad.logger != nil {
			ad.logger.Error("failed to update baseline", logger.Error(err))
		}
		return
	}

	// Update baseline with AI recommendations
	if baselineData, ok := output.Data.(map[string]interface{}); ok {
		ad.updateBaselineFromAI(baselineData)
	}

	// Exit learning mode after sufficient learning period
	if ad.learningMode && time.Since(ad.lastBaseline) > ad.config.LearningPeriod {
		ad.learningMode = false
		ad.stats.BaselineHealth = "active"

		if ad.logger != nil {
			ad.logger.Info("anomaly detection baseline established, exiting learning mode")
		}
	}
}

// updateBaselineFromAI updates baseline from AI output
func (ad *AnomalyDetection) updateBaselineFromAI(baselineData map[string]interface{}) {
	ad.mu.Lock()
	defer ad.mu.Unlock()

	// Update statistical metrics if provided
	if requestRate, ok := baselineData["request_rate"].(map[string]interface{}); ok {
		ad.baseline.RequestRate = ad.parseStatisticalMetrics(requestRate)
	}
	if responseTime, ok := baselineData["response_time"].(map[string]interface{}); ok {
		ad.baseline.ResponseTime = ad.parseStatisticalMetrics(responseTime)
	}
	if errorRate, ok := baselineData["error_rate"].(map[string]interface{}); ok {
		ad.baseline.ErrorRate = ad.parseStatisticalMetrics(errorRate)
	}
	if payloadSize, ok := baselineData["payload_size"].(map[string]interface{}); ok {
		ad.baseline.PayloadSize = ad.parseStatisticalMetrics(payloadSize)
	}

	ad.baseline.LastUpdated = time.Now()
	ad.stats.LastModelUpdate = time.Now()
}

// parseStatisticalMetrics parses statistical metrics from map
func (ad *AnomalyDetection) parseStatisticalMetrics(data map[string]interface{}) StatisticalMetrics {
	return StatisticalMetrics{
		Mean:         getFloat64(data, "mean"),
		StandardDev:  getFloat64(data, "standard_dev"),
		Min:          getFloat64(data, "min"),
		Max:          getFloat64(data, "max"),
		Percentile95: getFloat64(data, "percentile_95"),
		Percentile99: getFloat64(data, "percentile_99"),
		SampleCount:  int64(getFloat64(data, "sample_count")),
		LastUpdated:  time.Now(),
	}
}

// cleanupLoop performs periodic cleanup of expired blocks and quarantines
func (ad *AnomalyDetection) cleanupLoop(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			ad.cleanup()
		}
	}
}

// cleanup removes expired blocks and quarantines
func (ad *AnomalyDetection) cleanup() {
	ad.mu.Lock()
	defer ad.mu.Unlock()

	now := time.Now()

	// Clean up expired blocks
	for ip, blockTime := range ad.blockedIPs {
		if now.Sub(blockTime) > ad.config.ActionConfig.BlockDuration {
			delete(ad.blockedIPs, ip)
		}
	}

	// Clean up expired quarantines
	for ip, quarantineTime := range ad.quarantinedIPs {
		if now.Sub(quarantineTime) > ad.config.ActionConfig.QuarantineTTL {
			delete(ad.quarantinedIPs, ip)
		}
	}
}

// modelRetrainingLoop periodically retrains the anomaly detection model
func (ad *AnomalyDetection) modelRetrainingLoop(ctx context.Context) {
	ticker := time.NewTicker(6 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			ad.retrainModel(ctx)
		}
	}
}

// retrainModel retrains the anomaly detection model
func (ad *AnomalyDetection) retrainModel(ctx context.Context) {
	if ad.learningMode {
		return // Skip retraining during learning mode
	}

	// Implement model retraining logic
	// This would involve collecting labeled data and retraining the AI model
	if ad.logger != nil {
		ad.logger.Debug("performing anomaly detection model retraining")
	}

	ad.stats.LastModelUpdate = time.Now()
}

// sendAnomalyNotification sends notification about detected anomaly
func (ad *AnomalyDetection) sendAnomalyNotification(ctx context.Context, features RequestFeatures, result AnomalyDetectionResult) {
	// Implementation would depend on notification service
	// For now, just log
	if ad.logger != nil {
		ad.logger.Warn("anomaly notification",
			logger.String("ip", features.IPAddress),
			logger.String("type", result.AnomalyType),
			logger.String("severity", result.Severity),
			logger.Float64("confidence", result.ConfidenceScore),
		)
	}
}

// Utility functions
func (ad *AnomalyDetection) extractClientIP(req *http.Request) string {
	if xff := req.Header.Get("X-Forwarded-For"); xff != "" {
		ips := strings.Split(xff, ",")
		return strings.TrimSpace(ips[0])
	}
	if xri := req.Header.Get("X-Real-IP"); xri != "" {
		return strings.TrimSpace(xri)
	}
	ip := req.RemoteAddr
	if colon := strings.LastIndex(ip, ":"); colon != -1 {
		ip = ip[:colon]
	}
	return ip
}

func (ad *AnomalyDetection) extractUserID(req *http.Request) string {
	// Try various methods to extract user ID
	if userID := req.Header.Get("X-User-ID"); userID != "" {
		return userID
	}
	// Add JWT parsing logic here
	return ""
}

func (ad *AnomalyDetection) extractSessionID(req *http.Request) string {
	if cookie, err := req.Cookie("session_id"); err == nil {
		return cookie.Value
	}
	return req.Header.Get("X-Session-ID")
}

func (ad *AnomalyDetection) createRequestFingerprint(req *http.Request) string {
	// Create unique fingerprint for request
	data := fmt.Sprintf("%s:%s:%s:%s", req.Method, req.URL.Path, req.UserAgent(), ad.extractClientIP(req))
	hash := sha256.Sum256([]byte(data))
	return fmt.Sprintf("%x", hash)[:16]
}

func (ad *AnomalyDetection) extractCountryFromIP(ip string) string {
	// Simplified geo lookup - in production, use actual geo service
	return "unknown"
}

func (ad *AnomalyDetection) getResponseStatus(resp http.ResponseWriter) int {
	// Extract status code from response writer
	// This is simplified - actual implementation would depend on response writer type
	return 200
}

func (ad *AnomalyDetection) updateStats(startTime time.Time) {
	ad.mu.Lock()
	defer ad.mu.Unlock()

	ad.stats.TotalRequests++

	// Update processing latency
	latency := time.Since(startTime)
	if ad.stats.TotalRequests == 1 {
		ad.stats.ProcessingLatency = latency
	} else {
		ad.stats.ProcessingLatency = time.Duration(
			(int64(ad.stats.ProcessingLatency)*int64(ad.stats.TotalRequests-1) + int64(latency)) / int64(ad.stats.TotalRequests),
		)
	}

	// Update detection accuracy (simplified)
	if ad.stats.AnomaliesDetected > 0 {
		ad.stats.DetectionAccuracy = float64(ad.stats.TruePositives) / float64(ad.stats.AnomaliesDetected)
	}
}

// Helper functions for data extraction
func getBool(data map[string]interface{}, key string) bool {
	if val, ok := data[key].(bool); ok {
		return val
	}
	return false
}

func getFloat64(data map[string]interface{}, key string) float64 {
	if val, ok := data[key].(float64); ok {
		return val
	}
	return 0.0
}

func getString(data map[string]interface{}, key string) string {
	if val, ok := data[key].(string); ok {
		return val
	}
	return ""
}

// GetStats returns middleware statistics
func (ad *AnomalyDetection) GetStats() ai.AIMiddlewareStats {
	ad.mu.RLock()
	defer ad.mu.RUnlock()

	return ai.AIMiddlewareStats{
		Name:            ad.Name(),
		Type:            string(ad.Type()),
		RequestsTotal:   ad.stats.TotalRequests,
		RequestsBlocked: ad.stats.BlockedRequests,
		AverageLatency:  ad.stats.ProcessingLatency,
		LearningEnabled: true,
		AdaptiveChanges: 0, // Not applicable for anomaly detection
		LastUpdated:     ad.baseline.LastUpdated,
		CustomMetrics: map[string]interface{}{
			"anomalies_detected":    ad.stats.AnomaliesDetected,
			"detection_accuracy":    ad.stats.DetectionAccuracy,
			"false_positives":       ad.stats.FalsePositives,
			"true_positives":        ad.stats.TruePositives,
			"average_confidence":    ad.stats.AverageConfidence,
			"anomaly_types":         ad.stats.AnomalyTypes,
			"baseline_health":       ad.stats.BaselineHealth,
			"learning_mode":         ad.learningMode,
			"blocked_ips_count":     len(ad.blockedIPs),
			"quarantined_ips_count": len(ad.quarantinedIPs),
			"last_model_update":     ad.stats.LastModelUpdate,
		},
	}
}
