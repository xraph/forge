package storage

import "time"

// Config is the storage extension configuration
type Config struct {
	// Default backend name
	Default string `yaml:"default" json:"default" default:"local"`

	// Backend configurations
	Backends map[string]BackendConfig `yaml:"backends" json:"backends"`

	// Features
	EnablePresignedURLs bool          `yaml:"enable_presigned_urls" json:"enable_presigned_urls" default:"true"`
	PresignExpiry       time.Duration `yaml:"presign_expiry" json:"presign_expiry" default:"15m"`
	MaxUploadSize       int64         `yaml:"max_upload_size" json:"max_upload_size" default:"5368709120"` // 5GB
	ChunkSize           int           `yaml:"chunk_size" json:"chunk_size" default:"5242880"`              // 5MB

	// CDN
	EnableCDN  bool   `yaml:"enable_cdn" json:"enable_cdn" default:"false"`
	CDNBaseURL string `yaml:"cdn_base_url" json:"cdn_base_url"`
}

// BackendConfig is the configuration for a storage backend
type BackendConfig struct {
	Type   string                 `yaml:"type" json:"type"` // local, s3, gcs, azure
	Config map[string]interface{} `yaml:"config" json:"config"`
}

// DefaultConfig returns the default storage configuration
func DefaultConfig() Config {
	return Config{
		Default: "local",
		Backends: map[string]BackendConfig{
			"local": {
				Type: "local",
				Config: map[string]interface{}{
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
	}
}

// Validate validates the configuration
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
				Config: make(map[string]interface{}),
			}
		}
	}

	return nil
}
