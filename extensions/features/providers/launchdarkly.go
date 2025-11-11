package providers

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/launchdarkly/go-sdk-common/v3/ldcontext"
	"github.com/launchdarkly/go-sdk-common/v3/ldvalue"
	ld "github.com/launchdarkly/go-server-sdk/v7"
	"github.com/launchdarkly/go-server-sdk/v7/ldcomponents"
	"github.com/xraph/forge"
	"github.com/xraph/forge/errors"
)

// LaunchDarklyConfig holds configuration for LaunchDarkly.
type LaunchDarklyConfig struct {
	// SDKKey is your LaunchDarkly SDK key
	SDKKey string `json:"sdk_key" yaml:"sdk_key"`

	// BaseURI for custom endpoints (optional)
	BaseURI string `json:"base_uri" yaml:"base_uri"`

	// StreamURI for streaming connections (optional)
	StreamURI string `json:"stream_uri" yaml:"stream_uri"`

	// EventsURI for analytics events (optional)
	EventsURI string `json:"events_uri" yaml:"events_uri"`

	// StartWaitTime is the maximum time to wait for initialization
	StartWaitTime time.Duration `json:"start_wait_time" yaml:"start_wait_time"`

	// Offline mode for testing
	Offline bool `json:"offline" yaml:"offline"`

	// EnableEvents controls whether to send analytics events
	EnableEvents bool `json:"enable_events" yaml:"enable_events"`
}

// LaunchDarklyProvider implements feature flags using LaunchDarkly.
type LaunchDarklyProvider struct {
	config LaunchDarklyConfig
	client *ld.LDClient
	logger forge.Logger
	mu     sync.RWMutex
	ready  bool
}

// NewLaunchDarklyProvider creates a new LaunchDarkly provider.
func NewLaunchDarklyProvider(config LaunchDarklyConfig, logger forge.Logger) (*LaunchDarklyProvider, error) {
	if config.SDKKey == "" {
		return nil, errors.New("launchdarkly sdk_key is required")
	}

	if config.StartWaitTime == 0 {
		config.StartWaitTime = 5 * time.Second
	}

	p := &LaunchDarklyProvider{
		config: config,
		logger: logger,
	}

	// Build configuration
	ldConfig := ld.Config{}

	if config.Offline {
		ldConfig.Offline = true
	}

	if !config.EnableEvents {
		ldConfig.Events = ldcomponents.NoEvents()
	}

	// Service endpoints configuration removed in v7 - use default URLs

	// Create client
	client, err := ld.MakeCustomClient(config.SDKKey, ldConfig, config.StartWaitTime)
	if err != nil {
		return nil, fmt.Errorf("failed to create LaunchDarkly client: %w", err)
	}

	p.client = client
	p.ready = true

	logger.Info("LaunchDarkly provider initialized")

	return p, nil
}

// Name returns the provider name.
func (p *LaunchDarklyProvider) Name() string {
	return "launchdarkly"
}

// IsEnabled checks if a feature flag is enabled.
func (p *LaunchDarklyProvider) IsEnabled(ctx context.Context, key string, userCtx *UserContext) bool {
	if !p.ready {
		p.logger.Warn("LaunchDarkly client not ready")

		return false
	}

	ldCtx := p.buildContext(userCtx)

	value, err := p.client.BoolVariation(key, ldCtx, false)
	if err != nil {
		p.logger.Error("failed to evaluate flag",
			forge.String("key", key),
			forge.String("error", err.Error()))

		return false
	}

	return value
}

// GetValue returns the value of a feature flag.
func (p *LaunchDarklyProvider) GetValue(ctx context.Context, key string, userCtx *UserContext, defaultValue any) any {
	if !p.ready {
		p.logger.Warn("LaunchDarkly client not ready")

		return defaultValue
	}

	ldCtx := p.buildContext(userCtx)
	ldDefaultValue := ldvalue.CopyArbitraryValue(defaultValue)

	value, err := p.client.JSONVariation(key, ldCtx, ldDefaultValue)
	if err != nil {
		p.logger.Error("failed to evaluate flag",
			forge.String("key", key),
			forge.String("error", err.Error()))

		return defaultValue
	}

	return value.AsArbitraryValue()
}

// Evaluate evaluates a feature flag.
func (p *LaunchDarklyProvider) Evaluate(ctx context.Context, key string, userCtx *UserContext) (any, error) {
	if !p.ready {
		return nil, errors.New("LaunchDarkly client not ready")
	}

	ldCtx := p.buildContext(userCtx)
	value, _ := p.client.JSONVariation(key, ldCtx, ldvalue.Null())

	if value.IsNull() {
		return nil, fmt.Errorf("flag not found: %s", key)
	}

	return value.AsArbitraryValue(), nil
}

// buildContext converts UserContext to LaunchDarkly context.
func (p *LaunchDarklyProvider) buildContext(userCtx *UserContext) ldcontext.Context {
	if userCtx == nil {
		return ldcontext.New("anonymous")
	}

	builder := ldcontext.NewBuilder(userCtx.UserID)

	if userCtx.Name != "" {
		builder.Name(userCtx.Name)
	}

	if userCtx.Email != "" {
		builder.SetString("email", userCtx.Email)
	}

	if userCtx.IP != "" {
		builder.SetString("ip", userCtx.IP)
	}

	if userCtx.Country != "" {
		builder.SetString("country", userCtx.Country)
	}

	if len(userCtx.Groups) > 0 {
		values := make([]ldvalue.Value, len(userCtx.Groups))
		for i, group := range userCtx.Groups {
			values[i] = ldvalue.String(group)
		}

		builder.SetValue("groups", ldvalue.ArrayOf(values...))
	}

	// Add custom attributes
	for k, v := range userCtx.Attributes {
		builder.SetValue(k, ldvalue.CopyArbitraryValue(v))
	}

	return builder.Build()
}

