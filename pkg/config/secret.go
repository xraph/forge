package config

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/xraph/forge/pkg/common"
	configscore "github.com/xraph/forge/pkg/config/core"
	"github.com/xraph/forge/pkg/logger"
)

// SecretsManager manages secrets for configuration
type SecretsManager = configscore.SecretsManager

// SecretProvider defines an interface for different secret backends
type SecretProvider = configscore.SecretProvider

// SecretsConfig contains configuration for the secrets manager
type SecretsConfig struct {
	DefaultProvider   string                    `yaml:"default_provider" json:"default_provider"`
	Providers         map[string]ProviderConfig `yaml:"providers" json:"providers"`
	CacheEnabled      bool                      `yaml:"cache_enabled" json:"cache_enabled"`
	CacheTTL          time.Duration             `yaml:"cache_ttl" json:"cache_ttl"`
	RotationEnabled   bool                      `yaml:"rotation_enabled" json:"rotation_enabled"`
	RotationInterval  time.Duration             `yaml:"rotation_interval" json:"rotation_interval"`
	EncryptionEnabled bool                      `yaml:"encryption_enabled" json:"encryption_enabled"`
	EncryptionKey     string                    `yaml:"encryption_key" json:"encryption_key"`
	MetricsEnabled    bool                      `yaml:"metrics_enabled" json:"metrics_enabled"`
	Logger            common.Logger             `yaml:"-" json:"-"`
	Metrics           common.Metrics            `yaml:"-" json:"-"`
	ErrorHandler      common.ErrorHandler       `yaml:"-" json:"-"`
}

// ProviderConfig contains configuration for a secret provider
type ProviderConfig struct {
	Type       string                 `yaml:"type" json:"type"`
	Priority   int                    `yaml:"priority" json:"priority"`
	Properties map[string]interface{} `yaml:"properties" json:"properties"`
	Enabled    bool                   `yaml:"enabled" json:"enabled"`
}

// SecretMetadata contains metadata about a secret
type SecretMetadata struct {
	Key          string                 `json:"key"`
	Provider     string                 `json:"provider"`
	Created      time.Time              `json:"created"`
	LastAccessed time.Time              `json:"last_accessed"`
	LastRotated  time.Time              `json:"last_rotated,omitempty"`
	AccessCount  int64                  `json:"access_count"`
	Tags         map[string]string      `json:"tags,omitempty"`
	Encrypted    bool                   `json:"encrypted"`
	TTL          time.Duration          `json:"ttl,omitempty"`
	Properties   map[string]interface{} `json:"properties,omitempty"`
}

// CachedSecret represents a cached secret
type CachedSecret struct {
	Value     string         `json:"value"`
	Metadata  SecretMetadata `json:"metadata"`
	ExpiresAt time.Time      `json:"expires_at"`
	Encrypted bool           `json:"encrypted"`
}

// SecretRotationPolicy defines how secrets should be rotated
type SecretRotationPolicy struct {
	Enabled    bool              `json:"enabled"`
	Interval   time.Duration     `json:"interval"`
	MaxAge     time.Duration     `json:"max_age"`
	Generator  SecretGenerator   `json:"-"`
	Validators []SecretValidator `json:"-"`
	NotifyOn   []string          `json:"notify_on"`
}

// SecretGenerator generates new secret values
type SecretGenerator interface {
	GenerateSecret(ctx context.Context, key string, metadata SecretMetadata) (string, error)
	ValidateSecret(ctx context.Context, value string) error
}

// SecretValidator validates secret values
type SecretValidator interface {
	ValidateSecret(ctx context.Context, key, value string) error
	GetValidationRules() []string
}

// SecretsManagerImpl implements the SecretsManager interface
type SecretsManagerImpl struct {
	config           SecretsConfig
	providers        map[string]SecretProvider
	defaultProvider  SecretProvider
	cache            map[string]*CachedSecret
	metadata         map[string]*SecretMetadata
	rotationPolicies map[string]*SecretRotationPolicy
	encryptor        *SecretEncryptor
	logger           common.Logger
	metrics          common.Metrics
	errorHandler     common.ErrorHandler
	mu               sync.RWMutex
	started          bool
	stopChan         chan struct{}
	rotationTicker   *time.Ticker
}

