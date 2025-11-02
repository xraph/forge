package providers

import (
	"context"
	"fmt"
	"net/http"
	"sync"

	unleash "github.com/Unleash/unleash-client-go/v4"
	unleashContext "github.com/Unleash/unleash-client-go/v4/context"
	"github.com/xraph/forge"
)

// UnleashConfig holds configuration for Unleash
type UnleashConfig struct {
	// URL is the Unleash API URL
	URL string `yaml:"url" json:"url"`

	// APIToken for authentication
	APIToken string `yaml:"api_token" json:"api_token"`

	// AppName identifies your application
	AppName string `yaml:"app_name" json:"app_name"`

	// InstanceID uniquely identifies this instance (optional)
	InstanceID string `yaml:"instance_id" json:"instance_id"`

	// Environment name (optional)
	Environment string `yaml:"environment" json:"environment"`

	// ProjectName for multi-project setups (optional)
	ProjectName string `yaml:"project_name" json:"project_name"`

	// CustomHeaders for additional HTTP headers (optional)
	CustomHeaders map[string]string `yaml:"custom_headers" json:"custom_headers"`

	// DisableMetrics disables metrics reporting
	DisableMetrics bool `yaml:"disable_metrics" json:"disable_metrics"`
}

// UnleashProvider implements feature flags using Unleash
type UnleashProvider struct {
	config UnleashConfig
	client *unleash.Client
	logger forge.Logger
	mu     sync.RWMutex
	ready  bool
}

// NewUnleashProvider creates a new Unleash provider
func NewUnleashProvider(config UnleashConfig, logger forge.Logger) (*UnleashProvider, error) {
	if config.URL == "" {
		return nil, fmt.Errorf("unleash url is required")
	}
	if config.APIToken == "" {
		return nil, fmt.Errorf("unleash api_token is required")
	}
	if config.AppName == "" {
		return nil, fmt.Errorf("unleash app_name is required")
	}

	p := &UnleashProvider{
		config: config,
		logger: logger,
	}

	// Build configuration
	unleashConfig := []unleash.ConfigOption{
		unleash.WithUrl(config.URL),
		unleash.WithAppName(config.AppName),
	}

	if config.APIToken != "" {
		unleashConfig = append(unleashConfig, unleash.WithCustomHeaders(http.Header{
			"Authorization": []string{config.APIToken},
		}))
	}

	if config.InstanceID != "" {
		unleashConfig = append(unleashConfig, unleash.WithInstanceId(config.InstanceID))
	}

	if config.Environment != "" {
		unleashConfig = append(unleashConfig, unleash.WithEnvironment(config.Environment))
	}

	if config.ProjectName != "" {
		unleashConfig = append(unleashConfig, unleash.WithProjectName(config.ProjectName))
	}

	if config.DisableMetrics {
		unleashConfig = append(unleashConfig, unleash.WithDisableMetrics(true))
	}

	// Create client
	client, err := unleash.NewClient(unleashConfig...)
	if err != nil {
		return nil, fmt.Errorf("failed to create Unleash client: %w", err)
	}

	p.client = client

	// Wait for client to be ready
	<-client.Ready()
	p.ready = true

	logger.Info("Unleash provider initialized")

	return p, nil
}

// Name returns the provider name
func (p *UnleashProvider) Name() string {
	return "unleash"
}

// IsEnabled checks if a feature flag is enabled
func (p *UnleashProvider) IsEnabled(ctx context.Context, key string, userCtx *UserContext) bool {
	if !p.ready {
		p.logger.Warn("Unleash client not ready")
		return false
	}

	unleashCtx := p.buildContext(userCtx)
	return p.client.IsEnabled(key, unleash.WithContext(*unleashCtx))
}

// GetValue returns the value of a feature flag
func (p *UnleashProvider) GetValue(ctx context.Context, key string, userCtx *UserContext, defaultValue interface{}) interface{} {
	if !p.ready {
		p.logger.Warn("Unleash client not ready")
		return defaultValue
	}

	unleashCtx := p.buildContext(userCtx)

	// Get variant
	variant := p.client.GetVariant(key)
	if variant.Enabled && variant.Name != "" {
		// Return variant name
		return variant.Name
	}

	// Check if feature is simply enabled/disabled
	if p.client.IsEnabled(key, unleash.WithContext(*unleashCtx)) {
		return true
	}

	return defaultValue
}

// Evaluate evaluates a feature flag
func (p *UnleashProvider) Evaluate(ctx context.Context, key string, userCtx *UserContext) (interface{}, error) {
	if !p.ready {
		return nil, fmt.Errorf("Unleash client not ready")
	}

	unleashCtx := p.buildContext(userCtx)

	// Check if feature is enabled
	enabled := p.client.IsEnabled(key, unleash.WithContext(*unleashCtx))

	// Try to get variant
	variant := p.client.GetVariant(key)
	if variant.Enabled && variant.Name != "" {
		return variant.Name, nil
	}

	return enabled, nil
}

