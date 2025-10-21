package core

import (
	"context"
)

// =============================================================================
// SECRETS MANAGEMENT
// =============================================================================

// SecretsManager manages secrets for configuration
type SecretsManager interface {
	// GetSecret retrieves a secret by key
	GetSecret(ctx context.Context, key string) (string, error)

	// SetSecret stores a secret
	SetSecret(ctx context.Context, key, value string) error

	// DeleteSecret removes a secret
	DeleteSecret(ctx context.Context, key string) error

	// ListSecrets returns all secret keys
	ListSecrets(ctx context.Context) ([]string, error)

	// RotateSecret rotates a secret with a new value
	RotateSecret(ctx context.Context, key, newValue string) error

	// RegisterProvider registers a secrets provider
	RegisterProvider(name string, provider SecretProvider) error

	// GetProvider returns a secrets provider by name
	GetProvider(name string) (SecretProvider, error)

	// RefreshSecrets refreshes all cached secrets
	RefreshSecrets(ctx context.Context) error

	// Start starts the secrets manager
	Start(ctx context.Context) error

	// Stop stops the secrets manager
	Stop(ctx context.Context) error

	// HealthCheck performs a health check
	HealthCheck(ctx context.Context) error
}

// SecretProvider defines an interface for different secret backends
type SecretProvider interface {
	// Name returns the provider name
	Name() string

	// GetSecret retrieves a secret
	GetSecret(ctx context.Context, key string) (string, error)

	// SetSecret stores a secret
	SetSecret(ctx context.Context, key, value string) error

	// DeleteSecret removes a secret
	DeleteSecret(ctx context.Context, key string) error

	// ListSecrets returns all secret keys
	ListSecrets(ctx context.Context) ([]string, error)

	// HealthCheck performs a health check
	HealthCheck(ctx context.Context) error

	// SupportsRotation returns true if the provider supports secret rotation
	SupportsRotation() bool

	// SupportsCaching returns true if the provider supports caching
	SupportsCaching() bool

	// Initialize initializes the provider
	Initialize(ctx context.Context, config map[string]interface{}) error

	// Close closes the provider
	Close(ctx context.Context) error
}
