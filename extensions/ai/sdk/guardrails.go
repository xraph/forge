package sdk

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/xraph/forge"
)

// GuardrailManager manages safety guardrails for AI operations.
type GuardrailManager struct {
	logger  forge.Logger
	metrics forge.Metrics

	// Enabled guardrails
	enablePII             bool
	enableToxicity        bool
	enablePromptInjection bool
	enableContentFilter   bool

	// Configuration
	piiPatterns           []*regexp.Regexp
	toxicWords            map[string]bool
	injectionPatterns     []*regexp.Regexp
	blockedContentPattern []*regexp.Regexp
	maxInputLength        int
	maxOutputLength       int

	// Custom validators
	customValidators []func(context.Context, string) error
}

// GuardrailOptions configures the guardrail manager.
type GuardrailOptions struct {
	EnablePII             bool
	EnableToxicity        bool
	EnablePromptInjection bool
	EnableContentFilter   bool
	MaxInputLength        int
	MaxOutputLength       int
	CustomPIIPatterns     []string
	CustomToxicWords      []string
	CustomBlockedPatterns []string
}

// GuardrailViolation represents a detected safety violation.
type GuardrailViolation struct {
	Type        string
	Description string
	Severity    string // "low", "medium", "high", "critical"
	Location    string // "input" or "output"
	Offending   string // The offending text (may be redacted)
}

// NewGuardrailManager creates a new guardrail manager.
func NewGuardrailManager(
	logger forge.Logger,
	metrics forge.Metrics,
	opts *GuardrailOptions,
) *GuardrailManager {
	gm := &GuardrailManager{
		logger:                logger,
		metrics:               metrics,
		enablePII:             true,
		enableToxicity:        true,
		enablePromptInjection: true,
		enableContentFilter:   true,
		maxInputLength:        100000, // 100k chars
		maxOutputLength:       50000,  // 50k chars
		toxicWords:            make(map[string]bool),
		piiPatterns:           make([]*regexp.Regexp, 0),
		injectionPatterns:     make([]*regexp.Regexp, 0),
		blockedContentPattern: make([]*regexp.Regexp, 0),
		customValidators:      make([]func(context.Context, string) error, 0),
	}

	if opts != nil {
		gm.enablePII = opts.EnablePII
		gm.enableToxicity = opts.EnableToxicity
		gm.enablePromptInjection = opts.EnablePromptInjection
		gm.enableContentFilter = opts.EnableContentFilter

		if opts.MaxInputLength > 0 {
			gm.maxInputLength = opts.MaxInputLength
		}

		if opts.MaxOutputLength > 0 {
			gm.maxOutputLength = opts.MaxOutputLength
		}

		// Add custom PII patterns
		for _, pattern := range opts.CustomPIIPatterns {
			if re, err := regexp.Compile(pattern); err == nil {
				gm.piiPatterns = append(gm.piiPatterns, re)
			}
		}

		// Add custom toxic words
		for _, word := range opts.CustomToxicWords {
			gm.toxicWords[strings.ToLower(word)] = true
		}

		// Add custom blocked patterns
		for _, pattern := range opts.CustomBlockedPatterns {
			if re, err := regexp.Compile(pattern); err == nil {
				gm.blockedContentPattern = append(gm.blockedContentPattern, re)
			}
		}
	}

	// Initialize default patterns
	gm.initializeDefaultPatterns()

	return gm
}

