package queue

import (
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	if config.Driver != "inmemory" {
		t.Errorf("expected driver 'inmemory', got '%s'", config.Driver)
	}

	if config.MaxConnections != 10 {
		t.Errorf("expected max_connections 10, got %d", config.MaxConnections)
	}

	if config.DefaultPrefetch != 10 {
		t.Errorf("expected default_prefetch 10, got %d", config.DefaultPrefetch)
	}

	if !config.EnableMetrics {
		t.Error("expected metrics enabled by default")
	}

	if !config.EnableTracing {
		t.Error("expected tracing enabled by default")
	}
}

func TestConfigValidate_Valid(t *testing.T) {
	config := DefaultConfig()

	err := config.Validate()
	if err != nil {
		t.Fatalf("valid config should not error: %v", err)
	}
}

func TestConfigValidate_MissingDriver(t *testing.T) {
	config := DefaultConfig()
	config.Driver = ""

	err := config.Validate()
	if err == nil {
		t.Fatal("expected error for missing driver")
	}
}

func TestConfigValidate_UnsupportedDriver(t *testing.T) {
	config := DefaultConfig()
	config.Driver = "invalid"

	err := config.Validate()
	if err == nil {
		t.Fatal("expected error for unsupported driver")
	}
}

func TestConfigValidate_RedisMissingURL(t *testing.T) {
	config := DefaultConfig()
	config.Driver = "redis"
	config.URL = ""
	config.Hosts = []string{}

	err := config.Validate()
	if err == nil {
		t.Fatal("expected error for redis without URL or hosts")
	}
}

func TestConfigValidate_RedisWithHosts(t *testing.T) {
	config := DefaultConfig()
	config.Driver = "redis"
	config.Hosts = []string{"host1", "host2"}

	err := config.Validate()
	if err != nil {
		t.Fatalf("redis with hosts should be valid: %v", err)
	}
}

func TestConfigValidate_RabbitMQMissingURL(t *testing.T) {
	config := DefaultConfig()
	config.Driver = "rabbitmq"
	config.URL = ""
	config.Hosts = []string{}

	err := config.Validate()
	if err == nil {
		t.Fatal("expected error for rabbitmq without URL or hosts")
	}
}

func TestConfigValidate_RabbitMQWithURL(t *testing.T) {
	config := DefaultConfig()
	config.Driver = "rabbitmq"
	config.URL = "amqp://localhost:5672"

	err := config.Validate()
	if err != nil {
		t.Fatalf("rabbitmq with URL should be valid: %v", err)
	}
}

func TestConfigValidate_NATSMissingURL(t *testing.T) {
	config := DefaultConfig()
	config.Driver = "nats"
	config.URL = ""
	config.Hosts = []string{}

	err := config.Validate()
	if err == nil {
		t.Fatal("expected error for nats without URL or hosts")
	}
}

func TestConfigValidate_NATSWithURL(t *testing.T) {
	config := DefaultConfig()
	config.Driver = "nats"
	config.URL = "nats://localhost:4222"

	err := config.Validate()
	if err != nil {
		t.Fatalf("nats with URL should be valid: %v", err)
	}
}

func TestConfigValidate_InvalidMaxConnections(t *testing.T) {
	config := DefaultConfig()
	config.MaxConnections = 0

	err := config.Validate()
	if err == nil {
		t.Fatal("expected error for max_connections = 0")
	}

	config.MaxConnections = -1

	err = config.Validate()
	if err == nil {
		t.Fatal("expected error for max_connections < 0")
	}
}

func TestConfigValidate_InvalidMaxIdleConnections(t *testing.T) {
	config := DefaultConfig()
	config.MaxIdleConnections = -1

	err := config.Validate()
	if err == nil {
		t.Fatal("expected error for max_idle_connections < 0")
	}
}

func TestConfigValidate_MaxIdleExceedsMax(t *testing.T) {
	config := DefaultConfig()
	config.MaxConnections = 10
	config.MaxIdleConnections = 20

	err := config.Validate()
	if err == nil {
		t.Fatal("expected error for max_idle > max_connections")
	}
}