// NewSecretsManager creates a new secrets manager
func NewSecretsManager(config SecretsConfig) SecretsManager {
	if config.CacheTTL == 0 {
		config.CacheTTL = 15 * time.Minute
	}
	if config.RotationInterval == 0 {
		config.RotationInterval = 24 * time.Hour
	}

	manager := &SecretsManagerImpl{
		config:           config,
		providers:        make(map[string]SecretProvider),
		cache:            make(map[string]*CachedSecret),
		metadata:         make(map[string]*SecretMetadata),
		rotationPolicies: make(map[string]*SecretRotationPolicy),
		logger:           config.Logger,
		metrics:          config.Metrics,
		errorHandler:     config.ErrorHandler,
		stopChan:         make(chan struct{}),
	}

	// Initialize encryption if enabled
	if config.EncryptionEnabled {
		manager.encryptor = NewSecretEncryptor(config.EncryptionKey)
	}

	// Register default providers
	manager.registerDefaultProviders()

	return manager
}

// Start starts the secrets manager
func (sm *SecretsManagerImpl) Start(ctx context.Context) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if sm.started {
		return common.ErrLifecycleError("start", fmt.Errorf("secrets manager already started"))
	}

	// Initialize providers from config
	for name, providerConfig := range sm.config.Providers {
		if !providerConfig.Enabled {
			continue
		}

		provider, err := sm.createProvider(providerConfig)
		if err != nil {
			return common.ErrConfigError(fmt.Sprintf("failed to create provider %s", name), err)
		}

		if err := provider.Initialize(ctx, providerConfig.Properties); err != nil {
			return common.ErrConfigError(fmt.Sprintf("failed to initialize provider %s", name), err)
		}

		sm.providers[name] = provider

		if name == sm.config.DefaultProvider {
			sm.defaultProvider = provider
		}
	}

	// Set default provider if not explicitly set
	if sm.defaultProvider == nil && len(sm.providers) > 0 {
		for _, provider := range sm.providers {
			sm.defaultProvider = provider
			break
		}
	}

	// Start rotation if enabled
	if sm.config.RotationEnabled {
		sm.startRotation()
	}

	sm.started = true

	if sm.logger != nil {
		sm.logger.Info("secrets manager started",
			logger.Int("providers", len(sm.providers)),
			logger.String("default_provider", sm.config.DefaultProvider),
			logger.Bool("cache_enabled", sm.config.CacheEnabled),
			logger.Bool("rotation_enabled", sm.config.RotationEnabled),
		)
	}

	if sm.metrics != nil {
		sm.metrics.Counter("forge.config.secrets.manager_started").Inc()
		sm.metrics.Gauge("forge.config.secrets.providers_count").Set(float64(len(sm.providers)))
	}

	return nil
}

// Stop stops the secrets manager
func (sm *SecretsManagerImpl) Stop(ctx context.Context) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if !sm.started {
		return nil
	}

	// Stop rotation
	if sm.rotationTicker != nil {
		sm.rotationTicker.Stop()
	}

	// Signal stop
	close(sm.stopChan)

	// Close all providers
	for name, provider := range sm.providers {
		if err := provider.Close(ctx); err != nil {
			if sm.logger != nil {
				sm.logger.Error("failed to close provider",
					logger.String("provider", name),
					logger.Error(err),
				)
			}
		}
	}

	// Clear cache
	sm.cache = make(map[string]*CachedSecret)
	sm.metadata = make(map[string]*SecretMetadata)

	sm.started = false

	if sm.logger != nil {
		sm.logger.Info("secrets manager stopped")
	}

	if sm.metrics != nil {
		sm.metrics.Counter("forge.config.secrets.manager_stopped").Inc()
	}

	return nil
}

