package validation

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/xraph/forge/v0/pkg/common"
	"github.com/xraph/forge/v0/pkg/logger"
	"github.com/xraph/forge/v0/pkg/middleware"
)

// Config contains configuration for request validation
type Config struct {
	// General settings
	MaxBodySize       int64    `yaml:"max_body_size" json:"max_body_size" default:"1048576"` // 1MB
	AllowedMethods    []string `yaml:"allowed_methods" json:"allowed_methods"`
	RequiredHeaders   []string `yaml:"required_headers" json:"required_headers"`
	ForbiddenHeaders  []string `yaml:"forbidden_headers" json:"forbidden_headers"`
	AllowedUserAgents []string `yaml:"allowed_user_agents" json:"allowed_user_agents"`
	SkipPaths         []string `yaml:"skip_paths" json:"skip_paths"`

	// Content validation
	AllowedContentTypes []string `yaml:"allowed_content_types" json:"allowed_content_types"`
	RequireContentType  bool     `yaml:"require_content_type" json:"require_content_type"`
	ValidateJSON        bool     `yaml:"validate_json" json:"validate_json" default:"true"`
	ValidateXML         bool     `yaml:"validate_xml" json:"validate_xml" default:"true"`

	// Security settings
	BlockSuspiciousRequests bool     `yaml:"block_suspicious_requests" json:"block_suspicious_requests" default:"true"`
	SuspiciousPatterns      []string `yaml:"suspicious_patterns" json:"suspicious_patterns"`
	MaxHeaderSize           int      `yaml:"max_header_size" json:"max_header_size" default:"8192"`
	MaxQueryParams          int      `yaml:"max_query_params" json:"max_query_params" default:"100"`

	// Rate limiting for validation errors
	ErrorRateLimit int           `yaml:"error_rate_limit" json:"error_rate_limit" default:"10"`
	ErrorWindow    time.Duration `yaml:"error_window" json:"error_window" default:"1m"`

	// Response configuration
	IncludeDetails bool   `yaml:"include_details" json:"include_details" default:"true"`
	ErrorMessage   string `yaml:"error_message" json:"error_message" default:"Invalid request"`
	ErrorCode      string `yaml:"error_code" json:"error_code" default:"VALIDATION_ERROR"`
	LogFailures    bool   `yaml:"log_failures" json:"log_failures" default:"true"`
	LogSuccesses   bool   `yaml:"log_successes" json:"log_successes" default:"false"`
}

// ValidationMiddleware implements request validation middleware
type ValidationMiddleware struct {
	*middleware.BaseServiceMiddleware
	config      Config
	regexCache  map[string]*regexp.Regexp
	errorCounts map[string]*errorCounter
}

// ValidationError represents a validation error
type ValidationError struct {
	Field   string      `json:"field"`
	Message string      `json:"message"`
	Value   interface{} `json:"value,omitempty"`
	Code    string      `json:"code"`
}

// ValidationResult contains the result of validation
type ValidationResult struct {
	Valid  bool              `json:"valid"`
	Errors []ValidationError `json:"errors"`
}

// errorCounter tracks validation errors per IP
type errorCounter struct {
	count     int
	firstSeen time.Time
	lastSeen  time.Time
}

// Validator defines the interface for validation rules
type Validator interface {
	Validate(value interface{}) error
	Name() string
}

// NewValidationMiddleware creates a new validation middleware
func NewValidationMiddleware(config Config) *ValidationMiddleware {
	return &ValidationMiddleware{
		BaseServiceMiddleware: middleware.NewBaseServiceMiddleware("validation", 20, []string{"config-manager"}),
		config:                config,
		regexCache:            make(map[string]*regexp.Regexp),
		errorCounts:           make(map[string]*errorCounter),
	}
}

