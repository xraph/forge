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
	Workbench        Workbench              `json:"workbench,omitzero"          yaml:"workbench,omitempty"`
	Version          int                    `json:"version"                     yaml:"version"`
	Registry         string                 `json:"registry,omitzero"           yaml:"registry,omitempty"`
	Defaults         Defaults               `json:"defaults,omitzero"           yaml:"defaults,omitempty"`
	Spec             string                 `json:"spec,omitempty"              yaml:"spec,omitempty"`
	EnvironmentFiles map[string]string      `json:"environment_files,omitempty" yaml:"environment_files,omitempty"`
	Services         map[string]Service     `json:"services,omitempty"          yaml:"services,omitempty"`
	Resources        map[string]Resource    `json:"resources,omitempty"         yaml:"resources,omitempty"`
	Connections      []Connection           `json:"connections,omitempty"       yaml:"connections,omitempty"`
	Environments     map[string]Environment `json:"environments,omitempty"      yaml:"environments,omitempty"`
	Targets          map[string]Target      `json:"targets,omitempty"           yaml:"targets,omitempty"`
	Secrets          Secrets                `json:"secrets,omitzero"            yaml:"secrets,omitempty"`
}

type Defaults struct {
	Target      string `json:"target,omitempty"      yaml:"target,omitempty"`
	Environment string `json:"environment,omitempty" yaml:"environment,omitempty"`
}

type Service struct {
	App       string            `json:"app"                 yaml:"app"`
	Kind      Kind              `json:"kind"                yaml:"kind"`
	Ports     map[string]Port   `json:"ports,omitempty"     yaml:"ports,omitempty"`
	Health    *Health           `json:"health,omitempty"    yaml:"health,omitempty"`
	Config    []string          `json:"config,omitempty"    yaml:"config,omitempty"`
	Replicas  int               `json:"replicas,omitempty"  yaml:"replicas,omitempty"`
	Resources *ResourceSpec     `json:"resources,omitempty" yaml:"resources,omitempty"`
	Bindings  []Binding         `json:"bindings,omitempty"  yaml:"bindings,omitempty"`
	Calls     []string          `json:"calls,omitempty"     yaml:"calls,omitempty"`
	Env       map[string]string `json:"env,omitempty"       yaml:"env,omitempty"`
	Migrate   string            `json:"migrate,omitempty"   yaml:"migrate,omitempty"` // "", "auto", or a command string
	Discovery bool              `json:"discovery,omitempty" yaml:"discovery,omitempty"`
	Schedule  string            `json:"schedule,omitempty"  yaml:"schedule,omitempty"`
}

type Port struct {
	Port     int      `json:"port"               yaml:"port"`
	Protocol string   `json:"protocol,omitempty" yaml:"protocol,omitempty"` // "http" default, "grpc", "tcp"
	Exposure Exposure `json:"exposure,omitempty" yaml:"exposure,omitempty"` // private default
}

type Health struct {
	Readiness string `json:"readiness,omitempty" yaml:"readiness,omitempty"`
	Liveness  string `json:"liveness,omitempty"  yaml:"liveness,omitempty"`
	Startup   string `json:"startup,omitempty"   yaml:"startup,omitempty"`
	Heartbeat bool   `json:"heartbeat,omitempty" yaml:"heartbeat,omitempty"`
	None      bool   `json:"none,omitempty"      yaml:"none,omitempty"`
}

type ResourceSpec struct {
	CPU         string `json:"cpu,omitempty"          yaml:"cpu,omitempty"`
	Memory      string `json:"memory,omitempty"       yaml:"memory,omitempty"`
	CPULimit    string `json:"cpu_limit,omitempty"    yaml:"cpu_limit,omitempty"`
	MemoryLimit string `json:"memory_limit,omitempty" yaml:"memory_limit,omitempty"`
}

type Binding struct {
	Resource         string `json:"resource"                    yaml:"resource"`
	Extension        string `json:"extension"                   yaml:"extension"`
	Database         string `json:"database,omitempty"          yaml:"database,omitempty"`          // grove
	Store            string `json:"store,omitempty"             yaml:"store,omitempty"`             // trove, grove_kv
	MetadataDatabase string `json:"metadata_database,omitempty" yaml:"metadata_database,omitempty"` // trove
}

