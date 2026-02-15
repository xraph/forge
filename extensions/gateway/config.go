package gateway

import (
	"time"
)

// DI container keys for the gateway extension.
const (
	// ServiceKey is the DI key for the gateway service.
	ServiceKey = "gateway"
)

// Config holds the complete gateway configuration.
type Config struct {
	// Enabled determines if the gateway is active
	Enabled bool `json:"enabled" yaml:"enabled"`

	// BasePath is the prefix for all proxied routes (e.g., "/gw")
	BasePath string `json:"basePath" yaml:"base_path"`

	// --- Data Plane ---

	// Routes are manually defined upstream routes
	Routes []RouteConfig `json:"routes" yaml:"routes"`

	// Timeouts for upstream connections
	Timeouts TimeoutConfig `json:"timeouts" yaml:"timeouts"`

	// Retry is the global retry policy
	Retry RetryConfig `json:"retry" yaml:"retry"`

	// BufferPool configures request/response buffer sizes
	BufferPool BufferPoolConfig `json:"bufferPool" yaml:"buffer_pool"`

	// --- Resilience ---

	// CircuitBreaker is the global circuit breaker config
	CircuitBreaker CircuitBreakerConfig `json:"circuitBreaker" yaml:"circuit_breaker"`

	// RateLimiting is the global rate limiting config
	RateLimiting RateLimitConfig `json:"rateLimiting" yaml:"rate_limiting"`

	// HealthCheck configures upstream health checking
	HealthCheck HealthCheckConfig `json:"healthCheck" yaml:"health_check"`

	// --- Traffic Management ---

	// LoadBalancing configures the load balancing strategy
	LoadBalancing LoadBalancingConfig `json:"loadBalancing" yaml:"load_balancing"`

	// TrafficSplit configures global traffic splitting
	TrafficSplit TrafficSplitConfig `json:"trafficSplit" yaml:"traffic_split"`

	// --- Security ---

	// Auth configures gateway-level authentication
	Auth AuthConfig `json:"auth" yaml:"auth"`

	// TLS configures upstream TLS/mTLS
	TLS TLSConfig `json:"tls" yaml:"tls"`

	// IPFilter configures IP allow/deny lists
	IPFilter IPFilterConfig `json:"ipFilter" yaml:"ip_filter"`

	// CORS configures gateway-level CORS
	CORS CORSConfig `json:"cors" yaml:"cors"`

	// --- Caching ---

	// Caching configures response caching
	Caching CachingConfig `json:"caching" yaml:"caching"`

	// --- Discovery ---

	// Discovery configures FARP-based auto-discovery
	Discovery DiscoveryConfig `json:"discovery" yaml:"discovery"`

	// --- Observability ---

	// Metrics configures Prometheus metrics
	Metrics MetricsConfig `json:"metrics" yaml:"metrics"`

	// Tracing configures OpenTelemetry tracing
	Tracing TracingConfig `json:"tracing" yaml:"tracing"`

	// AccessLog configures structured access logging
	AccessLog AccessLogConfig `json:"accessLog" yaml:"access_log"`

	// --- OpenAPI ---

	// OpenAPI configures the aggregated OpenAPI spec from all upstream services
	OpenAPI OpenAPIConfig `json:"openapi" yaml:"openapi"`

	// --- Admin ---

	// Dashboard configures the admin UI
	Dashboard DashboardConfig `json:"dashboard" yaml:"dashboard"`

	// WebSocket configures WebSocket proxy settings
	WebSocket WebSocketConfig `json:"webSocket" yaml:"web_socket"`

	// SSE configures SSE proxy settings
	SSE SSEConfig `json:"sse" yaml:"sse"`
}

