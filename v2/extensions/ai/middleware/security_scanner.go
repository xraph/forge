package middleware

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/xraph/forge/v2"
	ai "github.com/xraph/forge/v2/extensions/ai/internal"
	"github.com/xraph/forge/v2/internal/logger"
)

// SecurityThreatLevel represents the severity of a security threat
type SecurityThreatLevel string

const (
	SecurityThreatLevelLow      SecurityThreatLevel = "low"
	SecurityThreatLevelMedium   SecurityThreatLevel = "medium"
	SecurityThreatLevelHigh     SecurityThreatLevel = "high"
	SecurityThreatLevelCritical SecurityThreatLevel = "critical"
)

// SecurityThreat represents a detected security threat
type SecurityThreat struct {
	ID          string                 `json:"id"`
	Type        string                 `json:"type"`
	Level       SecurityThreatLevel    `json:"level"`
	Description string                 `json:"description"`
	Source      string                 `json:"source"`
	Indicators  []string               `json:"indicators"`
	Confidence  float64                `json:"confidence"`
	Timestamp   time.Time              `json:"timestamp"`
	Metadata    map[string]interface{} `json:"metadata"`
	Blocked     bool                   `json:"blocked"`
	Action      string                 `json:"action"`
}

// SecurityScannerConfig contains configuration for the AI security scanner
type SecurityScannerConfig struct {
	Enabled                  bool                `yaml:"enabled" default:"true"`
	BlockThreshold           SecurityThreatLevel `yaml:"block_threshold" default:"high"`
	ScanHeaders              bool                `yaml:"scan_headers" default:"true"`
	ScanBody                 bool                `yaml:"scan_body" default:"true"`
	ScanParams               bool                `yaml:"scan_params" default:"true"`
	MaxBodySize              int64               `yaml:"max_body_size" default:"10485760"` // 10MB
	Timeout                  time.Duration       `yaml:"timeout" default:"5s"`
	EnableMLDetection        bool                `yaml:"enable_ml_detection" default:"true"`
	EnableRuleBasedDetection bool                `yaml:"enable_rule_based_detection" default:"true"`
	EnableBehavioralAnalysis bool                `yaml:"enable_behavioral_analysis" default:"true"`
	LogLevel                 string              `yaml:"log_level" default:"info"`
	AlertWebhook             string              `yaml:"alert_webhook"`
	WhitelistIPs             []string            `yaml:"whitelist_ips"`
	BlacklistIPs             []string            `yaml:"blacklist_ips"`
	CustomRules              []SecurityRule      `yaml:"custom_rules"`
	MLModelPath              string              `yaml:"ml_model_path"`
}

// SecurityRule represents a custom security rule
type SecurityRule struct {
	ID          string              `yaml:"id"`
	Name        string              `yaml:"name"`
	Description string              `yaml:"description"`
	Pattern     string              `yaml:"pattern"`
	Target      SecurityRuleTarget  `yaml:"target"`
	Level       SecurityThreatLevel `yaml:"level"`
	Action      SecurityRuleAction  `yaml:"action"`
	Enabled     bool                `yaml:"enabled" default:"true"`
}

// SecurityRuleTarget defines what to scan
type SecurityRuleTarget string

const (
	SecurityRuleTargetURL    SecurityRuleTarget = "url"
	SecurityRuleTargetHeader SecurityRuleTarget = "header"
	SecurityRuleTargetBody   SecurityRuleTarget = "body"
	SecurityRuleTargetParam  SecurityRuleTarget = "param"
	SecurityRuleTargetCookie SecurityRuleTarget = "cookie"
	SecurityRuleTargetAll    SecurityRuleTarget = "all"
)

// SecurityRuleAction defines what action to take
type SecurityRuleAction string

const (
	SecurityRuleActionLog   SecurityRuleAction = "log"
	SecurityRuleActionBlock SecurityRuleAction = "block"
	SecurityRuleActionAlert SecurityRuleAction = "alert"
)