// Initialize initializes the validation middleware
func (vm *ValidationMiddleware) Initialize(container common.Container) error {
	if err := vm.BaseServiceMiddleware.Initialize(container); err != nil {
		return err
	}

	// Load configuration from container if needed
	if configManager, err := container.Resolve((*common.ConfigManager)(nil)); err == nil {
		var validationConfig Config
		if err := configManager.(common.ConfigManager).Bind("middleware.validation", &validationConfig); err == nil {
			vm.config = validationConfig
		}
	}

	// Set defaults
	if vm.config.MaxBodySize == 0 {
		vm.config.MaxBodySize = 1048576 // 1MB
	}
	if vm.config.MaxHeaderSize == 0 {
		vm.config.MaxHeaderSize = 8192
	}
	if vm.config.MaxQueryParams == 0 {
		vm.config.MaxQueryParams = 100
	}
	if vm.config.ErrorRateLimit == 0 {
		vm.config.ErrorRateLimit = 10
	}
	if vm.config.ErrorWindow == 0 {
		vm.config.ErrorWindow = time.Minute
	}
	if vm.config.ErrorMessage == "" {
		vm.config.ErrorMessage = "Invalid request"
	}
	if vm.config.ErrorCode == "" {
		vm.config.ErrorCode = "VALIDATION_ERROR"
	}

	// Initialize default suspicious patterns if not configured
	if len(vm.config.SuspiciousPatterns) == 0 {
		vm.config.SuspiciousPatterns = []string{
			`<script.*?>.*?</script>`, // XSS
			`javascript:`,             // JavaScript injection
			`on\w+\s*=`,               // Event handlers
			`<iframe.*?>`,             // Iframe injection
			`union.*select`,           // SQL injection
			`drop\s+table`,            // SQL injection
			`exec\s*\(`,               // Command injection
			`\.\.\/`,                  // Path traversal
			`\/etc\/passwd`,           // System file access
		}
	}

	// Precompile regex patterns
	for _, pattern := range vm.config.SuspiciousPatterns {
		if _, err := vm.getCompiledRegex(pattern); err != nil {
			return fmt.Errorf("failed to compile suspicious pattern '%s': %w", pattern, err)
		}
	}

	return nil
}

// Handler returns the validation handler
func (vm *ValidationMiddleware) Handler() func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			// Update call count
			vm.UpdateStats(1, 0, 0, nil)

			// Check if path should be skipped
			if vm.shouldSkipPath(r.URL.Path) {
				next.ServeHTTP(w, r)
				vm.UpdateStats(0, 0, time.Since(start), nil)
				return
			}

			// Check error rate limiting
			clientIP := vm.getClientIP(r)
			if vm.isErrorRateLimited(clientIP) {
				vm.UpdateStats(0, 1, time.Since(start), fmt.Errorf("error rate limit exceeded"))
				vm.writeValidationError(w, r, []ValidationError{
					{
						Field:   "rate_limit",
						Message: "Too many validation errors",
						Code:    "RATE_LIMIT_EXCEEDED",
					},
				})
				return
			}

			// Perform validation
			result := vm.validateRequest(r)
			if !result.Valid {
				vm.recordValidationError(clientIP)
				vm.UpdateStats(0, 1, time.Since(start), fmt.Errorf("validation failed: %d errors", len(result.Errors)))
				vm.writeValidationError(w, r, result.Errors)
				return
			}

			// Continue to next handler
			next.ServeHTTP(w, r)

			// Update latency statistics
			vm.UpdateStats(0, 0, time.Since(start), nil)

			// Log successful validation if configured
			if vm.config.LogSuccesses && vm.Logger() != nil {
				vm.Logger().Debug("request validation successful",
					logger.String("path", r.URL.Path),
					logger.String("method", r.Method),
					logger.String("client_ip", clientIP),
				)
			}
		})
	}
}

// validateRequest performs comprehensive request validation
func (vm *ValidationMiddleware) validateRequest(r *http.Request) ValidationResult {
	var errors []ValidationError

	// Validate HTTP method
	if methodErrors := vm.validateMethod(r); len(methodErrors) > 0 {
		errors = append(errors, methodErrors...)
	}

	// Validate headers
	if headerErrors := vm.validateHeaders(r); len(headerErrors) > 0 {
		errors = append(errors, headerErrors...)
	}

	// Validate query parameters
	if queryErrors := vm.validateQueryParams(r); len(queryErrors) > 0 {
		errors = append(errors, queryErrors...)
	}

	// Validate body size
	if bodyErrors := vm.validateBodySize(r); len(bodyErrors) > 0 {
		errors = append(errors, bodyErrors...)
	}

	// Validate content type
	if contentErrors := vm.validateContentType(r); len(contentErrors) > 0 {
		errors = append(errors, contentErrors...)
	}

	// Validate body content
	if bodyContentErrors := vm.validateBodyContent(r); len(bodyContentErrors) > 0 {
		errors = append(errors, bodyContentErrors...)
	}

	// Check for suspicious content
	if vm.config.BlockSuspiciousRequests {
		if suspiciousErrors := vm.validateSuspiciousContent(r); len(suspiciousErrors) > 0 {
			errors = append(errors, suspiciousErrors...)
		}
	}

	// Validate user agent
	if uaErrors := vm.validateUserAgent(r); len(uaErrors) > 0 {
		errors = append(errors, uaErrors...)
	}

	return ValidationResult{
		Valid:  len(errors) == 0,
		Errors: errors,
	}
}