// RouteConfig defines a static route in configuration.
type RouteConfig struct {
	Path          string            `json:"path" yaml:"path"`
	Methods       []string          `json:"methods,omitempty" yaml:"methods"`
	Targets       []TargetConfig    `json:"targets" yaml:"targets"`
	StripPrefix   bool              `json:"stripPrefix" yaml:"strip_prefix"`
	AddPrefix     string            `json:"addPrefix,omitempty" yaml:"add_prefix"`
	RewritePath   string            `json:"rewritePath,omitempty" yaml:"rewrite_path"`
	Headers       HeaderPolicy      `json:"headers" yaml:"headers"`
	Protocol      RouteProtocol     `json:"protocol" yaml:"protocol"`
	Priority      int               `json:"priority" yaml:"priority"`
	Enabled       bool              `json:"enabled" yaml:"enabled"`
	Retry         *RetryConfig      `json:"retry,omitempty" yaml:"retry"`
	Timeout       *TimeoutConfig    `json:"timeout,omitempty" yaml:"timeout"`
	RateLimit     *RateLimitConfig  `json:"rateLimit,omitempty" yaml:"rate_limit"`
	Auth          *RouteAuthConfig  `json:"auth,omitempty" yaml:"auth"`
	Cache         *RouteCacheConfig `json:"cache,omitempty" yaml:"cache"`
	TrafficPolicy *TrafficPolicy    `json:"trafficPolicy,omitempty" yaml:"traffic_policy"`
	Metadata      map[string]any    `json:"metadata,omitempty" yaml:"metadata"`
}

// TargetConfig defines a target in configuration.
type TargetConfig struct {
	URL      string            `json:"url" yaml:"url"`
	Weight   int               `json:"weight" yaml:"weight"`
	Tags     []string          `json:"tags,omitempty" yaml:"tags"`
	Metadata map[string]string `json:"metadata,omitempty" yaml:"metadata"`
	TLS      *TargetTLSConfig  `json:"tls,omitempty" yaml:"tls"`
}

// TimeoutConfig holds timeout settings.
type TimeoutConfig struct {
	Connect time.Duration `json:"connect" yaml:"connect"`
	Read    time.Duration `json:"read" yaml:"read"`
	Write   time.Duration `json:"write" yaml:"write"`
	Idle    time.Duration `json:"idle" yaml:"idle"`
}

// RetryConfig holds retry settings.
type RetryConfig struct {
	Enabled          bool            `json:"enabled" yaml:"enabled"`
	MaxAttempts      int             `json:"maxAttempts" yaml:"max_attempts"`
	Backoff          BackoffStrategy `json:"backoff" yaml:"backoff"`
	InitialDelay     time.Duration   `json:"initialDelay" yaml:"initial_delay"`
	MaxDelay         time.Duration   `json:"maxDelay" yaml:"max_delay"`
	Multiplier       float64         `json:"multiplier" yaml:"multiplier"`
	Jitter           bool            `json:"jitter" yaml:"jitter"`
	RetryableStatus  []int           `json:"retryableStatus" yaml:"retryable_status"`
	RetryableMethods []string        `json:"retryableMethods" yaml:"retryable_methods"`
	BudgetPercent    float64         `json:"budgetPercent" yaml:"budget_percent"`
}

// BufferPoolConfig holds buffer pool settings.
type BufferPoolConfig struct {
	MaxRequestBodySize  int `json:"maxRequestBodySize" yaml:"max_request_body_size"`
	MaxResponseBodySize int `json:"maxResponseBodySize" yaml:"max_response_body_size"`
}

// CircuitBreakerConfig holds circuit breaker settings.
type CircuitBreakerConfig struct {
	Enabled          bool          `json:"enabled" yaml:"enabled"`
	FailureThreshold int           `json:"failureThreshold" yaml:"failure_threshold"`
	FailureWindow    time.Duration `json:"failureWindow" yaml:"failure_window"`
	ResetTimeout     time.Duration `json:"resetTimeout" yaml:"reset_timeout"`
	HalfOpenMax      int           `json:"halfOpenMax" yaml:"half_open_max"`
}

// RateLimitConfig holds rate limiting settings.
type RateLimitConfig struct {
	Enabled        bool    `json:"enabled" yaml:"enabled"`
	RequestsPerSec float64 `json:"requestsPerSec" yaml:"requests_per_sec"`
	Burst          int     `json:"burst" yaml:"burst"`
	PerClient      bool    `json:"perClient" yaml:"per_client"`
	KeyHeader      string  `json:"keyHeader,omitempty" yaml:"key_header"`
}

// HealthCheckConfig holds health check settings.
type HealthCheckConfig struct {
	Enabled              bool          `json:"enabled" yaml:"enabled"`
	Interval             time.Duration `json:"interval" yaml:"interval"`
	Timeout              time.Duration `json:"timeout" yaml:"timeout"`
	Path                 string        `json:"path" yaml:"path"`
	FailureThreshold     int           `json:"failureThreshold" yaml:"failure_threshold"`
	SuccessThreshold     int           `json:"successThreshold" yaml:"success_threshold"`
	EnablePassive        bool          `json:"enablePassive" yaml:"enable_passive"`
	PassiveFailThreshold int           `json:"passiveFailThreshold" yaml:"passive_fail_threshold"`
}

