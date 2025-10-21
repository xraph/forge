package middleware

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/xraph/forge/v2"
	ai "github.com/xraph/forge/v2/extensions/ai/internal"
	"github.com/xraph/forge/v2/internal/logger"
)

// PersonalizationConfig contains configuration for AI-powered content personalization
type PersonalizationConfig struct {
	Enabled              bool          `yaml:"enabled" default:"true"`
	MaxContentSize       int64         `yaml:"max_content_size" default:"1048576"`      // 1MB max content size
	PersonalizationLevel int           `yaml:"personalization_level" default:"2"`       // Personalization aggressiveness (1-3)
	LearningEnabled      bool          `yaml:"learning_enabled" default:"true"`         // Enable learning from user interactions
	RealTimeEnabled      bool          `yaml:"realtime_enabled" default:"false"`        // Enable real-time personalization
	CachingEnabled       bool          `yaml:"caching_enabled" default:"true"`          // Enable personalized content caching
	ModelID              string        `yaml:"model_id" default:"content-personalizer"` // AI model for personalization
	Timeout              time.Duration `yaml:"timeout" default:"200ms"`                 // Personalization timeout
	MinPersonalizeLength int           `yaml:"min_personalize_length" default:"500"`    // Minimum content length to personalize
	UserSegments         []UserSegment `yaml:"user_segments"`                           // Predefined user segments
	ContentTypes         []string      `yaml:"content_types"`                           // Content types to personalize
	ABTestingEnabled     bool          `yaml:"ab_testing_enabled" default:"false"`      // Enable A/B testing
	PrivacyLevel         PrivacyLevel  `yaml:"privacy_level" default:"balanced"`        // Privacy protection level
}

// UserSegment defines a user segment for personalization
type UserSegment struct {
	ID          string                 `yaml:"id"`
	Name        string                 `yaml:"name"`
	Description string                 `yaml:"description"`
	Criteria    map[string]interface{} `yaml:"criteria"`
	Weight      float64                `yaml:"weight" default:"1.0"`
}

// PrivacyLevel defines privacy protection levels
type PrivacyLevel string

const (
	PrivacyLevelMinimal  PrivacyLevel = "minimal"  // Maximum personalization, minimal privacy
	PrivacyLevelBalanced PrivacyLevel = "balanced" // Balanced personalization and privacy
	PrivacyLevelStrict   PrivacyLevel = "strict"   // Minimal personalization, maximum privacy
)

// responseCapture captures response data for personalization
type responseCapture struct {
	http.ResponseWriter
	buffer     bytes.Buffer
	statusCode int
	headers    http.Header
}

// PersonalizationStats contains statistics for content personalization
type PersonalizationStats struct {
	TotalRequests          int64                       `json:"total_requests"`
	PersonalizedContent    int64                       `json:"personalized_content"`
	CacheHits              int64                       `json:"cache_hits"`
	CacheMisses            int64                       `json:"cache_misses"`
	PersonalizationLatency time.Duration               `json:"personalization_latency"`
	UserSegmentStats       map[string]UserSegmentStats `json:"user_segment_stats"`
	ContentTypeStats       map[string]ContentTypeStats `json:"content_type_stats"`
	ABTestResults          map[string]ABTestResult     `json:"ab_test_results"`
	ErrorCount             int64                       `json:"error_count"`
	SkippedCount           int64                       `json:"skipped_count"`
	LearningMetrics        ai.LearningMetrics          `json:"learning_metrics"`
	LastUpdated            time.Time                   `json:"last_updated"`
}

// UserSegmentStats contains statistics for a user segment
type UserSegmentStats struct {
	UserCount             int64   `json:"user_count"`
	PersonalizationRate   float64 `json:"personalization_rate"`
	EngagementImprovement float64 `json:"engagement_improvement"`
	ConversionImprovement float64 `json:"conversion_improvement"`
}

// ContentTypeStats contains statistics for content type personalization
type ContentTypeStats struct {
	RequestCount       int64   `json:"request_count"`
	PersonalizedCount  int64   `json:"personalized_count"`
	AverageImprovement float64 `json:"average_improvement"`
}

