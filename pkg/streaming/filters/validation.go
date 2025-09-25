package filters

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/logger"
	streaming "github.com/xraph/forge/pkg/streaming/core"
)

// ValidationLevel represents the level of validation to apply
type ValidationLevel string

const (
	ValidationLevelNone   ValidationLevel = "none"
	ValidationLevelBasic  ValidationLevel = "basic"
	ValidationLevelStrict ValidationLevel = "strict"
	ValidationLevelCustom ValidationLevel = "custom"
)

// ValidationRule represents a validation rule for messages
type ValidationRule interface {
	Name() string
	Validate(ctx context.Context, message *streaming.Message, filterContext *FilterContext) *ValidationResult
	IsEnabled() bool
	SetEnabled(enabled bool)
	GetConfig() map[string]interface{}
	UpdateConfig(config map[string]interface{}) error
}

// ValidationResult represents the result of message validation
type ValidationResult struct {
	Valid         bool                   `json:"valid"`
	Errors        []ValidationError      `json:"errors,omitempty"`
	Warnings      []ValidationWarning    `json:"warnings,omitempty"`
	Metadata      map[string]interface{} `json:"metadata,omitempty"`
	ProcessedAt   time.Time              `json:"processed_at"`
	RuleApplied   string                 `json:"rule_applied"`
	SeverityLevel ValidationSeverity     `json:"severity_level"`
}

// ValidationError represents a validation error
type ValidationError struct {
	Field       string             `json:"field"`
	Code        string             `json:"code"`
	Message     string             `json:"message"`
	Value       interface{}        `json:"value,omitempty"`
	Constraint  string             `json:"constraint,omitempty"`
	Severity    ValidationSeverity `json:"severity"`
	Suggestions []string           `json:"suggestions,omitempty"`
}

// ValidationWarning represents a validation warning
type ValidationWarning struct {
	Field   string      `json:"field"`
	Code    string      `json:"code"`
	Message string      `json:"message"`
	Value   interface{} `json:"value,omitempty"`
}

// ValidationSeverity represents the severity of validation issues
type ValidationSeverity string

const (
	ValidationSeverityInfo     ValidationSeverity = "info"
	ValidationSeverityWarning  ValidationSeverity = "warning"
	ValidationSeverityError    ValidationSeverity = "error"
	ValidationSeverityCritical ValidationSeverity = "critical"
)

// ValidationConfig contains configuration for message validation
type ValidationConfig struct {
	Level                  ValidationLevel `yaml:"level" default:"basic"`
	MaxMessageSize         int             `yaml:"max_message_size" default:"65536"`
	MaxMetadataSize        int             `yaml:"max_metadata_size" default:"8192"`
	RequiredFields         []string        `yaml:"required_fields"`
	AllowedMessageTypes    []string        `yaml:"allowed_message_types"`
	ForbiddenPatterns      []string        `yaml:"forbidden_patterns"`
	EnableContentScan      bool            `yaml:"enable_content_scan" default:"true"`
	EnableSchemaValidation bool            `yaml:"enable_schema_validation" default:"false"`
	StrictTypeValidation   bool            `yaml:"strict_type_validation" default:"false"`
	MaxFieldDepth          int             `yaml:"max_field_depth" default:"10"`
	MaxArrayLength         int             `yaml:"max_array_length" default:"1000"`
	Enabled                bool            `yaml:"enabled" default:"true"`
}

// DefaultValidationConfig returns default validation configuration
func DefaultValidationConfig() ValidationConfig {
	return ValidationConfig{
		Level:                  ValidationLevelBasic,
		MaxMessageSize:         65536, // 64KB
		MaxMetadataSize:        8192,  // 8KB
		RequiredFields:         []string{"id", "type", "timestamp"},
		AllowedMessageTypes:    []string{"text", "event", "presence", "system", "broadcast", "private", "typing"},
		ForbiddenPatterns:      []string{},
		EnableContentScan:      true,
		EnableSchemaValidation: false,
		StrictTypeValidation:   false,
		MaxFieldDepth:          10,
		MaxArrayLength:         1000,
		Enabled:                true,
	}
}

// MessageValidator implements comprehensive message validation
type MessageValidator struct {
	config         ValidationConfig
	rules          map[string]ValidationRule
	schemas        map[string]*JSONSchema
	forbiddenRegex []*regexp.Regexp
	stats          *ValidationStats
	mu             sync.RWMutex
	logger         common.Logger
	metrics        common.Metrics
}

// ValidationStats represents validation statistics
type ValidationStats struct {
	TotalValidations   int64                          `json:"total_validations"`
	ValidMessages      int64                          `json:"valid_messages"`
	InvalidMessages    int64                          `json:"invalid_messages"`
	ValidationErrors   map[string]int64               `json:"validation_errors"`
	ValidationWarnings map[string]int64               `json:"validation_warnings"`
	RuleExecutions     map[string]*RuleExecutionStats `json:"rule_executions"`
	AverageLatency     time.Duration                  `json:"average_latency"`
	LastValidation     time.Time                      `json:"last_validation"`
}

