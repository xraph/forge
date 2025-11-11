package sources

// Consul source operations use error handlers and stop methods without error returns.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/consul/api"
	"github.com/xraph/forge/errors"
	configcore "github.com/xraph/forge/internal/config/core"
	"github.com/xraph/forge/internal/logger"
	"github.com/xraph/forge/internal/shared"
)

// ConsulSource represents a Consul configuration source.
type ConsulSource struct {
	name         string
	client       *api.Client
	prefix       string
	priority     int
	watching     bool
	watchStop    chan struct{}
	lastIndex    uint64
	logger       logger.Logger
	errorHandler shared.ErrorHandler
	options      ConsulSourceOptions
	mu           sync.RWMutex
}

// ConsulSourceOptions contains options for Consul configuration sources.
type ConsulSourceOptions struct {
	Name         string
	Address      string
	Token        string
	Datacenter   string
	Prefix       string
	Priority     int
	WatchEnabled bool
	Timeout      time.Duration
	RetryCount   int
	RetryDelay   time.Duration
	TLS          *ConsulTLSConfig
	Logger       logger.Logger
	ErrorHandler shared.ErrorHandler
}

// ConsulSourceConfig contains configuration for creating Consul sources.
type ConsulSourceConfig struct {
	Address      string           `json:"address"       yaml:"address"`
	Token        string           `json:"token"         yaml:"token"`
	Datacenter   string           `json:"datacenter"    yaml:"datacenter"`
	Prefix       string           `json:"prefix"        yaml:"prefix"`
	Priority     int              `json:"priority"      yaml:"priority"`
	WatchEnabled bool             `json:"watch_enabled" yaml:"watch_enabled"`
	Timeout      time.Duration    `json:"timeout"       yaml:"timeout"`
	RetryCount   int              `json:"retry_count"   yaml:"retry_count"`
	RetryDelay   time.Duration    `json:"retry_delay"   yaml:"retry_delay"`
	TLS          *ConsulTLSConfig `json:"tls"           yaml:"tls"`
}

// ConsulTLSConfig contains TLS configuration for Consul.
type ConsulTLSConfig struct {
	Enabled            bool   `json:"enabled"              yaml:"enabled"`
	CertFile           string `json:"cert_file"            yaml:"cert_file"`
	KeyFile            string `json:"key_file"             yaml:"key_file"`
	CAFile             string `json:"ca_file"              yaml:"ca_file"`
	InsecureSkipVerify bool   `json:"insecure_skip_verify" yaml:"insecure_skip_verify"`
}

// NewConsulSource creates a new Consul configuration source.
func NewConsulSource(prefix string, options ConsulSourceOptions) (configcore.ConfigSource, error) {
	if options.Address == "" {
		options.Address = "127.0.0.1:8500"
	}

	if options.Timeout == 0 {
		options.Timeout = 30 * time.Second
	}

	if options.RetryCount == 0 {
		options.RetryCount = 3
	}

	if options.RetryDelay == 0 {
		options.RetryDelay = 5 * time.Second
	}

	name := options.Name
	if name == "" {
		name = "consul:" + prefix
	}

	// Create Consul client configuration
	consulConfig := api.DefaultConfig()
	consulConfig.Address = options.Address
	consulConfig.Token = options.Token
	consulConfig.Datacenter = options.Datacenter
	consulConfig.WaitTime = options.Timeout

	// Configure TLS if enabled
	if options.TLS != nil && options.TLS.Enabled {
		consulConfig.Scheme = "https"
		consulConfig.TLSConfig.CertFile = options.TLS.CertFile
		consulConfig.TLSConfig.KeyFile = options.TLS.KeyFile
		consulConfig.TLSConfig.CAFile = options.TLS.CAFile
		consulConfig.TLSConfig.InsecureSkipVerify = options.TLS.InsecureSkipVerify
	}

	// Create Consul client
	client, err := api.NewClient(consulConfig)
	if err != nil {
		return nil, errors.ErrConfigError(fmt.Sprintf("failed to create Consul client: %v", err), err)
	}

	source := &ConsulSource{
		name:         name,
		client:       client,
		prefix:       prefix,
		priority:     options.Priority,
		logger:       options.Logger,
		errorHandler: options.ErrorHandler,
		options:      options,
	}

	// Test connection
	if err := source.testConnection(context.Background()); err != nil {
		return nil, errors.ErrConfigError(fmt.Sprintf("failed to connect to Consul: %v", err), err)
	}

	return source, nil
}