// LoadBalancingConfig holds load balancing settings.
type LoadBalancingConfig struct {
	Strategy      LoadBalanceStrategy `json:"strategy" yaml:"strategy"`
	ConsistentKey string              `json:"consistentKey,omitempty" yaml:"consistent_key"`
}

// TrafficSplitConfig holds global traffic split settings.
type TrafficSplitConfig struct {
	Enabled bool `json:"enabled" yaml:"enabled"`
}

// AuthConfig holds gateway-level auth settings.
type AuthConfig struct {
	Enabled        bool     `json:"enabled" yaml:"enabled"`
	DefaultPolicy  string   `json:"defaultPolicy,omitempty" yaml:"default_policy"`
	Providers      []string `json:"providers,omitempty" yaml:"providers"`
	ForwardHeaders bool     `json:"forwardHeaders" yaml:"forward_headers"`
}

// TLSConfig holds upstream TLS settings.
type TLSConfig struct {
	Enabled            bool          `json:"enabled" yaml:"enabled"`
	CACertFile         string        `json:"caCertFile,omitempty" yaml:"ca_cert_file"`
	ClientCertFile     string        `json:"clientCertFile,omitempty" yaml:"client_cert_file"`
	ClientKeyFile      string        `json:"clientKeyFile,omitempty" yaml:"client_key_file"`
	InsecureSkipVerify bool          `json:"insecureSkipVerify,omitempty" yaml:"insecure_skip_verify"`
	MinVersion         string        `json:"minVersion,omitempty" yaml:"min_version"`
	CipherSuites       []string      `json:"cipherSuites,omitempty" yaml:"cipher_suites"`
	ReloadInterval     time.Duration `json:"reloadInterval,omitempty" yaml:"reload_interval"`
}

// IPFilterConfig holds IP allow/deny list settings.
type IPFilterConfig struct {
	Enabled  bool     `json:"enabled" yaml:"enabled"`
	AllowIPs []string `json:"allowIps,omitempty" yaml:"allow_ips"`
	DenyIPs  []string `json:"denyIps,omitempty" yaml:"deny_ips"`
}

// CORSConfig holds CORS settings.
type CORSConfig struct {
	Enabled       bool     `json:"enabled" yaml:"enabled"`
	AllowOrigins  []string `json:"allowOrigins,omitempty" yaml:"allow_origins"`
	AllowMethods  []string `json:"allowMethods,omitempty" yaml:"allow_methods"`
	AllowHeaders  []string `json:"allowHeaders,omitempty" yaml:"allow_headers"`
	ExposeHeaders []string `json:"exposeHeaders,omitempty" yaml:"expose_headers"`
	AllowCreds    bool     `json:"allowCredentials" yaml:"allow_credentials"`
	MaxAge        int      `json:"maxAge" yaml:"max_age"`
}

// CachingConfig holds response caching settings.
type CachingConfig struct {
	Enabled    bool          `json:"enabled" yaml:"enabled"`
	DefaultTTL time.Duration `json:"defaultTtl" yaml:"default_ttl"`
	MaxSize    int           `json:"maxSize" yaml:"max_size"`
	Methods    []string      `json:"methods,omitempty" yaml:"methods"`
	VaryBy     []string      `json:"varyBy,omitempty" yaml:"vary_by"`
}

// DiscoveryConfig holds FARP auto-discovery settings.
type DiscoveryConfig struct {
	Enabled        bool            `json:"enabled" yaml:"enabled"`
	PollInterval   time.Duration   `json:"pollInterval" yaml:"poll_interval"`
	WatchMode      bool            `json:"watchMode" yaml:"watch_mode"`
	ServiceFilters []ServiceFilter `json:"serviceFilters,omitempty" yaml:"service_filters"`
	AutoPrefix     bool            `json:"autoPrefix" yaml:"auto_prefix"`
	PrefixTemplate string          `json:"prefixTemplate,omitempty" yaml:"prefix_template"`
	StripPrefix    bool            `json:"stripPrefix" yaml:"strip_prefix"`
}