// RuleExecutionStats represents statistics for a specific rule
type RuleExecutionStats struct {
	Executions     int64         `json:"executions"`
	Successes      int64         `json:"successes"`
	Failures       int64         `json:"failures"`
	AverageLatency time.Duration `json:"average_latency"`
	LastExecution  time.Time     `json:"last_execution"`
}

// NewMessageValidator creates a new message validator
func NewMessageValidator(config ValidationConfig, logger common.Logger, metrics common.Metrics) *MessageValidator {
	validator := &MessageValidator{
		config:  config,
		rules:   make(map[string]ValidationRule),
		schemas: make(map[string]*JSONSchema),
		logger:  logger,
		metrics: metrics,
		stats: &ValidationStats{
			ValidationErrors:   make(map[string]int64),
			ValidationWarnings: make(map[string]int64),
			RuleExecutions:     make(map[string]*RuleExecutionStats),
		},
	}

	// Compile forbidden patterns
	validator.compileForbiddenPatterns()

	// Add default validation rules
	validator.addDefaultRules()

	return validator
}

// Validate validates a message using all enabled rules
func (mv *MessageValidator) Validate(ctx context.Context, message *streaming.Message, filterContext *FilterContext) *ValidationResult {
	start := time.Now()
	defer func() {
		latency := time.Since(start)
		mv.updateStats(latency)
	}()

	if !mv.config.Enabled {
		return &ValidationResult{
			Valid:       true,
			ProcessedAt: time.Now(),
		}
	}

	// Run validation based on level
	switch mv.config.Level {
	case ValidationLevelNone:
		return &ValidationResult{Valid: true, ProcessedAt: time.Now()}
	case ValidationLevelBasic:
		return mv.validateBasic(ctx, message, filterContext)
	case ValidationLevelStrict:
		return mv.validateStrict(ctx, message, filterContext)
	case ValidationLevelCustom:
		return mv.validateCustom(ctx, message, filterContext)
	default:
		return mv.validateBasic(ctx, message, filterContext)
	}
}

// validateBasic performs basic validation
func (mv *MessageValidator) validateBasic(ctx context.Context, message *streaming.Message, filterContext *FilterContext) *ValidationResult {
	result := &ValidationResult{
		Valid:       true,
		ProcessedAt: time.Now(),
		Metadata:    make(map[string]interface{}),
	}

	// Basic field validation
	if err := mv.validateRequiredFields(message); err != nil {
		result.Valid = false
		result.Errors = append(result.Errors, *err)
	}

	// Size validation
	if err := mv.validateSize(message); err != nil {
		result.Valid = false
		result.Errors = append(result.Errors, *err)
	}

	// Message type validation
	if err := mv.validateMessageType(message); err != nil {
		result.Valid = false
		result.Errors = append(result.Errors, *err)
	}

	// Content scanning if enabled
	if mv.config.EnableContentScan {
		if warnings := mv.scanContent(message); len(warnings) > 0 {
			result.Warnings = append(result.Warnings, warnings...)
		}
	}

	return result
}

// validateStrict performs strict validation
func (mv *MessageValidator) validateStrict(ctx context.Context, message *streaming.Message, filterContext *FilterContext) *ValidationResult {
	// OnStart with basic validation
	result := mv.validateBasic(ctx, message, filterContext)

	// Additional strict validations
	if mv.config.StrictTypeValidation {
		if err := mv.validateTypes(message); err != nil {
			result.Valid = false
			result.Errors = append(result.Errors, *err)
		}
	}

	// Schema validation if enabled
	if mv.config.EnableSchemaValidation {
		if err := mv.validateSchema(message); err != nil {
			result.Valid = false
			result.Errors = append(result.Errors, *err)
		}
	}

	// Depth validation
	if err := mv.validateDepth(message); err != nil {
		result.Valid = false
		result.Errors = append(result.Errors, *err)
	}

	// Array length validation
	if err := mv.validateArrayLengths(message); err != nil {
		result.Valid = false
		result.Errors = append(result.Errors, *err)
	}

	return result
}