// Name returns the source name.
func (cs *ConsulSource) Name() string {
	return cs.name
}

// GetName returns the source name (alias for Name).
func (cs *ConsulSource) GetName() string {
	return cs.name
}

// GetType returns the source type.
func (cs *ConsulSource) GetType() string {
	return "consul"
}

// IsAvailable checks if the source is available.
func (cs *ConsulSource) IsAvailable(ctx context.Context) bool {
	// TODO: Implement actual Consul availability check
	return true
}

// Priority returns the source priority.
func (cs *ConsulSource) Priority() int {
	return cs.priority
}

// Load loads configuration from Consul KV store.
func (cs *ConsulSource) Load(ctx context.Context) (map[string]any, error) {
	if cs.logger != nil {
		cs.logger.Debug("loading configuration from Consul",
			logger.String("prefix", cs.prefix),
			logger.String("address", cs.options.Address),
		)
	}

	kv := cs.client.KV()

	pairs, meta, err := kv.List(cs.prefix, &api.QueryOptions{
		RequireConsistent: true,
		AllowStale:        false,
	})
	if err != nil {
		return nil, errors.ErrConfigError(fmt.Sprintf("failed to query Consul KV: %v", err), err)
	}

	// Update last index for watching
	cs.mu.Lock()
	cs.lastIndex = meta.LastIndex
	cs.mu.Unlock()

	config := make(map[string]any)

	for _, pair := range pairs {
		// Skip directories (keys ending with /)
		if strings.HasSuffix(pair.Key, "/") {
			continue
		}

		// Remove prefix from key
		key := strings.TrimPrefix(pair.Key, cs.prefix)
		key = strings.TrimPrefix(key, "/")

		if key == "" {
			continue
		}

		// Convert Consul key format to nested structure
		value, err := cs.parseValue(pair.Value)
		if err != nil {
			if cs.logger != nil {
				cs.logger.Warn("failed to parse Consul value",
					logger.String("key", pair.Key),
					logger.Error(err),
				)
			}

			continue
		}

		cs.setNestedValue(config, key, value)
	}

	if cs.logger != nil {
		cs.logger.Info("configuration loaded from Consul",
			logger.String("prefix", cs.prefix),
			logger.Int("keys", len(pairs)),
			logger.Uint64("last_index", meta.LastIndex),
		)
	}

	return config, nil
}

// Watch starts watching Consul KV for changes.
func (cs *ConsulSource) Watch(ctx context.Context, callback func(map[string]any)) error {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	if cs.watching {
		return errors.ErrConfigError("already watching Consul KV", nil)
	}

	if !cs.IsWatchable() {
		return errors.ErrConfigError("Consul watching is not enabled", nil)
	}

	cs.watchStop = make(chan struct{})
	cs.watching = true

	// Start watching goroutine
	go cs.watchLoop(ctx, callback)

	if cs.logger != nil {
		cs.logger.Info("started watching Consul KV",
			logger.String("prefix", cs.prefix),
		)
	}

	return nil
}

// StopWatch stops watching Consul KV.
func (cs *ConsulSource) StopWatch() error {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	if !cs.watching {
		return nil
	}

	if cs.watchStop != nil {
		close(cs.watchStop)
		cs.watchStop = nil
	}

	cs.watching = false

	if cs.logger != nil {
		cs.logger.Info("stopped watching Consul KV",
			logger.String("prefix", cs.prefix),
		)
	}

	return nil
}

