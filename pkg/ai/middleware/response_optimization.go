package middleware

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/xraph/forge/pkg/ai"
	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/logger"
)

// ResponseOptimizationConfig contains configuration for AI-powered response optimization
type ResponseOptimizationConfig struct {
	Enabled               bool          `yaml:"enabled" default:"true"`
	CompressionEnabled    bool          `yaml:"compression_enabled" default:"true"`
	CacheOptimization     bool          `yaml:"cache_optimization" default:"true"`
	ContentOptimization   bool          `yaml:"content_optimization" default:"true"`
	HeaderOptimization    bool          `yaml:"header_optimization" default:"true"`
	AdaptiveCompression   bool          `yaml:"adaptive_compression" default:"true"`
	MinCompressionSize    int64         `yaml:"min_compression_size" default:"1024"`       // Minimum size for compression
	CompressionLevel      int           `yaml:"compression_level" default:"6"`             // 1-9 compression level
	LearningEnabled       bool          `yaml:"learning_enabled" default:"true"`           // Enable learning from response patterns
	OptimizationAlgorithm string        `yaml:"optimization_algorithm" default:"adaptive"` // adaptive, aggressive, conservative
	ResponseSizeThreshold int64         `yaml:"response_size_threshold" default:"10240"`   // 10KB threshold for optimization
	LatencyThreshold      time.Duration `yaml:"latency_threshold" default:"500ms"`         // Latency threshold for optimization
	CacheHitRateTarget    float64       `yaml:"cache_hit_rate_target" default:"0.8"`       // Target cache hit rate
	BandwidthSavingTarget float64       `yaml:"bandwidth_saving_target" default:"0.3"`     // Target bandwidth saving (30%)
	ContentTypeFilters    []string      `yaml:"content_type_filters"`                      // Content types to optimize
}

// ResponseData contains response data for optimization analysis
type ResponseData struct {
	StatusCode          int                    `json:"status_code"`
	ContentType         string                 `json:"content_type"`
	OriginalSize        int64                  `json:"original_size"`
	CompressedSize      int64                  `json:"compressed_size"`
	CompressionRatio    float64                `json:"compression_ratio"`
	Headers             map[string]string      `json:"headers"`
	ProcessingTime      time.Duration          `json:"processing_time"`
	CompressionTime     time.Duration          `json:"compression_time"`
	CacheStatus         string                 `json:"cache_status"` // hit, miss, no-cache
	OptimizationApplied []string               `json:"optimization_applied"`
	Metadata            map[string]interface{} `json:"metadata"`
	Timestamp           time.Time              `json:"timestamp"`
}

// OptimizationDecision represents an optimization decision made by AI
type OptimizationDecision struct {
	ShouldCompress      bool              `json:"should_compress"`
	CompressionLevel    int               `json:"compression_level"`
	CacheHeaders        map[string]string `json:"cache_headers"`
	ContentFiltering    bool              `json:"content_filtering"`
	HeaderOptimizations []string          `json:"header_optimizations"`
	PredictedSaving     float64           `json:"predicted_saving"`
	Confidence          float64           `json:"confidence"`
	RecommendedActions  []string          `json:"recommended_actions"`
	Reasoning           string            `json:"reasoning"`
	OptimizationType    string            `json:"optimization_type"` // compression, caching, content, headers
	Timestamp           time.Time         `json:"timestamp"`
}

// ResponseOptimizationStats contains statistics for response optimization
type ResponseOptimizationStats struct {
	TotalRequests          int64                    `json:"total_requests"`
	OptimizedResponses     int64                    `json:"optimized_responses"`
	BytesSaved             int64                    `json:"bytes_saved"`
	BandwidthSavingPercent float64                  `json:"bandwidth_saving_percent"`
	AverageCompressionTime time.Duration            `json:"average_compression_time"`
	CompressionStats       CompressionStats         `json:"compression_stats"`
	CacheStats             CacheOptimizationStats   `json:"cache_stats"`
	ContentStats           ContentOptimizationStats `json:"content_stats"`
	HeaderStats            HeaderOptimizationStats  `json:"header_stats"`
	OptimizationAccuracy   float64                  `json:"optimization_accuracy"`
	PerformanceImpact      time.Duration            `json:"performance_impact"`
	LastOptimization       time.Time                `json:"last_optimization"`
	TopOptimizedPaths      []PathOptimizationStat   `json:"top_optimized_paths"`
}

// CompressionStats contains compression-specific statistics
type CompressionStats struct {
	CompressedResponses     int64            `json:"compressed_responses"`
	TotalOriginalSize       int64            `json:"total_original_size"`
	TotalCompressedSize     int64            `json:"total_compressed_size"`
	AverageCompressionRatio float64          `json:"average_compression_ratio"`
	CompressionByType       map[string]int64 `json:"compression_by_type"`
	OptimalCompressionLevel int              `json:"optimal_compression_level"`
}