// validateCustom performs custom rule-based validation
func (mv *MessageValidator) validateCustom(ctx context.Context, message *streaming.Message, filterContext *FilterContext) *ValidationResult {
	// OnStart with strict validation
	result := mv.validateStrict(ctx, message, filterContext)

	mv.mu.RLock()
	rules := make([]ValidationRule, 0, len(mv.rules))
	for _, rule := range mv.rules {
		if rule.IsEnabled() {
			rules = append(rules, rule)
		}
	}
	mv.mu.RUnlock()

	// Apply custom rules
	for _, rule := range rules {
		ruleStart := time.Now()
		ruleResult := rule.Validate(ctx, message, filterContext)
		ruleDuration := time.Since(ruleStart)

		// Update rule statistics
		mv.updateRuleStats(rule.Name(), ruleResult.Valid, ruleDuration)

		if !ruleResult.Valid {
			result.Valid = false
			result.Errors = append(result.Errors, ruleResult.Errors...)
		}

		result.Warnings = append(result.Warnings, ruleResult.Warnings...)

		// Merge metadata
		for k, v := range ruleResult.Metadata {
			result.Metadata[k] = v
		}
	}

	return result
}

// Validation helper methods

// validateRequiredFields validates that required fields are present
func (mv *MessageValidator) validateRequiredFields(message *streaming.Message) *ValidationError {
	for _, field := range mv.config.RequiredFields {
		switch field {
		case "id":
			if message.ID == "" {
				return &ValidationError{
					Field:    "id",
					Code:     "REQUIRED_FIELD_MISSING",
					Message:  "message ID is required",
					Severity: ValidationSeverityError,
				}
			}
		case "type":
			if message.Type == "" {
				return &ValidationError{
					Field:    "type",
					Code:     "REQUIRED_FIELD_MISSING",
					Message:  "message type is required",
					Severity: ValidationSeverityError,
				}
			}
		case "timestamp":
			if message.Timestamp.IsZero() {
				return &ValidationError{
					Field:    "timestamp",
					Code:     "REQUIRED_FIELD_MISSING",
					Message:  "message timestamp is required",
					Severity: ValidationSeverityError,
				}
			}
		case "from":
			if message.From == "" {
				return &ValidationError{
					Field:    "from",
					Code:     "REQUIRED_FIELD_MISSING",
					Message:  "message sender is required",
					Severity: ValidationSeverityError,
				}
			}
		case "room_id":
			if message.RoomID == "" {
				return &ValidationError{
					Field:    "room_id",
					Code:     "REQUIRED_FIELD_MISSING",
					Message:  "room ID is required",
					Severity: ValidationSeverityError,
				}
			}
		}
	}
	return nil
}

// validateSize validates message size constraints
func (mv *MessageValidator) validateSize(message *streaming.Message) *ValidationError {
	// Validate data size
	if message.Data != nil {
		if dataBytes, err := json.Marshal(message.Data); err != nil {
			return &ValidationError{
				Field:    "data",
				Code:     "SERIALIZATION_ERROR",
				Message:  fmt.Sprintf("failed to serialize message data: %v", err),
				Severity: ValidationSeverityError,
			}
		} else if len(dataBytes) > mv.config.MaxMessageSize {
			return &ValidationError{
				Field:       "data",
				Code:        "SIZE_LIMIT_EXCEEDED",
				Message:     fmt.Sprintf("message data too large: %d bytes (max: %d)", len(dataBytes), mv.config.MaxMessageSize),
				Value:       len(dataBytes),
				Constraint:  fmt.Sprintf("max_size=%d", mv.config.MaxMessageSize),
				Severity:    ValidationSeverityError,
				Suggestions: []string{"reduce message content", "split into multiple messages", "compress data"},
			}
		}
	}

	// Validate metadata size
	if message.Metadata != nil {
		if metaBytes, err := json.Marshal(message.Metadata); err != nil {
			return &ValidationError{
				Field:    "metadata",
				Code:     "SERIALIZATION_ERROR",
				Message:  fmt.Sprintf("failed to serialize message metadata: %v", err),
				Severity: ValidationSeverityError,
			}
		} else if len(metaBytes) > mv.config.MaxMetadataSize {
			return &ValidationError{
				Field:       "metadata",
				Code:        "SIZE_LIMIT_EXCEEDED",
				Message:     fmt.Sprintf("message metadata too large: %d bytes (max: %d)", len(metaBytes), mv.config.MaxMetadataSize),
				Value:       len(metaBytes),
				Constraint:  fmt.Sprintf("max_size=%d", mv.config.MaxMetadataSize),
				Severity:    ValidationSeverityError,
				Suggestions: []string{"reduce metadata", "move large data to message body"},
			}
		}
	}

	return nil
}

// validateMessageType validates that the message type is allowed
func (mv *MessageValidator) validateMessageType(message *streaming.Message) *ValidationError {
	if len(mv.config.AllowedMessageTypes) == 0 {
		return nil // No restrictions
	}

	messageType := string(message.Type)
	for _, allowedType := range mv.config.AllowedMessageTypes {
		if messageType == allowedType {
			return nil // Type is allowed
		}
	}

	return &ValidationError{
		Field:       "type",
		Code:        "INVALID_MESSAGE_TYPE",
		Message:     fmt.Sprintf("message type '%s' is not allowed", messageType),
		Value:       messageType,
		Constraint:  fmt.Sprintf("allowed_types=%v", mv.config.AllowedMessageTypes),
		Severity:    ValidationSeverityError,
		Suggestions: mv.config.AllowedMessageTypes,
	}
}

