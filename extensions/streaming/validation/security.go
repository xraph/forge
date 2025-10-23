package validation

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	streaming "github.com/xraph/forge/extensions/streaming/internal"
)

// SecurityValidatorConfig configures security validation.
type SecurityValidatorConfig struct {
	// XSS prevention
	EnableXSSPrevention bool
	AllowedTags         []string
	AllowedAttributes   []string

	// Injection prevention
	EnableSQLInjectionCheck    bool
	EnableScriptInjectionCheck bool

	// Link safety
	EnableLinkSafety bool
	AllowedProtocols []string
	BlockedDomains   []string

	// Malicious content
	EnableMaliciousContentCheck bool
}

// SecurityValidator validates message security.
type SecurityValidator struct {
	config SecurityValidatorConfig

	// Regex patterns for detection
	sqlInjectionRegex    *regexp.Regexp
	scriptInjectionRegex *regexp.Regexp
	xssRegex             *regexp.Regexp
}

// NewSecurityValidator creates a security validator.
func NewSecurityValidator(config SecurityValidatorConfig) *SecurityValidator {
	sv := &SecurityValidator{
		config: config,
	}

	// Build regex patterns
	if config.EnableSQLInjectionCheck {
		// Detect common SQL injection patterns
		sv.sqlInjectionRegex = regexp.MustCompile(`(?i)(union|select|insert|update|delete|drop|create|alter|exec|execute|script|javascript|onerror|onload)`)
	}

	if config.EnableScriptInjectionCheck {
		// Detect script injection
		sv.scriptInjectionRegex = regexp.MustCompile(`(?i)(<script|javascript:|onerror=|onload=|eval\(|setTimeout\(|setInterval\()`)
	}

	if config.EnableXSSPrevention {
		// Detect XSS patterns
		sv.xssRegex = regexp.MustCompile(`(?i)(<script|<iframe|<object|<embed|javascript:|vbscript:|data:text/html)`)
	}

	return sv
}

// Validate performs security validation.
func (sv *SecurityValidator) Validate(ctx context.Context, msg *streaming.Message, sender streaming.EnhancedConnection) error {
	// Validate content
	if err := sv.ValidateContent(msg.Data); err != nil {
		return err
	}

	// Validate metadata
	if err := sv.ValidateMetadata(msg.Metadata); err != nil {
		return err
	}

	return nil
}

// ValidateContent validates content for security issues.
func (sv *SecurityValidator) ValidateContent(content any) error {
	// Convert to string for validation
	str, ok := content.(string)
	if !ok {
		// Not a string, skip validation
		return nil
	}

	// XSS prevention
	if sv.config.EnableXSSPrevention && sv.xssRegex != nil {
		if sv.xssRegex.MatchString(str) {
			return NewValidationError("content", "potential XSS attack detected", "SECURITY_XSS")
		}
	}

	// SQL injection check
	if sv.config.EnableSQLInjectionCheck && sv.sqlInjectionRegex != nil {
		if sv.sqlInjectionRegex.MatchString(str) {
			return NewValidationError("content", "potential SQL injection detected", "SECURITY_SQL_INJECTION")
		}
	}

	// Script injection check
	if sv.config.EnableScriptInjectionCheck && sv.scriptInjectionRegex != nil {
		if sv.scriptInjectionRegex.MatchString(str) {
			return NewValidationError("content", "potential script injection detected", "SECURITY_SCRIPT_INJECTION")
		}
	}

	// Link safety check
	if sv.config.EnableLinkSafety {
		if err := sv.checkLinkSafety(str); err != nil {
			return err
		}
	}

	return nil
}

// ValidateMetadata validates metadata for security issues.
func (sv *SecurityValidator) ValidateMetadata(metadata map[string]any) error {
	// Check for suspicious metadata keys
	suspiciousKeys := []string{"__proto__", "constructor", "prototype"}
	for key := range metadata {
		for _, suspicious := range suspiciousKeys {
			if strings.Contains(strings.ToLower(key), suspicious) {
				return NewValidationError("metadata", fmt.Sprintf("suspicious metadata key: %s", key), "SECURITY_METADATA")
			}
		}
	}

	return nil
}

func (sv *SecurityValidator) checkLinkSafety(text string) error {
	// Extract URLs
	urlRegex := regexp.MustCompile(`https?://[^\s]+`)
	urls := urlRegex.FindAllString(text, -1)

	for _, urlStr := range urls {
		parsedURL, err := url.Parse(urlStr)
		if err != nil {
			return NewValidationError("content", fmt.Sprintf("invalid URL: %s", urlStr), "SECURITY_INVALID_URL")
		}

		// Check protocol
		if len(sv.config.AllowedProtocols) > 0 {
			allowed := false
			for _, protocol := range sv.config.AllowedProtocols {
				if parsedURL.Scheme == protocol {
					allowed = true
					break
				}
			}
			if !allowed {
				return NewValidationError("content", fmt.Sprintf("disallowed protocol: %s", parsedURL.Scheme), "SECURITY_PROTOCOL")
			}
		}

		// Check blocked domains
		for _, blocked := range sv.config.BlockedDomains {
			if strings.Contains(parsedURL.Host, blocked) {
				return NewValidationError("content", fmt.Sprintf("blocked domain: %s", parsedURL.Host), "SECURITY_BLOCKED_DOMAIN")
			}
		}
	}

	return nil
}
