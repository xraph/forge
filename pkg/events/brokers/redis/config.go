package redis

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
	eventsCore "github.com/xraph/forge/pkg/events/core"
)

// RedisConfig defines configuration for Redis broker
type RedisConfig struct {
	// Connection settings
	Addresses  []string `yaml:"addresses" json:"addresses"`
	Username   string   `yaml:"username" json:"username"`
	Password   string   `yaml:"password" json:"password"`
	Database   int      `yaml:"database" json:"database"`
	MasterName string   `yaml:"master_name" json:"master_name"` // For sentinel mode

	// Connection pool settings
	PoolSize        int           `yaml:"pool_size" json:"pool_size"`
	MinIdleConns    int           `yaml:"min_idle_conns" json:"min_idle_conns"`
	MaxIdleConns    int           `yaml:"max_idle_conns" json:"max_idle_conns"`
	ConnMaxIdleTime time.Duration `yaml:"conn_max_idle_time" json:"conn_max_idle_time"`
	ConnMaxLifetime time.Duration `yaml:"conn_max_lifetime" json:"conn_max_lifetime"`

	// Timeout settings
	DialTimeout  time.Duration `yaml:"dial_timeout" json:"dial_timeout"`
	ReadTimeout  time.Duration `yaml:"read_timeout" json:"read_timeout"`
	WriteTimeout time.Duration `yaml:"write_timeout" json:"write_timeout"`

	// Retry settings
	MaxRetries      int           `yaml:"max_retries" json:"max_retries"`
	MinRetryBackoff time.Duration `yaml:"min_retry_backoff" json:"min_retry_backoff"`
	MaxRetryBackoff time.Duration `yaml:"max_retry_backoff" json:"max_retry_backoff"`

	// TLS settings
	EnableTLS     bool   `yaml:"enable_tls" json:"enable_tls"`
	TLSCertFile   string `yaml:"tls_cert_file" json:"tls_cert_file"`
	TLSKeyFile    string `yaml:"tls_key_file" json:"tls_key_file"`
	TLSCAFile     string `yaml:"tls_ca_file" json:"tls_ca_file"`
	TLSSkipVerify bool   `yaml:"tls_skip_verify" json:"tls_skip_verify"`

	// Pub/Sub settings
	ChannelSize   int           `yaml:"channel_size" json:"channel_size"`
	Pattern       bool          `yaml:"pattern" json:"pattern"`
	PingTimeout   time.Duration `yaml:"ping_timeout" json:"ping_timeout"`
	PingFrequency time.Duration `yaml:"ping_frequency" json:"ping_frequency"`

	// Redis Streams settings (optional)
	EnableStreams bool   `yaml:"enable_streams" json:"enable_streams"`
	StreamMaxLen  int64  `yaml:"stream_max_len" json:"stream_max_len"`
	ConsumerGroup string `yaml:"consumer_group" json:"consumer_group"`
	ConsumerName  string `yaml:"consumer_name" json:"consumer_name"`
}

// RedisSubscription wraps a Redis pub/sub subscription
type RedisSubscription struct {
	pubsub   *redis.PubSub
	channel  string
	handlers []eventsCore.EventHandler
	cancel   context.CancelFunc
	broker   *RedisBroker
}

// DefaultRedisConfig returns default Redis configuration
func DefaultRedisConfig() *RedisConfig {
	return &RedisConfig{
		Addresses:       []string{"localhost:6379"},
		Database:        0,
		PoolSize:        10,
		MinIdleConns:    5,
		MaxIdleConns:    10,
		ConnMaxIdleTime: time.Minute * 30,
		ConnMaxLifetime: time.Hour,
		DialTimeout:     time.Second * 5,
		ReadTimeout:     time.Second * 3,
		WriteTimeout:    time.Second * 3,
		MaxRetries:      3,
		MinRetryBackoff: time.Millisecond * 8,
		MaxRetryBackoff: time.Millisecond * 512,
		ChannelSize:     100,
		Pattern:         false,
		PingTimeout:     time.Second * 30,
		PingFrequency:   time.Second * 10,
		EnableStreams:   false,
		StreamMaxLen:    10000,
		ConsumerGroup:   "forge-events",
		ConsumerName:    "consumer-1",
	}
}

// parseRedisConfig parses configuration map into RedisConfig
func parseRedisConfig(config map[string]interface{}, redisConfig *RedisConfig) error {
	// This is a simplified parser - in practice, you'd use a proper configuration library
	if addresses, ok := config["addresses"].([]interface{}); ok {
		redisConfig.Addresses = make([]string, len(addresses))
		for i, addr := range addresses {
			if addrStr, ok := addr.(string); ok {
				redisConfig.Addresses[i] = addrStr
			}
		}
	} else if addr, ok := config["address"].(string); ok {
		redisConfig.Addresses = []string{addr}
	}

	if username, ok := config["username"].(string); ok {
		redisConfig.Username = username
	}
	if password, ok := config["password"].(string); ok {
		redisConfig.Password = password
	}
	if database, ok := config["database"].(int); ok {
		redisConfig.Database = database
	}
	if masterName, ok := config["master_name"].(string); ok {
		redisConfig.MasterName = masterName
	}
	if poolSize, ok := config["pool_size"].(int); ok {
		redisConfig.PoolSize = poolSize
	}
	if enableStreams, ok := config["enable_streams"].(bool); ok {
		redisConfig.EnableStreams = enableStreams
	}
	if consumerGroup, ok := config["consumer_group"].(string); ok {
		redisConfig.ConsumerGroup = consumerGroup
	}
	if consumerName, ok := config["consumer_name"].(string); ok {
		redisConfig.ConsumerName = consumerName
	}

	return nil
}
