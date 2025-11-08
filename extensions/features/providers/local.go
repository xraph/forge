package providers

import (
	"context"
	"crypto/md5"
	"encoding/binary"
	"slices"
	"sync"
)

// LocalProvider is an in-memory feature flags provider.
type LocalProvider struct {
	config   LocalProviderConfig
	defaults map[string]any
	flags    map[string]FlagConfig
	mu       sync.RWMutex
}

// NewLocalProvider creates a new local provider.
func NewLocalProvider(config LocalProviderConfig, defaults map[string]any) *LocalProvider {
	return &LocalProvider{
		config:   config,
		defaults: defaults,
		flags:    config.Flags,
	}
}

// Name returns the provider name.
func (p *LocalProvider) Name() string {
	return "local"
}

// Initialize initializes the provider.
func (p *LocalProvider) Initialize(ctx context.Context) error {
	return nil
}

// IsEnabled checks if a boolean flag is enabled.
func (p *LocalProvider) IsEnabled(ctx context.Context, flagKey string, userCtx *UserContext, defaultValue bool) (bool, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	flag, ok := p.flags[flagKey]
	if !ok {
		// Check defaults
		if val, ok := p.defaults[flagKey]; ok {
			if boolVal, ok := val.(bool); ok {
				return boolVal, nil
			}
		}

		return defaultValue, nil
	}

	// Evaluate targeting rules
	if userCtx != nil && len(flag.Targeting) > 0 {
		for _, rule := range flag.Targeting {
			if p.evaluateRule(rule, userCtx) {
				if boolVal, ok := rule.Value.(bool); ok {
					return boolVal, nil
				}
			}
		}
	}

	// Evaluate rollout
	if flag.Rollout != nil && userCtx != nil {
		if p.evaluateRollout(flag.Rollout, userCtx) {
			return flag.Enabled, nil
		}

		return !flag.Enabled, nil
	}

	return flag.Enabled, nil
}

// GetString gets a string flag value.
func (p *LocalProvider) GetString(ctx context.Context, flagKey string, userCtx *UserContext, defaultValue string) (string, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	flag, ok := p.flags[flagKey]
	if !ok {
		// Check defaults
		if val, ok := p.defaults[flagKey]; ok {
			if strVal, ok := val.(string); ok {
				return strVal, nil
			}
		}

		return defaultValue, nil
	}

	// Evaluate targeting rules
	if userCtx != nil && len(flag.Targeting) > 0 {
		for _, rule := range flag.Targeting {
			if p.evaluateRule(rule, userCtx) {
				if strVal, ok := rule.Value.(string); ok {
					return strVal, nil
				}
			}
		}
	}

	if strVal, ok := flag.Value.(string); ok {
		return strVal, nil
	}

	return defaultValue, nil
}

// GetInt gets an integer flag value.
func (p *LocalProvider) GetInt(ctx context.Context, flagKey string, userCtx *UserContext, defaultValue int) (int, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	flag, ok := p.flags[flagKey]
	if !ok {
		// Check defaults
		if val, ok := p.defaults[flagKey]; ok {
			if intVal, ok := val.(int); ok {
				return intVal, nil
			}
		}

		return defaultValue, nil
	}

	// Evaluate targeting rules
	if userCtx != nil && len(flag.Targeting) > 0 {
		for _, rule := range flag.Targeting {
			if p.evaluateRule(rule, userCtx) {
				if intVal, ok := rule.Value.(int); ok {
					return intVal, nil
				}
			}
		}
	}

	if intVal, ok := flag.Value.(int); ok {
		return intVal, nil
	}

	return defaultValue, nil
}

// GetFloat gets a float flag value.
func (p *LocalProvider) GetFloat(ctx context.Context, flagKey string, userCtx *UserContext, defaultValue float64) (float64, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	flag, ok := p.flags[flagKey]
	if !ok {
		// Check defaults
		if val, ok := p.defaults[flagKey]; ok {
			if floatVal, ok := val.(float64); ok {
				return floatVal, nil
			}
		}

		return defaultValue, nil
	}

	// Evaluate targeting rules
	if userCtx != nil && len(flag.Targeting) > 0 {
		for _, rule := range flag.Targeting {
			if p.evaluateRule(rule, userCtx) {
				if floatVal, ok := rule.Value.(float64); ok {
					return floatVal, nil
				}
			}
		}
	}

	if floatVal, ok := flag.Value.(float64); ok {
		return floatVal, nil
	}

	return defaultValue, nil
}