// validateTypes performs strict type validation
func (mv *MessageValidator) validateTypes(message *streaming.Message) *ValidationError {
	// Validate TTL
	if message.TTL < 0 {
		return &ValidationError{
			Field:    "ttl",
			Code:     "INVALID_VALUE",
			Message:  "TTL cannot be negative",
			Value:    message.TTL,
			Severity: ValidationSeverityError,
		}
	}

	// Validate timestamp is not in the future (with small tolerance)
	now := time.Now()
	tolerance := 5 * time.Minute
	if message.Timestamp.After(now.Add(tolerance)) {
		return &ValidationError{
			Field:    "timestamp",
			Code:     "INVALID_TIMESTAMP",
			Message:  "timestamp cannot be in the future",
			Value:    message.Timestamp,
			Severity: ValidationSeverityWarning,
		}
	}

	return nil
}

// validateDepth validates the depth of nested structures
func (mv *MessageValidator) validateDepth(message *streaming.Message) *ValidationError {
	if message.Data != nil {
		if depth := mv.getDepth(message.Data); depth > mv.config.MaxFieldDepth {
			return &ValidationError{
				Field:      "data",
				Code:       "DEPTH_LIMIT_EXCEEDED",
				Message:    fmt.Sprintf("data structure too deep: %d levels (max: %d)", depth, mv.config.MaxFieldDepth),
				Value:      depth,
				Constraint: fmt.Sprintf("max_depth=%d", mv.config.MaxFieldDepth),
				Severity:   ValidationSeverityError,
			}
		}
	}

	if message.Metadata != nil {
		if depth := mv.getDepth(message.Metadata); depth > mv.config.MaxFieldDepth {
			return &ValidationError{
				Field:      "metadata",
				Code:       "DEPTH_LIMIT_EXCEEDED",
				Message:    fmt.Sprintf("metadata structure too deep: %d levels (max: %d)", depth, mv.config.MaxFieldDepth),
				Value:      depth,
				Constraint: fmt.Sprintf("max_depth=%d", mv.config.MaxFieldDepth),
				Severity:   ValidationSeverityError,
			}
		}
	}

	return nil
}

// validateArrayLengths validates array lengths
func (mv *MessageValidator) validateArrayLengths(message *streaming.Message) *ValidationError {
	if err := mv.validateArrayInValue(message.Data, "data"); err != nil {
		return err
	}

	if err := mv.validateArrayInValue(message.Metadata, "metadata"); err != nil {
		return err
	}

	return nil
}

// validateArrayInValue recursively validates arrays in a value
func (mv *MessageValidator) validateArrayInValue(value interface{}, fieldPath string) *ValidationError {
	if value == nil {
		return nil
	}

	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Slice, reflect.Array:
		if v.Len() > mv.config.MaxArrayLength {
			return &ValidationError{
				Field:      fieldPath,
				Code:       "ARRAY_LENGTH_EXCEEDED",
				Message:    fmt.Sprintf("array too long: %d elements (max: %d)", v.Len(), mv.config.MaxArrayLength),
				Value:      v.Len(),
				Constraint: fmt.Sprintf("max_length=%d", mv.config.MaxArrayLength),
				Severity:   ValidationSeverityError,
			}
		}

		// Validate nested arrays
		for i := 0; i < v.Len(); i++ {
			if err := mv.validateArrayInValue(v.Index(i).Interface(), fmt.Sprintf("%s[%d]", fieldPath, i)); err != nil {
				return err
			}
		}

	case reflect.Map:
		for _, key := range v.MapKeys() {
			keyStr := fmt.Sprintf("%v", key.Interface())
			if err := mv.validateArrayInValue(v.MapIndex(key).Interface(), fmt.Sprintf("%s.%s", fieldPath, keyStr)); err != nil {
				return err
			}
		}
	}

	return nil
}

// scanContent scans message content for forbidden patterns
func (mv *MessageValidator) scanContent(message *streaming.Message) []ValidationWarning {
	var warnings []ValidationWarning

	// Scan data content
	if content := mv.extractTextContent(message.Data); content != "" {
		for _, regex := range mv.forbiddenRegex {
			if regex.MatchString(content) {
				warnings = append(warnings, ValidationWarning{
					Field:   "data",
					Code:    "FORBIDDEN_CONTENT",
					Message: "content matches forbidden pattern",
				})
				break
			}
		}
	}

	return warnings
}