// CacheOptimizationStats contains cache optimization statistics
type CacheOptimizationStats struct {
	CacheHitRate           float64                      `json:"cache_hit_rate"`
	OptimalCacheHeaders    map[string]string            `json:"optimal_cache_headers"`
	CacheHeadersByPath     map[string]map[string]string `json:"cache_headers_by_path"`
	AverageCacheEfficiency float64                      `json:"average_cache_efficiency"`
}

// ContentOptimizationStats contains content optimization statistics
type ContentOptimizationStats struct {
	JSONMinified      int64 `json:"json_minified"`
	HTMLOptimized     int64 `json:"html_optimized"`
	CSSOptimized      int64 `json:"css_optimized"`
	JSOptimized       int64 `json:"js_optimized"`
	ImagesOptimized   int64 `json:"images_optimized"`
	ContentBytesSaved int64 `json:"content_bytes_saved"`
}

// HeaderOptimizationStats contains header optimization statistics
type HeaderOptimizationStats struct {
	RedundantHeadersRemoved int64               `json:"redundant_headers_removed"`
	SecurityHeadersAdded    int64               `json:"security_headers_added"`
	PerformanceHeadersAdded int64               `json:"performance_headers_added"`
	HeadersBytesSaved       int64               `json:"headers_bytes_saved"`
	OptimalHeadersByPath    map[string][]string `json:"optimal_headers_by_path"`
}

// PathOptimizationStat contains optimization statistics for a specific path
type PathOptimizationStat struct {
	Path                string  `json:"path"`
	RequestCount        int64   `json:"request_count"`
	BytesSaved          int64   `json:"bytes_saved"`
	AverageBytesSaved   float64 `json:"average_bytes_saved"`
	OptimizationSuccess float64 `json:"optimization_success"`
}

// OptimizedResponseWriter wraps http.ResponseWriter for response optimization
type OptimizedResponseWriter struct {
	http.ResponseWriter
	buffer       *bytes.Buffer
	statusCode   int
	originalSize int64
	headers      map[string]string
	optimizer    *ResponseOptimization
	startTime    time.Time
}

// ResponseOptimization implements AI-powered response optimization middleware
type ResponseOptimization struct {
	config         ResponseOptimizationConfig
	agent          ai.AIAgent
	logger         common.Logger
	metrics        common.Metrics
	stats          ResponseOptimizationStats
	responseBuffer []ResponseData
	pathPatterns   map[string]*PathOptimizationPattern
	mu             sync.RWMutex
	started        bool
}

// PathOptimizationPattern represents learned optimization patterns for a path
type PathOptimizationPattern struct {
	Path                     string                 `json:"path"`
	OptimalCompressionLevel  int                    `json:"optimal_compression_level"`
	OptimalCacheHeaders      map[string]string      `json:"optimal_cache_headers"`
	AverageResponseSize      int64                  `json:"average_response_size"`
	CompressionEffectiveness float64                `json:"compression_effectiveness"`
	LastUpdated              time.Time              `json:"last_updated"`
	RequestCount             int64                  `json:"request_count"`
	Metadata                 map[string]interface{} `json:"metadata"`
}

// NewResponseOptimization creates a new response optimization middleware
func NewResponseOptimization(config ResponseOptimizationConfig, logger common.Logger, metrics common.Metrics) (*ResponseOptimization, error) {
	// Create response optimization agent
	capabilities := []ai.Capability{
		{
			Name:        "optimize_compression",
			Description: "Optimize response compression based on content analysis",
			Metadata: map[string]interface{}{
				"type":      "compression_optimization",
				"algorithm": "adaptive_compression",
				"accuracy":  0.88,
			},
		},
		{
			Name:        "optimize_caching",
			Description: "Optimize cache headers for better cache efficiency",
			Metadata: map[string]interface{}{
				"type":      "cache_optimization",
				"algorithm": "cache_policy_optimization",
				"accuracy":  0.85,
			},
		},
		{
			Name:        "optimize_content",
			Description: "Optimize content structure and formatting",
			Metadata: map[string]interface{}{
				"type":      "content_optimization",
				"algorithm": "content_analysis",
				"accuracy":  0.82,
			},
		},
		{
			Name:        "optimize_headers",
			Description: "Optimize HTTP headers for performance and security",
			Metadata: map[string]interface{}{
				"type":      "header_optimization",
				"algorithm": "header_analysis",
				"accuracy":  0.90,
			},
		},
	}

	agent := ai.NewBaseAgent("response-optimizer", "Response Optimizer", ai.AgentTypeOptimization, capabilities)

	return &ResponseOptimization{
		config:         config,
		agent:          agent,
		logger:         logger,
		metrics:        metrics,
		responseBuffer: make([]ResponseData, 0, 1000),
		pathPatterns:   make(map[string]*PathOptimizationPattern),
		stats: ResponseOptimizationStats{
			CompressionStats: CompressionStats{
				CompressionByType: make(map[string]int64),
			},
			CacheStats: CacheOptimizationStats{
				OptimalCacheHeaders: make(map[string]string),
				CacheHeadersByPath:  make(map[string]map[string]string),
			},
			HeaderStats: HeaderOptimizationStats{
				OptimalHeadersByPath: make(map[string][]string),
			},
			TopOptimizedPaths: make([]PathOptimizationStat, 0),
		},
	}, nil
}

