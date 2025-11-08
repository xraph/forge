package features

import "context"

// Provider defines the interface for feature flag providers.
type Provider interface {
	// Name returns the provider name
	Name() string

	// Initialize initializes the provider
	Initialize(ctx context.Context) error

	// IsEnabled checks if a boolean flag is enabled for a user/context
	IsEnabled(ctx context.Context, flagKey string, userCtx *UserContext, defaultValue bool) (bool, error)

	// GetString gets a string flag value
	GetString(ctx context.Context, flagKey string, userCtx *UserContext, defaultValue string) (string, error)

	// GetInt gets an integer flag value
	GetInt(ctx context.Context, flagKey string, userCtx *UserContext, defaultValue int) (int, error)

	// GetFloat gets a float flag value
	GetFloat(ctx context.Context, flagKey string, userCtx *UserContext, defaultValue float64) (float64, error)

	// GetJSON gets a JSON flag value
	GetJSON(ctx context.Context, flagKey string, userCtx *UserContext, defaultValue any) (any, error)

	// GetAllFlags gets all flags for a user/context
	GetAllFlags(ctx context.Context, userCtx *UserContext) (map[string]any, error)

	// Refresh refreshes flags from remote source
	Refresh(ctx context.Context) error

	// Close closes the provider
	Close() error

	// Health checks provider health
	Health(ctx context.Context) error
}

// UserContext holds user/context information for flag evaluation.
type UserContext struct {
	// UserID is the unique user identifier
	UserID string `json:"user_id"`

	// Email is the user email
	Email string `json:"email,omitempty"`

	// Name is the user name
	Name string `json:"name,omitempty"`

	// Groups are user group memberships
	Groups []string `json:"groups,omitempty"`

	// Attributes are custom attributes for targeting
	Attributes map[string]any `json:"attributes,omitempty"`

	// IP is the user IP address
	IP string `json:"ip,omitempty"`

	// Country is the user country code
	Country string `json:"country,omitempty"`
}

// FlagValue represents a feature flag value with metadata.
type FlagValue struct {
	// Key is the flag key
	Key string `json:"key"`

	// Value is the flag value
	Value any `json:"value"`

	// Enabled indicates if the flag is enabled (for boolean flags)
	Enabled bool `json:"enabled"`

	// Variation is the variation key/name
	Variation string `json:"variation,omitempty"`

	// Reason is the evaluation reason
	Reason string `json:"reason,omitempty"`
}

// EvaluationResult holds the result of a flag evaluation.
type EvaluationResult struct {
	// FlagKey is the flag key
	FlagKey string `json:"flag_key"`

	// Value is the evaluated value
	Value any `json:"value"`

	// VariationIndex is the variation index (if applicable)
	VariationIndex int `json:"variation_index,omitempty"`

	// Reason is the evaluation reason
	Reason string `json:"reason"`

	// RuleID is the ID of the rule that matched (if applicable)
	RuleID string `json:"rule_id,omitempty"`
}

// FlagEvaluationEvent represents a flag evaluation event for analytics.
type FlagEvaluationEvent struct {
	// FlagKey is the flag key
	FlagKey string `json:"flag_key"`

	// UserContext is the user context
	UserContext *UserContext `json:"user_context"`

	// Value is the evaluated value
	Value any `json:"value"`

	// Default indicates if the default value was used
	Default bool `json:"default"`

	// Reason is the evaluation reason
	Reason string `json:"reason"`

	// Timestamp is when the evaluation occurred
	Timestamp int64 `json:"timestamp"`
}