// ABTestResult contains A/B test results
type ABTestResult struct {
	TestID       string    `json:"test_id"`
	VariantA     string    `json:"variant_a"`
	VariantB     string    `json:"variant_b"`
	ParticipantA int64     `json:"participant_a"`
	ParticipantB int64     `json:"participant_b"`
	ConversionA  float64   `json:"conversion_a"`
	ConversionB  float64   `json:"conversion_b"`
	Significance float64   `json:"significance"`
	Winner       string    `json:"winner"`
	LastUpdated  time.Time `json:"last_updated"`
}

// UserProfile represents a user's personalization profile
type UserProfile struct {
	UserID       string                 `json:"user_id"`
	Segment      string                 `json:"segment"`
	Preferences  map[string]interface{} `json:"preferences"`
	Behavior     UserBehavior           `json:"behavior"`
	Demographics Demographics           `json:"demographics"`
	Interests    []string               `json:"interests"`
	History      []InteractionHistory   `json:"history"`
	LastUpdated  time.Time              `json:"last_updated"`
	PrivacyLevel PrivacyLevel           `json:"privacy_level"`
}

// UserBehavior tracks user behavior patterns
type UserBehavior struct {
	ClickThroughRate     float64  `json:"click_through_rate"`
	TimeOnPage           float64  `json:"time_on_page"`
	BounceRate           float64  `json:"bounce_rate"`
	ConversionRate       float64  `json:"conversion_rate"`
	PreferredContentType string   `json:"preferred_content_type"`
	ActiveHours          []int    `json:"active_hours"`
	DevicePreference     string   `json:"device_preference"`
	LocationHistory      []string `json:"location_history"`
}

// Demographics contains user demographic information
type Demographics struct {
	AgeRange   string `json:"age_range"`
	Gender     string `json:"gender"`
	Location   string `json:"location"`
	Language   string `json:"language"`
	Timezone   string `json:"timezone"`
	Income     string `json:"income"`
	Education  string `json:"education"`
	Occupation string `json:"occupation"`
}

// InteractionHistory tracks user interactions
type InteractionHistory struct {
	Timestamp  time.Time              `json:"timestamp"`
	Action     string                 `json:"action"`
	Content    string                 `json:"content"`
	Context    map[string]interface{} `json:"context"`
	Engagement float64                `json:"engagement"`
}

// PersonalizedContent represents personalized content
type PersonalizedContent struct {
	Original            []byte                `json:"original"`
	Personalized        []byte                `json:"personalized"`
	UserID              string                `json:"user_id"`
	Segment             string                `json:"segment"`
	Personalizations    []PersonalizationRule `json:"personalizations"`
	Confidence          float64               `json:"confidence"`
	ProcessingTime      time.Duration         `json:"processing_time"`
	CacheKey            string                `json:"cache_key,omitempty"`
	ABTestVariant       string                `json:"ab_test_variant,omitempty"`
	PersonalizedContent interface{}           `json:"personalized_content,omitempty"`
}

// PersonalizationRule represents a personalization rule applied
type PersonalizationRule struct {
	Type        string                 `json:"type"` // content_swap, recommendation, layout_change, etc.
	Description string                 `json:"description"`
	Target      string                 `json:"target"` // CSS selector or content identifier
	Original    string                 `json:"original"`
	Modified    string                 `json:"modified"`
	Confidence  float64                `json:"confidence"`
	Reason      string                 `json:"reason"`
	Metadata    map[string]interface{} `json:"metadata"`
}

// ContentPersonalization implements AI-powered content personalization middleware
type ContentPersonalization struct {
	config       PersonalizationConfig
	agent        ai.AIAgent
	logger       forge.Logger
	metrics      forge.Metrics
	stats        PersonalizationStats
	userProfiles map[string]*UserProfile
	cache        map[string]*PersonalizedContent
	mu           sync.RWMutex
	started      bool
}