// validateSchema validates message against JSON schema
func (mv *MessageValidator) validateSchema(message *streaming.Message) *ValidationError {
	messageType := string(message.Type)

	mv.mu.RLock()
	schema, exists := mv.schemas[messageType]
	mv.mu.RUnlock()

	if !exists {
		return nil // No schema defined for this message type
	}

	if err := schema.Validate(message.Data); err != nil {
		return &ValidationError{
			Field:    "data",
			Code:     "SCHEMA_VALIDATION_FAILED",
			Message:  fmt.Sprintf("message data does not match schema: %v", err),
			Severity: ValidationSeverityError,
		}
	}

	return nil
}

// Helper methods

// getDepth calculates the depth of a nested structure
func (mv *MessageValidator) getDepth(value interface{}) int {
	return mv.getDepthRecursive(value, 0)
}

// getDepthRecursive recursively calculates depth
func (mv *MessageValidator) getDepthRecursive(value interface{}, currentDepth int) int {
	if value == nil {
		return currentDepth
	}

	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Map:
		maxDepth := currentDepth
		for _, key := range v.MapKeys() {
			depth := mv.getDepthRecursive(v.MapIndex(key).Interface(), currentDepth+1)
			if depth > maxDepth {
				maxDepth = depth
			}
		}
		return maxDepth

	case reflect.Slice, reflect.Array:
		maxDepth := currentDepth
		for i := 0; i < v.Len(); i++ {
			depth := mv.getDepthRecursive(v.Index(i).Interface(), currentDepth+1)
			if depth > maxDepth {
				maxDepth = depth
			}
		}
		return maxDepth

	default:
		return currentDepth
	}
}

// extractTextContent extracts text content from a value for scanning
func (mv *MessageValidator) extractTextContent(value interface{}) string {
	if value == nil {
		return ""
	}

	switch v := value.(type) {
	case string:
		return v
	case map[string]interface{}:
		if text, ok := v["text"].(string); ok {
			return text
		}
		if content, ok := v["content"].(string); ok {
			return content
		}
		if message, ok := v["message"].(string); ok {
			return message
		}
	}

	return ""
}

// compileForbiddenPatterns compiles forbidden patterns into regex
func (mv *MessageValidator) compileForbiddenPatterns() {
	mv.forbiddenRegex = make([]*regexp.Regexp, 0, len(mv.config.ForbiddenPatterns))

	for _, pattern := range mv.config.ForbiddenPatterns {
		if regex, err := regexp.Compile(pattern); err == nil {
			mv.forbiddenRegex = append(mv.forbiddenRegex, regex)
		} else if mv.logger != nil {
			mv.logger.Warn("invalid forbidden pattern",
				logger.String("pattern", pattern),
				logger.Error(err),
			)
		}
	}
}

// addDefaultRules adds default validation rules
func (mv *MessageValidator) addDefaultRules() {
	// Add basic validation rules
	mv.AddRule(NewBasicFieldValidationRule())
	mv.AddRule(NewSecurityValidationRule())
	mv.AddRule(NewPerformanceValidationRule())
}

// Rule management

// AddRule adds a validation rule
func (mv *MessageValidator) AddRule(rule ValidationRule) error {
	mv.mu.Lock()
	defer mv.mu.Unlock()

	name := rule.Name()
	if _, exists := mv.rules[name]; exists {
		return common.ErrServiceAlreadyExists(name)
	}

	mv.rules[name] = rule
	mv.stats.RuleExecutions[name] = &RuleExecutionStats{}

	if mv.logger != nil {
		mv.logger.Info("validation rule added", logger.String("rule", name))
	}

	return nil
}

// RemoveRule removes a validation rule
func (mv *MessageValidator) RemoveRule(name string) error {
	mv.mu.Lock()
	defer mv.mu.Unlock()

	if _, exists := mv.rules[name]; !exists {
		return common.ErrServiceNotFound(name)
	}

	delete(mv.rules, name)
	delete(mv.stats.RuleExecutions, name)

	if mv.logger != nil {
		mv.logger.Info("validation rule removed", logger.String("rule", name))
	}

	return nil
}

// GetRule gets a validation rule by name
func (mv *MessageValidator) GetRule(name string) (ValidationRule, error) {
	mv.mu.RLock()
	defer mv.mu.RUnlock()

	rule, exists := mv.rules[name]
	if !exists {
		return nil, common.ErrServiceNotFound(name)
	}

	return rule, nil
}

// Schema management

// AddSchema adds a JSON schema for a message type
func (mv *MessageValidator) AddSchema(messageType string, schema *JSONSchema) error {
	mv.mu.Lock()
	defer mv.mu.Unlock()

	mv.schemas[messageType] = schema

	if mv.logger != nil {
		mv.logger.Info("validation schema added", logger.String("message_type", messageType))
	}

	return nil
}