// GetJSON gets a JSON flag value.
func (p *LocalProvider) GetJSON(ctx context.Context, flagKey string, userCtx *UserContext, defaultValue any) (any, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	flag, ok := p.flags[flagKey]
	if !ok {
		// Check defaults
		if val, ok := p.defaults[flagKey]; ok {
			return val, nil
		}

		return defaultValue, nil
	}

	// Evaluate targeting rules
	if userCtx != nil && len(flag.Targeting) > 0 {
		for _, rule := range flag.Targeting {
			if p.evaluateRule(rule, userCtx) {
				return rule.Value, nil
			}
		}
	}

	if flag.Value != nil {
		return flag.Value, nil
	}

	return defaultValue, nil
}

// GetAllFlags gets all flags for a user/context.
func (p *LocalProvider) GetAllFlags(ctx context.Context, userCtx *UserContext) (map[string]any, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	result := make(map[string]any)

	for key, flag := range p.flags {
		// Evaluate flag based on type
		switch flag.Type {
		case "boolean":
			val, _ := p.IsEnabled(ctx, key, userCtx, flag.Enabled)
			result[key] = val
		case "string":
			val, _ := p.GetString(ctx, key, userCtx, "")
			result[key] = val
		case "number":
			val, _ := p.GetInt(ctx, key, userCtx, 0)
			result[key] = val
		default:
			result[key] = flag.Value
		}
	}

	return result, nil
}

// Refresh refreshes flags (no-op for local provider).
func (p *LocalProvider) Refresh(ctx context.Context) error {
	return nil
}

// Close closes the provider.
func (p *LocalProvider) Close() error {
	return nil
}

// Health checks provider health.
func (p *LocalProvider) Health(ctx context.Context) error {
	return nil
}

// UpdateFlag updates a flag configuration (for dynamic updates).
func (p *LocalProvider) UpdateFlag(key string, flag FlagConfig) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.flags[key] = flag
}

// RemoveFlag removes a flag.
func (p *LocalProvider) RemoveFlag(key string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	delete(p.flags, key)
}

// evaluateRule evaluates a targeting rule against user context.
func (p *LocalProvider) evaluateRule(rule TargetingRule, userCtx *UserContext) bool {
	var attrValue string

	// Get attribute value from user context
	switch rule.Attribute {
	case "user_id":
		attrValue = userCtx.UserID
	case "email":
		attrValue = userCtx.Email
	case "name":
		attrValue = userCtx.Name
	case "ip":
		attrValue = userCtx.IP
	case "country":
		attrValue = userCtx.Country
	case "group":
		// Special handling for groups
		if rule.Operator == "in" {
			for _, group := range userCtx.Groups {
				if slices.Contains(rule.Values, group) {
					return true
				}
			}

			return false
		}
	default:
		// Custom attribute
		if val, ok := userCtx.Attributes[rule.Attribute]; ok {
			if strVal, ok := val.(string); ok {
				attrValue = strVal
			}
		}
	}

	// Evaluate operator
	switch rule.Operator {
	case "equals":

		return slices.Contains(rule.Values, attrValue)

	case "contains":
		for _, value := range rule.Values {
			if containsSubstring(attrValue, value) {
				return true
			}
		}

		return false

	case "in":

		return slices.Contains(rule.Values, attrValue)

	case "not_in":

		return !slices.Contains(rule.Values, attrValue)

	default:
		return false
	}
}

// evaluateRollout evaluates percentage-based rollout.
func (p *LocalProvider) evaluateRollout(rollout *RolloutConfig, userCtx *UserContext) bool {
	if rollout.Percentage <= 0 {
		return false
	}

	if rollout.Percentage >= 100 {
		return true
	}

	// Get attribute value for consistent hashing
	attr := rollout.Attribute
	if attr == "" {
		attr = "user_id"
	}

	var attrValue string

	switch attr {
	case "user_id":
		attrValue = userCtx.UserID
	case "email":
		attrValue = userCtx.Email
	default:
		if val, ok := userCtx.Attributes[attr]; ok {
			if strVal, ok := val.(string); ok {
				attrValue = strVal
			}
		}
	}

	if attrValue == "" {
		return false
	}

	// Consistent hashing to determine if user is in rollout
	hash := md5.Sum([]byte(attrValue))
	bucket := binary.BigEndian.Uint32(hash[:4]) % 100

	return int(bucket) < rollout.Percentage
}

// containsSubstring checks if haystack contains needle (case-insensitive).
func containsSubstring(haystack, needle string) bool {
	if len(needle) > len(haystack) {
		return false
	}

	for i := 0; i <= len(haystack)-len(needle); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}

	return false
}
