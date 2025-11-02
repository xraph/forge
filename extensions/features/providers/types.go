package providers

// UserContext holds user/context information for flag evaluation
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
	Attributes map[string]interface{} `json:"attributes,omitempty"`

	// IP is the user IP address
	IP string `json:"ip,omitempty"`

	// Country is the user country code
	Country string `json:"country,omitempty"`
}

// LocalProviderConfig for local in-memory provider
type LocalProviderConfig struct {
	// Flags are the static flag definitions
	Flags map[string]FlagConfig `yaml:"flags" json:"flags"`
}

// FlagConfig defines a single feature flag
type FlagConfig struct {
	// Key is the unique flag identifier
	Key string `yaml:"key" json:"key"`

	// Name is the human-readable name
	Name string `yaml:"name" json:"name"`

	// Description describes what this flag controls
	Description string `yaml:"description" json:"description"`

	// Type is the flag value type: "boolean", "string", "number", "json"
	Type string `yaml:"type" json:"type"`

	// Enabled is the default enabled state for boolean flags
	Enabled bool `yaml:"enabled" json:"enabled"`

	// Value is the default value for non-boolean flags
	Value interface{} `yaml:"value" json:"value"`

	// Targeting defines user/group targeting rules
	Targeting []TargetingRule `yaml:"targeting" json:"targeting"`

	// Rollout defines percentage-based rollout
	Rollout *RolloutConfig `yaml:"rollout" json:"rollout"`
}

// TargetingRule defines targeting rules for a flag
type TargetingRule struct {
	// Attribute is the user attribute to match (e.g., "user_id", "email", "group")
	Attribute string `yaml:"attribute" json:"attribute"`

	// Operator is the comparison operator: "equals", "contains", "in", "not_in"
	Operator string `yaml:"operator" json:"operator"`

	// Values are the values to match against
	Values []string `yaml:"values" json:"values"`

	// Value is the flag value for matching users
	Value interface{} `yaml:"value" json:"value"`
}

// RolloutConfig defines percentage-based rollout
type RolloutConfig struct {
	// Percentage of users to enable (0-100)
	Percentage int `yaml:"percentage" json:"percentage"`

	// Attribute to use for consistent hashing (default: "user_id")
	Attribute string `yaml:"attribute" json:"attribute"`
}