// Name returns the middleware name
func (ro *ResponseOptimization) Name() string {
	return "response-optimization"
}

// Type returns the middleware type
func (ro *ResponseOptimization) Type() ai.AIMiddlewareType {
	return ai.AIMiddlewareTypeOptimization
}

// Initialize initializes the middleware
func (ro *ResponseOptimization) Initialize(ctx context.Context, config ai.AIMiddlewareConfig) error {
	ro.mu.Lock()
	defer ro.mu.Unlock()

	if ro.started {
		return fmt.Errorf("response optimization middleware already initialized")
	}

	// Initialize the AI agent
	agentConfig := ai.AgentConfig{
		MaxConcurrency:  15,
		Timeout:         3 * time.Second,
		LearningEnabled: ro.config.LearningEnabled,
		AutoApply:       true,
		Logger:          ro.logger,
		Metrics:         ro.metrics,
	}

	if err := ro.agent.Initialize(ctx, agentConfig); err != nil {
		return fmt.Errorf("failed to initialize response optimization agent: %w", err)
	}

	if err := ro.agent.Start(ctx); err != nil {
		return fmt.Errorf("failed to start response optimization agent: %w", err)
	}

	// Start background optimization tasks
	go ro.optimizationLoop(ctx)
	go ro.patternLearningLoop(ctx)
	go ro.performanceMonitoringLoop(ctx)

	ro.started = true

	if ro.logger != nil {
		ro.logger.Info("response optimization middleware initialized",
			logger.Bool("compression_enabled", ro.config.CompressionEnabled),
			logger.Bool("cache_optimization", ro.config.CacheOptimization),
			logger.String("algorithm", ro.config.OptimizationAlgorithm),
		)
	}

	return nil
}

// Process processes HTTP requests with response optimization
func (ro *ResponseOptimization) Process(ctx context.Context, req *http.Request, resp http.ResponseWriter, next http.HandlerFunc) error {
	if !ro.config.Enabled {
		next.ServeHTTP(resp, req)
		return nil
	}

	start := time.Now()

	// Create optimized response writer
	optimizedWriter := &OptimizedResponseWriter{
		ResponseWriter: resp,
		buffer:         &bytes.Buffer{},
		headers:        make(map[string]string),
		optimizer:      ro,
		startTime:      start,
	}

	// Get optimization decision from AI
	decision, err := ro.getOptimizationDecision(ctx, req)
	if err != nil {
		if ro.logger != nil {
			ro.logger.Error("failed to get optimization decision", logger.Error(err))
		}
		// Fallback to default optimization
		decision = ro.getDefaultOptimization(req)
	}

	// Apply pre-response optimizations (headers, cache directives)
	ro.applyPreResponseOptimizations(optimizedWriter, req, decision)

	// Process request
	next.ServeHTTP(optimizedWriter, req)

	// Apply post-response optimizations (compression, content optimization)
	if err := ro.applyPostResponseOptimizations(ctx, optimizedWriter, req, decision); err != nil {
		if ro.logger != nil {
			ro.logger.Error("failed to apply post-response optimizations", logger.Error(err))
		}
	}

	// Record optimization data
	ro.recordOptimizationData(req, optimizedWriter, decision, time.Since(start))

	return nil
}

// getOptimizationDecision gets AI-powered optimization decision
func (ro *ResponseOptimization) getOptimizationDecision(ctx context.Context, req *http.Request) (*OptimizationDecision, error) {
	// Extract request features
	requestFeatures := map[string]interface{}{
		"method":            req.Method,
		"path":              req.URL.Path,
		"content_type":      req.Header.Get("Content-Type"),
		"user_agent":        req.UserAgent(),
		"accept_encoding":   req.Header.Get("Accept-Encoding"),
		"if_modified_since": req.Header.Get("If-Modified-Since"),
		"cache_control":     req.Header.Get("Cache-Control"),
	}

	// Get historical patterns for this path
	pattern := ro.getPathPattern(req.URL.Path)

	input := ai.AgentInput{
		Type: "optimize_compression", // Start with compression optimization
		Data: map[string]interface{}{
			"request_features": requestFeatures,
			"path_pattern":     pattern,
			"config":           ro.config,
		},
		Context: map[string]interface{}{
			"optimization_goals": []string{"bandwidth", "latency", "cache_efficiency"},
			"algorithm":          ro.config.OptimizationAlgorithm,
		},
		RequestID: fmt.Sprintf("optimize-%s-%d", req.URL.Path, time.Now().UnixNano()),
		Timestamp: time.Now(),
	}

	output, err := ro.agent.Process(ctx, input)
	if err != nil {
		return nil, err
	}

	// Parse optimization decision
	return ro.parseOptimizationDecision(output), nil
}