// NewContentPersonalization creates a new content personalization middleware
func NewContentPersonalization(config PersonalizationConfig, logger forge.Logger, metrics forge.Metrics) (*ContentPersonalization, error) {
	// Create AI agent for content personalization
	capabilities := []ai.Capability{
		{
			Name:        "personalize_content",
			Description: "Personalize content based on user profile and behavior",
			Metadata: map[string]interface{}{
				"type":       "personalization",
				"strategies": []string{"content_swap", "recommendation", "layout_change", "language_adapt"},
			},
		},
		{
			Name:        "segment_user",
			Description: "Automatically segment users based on behavior and preferences",
			Metadata: map[string]interface{}{
				"type":     "segmentation",
				"segments": config.UserSegments,
			},
		},
		{
			Name:        "recommend_content",
			Description: "Generate personalized content recommendations",
			Metadata: map[string]interface{}{
				"type":       "recommendation",
				"algorithms": []string{"collaborative", "content_based", "hybrid"},
			},
		},
		{
			Name:        "ab_test_optimize",
			Description: "Optimize content using A/B testing results",
			Metadata: map[string]interface{}{
				"type":    "optimization",
				"enabled": config.ABTestingEnabled,
			},
		},
	}

	agent := ai.NewBaseAgent("content-personalizer", "Content Personalizer", ai.AgentTypeOptimization, capabilities)

	return &ContentPersonalization{
		config:       config,
		agent:        agent,
		logger:       logger,
		metrics:      metrics,
		userProfiles: make(map[string]*UserProfile),
		cache:        make(map[string]*PersonalizedContent),
		stats: PersonalizationStats{
			UserSegmentStats: make(map[string]UserSegmentStats),
			ContentTypeStats: make(map[string]ContentTypeStats),
			ABTestResults:    make(map[string]ABTestResult),
			LastUpdated:      time.Now(),
		},
	}, nil
}

// Name returns the middleware name
func (cp *ContentPersonalization) Name() string {
	return "content-personalization"
}

// Type returns the middleware type
func (cp *ContentPersonalization) Type() ai.AIMiddlewareType {
	return ai.AIMiddlewareTypeResponseOptimization
}

// Initialize initializes the middleware
func (cp *ContentPersonalization) Initialize(ctx context.Context, config ai.AIMiddlewareConfig) error {
	cp.mu.Lock()
	defer cp.mu.Unlock()

	if cp.started {
		return fmt.Errorf("content personalization middleware already initialized")
	}

	if !cp.config.Enabled {
		cp.started = true
		return nil // Skip initialization if disabled
	}

	// Initialize the AI agent
	agentConfig := ai.AgentConfig{
		MaxConcurrency:  10,
		Timeout:         cp.config.Timeout,
		LearningEnabled: cp.config.LearningEnabled,
		AutoApply:       true,
		Logger:          cp.logger,
		Metrics:         cp.metrics,
	}

	if err := cp.agent.Initialize(ctx, agentConfig); err != nil {
		return fmt.Errorf("failed to initialize personalization agent: %w", err)
	}

	if err := cp.agent.Start(ctx); err != nil {
		return fmt.Errorf("failed to start personalization agent: %w", err)
	}

	// Start background tasks
	go cp.cleanupProfiles(ctx)
	go cp.cleanupCache(ctx)
	if cp.config.LearningEnabled {
		go cp.updateUserSegments(ctx)
	}

	cp.started = true

	if cp.logger != nil {
		cp.logger.Info("content personalization middleware initialized",
			logger.Bool("enabled", cp.config.Enabled),
			logger.Bool("realtime_enabled", cp.config.RealTimeEnabled),
			logger.Bool("ab_testing_enabled", cp.config.ABTestingEnabled),
			logger.String("privacy_level", string(cp.config.PrivacyLevel)),
			logger.Int("personalization_level", cp.config.PersonalizationLevel),
		)
	}

	return nil
}

