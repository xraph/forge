package hosted

import "github.com/xraph/forge/cmd/forge/internal/deploy/model"

// Export carries provider-compatible fields. The hosted authority supplies account identity and applies bindings.
type Export struct {
	Schema      string     `json:"schema"`
	Project     string     `json:"project"`
	Environment string     `json:"environment"`
	Workloads   []Workload `json:"workloads"`
}
type Workload struct {
	Name           string          `json:"name"`
	Kind           string          `json:"kind"`
	Services       []Service       `json:"services"`
	SecretBindings []SecretBinding `json:"secret_bindings,omitempty"`
}
type Service struct {
	Name        string             `json:"name"`
	Image       string             `json:"image"`
	Role        string             `json:"role"`
	Resources   Resources          `json:"resources"`
	Env         map[string]string  `json:"env,omitempty"`
	Ports       []Port             `json:"ports,omitempty"`
	HealthCheck *HealthCheck       `json:"health_check,omitempty"`
	Secrets     []SecretRef        `json:"secrets,omitempty"`
	ConfigFiles []model.ConfigFile `json:"config_files,omitempty"`
	Annotations map[string]string  `json:"annotations,omitempty"`
}
type Resources struct {
	CPUMillis int `json:"cpu_millis"`
	MemoryMB  int `json:"memory_mb"`
	Replicas  int `json:"replicas"`
}
type Port struct {
	Container int    `json:"container"`
	Protocol  string `json:"protocol"`
}
type HealthCheck struct {
	Path     string `json:"path,omitempty"`
	Port     int    `json:"port"`
	Interval int64  `json:"interval"`
	Timeout  int64  `json:"timeout"`
	Retries  int    `json:"retries"`
}
type SecretRef struct {
	Key  string `json:"key"`
	Type string `json:"type"`
}
type SecretBinding struct {
	VarName string    `json:"var_name"`
	EnvKey  string    `json:"env_key"`
	Ref     SecretRef `json:"ref"`
}