// parseOptimizationDecision parses AI output into optimization decision
func (ro *ResponseOptimization) parseOptimizationDecision(output ai.AgentOutput) *OptimizationDecision {
	decision := &OptimizationDecision{
		Confidence:   output.Confidence,
		Reasoning:    output.Explanation,
		Timestamp:    time.Now(),
		CacheHeaders: make(map[string]string),
	}

	if decisionData, ok := output.Data.(map[string]interface{}); ok {
		decision.ShouldCompress = getBool(decisionData, "should_compress")
		decision.CompressionLevel = int(getFloat64(decisionData, "compression_level"))
		decision.ContentFiltering = getBool(decisionData, "content_filtering")
		decision.PredictedSaving = getFloat64(decisionData, "predicted_saving")
		decision.OptimizationType = getString(decisionData, "optimization_type")

		// Parse cache headers
		if cacheHeaders, ok := decisionData["cache_headers"].(map[string]interface{}); ok {
			for key, value := range cacheHeaders {
				if strValue, ok := value.(string); ok {
					decision.CacheHeaders[key] = strValue
				}
			}
		}

		// Parse header optimizations
		if headerOpts, ok := decisionData["header_optimizations"].([]interface{}); ok {
			for _, opt := range headerOpts {
				if optStr, ok := opt.(string); ok {
					decision.HeaderOptimizations = append(decision.HeaderOptimizations, optStr)
				}
			}
		}

		// Parse recommended actions
		if actions, ok := decisionData["recommended_actions"].([]interface{}); ok {
			for _, action := range actions {
				if actionStr, ok := action.(string); ok {
					decision.RecommendedActions = append(decision.RecommendedActions, actionStr)
				}
			}
		}
	}

	return decision
}

// getDefaultOptimization provides fallback optimization when AI fails
func (ro *ResponseOptimization) getDefaultOptimization(req *http.Request) *OptimizationDecision {
	supportsGzip := strings.Contains(req.Header.Get("Accept-Encoding"), "gzip")

	return &OptimizationDecision{
		ShouldCompress:   supportsGzip && ro.config.CompressionEnabled,
		CompressionLevel: ro.config.CompressionLevel,
		ContentFiltering: ro.config.ContentOptimization,
		CacheHeaders:     map[string]string{"Cache-Control": "public, max-age=3600"},
		PredictedSaving:  0.3,
		Confidence:       0.7,
		OptimizationType: "default",
		Reasoning:        "Default optimization applied due to AI failure",
		Timestamp:        time.Now(),
	}
}

// applyPreResponseOptimizations applies optimizations before response is written
func (ro *ResponseOptimization) applyPreResponseOptimizations(writer *OptimizedResponseWriter, req *http.Request, decision *OptimizationDecision) {
	// Apply cache headers
	for key, value := range decision.CacheHeaders {
		writer.Header().Set(key, value)
	}

	// Apply header optimizations
	if ro.config.HeaderOptimization {
		ro.optimizeHeaders(writer, decision.HeaderOptimizations)
	}

	// Set compression headers if compression will be applied
	if decision.ShouldCompress && strings.Contains(req.Header.Get("Accept-Encoding"), "gzip") {
		writer.Header().Set("Content-Encoding", "gzip")
		writer.Header().Set("Vary", "Accept-Encoding")
	}
}

// applyPostResponseOptimizations applies optimizations after response is written
func (ro *ResponseOptimization) applyPostResponseOptimizations(ctx context.Context, writer *OptimizedResponseWriter, req *http.Request, decision *OptimizationDecision) error {
	responseData := writer.buffer.Bytes()
	writer.originalSize = int64(len(responseData))

	// Apply content optimization
	if ro.config.ContentOptimization && decision.ContentFiltering {
		optimizedContent, err := ro.optimizeContent(responseData, writer.Header().Get("Content-Type"))
		if err == nil {
			responseData = optimizedContent
		}
	}

	// Apply compression
	if decision.ShouldCompress && ro.shouldCompress(responseData, writer.Header().Get("Content-Type")) {
		compressedData, err := ro.compressResponse(responseData, decision.CompressionLevel)
		if err == nil && len(compressedData) < len(responseData) {
			responseData = compressedData
			writer.Header().Set("Content-Encoding", "gzip")
		}
	}

	// Update content length
	writer.Header().Set("Content-Length", strconv.Itoa(len(responseData)))

	// Write optimized response
	writer.ResponseWriter.WriteHeader(writer.statusCode)
	_, err := writer.ResponseWriter.Write(responseData)

	return err
}