// GetSecret retrieves a secret by key
func (sm *SecretsManagerImpl) GetSecret(ctx context.Context, key string) (string, error) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	if !sm.started {
		return "", common.ErrLifecycleError("get_secret", fmt.Errorf("secrets manager not started"))
	}

	// Check cache first if enabled
	if sm.config.CacheEnabled {
		if cached := sm.getCachedSecret(key); cached != nil {
			sm.updateSecretAccess(key)
			if sm.metrics != nil {
				sm.metrics.Counter("forge.config.secrets.cache_hits").Inc()
			}
			return cached.Value, nil
		}
	}

	// Try providers in priority order
	providers := sm.getProvidersInOrder()
	var lastErr error

	for _, provider := range providers {
		value, err := provider.GetSecret(ctx, key)
		if err == nil {
			// Cache the secret if caching is enabled
			if sm.config.CacheEnabled {
				sm.cacheSecret(key, value, provider.Name())
			}

			sm.updateSecretAccess(key)

			if sm.metrics != nil {
				sm.metrics.Counter("forge.config.secrets.gets_success", "provider", provider.Name()).Inc()
			}

			return value, nil
		}
		lastErr = err
	}

	if sm.metrics != nil {
		sm.metrics.Counter("forge.config.secrets.gets_failed").Inc()
	}

	return "", common.ErrServiceNotFound(fmt.Sprintf("secret '%s' not found", key)).WithCause(lastErr)
}

// SetSecret stores a secret
func (sm *SecretsManagerImpl) SetSecret(ctx context.Context, key, value string) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if !sm.started {
		return common.ErrLifecycleError("set_secret", fmt.Errorf("secrets manager not started"))
	}

	if sm.defaultProvider == nil {
		return common.ErrConfigError("no default provider configured", nil)
	}

	// Encrypt value if encryption is enabled
	if sm.config.EncryptionEnabled && sm.encryptor != nil {
		encrypted, err := sm.encryptor.Encrypt(value)
		if err != nil {
			return common.ErrConfigError("failed to encrypt secret", err)
		}
		value = encrypted
	}

	// Store in default provider
	if err := sm.defaultProvider.SetSecret(ctx, key, value); err != nil {
		if sm.metrics != nil {
			sm.metrics.Counter("forge.config.secrets.sets_failed").Inc()
		}
		return common.ErrConfigError(fmt.Sprintf("failed to set secret '%s'", key), err)
	}

	// Update metadata
	sm.updateSecretMetadata(key, sm.defaultProvider.Name())

	// Cache the secret if caching is enabled
	if sm.config.CacheEnabled {
		sm.cacheSecret(key, value, sm.defaultProvider.Name())
	}

	if sm.logger != nil {
		sm.logger.Info("secret stored",
			logger.String("key", key),
			logger.String("provider", sm.defaultProvider.Name()),
		)
	}

	if sm.metrics != nil {
		sm.metrics.Counter("forge.config.secrets.sets_success", "provider", sm.defaultProvider.Name()).Inc()
	}

	return nil
}

// DeleteSecret removes a secret
func (sm *SecretsManagerImpl) DeleteSecret(ctx context.Context, key string) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if !sm.started {
		return common.ErrLifecycleError("delete_secret", fmt.Errorf("secrets manager not started"))
	}

	// Remove from all providers
	var errors []error
	for name, provider := range sm.providers {
		if err := provider.DeleteSecret(ctx, key); err != nil {
			errors = append(errors, fmt.Errorf("provider %s: %w", name, err))
		}
	}

	// Remove from cache and metadata
	delete(sm.cache, key)
	delete(sm.metadata, key)

	if len(errors) > 0 {
		if sm.metrics != nil {
			sm.metrics.Counter("forge.config.secrets.deletes_failed").Inc()
		}
		return common.ErrConfigError(fmt.Sprintf("failed to delete secret '%s' from some providers", key), fmt.Errorf("%v", errors))
	}

	if sm.logger != nil {
		sm.logger.Info("secret deleted",
			logger.String("key", key),
		)
	}

	if sm.metrics != nil {
		sm.metrics.Counter("forge.config.secrets.deletes_success").Inc()
	}

	return nil
}

