package providers

import (
	"context"
	"fmt"
	"sync"

	flagsmith "github.com/Flagsmith/flagsmith-go-client/v3"
	"github.com/xraph/forge"
	"github.com/xraph/forge/internal/errors"
)

// FlagsmithConfig holds configuration for Flagsmith.
type FlagsmithConfig struct {
	// EnvironmentKey is your Flagsmith environment key
	EnvironmentKey string `json:"environment_key" yaml:"environment_key"`

	// APIURL is the Flagsmith API URL (default: https://edge.api.flagsmith.com/api/v1/)
	APIURL string `json:"api_url" yaml:"api_url"`

	// EnableLocalEvaluation enables local flag evaluation
	EnableLocalEvaluation bool `json:"enable_local_evaluation" yaml:"enable_local_evaluation"`

	// EnableAnalytics enables analytics tracking
	EnableAnalytics bool `json:"enable_analytics" yaml:"enable_analytics"`

	// CustomHeaders for additional HTTP headers
	CustomHeaders map[string]string `json:"custom_headers" yaml:"custom_headers"`

	// RequestTimeout in seconds
	RequestTimeout int `json:"request_timeout" yaml:"request_timeout"`

	// Retries for failed requests
	Retries int `json:"retries" yaml:"retries"`
}

// FlagsmithProvider implements feature flags using Flagsmith.
type FlagsmithProvider struct {
	config FlagsmithConfig
	client *flagsmith.Client
	logger forge.Logger
	mu     sync.RWMutex
	ready  bool
}

// NewFlagsmithProvider creates a new Flagsmith provider.
func NewFlagsmithProvider(config FlagsmithConfig, logger forge.Logger) (*FlagsmithProvider, error) {
	if config.EnvironmentKey == "" {
		return nil, errors.New("flagsmith environment_key is required")
	}

	if config.APIURL == "" {
		config.APIURL = "https://edge.api.flagsmith.com/api/v1/"
	}

	if config.RequestTimeout == 0 {
		config.RequestTimeout = 10 // Default 10 seconds
	}

	if config.Retries == 0 {
		config.Retries = 3
	}

	p := &FlagsmithProvider{
		config: config,
		logger: logger,
	}

	// Build configuration options
	opts := []flagsmith.Option{}

	if config.APIURL != "" {
		opts = append(opts, flagsmith.WithBaseURL(config.APIURL))
	}

	if config.EnableLocalEvaluation {
		opts = append(opts, flagsmith.WithLocalEvaluation(context.Background()))
	}

	// Create client
	client := flagsmith.NewClient(config.EnvironmentKey, opts...)
	p.client = client

	p.ready = true

	logger.Info("Flagsmith provider initialized")

	return p, nil
}

// Name returns the provider name.
func (p *FlagsmithProvider) Name() string {
	return "flagsmith"
}

// IsEnabled checks if a feature flag is enabled.
func (p *FlagsmithProvider) IsEnabled(ctx context.Context, key string, userCtx *UserContext) bool {
	if !p.ready {
		p.logger.Warn("Flagsmith client not ready")

		return false
	}

	var (
		flags flagsmith.Flags
		err   error
	)

	if userCtx != nil && userCtx.UserID != "" {
		// Get flags for specific user
		traits := p.buildTraits(userCtx)
		flags, err = p.client.GetIdentityFlags(ctx, userCtx.UserID, traits)
	} else {
		// Get environment flags
		flags, err = p.client.GetEnvironmentFlags(ctx)
	}

	if err != nil {
		p.logger.Error("failed to get flags",
			forge.String("key", key),
			forge.String("error", err.Error()))

		return false
	}

	flag, err := flags.GetFlag(key)
	if err != nil {
		p.logger.Debug("flag not found", forge.String("key", key))

		return false
	}

	return flag.Enabled
}

// GetValue returns the value of a feature flag.
func (p *FlagsmithProvider) GetValue(ctx context.Context, key string, userCtx *UserContext, defaultValue any) any {
	if !p.ready {
		p.logger.Warn("Flagsmith client not ready")

		return defaultValue
	}

	var (
		flags flagsmith.Flags
		err   error
	)

	if userCtx != nil && userCtx.UserID != "" {
		traits := p.buildTraits(userCtx)
		flags, err = p.client.GetIdentityFlags(ctx, userCtx.UserID, traits)
	} else {
		flags, err = p.client.GetEnvironmentFlags(ctx)
	}

	if err != nil {
		p.logger.Error("failed to get flags",
			forge.String("key", key),
			forge.String("error", err.Error()))

		return defaultValue
	}

	flag, err := flags.GetFlag(key)
	if err != nil {
		return defaultValue
	}

	if !flag.Enabled {
		return defaultValue
	}

	// Return the flag value if present, otherwise return enabled status
	if flag.Value != nil {
		return flag.Value
	}

	return flag.Enabled
}