// shouldCompress determines if response should be compressed
func (ro *ResponseOptimization) shouldCompress(data []byte, contentType string) bool {
	if len(data) < int(ro.config.MinCompressionSize) {
		return false
	}

	// Check content type filters
	if len(ro.config.ContentTypeFilters) > 0 {
		allowed := false
		for _, allowedType := range ro.config.ContentTypeFilters {
			if strings.Contains(contentType, allowedType) {
				allowed = true
				break
			}
		}
		if !allowed {
			return false
		}
	}

	// Don't compress already compressed content
	compressedTypes := []string{"gzip", "deflate", "compress", "br"}
	for _, compType := range compressedTypes {
		if strings.Contains(contentType, compType) {
			return false
		}
	}

	// Don't compress images (usually already compressed)
	imageTypes := []string{"image/", "video/", "audio/"}
	for _, imgType := range imageTypes {
		if strings.HasPrefix(contentType, imgType) {
			return false
		}
	}

	return true
}

// compressResponse compresses response data using gzip
func (ro *ResponseOptimization) compressResponse(data []byte, level int) ([]byte, error) {
	var compressed bytes.Buffer

	writer, err := gzip.NewWriterLevel(&compressed, level)
	if err != nil {
		return nil, err
	}

	_, err = writer.Write(data)
	if err != nil {
		writer.Close()
		return nil, err
	}

	err = writer.Close()
	if err != nil {
		return nil, err
	}

	return compressed.Bytes(), nil
}

// optimizeContent optimizes content based on content type
func (ro *ResponseOptimization) optimizeContent(data []byte, contentType string) ([]byte, error) {
	switch {
	case strings.Contains(contentType, "application/json"):
		return ro.minifyJSON(data)
	case strings.Contains(contentType, "text/html"):
		return ro.optimizeHTML(data)
	case strings.Contains(contentType, "text/css"):
		return ro.minifyCSS(data)
	case strings.Contains(contentType, "text/javascript"), strings.Contains(contentType, "application/javascript"):
		return ro.minifyJS(data)
	default:
		return data, nil
	}
}

// minifyJSON removes unnecessary whitespace from JSON
func (ro *ResponseOptimization) minifyJSON(data []byte) ([]byte, error) {
	var compactData bytes.Buffer
	err := json.Compact(&compactData, data)
	if err != nil {
		return data, err
	}
	return compactData.Bytes(), nil
}

// optimizeHTML performs basic HTML optimization
func (ro *ResponseOptimization) optimizeHTML(data []byte) ([]byte, error) {
	// Simple HTML optimization - remove extra whitespace
	html := string(data)

	// Remove multiple spaces
	html = strings.ReplaceAll(html, "  ", " ")
	html = strings.ReplaceAll(html, "\n\n", "\n")
	html = strings.ReplaceAll(html, "\t", "")

	// Remove spaces around certain elements
	html = strings.ReplaceAll(html, "> <", "><")

	return []byte(html), nil
}

// minifyCSS performs basic CSS minification
func (ro *ResponseOptimization) minifyCSS(data []byte) ([]byte, error) {
	css := string(data)

	// Remove comments
	css = strings.ReplaceAll(css, "/*", "")
	css = strings.ReplaceAll(css, "*/", "")

	// Remove extra whitespace
	css = strings.ReplaceAll(css, "\n", "")
	css = strings.ReplaceAll(css, "\t", "")
	css = strings.ReplaceAll(css, "  ", " ")
	css = strings.ReplaceAll(css, ": ", ":")
	css = strings.ReplaceAll(css, "; ", ";")

	return []byte(css), nil
}

// minifyJS performs basic JavaScript minification
func (ro *ResponseOptimization) minifyJS(data []byte) ([]byte, error) {
	js := string(data)

	// Very basic JS minification - remove extra whitespace and line breaks
	js = strings.ReplaceAll(js, "\n", "")
	js = strings.ReplaceAll(js, "\t", "")
	js = strings.ReplaceAll(js, "  ", " ")

	return []byte(js), nil
}

// optimizeHeaders optimizes HTTP headers
func (ro *ResponseOptimization) optimizeHeaders(writer *OptimizedResponseWriter, optimizations []string) {
	for _, optimization := range optimizations {
		switch optimization {
		case "add_security_headers":
			writer.Header().Set("X-Content-Type-Options", "nosniff")
			writer.Header().Set("X-Frame-Options", "DENY")
			writer.Header().Set("X-XSS-Protection", "1; mode=block")
		case "add_performance_headers":
			writer.Header().Set("X-DNS-Prefetch-Control", "on")
			writer.Header().Set("X-Robots-Tag", "index, follow")
		case "remove_server_header":
			writer.Header().Del("Server")
		case "remove_powered_by":
			writer.Header().Del("X-Powered-By")
		case "optimize_etag":
			// Generate optimized ETag
			writer.Header().Set("ETag", fmt.Sprintf(`"opt-%d"`, time.Now().Unix()))
		}
	}
}