// SecurityScannerStats contains statistics about the security scanner
type SecurityScannerStats struct {
	RequestsScanned int64                           `json:"requests_scanned"`
	ThreatsDetected int64                           `json:"threats_detected"`
	RequestsBlocked int64                           `json:"requests_blocked"`
	ThreatsByLevel  map[SecurityThreatLevel]int64   `json:"threats_by_level"`
	ThreatsByType   map[string]int64                `json:"threats_by_type"`
	AverageScanTime time.Duration                   `json:"average_scan_time"`
	FalsePositives  int64                           `json:"false_positives"`
	TruePositives   int64                           `json:"true_positives"`
	MLAccuracy      float64                         `json:"ml_accuracy"`
	LastScan        time.Time                       `json:"last_scan"`
	TopThreats      []ThreatSummary                 `json:"top_threats"`
	Performance     SecurityScannerPerformanceStats `json:"performance"`
}

// ThreatSummary provides a summary of threat types
type ThreatSummary struct {
	Type     string              `json:"type"`
	Count    int64               `json:"count"`
	LastSeen time.Time           `json:"last_seen"`
	Severity SecurityThreatLevel `json:"severity"`
}

// SecurityScannerPerformanceStats tracks scanner performance
type SecurityScannerPerformanceStats struct {
	MinScanTime   time.Duration `json:"min_scan_time"`
	MaxScanTime   time.Duration `json:"max_scan_time"`
	TotalScanTime time.Duration `json:"total_scan_time"`
	CacheHits     int64         `json:"cache_hits"`
	CacheMisses   int64         `json:"cache_misses"`
	CPUUsage      float64       `json:"cpu_usage"`
	MemoryUsage   int64         `json:"memory_usage"`
}

// AISecurityScanner implements AI-powered security scanning middleware
type AISecurityScanner struct {
	config          SecurityScannerConfig
	agent           ai.AIAgent
	logger          forge.Logger
	metrics         forge.Metrics
	stats           SecurityScannerStats
	threatCache     map[string]*SecurityThreat
	rulePatterns    map[string]*regexp.Regexp
	behaviorTracker map[string]*BehaviorProfile
	mu              sync.RWMutex
	started         bool
}

// BehaviorProfile tracks behavioral patterns for IPs
type BehaviorProfile struct {
	IP              string            `json:"ip"`
	RequestCount    int64             `json:"request_count"`
	ThreatCount     int64             `json:"threat_count"`
	FirstSeen       time.Time         `json:"first_seen"`
	LastSeen        time.Time         `json:"last_seen"`
	RequestPatterns map[string]int64  `json:"request_patterns"`
	Anomalies       []BehaviorAnomaly `json:"anomalies"`
	RiskScore       float64           `json:"risk_score"`
	IsBot           bool              `json:"is_bot"`
}

// BehaviorAnomaly represents a behavioral anomaly
type BehaviorAnomaly struct {
	Type        string                 `json:"type"`
	Description string                 `json:"description"`
	Timestamp   time.Time              `json:"timestamp"`
	Confidence  float64                `json:"confidence"`
	Metadata    map[string]interface{} `json:"metadata"`
}

// NewAISecurityScanner creates a new AI security scanner middleware
func NewAISecurityScanner(config SecurityScannerConfig, agent ai.AIAgent, logger forge.Logger, metrics forge.Metrics) *AISecurityScanner {
	scanner := &AISecurityScanner{
		config:          config,
		agent:           agent,
		logger:          logger,
		metrics:         metrics,
		threatCache:     make(map[string]*SecurityThreat),
		rulePatterns:    make(map[string]*regexp.Regexp),
		behaviorTracker: make(map[string]*BehaviorProfile),
		stats: SecurityScannerStats{
			ThreatsByLevel: make(map[SecurityThreatLevel]int64),
			ThreatsByType:  make(map[string]int64),
			TopThreats:     make([]ThreatSummary, 0),
		},
	}

	// Compile rule patterns
	scanner.compileRulePatterns()

	return scanner
}

// Name returns the middleware name
func (s *AISecurityScanner) Name() string {
	return "ai-security-scanner"
}

// Type returns the middleware type
func (s *AISecurityScanner) Type() ai.AIMiddlewareType {
	return ai.AIMiddlewareTypeSecurity
}

