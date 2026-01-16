package hls

import (
	"errors"
	"time"
)

// DI container keys for HLS extension services.
const (
	// ServiceKey is the DI key for the HLS service.
	ServiceKey = "hls"
)

// Config holds HLS extension configuration
type Config struct {
	// Server configuration
	Enabled  bool   `mapstructure:"enabled" yaml:"enabled" env:"HLS_ENABLED"`
	BasePath string `mapstructure:"base_path" yaml:"base_path" env:"HLS_BASE_PATH"`
	BaseURL  string `mapstructure:"base_url" yaml:"base_url" env:"HLS_BASE_URL"`

	// Storage configuration
	StorageBackend string `mapstructure:"storage_backend" yaml:"storage_backend" env:"HLS_STORAGE_BACKEND"`
	StoragePrefix  string `mapstructure:"storage_prefix" yaml:"storage_prefix" env:"HLS_STORAGE_PREFIX"`

	// Stream configuration
	TargetDuration int   `mapstructure:"target_duration" yaml:"target_duration" env:"HLS_TARGET_DURATION"`
	DVRWindowSize  int   `mapstructure:"dvr_window_size" yaml:"dvr_window_size" env:"HLS_DVR_WINDOW_SIZE"`
	MaxSegmentSize int64 `mapstructure:"max_segment_size" yaml:"max_segment_size" env:"HLS_MAX_SEGMENT_SIZE"`

	// Transcoding configuration
	EnableTranscoding       bool               `mapstructure:"enable_transcoding" yaml:"enable_transcoding" env:"HLS_ENABLE_TRANSCODING"`
	TranscodeProfiles       []TranscodeProfile `mapstructure:"transcode_profiles" yaml:"transcode_profiles"`
	FFmpegPath              string             `mapstructure:"ffmpeg_path" yaml:"ffmpeg_path" env:"HLS_FFMPEG_PATH"`
	FFprobePath             string             `mapstructure:"ffprobe_path" yaml:"ffprobe_path" env:"HLS_FFPROBE_PATH"`
	MaxConcurrentTranscodes int                `mapstructure:"max_concurrent_transcodes" yaml:"max_concurrent_transcodes" env:"HLS_MAX_CONCURRENT_TRANSCODES"`

	// Distributed configuration (requires consensus extension)
	EnableDistributed bool   `mapstructure:"enable_distributed" yaml:"enable_distributed" env:"HLS_ENABLE_DISTRIBUTED"`
	NodeID            string `mapstructure:"node_id" yaml:"node_id" env:"HLS_NODE_ID"`
	ClusterID         string `mapstructure:"cluster_id" yaml:"cluster_id" env:"HLS_CLUSTER_ID"`

	// Distributed behavior
	RedirectToLeader bool `mapstructure:"redirect_to_leader" yaml:"redirect_to_leader" env:"HLS_REDIRECT_TO_LEADER"`
	RedirectToOwner  bool `mapstructure:"redirect_to_owner" yaml:"redirect_to_owner" env:"HLS_REDIRECT_TO_OWNER"`
	EnableFailover   bool `mapstructure:"enable_failover" yaml:"enable_failover" env:"HLS_ENABLE_FAILOVER"`

	// Cleanup configuration
	SegmentRetention  time.Duration `mapstructure:"segment_retention" yaml:"segment_retention" env:"HLS_SEGMENT_RETENTION"`
	CleanupInterval   time.Duration `mapstructure:"cleanup_interval" yaml:"cleanup_interval" env:"HLS_CLEANUP_INTERVAL"`
	EnableAutoCleanup bool          `mapstructure:"enable_auto_cleanup" yaml:"enable_auto_cleanup" env:"HLS_ENABLE_AUTO_CLEANUP"`

	// Security
	RequireAuth    bool     `mapstructure:"require_auth" yaml:"require_auth" env:"HLS_REQUIRE_AUTH"`
	AllowedOrigins []string `mapstructure:"allowed_origins" yaml:"allowed_origins" env:"HLS_ALLOWED_ORIGINS"`
	EnableCORS     bool     `mapstructure:"enable_cors" yaml:"enable_cors" env:"HLS_ENABLE_CORS"`

	// Limits
	MaxStreams            int   `mapstructure:"max_streams" yaml:"max_streams" env:"HLS_MAX_STREAMS"`
	MaxViewersPerStream   int   `mapstructure:"max_viewers_per_stream" yaml:"max_viewers_per_stream" env:"HLS_MAX_VIEWERS_PER_STREAM"`
	MaxBandwidthPerStream int64 `mapstructure:"max_bandwidth_per_stream" yaml:"max_bandwidth_per_stream" env:"HLS_MAX_BANDWIDTH_PER_STREAM"`

	// Cache configuration
	EnableCaching bool          `mapstructure:"enable_caching" yaml:"enable_caching" env:"HLS_ENABLE_CACHING"`
	CacheTTL      time.Duration `mapstructure:"cache_ttl" yaml:"cache_ttl" env:"HLS_CACHE_TTL"`

	// Config loading
	RequireConfig bool `mapstructure:"-" yaml:"-"`
}

