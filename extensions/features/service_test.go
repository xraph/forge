package features

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewService(t *testing.T) {
	provider := NewLocalProvider(LocalProviderConfig{}, nil)
	logger := newMockLogger()

	service := NewService(provider, logger)

	assert.NotNil(t, service)
	assert.NotNil(t, service.provider)
	assert.NotNil(t, service.logger)
}

func TestService_IsEnabled(t *testing.T) {
	provider := NewLocalProvider(
		LocalProviderConfig{
			Flags: map[string]FlagConfig{
				"feature-a": {
					Key:     "feature-a",
					Type:    "boolean",
					Enabled: true,
				},
				"feature-b": {
					Key:     "feature-b",
					Type:    "boolean",
					Enabled: false,
				},
			},
		},
		nil,
	)

	service := NewService(provider, newMockLogger())
	ctx := context.Background()
	userCtx := NewUserContext("test-user")

	// Test enabled flag
	enabled := service.IsEnabled(ctx, "feature-a", userCtx)
	assert.True(t, enabled)

	// Test disabled flag
	enabled = service.IsEnabled(ctx, "feature-b", userCtx)
	assert.False(t, enabled)

	// Test non-existent flag (should return false)
	enabled = service.IsEnabled(ctx, "non-existent", userCtx)
	assert.False(t, enabled)
}

func TestService_IsEnabledWithDefault(t *testing.T) {
	provider := NewLocalProvider(LocalProviderConfig{}, nil)
	service := NewService(provider, newMockLogger())
	ctx := context.Background()
	userCtx := NewUserContext("test-user")

	// Non-existent flag should return default
	enabled := service.IsEnabledWithDefault(ctx, "non-existent", userCtx, true)
	assert.True(t, enabled)

	enabled = service.IsEnabledWithDefault(ctx, "non-existent", userCtx, false)
	assert.False(t, enabled)
}

func TestService_GetString(t *testing.T) {
	provider := NewLocalProvider(
		LocalProviderConfig{
			Flags: map[string]FlagConfig{
				"theme": {
					Key:   "theme",
					Type:  "string",
					Value: "dark",
				},
			},
		},
		nil,
	)

	service := NewService(provider, newMockLogger())
	ctx := context.Background()
	userCtx := NewUserContext("test-user")

	// Test existing flag
	value := service.GetString(ctx, "theme", userCtx, "light")
	assert.Equal(t, "dark", value)

	// Test non-existent flag (should return default)
	value = service.GetString(ctx, "non-existent", userCtx, "light")
	assert.Equal(t, "light", value)
}

func TestService_GetInt(t *testing.T) {
	provider := NewLocalProvider(
		LocalProviderConfig{
			Flags: map[string]FlagConfig{
				"max-items": {
					Key:   "max-items",
					Type:  "number",
					Value: 100,
				},
			},
		},
		nil,
	)

	service := NewService(provider, newMockLogger())
	ctx := context.Background()
	userCtx := NewUserContext("test-user")

	// Test existing flag
	value := service.GetInt(ctx, "max-items", userCtx, 50)
	assert.Equal(t, 100, value)

	// Test non-existent flag (should return default)
	value = service.GetInt(ctx, "non-existent", userCtx, 50)
	assert.Equal(t, 50, value)
}

func TestService_GetFloat(t *testing.T) {
	provider := NewLocalProvider(
		LocalProviderConfig{
			Flags: map[string]FlagConfig{
				"tax-rate": {
					Key:   "tax-rate",
					Type:  "number",
					Value: 0.15,
				},
			},
		},
		nil,
	)

	service := NewService(provider, newMockLogger())
	ctx := context.Background()
	userCtx := NewUserContext("test-user")

	// Test existing flag
	value := service.GetFloat(ctx, "tax-rate", userCtx, 0.10)
	assert.Equal(t, 0.15, value)

	// Test non-existent flag (should return default)
	value = service.GetFloat(ctx, "non-existent", userCtx, 0.10)
	assert.Equal(t, 0.10, value)
}