// Initialize initializes the security scanner
func (s *AISecurityScanner) Initialize(ctx context.Context, config ai.AIMiddlewareConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.started {
		return fmt.Errorf("security scanner already initialized")
	}

	// Initialize the AI agent if provided
	if s.agent != nil {
		agentConfig := ai.AgentConfig{
			Logger:          s.logger,
			Metrics:         s.metrics,
			MaxConcurrency:  10,
			Timeout:         s.config.Timeout,
			LearningEnabled: true,
			AutoApply:       true,
			Metadata:        map[string]interface{}{"type": "security_scanner"},
		}

		if err := s.agent.Initialize(ctx, agentConfig); err != nil {
			return fmt.Errorf("failed to initialize security agent: %w", err)
		}

		if err := s.agent.Start(ctx); err != nil {
			return fmt.Errorf("failed to start security agent: %w", err)
		}
	}

	// Start background routines
	go s.performanceMonitoring(ctx)
	go s.threatAnalysis(ctx)
	go s.behaviorAnalysis(ctx)

	s.started = true

	if s.logger != nil {
		s.logger.Info("AI security scanner initialized",
			logger.Bool("enabled", s.config.Enabled),
			logger.String("block_threshold", string(s.config.BlockThreshold)),
			logger.Bool("ml_detection", s.config.EnableMLDetection),
		)
	}

	return nil
}

// Process processes HTTP requests through the security scanner
func (s *AISecurityScanner) Process(ctx context.Context, request *http.Request, response http.ResponseWriter, next http.HandlerFunc) error {
	if !s.config.Enabled {
		next.ServeHTTP(response, request)
		return nil
	}

	startTime := time.Now()

	// Update stats
	s.mu.Lock()
	s.stats.RequestsScanned++
	s.stats.LastScan = startTime
	s.mu.Unlock()

	// Perform security scan
	threats, err := s.scanRequest(ctx, request)
	scanDuration := time.Since(startTime)

	// Update performance stats
	s.updatePerformanceStats(scanDuration)

	if err != nil {
		if s.logger != nil {
			s.logger.Error("security scan failed",
				logger.String("error", err.Error()),
				logger.String("url", request.URL.String()),
			)
		}
		// Continue processing on scan failure
		next.ServeHTTP(response, request)
		return nil
	}

	// Process detected threats
	shouldBlock := s.processThreats(ctx, request, threats)

	if shouldBlock {
		s.mu.Lock()
		s.stats.RequestsBlocked++
		s.mu.Unlock()

		// Block the request
		s.blockRequest(response, request, threats)
		return nil
	}

	// Update behavior tracking
	s.updateBehaviorProfile(request, threats)

	// Record metrics
	if s.metrics != nil {
		s.metrics.Counter("forge.ai.security.requests_scanned").Inc()
		s.metrics.Histogram("forge.ai.security.scan_duration").Observe(scanDuration.Seconds())
		if len(threats) > 0 {
			s.metrics.Counter("forge.ai.security.threats_detected").Inc()
		}
	}

	// Continue with request processing
	next.ServeHTTP(response, request)
	return nil
}

// scanRequest performs comprehensive security scanning of the request
func (s *AISecurityScanner) scanRequest(ctx context.Context, request *http.Request) ([]*SecurityThreat, error) {
	var threats []*SecurityThreat

	// Quick IP-based checks first
	clientIP := s.getClientIP(request)
	if s.isBlacklisted(clientIP) {
		threats = append(threats, &SecurityThreat{
			ID:          s.generateThreatID(),
			Type:        "blacklisted_ip",
			Level:       SecurityThreatLevelHigh,
			Description: "Request from blacklisted IP address",
			Source:      clientIP,
			Confidence:  1.0,
			Timestamp:   time.Now(),
			Blocked:     true,
		})
		return threats, nil
	}

	if s.isWhitelisted(clientIP) {
		// Skip detailed scanning for whitelisted IPs
		return threats, nil
	}

	// Rule-based detection
	if s.config.EnableRuleBasedDetection {
		ruleThreats := s.performRuleBasedScanning(request)
		threats = append(threats, ruleThreats...)
	}

	// ML-based detection
	if s.config.EnableMLDetection && s.agent != nil {
		mlThreats, err := s.performMLDetection(ctx, request)
		if err != nil {
			if s.logger != nil {
				s.logger.Warn("ML detection failed", logger.Error(err))
			}
		} else {
			threats = append(threats, mlThreats...)
		}
	}

	// Behavioral analysis
	if s.config.EnableBehavioralAnalysis {
		behaviorThreats := s.performBehavioralAnalysis(request)
		threats = append(threats, behaviorThreats...)
	}

	return threats, nil
}

