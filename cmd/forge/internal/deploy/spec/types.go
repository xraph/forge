// Package spec reads and edits versioned deployment configuration.
package spec

import "time"

const Version = 2

type Kind string

const (
	KindWeb     Kind = "web"
	KindWorker  Kind = "worker"
	KindJob     Kind = "job"
	KindCron    Kind = "cron"
	KindGateway Kind = "gateway"
)

type Exposure string

const (
	ExposurePublic  Exposure = "public"
	ExposurePrivate Exposure = "private"
)

type Lifecycle string

const (
	LifecycleContainer Lifecycle = "container"
	LifecycleExternal  Lifecycle = "external"
	LifecycleManaged   Lifecycle = "managed"
)

type Deploy struct {
	Version          int                    `yaml:"version"`
	Registry         string                 `yaml:"registry,omitempty"`
	Defaults         Defaults               `yaml:"defaults,omitempty"`
	Spec             string                 `yaml:"spec,omitempty"`
	EnvironmentFiles map[string]string      `yaml:"environment_files,omitempty"`
	Services         map[string]Service     `yaml:"services,omitempty"`
	Resources        map[string]Resource    `yaml:"resources,omitempty"`
	Connections      []Connection           `yaml:"connections,omitempty"`
	Environments     map[string]Environment `yaml:"environments,omitempty"`
	Targets          map[string]Target      `yaml:"targets,omitempty"`
	Secrets          Secrets                `yaml:"secrets,omitempty"`
}

type Defaults struct {
	Target      string `yaml:"target,omitempty"`
	Environment string `yaml:"environment,omitempty"`
}

type Service struct {
	App       string            `yaml:"app"`
	Kind      Kind              `yaml:"kind"`
	Ports     map[string]Port   `yaml:"ports,omitempty"`
	Health    *Health           `yaml:"health,omitempty"`
	Config    []string          `yaml:"config,omitempty"`
	Replicas  int               `yaml:"replicas,omitempty"`
	Resources *ResourceSpec     `yaml:"resources,omitempty"`
	Bindings  []Binding         `yaml:"bindings,omitempty"`
	Calls     []string          `yaml:"calls,omitempty"`
	Env       map[string]string `yaml:"env,omitempty"`
	Migrate   string            `yaml:"migrate,omitempty"` // "", "auto", or a command string
	Discovery bool              `yaml:"discovery,omitempty"`
	Schedule  string            `yaml:"schedule,omitempty"`
}

type Port struct {
	Port     int      `yaml:"port"`
	Protocol string   `yaml:"protocol,omitempty"` // "http" default, "grpc", "tcp"
	Exposure Exposure `yaml:"exposure,omitempty"` // private default
}

type Health struct {
	Readiness string `yaml:"readiness,omitempty"`
	Liveness  string `yaml:"liveness,omitempty"`
	Startup   string `yaml:"startup,omitempty"`
	Heartbeat bool   `yaml:"heartbeat,omitempty"`
	None      bool   `yaml:"none,omitempty"`
}

type ResourceSpec struct {
	CPU         string `yaml:"cpu,omitempty"`
	Memory      string `yaml:"memory,omitempty"`
	CPULimit    string `yaml:"cpu_limit,omitempty"`
	MemoryLimit string `yaml:"memory_limit,omitempty"`
}

type Binding struct {
	Resource         string `yaml:"resource"`
	Extension        string `yaml:"extension"`
	Database         string `yaml:"database,omitempty"`          // grove
	Store            string `yaml:"store,omitempty"`             // trove, grove_kv
	MetadataDatabase string `yaml:"metadata_database,omitempty"` // trove
}

type Resource struct {
	Type     string   `yaml:"type"`
	Version  string   `yaml:"version,omitempty"`
	Features []string `yaml:"features,omitempty"`
	Bucket   string   `yaml:"bucket,omitempty"`
}

type Connection struct {
	From      string        `yaml:"from"`
	To        string        `yaml:"to"`
	Port      string        `yaml:"port,omitempty"`       // "http" default
	ConfigKey string        `yaml:"config_key,omitempty"` // "services.<to>.url" default
	Timeout   time.Duration `yaml:"timeout,omitempty"`
	Retry     Retry         `yaml:"retry,omitempty"`
}

type Retry struct {
	Attempts int `yaml:"attempts,omitempty"`
}

type Environment struct {
	Target    string                      `yaml:"target"`
	Replicas  map[string]int              `yaml:"replicas,omitempty"`
	Resources map[string]ResourceOverride `yaml:"resources,omitempty"`
	Env       map[string]string           `yaml:"env,omitempty"`
	Ingress   *Ingress                    `yaml:"ingress,omitempty"`
	Backup    *Backup                     `yaml:"backup,omitempty"`
}

type ResourceOverride struct {
	Lifecycle Lifecycle `yaml:"lifecycle,omitempty"`
	Secret    string    `yaml:"secret,omitempty"`
	Recipe    string    `yaml:"recipe,omitempty"`
	Remove    bool      `yaml:"remove,omitempty"`
}

type Ingress struct {
	Host string `yaml:"host"`
	TLS  string `yaml:"tls,omitempty"` // issuer name, "letsencrypt" by convention
}

type Backup struct {
	Schedule    string `yaml:"schedule"`
	Destination string `yaml:"destination"`
	Retention   string `yaml:"retention,omitempty"`
}

type Target struct {
	Provider      string         `yaml:"provider"`
	Context       string         `yaml:"context,omitempty"`
	Namespace     string         `yaml:"namespace,omitempty"`
	Region        string         `yaml:"region,omitempty"`
	IngressClass  string         `yaml:"ingress_class,omitempty"`
	GatewayAPI    bool           `yaml:"gateway_api,omitempty"`
	NetworkPolicy bool           `yaml:"network_policy,omitempty"`
	Project       string         `yaml:"project,omitempty"` // compose project name
	DockerContext string         `yaml:"docker_context,omitempty"`
	Extra         map[string]any `yaml:",inline"`
}

type Secrets struct {
	Resolver string `yaml:"resolver,omitempty"` // "env" default, "file", "kubernetes"
	File     string `yaml:"file,omitempty"`     // ".forge/secrets.env" default
}