// ListSecrets returns all secret keys
func (sm *SecretsManagerImpl) ListSecrets(ctx context.Context) ([]string, error) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	if !sm.started {
		return nil, common.ErrLifecycleError("list_secrets", fmt.Errorf("secrets manager not started"))
	}

	keySet := make(map[string]bool)

	// Collect keys from all providers
	for _, provider := range sm.providers {
		keys, err := provider.ListSecrets(ctx)
		if err != nil {
			if sm.logger != nil {
				sm.logger.Warn("failed to list secrets from provider",
					logger.String("provider", provider.Name()),
					logger.Error(err),
				)
			}
			continue
		}

		for _, key := range keys {
			keySet[key] = true
		}
	}

	// Convert set to slice
	keys := make([]string, 0, len(keySet))
	for key := range keySet {
		keys = append(keys, key)
	}

	return keys, nil
}

// RotateSecret rotates a secret with a new value
func (sm *SecretsManagerImpl) RotateSecret(ctx context.Context, key, newValue string) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if !sm.started {
		return common.ErrLifecycleError("rotate_secret", fmt.Errorf("secrets manager not started"))
	}

	// Get current value for backup
	oldValue, err := sm.GetSecret(ctx, key)
	if err != nil {
		return common.ErrConfigError(fmt.Sprintf("failed to get current value for secret '%s'", key), err)
	}

	// Set new value
	if err := sm.SetSecret(ctx, key, newValue); err != nil {
		return common.ErrConfigError(fmt.Sprintf("failed to rotate secret '%s'", key), err)
	}

	// Update rotation metadata
	if metadata, exists := sm.metadata[key]; exists {
		metadata.LastRotated = time.Now()
	}

	if sm.logger != nil {
		sm.logger.Info("secret rotated",
			logger.String("key", key),
		)
	}

	if sm.metrics != nil {
		sm.metrics.Counter("forge.config.secrets.rotations_success").Inc()
	}

	// Backup old value (implementation specific)
	_ = oldValue // placeholder for backup logic

	return nil
}

// RegisterProvider registers a secrets provider
func (sm *SecretsManagerImpl) RegisterProvider(name string, provider SecretProvider) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	sm.providers[name] = provider

	if sm.logger != nil {
		sm.logger.Info("secrets provider registered",
			logger.String("name", name),
			logger.String("type", provider.Name()),
		)
	}

	return nil
}

// GetProvider returns a secrets provider by name
func (sm *SecretsManagerImpl) GetProvider(name string) (SecretProvider, error) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	provider, exists := sm.providers[name]
	if !exists {
		return nil, common.ErrServiceNotFound(fmt.Sprintf("provider '%s'", name))
	}

	return provider, nil
}

// RefreshSecrets refreshes all cached secrets
func (sm *SecretsManagerImpl) RefreshSecrets(ctx context.Context) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if !sm.started {
		return common.ErrLifecycleError("refresh_secrets", fmt.Errorf("secrets manager not started"))
	}

	// Clear cache
	sm.cache = make(map[string]*CachedSecret)

	if sm.logger != nil {
		sm.logger.Info("secrets cache refreshed")
	}

	if sm.metrics != nil {
		sm.metrics.Counter("forge.config.secrets.cache_refreshed").Inc()
	}

	return nil
}

// HealthCheck performs a health check
func (sm *SecretsManagerImpl) HealthCheck(ctx context.Context) error {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	if !sm.started {
		return common.ErrHealthCheckFailed("secrets_manager", fmt.Errorf("secrets manager not started"))
	}

	// Check all providers
	for name, provider := range sm.providers {
		if err := provider.HealthCheck(ctx); err != nil {
			return common.ErrHealthCheckFailed("secrets_manager", fmt.Errorf("provider %s failed health check: %w", name, err))
		}
	}

	return nil
}

// Helper methods

func (sm *SecretsManagerImpl) getCachedSecret(key string) *CachedSecret {
	cached, exists := sm.cache[key]
	if !exists {
		return nil
	}

	// Check if expired
	if time.Now().After(cached.ExpiresAt) {
		delete(sm.cache, key)
		return nil
	}

	// Decrypt if needed
	if cached.Encrypted && sm.encryptor != nil {
		decrypted, err := sm.encryptor.Decrypt(cached.Value)
		if err != nil {
			delete(sm.cache, key)
			return nil
		}
		cached.Value = decrypted
		cached.Encrypted = false
	}

	return cached
}