// RemoveSchema removes a JSON schema
func (mv *MessageValidator) RemoveSchema(messageType string) error {
	mv.mu.Lock()
	defer mv.mu.Unlock()

	delete(mv.schemas, messageType)

	if mv.logger != nil {
		mv.logger.Info("validation schema removed", logger.String("message_type", messageType))
	}

	return nil
}

// Statistics

// GetStats returns validation statistics
func (mv *MessageValidator) GetStats() *ValidationStats {
	mv.mu.RLock()
	defer mv.mu.RUnlock()

	// Create a copy to avoid race conditions
	stats := &ValidationStats{
		TotalValidations:   mv.stats.TotalValidations,
		ValidMessages:      mv.stats.ValidMessages,
		InvalidMessages:    mv.stats.InvalidMessages,
		ValidationErrors:   make(map[string]int64),
		ValidationWarnings: make(map[string]int64),
		RuleExecutions:     make(map[string]*RuleExecutionStats),
		AverageLatency:     mv.stats.AverageLatency,
		LastValidation:     mv.stats.LastValidation,
	}

	for k, v := range mv.stats.ValidationErrors {
		stats.ValidationErrors[k] = v
	}

	for k, v := range mv.stats.ValidationWarnings {
		stats.ValidationWarnings[k] = v
	}

	for k, v := range mv.stats.RuleExecutions {
		stats.RuleExecutions[k] = &RuleExecutionStats{
			Executions:     v.Executions,
			Successes:      v.Successes,
			Failures:       v.Failures,
			AverageLatency: v.AverageLatency,
			LastExecution:  v.LastExecution,
		}
	}

	return stats
}

// updateStats updates validation statistics
func (mv *MessageValidator) updateStats(latency time.Duration) {
	mv.mu.Lock()
	defer mv.mu.Unlock()

	mv.stats.TotalValidations++
	mv.stats.LastValidation = time.Now()

	// Update average latency
	if mv.stats.TotalValidations == 1 {
		mv.stats.AverageLatency = latency
	} else {
		total := time.Duration(mv.stats.TotalValidations-1) * mv.stats.AverageLatency
		mv.stats.AverageLatency = (total + latency) / time.Duration(mv.stats.TotalValidations)
	}

	if mv.metrics != nil {
		mv.metrics.Counter("streaming.validation.total").Inc()
		mv.metrics.Histogram("streaming.validation.latency").Observe(latency.Seconds())
	}
}

// updateRuleStats updates rule execution statistics
func (mv *MessageValidator) updateRuleStats(ruleName string, success bool, latency time.Duration) {
	mv.mu.Lock()
	defer mv.mu.Unlock()

	stats := mv.stats.RuleExecutions[ruleName]
	if stats == nil {
		stats = &RuleExecutionStats{}
		mv.stats.RuleExecutions[ruleName] = stats
	}

	stats.Executions++
	stats.LastExecution = time.Now()

	if success {
		stats.Successes++
	} else {
		stats.Failures++
	}

	// Update average latency
	if stats.Executions == 1 {
		stats.AverageLatency = latency
	} else {
		total := time.Duration(stats.Executions-1) * stats.AverageLatency
		stats.AverageLatency = (total + latency) / time.Duration(stats.Executions)
	}

	if mv.metrics != nil {
		mv.metrics.Counter("streaming.validation.rule_executions", "rule", ruleName).Inc()
		mv.metrics.Histogram("streaming.validation.rule_latency", "rule", ruleName).Observe(latency.Seconds())
	}
}

// Configuration

// UpdateConfig updates the validation configuration
func (mv *MessageValidator) UpdateConfig(config ValidationConfig) error {
	mv.mu.Lock()
	defer mv.mu.Unlock()

	mv.config = config
	mv.compileForbiddenPatterns()

	if mv.logger != nil {
		mv.logger.Info("validation config updated",
			logger.String("level", string(config.Level)),
			logger.Bool("content_scan", config.EnableContentScan),
			logger.Bool("schema_validation", config.EnableSchemaValidation),
		)
	}

	return nil
}

// ValidationFilter implements message filtering based on validation
type ValidationFilter struct {
	*BaseFilter
	validator *MessageValidator
}

// NewValidationFilter creates a new validation filter
func NewValidationFilter(config ValidationConfig, logger common.Logger, metrics common.Metrics) MessageFilter {
	return &ValidationFilter{
		BaseFilter: NewBaseFilter("validation", 15), // High priority
		validator:  NewMessageValidator(config, logger, metrics),
	}
}