// performRuleBasedScanning performs rule-based security scanning
func (s *AISecurityScanner) performRuleBasedScanning(request *http.Request) []*SecurityThreat {
	var threats []*SecurityThreat

	// Common attack patterns
	patterns := map[string]SecurityThreatLevel{
		// SQL Injection
		`(?i)(union\s+select|drop\s+table|insert\s+into|delete\s+from|update\s+set)`: SecurityThreatLevelHigh,
		`(?i)(\'\s*or\s+\d+\s*=\s*\d+|admin\'\s*--|\'\s*or\s*\'\w+\'\s*=\s*\'\w+)`:   SecurityThreatLevelHigh,

		// XSS
		`(?i)(<script[^>]*>|javascript:|vbscript:|onload\s*=|onerror\s*=)`: SecurityThreatLevelMedium,
		`(?i)(alert\s*\(|document\.cookie|window\.location)`:               SecurityThreatLevelMedium,

		// Command Injection
		`(?i)(;.*cat\s+|;.*ls\s+|;.*pwd|;.*whoami|\|\s*nc\s+)`:              SecurityThreatLevelCritical,
		`(?i)(\$\(.*\)|` + "`" + `.*` + "`" + `|\|\s*curl\s+|\|\s*wget\s+)`: SecurityThreatLevelCritical,

		// Path Traversal
		`(\.\.\/|\.\.\\\|%2e%2e%2f|%2e%2e%5c)`: SecurityThreatLevelHigh,

		// LDAP Injection
		`(?i)(\*\)\(\w+=\*|\)\(|\(\w+=\*\))`: SecurityThreatLevelMedium,

		// XXE
		`(?i)(<!ENTITY|SYSTEM\s+["']|PUBLIC\s+["'])`: SecurityThreatLevelHigh,
	}

	// Scan URL and parameters
	if s.config.ScanParams {
		fullURL := request.URL.String()
		for pattern, level := range patterns {
			if matched, _ := regexp.MatchString(pattern, fullURL); matched {
				threats = append(threats, &SecurityThreat{
					ID:          s.generateThreatID(),
					Type:        s.identifyThreatType(pattern),
					Level:       level,
					Description: "Suspicious pattern detected in URL",
					Source:      s.getClientIP(request),
					Indicators:  []string{pattern},
					Confidence:  0.8,
					Timestamp:   time.Now(),
					Metadata:    map[string]interface{}{"url": fullURL},
				})
			}
		}
	}

	// Scan headers
	if s.config.ScanHeaders {
		for name, values := range request.Header {
			for _, value := range values {
				for pattern, level := range patterns {
					if matched, _ := regexp.MatchString(pattern, value); matched {
						threats = append(threats, &SecurityThreat{
							ID:          s.generateThreatID(),
							Type:        s.identifyThreatType(pattern),
							Level:       level,
							Description: fmt.Sprintf("Suspicious pattern detected in header %s", name),
							Source:      s.getClientIP(request),
							Indicators:  []string{pattern},
							Confidence:  0.8,
							Timestamp:   time.Now(),
							Metadata:    map[string]interface{}{"header": name, "value": value},
						})
					}
				}
			}
		}
	}

	// Custom rules
	for _, rule := range s.config.CustomRules {
		if !rule.Enabled {
			continue
		}

		if pattern, exists := s.rulePatterns[rule.ID]; exists {
			target := s.extractTargetContent(request, rule.Target)
			if pattern.MatchString(target) {
				threats = append(threats, &SecurityThreat{
					ID:          s.generateThreatID(),
					Type:        rule.Name,
					Level:       rule.Level,
					Description: rule.Description,
					Source:      s.getClientIP(request),
					Indicators:  []string{rule.Pattern},
					Confidence:  0.9,
					Timestamp:   time.Now(),
					Metadata:    map[string]interface{}{"rule_id": rule.ID, "target": string(rule.Target)},
				})
			}
		}
	}

	return threats
}