// DefaultConfig returns default HLS configuration
func DefaultConfig() Config {
	return Config{
		Enabled:  true,
		BasePath: "/hls",
		BaseURL:  "http://localhost:8080/hls",

		StorageBackend: "default", // Use default storage backend from forge storage extension
		StoragePrefix:  "hls",

		TargetDuration: 6,                // 6 second segments
		DVRWindowSize:  10,               // Keep last 10 segments for DVR
		MaxSegmentSize: 10 * 1024 * 1024, // 10MB

		EnableTranscoding:       true,
		TranscodeProfiles:       DefaultProfiles(),
		FFmpegPath:              "ffmpeg",
		FFprobePath:             "ffprobe",
		MaxConcurrentTranscodes: 4,

		EnableDistributed: false,
		ClusterID:         "hls-cluster",
		RedirectToLeader:  true,
		RedirectToOwner:   true,
		EnableFailover:    true,

		SegmentRetention:  24 * time.Hour,
		CleanupInterval:   1 * time.Hour,
		EnableAutoCleanup: true,

		RequireAuth:    false,
		AllowedOrigins: []string{"*"},
		EnableCORS:     true,

		MaxStreams:            100,
		MaxViewersPerStream:   10000,
		MaxBandwidthPerStream: 100 * 1024 * 1024, // 100 Mbps

		EnableCaching: true,
		CacheTTL:      5 * time.Minute,

		RequireConfig: false,
	}
}

// Validate validates the configuration
func (c *Config) Validate() error {
	if c.TargetDuration <= 0 {
		return errors.New("target_duration must be positive")
	}

	if c.DVRWindowSize < 0 {
		return errors.New("dvr_window_size cannot be negative")
	}

	if c.MaxSegmentSize <= 0 {
		return errors.New("max_segment_size must be positive")
	}

	// Storage backend validation removed - managed by forge storage extension

	if c.EnableTranscoding {
		if c.FFmpegPath == "" {
			return errors.New("ffmpeg_path is required when transcoding is enabled")
		}
		if c.MaxConcurrentTranscodes <= 0 {
			return errors.New("max_concurrent_transcodes must be positive")
		}
	}

	if c.MaxStreams <= 0 {
		return errors.New("max_streams must be positive")
	}

	if c.MaxViewersPerStream <= 0 {
		return errors.New("max_viewers_per_stream must be positive")
	}

	return nil
}

// ConfigOption is a functional option for Config
type ConfigOption func(*Config)

// WithConfig sets the entire configuration
func WithConfig(config Config) ConfigOption {
	return func(c *Config) {
		*c = config
	}
}

// WithBasePath sets the base HTTP path
func WithBasePath(path string) ConfigOption {
	return func(c *Config) {
		c.BasePath = path
	}
}

// WithBaseURL sets the base URL for generating segment URLs
func WithBaseURL(url string) ConfigOption {
	return func(c *Config) {
		c.BaseURL = url
	}
}

// WithStorageBackend configures the storage backend to use
func WithStorageBackend(backend string) ConfigOption {
	return func(c *Config) {
		c.StorageBackend = backend
	}
}

// WithStoragePrefix sets the storage prefix for HLS content
func WithStoragePrefix(prefix string) ConfigOption {
	return func(c *Config) {
		c.StoragePrefix = prefix
	}
}

// WithTargetDuration sets the segment duration
func WithTargetDuration(duration int) ConfigOption {
	return func(c *Config) {
		c.TargetDuration = duration
	}
}

// WithDVRWindow sets the DVR window size
func WithDVRWindow(size int) ConfigOption {
	return func(c *Config) {
		c.DVRWindowSize = size
	}
}

// WithTranscoding enables/disables transcoding
func WithTranscoding(enabled bool, profiles ...TranscodeProfile) ConfigOption {
	return func(c *Config) {
		c.EnableTranscoding = enabled
		if len(profiles) > 0 {
			c.TranscodeProfiles = profiles
		}
	}
}

// WithFFmpegPaths sets FFmpeg and FFprobe paths
func WithFFmpegPaths(ffmpegPath, ffprobePath string) ConfigOption {
	return func(c *Config) {
		c.FFmpegPath = ffmpegPath
		c.FFprobePath = ffprobePath
	}
}

// WithDistributed enables distributed streaming with consensus
func WithDistributed(enabled bool) ConfigOption {
	return func(c *Config) {
		c.EnableDistributed = enabled
	}
}

// WithNodeID sets the node ID for distributed streaming
func WithNodeID(nodeID string) ConfigOption {
	return func(c *Config) {
		c.NodeID = nodeID
	}
}

// WithClusterID sets the cluster ID for distributed streaming
func WithClusterID(clusterID string) ConfigOption {
	return func(c *Config) {
		c.ClusterID = clusterID
	}
}

// WithFailover enables automatic failover
func WithFailover(enabled bool) ConfigOption {
	return func(c *Config) {
		c.EnableFailover = enabled
	}
}

// WithCleanup configures automatic cleanup
func WithCleanup(retention, interval time.Duration) ConfigOption {
	return func(c *Config) {
		c.EnableAutoCleanup = true
		c.SegmentRetention = retention
		c.CleanupInterval = interval
	}
}

// WithAuth enables authentication
func WithAuth(required bool) ConfigOption {
	return func(c *Config) {
		c.RequireAuth = required
	}
}

// WithCORS configures CORS
func WithCORS(enabled bool, origins ...string) ConfigOption {
	return func(c *Config) {
		c.EnableCORS = enabled
		if len(origins) > 0 {
			c.AllowedOrigins = origins
		}
	}
}

// WithLimits sets resource limits
func WithLimits(maxStreams, maxViewersPerStream int, maxBandwidthPerStream int64) ConfigOption {
	return func(c *Config) {
		c.MaxStreams = maxStreams
		c.MaxViewersPerStream = maxViewersPerStream
		c.MaxBandwidthPerStream = maxBandwidthPerStream
	}
}

// WithCaching enables/disables caching
func WithCaching(enabled bool, ttl time.Duration) ConfigOption {
	return func(c *Config) {
		c.EnableCaching = enabled
		c.CacheTTL = ttl
	}
}

// WithRequireConfig sets whether config loading is required
func WithRequireConfig(require bool) ConfigOption {
	return func(c *Config) {
		c.RequireConfig = require
	}
}