// Apply applies the validation filter
func (f *ValidationFilter) Apply(ctx context.Context, message *streaming.Message, filterContext *FilterContext) *FilterResult {
	if !f.IsEnabled() {
		return nil
	}

	// Validate the message
	result := f.validator.Validate(ctx, message, filterContext)

	if !result.Valid {
		// Collect error messages
		var errorMessages []string
		for _, err := range result.Errors {
			errorMessages = append(errorMessages, err.Message)
		}

		return &FilterResult{
			Action:  FilterActionBlock,
			Allowed: false,
			Reason:  fmt.Sprintf("validation failed: %s", strings.Join(errorMessages, "; ")),
			Metadata: map[string]interface{}{
				"validation_errors":   result.Errors,
				"validation_warnings": result.Warnings,
				"validation_metadata": result.Metadata,
			},
		}
	}

	// Message is valid, add validation metadata
	return &FilterResult{
		Action:  FilterActionAllow,
		Allowed: true,
		Metadata: map[string]interface{}{
			"validation_passed":   true,
			"validation_warnings": result.Warnings,
			"validation_metadata": result.Metadata,
		},
	}
}

// GetValidator returns the underlying validator
func (f *ValidationFilter) GetValidator() *MessageValidator {
	return f.validator
}

// Built-in validation rules

// BaseValidationRule provides common functionality for validation rules
type BaseValidationRule struct {
	name    string
	enabled bool
	config  map[string]interface{}
	mu      sync.RWMutex
}

// NewBaseValidationRule creates a new base validation rule
func NewBaseValidationRule(name string) *BaseValidationRule {
	return &BaseValidationRule{
		name:    name,
		enabled: true,
		config:  make(map[string]interface{}),
	}
}

// Name returns the rule name
func (r *BaseValidationRule) Name() string {
	return r.name
}

// IsEnabled returns true if the rule is enabled
func (r *BaseValidationRule) IsEnabled() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.enabled
}

// SetEnabled enables or disables the rule
func (r *BaseValidationRule) SetEnabled(enabled bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.enabled = enabled
}

// GetConfig returns the rule configuration
func (r *BaseValidationRule) GetConfig() map[string]interface{} {
	r.mu.RLock()
	defer r.mu.RUnlock()

	config := make(map[string]interface{})
	for k, v := range r.config {
		config[k] = v
	}
	return config
}

// UpdateConfig updates the rule configuration
func (r *BaseValidationRule) UpdateConfig(config map[string]interface{}) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	for k, v := range config {
		r.config[k] = v
	}

	return nil
}

// Specific validation rules

// BasicFieldValidationRule validates basic message fields
type BasicFieldValidationRule struct {
	*BaseValidationRule
}

// NewBasicFieldValidationRule creates a new basic field validation rule
func NewBasicFieldValidationRule() ValidationRule {
	return &BasicFieldValidationRule{
		BaseValidationRule: NewBaseValidationRule("basic_fields"),
	}
}

// Validate validates basic message fields
func (r *BasicFieldValidationRule) Validate(ctx context.Context, message *streaming.Message, filterContext *FilterContext) *ValidationResult {
	result := &ValidationResult{
		Valid:       true,
		ProcessedAt: time.Now(),
		RuleApplied: r.Name(),
		Metadata:    make(map[string]interface{}),
	}

	// Validate ID format
	if message.ID != "" && len(message.ID) < 3 {
		result.Valid = false
		result.Errors = append(result.Errors, ValidationError{
			Field:    "id",
			Code:     "INVALID_ID_FORMAT",
			Message:  "message ID must be at least 3 characters long",
			Value:    message.ID,
			Severity: ValidationSeverityError,
		})
	}

	// Validate timestamp is reasonable
	now := time.Now()
	if !message.Timestamp.IsZero() {
		if message.Timestamp.Before(now.Add(-24 * time.Hour)) {
			result.Warnings = append(result.Warnings, ValidationWarning{
				Field:   "timestamp",
				Code:    "OLD_TIMESTAMP",
				Message: "message timestamp is more than 24 hours old",
				Value:   message.Timestamp,
			})
		}
	}

	return result
}

// SecurityValidationRule validates security aspects of messages
type SecurityValidationRule struct {
	*BaseValidationRule
	suspiciousPatterns []*regexp.Regexp
}

// NewSecurityValidationRule creates a new security validation rule
func NewSecurityValidationRule() ValidationRule {
	rule := &SecurityValidationRule{
		BaseValidationRule: NewBaseValidationRule("security"),
	}

	// Common injection patterns
	patterns := []string{
		`<script.*?>.*?</script>`,
		`javascript:`,
		`data:text/html`,
		`on\w+\s*=`,
		`<iframe.*?>`,
	}

	for _, pattern := range patterns {
		if regex, err := regexp.Compile(`(?i)` + pattern); err == nil {
			rule.suspiciousPatterns = append(rule.suspiciousPatterns, regex)
		}
	}

	return rule
}