// performMLDetection performs ML-based threat detection
func (s *AISecurityScanner) performMLDetection(ctx context.Context, request *http.Request) ([]*SecurityThreat, error) {
	// Prepare input for AI agent
	input := ai.AgentInput{
		Type: "security_scan",
		Data: map[string]interface{}{
			"method":     request.Method,
			"url":        request.URL.String(),
			"headers":    request.Header,
			"user_agent": request.UserAgent(),
			"ip":         s.getClientIP(request),
			"timestamp":  time.Now(),
		},
		Context:   map[string]interface{}{"source": "security_scanner"},
		Timestamp: time.Now(),
		RequestID: s.generateThreatID(),
	}

	// Process through AI agent
	output, err := s.agent.Process(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("ML detection failed: %w", err)
	}

	// Parse AI output
	var threats []*SecurityThreat
	if output.Confidence > 0.5 {
		// Convert agent actions to security threats
		for _, action := range output.Actions {
			level := SecurityThreatLevelLow
			if action.Priority > 80 {
				level = SecurityThreatLevelCritical
			} else if action.Priority > 60 {
				level = SecurityThreatLevelHigh
			} else if action.Priority > 40 {
				level = SecurityThreatLevelMedium
			}

			threats = append(threats, &SecurityThreat{
				ID:          s.generateThreatID(),
				Type:        action.Type,
				Level:       level,
				Description: action.Description,
				Source:      s.getClientIP(request),
				Confidence:  output.Confidence,
				Timestamp:   time.Now(),
				Metadata: map[string]interface{}{
					"ai_explanation": output.Explanation,
					"action_id":      action.ID,
				},
			})
		}
	}

	return threats, nil
}

// performBehavioralAnalysis performs behavioral threat analysis
func (s *AISecurityScanner) performBehavioralAnalysis(request *http.Request) []*SecurityThreat {
	var threats []*SecurityThreat
	clientIP := s.getClientIP(request)

	s.mu.RLock()
	profile, exists := s.behaviorTracker[clientIP]
	s.mu.RUnlock()

	if !exists {
		return threats // No behavioral data yet
	}

	// Check for unusual request patterns
	if profile.RequestCount > 1000 && time.Since(profile.FirstSeen) < time.Hour {
		threats = append(threats, &SecurityThreat{
			ID:          s.generateThreatID(),
			Type:        "high_request_rate",
			Level:       SecurityThreatLevelMedium,
			Description: "Unusually high request rate detected",
			Source:      clientIP,
			Confidence:  0.7,
			Timestamp:   time.Now(),
			Metadata: map[string]interface{}{
				"request_count": profile.RequestCount,
				"time_window":   time.Since(profile.FirstSeen),
			},
		})
	}

	// Check threat-to-request ratio
	if profile.RequestCount > 100 && float64(profile.ThreatCount)/float64(profile.RequestCount) > 0.1 {
		threats = append(threats, &SecurityThreat{
			ID:          s.generateThreatID(),
			Type:        "persistent_threats",
			Level:       SecurityThreatLevelHigh,
			Description: "High threat-to-request ratio detected",
			Source:      clientIP,
			Confidence:  0.8,
			Timestamp:   time.Now(),
			Metadata: map[string]interface{}{
				"threat_ratio":  float64(profile.ThreatCount) / float64(profile.RequestCount),
				"total_threats": profile.ThreatCount,
			},
		})
	}

	// Check risk score
	if profile.RiskScore > 0.8 {
		threats = append(threats, &SecurityThreat{
			ID:          s.generateThreatID(),
			Type:        "high_risk_behavior",
			Level:       SecurityThreatLevelHigh,
			Description: "High risk behavioral pattern detected",
			Source:      clientIP,
			Confidence:  profile.RiskScore,
			Timestamp:   time.Now(),
			Metadata: map[string]interface{}{
				"risk_score": profile.RiskScore,
				"is_bot":     profile.IsBot,
			},
		})
	}

	return threats
}