type Resource struct {
	Type     string   `json:"type"               yaml:"type"`
	Version  string   `json:"version,omitempty"  yaml:"version,omitempty"`
	Features []string `json:"features,omitempty" yaml:"features,omitempty"`
	Bucket   string   `json:"bucket,omitempty"   yaml:"bucket,omitempty"`
}

type Connection struct {
	From      string        `json:"from"                 yaml:"from"`
	To        string        `json:"to"                   yaml:"to"`
	Port      string        `json:"port,omitempty"       yaml:"port,omitempty"`       // "http" default
	ConfigKey string        `json:"config_key,omitempty" yaml:"config_key,omitempty"` // "services.<to>.url" default
	Timeout   time.Duration `json:"timeout,omitempty"    yaml:"timeout,omitempty"`
	Retry     Retry         `json:"retry,omitzero"       yaml:"retry,omitempty"`
}

type Retry struct {
	Attempts int `json:"attempts,omitempty" yaml:"attempts,omitempty"`
}

type Environment struct {
	Target            string                      `json:"target"                       yaml:"target"`
	Services          []string                    `json:"services,omitempty"           yaml:"services,omitempty"`
	Purpose           string                      `json:"purpose,omitempty"            yaml:"purpose,omitempty"`
	EnvironmentAction string                      `json:"environment_action,omitempty" yaml:"environment_action,omitempty"`
	ExternalServices  map[string]ExternalService  `json:"external_services,omitempty"  yaml:"external_services,omitempty"`
	HealthOverrides   map[string]Health           `json:"health_overrides,omitempty"   yaml:"health_overrides,omitempty"`
	BindingOverrides  map[string][]Binding        `json:"binding_overrides,omitempty"  yaml:"binding_overrides,omitempty"`
	Replicas          map[string]int              `json:"replicas,omitempty"           yaml:"replicas,omitempty"`
	Resources         map[string]ResourceOverride `json:"resources,omitempty"          yaml:"resources,omitempty"`
	Env               map[string]string           `json:"env,omitempty"                yaml:"env,omitempty"`
	Ingress           *Ingress                    `json:"ingress,omitempty"            yaml:"ingress,omitempty"`
	Backup            *Backup                     `json:"backup,omitempty"             yaml:"backup,omitempty"`
}

type ResourceOverride struct {
	Target    string    `json:"target,omitempty"    yaml:"target,omitempty"`
	Lifecycle Lifecycle `json:"lifecycle,omitempty" yaml:"lifecycle,omitempty"`
	Secret    string    `json:"secret,omitempty"    yaml:"secret,omitempty"`
	Recipe    string    `json:"recipe,omitempty"    yaml:"recipe,omitempty"`
	Remove    bool      `json:"remove,omitempty"    yaml:"remove,omitempty"`
}

type Ingress struct {
	Host string `json:"host"          yaml:"host"`
	TLS  string `json:"tls,omitempty" yaml:"tls,omitempty"` // issuer name, "letsencrypt" by convention
}

type Backup struct {
	Schedule    string `json:"schedule"            yaml:"schedule"`
	Destination string `json:"destination"         yaml:"destination"`
	Retention   string `json:"retention,omitempty" yaml:"retention,omitempty"`
}

type Target struct {
	LocalCluster     string              `json:"local_cluster,omitempty"     yaml:"local_cluster,omitempty"`
	StorageClass     string              `json:"storage_class,omitempty"     yaml:"storage_class,omitempty"`
	StorageSize      string              `json:"storage_size,omitempty"      yaml:"storage_size,omitempty"`
	IngressNamespace string              `json:"ingress_namespace,omitempty" yaml:"ingress_namespace,omitempty"`
	ExternalCIDRs    map[string][]string `json:"external_cidrs,omitempty"    yaml:"external_cidrs,omitempty"`
	APIServerCIDRs   []string            `json:"api_server_cidrs,omitempty"  yaml:"api_server_cidrs,omitempty"`
	Gateway          string              `json:"gateway,omitempty"           yaml:"gateway,omitempty"`

	Build         Build          `json:"build,omitzero"           yaml:"build,omitempty"`
	Release       Release        `json:"release,omitzero"         yaml:"release,omitempty"`
	ResourceOnly  bool           `json:"resource_only,omitempty"  yaml:"resource_only,omitempty"`
	Provider      string         `json:"provider"                 yaml:"provider"`
	Context       string         `json:"context,omitempty"        yaml:"context,omitempty"`
	Namespace     string         `json:"namespace,omitempty"      yaml:"namespace,omitempty"`
	Region        string         `json:"region,omitempty"         yaml:"region,omitempty"`
	IngressClass  string         `json:"ingress_class,omitempty"  yaml:"ingress_class,omitempty"`
	GatewayAPI    bool           `json:"gateway_api,omitempty"    yaml:"gateway_api,omitempty"`
	NetworkPolicy bool           `json:"network_policy,omitempty" yaml:"network_policy,omitempty"`
	Project       string         `json:"project,omitempty"        yaml:"project,omitempty"` // compose project name
	DockerContext string         `json:"docker_context,omitempty" yaml:"docker_context,omitempty"`
	Extra         map[string]any `json:"extra,omitempty"          yaml:",inline"`
}