// Process processes HTTP requests with content personalization
func (cp *ContentPersonalization) Process(ctx context.Context, req *http.Request, resp http.ResponseWriter, next http.HandlerFunc) error {
	if !cp.config.Enabled {
		next.ServeHTTP(resp, req)
		return nil
	}

	start := time.Now()

	// Extract user identification
	userID := cp.extractUserID(req)
	if userID == "" {
		// No personalization without user identification
		next.ServeHTTP(resp, req)
		return nil
	}

	// Create response wrapper to capture content
	wrapper := &responseCapture{
		ResponseWriter: resp,
		buffer:         bytes.Buffer{},
		statusCode:     http.StatusOK,
		headers:        make(http.Header),
	}

	// Process request
	next.ServeHTTP(wrapper, req)

	// Skip personalization for certain conditions
	if cp.shouldSkipPersonalization(wrapper, req) {
		cp.updateSkippedStats()
		cp.serveOriginalResponse(resp, wrapper)
		return nil
	}

	// Get or create user profile
	profile := cp.getUserProfile(userID, req)

	// Check cache for personalized content
	cacheKey := cp.generateCacheKey(userID, profile.Segment, req, wrapper)
	if cp.config.CachingEnabled {
		if cached := cp.getCachedContent(cacheKey); cached != nil {
			cp.servePersonalizedContent(resp, cached)
			cp.updateCacheStats(true)
			return nil
		}
		cp.updateCacheStats(false)
	}

	// Personalize content using AI
	personalized, err := cp.personalizeContent(ctx, req, wrapper, profile)
	if err != nil {
		cp.updateErrorStats(err)
		cp.serveOriginalResponse(resp, wrapper)
		return nil
	}

	// Cache personalized content
	if cp.config.CachingEnabled {
		cp.cacheContent(cacheKey, personalized)
	}

	// Serve personalized content
	cp.servePersonalizedContent(resp, personalized)

	// Update statistics
	cp.updatePersonalizationStats(profile.Segment, wrapper.Header().Get("Content-Type"), time.Since(start))

	// Track user interaction for learning
	if cp.config.LearningEnabled {
		go cp.trackInteraction(ctx, userID, req, wrapper, personalized)
	}

	return nil
}

// shouldSkipPersonalization determines if personalization should be skipped
func (cp *ContentPersonalization) shouldSkipPersonalization(wrapper *responseCapture, req *http.Request) bool {
	// Skip if content is too small
	if wrapper.buffer.Len() < cp.config.MinPersonalizeLength {
		return true
	}

	// Skip if content is too large
	if int64(wrapper.buffer.Len()) > cp.config.MaxContentSize {
		return true
	}

	// Skip for error responses
	if wrapper.statusCode >= 400 {
		return true
	}

	// Skip for non-personalizable content types
	contentType := wrapper.Header().Get("Content-Type")
	if !cp.isPersonalizableContentType(contentType) {
		return true
	}

	return false
}

// isPersonalizableContentType checks if content type can be personalized
func (cp *ContentPersonalization) isPersonalizableContentType(contentType string) bool {
	if len(cp.config.ContentTypes) == 0 {
		// Default personalizable types
		personalizableTypes := []string{
			"text/html",
			"application/json",
			"text/plain",
			"application/xml",
			"text/xml",
		}

		contentType = strings.ToLower(contentType)
		for _, pType := range personalizableTypes {
			if strings.Contains(contentType, pType) {
				return true
			}
		}
		return false
	}

	// Check configured content types
	contentType = strings.ToLower(contentType)
	for _, cType := range cp.config.ContentTypes {
		if strings.Contains(contentType, strings.ToLower(cType)) {
			return true
		}
	}

	return false
}

// extractUserID extracts user ID from the request
func (cp *ContentPersonalization) extractUserID(req *http.Request) string {
	// Try various sources for user identification
	if userID := req.Header.Get("X-User-ID"); userID != "" {
		return userID
	}

	if userID := req.Header.Get("Authorization"); userID != "" && strings.HasPrefix(userID, "Bearer ") {
		// Extract user ID from JWT token (simplified)
		return fmt.Sprintf("jwt:%s", userID[7:20])
	}

	if userID := req.URL.Query().Get("user_id"); userID != "" {
		return userID
	}

	// Fallback to session or cookie-based identification
	if cookie, err := req.Cookie("user_id"); err == nil {
		return cookie.Value
	}

	return ""
}

// getUserProfile gets or creates a user profile
func (cp *ContentPersonalization) getUserProfile(userID string, req *http.Request) *UserProfile {
	cp.mu.RLock()
	if profile, exists := cp.userProfiles[userID]; exists {
		cp.mu.RUnlock()
		return profile
	}
	cp.mu.RUnlock()

	// Create new profile
	cp.mu.Lock()
	defer cp.mu.Unlock()

	// Double-check after acquiring write lock
	if profile, exists := cp.userProfiles[userID]; exists {
		return profile
	}

	profile := &UserProfile{
		UserID:       userID,
		Segment:      cp.determineUserSegment(req),
		Preferences:  make(map[string]interface{}),
		Behavior:     UserBehavior{},
		Demographics: cp.extractDemographics(req),
		Interests:    []string{},
		History:      []InteractionHistory{},
		LastUpdated:  time.Now(),
		PrivacyLevel: cp.config.PrivacyLevel,
	}

	cp.userProfiles[userID] = profile
	return profile
}

