package search

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

	if config.DefaultLimit != 20 {
		t.Errorf("expected default_limit 20, got %d", config.DefaultLimit)
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

func TestConfigValidate_ElasticsearchMissingURL(t *testing.T) {
	config := DefaultConfig()
	config.Driver = "elasticsearch"
	config.URL = ""
	config.Hosts = []string{}

	err := config.Validate()
	if err == nil {
		t.Fatal("expected error for elasticsearch without URL or hosts")
	}
}

func TestConfigValidate_ElasticsearchWithHosts(t *testing.T) {
	config := DefaultConfig()
	config.Driver = "elasticsearch"
	config.Hosts = []string{"host1", "host2"}

	err := config.Validate()
	if err != nil {
		t.Fatalf("elasticsearch with hosts should be valid: %v", err)
	}
}

func TestConfigValidate_MeilisearchMissingURL(t *testing.T) {
	config := DefaultConfig()
	config.Driver = "meilisearch"
	config.URL = ""

	err := config.Validate()
	if err == nil {
		t.Fatal("expected error for meilisearch without URL")
	}
}

func TestConfigValidate_MeilisearchWithURL(t *testing.T) {
	config := DefaultConfig()
	config.Driver = "meilisearch"
	config.URL = "http://localhost:7700"

	err := config.Validate()
	if err != nil {
		t.Fatalf("meilisearch with URL should be valid: %v", err)
	}
}

func TestConfigValidate_TypesenseMissingURL(t *testing.T) {
	config := DefaultConfig()
	config.Driver = "typesense"
	config.URL = ""
	config.Hosts = []string{}
	config.APIKey = "key"

	err := config.Validate()
	if err == nil {
		t.Fatal("expected error for typesense without URL or hosts")
	}
}

func TestConfigValidate_TypesenseMissingAPIKey(t *testing.T) {
	config := DefaultConfig()
	config.Driver = "typesense"
	config.URL = "http://localhost:8108"
	config.APIKey = ""

	err := config.Validate()
	if err == nil {
		t.Fatal("expected error for typesense without API key")
	}
}

func TestConfigValidate_TypesenseValid(t *testing.T) {
	config := DefaultConfig()
	config.Driver = "typesense"
	config.URL = "http://localhost:8108"
	config.APIKey = "test-key"

	err := config.Validate()
	if err != nil {
		t.Fatalf("typesense with URL and API key should be valid: %v", err)
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

func TestConfigValidate_InvalidRequestTimeout(t *testing.T) {
	config := DefaultConfig()
	config.RequestTimeout = 0

	err := config.Validate()
	if err == nil {
		t.Fatal("expected error for request_timeout = 0")
	}
}

func TestConfigValidate_InvalidDefaultLimit(t *testing.T) {
	config := DefaultConfig()
	config.DefaultLimit = 0

	err := config.Validate()
	if err == nil {
		t.Fatal("expected error for default_limit = 0")
	}

	config.DefaultLimit = -1

	err = config.Validate()
	if err == nil {
		t.Fatal("expected error for default_limit < 0")
	}
}

func TestConfigValidate_MaxLimitLessThanDefault(t *testing.T) {
	config := DefaultConfig()
	config.DefaultLimit = 50
	config.MaxLimit = 20

	err := config.Validate()
	if err == nil {
		t.Fatal("expected error for max_limit < default_limit")
	}
}

func TestConfigValidate_InvalidBulkSize(t *testing.T) {
	config := DefaultConfig()
	config.BulkSize = 0

	err := config.Validate()
	if err == nil {
		t.Fatal("expected error for bulk_size = 0")
	}
}

func TestConfigOptions_MultipleOptions(t *testing.T) {
	config := DefaultConfig()

	opts := []ConfigOption{
		WithDriver("elasticsearch"),
		WithURL("http://localhost:9200"),
		WithAuth("user", "pass"),
		WithMaxConnections(20),
		WithDefaultLimit(50),
		WithMaxLimit(200),
		WithMetrics(false),
		WithTracing(false),
	}

	for _, opt := range opts {
		opt(&config)
	}

	if config.Driver != "elasticsearch" {
		t.Error("driver not set")
	}

	if config.URL != "http://localhost:9200" {
		t.Error("url not set")
	}

	if config.Username != "user" || config.Password != "pass" {
		t.Error("auth not set")
	}

	if config.MaxConnections != 20 {
		t.Error("max_connections not set")
	}

	if config.DefaultLimit != 50 {
		t.Error("default_limit not set")
	}

	if config.MaxLimit != 200 {
		t.Error("max_limit not set")
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

func TestConfigOption_WithTimeout(t *testing.T) {
	config := DefaultConfig()
	timeout := 60 * time.Second
	WithTimeout(timeout)(&config)

	if config.RequestTimeout != timeout {
		t.Errorf("expected timeout %v, got %v", timeout, config.RequestTimeout)
	}
}
