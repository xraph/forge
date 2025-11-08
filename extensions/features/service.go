package features

import (
	"context"
	"fmt"
	"slices"

	"github.com/xraph/forge"
)

// Service provides high-level feature flag operations.
type Service struct {
	provider Provider
	logger   forge.Logger
}

// NewService creates a new feature flags service.
func NewService(provider Provider, logger forge.Logger) *Service {
	return &Service{
		provider: provider,
		logger:   logger,
	}
}

// IsEnabled checks if a boolean feature flag is enabled.
func (s *Service) IsEnabled(ctx context.Context, flagKey string, userCtx *UserContext) bool {
	enabled, err := s.provider.IsEnabled(ctx, flagKey, userCtx, false)
	if err != nil {
		s.logger.Warn("failed to evaluate feature flag",
			forge.F("flag_key", flagKey),
			forge.F("error", err),
		)

		return false
	}

	return enabled
}

// IsEnabledWithDefault checks if a boolean feature flag is enabled with a custom default.
func (s *Service) IsEnabledWithDefault(ctx context.Context, flagKey string, userCtx *UserContext, defaultValue bool) bool {
	enabled, err := s.provider.IsEnabled(ctx, flagKey, userCtx, defaultValue)
	if err != nil {
		s.logger.Warn("failed to evaluate feature flag",
			forge.F("flag_key", flagKey),
			forge.F("error", err),
		)

		return defaultValue
	}

	return enabled
}

// GetString gets a string feature flag value.
func (s *Service) GetString(ctx context.Context, flagKey string, userCtx *UserContext, defaultValue string) string {
	value, err := s.provider.GetString(ctx, flagKey, userCtx, defaultValue)
	if err != nil {
		s.logger.Warn("failed to get string flag",
			forge.F("flag_key", flagKey),
			forge.F("error", err),
		)

		return defaultValue
	}

	return value
}

// GetInt gets an integer feature flag value.
func (s *Service) GetInt(ctx context.Context, flagKey string, userCtx *UserContext, defaultValue int) int {
	value, err := s.provider.GetInt(ctx, flagKey, userCtx, defaultValue)
	if err != nil {
		s.logger.Warn("failed to get int flag",
			forge.F("flag_key", flagKey),
			forge.F("error", err),
		)

		return defaultValue
	}

	return value
}

// GetFloat gets a float feature flag value.
func (s *Service) GetFloat(ctx context.Context, flagKey string, userCtx *UserContext, defaultValue float64) float64 {
	value, err := s.provider.GetFloat(ctx, flagKey, userCtx, defaultValue)
	if err != nil {
		s.logger.Warn("failed to get float flag",
			forge.F("flag_key", flagKey),
			forge.F("error", err),
		)

		return defaultValue
	}

	return value
}

// GetJSON gets a JSON feature flag value.
func (s *Service) GetJSON(ctx context.Context, flagKey string, userCtx *UserContext, defaultValue any) any {
	value, err := s.provider.GetJSON(ctx, flagKey, userCtx, defaultValue)
	if err != nil {
		s.logger.Warn("failed to get json flag",
			forge.F("flag_key", flagKey),
			forge.F("error", err),
		)

		return defaultValue
	}

	return value
}

// GetAllFlags gets all feature flags for a user/context.
func (s *Service) GetAllFlags(ctx context.Context, userCtx *UserContext) (map[string]any, error) {
	return s.provider.GetAllFlags(ctx, userCtx)
}

// Refresh refreshes flags from remote source.
func (s *Service) Refresh(ctx context.Context) error {
	return s.provider.Refresh(ctx)
}

// NewUserContext creates a new user context for flag evaluation.
func NewUserContext(userID string) *UserContext {
	return &UserContext{
		UserID:     userID,
		Attributes: make(map[string]any),
	}
}

// WithEmail sets the user email.
func (uc *UserContext) WithEmail(email string) *UserContext {
	uc.Email = email

	return uc
}

// WithName sets the user name.
func (uc *UserContext) WithName(name string) *UserContext {
	uc.Name = name

	return uc
}

// WithGroups sets the user groups.
func (uc *UserContext) WithGroups(groups []string) *UserContext {
	uc.Groups = groups

	return uc
}

// WithAttribute sets a custom attribute.
func (uc *UserContext) WithAttribute(key string, value any) *UserContext {
	if uc.Attributes == nil {
		uc.Attributes = make(map[string]any)
	}

	uc.Attributes[key] = value

	return uc
}

// WithIP sets the user IP address.
func (uc *UserContext) WithIP(ip string) *UserContext {
	uc.IP = ip

	return uc
}

// WithCountry sets the user country.
func (uc *UserContext) WithCountry(country string) *UserContext {
	uc.Country = country

	return uc
}

// GetAttribute gets a custom attribute.
func (uc *UserContext) GetAttribute(key string) (any, bool) {
	if uc.Attributes == nil {
		return nil, false
	}

	val, ok := uc.Attributes[key]

	return val, ok
}

// GetAttributeString gets a string attribute.
func (uc *UserContext) GetAttributeString(key string) (string, error) {
	val, ok := uc.GetAttribute(key)
	if !ok {
		return "", fmt.Errorf("attribute not found: %s", key)
	}

	str, ok := val.(string)
	if !ok {
		return "", fmt.Errorf("attribute is not a string: %s", key)
	}

	return str, nil
}

// HasGroup checks if user is in a group.
func (uc *UserContext) HasGroup(group string) bool {

	return slices.Contains(uc.Groups, group)
}