// determineUserSegment determines user segment based on request
func (cp *ContentPersonalization) determineUserSegment(req *http.Request) string {
	// Simple segmentation logic - in production, use AI agent
	userAgent := strings.ToLower(req.UserAgent())

	if strings.Contains(userAgent, "mobile") {
		return "mobile"
	}
	if strings.Contains(userAgent, "tablet") {
		return "tablet"
	}

	// Check for configured segments
	for _, segment := range cp.config.UserSegments {
		if cp.matchesSegmentCriteria(req, segment.Criteria) {
			return segment.ID
		}
	}

	return "default"
}

// matchesSegmentCriteria checks if request matches segment criteria
func (cp *ContentPersonalization) matchesSegmentCriteria(req *http.Request, criteria map[string]interface{}) bool {
	// Simple criteria matching - extend based on needs
	for key, value := range criteria {
		switch key {
		case "user_agent":
			if !strings.Contains(strings.ToLower(req.UserAgent()), strings.ToLower(value.(string))) {
				return false
			}
		case "language":
			if !strings.Contains(req.Header.Get("Accept-Language"), value.(string)) {
				return false
			}
		}
	}
	return true
}

// extractDemographics extracts user demographics from request
func (cp *ContentPersonalization) extractDemographics(req *http.Request) Demographics {
	demographics := Demographics{}

	// Extract language
	if acceptLang := req.Header.Get("Accept-Language"); acceptLang != "" {
		// Simple language extraction
		if len(acceptLang) >= 2 {
			demographics.Language = acceptLang[:2]
		}
	}

	// Extract location from headers (if available)
	if location := req.Header.Get("X-User-Location"); location != "" {
		demographics.Location = location
	}

	return demographics
}

// personalizeContent personalizes content using AI
func (cp *ContentPersonalization) personalizeContent(ctx context.Context, req *http.Request, wrapper *responseCapture, profile *UserProfile) (*PersonalizedContent, error) {
	originalContent := wrapper.buffer.Bytes()
	contentType := wrapper.Header().Get("Content-Type")

	// Create AI input for personalization
	input := ai.AgentInput{
		Type: "personalize_content",
		Data: map[string]interface{}{
			"content":      originalContent,
			"content_type": contentType,
			"user_profile": profile,
			"request_context": map[string]interface{}{
				"path":       req.URL.Path,
				"method":     req.Method,
				"user_agent": req.UserAgent(),
				"headers":    cp.extractRelevantHeaders(req),
			},
		},
		Context: map[string]interface{}{
			"personalization_level": cp.config.PersonalizationLevel,
			"privacy_level":         cp.config.PrivacyLevel,
			"realtime_enabled":      cp.config.RealTimeEnabled,
		},
		RequestID: fmt.Sprintf("personalize-%s-%d", profile.UserID, time.Now().UnixNano()),
		Timestamp: time.Now(),
	}

	// Process through AI agent
	output, err := cp.agent.Process(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("AI personalization failed: %w", err)
	}

	// Parse personalization result
	personalized, err := cp.parsePersonalizationOutput(output, originalContent, profile)
	if err != nil {
		return nil, fmt.Errorf("failed to parse personalization output: %w", err)
	}

	return personalized, nil
}

// parsePersonalizationOutput parses AI agent output into PersonalizedContent
func (cp *ContentPersonalization) parsePersonalizationOutput(output ai.AgentOutput, originalContent []byte, profile *UserProfile) (*PersonalizedContent, error) {
	data, ok := output.Data.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid personalization output format")
	}

	// Default to original content if personalization fails
	personalizedContent := originalContent
	var personalizations []PersonalizationRule

	// Extract personalized content if available
	if content, exists := data["personalized_content"]; exists {
		if contentBytes, ok := content.([]byte); ok {
			personalizedContent = contentBytes
		} else if contentStr, ok := content.(string); ok {
			personalizedContent = []byte(contentStr)
		}
	}

	// Extract personalization rules applied
	if rulesData, exists := data["personalizations"].([]interface{}); exists {
		for _, rule := range rulesData {
			if ruleMap, ok := rule.(map[string]interface{}); ok {
				personalization := PersonalizationRule{
					Type:        getStringValue(ruleMap, "type"),
					Description: getStringValue(ruleMap, "description"),
					Target:      getStringValue(ruleMap, "target"),
					Original:    getStringValue(ruleMap, "original"),
					Modified:    getStringValue(ruleMap, "modified"),
					Confidence:  getFloat64Value(ruleMap, "confidence"),
					Reason:      getStringValue(ruleMap, "reason"),
					Metadata:    getMapValue(ruleMap, "metadata"),
				}
				personalizations = append(personalizations, personalization)
			}
		}
	}

	return &PersonalizedContent{
		Original:         originalContent,
		Personalized:     personalizedContent,
		UserID:           profile.UserID,
		Segment:          profile.Segment,
		Personalizations: personalizations,
		Confidence:       output.Confidence,
		ProcessingTime:   time.Since(time.Now()), // Will be corrected by caller
	}, nil
}

