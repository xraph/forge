package storage

import "time"

// BackendType represents the type of storage backend.
type BackendType string

const (
	// BackendTypeLocal represents local filesystem storage.
	BackendTypeLocal BackendType = "local"

	// BackendTypeS3 represents AWS S3 storage.
	BackendTypeS3 BackendType = "s3"

	// BackendTypeGCS represents Google Cloud Storage.
	BackendTypeGCS BackendType = "gcs"

	// BackendTypeAzure represents Azure Blob Storage.
	BackendTypeAzure BackendType = "azure"
)

// String returns the string representation of the backend type.
func (t BackendType) String() string {
	return string(t)
}

// Config is the storage extension configuration.
type Config struct {
	// Default backend name
	Default string `default:"local" json:"default" yaml:"default"`

	// Backend configurations
	Backends map[string]BackendConfig `json:"backends" yaml:"backends"`

	// Features
	EnablePresignedURLs bool          `default:"true"       json:"enable_presigned_urls" yaml:"enable_presigned_urls"`
	PresignExpiry       time.Duration `default:"15m"        json:"presign_expiry"        yaml:"presign_expiry"`
	MaxUploadSize       int64         `default:"5368709120" json:"max_upload_size"       yaml:"max_upload_size"` // 5GB
	ChunkSize           int           `default:"5242880"    json:"chunk_size"            yaml:"chunk_size"`      // 5MB

	// CDN
	EnableCDN  bool   `default:"false"     json:"enable_cdn"   yaml:"enable_cdn"`
	CDNBaseURL string `json:"cdn_base_url" yaml:"cdn_base_url"`

	// Resilience configuration
	Resilience ResilienceConfig `json:"resilience" yaml:"resilience"`

	// Use enhanced backend (with locking, pooling, etc.)
	UseEnhancedBackend bool `default:"true" json:"use_enhanced_backend" yaml:"use_enhanced_backend"`

	// Config loading flags
	RequireConfig bool `json:"-" yaml:"-"`
}

// BackendConfig is the configuration for a storage backend.
type BackendConfig struct {
	Type   BackendType    `json:"type"   yaml:"type"`
	Config map[string]any `json:"config" yaml:"config"`
}

// DefaultConfig returns the default storage configuration.
func DefaultConfig() Config {
	return Config{
		Default: "local",
		Backends: map[string]BackendConfig{
			"local": {
				Type: BackendTypeLocal,
				Config: map[string]any{
					"root_dir": "./storage",
					"base_url": "http://localhost:8080/files",
				},
			},
		},
		EnablePresignedURLs: true,
		PresignExpiry:       15 * time.Minute,
		MaxUploadSize:       5368709120, // 5GB
		ChunkSize:           5242880,    // 5MB
		EnableCDN:           false,
		Resilience:          DefaultResilienceConfig(),
		UseEnhancedBackend:  true,
	}
}

// Validate validates the configuration.
func (c *Config) Validate() error {
	if len(c.Backends) == 0 {
		return ErrNoBackendsConfigured
	}

	if c.Default == "" {
		return ErrNoDefaultBackend
	}

	if _, exists := c.Backends[c.Default]; !exists {
		return ErrDefaultBackendNotFound
	}

	// Validate each backend
	for name, backend := range c.Backends {
		if backend.Type == "" {
			return ErrInvalidBackendType
		}

		if backend.Config == nil {
			c.Backends[name] = BackendConfig{
				Type:   backend.Type,
				Config: make(map[string]any),
			}
		}
	}

	return nil
}

// ConfigOption is a functional option for Config.
type ConfigOption func(*Config)

// WithDefault sets the default backend name.
func WithDefault(name string) ConfigOption {
	return func(c *Config) { c.Default = name }
}

// WithBackend adds a single backend configuration.
func WithBackend(name string, backend BackendConfig) ConfigOption {
	return func(c *Config) {
		if c.Backends == nil {
			c.Backends = make(map[string]BackendConfig)
		}

		c.Backends[name] = backend
	}
}

// WithBackends sets all backend configurations.
func WithBackends(backends map[string]BackendConfig) ConfigOption {
	return func(c *Config) {
		c.Backends = backends
	}
}

// WithLocalBackend adds a local filesystem backend.
func WithLocalBackend(name, rootDir, baseURL string) ConfigOption {
	return func(c *Config) {
		if c.Backends == nil {
			c.Backends = make(map[string]BackendConfig)
		}

		c.Backends[name] = BackendConfig{
			Type: BackendTypeLocal,
			Config: map[string]any{
				"root_dir": rootDir,
				"base_url": baseURL,
			},
		}
	}
}

// WithS3Backend adds an S3 backend.
func WithS3Backend(name, bucket, region, endpoint string) ConfigOption {
	return func(c *Config) {
		if c.Backends == nil {
			c.Backends = make(map[string]BackendConfig)
		}

		config := map[string]any{
			"bucket": bucket,
			"region": region,
		}
		if endpoint != "" {
			config["endpoint"] = endpoint
		}

		c.Backends[name] = BackendConfig{
			Type:   BackendTypeS3,
			Config: config,
		}
	}
}

// WithPresignedURLs enables/disables presigned URLs.
func WithPresignedURLs(enabled bool, expiry time.Duration) ConfigOption {
	return func(c *Config) {
		c.EnablePresignedURLs = enabled
		if expiry > 0 {
			c.PresignExpiry = expiry
		}
	}
}

// WithMaxUploadSize sets the maximum upload size.
func WithMaxUploadSize(size int64) ConfigOption {
	return func(c *Config) { c.MaxUploadSize = size }
}

// WithChunkSize sets the chunk size for multipart uploads.
func WithChunkSize(size int) ConfigOption {
	return func(c *Config) { c.ChunkSize = size }
}

// WithCDN enables CDN with the specified base URL.
func WithCDN(enabled bool, baseURL string) ConfigOption {
	return func(c *Config) {
		c.EnableCDN = enabled
		c.CDNBaseURL = baseURL
	}
}

// WithResilience sets the resilience configuration.
func WithResilience(resilience ResilienceConfig) ConfigOption {
	return func(c *Config) { c.Resilience = resilience }
}

// WithEnhancedBackend enables/disables enhanced backend features (locking, pooling, etc.).
func WithEnhancedBackend(enabled bool) ConfigOption {
	return func(c *Config) { c.UseEnhancedBackend = enabled }
}

// WithRequireConfig requires config from YAML.
func WithRequireConfig(require bool) ConfigOption {
	return func(c *Config) { c.RequireConfig = require }
}

// WithConfig replaces the entire config.
func WithConfig(config Config) ConfigOption {
	return func(c *Config) { *c = config }
}