// initializeDefaultPatterns sets up default detection patterns.
func (gm *GuardrailManager) initializeDefaultPatterns() {
	// PII Patterns
	piiPatterns := []string{
		// Email addresses
		`\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Z|a-z]{2,}\b`,
		// Phone numbers (US format)
		`\b\d{3}[-.]?\d{3}[-.]?\d{4}\b`,
		// SSN
		`\b\d{3}-\d{2}-\d{4}\b`,
		// Credit card numbers (simple pattern)
		`\b\d{4}[\s-]?\d{4}[\s-]?\d{4}[\s-]?\d{4}\b`,
		// IP addresses
		`\b\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}\b`,
	}

	for _, pattern := range piiPatterns {
		if re, err := regexp.Compile(pattern); err == nil {
			gm.piiPatterns = append(gm.piiPatterns, re)
		}
	}

	// Prompt injection patterns
	injectionPatterns := []string{
		`(?i)ignore\s+(previous|above|all)\s+(instructions?|prompts?)`,
		`(?i)disregard\s+(previous|above|all)`,
		`(?i)forget\s+(everything|all|previous)`,
		`(?i)new\s+instructions?:`,
		`(?i)system\s*:\s*`,
		`(?i)\[INST\]|\[/INST\]`, // Common injection markers
		`(?i)<\|im_start\|>`,     // ChatML markers
	}

	for _, pattern := range injectionPatterns {
		if re, err := regexp.Compile(pattern); err == nil {
			gm.injectionPatterns = append(gm.injectionPatterns, re)
		}
	}

	// Default toxic words (simplified list - in production, use a comprehensive database)
	toxicWords := []string{
		"offensive1", "offensive2", "hate", // Placeholder - add actual words
	}

	for _, word := range toxicWords {
		gm.toxicWords[strings.ToLower(word)] = true
	}
}

// ValidateInput validates input text against all enabled guardrails.
func (gm *GuardrailManager) ValidateInput(ctx context.Context, input string) ([]GuardrailViolation, error) {
	violations := make([]GuardrailViolation, 0)

	if gm.logger != nil {
		gm.logger.Debug("Validating input",
			F("length", len(input)),
			F("pii_enabled", gm.enablePII),
			F("toxicity_enabled", gm.enableToxicity),
			F("injection_enabled", gm.enablePromptInjection),
		)
	}

	// Check input length
	if len(input) > gm.maxInputLength {
		violations = append(violations, GuardrailViolation{
			Type:        "input_length",
			Description: fmt.Sprintf("Input exceeds maximum length of %d characters", gm.maxInputLength),
			Severity:    "high",
			Location:    "input",
		})
	}

	// Check for PII
	if gm.enablePII {
		piiViolations := gm.detectPII(input, "input")
		violations = append(violations, piiViolations...)
	}

	// Check for toxicity
	if gm.enableToxicity {
		toxicViolations := gm.detectToxicity(input, "input")
		violations = append(violations, toxicViolations...)
	}

	// Check for prompt injection
	if gm.enablePromptInjection {
		injectionViolations := gm.detectPromptInjection(input, "input")
		violations = append(violations, injectionViolations...)
	}

	// Check for blocked content
	if gm.enableContentFilter {
		contentViolations := gm.detectBlockedContent(input, "input")
		violations = append(violations, contentViolations...)
	}

	// Run custom validators
	for i, validator := range gm.customValidators {
		if err := validator(ctx, input); err != nil {
			violations = append(violations, GuardrailViolation{
				Type:        fmt.Sprintf("custom_%d", i),
				Description: err.Error(),
				Severity:    "medium",
				Location:    "input",
			})
		}
	}

	// Record metrics
	if gm.metrics != nil {
		gm.metrics.Counter("forge.ai.sdk.guardrails.input_checks").Inc()

		if len(violations) > 0 {
			gm.metrics.Counter("forge.ai.sdk.guardrails.input_violations", "count", strconv.Itoa(len(violations))).Inc()
		}
	}

	return violations, nil
}

// ValidateOutput validates output text against all enabled guardrails.
func (gm *GuardrailManager) ValidateOutput(ctx context.Context, output string) ([]GuardrailViolation, error) {
	violations := make([]GuardrailViolation, 0)

	// Check output length
	if len(output) > gm.maxOutputLength {
		violations = append(violations, GuardrailViolation{
			Type:        "output_length",
			Description: fmt.Sprintf("Output exceeds maximum length of %d characters", gm.maxOutputLength),
			Severity:    "medium",
			Location:    "output",
		})
	}

	// Check for PII in output
	if gm.enablePII {
		piiViolations := gm.detectPII(output, "output")
		violations = append(violations, piiViolations...)
	}

	// Check for toxicity in output
	if gm.enableToxicity {
		toxicViolations := gm.detectToxicity(output, "output")
		violations = append(violations, toxicViolations...)
	}

	// Check for blocked content in output
	if gm.enableContentFilter {
		contentViolations := gm.detectBlockedContent(output, "output")
		violations = append(violations, contentViolations...)
	}

	// Record metrics
	if gm.metrics != nil {
		gm.metrics.Counter("forge.ai.sdk.guardrails.output_checks").Inc()

		if len(violations) > 0 {
			gm.metrics.Counter("forge.ai.sdk.guardrails.output_violations", "count", strconv.Itoa(len(violations))).Inc()
		}
	}

	return violations, nil
}