// Reload forces a reload of Consul configuration.
func (cs *ConsulSource) Reload(ctx context.Context) error {
	if cs.logger != nil {
		cs.logger.Info("reloading Consul configuration",
			logger.String("prefix", cs.prefix),
		)
	}

	// Just load again - the configuration will be updated
	_, err := cs.Load(ctx)

	return err
}

// IsWatchable returns true if Consul watching is enabled.
func (cs *ConsulSource) IsWatchable() bool {
	return cs.options.WatchEnabled
}

// SupportsSecrets returns true (Consul can store secrets).
func (cs *ConsulSource) SupportsSecrets() bool {
	return true
}

// GetSecret retrieves a secret from Consul KV.
func (cs *ConsulSource) GetSecret(ctx context.Context, key string) (string, error) {
	kv := cs.client.KV()

	// Prepend prefix if not already present
	secretKey := key
	if !strings.HasPrefix(key, cs.prefix) {
		secretKey = cs.prefix + "/" + key
	}

	pair, _, err := kv.Get(secretKey, &api.QueryOptions{
		RequireConsistent: true,
	})
	if err != nil {
		return "", errors.ErrConfigError(fmt.Sprintf("failed to get secret from Consul: %v", err), err)
	}

	if pair == nil {
		return "", errors.ErrConfigError("secret not found in Consul: "+key, nil)
	}

	return string(pair.Value), nil
}

// watchLoop is the main watching loop for Consul KV changes.
func (cs *ConsulSource) watchLoop(ctx context.Context, callback func(map[string]any)) {
	defer func() {
		if r := recover(); r != nil {
			if cs.logger != nil {
				cs.logger.Error("panic in Consul watch loop",
					logger.String("prefix", cs.prefix),
					logger.Any("panic", r),
				)
			}
		}
	}()

	kv := cs.client.KV()
	retryCount := 0

	for {
		select {
		case <-ctx.Done():
			return
		case <-cs.watchStop:
			return
		default:
		}

		// Use blocking query to watch for changes
		cs.mu.RLock()
		lastIndex := cs.lastIndex
		cs.mu.RUnlock()

		pairs, meta, err := kv.List(cs.prefix, &api.QueryOptions{
			WaitIndex: lastIndex,
			WaitTime:  cs.options.Timeout,
		})
		if err != nil {
			retryCount++
			if retryCount <= cs.options.RetryCount {
				if cs.logger != nil {
					cs.logger.Warn("Consul watch error, retrying",
						logger.String("prefix", cs.prefix),
						logger.Int("attempt", retryCount),
						logger.Error(err),
					)
				}

				select {
				case <-ctx.Done():
					return
				case <-cs.watchStop:
					return
				case <-time.After(cs.options.RetryDelay):
					continue
				}
			} else {
				cs.handleWatchError(err)

				return
			}
		}

		// Reset retry count on successful operation
		retryCount = 0

		// Check if index has changed (indicating changes)
		if meta.LastIndex > lastIndex {
			cs.mu.Lock()
			cs.lastIndex = meta.LastIndex
			cs.mu.Unlock()

			if cs.logger != nil {
				cs.logger.Info("Consul KV changes detected",
					logger.String("prefix", cs.prefix),
					logger.Uint64("new_index", meta.LastIndex),
				)
			}

			// Parse changes and notify callback
			config := make(map[string]any)

			for _, pair := range pairs {
				if strings.HasSuffix(pair.Key, "/") {
					continue
				}

				key := strings.TrimPrefix(pair.Key, cs.prefix)
				key = strings.TrimPrefix(key, "/")

				if key == "" {
					continue
				}

				value, err := cs.parseValue(pair.Value)
				if err != nil {
					continue
				}

				cs.setNestedValue(config, key, value)
			}

			// Notify callback
			if callback != nil {
				go func() {
					defer func() {
						if r := recover(); r != nil {
							if cs.logger != nil {
								cs.logger.Error("panic in Consul watch callback",
									logger.String("prefix", cs.prefix),
									logger.Any("panic", r),
								)
							}
						}
					}()

					callback(config)
				}()
			}
		}
	}
}