// validateMethod validates the HTTP method
func (vm *ValidationMiddleware) validateMethod(r *http.Request) []ValidationError {
	if len(vm.config.AllowedMethods) == 0 {
		return nil
	}

	for _, allowed := range vm.config.AllowedMethods {
		if strings.EqualFold(r.Method, allowed) {
			return nil
		}
	}

	return []ValidationError{
		{
			Field:   "method",
			Message: fmt.Sprintf("HTTP method '%s' is not allowed", r.Method),
			Value:   r.Method,
			Code:    "METHOD_NOT_ALLOWED",
		},
	}
}

// validateHeaders validates request headers
func (vm *ValidationMiddleware) validateHeaders(r *http.Request) []ValidationError {
	var errors []ValidationError

	// Check required headers
	for _, required := range vm.config.RequiredHeaders {
		if r.Header.Get(required) == "" {
			errors = append(errors, ValidationError{
				Field:   "headers." + required,
				Message: fmt.Sprintf("Required header '%s' is missing", required),
				Code:    "MISSING_REQUIRED_HEADER",
			})
		}
	}

	// Check forbidden headers
	for _, forbidden := range vm.config.ForbiddenHeaders {
		if r.Header.Get(forbidden) != "" {
			errors = append(errors, ValidationError{
				Field:   "headers." + forbidden,
				Message: fmt.Sprintf("Forbidden header '%s' is present", forbidden),
				Code:    "FORBIDDEN_HEADER_PRESENT",
			})
		}
	}

	// Check header sizes
	for name, values := range r.Header {
		headerSize := len(name)
		for _, value := range values {
			headerSize += len(value)
		}
		if headerSize > vm.config.MaxHeaderSize {
			errors = append(errors, ValidationError{
				Field:   "headers." + name,
				Message: fmt.Sprintf("Header '%s' is too large (%d bytes)", name, headerSize),
				Code:    "HEADER_TOO_LARGE",
			})
		}
	}

	return errors
}

// validateQueryParams validates query parameters
func (vm *ValidationMiddleware) validateQueryParams(r *http.Request) []ValidationError {
	var errors []ValidationError

	query := r.URL.Query()

	// Check number of query parameters
	if len(query) > vm.config.MaxQueryParams {
		errors = append(errors, ValidationError{
			Field:   "query_params",
			Message: fmt.Sprintf("Too many query parameters (%d), maximum allowed is %d", len(query), vm.config.MaxQueryParams),
			Code:    "TOO_MANY_QUERY_PARAMS",
		})
	}

	// Validate each query parameter for suspicious content
	if vm.config.BlockSuspiciousRequests {
		for key, values := range query {
			for _, value := range values {
				if vm.containsSuspiciousContent(value) {
					errors = append(errors, ValidationError{
						Field:   "query_params." + key,
						Message: "Query parameter contains suspicious content",
						Value:   value,
						Code:    "SUSPICIOUS_QUERY_PARAM",
					})
				}
			}
		}
	}

	return errors
}

// validateBodySize validates request body size
func (vm *ValidationMiddleware) validateBodySize(r *http.Request) []ValidationError {
	if r.ContentLength > vm.config.MaxBodySize {
		return []ValidationError{
			{
				Field:   "body",
				Message: fmt.Sprintf("Request body too large (%d bytes), maximum allowed is %d bytes", r.ContentLength, vm.config.MaxBodySize),
				Value:   r.ContentLength,
				Code:    "BODY_TOO_LARGE",
			},
		}
	}

	return nil
}

// validateContentType validates content type
func (vm *ValidationMiddleware) validateContentType(r *http.Request) []ValidationError {
	var errors []ValidationError

	contentType := r.Header.Get("Content-Type")

	// Check if content type is required
	if vm.config.RequireContentType && contentType == "" && r.ContentLength > 0 {
		errors = append(errors, ValidationError{
			Field:   "content_type",
			Message: "Content-Type header is required for requests with body",
			Code:    "MISSING_CONTENT_TYPE",
		})
	}

	// Check allowed content types
	if len(vm.config.AllowedContentTypes) > 0 && contentType != "" {
		allowed := false
		for _, allowedType := range vm.config.AllowedContentTypes {
			if strings.HasPrefix(contentType, allowedType) {
				allowed = true
				break
			}
		}
		if !allowed {
			errors = append(errors, ValidationError{
				Field:   "content_type",
				Message: fmt.Sprintf("Content-Type '%s' is not allowed", contentType),
				Value:   contentType,
				Code:    "CONTENT_TYPE_NOT_ALLOWED",
			})
		}
	}

	return errors
}