// extractRelevantHeaders extracts headers relevant for personalization
func (cp *ContentPersonalization) extractRelevantHeaders(req *http.Request) map[string]string {
	relevant := map[string]string{
		"accept":          req.Header.Get("Accept"),
		"accept-language": req.Header.Get("Accept-Language"),
		"user-agent":      req.Header.Get("User-Agent"),
	}

	// Add custom preference headers if present
	if prefs := req.Header.Get("X-User-Preferences"); prefs != "" {
		relevant["preferences"] = prefs
	}

	return relevant
}

// generateCacheKey generates a cache key for personalized content
func (cp *ContentPersonalization) generateCacheKey(userID, segment string, req *http.Request, wrapper *responseCapture) string {
	return fmt.Sprintf("%s:%s:%s:%x", userID, segment, req.URL.Path, cp.hashContent(wrapper.buffer.String()))
}

// hashContent creates a hash of content for cache keys
func (cp *ContentPersonalization) hashContent(content string) string {
	// Simple hash implementation
	hash := 0
	for _, char := range content {
		hash = hash*31 + int(char)
	}
	return fmt.Sprintf("%x", hash)
}

// getCachedContent retrieves cached personalized content
func (cp *ContentPersonalization) getCachedContent(cacheKey string) *PersonalizedContent {
	cp.mu.RLock()
	defer cp.mu.RUnlock()

	if content, exists := cp.cache[cacheKey]; exists {
		return content
	}

	return nil
}

// cacheContent caches personalized content
func (cp *ContentPersonalization) cacheContent(cacheKey string, content *PersonalizedContent) {
	cp.mu.Lock()
	defer cp.mu.Unlock()

	// Simple cache with size limit
	if len(cp.cache) > 1000 {
		// Remove oldest entries
		for k := range cp.cache {
			delete(cp.cache, k)
			if len(cp.cache) <= 800 {
				break
			}
		}
	}

	content.CacheKey = cacheKey
	cp.cache[cacheKey] = content
}

// serveOriginalResponse serves the original response
func (cp *ContentPersonalization) serveOriginalResponse(resp http.ResponseWriter, wrapper *responseCapture) {
	// Copy headers
	for key, values := range wrapper.Header() {
		for _, value := range values {
			resp.Header().Add(key, value)
		}
	}

	resp.WriteHeader(wrapper.statusCode)
	resp.Write(wrapper.buffer.Bytes())
}

// servePersonalizedContent serves personalized content
func (cp *ContentPersonalization) servePersonalizedContent(resp http.ResponseWriter, content *PersonalizedContent) {
	// Copy original headers
	// Note: Headers would need to be stored in the PersonalizedContent struct
	// For now, we'll skip this functionality

	// Add personalization info headers
	resp.Header().Set("X-Personalized", "true")
	resp.Header().Set("X-User-Segment", content.Segment)
	if len(content.Personalizations) > 0 {
		resp.Header().Set("X-Personalization-Count", fmt.Sprintf("%d", len(content.Personalizations)))
	}

	resp.WriteHeader(http.StatusOK)
	resp.Write(content.Personalized)
}