func TestConfigValidate_InvalidConnectTimeout(t *testing.T) {
	config := DefaultConfig()
	config.ConnectTimeout = 0

	err := config.Validate()
	if err == nil {
		t.Fatal("expected error for connect_timeout = 0")
	}

	config.ConnectTimeout = -1 * time.Second

	err = config.Validate()
	if err == nil {
		t.Fatal("expected error for connect_timeout < 0")
	}
}

func TestConfigValidate_InvalidDefaultPrefetch(t *testing.T) {
	config := DefaultConfig()
	config.DefaultPrefetch = -1

	err := config.Validate()
	if err == nil {
		t.Fatal("expected error for default_prefetch < 0")
	}
}

func TestConfigValidate_InvalidDefaultConcurrency(t *testing.T) {
	config := DefaultConfig()
	config.DefaultConcurrency = 0

	err := config.Validate()
	if err == nil {
		t.Fatal("expected error for default_concurrency = 0")
	}

	config.DefaultConcurrency = -1

	err = config.Validate()
	if err == nil {
		t.Fatal("expected error for default_concurrency < 0")
	}
}

func TestConfigValidate_InvalidMaxMessageSize(t *testing.T) {
	config := DefaultConfig()
	config.MaxMessageSize = 0

	err := config.Validate()
	if err == nil {
		t.Fatal("expected error for max_message_size = 0")
	}

	config.MaxMessageSize = -1

	err = config.Validate()
	if err == nil {
		t.Fatal("expected error for max_message_size < 0")
	}
}

func TestConfigOptions_MultipleOptions(t *testing.T) {
	config := DefaultConfig()

	opts := []ConfigOption{
		WithDriver("rabbitmq"),
		WithURL("amqp://localhost:5672"),
		WithAuth("user", "pass"),
		WithVHost("/test"),
		WithMaxConnections(20),
		WithPrefetch(50),
		WithConcurrency(10),
		WithTimeout(60 * time.Second),
		WithDeadLetter(false),
		WithPersistence(false),
		WithPriority(true),
		WithDelayed(true),
		WithMetrics(false),
		WithTracing(false),
	}

	for _, opt := range opts {
		opt(&config)
	}

	if config.Driver != "rabbitmq" {
		t.Error("driver not set")
	}

	if config.URL != "amqp://localhost:5672" {
		t.Error("url not set")
	}

	if config.Username != "user" || config.Password != "pass" {
		t.Error("auth not set")
	}

	if config.VHost != "/test" {
		t.Error("vhost not set")
	}

	if config.MaxConnections != 20 {
		t.Error("max_connections not set")
	}

	if config.DefaultPrefetch != 50 {
		t.Error("prefetch not set")
	}

	if config.DefaultConcurrency != 10 {
		t.Error("concurrency not set")
	}

	if config.DefaultTimeout != 60*time.Second {
		t.Error("timeout not set")
	}

	if config.EnableDeadLetter {
		t.Error("dead letter should be disabled")
	}

	if config.EnablePersistence {
		t.Error("persistence should be disabled")
	}

	if !config.EnablePriority {
		t.Error("priority should be enabled")
	}

	if !config.EnableDelayed {
		t.Error("delayed should be enabled")
	}

	if config.EnableMetrics {
		t.Error("metrics should be disabled")
	}

	if config.EnableTracing {
		t.Error("tracing should be disabled")
	}
}

func TestConfigOption_WithTLS(t *testing.T) {
	config := DefaultConfig()
	WithTLS("cert.pem", "key.pem", "ca.pem")(&config)

	if !config.EnableTLS {
		t.Error("tls not enabled")
	}

	if config.TLSCertFile != "cert.pem" {
		t.Error("cert file not set")
	}

	if config.TLSKeyFile != "key.pem" {
		t.Error("key file not set")
	}

	if config.TLSCAFile != "ca.pem" {
		t.Error("ca file not set")
	}
}