// ServiceFilter defines a filter for discovered services.
type ServiceFilter struct {
	IncludeNames    []string          `json:"includeNames,omitempty" yaml:"include_names"`
	ExcludeNames    []string          `json:"excludeNames,omitempty" yaml:"exclude_names"`
	IncludeTags     []string          `json:"includeTags,omitempty" yaml:"include_tags"`
	ExcludeTags     []string          `json:"excludeTags,omitempty" yaml:"exclude_tags"`
	RequireMetadata map[string]string `json:"requireMetadata,omitempty" yaml:"require_metadata"`
}

// MetricsConfig holds metrics settings.
type MetricsConfig struct {
	Enabled bool   `json:"enabled" yaml:"enabled"`
	Prefix  string `json:"prefix,omitempty" yaml:"prefix"`
}

// TracingConfig holds tracing settings.
type TracingConfig struct {
	Enabled         bool    `json:"enabled" yaml:"enabled"`
	PropagateFormat string  `json:"propagateFormat,omitempty" yaml:"propagate_format"`
	SampleRate      float64 `json:"sampleRate" yaml:"sample_rate"`
}

// AccessLogConfig holds access logging settings.
type AccessLogConfig struct {
	Enabled        bool     `json:"enabled" yaml:"enabled"`
	RedactHeaders  []string `json:"redactHeaders,omitempty" yaml:"redact_headers"`
	IncludeBody    bool     `json:"includeBody" yaml:"include_body"`
	MaxBodyLogSize int      `json:"maxBodyLogSize" yaml:"max_body_log_size"`
}

// DashboardConfig holds admin dashboard settings.
type DashboardConfig struct {
	Enabled  bool   `json:"enabled" yaml:"enabled"`
	BasePath string `json:"basePath" yaml:"base_path"`
	Title    string `json:"title,omitempty" yaml:"title"`
	Realtime bool   `json:"realtime" yaml:"realtime"`
}

// WebSocketConfig holds WebSocket proxy settings.
type WebSocketConfig struct {
	ReadBufferSize   int           `json:"readBufferSize" yaml:"read_buffer_size"`
	WriteBufferSize  int           `json:"writeBufferSize" yaml:"write_buffer_size"`
	HandshakeTimeout time.Duration `json:"handshakeTimeout" yaml:"handshake_timeout"`
	PingInterval     time.Duration `json:"pingInterval" yaml:"ping_interval"`
	PongTimeout      time.Duration `json:"pongTimeout" yaml:"pong_timeout"`
}

// SSEConfig holds SSE proxy settings.
type SSEConfig struct {
	FlushInterval time.Duration `json:"flushInterval" yaml:"flush_interval"`
}