// GetAllFlags returns all feature flags.
func (p *LaunchDarklyProvider) GetAllFlags(ctx context.Context, userCtx *UserContext) (map[string]any, error) {
	if !p.ready {
		return nil, errors.New("LaunchDarkly client not ready")
	}

	ldCtx := p.buildContext(userCtx)
	allFlags := p.client.AllFlagsState(ldCtx)

	result := make(map[string]any)
	for key, value := range allFlags.ToValuesMap() {
		result[key] = value.AsArbitraryValue()
	}

	return result, nil
}

// Refresh forces a refresh of flags (LaunchDarkly handles this automatically).
func (p *LaunchDarklyProvider) Refresh(ctx context.Context) error {
	if !p.ready {
		return errors.New("LaunchDarkly client not ready")
	}

	// LaunchDarkly SDK handles flag updates automatically via streaming
	// This is a no-op but provided for interface compatibility
	p.logger.Debug("LaunchDarkly handles flag updates automatically")

	return nil
}

// Start starts the provider (LaunchDarkly client already initialized).
func (p *LaunchDarklyProvider) Start(ctx context.Context) error {
	if p.client == nil {
		return errors.New("LaunchDarkly client not initialized")
	}

	// Wait for client to be ready
	if !p.client.Initialized() {
		p.logger.Info("waiting for LaunchDarkly client to initialize")

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(p.config.StartWaitTime):
			if !p.client.Initialized() {
				return fmt.Errorf("LaunchDarkly client failed to initialize within %s", p.config.StartWaitTime)
			}
		}
	}

	p.mu.Lock()
	p.ready = true
	p.mu.Unlock()

	p.logger.Info("LaunchDarkly provider started")

	return nil
}

// Stop stops the provider.
func (p *LaunchDarklyProvider) Stop(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.client != nil {
		if err := p.client.Close(); err != nil {
			p.logger.Error("error closing LaunchDarkly client",
				forge.String("error", err.Error()))

			return err
		}
	}

	p.ready = false
	p.logger.Info("LaunchDarkly provider stopped")

	return nil
}

// Health checks the provider health.
func (p *LaunchDarklyProvider) Health(ctx context.Context) error {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.client == nil {
		return errors.New("LaunchDarkly client not initialized")
	}

	if !p.ready || !p.client.Initialized() {
		return errors.New("LaunchDarkly client not ready")
	}

	return nil
}

// Track sends an analytics event to LaunchDarkly.
func (p *LaunchDarklyProvider) Track(ctx context.Context, userID string, event string, data any) error {
	if !p.ready {
		return errors.New("LaunchDarkly client not ready")
	}

	ldCtx := ldcontext.New(userID)

	// TrackEvent in v7 doesn't take data parameter directly
	p.client.TrackEvent(event, ldCtx)

	return nil
}

// Identify updates user attributes in LaunchDarkly.
func (p *LaunchDarklyProvider) Identify(ctx context.Context, userCtx *UserContext) error {
	if !p.ready {
		return errors.New("LaunchDarkly client not ready")
	}

	ldCtx := p.buildContext(userCtx)

	// Trigger an evaluation to register the user context
	_, _ = p.client.BoolVariation("__identify__", ldCtx, false)

	return nil
}

// Flush flushes pending analytics events.
func (p *LaunchDarklyProvider) Flush(ctx context.Context) error {
	if !p.ready || p.client == nil {
		return errors.New("LaunchDarkly client not ready")
	}

	p.client.Flush()

	return nil
}

// GetBoolVariation gets a boolean flag with variation details.
func (p *LaunchDarklyProvider) GetBoolVariation(ctx context.Context, key string, userCtx *UserContext, defaultValue bool) (bool, error) {
	if !p.ready {
		return defaultValue, errors.New("LaunchDarkly client not ready")
	}

	ldCtx := p.buildContext(userCtx)

	value, err := p.client.BoolVariation(key, ldCtx, defaultValue)
	if err != nil {
		return defaultValue, err
	}

	return value, nil
}

// GetStringVariation gets a string flag.
func (p *LaunchDarklyProvider) GetStringVariation(ctx context.Context, key string, userCtx *UserContext, defaultValue string) (string, error) {
	if !p.ready {
		return defaultValue, errors.New("LaunchDarkly client not ready")
	}

	ldCtx := p.buildContext(userCtx)

	value, err := p.client.StringVariation(key, ldCtx, defaultValue)
	if err != nil {
		return defaultValue, err
	}

	return value, nil
}

// GetIntVariation gets an integer flag.
func (p *LaunchDarklyProvider) GetIntVariation(ctx context.Context, key string, userCtx *UserContext, defaultValue int) (int, error) {
	if !p.ready {
		return defaultValue, errors.New("LaunchDarkly client not ready")
	}

	ldCtx := p.buildContext(userCtx)

	value, err := p.client.IntVariation(key, ldCtx, defaultValue)
	if err != nil {
		return defaultValue, err
	}

	return value, nil
}

// GetFloatVariation gets a float flag.
func (p *LaunchDarklyProvider) GetFloatVariation(ctx context.Context, key string, userCtx *UserContext, defaultValue float64) (float64, error) {
	if !p.ready {
		return defaultValue, errors.New("LaunchDarkly client not ready")
	}

	ldCtx := p.buildContext(userCtx)

	value, err := p.client.Float64Variation(key, ldCtx, defaultValue)
	if err != nil {
		return defaultValue, err
	}

	return value, nil
}