// buildContext converts UserContext to Unleash context
func (p *UnleashProvider) buildContext(userCtx *UserContext) *unleashContext.Context {
	if userCtx == nil {
		return &unleashContext.Context{}
	}

	ctx := unleashContext.Context{
		UserId:        userCtx.UserID,
		RemoteAddress: userCtx.IP,
		Properties:    make(map[string]string),
	}

	if userCtx.Email != "" {
		ctx.Properties["email"] = userCtx.Email
	}

	if userCtx.Name != "" {
		ctx.Properties["name"] = userCtx.Name
	}

	if userCtx.Country != "" {
		ctx.Properties["country"] = userCtx.Country
	}

	if len(userCtx.Groups) > 0 {
		// Store first group or join them
		if len(userCtx.Groups) > 0 {
			ctx.Properties["group"] = userCtx.Groups[0]
		}
		// Store all groups as comma-separated
		groupsStr := ""
		for i, g := range userCtx.Groups {
			if i > 0 {
				groupsStr += ","
			}
			groupsStr += g
		}
		ctx.Properties["groups"] = groupsStr
	}

	// Add custom attributes
	for k, v := range userCtx.Attributes {
		ctx.Properties[k] = fmt.Sprintf("%v", v)
	}

	return &ctx
}

// GetAllFlags returns all feature flags
func (p *UnleashProvider) GetAllFlags(ctx context.Context, userCtx *UserContext) (map[string]interface{}, error) {
	if !p.ready {
		return nil, fmt.Errorf("Unleash client not ready")
	}

	unleashCtx := p.buildContext(userCtx)
	features := p.client.ListFeatures()

	result := make(map[string]interface{})
	for _, feature := range features {
		// Check if enabled
		enabled := p.client.IsEnabled(feature.Name, unleash.WithContext(*unleashCtx))

		// Get variant if available
		variant := p.client.GetVariant(feature.Name)
		if variant.Enabled && variant.Name != "" {
			result[feature.Name] = variant.Name
		} else {
			result[feature.Name] = enabled
		}
	}

	return result, nil
}

// Refresh forces a refresh of flags
func (p *UnleashProvider) Refresh(ctx context.Context) error {
	if !p.ready || p.client == nil {
		return fmt.Errorf("Unleash client not ready")
	}

	// Unleash SDK handles flag updates automatically via polling
	// This is a no-op but provided for interface compatibility
	p.logger.Debug("Unleash handles flag updates automatically")
	return nil
}

// Start starts the provider
func (p *UnleashProvider) Start(ctx context.Context) error {
	if p.client == nil {
		return fmt.Errorf("Unleash client not initialized")
	}

	// Client is already started in constructor
	p.mu.Lock()
	p.ready = true
	p.mu.Unlock()

	p.logger.Info("Unleash provider started")
	return nil
}

// Stop stops the provider
func (p *UnleashProvider) Stop(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.client != nil {
		p.client.Close()
	}

	p.ready = false
	p.logger.Info("Unleash provider stopped")
	return nil
}

// Health checks the provider health
func (p *UnleashProvider) Health(ctx context.Context) error {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.client == nil {
		return fmt.Errorf("Unleash client not initialized")
	}

	if !p.ready {
		return fmt.Errorf("Unleash client not ready")
	}

	// Check if we have received any features
	features := p.client.ListFeatures()
	if len(features) == 0 {
		p.logger.Warn("No features loaded from Unleash")
	}

	return nil
}

// GetVariant gets a feature variant
func (p *UnleashProvider) GetVariant(ctx context.Context, key string, userCtx *UserContext) (string, map[string]interface{}, error) {
	if !p.ready {
		return "", nil, fmt.Errorf("Unleash client not ready")
	}

	variant := p.client.GetVariant(key)

	if !variant.Enabled {
		return "", nil, fmt.Errorf("variant not enabled for flag: %s", key)
	}

	payload := make(map[string]interface{})
	// Note: Unleash v4 API Payload is a struct, not a pointer
	payload["type"] = variant.Payload.Type
	payload["value"] = variant.Payload.Value

	return variant.Name, payload, nil
}

// ListFeatures lists all available features
func (p *UnleashProvider) ListFeatures() []string {
	if !p.ready || p.client == nil {
		return []string{}
	}

	features := p.client.ListFeatures()
	names := make([]string, len(features))
	for i, f := range features {
		names[i] = f.Name
	}

	return names
}