// processThreats processes detected threats and determines if request should be blocked
func (s *AISecurityScanner) processThreats(ctx context.Context, request *http.Request, threats []*SecurityThreat) bool {
	if len(threats) == 0 {
		return false
	}

	s.mu.Lock()
	s.stats.ThreatsDetected += int64(len(threats))
	s.mu.Unlock()

	shouldBlock := false
	highestLevel := SecurityThreatLevelLow

	for _, threat := range threats {
		// Update stats
		s.mu.Lock()
		s.stats.ThreatsByLevel[threat.Level]++
		s.stats.ThreatsByType[threat.Type]++
		s.mu.Unlock()

		// Cache threat
		s.threatCache[threat.ID] = threat

		// Determine if we should block
		if s.shouldBlockThreat(threat) {
			shouldBlock = true
			threat.Blocked = true
			threat.Action = "blocked"
		} else {
			threat.Action = "logged"
		}

		// Track highest threat level
		if s.compareThreatLevels(threat.Level, highestLevel) > 0 {
			highestLevel = threat.Level
		}

		// Send alerts for high-level threats
		if threat.Level == SecurityThreatLevelHigh || threat.Level == SecurityThreatLevelCritical {
			go s.sendAlert(ctx, threat, request)
		}

		// Log threat
		if s.logger != nil {
			s.logger.Warn("security threat detected",
				logger.String("threat_id", threat.ID),
				logger.String("type", threat.Type),
				logger.String("level", string(threat.Level)),
				logger.String("source", threat.Source),
				logger.Float64("confidence", threat.Confidence),
				logger.Bool("blocked", threat.Blocked),
			)
		}
	}

	return shouldBlock
}

// shouldBlockThreat determines if a threat should result in blocking the request
func (s *AISecurityScanner) shouldBlockThreat(threat *SecurityThreat) bool {
	return s.compareThreatLevels(threat.Level, s.config.BlockThreshold) >= 0
}

// compareThreatLevels compares two threat levels (-1: a < b, 0: a == b, 1: a > b)
func (s *AISecurityScanner) compareThreatLevels(a, b SecurityThreatLevel) int {
	levels := map[SecurityThreatLevel]int{
		SecurityThreatLevelLow:      1,
		SecurityThreatLevelMedium:   2,
		SecurityThreatLevelHigh:     3,
		SecurityThreatLevelCritical: 4,
	}

	levelA := levels[a]
	levelB := levels[b]

	if levelA < levelB {
		return -1
	} else if levelA > levelB {
		return 1
	}
	return 0
}

// blockRequest blocks a request and sends appropriate response
func (s *AISecurityScanner) blockRequest(response http.ResponseWriter, request *http.Request, threats []*SecurityThreat) {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(http.StatusForbidden)

	errorResponse := map[string]interface{}{
		"error": "Security threat detected",
		"code":  "SECURITY_THREAT_DETECTED",
		"details": map[string]interface{}{
			"threats":    len(threats),
			"client_ip":  s.getClientIP(request),
			"timestamp":  time.Now().Format(time.RFC3339),
			"request_id": request.Header.Get("X-Request-ID"),
		},
	}

	json.NewEncoder(response).Encode(errorResponse)

	if s.logger != nil {
		s.logger.Warn("request blocked due to security threats",
			logger.String("client_ip", s.getClientIP(request)),
			logger.String("url", request.URL.String()),
			logger.Int("threat_count", len(threats)),
		)
	}
}

// Helper methods

func (s *AISecurityScanner) compileRulePatterns() {
	for _, rule := range s.config.CustomRules {
		if pattern, err := regexp.Compile(rule.Pattern); err == nil {
			s.rulePatterns[rule.ID] = pattern
		} else if s.logger != nil {
			s.logger.Error("failed to compile security rule pattern",
				logger.String("rule_id", rule.ID),
				logger.String("pattern", rule.Pattern),
				logger.Error(err),
			)
		}
	}
}