// getPathPattern gets optimization pattern for a path
func (ro *ResponseOptimization) getPathPattern(path string) *PathOptimizationPattern {
	ro.mu.RLock()
	defer ro.mu.RUnlock()

	if pattern, exists := ro.pathPatterns[path]; exists {
		return pattern
	}

	// Return default pattern
	return &PathOptimizationPattern{
		Path:                    path,
		OptimalCompressionLevel: ro.config.CompressionLevel,
		OptimalCacheHeaders:     make(map[string]string),
		LastUpdated:             time.Now(),
		Metadata:                make(map[string]interface{}),
	}
}

// recordOptimizationData records data about optimization performance
func (ro *ResponseOptimization) recordOptimizationData(req *http.Request, writer *OptimizedResponseWriter, decision *OptimizationDecision, processingTime time.Duration) {
	responseData := ResponseData{
		StatusCode:          writer.statusCode,
		ContentType:         writer.Header().Get("Content-Type"),
		OriginalSize:        writer.originalSize,
		ProcessingTime:      processingTime,
		CacheStatus:         ro.determineCacheStatus(req, writer),
		OptimizationApplied: decision.RecommendedActions,
		Timestamp:           time.Now(),
		Metadata:            make(map[string]interface{}),
	}

	// Calculate compression metrics if compression was applied
	if writer.Header().Get("Content-Encoding") == "gzip" {
		responseData.CompressedSize = int64(len(writer.buffer.Bytes()))
		if responseData.OriginalSize > 0 {
			responseData.CompressionRatio = float64(responseData.CompressedSize) / float64(responseData.OriginalSize)
		}
	}

	// Add to buffer for analysis
	ro.mu.Lock()
	ro.responseBuffer = append(ro.responseBuffer, responseData)
	if len(ro.responseBuffer) > 1000 {
		ro.responseBuffer = ro.responseBuffer[1:] // Remove oldest
	}

	// Update statistics
	ro.updateStats(responseData)
	ro.mu.Unlock()

	// Learn from optimization if enabled
	if ro.config.LearningEnabled {
		go ro.learnFromOptimization(req, responseData, decision)
	}
}

// updateStats updates optimization statistics
func (ro *ResponseOptimization) updateStats(data ResponseData) {
	ro.stats.TotalRequests++

	if len(data.OptimizationApplied) > 0 {
		ro.stats.OptimizedResponses++
	}

	// Update bytes saved
	if data.OriginalSize > 0 && data.CompressedSize > 0 {
		bytesSaved := data.OriginalSize - data.CompressedSize
		ro.stats.BytesSaved += bytesSaved

		// Update compression stats
		ro.stats.CompressionStats.CompressedResponses++
		ro.stats.CompressionStats.TotalOriginalSize += data.OriginalSize
		ro.stats.CompressionStats.TotalCompressedSize += data.CompressedSize

		if ro.stats.CompressionStats.TotalOriginalSize > 0 {
			ro.stats.CompressionStats.AverageCompressionRatio =
				float64(ro.stats.CompressionStats.TotalCompressedSize) /
					float64(ro.stats.CompressionStats.TotalOriginalSize)
		}

		// Update compression by type
		contentType := strings.Split(data.ContentType, ";")[0]
		ro.stats.CompressionStats.CompressionByType[contentType]++
	}

	// Update bandwidth saving percentage
	if ro.stats.TotalRequests > 0 {
		totalOriginalBytes := ro.stats.CompressionStats.TotalOriginalSize
		if totalOriginalBytes > 0 {
			ro.stats.BandwidthSavingPercent = float64(ro.stats.BytesSaved) / float64(totalOriginalBytes) * 100
		}
	}

	// Update cache stats
	if data.CacheStatus == "hit" {
		// Cache hit rate calculation would be more complex in practice
		ro.stats.CacheStats.CacheHitRate += 0.01 // Simplified increment
	}

	ro.stats.LastOptimization = time.Now()

	// Update performance impact
	if ro.stats.TotalRequests == 1 {
		ro.stats.PerformanceImpact = data.ProcessingTime
	} else {
		// Moving average
		ro.stats.PerformanceImpact = time.Duration(
			(int64(ro.stats.PerformanceImpact)*int64(ro.stats.TotalRequests-1) + int64(data.ProcessingTime)) / int64(ro.stats.TotalRequests),
		)
	}

	if ro.metrics != nil {
		ro.metrics.Counter("forge.ai.response_optimized").Inc()
		ro.metrics.Histogram("forge.ai.optimization_time").Observe(data.ProcessingTime.Seconds())
		if data.OriginalSize > 0 && data.CompressedSize > 0 {
			ro.metrics.Histogram("forge.ai.compression_ratio").Observe(data.CompressionRatio)
		}
	}
}