// DefaultConfig returns the default gateway configuration.
func DefaultConfig() Config {
	return Config{
		Enabled:  true,
		BasePath: "",
		Routes:   []RouteConfig{},
		Timeouts: TimeoutConfig{
			Connect: 10 * time.Second,
			Read:    30 * time.Second,
			Write:   30 * time.Second,
			Idle:    90 * time.Second,
		},
		Retry: RetryConfig{
			Enabled:          true,
			MaxAttempts:      3,
			Backoff:          BackoffExponential,
			InitialDelay:     100 * time.Millisecond,
			MaxDelay:         5 * time.Second,
			Multiplier:       2.0,
			Jitter:           true,
			RetryableStatus:  []int{502, 503, 504},
			RetryableMethods: []string{"GET", "HEAD", "OPTIONS"},
			BudgetPercent:    20.0,
		},
		BufferPool: BufferPoolConfig{
			MaxRequestBodySize:  10 * 1024 * 1024, // 10MB
			MaxResponseBodySize: 50 * 1024 * 1024, // 50MB
		},
		CircuitBreaker: CircuitBreakerConfig{
			Enabled:          true,
			FailureThreshold: 5,
			FailureWindow:    60 * time.Second,
			ResetTimeout:     30 * time.Second,
			HalfOpenMax:      3,
		},
		RateLimiting: RateLimitConfig{
			Enabled:        false,
			RequestsPerSec: 1000,
			Burst:          100,
			PerClient:      false,
		},
		HealthCheck: HealthCheckConfig{
			Enabled:              true,
			Interval:             10 * time.Second,
			Timeout:              5 * time.Second,
			Path:                 "/_/health",
			FailureThreshold:     3,
			SuccessThreshold:     2,
			EnablePassive:        true,
			PassiveFailThreshold: 5,
		},
		LoadBalancing: LoadBalancingConfig{
			Strategy: LBRoundRobin,
		},
		TrafficSplit: TrafficSplitConfig{
			Enabled: false,
		},
		Auth: AuthConfig{
			Enabled:        false,
			ForwardHeaders: true,
		},
		TLS: TLSConfig{
			Enabled:    false,
			MinVersion: "1.2",
		},
		IPFilter: IPFilterConfig{
			Enabled: false,
		},
		CORS: CORSConfig{
			Enabled:      false,
			AllowOrigins: []string{"*"},
			AllowMethods: []string{"GET", "POST", "PUT", "DELETE", "PATCH", "OPTIONS"},
			AllowHeaders: []string{"*"},
			MaxAge:       86400,
		},
		Caching: CachingConfig{
			Enabled:    false,
			DefaultTTL: 5 * time.Minute,
			MaxSize:    1000,
			Methods:    []string{"GET", "HEAD"},
		},
		Discovery: DiscoveryConfig{
			Enabled:        true,
			PollInterval:   30 * time.Second,
			WatchMode:      true,
			ServiceFilters: []ServiceFilter{},
			AutoPrefix:     true,
			PrefixTemplate: "/{{.ServiceName}}",
			StripPrefix:    true,
		},
		Metrics: MetricsConfig{
			Enabled: true,
			Prefix:  "gateway",
		},
		Tracing: TracingConfig{
			Enabled:         true,
			PropagateFormat: "w3c",
			SampleRate:      1.0,
		},
		AccessLog: AccessLogConfig{
			Enabled:        true,
			RedactHeaders:  []string{"Authorization", "Cookie", "Set-Cookie"},
			IncludeBody:    false,
			MaxBodyLogSize: 4096,
		},
		OpenAPI: DefaultOpenAPIConfig(),
		Dashboard: DashboardConfig{
			Enabled:  true,
			BasePath: "/gateway",
			Title:    "Forge Gateway",
			Realtime: true,
		},
		WebSocket: WebSocketConfig{
			ReadBufferSize:   4096,
			WriteBufferSize:  4096,
			HandshakeTimeout: 10 * time.Second,
			PingInterval:     30 * time.Second,
			PongTimeout:      60 * time.Second,
		},
		SSE: SSEConfig{
			FlushInterval: 100 * time.Millisecond,
		},
	}
}

// ConfigOption configures the gateway extension.
type ConfigOption func(*Config)

// WithEnabled sets whether the gateway is enabled.
func WithEnabled(enabled bool) ConfigOption {
	return func(c *Config) { c.Enabled = enabled }
}

// WithBasePath sets the base path prefix for all gateway routes.
func WithBasePath(path string) ConfigOption {
	return func(c *Config) { c.BasePath = path }
}

// WithRoute adds a manual route configuration.
func WithRoute(route RouteConfig) ConfigOption {
	return func(c *Config) { c.Routes = append(c.Routes, route) }
}

// WithRoutes sets all manual route configurations.
func WithRoutes(routes []RouteConfig) ConfigOption {
	return func(c *Config) { c.Routes = routes }
}

// WithTimeouts sets the timeout configuration.
func WithTimeouts(timeouts TimeoutConfig) ConfigOption {
	return func(c *Config) { c.Timeouts = timeouts }
}

// WithRetry sets the retry configuration.
func WithRetry(retry RetryConfig) ConfigOption {
	return func(c *Config) { c.Retry = retry }
}

// WithCircuitBreaker sets the circuit breaker configuration.
func WithCircuitBreaker(cb CircuitBreakerConfig) ConfigOption {
	return func(c *Config) { c.CircuitBreaker = cb }
}

// WithRateLimiting sets the rate limiting configuration.
func WithRateLimiting(rl RateLimitConfig) ConfigOption {
	return func(c *Config) { c.RateLimiting = rl }
}

// WithHealthCheck sets the health check configuration.
func WithHealthCheck(hc HealthCheckConfig) ConfigOption {
	return func(c *Config) { c.HealthCheck = hc }
}

// WithLoadBalancing sets the load balancing strategy.
func WithLoadBalancing(lb LoadBalancingConfig) ConfigOption {
	return func(c *Config) { c.LoadBalancing = lb }
}