// validateBodyContent validates request body content
func (vm *ValidationMiddleware) validateBodyContent(r *http.Request) []ValidationError {
	var errors []ValidationError

	if r.ContentLength == 0 {
		return errors
	}

	contentType := r.Header.Get("Content-Type")

	// Validate JSON content
	if vm.config.ValidateJSON && strings.HasPrefix(contentType, "application/json") {
		if jsonErrors := vm.validateJSON(r); len(jsonErrors) > 0 {
			errors = append(errors, jsonErrors...)
		}
	}

	// Validate XML content
	if vm.config.ValidateXML && (strings.HasPrefix(contentType, "application/xml") || strings.HasPrefix(contentType, "text/xml")) {
		if xmlErrors := vm.validateXML(r); len(xmlErrors) > 0 {
			errors = append(errors, xmlErrors...)
		}
	}

	return errors
}

// validateJSON validates JSON content
func (vm *ValidationMiddleware) validateJSON(r *http.Request) []ValidationError {
	body, err := io.ReadAll(io.LimitReader(r.Body, vm.config.MaxBodySize))
	if err != nil {
		return []ValidationError{
			{
				Field:   "body",
				Message: "Failed to read request body",
				Code:    "BODY_READ_ERROR",
			},
		}
	}

	// Restore body for subsequent handlers
	r.Body = io.NopCloser(strings.NewReader(string(body)))

	var jsonData interface{}
	if err := json.Unmarshal(body, &jsonData); err != nil {
		return []ValidationError{
			{
				Field:   "body",
				Message: "Invalid JSON format",
				Code:    "INVALID_JSON",
			},
		}
	}

	// Check for suspicious content in JSON
	if vm.config.BlockSuspiciousRequests {
		if vm.containsSuspiciousContentInJSON(jsonData) {
			return []ValidationError{
				{
					Field:   "body",
					Message: "JSON contains suspicious content",
					Code:    "SUSPICIOUS_JSON_CONTENT",
				},
			}
		}
	}

	return nil
}

// validateXML validates XML content (simplified validation)
func (vm *ValidationMiddleware) validateXML(r *http.Request) []ValidationError {
	body, err := io.ReadAll(io.LimitReader(r.Body, vm.config.MaxBodySize))
	if err != nil {
		return []ValidationError{
			{
				Field:   "body",
				Message: "Failed to read request body",
				Code:    "BODY_READ_ERROR",
			},
		}
	}

	// Restore body for subsequent handlers
	r.Body = io.NopCloser(strings.NewReader(string(body)))

	// Basic XML validation - check for balanced tags
	bodyStr := string(body)
	if vm.containsSuspiciousContent(bodyStr) {
		return []ValidationError{
			{
				Field:   "body",
				Message: "XML contains suspicious content",
				Code:    "SUSPICIOUS_XML_CONTENT",
			},
		}
	}

	return nil
}

// validateSuspiciousContent checks for suspicious patterns in the request
func (vm *ValidationMiddleware) validateSuspiciousContent(r *http.Request) []ValidationError {
	var errors []ValidationError

	// Check URL path
	if vm.containsSuspiciousContent(r.URL.Path) {
		errors = append(errors, ValidationError{
			Field:   "path",
			Message: "Request path contains suspicious content",
			Value:   r.URL.Path,
			Code:    "SUSPICIOUS_PATH",
		})
	}

	// Check headers
	for name, values := range r.Header {
		for _, value := range values {
			if vm.containsSuspiciousContent(value) {
				errors = append(errors, ValidationError{
					Field:   "headers." + name,
					Message: "Header contains suspicious content",
					Value:   value,
					Code:    "SUSPICIOUS_HEADER",
				})
			}
		}
	}

	return errors
}

// validateUserAgent validates user agent
func (vm *ValidationMiddleware) validateUserAgent(r *http.Request) []ValidationError {
	if len(vm.config.AllowedUserAgents) == 0 {
		return nil
	}

	userAgent := r.UserAgent()
	for _, allowed := range vm.config.AllowedUserAgents {
		if strings.Contains(userAgent, allowed) {
			return nil
		}
	}

	return []ValidationError{
		{
			Field:   "user_agent",
			Message: "User agent is not allowed",
			Value:   userAgent,
			Code:    "USER_AGENT_NOT_ALLOWED",
		},
	}
}