// determineCacheStatus determines the cache status of a request
func (ro *ResponseOptimization) determineCacheStatus(req *http.Request, writer *OptimizedResponseWriter) string {
	if req.Header.Get("If-Modified-Since") != "" || req.Header.Get("If-None-Match") != "" {
		if writer.statusCode == http.StatusNotModified {
			return "hit"
		}
	}

	cacheControl := req.Header.Get("Cache-Control")
	if strings.Contains(cacheControl, "no-cache") || strings.Contains(cacheControl, "no-store") {
		return "no-cache"
	}

	return "miss"
}

// optimizationLoop runs periodic optimization tasks
func (ro *ResponseOptimization) optimizationLoop(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			ro.performOptimizationAnalysis(ctx)
		}
	}
}

// performOptimizationAnalysis analyzes optimization effectiveness
func (ro *ResponseOptimization) performOptimizationAnalysis(ctx context.Context) {
	ro.mu.RLock()
	bufferCopy := make([]ResponseData, len(ro.responseBuffer))
	copy(bufferCopy, ro.responseBuffer)
	ro.mu.RUnlock()

	if len(bufferCopy) < 10 {
		return
	}

	// Analyze optimization effectiveness
	input := ai.AgentInput{
		Type: "optimize_caching",
		Data: map[string]interface{}{
			"response_data":     bufferCopy,
			"current_stats":     ro.stats,
			"target_bandwidth":  ro.config.BandwidthSavingTarget,
			"target_cache_rate": ro.config.CacheHitRateTarget,
		},
		Context: map[string]interface{}{
			"analysis_type": "effectiveness_analysis",
		},
		RequestID: fmt.Sprintf("analysis-%d", time.Now().UnixNano()),
		Timestamp: time.Now(),
	}

	output, err := ro.agent.Process(ctx, input)
	if err != nil {
		if ro.logger != nil {
			ro.logger.Error("optimization analysis failed", logger.Error(err))
		}
		return
	}

	// Apply recommendations from analysis
	ro.applyOptimizationRecommendations(output)
}

// applyOptimizationRecommendations applies AI recommendations
func (ro *ResponseOptimization) applyOptimizationRecommendations(output ai.AgentOutput) {
	if recommendations, ok := output.Data.(map[string]interface{}); ok {
		ro.mu.Lock()
		defer ro.mu.Unlock()

		// Update optimal compression level
		if level, exists := recommendations["optimal_compression_level"].(float64); exists {
			ro.config.CompressionLevel = int(level)
		}

		// Update cache optimization settings
		if cacheSettings, ok := recommendations["cache_settings"].(map[string]interface{}); ok {
			for path, headers := range cacheSettings {
				if headerMap, ok := headers.(map[string]interface{}); ok {
					pathHeaders := make(map[string]string)
					for k, v := range headerMap {
						if strValue, ok := v.(string); ok {
							pathHeaders[k] = strValue
						}
					}
					ro.stats.CacheStats.CacheHeadersByPath[path] = pathHeaders
				}
			}
		}

		if ro.logger != nil {
			ro.logger.Debug("applied optimization recommendations",
				logger.Float64("confidence", output.Confidence),
				logger.String("explanation", output.Explanation),
			)
		}
	}
}

// patternLearningLoop learns optimization patterns
func (ro *ResponseOptimization) patternLearningLoop(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			ro.learnOptimizationPatterns(ctx)
		}
	}
}

// learnOptimizationPatterns learns patterns for different paths
func (ro *ResponseOptimization) learnOptimizationPatterns(ctx context.Context) {
	ro.mu.RLock()
	bufferCopy := make([]ResponseData, len(ro.responseBuffer))
	copy(bufferCopy, ro.responseBuffer)
	ro.mu.RUnlock()

	// Group responses by path
	pathGroups := make(map[string][]ResponseData)
	for _, data := range bufferCopy {
		// Extract path from metadata (simplified)
		path := "/unknown" // Would extract from actual request data
		pathGroups[path] = append(pathGroups[path], data)
	}

	// Learn patterns for each path
	for path, responses := range pathGroups {
		if len(responses) < 5 {
			continue
		}

		ro.updatePathPattern(path, responses)
	}
}

// updatePathPattern updates optimization pattern for a path
func (ro *ResponseOptimization) updatePathPattern(path string, responses []ResponseData) {
	ro.mu.Lock()
	defer ro.mu.Unlock()

	pattern := ro.pathPatterns[path]
	if pattern == nil {
		pattern = &PathOptimizationPattern{
			Path:                path,
			OptimalCacheHeaders: make(map[string]string),
			Metadata:            make(map[string]interface{}),
		}
		ro.pathPatterns[path] = pattern
	}

	// Calculate average response size
	var totalSize int64
	var compressionEffectiveness float64
	validCompressions := 0

	for _, response := range responses {
		totalSize += response.OriginalSize
		if response.CompressedSize > 0 {
			compressionEffectiveness += 1.0 - response.CompressionRatio
			validCompressions++
		}
	}

	pattern.AverageResponseSize = totalSize / int64(len(responses))
	if validCompressions > 0 {
		pattern.CompressionEffectiveness = compressionEffectiveness / float64(validCompressions)
	}

	pattern.RequestCount += int64(len(responses))
	pattern.LastUpdated = time.Now()
}