// detectPII detects personally identifiable information.
func (gm *GuardrailManager) detectPII(text, location string) []GuardrailViolation {
	violations := make([]GuardrailViolation, 0)

	for _, pattern := range gm.piiPatterns {
		if matches := pattern.FindAllString(text, -1); len(matches) > 0 {
			// Redact the actual PII in the violation
			redacted := "[REDACTED]"
			violations = append(violations, GuardrailViolation{
				Type:        "pii",
				Description: fmt.Sprintf("Detected potential PII (%d instances)", len(matches)),
				Severity:    "critical",
				Location:    location,
				Offending:   redacted,
			})
		}
	}

	return violations
}

// detectToxicity detects toxic or harmful content.
func (gm *GuardrailManager) detectToxicity(text, location string) []GuardrailViolation {
	violations := make([]GuardrailViolation, 0)

	words := strings.Fields(strings.ToLower(text))
	toxicFound := make([]string, 0)

	for _, word := range words {
		// Remove punctuation
		cleaned := strings.Trim(word, ".,!?;:")
		if gm.toxicWords[cleaned] {
			toxicFound = append(toxicFound, "[REDACTED]")
		}
	}

	if len(toxicFound) > 0 {
		violations = append(violations, GuardrailViolation{
			Type:        "toxicity",
			Description: fmt.Sprintf("Detected toxic content (%d instances)", len(toxicFound)),
			Severity:    "high",
			Location:    location,
			Offending:   "[REDACTED]",
		})
	}

	return violations
}

// detectPromptInjection detects prompt injection attempts.
func (gm *GuardrailManager) detectPromptInjection(text, location string) []GuardrailViolation {
	violations := make([]GuardrailViolation, 0)

	for _, pattern := range gm.injectionPatterns {
		if match := pattern.FindString(text); match != "" {
			violations = append(violations, GuardrailViolation{
				Type:        "prompt_injection",
				Description: "Detected potential prompt injection attempt",
				Severity:    "critical",
				Location:    location,
				Offending:   match,
			})
		}
	}

	return violations
}

// detectBlockedContent detects blocked content patterns.
func (gm *GuardrailManager) detectBlockedContent(text, location string) []GuardrailViolation {
	violations := make([]GuardrailViolation, 0)

	for _, pattern := range gm.blockedContentPattern {
		if match := pattern.FindString(text); match != "" {
			violations = append(violations, GuardrailViolation{
				Type:        "blocked_content",
				Description: "Content matches blocked pattern",
				Severity:    "high",
				Location:    location,
				Offending:   "[REDACTED]",
			})
		}
	}

	return violations
}

// RedactPII removes PII from text.
func (gm *GuardrailManager) RedactPII(text string) string {
	result := text

	for _, pattern := range gm.piiPatterns {
		result = pattern.ReplaceAllString(result, "[REDACTED]")
	}

	return result
}

// AddCustomValidator adds a custom validation function.
func (gm *GuardrailManager) AddCustomValidator(validator func(context.Context, string) error) {
	gm.customValidators = append(gm.customValidators, validator)
}

// ShouldBlock returns true if violations warrant blocking the operation.
func ShouldBlock(violations []GuardrailViolation) bool {
	for _, v := range violations {
		if v.Severity == "critical" || v.Severity == "high" {
			return true
		}
	}

	return false
}

// FormatViolations formats violations into a human-readable string.
func FormatViolations(violations []GuardrailViolation) string {
	if len(violations) == 0 {
		return "No violations detected"
	}

	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("Found %d violation(s):\n", len(violations)))

	for i, v := range violations {
		builder.WriteString(fmt.Sprintf("%d. [%s] %s - %s (Severity: %s, Location: %s)\n",
			i+1, v.Type, v.Description, v.Offending, v.Severity, v.Location))
	}

	return builder.String()
}
