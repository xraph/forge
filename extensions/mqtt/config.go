package mqtt

import (
	"errors"
	"time"
)

// DI container keys for MQTT extension services.
const (
	// ServiceKey is the DI key for the MQTT service.
	ServiceKey = "mqtt"
)

// Config contains configuration for the MQTT extension.
type Config struct {
	// Connection settings
	Broker            string        `json:"broker"              mapstructure:"broker"              yaml:"broker"`
	ClientID          string        `json:"client_id"           mapstructure:"client_id"           yaml:"client_id"`
	Username          string        `json:"username,omitempty"  mapstructure:"username"            yaml:"username,omitempty"`
	Password          string        `json:"password,omitempty"  mapstructure:"password"            yaml:"password,omitempty"`
	CleanSession      bool          `json:"clean_session"       mapstructure:"clean_session"       yaml:"clean_session"`
	ConnectTimeout    time.Duration `json:"connect_timeout"     mapstructure:"connect_timeout"     yaml:"connect_timeout"`
	KeepAlive         time.Duration `json:"keep_alive"          mapstructure:"keep_alive"          yaml:"keep_alive"`
	PingTimeout       time.Duration `json:"ping_timeout"        mapstructure:"ping_timeout"        yaml:"ping_timeout"`
	MaxReconnectDelay time.Duration `json:"max_reconnect_delay" mapstructure:"max_reconnect_delay" yaml:"max_reconnect_delay"`

	// TLS/SSL
	EnableTLS     bool   `json:"enable_tls"              mapstructure:"enable_tls"      yaml:"enable_tls"`
	TLSCertFile   string `json:"tls_cert_file,omitempty" mapstructure:"tls_cert_file"   yaml:"tls_cert_file,omitempty"`
	TLSKeyFile    string `json:"tls_key_file,omitempty"  mapstructure:"tls_key_file"    yaml:"tls_key_file,omitempty"`
	TLSCAFile     string `json:"tls_ca_file,omitempty"   mapstructure:"tls_ca_file"     yaml:"tls_ca_file,omitempty"`
	TLSSkipVerify bool   `json:"tls_skip_verify"         mapstructure:"tls_skip_verify" yaml:"tls_skip_verify"`

	// QoS settings
	DefaultQoS byte `json:"default_qos" mapstructure:"default_qos" yaml:"default_qos"`

	// Retry and reliability
	AutoReconnect        bool          `json:"auto_reconnect"         mapstructure:"auto_reconnect"         yaml:"auto_reconnect"`
	ResumeSubs           bool          `json:"resume_subs"            mapstructure:"resume_subs"            yaml:"resume_subs"`
	MaxReconnectAttempts int           `json:"max_reconnect_attempts" mapstructure:"max_reconnect_attempts" yaml:"max_reconnect_attempts"`
	WriteTimeout         time.Duration `json:"write_timeout"          mapstructure:"write_timeout"          yaml:"write_timeout"`

	// Message handling
	MessageChannelDepth uint   `json:"message_channel_depth"     mapstructure:"message_channel_depth" yaml:"message_channel_depth"`
	OrderMatters        bool   `json:"order_matters"             mapstructure:"order_matters"         yaml:"order_matters"`
	MessageStore        string `json:"message_store"             mapstructure:"message_store"         yaml:"message_store"` // "memory", "file"
	StoreDirectory      string `json:"store_directory,omitempty" mapstructure:"store_directory"       yaml:"store_directory,omitempty"`

	// Last Will and Testament
	WillEnabled  bool   `json:"will_enabled"           mapstructure:"will_enabled"  yaml:"will_enabled"`
	WillTopic    string `json:"will_topic,omitempty"   mapstructure:"will_topic"    yaml:"will_topic,omitempty"`
	WillPayload  string `json:"will_payload,omitempty" mapstructure:"will_payload"  yaml:"will_payload,omitempty"`
	WillQoS      byte   `json:"will_qos"               mapstructure:"will_qos"      yaml:"will_qos"`
	WillRetained bool   `json:"will_retained"          mapstructure:"will_retained" yaml:"will_retained"`

	// Observability
	EnableMetrics bool `json:"enable_metrics" mapstructure:"enable_metrics" yaml:"enable_metrics"`
	// EnableTracing is ignored.
	//
	// Deprecated: MQTT 3.1.1 has no user properties, so this extension can't carry trace
	// context. The field does nothing. It stays so existing config keeps loading.
	EnableTracing bool `json:"enable_tracing" mapstructure:"enable_tracing" yaml:"enable_tracing"`
	EnableLogging bool `json:"enable_logging" mapstructure:"enable_logging" yaml:"enable_logging"`

	// Config loading flags
	RequireConfig bool `json:"-" mapstructure:"-" yaml:"-"`
}