func TestService_GetJSON(t *testing.T) {
	config := map[string]any{
		"timeout": 30,
		"retry":   3,
	}

	provider := NewLocalProvider(
		LocalProviderConfig{
			Flags: map[string]FlagConfig{
				"api-config": {
					Key:   "api-config",
					Type:  "json",
					Value: config,
				},
			},
		},
		nil,
	)

	service := NewService(provider, newMockLogger())
	ctx := context.Background()
	userCtx := NewUserContext("test-user")

	// Test existing flag
	value := service.GetJSON(ctx, "api-config", userCtx, nil)
	assert.Equal(t, config, value)

	// Test non-existent flag (should return default)
	defaultValue := map[string]any{"default": true}
	value = service.GetJSON(ctx, "non-existent", userCtx, defaultValue)
	assert.Equal(t, defaultValue, value)
}

func TestService_GetAllFlags(t *testing.T) {
	provider := NewLocalProvider(
		LocalProviderConfig{
			Flags: map[string]FlagConfig{
				"feature-a": {
					Key:     "feature-a",
					Type:    "boolean",
					Enabled: true,
				},
				"theme": {
					Key:   "theme",
					Type:  "string",
					Value: "dark",
				},
				"max-items": {
					Key:   "max-items",
					Type:  "number",
					Value: 100,
				},
			},
		},
		nil,
	)

	service := NewService(provider, newMockLogger())
	ctx := context.Background()
	userCtx := NewUserContext("test-user")

	flags, err := service.GetAllFlags(ctx, userCtx)
	require.NoError(t, err)
	assert.Len(t, flags, 3)
	assert.Equal(t, true, flags["feature-a"])
	assert.Equal(t, "dark", flags["theme"])
	assert.Equal(t, 100, flags["max-items"])
}

func TestNewUserContext(t *testing.T) {
	userCtx := NewUserContext("test-user")

	assert.Equal(t, "test-user", userCtx.UserID)
	assert.NotNil(t, userCtx.Attributes)
	assert.Empty(t, userCtx.Attributes)
}

func TestUserContext_Builders(t *testing.T) {
	userCtx := NewUserContext("test-user").
		WithEmail("user@example.com").
		WithName("Test User").
		WithGroups([]string{"admin", "users"}).
		WithAttribute("plan", "premium").
		WithIP("192.168.1.1").
		WithCountry("US")

	assert.Equal(t, "test-user", userCtx.UserID)
	assert.Equal(t, "user@example.com", userCtx.Email)
	assert.Equal(t, "Test User", userCtx.Name)
	assert.ElementsMatch(t, []string{"admin", "users"}, userCtx.Groups)
	assert.Equal(t, "premium", userCtx.Attributes["plan"])
	assert.Equal(t, "192.168.1.1", userCtx.IP)
	assert.Equal(t, "US", userCtx.Country)
}

func TestUserContext_GetAttribute(t *testing.T) {
	userCtx := NewUserContext("test-user").
		WithAttribute("plan", "premium").
		WithAttribute("level", 5)

	// Test existing attribute
	val, ok := userCtx.GetAttribute("plan")
	assert.True(t, ok)
	assert.Equal(t, "premium", val)

	// Test non-existent attribute
	val, ok = userCtx.GetAttribute("nonexistent")
	assert.False(t, ok)
	assert.Nil(t, val)
}