// testConnection tests the connection to Consul.
func (cs *ConsulSource) testConnection(ctx context.Context) error {
	agent := cs.client.Agent()
	_, err := agent.Self()

	return err
}

// parseValue parses a Consul value, attempting JSON first, then treating as string.
func (cs *ConsulSource) parseValue(data []byte) (any, error) {
	if len(data) == 0 {
		return "", nil
	}

	// Try to parse as JSON first
	var jsonValue any
	if err := json.Unmarshal(data, &jsonValue); err == nil {
		return jsonValue, nil
	}

	// If JSON parsing fails, treat as string
	return string(data), nil
}

// setNestedValue sets a nested configuration value using slash notation.
func (cs *ConsulSource) setNestedValue(config map[string]any, key string, value any) {
	keys := strings.Split(key, "/")
	current := config

	for i, k := range keys {
		if i == len(keys)-1 {
			// Last key - set the value
			current[k] = value
		} else {
			// Intermediate key - ensure map exists
			if _, exists := current[k]; !exists {
				current[k] = make(map[string]any)
			}

			if nextMap, ok := current[k].(map[string]any); ok {
				current = nextMap
			} else {
				// Type conflict - create new map
				current[k] = make(map[string]any)
				current = current[k].(map[string]any)
			}
		}
	}
}

// handleWatchError handles errors during watching.
func (cs *ConsulSource) handleWatchError(err error) {
	if cs.logger != nil {
		cs.logger.Error("Consul watch error",
			logger.String("prefix", cs.prefix),
			logger.Error(err),
		)
	}

	if cs.errorHandler != nil {
		cs.errorHandler.HandleError(nil, errors.ErrConfigError("Consul watch error for prefix "+cs.prefix, err))
	}

	// Stop watching on persistent errors
	cs.StopWatch()
}

// ConsulSourceFactory creates Consul configuration sources.
type ConsulSourceFactory struct {
	logger       logger.Logger
	errorHandler shared.ErrorHandler
}

// NewConsulSourceFactory creates a new Consul source factory.
func NewConsulSourceFactory(logger logger.Logger, errorHandler shared.ErrorHandler) *ConsulSourceFactory {
	return &ConsulSourceFactory{
		logger:       logger,
		errorHandler: errorHandler,
	}
}

// CreateFromConfig creates a Consul source from configuration.
func (factory *ConsulSourceFactory) CreateFromConfig(config ConsulSourceConfig) (configcore.ConfigSource, error) {
	options := ConsulSourceOptions{
		Name:         "consul:" + config.Prefix,
		Address:      config.Address,
		Token:        config.Token,
		Datacenter:   config.Datacenter,
		Prefix:       config.Prefix,
		Priority:     config.Priority,
		WatchEnabled: config.WatchEnabled,
		Timeout:      config.Timeout,
		RetryCount:   config.RetryCount,
		RetryDelay:   config.RetryDelay,
		TLS:          config.TLS,
		Logger:       factory.logger,
		ErrorHandler: factory.errorHandler,
	}

	return NewConsulSource(config.Prefix, options)
}

// CreateWithDefaults creates a Consul source with default settings.
func (factory *ConsulSourceFactory) CreateWithDefaults(prefix string) (configcore.ConfigSource, error) {
	options := ConsulSourceOptions{
		Address:      "127.0.0.1:8500",
		Prefix:       prefix,
		Priority:     200, // Higher priority than env, lower than files
		WatchEnabled: true,
		Timeout:      30 * time.Second,
		RetryCount:   3,
		RetryDelay:   5 * time.Second,
		Logger:       factory.logger,
		ErrorHandler: factory.errorHandler,
	}

	return NewConsulSource(prefix, options)
}
