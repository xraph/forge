package grpc

import (
	"errors"
	"time"
)

// DI container keys for gRPC extension services.
const (
	// ServiceKey is the DI key for the gRPC service.
	ServiceKey = "grpc"
)

// Config contains configuration for the gRPC extension.
type Config struct {
	// Server settings
	Address              string          `json:"address"                mapstructure:"address"                yaml:"address"`
	MaxRecvMsgSize       int             `json:"max_recv_msg_size"      mapstructure:"max_recv_msg_size"      yaml:"max_recv_msg_size"`
	MaxSendMsgSize       int             `json:"max_send_msg_size"      mapstructure:"max_send_msg_size"      yaml:"max_send_msg_size"`
	MaxConcurrentStreams uint32          `json:"max_concurrent_streams" mapstructure:"max_concurrent_streams" yaml:"max_concurrent_streams"`
	ConnectionTimeout    time.Duration   `json:"connection_timeout"     mapstructure:"connection_timeout"     yaml:"connection_timeout"`
	Keepalive            KeepaliveConfig `json:"keepalive"              mapstructure:"keepalive"              yaml:"keepalive"`

	// TLS/mTLS
	EnableTLS   bool   `json:"enable_tls"              mapstructure:"enable_tls"    yaml:"enable_tls"`
	TLSCertFile string `json:"tls_cert_file,omitempty" mapstructure:"tls_cert_file" yaml:"tls_cert_file,omitempty"`
	TLSKeyFile  string `json:"tls_key_file,omitempty"  mapstructure:"tls_key_file"  yaml:"tls_key_file,omitempty"`
	TLSCAFile   string `json:"tls_ca_file,omitempty"   mapstructure:"tls_ca_file"   yaml:"tls_ca_file,omitempty"`
	ClientAuth  bool   `json:"client_auth"             mapstructure:"client_auth"   yaml:"client_auth"` // Require client cert

	// Health checking
	EnableHealthCheck bool `json:"enable_health_check" mapstructure:"enable_health_check" yaml:"enable_health_check"`

	// Reflection
	EnableReflection bool `json:"enable_reflection" mapstructure:"enable_reflection" yaml:"enable_reflection"`

	// Observability
	EnableMetrics bool `json:"enable_metrics" mapstructure:"enable_metrics" yaml:"enable_metrics"`
	EnableTracing bool `json:"enable_tracing" mapstructure:"enable_tracing" yaml:"enable_tracing"`
	EnableLogging bool `json:"enable_logging" mapstructure:"enable_logging" yaml:"enable_logging"`

	// Config loading flags
	RequireConfig bool `json:"-" mapstructure:"-" yaml:"-"`
}

// KeepaliveConfig contains keepalive settings.
type KeepaliveConfig struct {
	Time                time.Duration `json:"time"                  mapstructure:"time"                  yaml:"time"`
	Timeout             time.Duration `json:"timeout"               mapstructure:"timeout"               yaml:"timeout"`
	EnforcementPolicy   bool          `json:"enforcement_policy"    mapstructure:"enforcement_policy"    yaml:"enforcement_policy"`
	MinTime             time.Duration `json:"min_time"              mapstructure:"min_time"              yaml:"min_time"`
	PermitWithoutStream bool          `json:"permit_without_stream" mapstructure:"permit_without_stream" yaml:"permit_without_stream"`
}

// DefaultConfig returns default gRPC configuration.
func DefaultConfig() Config {
	return Config{
		Address:              ":50051",
		MaxRecvMsgSize:       4 * 1024 * 1024, // 4MB
		MaxSendMsgSize:       4 * 1024 * 1024, // 4MB
		MaxConcurrentStreams: 0,               // Unlimited
		ConnectionTimeout:    120 * time.Second,
		Keepalive: KeepaliveConfig{
			Time:                2 * time.Hour,
			Timeout:             20 * time.Second,
			EnforcementPolicy:   true,
			MinTime:             5 * time.Minute,
			PermitWithoutStream: false,
		},
		EnableTLS:         false,
		ClientAuth:        false,
		EnableHealthCheck: true,
		EnableReflection:  true,
		EnableMetrics:     true,
		EnableTracing:     true,
		EnableLogging:     true,
		RequireConfig:     false,
	}
}

// Validate validates the configuration.
func (c *Config) Validate() error {
	if c.Address == "" {
		return errors.New("address is required")
	}

	if c.MaxRecvMsgSize < 0 {
		return errors.New("max_recv_msg_size must be non-negative")
	}

	if c.MaxSendMsgSize < 0 {
		return errors.New("max_send_msg_size must be non-negative")
	}

	if c.EnableTLS {
		if c.TLSCertFile == "" || c.TLSKeyFile == "" {
			return errors.New("tls requires cert_file and key_file")
		}

		if c.ClientAuth && c.TLSCAFile == "" {
			return errors.New("client_auth requires ca_file")
		}
	}

	return nil
}

// ConfigOption is a functional option for Config.
type ConfigOption func(*Config)

func WithAddress(addr string) ConfigOption {
	return func(c *Config) { c.Address = addr }
}

func WithMaxMessageSize(size int) ConfigOption {
	return func(c *Config) {
		c.MaxRecvMsgSize = size
		c.MaxSendMsgSize = size
	}
}

func WithMaxConcurrentStreams(maxStreams uint32) ConfigOption {
	return func(c *Config) { c.MaxConcurrentStreams = maxStreams }
}

func WithTLS(certFile, keyFile, caFile string) ConfigOption {
	return func(c *Config) {
		c.EnableTLS = true
		c.TLSCertFile = certFile
		c.TLSKeyFile = keyFile
		c.TLSCAFile = caFile
	}
}

func WithClientAuth(enable bool) ConfigOption {
	return func(c *Config) { c.ClientAuth = enable }
}

func WithHealthCheck(enable bool) ConfigOption {
	return func(c *Config) { c.EnableHealthCheck = enable }
}

func WithReflection(enable bool) ConfigOption {
	return func(c *Config) { c.EnableReflection = enable }
}

func WithMetrics(enable bool) ConfigOption {
	return func(c *Config) { c.EnableMetrics = enable }
}

func WithTracing(enable bool) ConfigOption {
	return func(c *Config) { c.EnableTracing = enable }
}

func WithRequireConfig(require bool) ConfigOption {
	return func(c *Config) { c.RequireConfig = require }
}

func WithConfig(config Config) ConfigOption {
	return func(c *Config) { *c = config }
}