func (sm *SecretsManagerImpl) cacheSecret(key, value, provider string) {
	expiresAt := time.Now().Add(sm.config.CacheTTL)

	// Encrypt if needed
	encrypted := false
	if sm.config.EncryptionEnabled && sm.encryptor != nil {
		if encryptedValue, err := sm.encryptor.Encrypt(value); err == nil {
			value = encryptedValue
			encrypted = true
		}
	}

	sm.cache[key] = &CachedSecret{
		Value:     value,
		ExpiresAt: expiresAt,
		Encrypted: encrypted,
		Metadata: SecretMetadata{
			Key:          key,
			Provider:     provider,
			Created:      time.Now(),
			LastAccessed: time.Now(),
		},
	}
}

func (sm *SecretsManagerImpl) updateSecretAccess(key string) {
	if metadata, exists := sm.metadata[key]; exists {
		metadata.LastAccessed = time.Now()
		metadata.AccessCount++
	}
}

func (sm *SecretsManagerImpl) updateSecretMetadata(key, provider string) {
	if _, exists := sm.metadata[key]; !exists {
		sm.metadata[key] = &SecretMetadata{
			Key:          key,
			Provider:     provider,
			Created:      time.Now(),
			LastAccessed: time.Now(),
			AccessCount:  0,
			Tags:         make(map[string]string),
			Properties:   make(map[string]interface{}),
		}
	}
}

func (sm *SecretsManagerImpl) getProvidersInOrder() []SecretProvider {
	type providerWithPriority struct {
		provider SecretProvider
		priority int
	}

	var providers []providerWithPriority

	for name, provider := range sm.providers {
		priority := 0
		if config, exists := sm.config.Providers[name]; exists {
			priority = config.Priority
		}
		providers = append(providers, providerWithPriority{
			provider: provider,
			priority: priority,
		})
	}

	// Sort by priority (higher first)
	for i := 0; i < len(providers)-1; i++ {
		for j := i + 1; j < len(providers); j++ {
			if providers[i].priority < providers[j].priority {
				providers[i], providers[j] = providers[j], providers[i]
			}
		}
	}

	result := make([]SecretProvider, len(providers))
	for i, p := range providers {
		result[i] = p.provider
	}

	return result
}

func (sm *SecretsManagerImpl) registerDefaultProviders() {
	// Register built-in providers
	sm.RegisterProvider("env", &EnvironmentSecretProvider{})
	sm.RegisterProvider("file", &FileSecretProvider{})
	sm.RegisterProvider("memory", &MemorySecretProvider{})
}

func (sm *SecretsManagerImpl) createProvider(config ProviderConfig) (SecretProvider, error) {
	switch config.Type {
	case "env":
		return &EnvironmentSecretProvider{}, nil
	case "file":
		return &FileSecretProvider{}, nil
	case "memory":
		return &MemorySecretProvider{}, nil
	case "vault":
		return &VaultSecretProvider{}, nil
	default:
		return nil, fmt.Errorf("unknown provider type: %s", config.Type)
	}
}

func (sm *SecretsManagerImpl) startRotation() {
	sm.rotationTicker = time.NewTicker(sm.config.RotationInterval)

	go func() {
		for {
			select {
			case <-sm.stopChan:
				return
			case <-sm.rotationTicker.C:
				sm.performRotation()
			}
		}
	}()
}

func (sm *SecretsManagerImpl) performRotation() {
	ctx := context.Background()

	for key, policy := range sm.rotationPolicies {
		if !policy.Enabled {
			continue
		}

		metadata, exists := sm.metadata[key]
		if !exists {
			continue
		}

		// Check if rotation is needed
		if time.Since(metadata.LastRotated) < policy.Interval {
			continue
		}

		// Generate new secret value
		if policy.Generator != nil {
			newValue, err := policy.Generator.GenerateSecret(ctx, key, *metadata)
			if err != nil {
				if sm.logger != nil {
					sm.logger.Error("failed to generate new secret value",
						logger.String("key", key),
						logger.Error(err),
					)
				}
				continue
			}

			// Rotate the secret
			if err := sm.RotateSecret(ctx, key, newValue); err != nil {
				if sm.logger != nil {
					sm.logger.Error("failed to rotate secret",
						logger.String("key", key),
						logger.Error(err),
					)
				}
				continue
			}

			if sm.logger != nil {
				sm.logger.Info("secret automatically rotated",
					logger.String("key", key),
				)
			}
		}
	}
}

