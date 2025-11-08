package features

import (
	"context"
	"fmt"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/features/providers"
)

// NewLocalProvider creates a new local in-memory provider.
func NewLocalProvider(config LocalProviderConfig, defaults map[string]any) Provider {
	// Convert LocalProviderConfig from features package to providers package
	providerConfig := providers.LocalProviderConfig{
		Flags: make(map[string]providers.FlagConfig),
	}

	for k, v := range config.Flags {
		providerConfig.Flags[k] = providers.FlagConfig{
			Key:         v.Key,
			Name:        v.Name,
			Description: v.Description,
			Type:        v.Type,
			Enabled:     v.Enabled,
			Value:       v.Value,
			Targeting:   convertTargetingRules(v.Targeting),
			Rollout:     convertRolloutConfig(v.Rollout),
		}
	}

	localProvider := providers.NewLocalProvider(providerConfig, defaults)

	return &localProviderAdapter{provider: localProvider}
}

// convertTargetingRules converts targeting rules.
func convertTargetingRules(rules []TargetingRule) []providers.TargetingRule {
	if rules == nil {
		return nil
	}

	result := make([]providers.TargetingRule, len(rules))
	for i, r := range rules {
		result[i] = providers.TargetingRule{
			Attribute: r.Attribute,
			Operator:  r.Operator,
			Values:    r.Values,
			Value:     r.Value,
		}
	}

	return result
}

// convertRolloutConfig converts rollout config.
func convertRolloutConfig(rollout *RolloutConfig) *providers.RolloutConfig {
	if rollout == nil {
		return nil
	}

	return &providers.RolloutConfig{
		Percentage: rollout.Percentage,
		Attribute:  rollout.Attribute,
	}
}

// NewLaunchDarklyProvider creates a new LaunchDarkly provider
// NOTE: Temporarily disabled until dependencies are resolved.
func NewLaunchDarklyProvider(config LaunchDarklyConfig, defaults map[string]any) (Provider, error) {
	return nil, errors.New("launchdarkly provider not yet available - dependencies not resolved")
}

// NewUnleashProvider creates a new Unleash provider
// NOTE: Temporarily disabled until dependencies are resolved.
func NewUnleashProvider(config UnleashConfig, defaults map[string]any) (Provider, error) {
	return nil, errors.New("unleash provider not yet available - dependencies not resolved")
}

// NewFlagsmithProvider creates a new Flagsmith provider
// NOTE: Temporarily disabled until dependencies are resolved.
func NewFlagsmithProvider(config FlagsmithConfig, defaults map[string]any) (Provider, error) {
	return nil, errors.New("flagsmith provider not yet available - dependencies not resolved")
}

// NewPostHogProvider creates a new PostHog provider
// NOTE: Temporarily disabled until dependencies are resolved.
func NewPostHogProvider(config PostHogConfig, defaults map[string]any) (Provider, error) {
	return nil, errors.New("posthog provider not yet available - dependencies not resolved")
}

// convertUserContext converts features.UserContext to providers.UserContext.
func convertUserContext(uctx *UserContext) *providers.UserContext {
	if uctx == nil {
		return nil
	}

	return &providers.UserContext{
		UserID:     uctx.UserID,
		Email:      uctx.Email,
		Name:       uctx.Name,
		Groups:     uctx.Groups,
		Attributes: uctx.Attributes,
		IP:         uctx.IP,
		Country:    uctx.Country,
	}
}

// noopSugarLogger implements logger.SugarLogger.
type noopSugarLogger struct{}

func (l *noopSugarLogger) Debugw(msg string, keysAndValues ...any) {}
func (l *noopSugarLogger) Infow(msg string, keysAndValues ...any)  {}
func (l *noopSugarLogger) Warnw(msg string, keysAndValues ...any)  {}
func (l *noopSugarLogger) Errorw(msg string, keysAndValues ...any) {}
func (l *noopSugarLogger) Fatalw(msg string, keysAndValues ...any) {}
func (l *noopSugarLogger) With(args ...any) forge.SugarLogger      { return l }

// noopLogger is a minimal logger implementation for providers.
type noopLogger struct{}

func (l *noopLogger) Debug(msg string, fields ...forge.Field)      {}
func (l *noopLogger) Info(msg string, fields ...forge.Field)       {}
func (l *noopLogger) Warn(msg string, fields ...forge.Field)       {}
func (l *noopLogger) Error(msg string, fields ...forge.Field)      {}
func (l *noopLogger) Fatal(msg string, fields ...forge.Field)      {}
func (l *noopLogger) Debugf(template string, args ...any)          {}
func (l *noopLogger) Infof(template string, args ...any)           {}
func (l *noopLogger) Warnf(template string, args ...any)           {}
func (l *noopLogger) Errorf(template string, args ...any)          {}
func (l *noopLogger) Fatalf(template string, args ...any)          {}
func (l *noopLogger) With(fields ...forge.Field) forge.Logger      { return l }
func (l *noopLogger) WithContext(ctx context.Context) forge.Logger { return l }
func (l *noopLogger) Named(name string) forge.Logger               { return l }
func (l *noopLogger) Sugar() forge.SugarLogger                     { return &noopSugarLogger{} }
func (l *noopLogger) Sync() error                                  { return nil }

// Adapter implementations

// localProviderAdapter adapts LocalProvider to Provider interface.
type localProviderAdapter struct {
	provider *providers.LocalProvider
}

func (a *localProviderAdapter) Name() string {
	return a.provider.Name()
}

func (a *localProviderAdapter) Initialize(ctx context.Context) error {
	return a.provider.Initialize(ctx)
}

func (a *localProviderAdapter) IsEnabled(ctx context.Context, flagKey string, userCtx *UserContext, defaultValue bool) (bool, error) {
	return a.provider.IsEnabled(ctx, flagKey, convertUserContext(userCtx), defaultValue)
}

func (a *localProviderAdapter) GetString(ctx context.Context, flagKey string, userCtx *UserContext, defaultValue string) (string, error) {
	return a.provider.GetString(ctx, flagKey, convertUserContext(userCtx), defaultValue)
}

func (a *localProviderAdapter) GetInt(ctx context.Context, flagKey string, userCtx *UserContext, defaultValue int) (int, error) {
	return a.provider.GetInt(ctx, flagKey, convertUserContext(userCtx), defaultValue)
}

func (a *localProviderAdapter) GetFloat(ctx context.Context, flagKey string, userCtx *UserContext, defaultValue float64) (float64, error) {
	return a.provider.GetFloat(ctx, flagKey, convertUserContext(userCtx), defaultValue)
}

func (a *localProviderAdapter) GetJSON(ctx context.Context, flagKey string, userCtx *UserContext, defaultValue any) (any, error) {
	return a.provider.GetJSON(ctx, flagKey, convertUserContext(userCtx), defaultValue)
}

func (a *localProviderAdapter) GetAllFlags(ctx context.Context, userCtx *UserContext) (map[string]any, error) {
	return a.provider.GetAllFlags(ctx, convertUserContext(userCtx))
}

func (a *localProviderAdapter) Refresh(ctx context.Context) error {
	return a.provider.Refresh(ctx)
}

func (a *localProviderAdapter) Close() error {
	return a.provider.Close()
}

func (a *localProviderAdapter) Health(ctx context.Context) error {
	return a.provider.Health(ctx)
}