// performanceMonitoringLoop monitors optimization performance
func (ro *ResponseOptimization) performanceMonitoringLoop(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			ro.monitorPerformance()
		}
	}
}

// monitorPerformance monitors optimization performance
func (ro *ResponseOptimization) monitorPerformance() {
	ro.mu.RLock()
	defer ro.mu.RUnlock()

	// Update top optimized paths
	pathStats := make([]PathOptimizationStat, 0, len(ro.pathPatterns))
	for path, pattern := range ro.pathPatterns {
		stat := PathOptimizationStat{
			Path:              path,
			RequestCount:      pattern.RequestCount,
			AverageBytesSaved: float64(pattern.AverageResponseSize) * pattern.CompressionEffectiveness,
		}
		pathStats = append(pathStats, stat)
	}

	// Sort by bytes saved
	sort.Slice(pathStats, func(i, j int) bool {
		return pathStats[i].AverageBytesSaved > pathStats[j].AverageBytesSaved
	})

	// Keep top 10
	if len(pathStats) > 10 {
		pathStats = pathStats[:10]
	}

	ro.stats.TopOptimizedPaths = pathStats
}

// learnFromOptimization learns from individual optimization results
func (ro *ResponseOptimization) learnFromOptimization(req *http.Request, data ResponseData, decision *OptimizationDecision) {
	// Create feedback based on optimization effectiveness
	success := true
	actualSaving := 0.0

	if data.OriginalSize > 0 && data.CompressedSize > 0 {
		actualSaving = 1.0 - (float64(data.CompressedSize) / float64(data.OriginalSize))

		// Consider optimization successful if actual saving is close to predicted
		if math.Abs(actualSaving-decision.PredictedSaving) > 0.2 {
			success = false
		}
	}

	feedback := ai.AgentFeedback{
		ActionID: fmt.Sprintf("optimization-%s-%d", req.URL.Path, time.Now().UnixNano()),
		Success:  success,
		Outcome: map[string]interface{}{
			"actual_saving":    actualSaving,
			"predicted_saving": decision.PredictedSaving,
			"compression_time": data.CompressionTime,
			"original_size":    data.OriginalSize,
			"compressed_size":  data.CompressedSize,
		},
		Metrics: map[string]float64{
			"optimization_accuracy": 1.0 - math.Abs(actualSaving-decision.PredictedSaving),
			"performance_impact":    data.ProcessingTime.Seconds(),
		},
		Context: map[string]interface{}{
			"path":              req.URL.Path,
			"optimization_type": decision.OptimizationType,
			"confidence":        decision.Confidence,
		},
		Timestamp: time.Now(),
	}

	if err := ro.agent.Learn(context.Background(), feedback); err != nil {
		if ro.logger != nil {
			ro.logger.Error("failed to learn from optimization", logger.Error(err))
		}
	}
}

// OptimizedResponseWriter methods
func (w *OptimizedResponseWriter) Write(data []byte) (int, error) {
	return w.buffer.Write(data)
}

func (w *OptimizedResponseWriter) WriteHeader(statusCode int) {
	w.statusCode = statusCode
}

func (w *OptimizedResponseWriter) Header() http.Header {
	return w.ResponseWriter.Header()
}

// Helper functions
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
func (ro *ResponseOptimization) GetStats() ai.AIMiddlewareStats {
	ro.mu.RLock()
	defer ro.mu.RUnlock()

	return ai.AIMiddlewareStats{
		Name:            ro.Name(),
		Type:            string(ro.Type()),
		RequestsTotal:   ro.stats.TotalRequests,
		RequestsBlocked: 0, // Not applicable for optimization
		AverageLatency:  ro.stats.PerformanceImpact,
		LearningEnabled: ro.config.LearningEnabled,
		AdaptiveChanges: 0, // Could track optimization setting changes
		LastUpdated:     ro.stats.LastOptimization,
		CustomMetrics: map[string]interface{}{
			"optimized_responses":      ro.stats.OptimizedResponses,
			"bytes_saved":              ro.stats.BytesSaved,
			"bandwidth_saving_percent": ro.stats.BandwidthSavingPercent,
			"compression_stats":        ro.stats.CompressionStats,
			"cache_stats":              ro.stats.CacheStats,
			"content_stats":            ro.stats.ContentStats,
			"header_stats":             ro.stats.HeaderStats,
			"top_optimized_paths":      ro.stats.TopOptimizedPaths,
			"optimization_accuracy":    ro.stats.OptimizationAccuracy,
		},
	}
}