// WithAuth sets the auth configuration.
func WithAuth(auth AuthConfig) ConfigOption {
	return func(c *Config) { c.Auth = auth }
}

// WithTLS sets the TLS configuration.
func WithTLS(tls TLSConfig) ConfigOption {
	return func(c *Config) { c.TLS = tls }
}

// WithCaching sets the caching configuration.
func WithCaching(caching CachingConfig) ConfigOption {
	return func(c *Config) { c.Caching = caching }
}

// WithDiscovery sets the discovery configuration.
func WithDiscovery(disc DiscoveryConfig) ConfigOption {
	return func(c *Config) { c.Discovery = disc }
}

// WithDiscoveryEnabled enables/disables FARP discovery.
func WithDiscoveryEnabled(enabled bool) ConfigOption {
	return func(c *Config) { c.Discovery.Enabled = enabled }
}

// WithDiscoveryPollInterval sets the service discovery polling interval.
func WithDiscoveryPollInterval(d time.Duration) ConfigOption {
	return func(c *Config) { c.Discovery.PollInterval = d }
}

// WithDiscoveryWatchMode sets whether discovery uses watch mode instead of polling.
func WithDiscoveryWatchMode(enabled bool) ConfigOption {
	return func(c *Config) { c.Discovery.WatchMode = enabled }
}

// WithDiscoveryAutoPrefix sets whether discovered services get automatic path prefixes.
func WithDiscoveryAutoPrefix(enabled bool) ConfigOption {
	return func(c *Config) { c.Discovery.AutoPrefix = enabled }
}

// WithDiscoveryPrefixTemplate sets the template for auto-generated route prefixes.
func WithDiscoveryPrefixTemplate(tmpl string) ConfigOption {
	return func(c *Config) { c.Discovery.PrefixTemplate = tmpl }
}

// WithDiscoveryStripPrefix sets whether auto-generated prefixes are stripped before proxying.
func WithDiscoveryStripPrefix(strip bool) ConfigOption {
	return func(c *Config) { c.Discovery.StripPrefix = strip }
}

// WithDiscoveryServiceFilters sets the service filters for discovery.
func WithDiscoveryServiceFilters(filters ...ServiceFilter) ConfigOption {
	return func(c *Config) { c.Discovery.ServiceFilters = filters }
}

// WithMetrics sets the metrics configuration.
func WithMetrics(m MetricsConfig) ConfigOption {
	return func(c *Config) { c.Metrics = m }
}

// WithTracing sets the tracing configuration.
func WithTracing(t TracingConfig) ConfigOption {
	return func(c *Config) { c.Tracing = t }
}

// WithAccessLog sets the access log configuration.
func WithAccessLog(al AccessLogConfig) ConfigOption {
	return func(c *Config) { c.AccessLog = al }
}

// WithOpenAPI sets the OpenAPI aggregation configuration.
func WithOpenAPI(o OpenAPIConfig) ConfigOption {
	return func(c *Config) { c.OpenAPI = o }
}

// WithOpenAPIEnabled enables/disables OpenAPI aggregation.
func WithOpenAPIEnabled(enabled bool) ConfigOption {
	return func(c *Config) { c.OpenAPI.Enabled = enabled }
}

// WithDashboard sets the dashboard configuration.
func WithDashboard(d DashboardConfig) ConfigOption {
	return func(c *Config) { c.Dashboard = d }
}

// WithDashboardEnabled enables/disables the admin dashboard.
func WithDashboardEnabled(enabled bool) ConfigOption {
	return func(c *Config) { c.Dashboard.Enabled = enabled }
}

// WithCORS sets the CORS configuration.
func WithCORS(cors CORSConfig) ConfigOption {
	return func(c *Config) { c.CORS = cors }
}

// WithIPFilter sets the IP filter configuration.
func WithIPFilter(ipf IPFilterConfig) ConfigOption {
	return func(c *Config) { c.IPFilter = ipf }
}

// WithWebSocket sets the WebSocket proxy configuration.
func WithWebSocket(ws WebSocketConfig) ConfigOption {
	return func(c *Config) { c.WebSocket = ws }
}

// WithSSE sets the SSE proxy configuration.
func WithSSE(sse SSEConfig) ConfigOption {
	return func(c *Config) { c.SSE = sse }
}

// WithConfig sets the complete config.
func WithConfig(config Config) ConfigOption {
	return func(c *Config) { *c = config }
}
