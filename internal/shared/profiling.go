package shared

// ProfilingConfig configures continuous profiling. Setting ServerAddress turns
// on push to Pyroscope. SpanLabels is a pointer so "not set" (follow push) is
// told apart from a deliberate false.
type ProfilingConfig struct {
	ServerAddress     string            `json:"server_address,omitempty"      yaml:"server_address,omitempty"`
	ApplicationName   string            `json:"application_name,omitempty"    yaml:"application_name,omitempty"`
	BasicAuthUser     string            `json:"basic_auth_user,omitempty"     yaml:"basic_auth_user,omitempty"`
	BasicAuthPassword string            `json:"basic_auth_password,omitempty" yaml:"basic_auth_password,omitempty"`
	TenantID          string            `json:"tenant_id,omitempty"           yaml:"tenant_id,omitempty"`
	Tags              map[string]string `json:"tags,omitempty"                yaml:"tags,omitempty"`
	ProfileTypes      []string          `json:"profile_types,omitempty"       yaml:"profile_types,omitempty"`
	SpanLabels        *bool             `json:"span_labels,omitempty"         yaml:"span_labels,omitempty"`
}
