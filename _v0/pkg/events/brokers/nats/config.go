package nats

import (
	"time"

	"github.com/nats-io/nats.go"
)

// NATSConfig defines configuration for NATS broker
type NATSConfig struct {
	URL               string        `yaml:"url" json:"url"`
	Name              string        `yaml:"name" json:"name"`
	Username          string        `yaml:"username" json:"username"`
	Password          string        `yaml:"password" json:"password"`
	Token             string        `yaml:"token" json:"token"`
	MaxReconnects     int           `yaml:"max_reconnects" json:"max_reconnects"`
	ReconnectWait     time.Duration `yaml:"reconnect_wait" json:"reconnect_wait"`
	ConnectTimeout    time.Duration `yaml:"connect_timeout" json:"connect_timeout"`
	PingInterval      time.Duration `yaml:"ping_interval" json:"ping_interval"`
	MaxPingsOut       int           `yaml:"max_pings_out" json:"max_pings_out"`
	EnableCompression bool          `yaml:"enable_compression" json:"enable_compression"`
	EnableTLS         bool          `yaml:"enable_tls" json:"enable_tls"`
	TLSConfig         *TLSConfig    `yaml:"tls_config" json:"tls_config"`
	JetStream         *JSConfig     `yaml:"jetstream" json:"jetstream"`
}

// TLSConfig defines TLS configuration for NATS
type TLSConfig struct {
	CertFile   string `yaml:"cert_file" json:"cert_file"`
	KeyFile    string `yaml:"key_file" json:"key_file"`
	CAFile     string `yaml:"ca_file" json:"ca_file"`
	SkipVerify bool   `yaml:"skip_verify" json:"skip_verify"`
}

// JSConfig defines JetStream configuration
type JSConfig struct {
	Enabled      bool          `yaml:"enabled" json:"enabled"`
	Domain       string        `yaml:"domain" json:"domain"`
	APIPrefix    string        `yaml:"api_prefix" json:"api_prefix"`
	Timeout      time.Duration `yaml:"timeout" json:"timeout"`
	StreamConfig *StreamConfig `yaml:"stream_config" json:"stream_config"`
}

// StreamConfig defines JetStream stream configuration
type StreamConfig struct {
	Name       string        `yaml:"name" json:"name"`
	Subjects   []string      `yaml:"subjects" json:"subjects"`
	Retention  string        `yaml:"retention" json:"retention"` // workqueue, limits, interest
	Storage    string        `yaml:"storage" json:"storage"`     // file, memory
	Replicas   int           `yaml:"replicas" json:"replicas"`
	MaxMsgs    int64         `yaml:"max_msgs" json:"max_msgs"`
	MaxBytes   int64         `yaml:"max_bytes" json:"max_bytes"`
	MaxAge     time.Duration `yaml:"max_age" json:"max_age"`
	MaxMsgSize int32         `yaml:"max_msg_size" json:"max_msg_size"`
	Discard    string        `yaml:"discard" json:"discard"` // old, new
}

// BrokerStats contains NATS broker statistics
type BrokerStats struct {
	Connected         bool          `json:"connected"`
	Subscriptions     int           `json:"subscriptions"`
	MessagesPublished int64         `json:"messages_published"`
	MessagesReceived  int64         `json:"messages_received"`
	PublishErrors     int64         `json:"publish_errors"`
	ReceiveErrors     int64         `json:"receive_errors"`
	ConnectionErrors  int64         `json:"connection_errors"`
	LastConnected     *time.Time    `json:"last_connected"`
	LastError         *time.Time    `json:"last_error"`
	TotalPublishTime  time.Duration `json:"total_publish_time"`
	AvgPublishTime    time.Duration `json:"avg_publish_time"`
}

// DefaultNATSConfig returns default NATS configuration
func DefaultNATSConfig() *NATSConfig {
	return &NATSConfig{
		URL:            nats.DefaultURL,
		Name:           "forge-events",
		MaxReconnects:  10,
		ReconnectWait:  time.Second * 2,
		ConnectTimeout: time.Second * 30,
		PingInterval:   time.Second * 20,
		MaxPingsOut:    2,
		JetStream: &JSConfig{
			Enabled: false,
			Timeout: time.Second * 5,
		},
	}
}

// parseNATSConfig parses configuration map into NATSConfig
func parseNATSConfig(config map[string]interface{}, natsConfig *NATSConfig) error {
	// This is a simplified parser - in practice, you'd use a proper configuration library
	if url, ok := config["url"].(string); ok {
		natsConfig.URL = url
	}
	if name, ok := config["name"].(string); ok {
		natsConfig.Name = name
	}
	if username, ok := config["username"].(string); ok {
		natsConfig.Username = username
	}
	if password, ok := config["password"].(string); ok {
		natsConfig.Password = password
	}
	if token, ok := config["token"].(string); ok {
		natsConfig.Token = token
	}
	if maxReconnects, ok := config["max_reconnects"].(int); ok {
		natsConfig.MaxReconnects = maxReconnects
	}

	return nil
}
