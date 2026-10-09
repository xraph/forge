// Package model is the provider-independent deployment graph.
package model

import (
	"github.com/xraph/forge/cmd/forge/internal/deploy/spec"
	"time"
)

type ResourceType string

const (
	Postgres      ResourceType = "postgres"
	MySQL         ResourceType = "mysql"
	SQLite        ResourceType = "sqlite"
	MongoDB       ResourceType = "mongodb"
	ClickHouse    ResourceType = "clickhouse"
	Turso         ResourceType = "turso"
	Redis         ResourceType = "redis"
	Memcached     ResourceType = "memcached"
	NATS          ResourceType = "nats"
	Kafka         ResourceType = "kafka"
	RabbitMQ      ResourceType = "rabbitmq"
	ObjectStorage ResourceType = "object-storage"
	SMTP          ResourceType = "smtp"
	MQTT          ResourceType = "mqtt"
	Meilisearch   ResourceType = "meilisearch"
	Elasticsearch ResourceType = "elasticsearch"
	Typesense     ResourceType = "typesense"
)

type Lifecycle = spec.Lifecycle // same three values
type Kind = spec.Kind
type Exposure = spec.Exposure

type Deployment struct {
	Project     string
	Environment string
	TargetName  string
	Target      spec.Target
	Registry    string
	Services    []Service
	Resources   []Resource
	Connections []Connection
	Secrets     []SecretRef
	Migrations  []Migration
	Routes      []Route
	Overlay     OverlayMode // file, inline, local-file-fallback
}

type OverlayMode string

const (
	OverlayFile     OverlayMode = "file"
	OverlayInline   OverlayMode = "inline"
	OverlayFallback OverlayMode = "config-local"
)

type Service struct {
	Name        string
	App         string
	Dir         string // absolute path to the app's main package
	MainPath    string // relative to project root, e.g. "cmd/api"
	Kind        Kind
	Image       Image
	Ports       []Port
	Health      Health
	Replicas    int
	Resources   spec.ResourceSpec
	ConfigFiles []ConfigFile // the overlay and any mounted config
	Bindings    []Binding
	Env         map[string]string
	Calls       []string
	Discovery   bool
	Migrate     []string // nil: none; the command when set
	Schedule    string
}

type Image struct {
	Repository string
	Tag        string
	Digest     string
	Dockerfile string // relative path when the user supplied one
}

type Port struct {
	Name     string
	Port     int
	Protocol string
	Exposure Exposure
}

type Health struct {
	Readiness string
	Liveness  string
	Startup   string
	Heartbeat bool
	None      bool
}

// ConfigFile matches ctrlplane's provider.ConfigFile field for field.
type ConfigFile struct {
	Name    string
	Path    string // mount path inside the container
	Format  string // "yaml"
	Content string
}

type Binding struct {
	Resource  string
	Extension string
	Instance  string            // grove database name, trove store, kv store
	Keys      map[string]string // config key -> value or ${VAR} reference
}

type Resource struct {
	Name      string
	Type      ResourceType
	Lifecycle Lifecycle
	Version   string
	Recipe    string
	Features  []string
	Bucket    string
	Secret    SecretRef
	UsedBy    []string
}

type Connection struct {
	From, To  string
	Port      string
	Address   string // target-specific, filled by the provider
	ConfigKey string
	EnvVar    string // "<TO>_URL"
	Timeout   time.Duration
	Retries   int
}

// SecretRef matches ctrlplane's provider.SecretRef in spirit: a name and
// where it resolves, never a value.
type SecretRef struct {
	Name     string
	Resolver string // "env", "file", "kubernetes"
	EnvVar   string // the variable the overlay references
	Resolved bool
	Where    string // human description of where it was found
}

type Migration struct {
	Service   string
	Resources []string
	Command   []string
}

type Route struct {
	Service string
	Port    string
	Host    string
	TLS     string
	Path    string // "/" default
}

type Level string

const (
	LevelRenderable    Level = "renderable"
	LevelValidated     Level = "validated"
	LevelApply         Level = "apply"
	LevelLiveQualified Level = "live-qualified"
)

type Capabilities struct {
	Level         Level
	Resources     map[ResourceType][]Lifecycle
	Ingress       bool
	FileMounts    bool
	NetworkPolicy bool
	Observe       bool
	Logs          bool
	Rollback      bool
}