// SecretEncryptor handles encryption and decryption of secrets
type SecretEncryptor struct {
	key []byte
	gcm cipher.AEAD
}

// NewSecretEncryptor creates a new secret encryptor
func NewSecretEncryptor(keyStr string) *SecretEncryptor {
	// Create key from string
	hash := sha256.Sum256([]byte(keyStr))
	key := hash[:]

	// Create cipher
	block, err := aes.NewCipher(key)
	if err != nil {
		panic(fmt.Sprintf("failed to create cipher: %v", err))
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		panic(fmt.Sprintf("failed to create GCM: %v", err))
	}

	return &SecretEncryptor{
		key: key,
		gcm: gcm,
	}
}

// Encrypt encrypts a secret value
func (se *SecretEncryptor) Encrypt(plaintext string) (string, error) {
	// Create nonce
	nonce := make([]byte, se.gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("failed to generate nonce: %w", err)
	}

	// Encrypt
	ciphertext := se.gcm.Seal(nonce, nonce, []byte(plaintext), nil)

	// Encode to base64
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// Decrypt decrypts a secret value
func (se *SecretEncryptor) Decrypt(ciphertext string) (string, error) {
	// Decode from base64
	data, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", fmt.Errorf("failed to decode base64: %w", err)
	}

	// Extract nonce
	nonceSize := se.gcm.NonceSize()
	if len(data) < nonceSize {
		return "", fmt.Errorf("ciphertext too short")
	}

	nonce, cipherData := data[:nonceSize], data[nonceSize:]

	// Decrypt
	plaintext, err := se.gcm.Open(nil, nonce, cipherData, nil)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt: %w", err)
	}

	return string(plaintext), nil
}

// Built-in Secret Providers

// EnvironmentSecretProvider provides secrets from environment variables
type EnvironmentSecretProvider struct {
	prefix string
}

func (esp *EnvironmentSecretProvider) Name() string { return "environment" }

func (esp *EnvironmentSecretProvider) GetSecret(ctx context.Context, key string) (string, error) {
	envKey := esp.prefix + strings.ToUpper(key)
	value := os.Getenv(envKey)
	if value == "" {
		return "", fmt.Errorf("environment variable %s not found", envKey)
	}
	return value, nil
}

func (esp *EnvironmentSecretProvider) SetSecret(ctx context.Context, key, value string) error {
	envKey := esp.prefix + strings.ToUpper(key)
	return os.Setenv(envKey, value)
}

func (esp *EnvironmentSecretProvider) DeleteSecret(ctx context.Context, key string) error {
	envKey := esp.prefix + strings.ToUpper(key)
	return os.Unsetenv(envKey)
}

func (esp *EnvironmentSecretProvider) ListSecrets(ctx context.Context) ([]string, error) {
	var secrets []string
	for _, env := range os.Environ() {
		if strings.HasPrefix(env, esp.prefix) {
			parts := strings.SplitN(env, "=", 2)
			if len(parts) == 2 {
				key := strings.TrimPrefix(parts[0], esp.prefix)
				secrets = append(secrets, strings.ToLower(key))
			}
		}
	}
	return secrets, nil
}

func (esp *EnvironmentSecretProvider) HealthCheck(ctx context.Context) error {
	return nil // Environment variables are always available
}

func (esp *EnvironmentSecretProvider) SupportsRotation() bool { return false }
func (esp *EnvironmentSecretProvider) SupportsCaching() bool  { return true }

func (esp *EnvironmentSecretProvider) Initialize(ctx context.Context, config map[string]interface{}) error {
	if prefix, ok := config["prefix"].(string); ok {
		esp.prefix = prefix
	} else {
		esp.prefix = "SECRET_"
	}
	return nil
}

func (esp *EnvironmentSecretProvider) Close(ctx context.Context) error {
	return nil
}