func (s *AISecurityScanner) getClientIP(request *http.Request) string {
	// Check X-Forwarded-For header
	if xff := request.Header.Get("X-Forwarded-For"); xff != "" {
		ips := strings.Split(xff, ",")
		if len(ips) > 0 {
			return strings.TrimSpace(ips[0])
		}
	}

	// Check X-Real-IP header
	if xri := request.Header.Get("X-Real-IP"); xri != "" {
		return strings.TrimSpace(xri)
	}

	// Fallback to RemoteAddr
	ip := request.RemoteAddr
	if colon := strings.LastIndex(ip, ":"); colon != -1 {
		ip = ip[:colon]
	}

	return ip
}

func (s *AISecurityScanner) isBlacklisted(ip string) bool {
	for _, blacklistedIP := range s.config.BlacklistIPs {
		if ip == blacklistedIP {
			return true
		}
	}
	return false
}

func (s *AISecurityScanner) isWhitelisted(ip string) bool {
	for _, whitelistedIP := range s.config.WhitelistIPs {
		if ip == whitelistedIP {
			return true
		}
	}
	return false
}

func (s *AISecurityScanner) identifyThreatType(pattern string) string {
	typeMap := map[string]string{
		`(?i)(union\s+select|drop\s+table)`: "sql_injection",
		`(?i)(<script[^>]*>|javascript:)`:   "xss",
		`(?i)(;.*cat\s+|;.*ls\s+)`:          "command_injection",
		`(\.\.\/|\.\.\\\)`:                  "path_traversal",
		`(?i)(\*\)\(\w+=\*)`:                "ldap_injection",
		`(?i)(<!ENTITY|SYSTEM\s+["'])`:      "xxe",
	}

	for patternRegex, threatType := range typeMap {
		if matched, _ := regexp.MatchString(patternRegex, pattern); matched {
			return threatType
		}
	}

	return "unknown_threat"
}

func (s *AISecurityScanner) extractTargetContent(request *http.Request, target SecurityRuleTarget) string {
	switch target {
	case SecurityRuleTargetURL:
		return request.URL.String()
	case SecurityRuleTargetHeader:
		var headers []string
		for name, values := range request.Header {
			headers = append(headers, fmt.Sprintf("%s: %s", name, strings.Join(values, ", ")))
		}
		return strings.Join(headers, "\n")
	case SecurityRuleTargetParam:
		return request.URL.RawQuery
	case SecurityRuleTargetCookie:
		return request.Header.Get("Cookie")
	default:
		return request.URL.String()
	}
}

func (s *AISecurityScanner) updateBehaviorProfile(request *http.Request, threats []*SecurityThreat) {
	clientIP := s.getClientIP(request)

	s.mu.Lock()
	defer s.mu.Unlock()

	profile, exists := s.behaviorTracker[clientIP]
	if !exists {
		profile = &BehaviorProfile{
			IP:              clientIP,
			FirstSeen:       time.Now(),
			RequestPatterns: make(map[string]int64),
			Anomalies:       make([]BehaviorAnomaly, 0),
		}
		s.behaviorTracker[clientIP] = profile
	}

	profile.RequestCount++
	profile.LastSeen = time.Now()
	profile.ThreatCount += int64(len(threats))

	// Update request patterns
	pattern := fmt.Sprintf("%s %s", request.Method, request.URL.Path)
	profile.RequestPatterns[pattern]++

	// Calculate risk score
	profile.RiskScore = s.calculateRiskScore(profile)

	// Bot detection (simple heuristic)
	userAgent := request.UserAgent()
	profile.IsBot = strings.Contains(strings.ToLower(userAgent), "bot") ||
		strings.Contains(strings.ToLower(userAgent), "crawler") ||
		strings.Contains(strings.ToLower(userAgent), "spider")
}

func (s *AISecurityScanner) calculateRiskScore(profile *BehaviorProfile) float64 {
	score := 0.0

	// High request rate increases risk
	if profile.RequestCount > 1000 {
		score += 0.3
	}

	// High threat ratio increases risk
	if profile.RequestCount > 0 {
		threatRatio := float64(profile.ThreatCount) / float64(profile.RequestCount)
		score += threatRatio * 0.5
	}

	// Bot behavior increases risk
	if profile.IsBot {
		score += 0.2
	}

	// Normalize score to 0-1 range
	if score > 1.0 {
		score = 1.0
	}

	return score
}