// trackInteraction tracks user interaction for learning
func (cp *ContentPersonalization) trackInteraction(ctx context.Context, userID string, req *http.Request, wrapper *responseCapture, personalized *PersonalizedContent) {
	interaction := InteractionHistory{
		Timestamp: time.Now(),
		Action:    "view",
		Content:   req.URL.Path,
		Context: map[string]interface{}{
			"method":       req.Method,
			"personalized": len(personalized.Personalizations) > 0,
			"segment":      personalized.Segment,
			"confidence":   personalized.Confidence,
		},
		Engagement: 1.0, // Default engagement score
	}

	// Update user profile with interaction
	cp.mu.Lock()
	if profile, exists := cp.userProfiles[userID]; exists {
		profile.History = append(profile.History, interaction)

		// Keep only recent history (last 100 interactions)
		if len(profile.History) > 100 {
			profile.History = profile.History[len(profile.History)-100:]
		}

		profile.LastUpdated = time.Now()
	}
	cp.mu.Unlock()

	// Create feedback for AI agent
	feedback := ai.AgentFeedback{
		ActionID: fmt.Sprintf("personalize-%s-%d", userID, time.Now().UnixNano()),
		Success:  len(personalized.Personalizations) > 0,
		Outcome: map[string]interface{}{
			"personalizations_applied": len(personalized.Personalizations),
			"confidence":               personalized.Confidence,
		},
		Metrics: map[string]float64{
			"processing_time": personalized.ProcessingTime.Seconds(),
			"confidence":      personalized.Confidence,
		},
		Context: map[string]interface{}{
			"user_id": userID,
			"segment": personalized.Segment,
			"path":    req.URL.Path,
		},
		Timestamp: time.Now(),
	}

	if err := cp.agent.Learn(ctx, feedback); err != nil {
		if cp.logger != nil {
			cp.logger.Error("failed to learn from personalization",
				logger.String("user_id", userID),
				logger.Error(err),
			)
		}
	}
}

// Background maintenance tasks
func (cp *ContentPersonalization) cleanupProfiles(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cp.performProfileCleanup()
		}
	}
}

func (cp *ContentPersonalization) cleanupCache(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cp.performCacheCleanup()
		}
	}
}

func (cp *ContentPersonalization) updateUserSegments(ctx context.Context) {
	ticker := time.NewTicker(15 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cp.recomputeUserSegments()
		}
	}
}

func (cp *ContentPersonalization) performProfileCleanup() {
	cp.mu.Lock()
	defer cp.mu.Unlock()

	cutoff := time.Now().Add(-24 * time.Hour)
	for userID, profile := range cp.userProfiles {
		if profile.LastUpdated.Before(cutoff) {
			delete(cp.userProfiles, userID)
		}
	}
}

func (cp *ContentPersonalization) performCacheCleanup() {
	cp.mu.Lock()
	defer cp.mu.Unlock()

	if len(cp.cache) > 500 {
		newCache := make(map[string]*PersonalizedContent)
		count := 0
		for k, v := range cp.cache {
			if count < 400 {
				newCache[k] = v
				count++
			}
		}
		cp.cache = newCache
	}
}

func (cp *ContentPersonalization) recomputeUserSegments() {
	// Recompute user segments based on recent behavior
	cp.mu.RLock()
	users := make([]*UserProfile, 0, len(cp.userProfiles))
	for _, profile := range cp.userProfiles {
		users = append(users, profile)
	}
	cp.mu.RUnlock()

	// Simple re-segmentation logic
	for _, profile := range users {
		if len(profile.History) > 10 {
			// Analyze user behavior patterns and update segment if needed
			newSegment := cp.analyzeUserBehavior(profile)
			if newSegment != profile.Segment {
				cp.mu.Lock()
				profile.Segment = newSegment
				profile.LastUpdated = time.Now()
				cp.mu.Unlock()
			}
		}
	}
}

func (cp *ContentPersonalization) analyzeUserBehavior(profile *UserProfile) string {
	// Simple behavior analysis - in production, use AI agent
	mobileCount := 0
	for _, interaction := range profile.History {
		if context, ok := interaction.Context["user_agent"].(string); ok {
			if strings.Contains(strings.ToLower(context), "mobile") {
				mobileCount++
			}
		}
	}

	if float64(mobileCount)/float64(len(profile.History)) > 0.7 {
		return "mobile_heavy"
	}

	return profile.Segment
}