// FileSecretProvider provides secrets from files
type FileSecretProvider struct {
	basePath string
	secrets  map[string]string
	mu       sync.RWMutex
}

func (fsp *FileSecretProvider) Name() string { return "file" }

func (fsp *FileSecretProvider) GetSecret(ctx context.Context, key string) (string, error) {
	fsp.mu.RLock()
	defer fsp.mu.RUnlock()

	if value, exists := fsp.secrets[key]; exists {
		return value, nil
	}

	// Try to load from file
	filePath := fmt.Sprintf("%s/%s", fsp.basePath, key)
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to read secret file %s: %w", filePath, err)
	}

	value := strings.TrimSpace(string(data))
	fsp.secrets[key] = value
	return value, nil
}

func (fsp *FileSecretProvider) SetSecret(ctx context.Context, key, value string) error {
	fsp.mu.Lock()
	defer fsp.mu.Unlock()

	filePath := fmt.Sprintf("%s/%s", fsp.basePath, key)
	if err := os.WriteFile(filePath, []byte(value), 0600); err != nil {
		return fmt.Errorf("failed to write secret file %s: %w", filePath, err)
	}

	fsp.secrets[key] = value
	return nil
}

func (fsp *FileSecretProvider) DeleteSecret(ctx context.Context, key string) error {
	fsp.mu.Lock()
	defer fsp.mu.Unlock()

	filePath := fmt.Sprintf("%s/%s", fsp.basePath, key)
	if err := os.Remove(filePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete secret file %s: %w", filePath, err)
	}

	delete(fsp.secrets, key)
	return nil
}

func (fsp *FileSecretProvider) ListSecrets(ctx context.Context) ([]string, error) {
	fsp.mu.RLock()
	defer fsp.mu.RUnlock()

	var secrets []string
	for key := range fsp.secrets {
		secrets = append(secrets, key)
	}
	return secrets, nil
}

func (fsp *FileSecretProvider) HealthCheck(ctx context.Context) error {
	if _, err := os.Stat(fsp.basePath); err != nil {
		return fmt.Errorf("base path %s not accessible: %w", fsp.basePath, err)
	}
	return nil
}

func (fsp *FileSecretProvider) SupportsRotation() bool { return true }
func (fsp *FileSecretProvider) SupportsCaching() bool  { return true }

func (fsp *FileSecretProvider) Initialize(ctx context.Context, config map[string]interface{}) error {
	if basePath, ok := config["base_path"].(string); ok {
		fsp.basePath = basePath
	} else {
		fsp.basePath = "/etc/secrets"
	}

	fsp.secrets = make(map[string]string)

	// Create directory if it doesn't exist
	if err := os.MkdirAll(fsp.basePath, 0700); err != nil {
		return fmt.Errorf("failed to create secrets directory: %w", err)
	}

	return nil
}

func (fsp *FileSecretProvider) Close(ctx context.Context) error {
	fsp.mu.Lock()
	defer fsp.mu.Unlock()
	fsp.secrets = make(map[string]string)
	return nil
}

// MemorySecretProvider provides secrets from memory (for testing)
type MemorySecretProvider struct {
	secrets map[string]string
	mu      sync.RWMutex
}

func (msp *MemorySecretProvider) Name() string { return "memory" }

func (msp *MemorySecretProvider) GetSecret(ctx context.Context, key string) (string, error) {
	msp.mu.RLock()
	defer msp.mu.RUnlock()

	if value, exists := msp.secrets[key]; exists {
		return value, nil
	}
	return "", fmt.Errorf("secret %s not found", key)
}

func (msp *MemorySecretProvider) SetSecret(ctx context.Context, key, value string) error {
	msp.mu.Lock()
	defer msp.mu.Unlock()
	msp.secrets[key] = value
	return nil
}

func (msp *MemorySecretProvider) DeleteSecret(ctx context.Context, key string) error {
	msp.mu.Lock()
	defer msp.mu.Unlock()
	delete(msp.secrets, key)
	return nil
}

func (msp *MemorySecretProvider) ListSecrets(ctx context.Context) ([]string, error) {
	msp.mu.RLock()
	defer msp.mu.RUnlock()

	var secrets []string
	for key := range msp.secrets {
		secrets = append(secrets, key)
	}
	return secrets, nil
}

