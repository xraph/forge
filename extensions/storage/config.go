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