// DefaultConfig returns default MQTT configuration.
func DefaultConfig() Config {
	return Config{
		Broker:               "tcp://localhost:1883",
		ClientID:             "forge-mqtt-client",
		CleanSession:         true,
		ConnectTimeout:       30 * time.Second,
		KeepAlive:            60 * time.Second,
		PingTimeout:          10 * time.Second,
		MaxReconnectDelay:    10 * time.Minute,
		EnableTLS:            false,
		TLSSkipVerify:        false,
		DefaultQoS:           1, // At least once
		AutoReconnect:        true,
		ResumeSubs:           true,
		MaxReconnectAttempts: 0, // Unlimited
		WriteTimeout:         30 * time.Second,
		MessageChannelDepth:  100,
		OrderMatters:         true,
		MessageStore:         "memory",
		WillEnabled:          false,
		WillQoS:              0,
		WillRetained:         false,
		EnableMetrics:        true,
		EnableTracing:        true,
		EnableLogging:        true,
		RequireConfig:        false,
	}
}

// Validate validates the configuration.
func (c *Config) Validate() error {
	if c.Broker == "" {
		return errors.New("broker is required")
	}

	if c.ClientID == "" {
		return errors.New("client_id is required")
	}

	if c.DefaultQoS > 2 {
		return errors.New("default_qos must be 0, 1, or 2")
	}

	if c.EnableTLS && !c.TLSSkipVerify {
		if c.TLSCertFile == "" || c.TLSKeyFile == "" {
			return errors.New("tls requires cert_file and key_file when not skipping verification")
		}
	}

	if c.WillEnabled {
		if c.WillTopic == "" {
			return errors.New("will_topic is required when will is enabled")
		}

		if c.WillQoS > 2 {
			return errors.New("will_qos must be 0, 1, or 2")
		}
	}

	if c.MessageStore != "memory" && c.MessageStore != "file" {
		return errors.New("message_store must be 'memory' or 'file'")
	}

	if c.MessageStore == "file" && c.StoreDirectory == "" {
		return errors.New("store_directory is required when message_store is 'file'")
	}

	return nil
}

// ConfigOption is a functional option for Config.
type ConfigOption func(*Config)

func WithBroker(broker string) ConfigOption {
	return func(c *Config) { c.Broker = broker }
}

func WithClientID(clientID string) ConfigOption {
	return func(c *Config) { c.ClientID = clientID }
}

func WithCredentials(username, password string) ConfigOption {
	return func(c *Config) {
		c.Username = username
		c.Password = password
	}
}

func WithTLS(certFile, keyFile, caFile string, skipVerify bool) ConfigOption {
	return func(c *Config) {
		c.EnableTLS = true
		c.TLSCertFile = certFile
		c.TLSKeyFile = keyFile
		c.TLSCAFile = caFile
		c.TLSSkipVerify = skipVerify
	}
}

func WithQoS(qos byte) ConfigOption {
	return func(c *Config) { c.DefaultQoS = qos }
}

func WithCleanSession(clean bool) ConfigOption {
	return func(c *Config) { c.CleanSession = clean }
}

func WithKeepAlive(duration time.Duration) ConfigOption {
	return func(c *Config) { c.KeepAlive = duration }
}

func WithAutoReconnect(enable bool) ConfigOption {
	return func(c *Config) { c.AutoReconnect = enable }
}

func WithWill(topic, payload string, qos byte, retained bool) ConfigOption {
	return func(c *Config) {
		c.WillEnabled = true
		c.WillTopic = topic
		c.WillPayload = payload
		c.WillQoS = qos
		c.WillRetained = retained
	}
}

func WithMetrics(enable bool) ConfigOption {
	return func(c *Config) { c.EnableMetrics = enable }
}

// WithTracing sets EnableTracing.
//
// Deprecated: MQTT 3.1.1 has no user properties, so this extension can't carry trace
// context. The option does nothing. It stays so existing code keeps compiling.
func WithTracing(enable bool) ConfigOption {
	return func(c *Config) { c.EnableTracing = enable }
}

func WithRequireConfig(require bool) ConfigOption {
	return func(c *Config) { c.RequireConfig = require }
}

func WithConfig(config Config) ConfigOption {
	return func(c *Config) { *c = config }
}