// containsSuspiciousContent checks if content contains suspicious patterns
func (vm *ValidationMiddleware) containsSuspiciousContent(content string) bool {
	content = strings.ToLower(content)

	for _, pattern := range vm.config.SuspiciousPatterns {
		regex, err := vm.getCompiledRegex(pattern)
		if err != nil {
			continue
		}
		if regex.MatchString(content) {
			return true
		}
	}

	return false
}

// containsSuspiciousContentInJSON recursively checks JSON for suspicious content
func (vm *ValidationMiddleware) containsSuspiciousContentInJSON(data interface{}) bool {
	switch v := data.(type) {
	case string:
		return vm.containsSuspiciousContent(v)
	case map[string]interface{}:
		for key, value := range v {
			if vm.containsSuspiciousContent(key) || vm.containsSuspiciousContentInJSON(value) {
				return true
			}
		}
	case []interface{}:
		for _, item := range v {
			if vm.containsSuspiciousContentInJSON(item) {
				return true
			}
		}
	}
	return false
}

// getCompiledRegex returns a compiled regex, using cache
func (vm *ValidationMiddleware) getCompiledRegex(pattern string) (*regexp.Regexp, error) {
	if regex, exists := vm.regexCache[pattern]; exists {
		return regex, nil
	}

	regex, err := regexp.Compile(`(?i)` + pattern)
	if err != nil {
		return nil, err
	}

	vm.regexCache[pattern] = regex
	return regex, nil
}

// shouldSkipPath checks if the current path should skip validation
func (vm *ValidationMiddleware) shouldSkipPath(path string) bool {
	for _, skipPath := range vm.config.SkipPaths {
		if strings.HasPrefix(path, skipPath) {
			return true
		}
	}
	return false
}

// getClientIP extracts client IP from request
func (vm *ValidationMiddleware) getClientIP(r *http.Request) string {
	// Check X-Forwarded-For header
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		ips := strings.Split(xff, ",")
		if len(ips) > 0 {
			return strings.TrimSpace(ips[0])
		}
	}

	// Check X-Real-IP header
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return strings.TrimSpace(xri)
	}

	// Use remote address
	if ip := r.RemoteAddr; ip != "" {
		// Remove port if present
		if colon := strings.LastIndex(ip, ":"); colon != -1 {
			return ip[:colon]
		}
		return ip
	}

	return "unknown"
}

// isErrorRateLimited checks if client has exceeded error rate limit
func (vm *ValidationMiddleware) isErrorRateLimited(clientIP string) bool {
	counter, exists := vm.errorCounts[clientIP]
	if !exists {
		return false
	}

	now := time.Now()

	// Clean up old entries
	if now.Sub(counter.firstSeen) > vm.config.ErrorWindow {
		delete(vm.errorCounts, clientIP)
		return false
	}

	return counter.count >= vm.config.ErrorRateLimit
}

// recordValidationError records a validation error for rate limiting
func (vm *ValidationMiddleware) recordValidationError(clientIP string) {
	now := time.Now()

	counter, exists := vm.errorCounts[clientIP]
	if !exists {
		vm.errorCounts[clientIP] = &errorCounter{
			count:     1,
			firstSeen: now,
			lastSeen:  now,
		}
		return
	}

	// Reset counter if window has passed
	if now.Sub(counter.firstSeen) > vm.config.ErrorWindow {
		counter.count = 1
		counter.firstSeen = now
	} else {
		counter.count++
	}

	counter.lastSeen = now
}

// writeValidationError writes a validation error response
func (vm *ValidationMiddleware) writeValidationError(w http.ResponseWriter, r *http.Request, errors []ValidationError) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)

	response := map[string]interface{}{
		"error": map[string]interface{}{
			"code":    vm.config.ErrorCode,
			"message": vm.config.ErrorMessage,
		},
	}

	if vm.config.IncludeDetails {
		response["error"].(map[string]interface{})["details"] = errors
		response["error"].(map[string]interface{})["validation_failed"] = true
		response["error"].(map[string]interface{})["error_count"] = len(errors)
	}

	json.NewEncoder(w).Encode(response)

	// Log validation failure if configured
	if vm.config.LogFailures && vm.Logger() != nil {
		vm.Logger().Warn("request validation failed",
			logger.String("path", r.URL.Path),
			logger.String("method", r.Method),
			logger.String("client_ip", vm.getClientIP(r)),
			logger.Int("error_count", len(errors)),
			logger.Any("errors", errors),
		)
	}
}