// Statistics update methods
func (cp *ContentPersonalization) updatePersonalizationStats(segment, contentType string, duration time.Duration) {
	cp.mu.Lock()
	defer cp.mu.Unlock()

	cp.stats.TotalRequests++
	cp.stats.PersonalizedContent++
	cp.stats.PersonalizationLatency = (cp.stats.PersonalizationLatency + duration) / 2
	cp.stats.LastUpdated = time.Now()

	// Update segment stats
	if segmentStats, exists := cp.stats.UserSegmentStats[segment]; exists {
		segmentStats.PersonalizationRate = (segmentStats.PersonalizationRate + 1.0) / 2
		cp.stats.UserSegmentStats[segment] = segmentStats
	} else {
		cp.stats.UserSegmentStats[segment] = UserSegmentStats{
			UserCount:           1,
			PersonalizationRate: 1.0,
		}
	}

	// Update content type stats
	if ctStats, exists := cp.stats.ContentTypeStats[contentType]; exists {
		ctStats.RequestCount++
		ctStats.PersonalizedCount++
		cp.stats.ContentTypeStats[contentType] = ctStats
	} else {
		cp.stats.ContentTypeStats[contentType] = ContentTypeStats{
			RequestCount:      1,
			PersonalizedCount: 1,
		}
	}
}

func (cp *ContentPersonalization) updateCacheStats(hit bool) {
	cp.mu.Lock()
	defer cp.mu.Unlock()

	if hit {
		cp.stats.CacheHits++
	} else {
		cp.stats.CacheMisses++
	}
}

func (cp *ContentPersonalization) updateErrorStats(err error) {
	cp.mu.Lock()
	defer cp.mu.Unlock()

	cp.stats.ErrorCount++

	if cp.logger != nil {
		cp.logger.Error("content personalization error", logger.Error(err))
	}
}

func (cp *ContentPersonalization) updateSkippedStats() {
	cp.mu.Lock()
	defer cp.mu.Unlock()

	cp.stats.SkippedCount++
}

// GetStats returns middleware statistics
func (cp *ContentPersonalization) GetStats() ai.AIMiddlewareStats {
	cp.mu.RLock()
	defer cp.mu.RUnlock()

	return ai.AIMiddlewareStats{
		Name:            cp.Name(),
		Type:            string(cp.Type()),
		RequestsTotal:   cp.stats.TotalRequests,
		RequestsBlocked: 0, // Not applicable for personalization
		AverageLatency:  cp.stats.PersonalizationLatency,
		LearningEnabled: cp.config.LearningEnabled,
		AdaptiveChanges: cp.stats.PersonalizedContent,
		LastUpdated:     cp.stats.LastUpdated,
		CustomMetrics: map[string]interface{}{
			"personalized_content": cp.stats.PersonalizedContent,
			"cache_hits":           cp.stats.CacheHits,
			"cache_misses":         cp.stats.CacheMisses,
			"error_count":          cp.stats.ErrorCount,
			"skipped_count":        cp.stats.SkippedCount,
			"user_profiles":        len(cp.userProfiles),
			"user_segment_stats":   cp.stats.UserSegmentStats,
			"content_type_stats":   cp.stats.ContentTypeStats,
			"ab_test_results":      cp.stats.ABTestResults,
		},
	}
}

// Additional helper type for PersonalizedContent to store headers
type PersonalizedContentHeaders struct {
	Headers map[string]string `json:"headers"`
}

// Update PersonalizedContent to include headers properly
func (pc *PersonalizedContent) SetHeaders(headers map[string]string) {
	if pc.PersonalizedContent == nil {
		pc.PersonalizedContent = &PersonalizedContentHeaders{}
	}
	pc.PersonalizedContent.(*PersonalizedContentHeaders).Headers = headers
}

func (pc *PersonalizedContent) GetHeaders() map[string]string {
	if pc.PersonalizedContent != nil {
		if headers, ok := pc.PersonalizedContent.(*PersonalizedContentHeaders); ok {
			return headers.Headers
		}
	}
	return make(map[string]string)
}

// Helper functions for data extraction
func getStringValue(data map[string]interface{}, key string) string {
	if val, ok := data[key].(string); ok {
		return val
	}
	return ""
}

func getFloat64Value(data map[string]interface{}, key string) float64 {
	if val, ok := data[key].(float64); ok {
		return val
	}
	return 0.0
}

func getMapValue(data map[string]interface{}, key string) map[string]interface{} {
	if val, ok := data[key].(map[string]interface{}); ok {
		return val
	}
	return make(map[string]interface{})
}