// Evaluate evaluates a feature flag.
func (p *FlagsmithProvider) Evaluate(ctx context.Context, key string, userCtx *UserContext) (any, error) {
	if !p.ready {
		return nil, errors.New("Flagsmith client not ready")
	}

	var (
		flags flagsmith.Flags
		err   error
	)

	if userCtx != nil && userCtx.UserID != "" {
		traits := p.buildTraits(userCtx)
		flags, err = p.client.GetIdentityFlags(ctx, userCtx.UserID, traits)
	} else {
		flags, err = p.client.GetEnvironmentFlags(ctx)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to get flags: %w", err)
	}

	flag, err := flags.GetFlag(key)
	if err != nil {
		return nil, fmt.Errorf("flag not found: %s", key)
	}

	if flag.Value != nil {
		return flag.Value, nil
	}

	return flag.Enabled, nil
}

// buildTraits converts UserContext to Flagsmith traits.
func (p *FlagsmithProvider) buildTraits(userCtx *UserContext) []*flagsmith.Trait {
	if userCtx == nil {
		return nil
	}

	traits := make([]*flagsmith.Trait, 0)

	if userCtx.Email != "" {
		traits = append(traits, &flagsmith.Trait{
			TraitKey:   "email",
			TraitValue: userCtx.Email,
		})
	}

	if userCtx.Name != "" {
		traits = append(traits, &flagsmith.Trait{
			TraitKey:   "name",
			TraitValue: userCtx.Name,
		})
	}

	if userCtx.IP != "" {
		traits = append(traits, &flagsmith.Trait{
			TraitKey:   "ip",
			TraitValue: userCtx.IP,
		})
	}

	if userCtx.Country != "" {
		traits = append(traits, &flagsmith.Trait{
			TraitKey:   "country",
			TraitValue: userCtx.Country,
		})
	}

	if len(userCtx.Groups) > 0 {
		// Store first group
		if len(userCtx.Groups) > 0 {
			traits = append(traits, &flagsmith.Trait{
				TraitKey:   "group",
				TraitValue: userCtx.Groups[0],
			})
		}
	}

	// Add custom attributes
	for k, v := range userCtx.Attributes {
		traits = append(traits, &flagsmith.Trait{
			TraitKey:   k,
			TraitValue: v,
		})
	}

	return traits
}

// GetAllFlags returns all feature flags.
func (p *FlagsmithProvider) GetAllFlags(ctx context.Context, userCtx *UserContext) (map[string]any, error) {
	if !p.ready {
		return nil, errors.New("Flagsmith client not ready")
	}

	// Flagsmith v3 API doesn't support listing all flags in one call
	// Each flag needs to be evaluated individually
	// For now, return empty map
	result := make(map[string]any)

	return result, nil
}

// Refresh forces a refresh of flags (if local evaluation is enabled).
func (p *FlagsmithProvider) Refresh(ctx context.Context) error {
	if !p.ready || p.client == nil {
		return errors.New("Flagsmith client not ready")
	}

	if p.config.EnableLocalEvaluation {
		// Update local environment
		err := p.client.UpdateEnvironment(ctx)
		if err != nil {
			return fmt.Errorf("failed to update environment: %w", err)
		}

		p.logger.Info("Flagsmith environment refreshed")
	}

	return nil
}

// Start starts the provider.
func (p *FlagsmithProvider) Start(ctx context.Context) error {
	if p.client == nil {
		return errors.New("Flagsmith client not initialized")
	}

	// Update environment if local evaluation is enabled
	if p.config.EnableLocalEvaluation {
		err := p.client.UpdateEnvironment(ctx)
		if err != nil {
			p.logger.Warn("failed to update environment on start",
				forge.String("error", err.Error()))
		}
	}

	p.mu.Lock()
	p.ready = true
	p.mu.Unlock()

	p.logger.Info("Flagsmith provider started")

	return nil
}

// Stop stops the provider.
func (p *FlagsmithProvider) Stop(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.ready = false
	p.logger.Info("Flagsmith provider stopped")

	return nil
}

// Health checks the provider health.
func (p *FlagsmithProvider) Health(ctx context.Context) error {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.client == nil {
		return errors.New("Flagsmith client not initialized")
	}

	if !p.ready {
		return errors.New("Flagsmith client not ready")
	}

	// Try to get environment flags as a health check
	_, err := p.client.GetEnvironmentFlags(ctx)
	if err != nil {
		return fmt.Errorf("health check failed: %w", err)
	}

	return nil
}

// GetTrait gets a user trait.
func (p *FlagsmithProvider) GetTrait(ctx context.Context, userID string, traitKey string) (any, error) {
	if !p.ready || p.client == nil {
		return nil, errors.New("Flagsmith client not ready")
	}

	// Traits API requires different client method in v3
	return nil, errors.New("GetTrait not implemented - use flag evaluation instead")
}

// SetTrait sets a user trait.
func (p *FlagsmithProvider) SetTrait(ctx context.Context, userID string, traitKey string, traitValue any) error {
	if !p.ready || p.client == nil {
		return errors.New("Flagsmith client not ready")
	}

	trait := &flagsmith.Trait{
		TraitKey:   traitKey,
		TraitValue: traitValue,
	}

	// This will update the trait in Flagsmith
	_, err := p.client.GetIdentityFlags(ctx, userID, []*flagsmith.Trait{trait})
	if err != nil {
		return fmt.Errorf("failed to set trait: %w", err)
	}

	return nil
}