// Validate validates security aspects
func (r *SecurityValidationRule) Validate(ctx context.Context, message *streaming.Message, filterContext *FilterContext) *ValidationResult {
	result := &ValidationResult{
		Valid:       true,
		ProcessedAt: time.Now(),
		RuleApplied: r.Name(),
		Metadata:    make(map[string]interface{}),
	}

	// Check for suspicious content
	if content := extractStringContent(message.Data); content != "" {
		for _, pattern := range r.suspiciousPatterns {
			if pattern.MatchString(content) {
				result.Warnings = append(result.Warnings, ValidationWarning{
					Field:   "data",
					Code:    "SUSPICIOUS_CONTENT",
					Message: "message contains potentially suspicious content",
				})
				break
			}
		}
	}

	return result
}

// PerformanceValidationRule validates performance-related aspects
type PerformanceValidationRule struct {
	*BaseValidationRule
}

// NewPerformanceValidationRule creates a new performance validation rule
func NewPerformanceValidationRule() ValidationRule {
	return &PerformanceValidationRule{
		BaseValidationRule: NewBaseValidationRule("performance"),
	}
}

// Validate validates performance aspects
func (r *PerformanceValidationRule) Validate(ctx context.Context, message *streaming.Message, filterContext *FilterContext) *ValidationResult {
	result := &ValidationResult{
		Valid:       true,
		ProcessedAt: time.Now(),
		RuleApplied: r.Name(),
		Metadata:    make(map[string]interface{}),
	}

	// Check for very long strings that might impact performance
	if content := extractStringContent(message.Data); content != "" {
		if len(content) > 10000 { // 10KB text threshold
			result.Warnings = append(result.Warnings, ValidationWarning{
				Field:   "data",
				Code:    "LARGE_TEXT_CONTENT",
				Message: "message contains very large text content which may impact performance",
				Value:   len(content),
			})
		}
	}

	return result
}

// Helper functions

// extractStringContent extracts string content from data for validation
func extractStringContent(data interface{}) string {
	if data == nil {
		return ""
	}

	switch v := data.(type) {
	case string:
		return v
	case map[string]interface{}:
		var content strings.Builder
		for _, value := range v {
			if str := extractStringContent(value); str != "" {
				content.WriteString(str)
				content.WriteString(" ")
			}
		}
		return content.String()
	case []interface{}:
		var content strings.Builder
		for _, value := range v {
			if str := extractStringContent(value); str != "" {
				content.WriteString(str)
				content.WriteString(" ")
			}
		}
		return content.String()
	default:
		return fmt.Sprintf("%v", v)
	}
}

// JSONSchema represents a simple JSON schema for validation
type JSONSchema struct {
	Type       string                 `json:"type"`
	Properties map[string]*JSONSchema `json:"properties,omitempty"`
	Required   []string               `json:"required,omitempty"`
	Items      *JSONSchema            `json:"items,omitempty"`
	MinLength  *int                   `json:"minLength,omitempty"`
	MaxLength  *int                   `json:"maxLength,omitempty"`
}

// Validate validates data against the schema
func (s *JSONSchema) Validate(data interface{}) error {
	return s.validateValue(data, "")
}

// validateValue validates a value against the schema
func (s *JSONSchema) validateValue(value interface{}, path string) error {
	if value == nil {
		return nil
	}

	switch s.Type {
	case "string":
		str, ok := value.(string)
		if !ok {
			return fmt.Errorf("expected string at %s, got %T", path, value)
		}
		if s.MinLength != nil && len(str) < *s.MinLength {
			return fmt.Errorf("string too short at %s: %d < %d", path, len(str), *s.MinLength)
		}
		if s.MaxLength != nil && len(str) > *s.MaxLength {
			return fmt.Errorf("string too long at %s: %d > %d", path, len(str), *s.MaxLength)
		}

	case "object":
		obj, ok := value.(map[string]interface{})
		if !ok {
			return fmt.Errorf("expected object at %s, got %T", path, value)
		}

		// Check required fields
		for _, required := range s.Required {
			if _, exists := obj[required]; !exists {
				return fmt.Errorf("required field missing at %s.%s", path, required)
			}
		}

		// Validate properties
		for key, propSchema := range s.Properties {
			if propValue, exists := obj[key]; exists {
				propPath := path + "." + key
				if path == "" {
					propPath = key
				}
				if err := propSchema.validateValue(propValue, propPath); err != nil {
					return err
				}
			}
		}

	case "array":
		arr, ok := value.([]interface{})
		if !ok {
			return fmt.Errorf("expected array at %s, got %T", path, value)
		}

		if s.Items != nil {
			for i, item := range arr {
				itemPath := fmt.Sprintf("%s[%d]", path, i)
				if err := s.Items.validateValue(item, itemPath); err != nil {
					return err
				}
			}
		}
	}

	return nil
}