type Secrets struct {
	References map[string]string `json:"references,omitempty" yaml:"references,omitempty"`
	Resolver   string            `json:"resolver,omitempty"   yaml:"resolver,omitempty"` // "env" default, "file", "kubernetes"
	File       string            `json:"file,omitempty"       yaml:"file,omitempty"`     // ".forge/secrets.env" default
}

// Build describes image provenance separately from the provider that runs it.
type Build struct {
	Source     string                  `json:"source,omitempty"      yaml:"source,omitempty"`
	Delivery   string                  `json:"delivery,omitempty"    yaml:"delivery,omitempty"`
	Platforms  []string                `json:"platforms,omitempty"   yaml:"platforms,omitempty"`
	Builder    string                  `json:"builder,omitempty"     yaml:"builder,omitempty"`
	Registry   Registry                `json:"registry,omitzero"     yaml:"registry,omitempty"`
	Images     map[string]string       `json:"images,omitempty"      yaml:"images,omitempty"`
	Repo       string                  `json:"repo,omitempty"        yaml:"repo,omitempty"`
	Branch     string                  `json:"branch,omitempty"      yaml:"branch,omitempty"`
	Commit     string                  `json:"commit,omitempty"      yaml:"commit,omitempty"`
	Trigger    string                  `json:"trigger,omitempty"     yaml:"trigger,omitempty"`
	ConfigPath string                  `json:"config_path,omitempty" yaml:"config_path,omitempty"`
	Services   map[string]ServiceBuild `json:"services,omitempty"    yaml:"services,omitempty"`
}

type Registry struct {
	Username   string `json:"username,omitempty"    yaml:"username,omitempty"`
	PullSecret string `json:"pull_secret,omitempty" yaml:"pull_secret,omitempty"`
	Host       string `json:"host,omitempty"        yaml:"host,omitempty"`
	Namespace  string `json:"namespace,omitempty"   yaml:"namespace,omitempty"`
	Auth       string `json:"auth,omitempty"        yaml:"auth,omitempty"`
	Visibility string `json:"visibility,omitempty"  yaml:"visibility,omitempty"`
	SecretRef  string `json:"secret_ref,omitempty"  yaml:"secret_ref,omitempty"`
}

type ServiceBuild struct {
	RootDir      string `json:"root_dir,omitempty"      yaml:"root_dir,omitempty"`
	Dockerfile   string `json:"dockerfile,omitempty"    yaml:"dockerfile,omitempty"`
	BuildCommand string `json:"build_command,omitempty" yaml:"build_command,omitempty"`
	StartCommand string `json:"start_command,omitempty" yaml:"start_command,omitempty"`
}

type Release struct {
	Mode       string `json:"mode,omitempty"       yaml:"mode,omitempty"`
	Repo       string `json:"repo,omitempty"       yaml:"repo,omitempty"`
	Branch     string `json:"branch,omitempty"     yaml:"branch,omitempty"`
	Path       string `json:"path,omitempty"       yaml:"path,omitempty"`
	Approval   string `json:"approval,omitempty"   yaml:"approval,omitempty"`
	Controller string `json:"controller,omitempty" yaml:"controller,omitempty"`
}

type ExternalService struct {
	URL  string `json:"url"            yaml:"url"`
	Auth string `json:"auth,omitempty" yaml:"auth,omitempty"`
}

type Workbench struct {
	Persistence Persistence `json:"persistence,omitzero" yaml:"persistence,omitempty"`
}
type Persistence struct {
	Backend   string `json:"backend,omitempty"   yaml:"backend,omitempty"`
	Reference string `json:"reference,omitempty" yaml:"reference,omitempty"`
}
