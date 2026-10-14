package shared

// TracingConfig configures distributed tracing. Pointer fields tell "not set"
// apart from a deliberate zero: enabled false, sample_ratio 0 and an empty
// response_header are all meaningful.
type TracingConfig struct {
	Enabled        *bool             `json:"enabled,omitempty"         yaml:"enabled,omitempty"`
	Endpoint       string            `json:"endpoint,omitempty"        yaml:"endpoint,omitempty"`
	Headers        map[string]string `json:"headers,omitempty"         yaml:"headers,omitempty"`
	SampleRatio    *float64          `json:"sample_ratio,omitempty"    yaml:"sample_ratio,omitempty"`
	Propagators    []string          `json:"propagators,omitempty"     yaml:"propagators,omitempty"`
	ExcludePaths   []string          `json:"exclude_paths,omitempty"   yaml:"exclude_paths,omitempty"`
	ResponseHeader *string           `json:"response_header,omitempty" yaml:"response_header,omitempty"`
	Resource       map[string]string `json:"resource,omitempty"        yaml:"resource,omitempty"`
}