func (s *AISecurityScanner) generateThreatID() string {
	return fmt.Sprintf("threat-%d", time.Now().UnixNano())
}

func (s *AISecurityScanner) sendAlert(ctx context.Context, threat *SecurityThreat, request *http.Request) {
	if s.config.AlertWebhook == "" {
		return
	}

	alertData := map[string]interface{}{
		"threat": threat,
		"request": map[string]interface{}{
			"method":     request.Method,
			"url":        request.URL.String(),
			"user_agent": request.UserAgent(),
			"ip":         s.getClientIP(request),
		},
		"timestamp": time.Now().Format(time.RFC3339),
	}

	// Send webhook alert (simplified implementation)
	go func() {
		if _, err := json.Marshal(alertData); err == nil {
			// Implementation would send HTTP POST to webhook URL
			// http.Post(s.config.AlertWebhook, "application/json", bytes.NewBuffer(data))
			if s.logger != nil {
				s.logger.Info("security alert sent",
					logger.String("webhook", s.config.AlertWebhook),
					logger.String("threat_id", threat.ID),
				)
			}
		}
	}()
}

func (s *AISecurityScanner) updatePerformanceStats(duration time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()

	perf := &s.stats.Performance

	// Update min/max
	if perf.MinScanTime == 0 || duration < perf.MinScanTime {
		perf.MinScanTime = duration
	}
	if duration > perf.MaxScanTime {
		perf.MaxScanTime = duration
	}

	// Update totals
	perf.TotalScanTime += duration

	// Calculate average
	if s.stats.RequestsScanned > 0 {
		s.stats.AverageScanTime = perf.TotalScanTime / time.Duration(s.stats.RequestsScanned)
	}
}

// Background routines

func (s *AISecurityScanner) performanceMonitoring(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.collectPerformanceMetrics()
		}
	}
}

func (s *AISecurityScanner) threatAnalysis(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.analyzeThreatTrends()
		}
	}
}

func (s *AISecurityScanner) behaviorAnalysis(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.analyzeBehaviorPatterns()
		}
	}
}

func (s *AISecurityScanner) collectPerformanceMetrics() {
	// Implementation would collect system metrics
	if s.metrics != nil {
		s.mu.RLock()
		s.metrics.Gauge("forge.ai.security.requests_scanned").Set(float64(s.stats.RequestsScanned))
		s.metrics.Gauge("forge.ai.security.threats_detected").Set(float64(s.stats.ThreatsDetected))
		s.metrics.Gauge("forge.ai.security.requests_blocked").Set(float64(s.stats.RequestsBlocked))
		s.metrics.Histogram("forge.ai.security.average_scan_time").Observe(s.stats.AverageScanTime.Seconds())
		s.mu.RUnlock()
	}
}

func (s *AISecurityScanner) analyzeThreatTrends() {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Analyze threat patterns and update AI agent with feedback
	// This could include feeding back threat detection accuracy
	// and adjusting ML model parameters
}

func (s *AISecurityScanner) analyzeBehaviorPatterns() {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Clean up old behavior profiles
	cutoff := time.Now().Add(-24 * time.Hour)
	for ip, profile := range s.behaviorTracker {
		if profile.LastSeen.Before(cutoff) {
			delete(s.behaviorTracker, ip)
		}
	}

	// Analyze patterns for anomalies
	// This could include identifying new bot patterns,
	// unusual geographic distributions, etc.
}

// GetStats returns the current security scanner statistics
func (s *AISecurityScanner) GetStats() ai.AIMiddlewareStats {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return ai.AIMiddlewareStats{
		Name:              s.Name(),
		Type:              string(s.Type()),
		RequestsProcessed: s.stats.RequestsScanned,
		AverageLatency:    s.stats.AverageScanTime,
		ErrorRate:         0.0, // Security scanner doesn't have traditional "errors"
		Metadata: map[string]interface{}{
			"threats_detected":  s.stats.ThreatsDetected,
			"requests_blocked":  s.stats.RequestsBlocked,
			"threats_by_level":  s.stats.ThreatsByLevel,
			"threats_by_type":   s.stats.ThreatsByType,
			"behavior_profiles": len(s.behaviorTracker),
		},
		LastUpdated: time.Now(),
	}
}
