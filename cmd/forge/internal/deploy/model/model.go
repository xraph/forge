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
	Project     string       `json:"project"`
	Environment string       `json:"environment"`
	TargetName  string       `json:"target_name"`
	Target      spec.Target  `json:"target"`
	Registry    string       `json:"registry"`
	Services    []Service    `json:"services"`
	Resources   []Resource   `json:"resources"`
	Connections []Connection `json:"connections"`
	Secrets     []SecretRef  `json:"secrets"`
	Migrations  []Migration  `json:"migrations"`
	Routes      []Route      `json:"routes"`
	Overlay     OverlayMode  `json:"overlay"` // file, inline, local-file-fallback
}

type OverlayMode string

const (
	OverlayFile     OverlayMode = "file"
	OverlayInline   OverlayMode = "inline"
	OverlayFallback OverlayMode = "config-local"
)

type Service struct {
	Name        string            `json:"name"`
	App         string            `json:"app"`
	Dir         string            `json:"dir"`       // absolute path to the app's main package
	MainPath    string            `json:"main_path"` // relative to project root, e.g. "cmd/api"
	Kind        Kind              `json:"kind"`
	Image       Image             `json:"image"`
	Ports       []Port            `json:"ports"`
	Health      Health            `json:"health"`
	Replicas    int               `json:"replicas"`
	Resources   spec.ResourceSpec `json:"resources"`
	ConfigFiles []ConfigFile      `json:"config_files"` // the overlay and any mounted config
	Bindings    []Binding         `json:"bindings"`
	Env         map[string]string `json:"env"`
	Calls       []string          `json:"calls"`
	Discovery   bool              `json:"discovery"`
	Migrate     []string          `json:"migrate"` // nil: none; the command when set
	Schedule    string            `json:"schedule"`
}

type Image struct {
	Repository string `json:"repository"`
	Tag        string `json:"tag"`
	Digest     string `json:"digest"`
	Dockerfile string `json:"dockerfile"` // relative path when the user supplied one
}

type Port struct {
	Name     string   `json:"name"`
	Port     int      `json:"port"`
	Protocol string   `json:"protocol"`
	Exposure Exposure `json:"exposure"`
}

type Health struct {
	Readiness string `json:"readiness"`
	Liveness  string `json:"liveness"`
	Startup   string `json:"startup"`
	Heartbeat bool   `json:"heartbeat"`
	None      bool   `json:"none"`
}

// ConfigFile matches ctrlplane's provider.ConfigFile field for field.
type ConfigFile struct {
	Name    string `json:"name"`
	Path    string `json:"path"`   // mount path inside the container
	Format  string `json:"format"` // "yaml"
	Content string `json:"content"`
}

type Binding struct {
	Resource  string            `json:"resource"`
	Extension string            `json:"extension"`
	Instance  string            `json:"instance"` // grove database name, trove store, kv store
	Keys      map[string]string `json:"keys"`     // config key -> value or ${VAR} reference
}

type Resource struct {
	Name      string       `json:"name"`
	Type      ResourceType `json:"type"`
	Lifecycle Lifecycle    `json:"lifecycle"`
	Version   string       `json:"version"`
	Recipe    string       `json:"recipe"`
	Features  []string     `json:"features"`
	Bucket    string       `json:"bucket"`
	Secret    SecretRef    `json:"secret"`
	UsedBy    []string     `json:"used_by"`
}

type Connection struct {
	From      string        `json:"from"`
	To        string        `json:"to"`
	Port      string        `json:"port"`
	Address   string        `json:"address"` // target-specific, filled by the provider
	ConfigKey string        `json:"config_key"`
	EnvVar    string        `json:"env_var"` // "<TO>_URL"
	Timeout   time.Duration `json:"timeout"`
	Retries   int           `json:"retries"`
}

// SecretRef matches ctrlplane's provider.SecretRef in spirit: a name and
// where it resolves, never a value.
type SecretRef struct {
	Name     string `json:"name"`
	Resolver string `json:"resolver"` // "env", "file", "kubernetes"
	EnvVar   string `json:"env_var"`  // the variable the overlay references
	Resolved bool   `json:"resolved"`
	Where    string `json:"where"` // human description of where it was found
}

type Migration struct {
	Service   string   `json:"service"`
	Resources []string `json:"resources"`
	Command   []string `json:"command"`
}

type Route struct {
	Service string `json:"service"`
	Port    string `json:"port"`
	Host    string `json:"host"`
	TLS     string `json:"tls"`
	Path    string `json:"path"` // "/" default
}

type Level string

const (
	LevelRenderable    Level = "renderable"
	LevelValidated     Level = "validated"
	LevelApply         Level = "apply"
	LevelLiveQualified Level = "live-qualified"
)

type Capabilities struct {
	Level         Level                        `json:"level"`
	Resources     map[ResourceType][]Lifecycle `json:"resources"`
	Ingress       bool                         `json:"ingress"`
	FileMounts    bool                         `json:"file_mounts"`
	NetworkPolicy bool                         `json:"network_policy"`
	Observe       bool                         `json:"observe"`
	Logs          bool                         `json:"logs"`
	Rollback      bool                         `json:"rollback"`
}