func TestUserContext_GetAttributeString(t *testing.T) {
	userCtx := NewUserContext("test-user").
		WithAttribute("plan", "premium").
		WithAttribute("level", 5)

	// Test valid string attribute
	val, err := userCtx.GetAttributeString("plan")
	assert.NoError(t, err)
	assert.Equal(t, "premium", val)

	// Test non-existent attribute
	_, err = userCtx.GetAttributeString("nonexistent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "attribute not found")

	// Test non-string attribute
	_, err = userCtx.GetAttributeString("level")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not a string")
}

func TestUserContext_HasGroup(t *testing.T) {
	userCtx := NewUserContext("test-user").
		WithGroups([]string{"admin", "users"})

	assert.True(t, userCtx.HasGroup("admin"))
	assert.True(t, userCtx.HasGroup("users"))
	assert.False(t, userCtx.HasGroup("guests"))
}

func TestService_WithTargeting(t *testing.T) {
	provider := NewLocalProvider(
		LocalProviderConfig{
			Flags: map[string]FlagConfig{
				"vip-feature": {
					Key:  "vip-feature",
					Type: "boolean",
					Targeting: []TargetingRule{
						{
							Attribute: "group",
							Operator:  "in",
							Values:    []string{"vip", "enterprise"},
							Value:     true,
						},
					},
				},
			},
		},
		nil,
	)

	service := NewService(provider, newMockLogger())
	ctx := context.Background()

	// VIP user should see the feature
	vipUser := NewUserContext("vip-user").WithGroups([]string{"vip"})
	enabled := service.IsEnabled(ctx, "vip-feature", vipUser)
	assert.True(t, enabled)

	// Regular user should not see the feature
	regularUser := NewUserContext("regular-user").WithGroups([]string{"users"})
	enabled = service.IsEnabled(ctx, "vip-feature", regularUser)
	assert.False(t, enabled)
}

func TestService_WithRollout(t *testing.T) {
	provider := NewLocalProvider(
		LocalProviderConfig{
			Flags: map[string]FlagConfig{
				"gradual-rollout": {
					Key:  "gradual-rollout",
					Type: "boolean",
					Rollout: &RolloutConfig{
						Percentage: 50,
						Attribute:  "user_id",
					},
				},
			},
		},
		nil,
	)

	service := NewService(provider, newMockLogger())
	ctx := context.Background()

	// Test with multiple users to verify consistent hashing
	results := make(map[bool]int)

	for i := range 100 {
		userCtx := NewUserContext(string(rune('a' + i)))
		enabled := service.IsEnabled(ctx, "gradual-rollout", userCtx)
		results[enabled]++
	}

	// Should have approximately 50/50 distribution
	// Allow 30-70% range due to small sample size
	enabledCount := results[true]
	assert.Greater(t, enabledCount, 30)
	assert.Less(t, enabledCount, 70)
}

func TestService_ErrorHandling(t *testing.T) {
	provider := NewLocalProvider(LocalProviderConfig{}, nil)
	service := NewService(provider, newMockLogger())
	ctx := context.Background()

	// Should handle nil user context gracefully
	enabled := service.IsEnabled(ctx, "test-flag", nil)
	assert.False(t, enabled)

	// Should handle empty flag key
	enabled = service.IsEnabled(ctx, "", NewUserContext("user"))
	assert.False(t, enabled)
}

// Benchmark tests.
func BenchmarkService_IsEnabled(b *testing.B) {
	provider := NewLocalProvider(
		LocalProviderConfig{
			Flags: map[string]FlagConfig{
				"test-flag": {
					Key:     "test-flag",
					Type:    "boolean",
					Enabled: true,
				},
			},
		},
		nil,
	)

	service := NewService(provider, newMockLogger())
	ctx := context.Background()
	userCtx := NewUserContext("test-user")

	for b.Loop() {
		service.IsEnabled(ctx, "test-flag", userCtx)
	}
}

func BenchmarkService_GetString(b *testing.B) {
	provider := NewLocalProvider(
		LocalProviderConfig{
			Flags: map[string]FlagConfig{
				"theme": {
					Key:   "theme",
					Type:  "string",
					Value: "dark",
				},
			},
		},
		nil,
	)

	service := NewService(provider, newMockLogger())
	ctx := context.Background()
	userCtx := NewUserContext("test-user")

	for b.Loop() {
		service.GetString(ctx, "theme", userCtx, "light")
	}
}

func BenchmarkService_WithTargeting(b *testing.B) {
	provider := NewLocalProvider(
		LocalProviderConfig{
			Flags: map[string]FlagConfig{
				"vip-feature": {
					Key:  "vip-feature",
					Type: "boolean",
					Targeting: []TargetingRule{
						{
							Attribute: "group",
							Operator:  "in",
							Values:    []string{"vip"},
							Value:     true,
						},
					},
				},
			},
		},
		nil,
	)

	service := NewService(provider, newMockLogger())
	ctx := context.Background()
	userCtx := NewUserContext("test-user").WithGroups([]string{"vip"})

	for b.Loop() {
		service.IsEnabled(ctx, "vip-feature", userCtx)
	}
}

func BenchmarkUserContext_Creation(b *testing.B) {

	for b.Loop() {
		NewUserContext("test-user").
			WithEmail("user@example.com").
			WithName("Test User").
			WithGroups([]string{"admin"}).
			WithAttribute("plan", "premium")
	}
}