func (msp *MemorySecretProvider) HealthCheck(ctx context.Context) error {
	return nil
}

func (msp *MemorySecretProvider) SupportsRotation() bool { return true }
func (msp *MemorySecretProvider) SupportsCaching() bool  { return false }

func (msp *MemorySecretProvider) Initialize(ctx context.Context, config map[string]interface{}) error {
	msp.secrets = make(map[string]string)
	return nil
}

func (msp *MemorySecretProvider) Close(ctx context.Context) error {
	msp.mu.Lock()
	defer msp.mu.Unlock()
	msp.secrets = make(map[string]string)
	return nil
}

// VaultSecretProvider provides secrets from HashiCorp Vault (placeholder)
type VaultSecretProvider struct {
	endpoint string
	token    string
	// Add vault client when implementing
}

func (vsp *VaultSecretProvider) Name() string { return "vault" }

func (vsp *VaultSecretProvider) GetSecret(ctx context.Context, key string) (string, error) {
	// Placeholder implementation
	return "", fmt.Errorf("vault provider not implemented")
}

func (vsp *VaultSecretProvider) SetSecret(ctx context.Context, key, value string) error {
	return fmt.Errorf("vault provider not implemented")
}

func (vsp *VaultSecretProvider) DeleteSecret(ctx context.Context, key string) error {
	return fmt.Errorf("vault provider not implemented")
}

func (vsp *VaultSecretProvider) ListSecrets(ctx context.Context) ([]string, error) {
	return nil, fmt.Errorf("vault provider not implemented")
}

func (vsp *VaultSecretProvider) HealthCheck(ctx context.Context) error {
	return fmt.Errorf("vault provider not implemented")
}

func (vsp *VaultSecretProvider) SupportsRotation() bool { return true }
func (vsp *VaultSecretProvider) SupportsCaching() bool  { return true }

func (vsp *VaultSecretProvider) Initialize(ctx context.Context, config map[string]interface{}) error {
	if endpoint, ok := config["endpoint"].(string); ok {
		vsp.endpoint = endpoint
	}
	if token, ok := config["token"].(string); ok {
		vsp.token = token
	}
	return nil
}

func (vsp *VaultSecretProvider) Close(ctx context.Context) error {
	return nil
}

// Utility functions for secret reference handling

// IsSecretReference checks if a value is a secret reference
func IsSecretReference(value string) bool {
	return strings.HasPrefix(value, "${secret:") && strings.HasSuffix(value, "}")
}

// ExtractSecretKey extracts the secret key from a reference
func ExtractSecretKey(reference string) string {
	if !IsSecretReference(reference) {
		return reference
	}

	// Remove ${secret: and }
	key := reference[9 : len(reference)-1]
	return key
}

// ExpandSecretReferences expands secret references in configuration
func ExpandSecretReferences(ctx context.Context, data map[string]interface{}, secretsManager SecretsManager) (map[string]interface{}, error) {
	result := make(map[string]interface{})

	for key, value := range data {
		expandedValue, err := expandSecretValue(ctx, value, secretsManager)
		if err != nil {
			return nil, fmt.Errorf("failed to expand secrets for key %s: %w", key, err)
		}
		result[key] = expandedValue
	}

	return result, nil
}

// expandSecretValue recursively expands secret references in a value
func expandSecretValue(ctx context.Context, value interface{}, secretsManager SecretsManager) (interface{}, error) {
	switch v := value.(type) {
	case string:
		if IsSecretReference(v) {
			secretKey := ExtractSecretKey(v)
			return secretsManager.GetSecret(ctx, secretKey)
		}
		return v, nil
	case map[string]interface{}:
		return ExpandSecretReferences(ctx, v, secretsManager)
	case []interface{}:
		result := make([]interface{}, len(v))
		for i, item := range v {
			expandedItem, err := expandSecretValue(ctx, item, secretsManager)
			if err != nil {
				return nil, err
			}
			result[i] = expandedItem
		}
		return result, nil
	default:
		return value, nil
	}
}
