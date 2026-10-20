package shared

// TracingConfig configures distributed tracing. Pointer fields tell "not set"
// apart from a deliberate zero: enabled false, sample_ratio 0 and an empty
// response_header are all meaningful.
//
// The last three fields decide how much of a request reaches its server span.
// By default url.path holds the matched route template (the same value as
// http.route) and is left off a request that matched no route, url.query is
// not recorded and neither is user_agent.original. A raw path can carry a
// session ID or a secret, a query string a ticket, and a user agent is
// personal data, so each one is something you turn on.
type TracingConfig struct {
	Enabled        *bool             `json:"enabled,omitempty"         yaml:"enabled,omitempty"`
	Endpoint       string            `json:"endpoint,omitempty"        yaml:"endpoint,omitempty"`
	Headers        map[string]string `json:"headers,omitempty"         yaml:"headers,omitempty"`
	SampleRatio    *float64          `json:"sample_ratio,omitempty"    yaml:"sample_ratio,omitempty"`
	Propagators    []string          `json:"propagators,omitempty"     yaml:"propagators,omitempty"`
	ExcludePaths   []string          `json:"exclude_paths,omitempty"   yaml:"exclude_paths,omitempty"`
	ResponseHeader *string           `json:"response_header,omitempty" yaml:"response_header,omitempty"`
	Resource       map[string]string `json:"resource,omitempty"        yaml:"resource,omitempty"`

	// RecordRawPath puts the request's raw path in url.path, matched or
	// not, in place of the route template.
	RecordRawPath *bool `json:"record_raw_path,omitempty" yaml:"record_raw_path,omitempty"`
	// RecordUserAgent puts the User-Agent header in user_agent.original.
	RecordUserAgent *bool `json:"record_user_agent,omitempty" yaml:"record_user_agent,omitempty"`
	// AllowedQueryKeys turns url.query on. Every key is kept, the values of
	// the keys listed here are kept, and every other value reads REDACTED.
	// A key sent without a value reads REDACTED unless it is listed.
	AllowedQueryKeys []string `json:"allowed_query_keys,omitempty" yaml:"allowed_query_keys,omitempty"`
}
