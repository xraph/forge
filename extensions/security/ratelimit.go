package security

import (
	"net"
	"net/http"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/xraph/forge"
)

// RateLimitConfig holds rate limiting configuration.
type RateLimitConfig struct {
	// Enabled determines if rate limiting is enabled
	Enabled bool `yaml:"enabled"`

	// RequestsPerWindow is the maximum number of requests allowed per window
	RequestsPerWindow int `yaml:"requests_per_window"`

	// Window is the time window for rate limiting
	Window time.Duration `yaml:"window"`

	// Requests are keyed by client IP. By default that is the TCP peer
	// address (RemoteAddr). X-Forwarded-For and X-Real-IP are client
	// controlled, so they are only read when the peer is one of
	// TrustedProxies. For custom key extraction, wrap the middleware with
	// your own logic.

	// TrustedProxies lists the reverse proxies, as IPs or CIDR ranges, whose
	// forwarding headers are believed. When the peer is in this list the
	// client IP is the right-most X-Forwarded-For entry that is not itself a
	// trusted proxy, falling back to X-Real-IP. Leave it empty when the app
	// is reached directly. Invalid entries are ignored and logged.
	TrustedProxies []string `yaml:"trusted_proxies"`

	// SkipPaths is a list of paths to skip rate limiting
	SkipPaths []string `yaml:"skip_paths"`

	// AutoApplyMiddleware automatically applies rate limiting middleware globally
	AutoApplyMiddleware bool `yaml:"auto_apply_middleware"`

	// Store specifies the rate limit store backend ("memory", "redis")
	// For now, only memory is implemented
	Store string `yaml:"store"`
}

// RateLimitInfo contains information about the current rate limit status.
type RateLimitInfo struct {
	Limit      int           // Maximum requests allowed
	Remaining  int           // Remaining requests in current window
	Reset      time.Time     // When the rate limit resets
	RetryAfter time.Duration // How long to wait before retrying
}

// DefaultRateLimitConfig returns the default rate limit configuration.
func DefaultRateLimitConfig() RateLimitConfig {
	return RateLimitConfig{
		Enabled:             true,
		RequestsPerWindow:   100,
		Window:              1 * time.Minute,
		SkipPaths:           []string{"/health", "/metrics"},
		AutoApplyMiddleware: false, // Default to false for backwards compatibility
		Store:               "memory",
	}
}

// tokenBucket represents a token bucket for rate limiting.
type tokenBucket struct {
	tokens       int       // Current number of tokens
	lastRefill   time.Time // Last time tokens were refilled
	capacity     int       // Maximum number of tokens
	refillRate   int       // Tokens added per refill period
	refillPeriod time.Duration
	mu           sync.Mutex
}

// newTokenBucket creates a new token bucket.
func newTokenBucket(capacity int, window time.Duration) *tokenBucket {
	return &tokenBucket{
		tokens:       capacity,
		lastRefill:   time.Now(),
		capacity:     capacity,
		refillRate:   capacity,
		refillPeriod: window,
	}
}

// take attempts to take n tokens from the bucket
// Returns true if tokens were taken, false if not enough tokens available.
func (tb *tokenBucket) take(n int) (bool, *RateLimitInfo) {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	// Refill tokens based on elapsed time
	now := time.Now()
	elapsed := now.Sub(tb.lastRefill)

	if elapsed >= tb.refillPeriod {
		// Full refill if a full period has elapsed
		tb.tokens = tb.capacity
		tb.lastRefill = now
	}

	// Check if we have enough tokens
	if tb.tokens >= n {
		tb.tokens -= n

		return true, &RateLimitInfo{
			Limit:      tb.capacity,
			Remaining:  tb.tokens,
			Reset:      tb.lastRefill.Add(tb.refillPeriod),
			RetryAfter: 0,
		}
	}

	// Not enough tokens - calculate retry after
	timeUntilRefill := tb.refillPeriod - elapsed

	return false, &RateLimitInfo{
		Limit:      tb.capacity,
		Remaining:  0,
		Reset:      tb.lastRefill.Add(tb.refillPeriod),
		RetryAfter: timeUntilRefill,
	}
}

// MemoryRateLimiter implements in-memory rate limiting.
type MemoryRateLimiter struct {
	config         RateLimitConfig
	buckets        sync.Map // map[string]*tokenBucket
	logger         forge.Logger
	metrics        forge.Metrics
	trustedProxies []netip.Prefix
	stopCh         chan struct{}
	stopOnce       sync.Once
}

// NewMemoryRateLimiter creates a new in-memory rate limiter.
func NewMemoryRateLimiter(config RateLimitConfig, logger forge.Logger, metrics forge.Metrics) *MemoryRateLimiter {
	if config.RequestsPerWindow <= 0 {
		config.RequestsPerWindow = 100
	}

	if config.Window <= 0 {
		config.Window = 1 * time.Minute
	}

	limiter := &MemoryRateLimiter{
		config:         config,
		logger:         logger,
		metrics:        metrics,
		trustedProxies: parseTrustedProxies(config.TrustedProxies, logger),
		stopCh:         make(chan struct{}),
	}

	// Start cleanup goroutine; Stop ends it.
	go limiter.cleanup()

	return limiter
}

// Stop ends the background cleanup goroutine. It is safe to call more than
// once. The limiter keeps working after Stop, it just no longer evicts idle
// buckets.
func (rl *MemoryRateLimiter) Stop() {
	rl.stopOnce.Do(func() { close(rl.stopCh) })
}

// cleanup periodically removes expired buckets.
func (rl *MemoryRateLimiter) cleanup() {
	ticker := time.NewTicker(rl.config.Window * 2)
	defer ticker.Stop()

	for {
		select {
		case <-rl.stopCh:
			return
		case <-ticker.C:
		}

		now := time.Now()

		rl.buckets.Range(func(key, value any) bool {
			bucket := value.(*tokenBucket)
			bucket.mu.Lock()
			// Remove bucket if it hasn't been used in 2 windows
			if now.Sub(bucket.lastRefill) > rl.config.Window*2 {
				rl.buckets.Delete(key)
			}

			bucket.mu.Unlock()

			return true
		})
	}
}

// Allow checks if a request is allowed based on rate limit.
func (rl *MemoryRateLimiter) Allow(key string) (bool, *RateLimitInfo) {
	// Load first: the bucket almost always exists, and building one just to
	// throw it away cost three allocations per request.
	val, ok := rl.buckets.Load(key)
	if !ok {
		val, _ = rl.buckets.LoadOrStore(key, newTokenBucket(rl.config.RequestsPerWindow, rl.config.Window))
	}

	bucket := val.(*tokenBucket)

	// Try to take a token
	allowed, info := bucket.take(1)

	if !allowed {
		// Rate limit exceeded
		if rl.metrics != nil {
			rl.metrics.Counter("security.ratelimit.exceeded").Inc()
		}
	}

	return allowed, info
}

// shouldSkipPath checks if the path should skip rate limiting.
func (rl *MemoryRateLimiter) shouldSkipPath(path string) bool {
	return shouldSkipPath(path, rl.config.SkipPaths)
}

// RateLimitMiddleware returns a middleware function for rate limiting.
func RateLimitMiddleware(limiter *MemoryRateLimiter) forge.Middleware {
	return func(next forge.Handler) forge.Handler {
		return func(ctx forge.Context) error {
			if !limiter.config.Enabled {
				return next(ctx)
			}

			r := ctx.Request()
			w := ctx.Response()

			// Skip rate limiting for specified paths
			if limiter.shouldSkipPath(r.URL.Path) {
				return next(ctx)
			}

			// Extract rate limit key
			key := limiter.extractRateLimitKey(r)
			if key == "" {
				limiter.logger.Warn("rate limit key extraction failed, allowing request")

				return next(ctx)
			}

			// Check rate limit
			allowed, info := limiter.Allow(key)

			// Set rate limit headers
			w.Header().Set("X-Ratelimit-Limit", strconv.Itoa(info.Limit))
			w.Header().Set("X-Ratelimit-Remaining", strconv.Itoa(info.Remaining))
			w.Header().Set("X-Ratelimit-Reset", strconv.FormatInt(info.Reset.Unix(), 10))

			if !allowed {
				limiter.logger.Warn("rate limit exceeded",
					forge.F("key", key),
					forge.F("path", r.URL.Path),
				)
				w.Header().Set("Retry-After", strconv.Itoa(int(info.RetryAfter.Seconds())))

				return ctx.String(http.StatusTooManyRequests, "Rate limit exceeded")
			}

			return next(ctx)
		}
	}
}

// extractRateLimitKey returns the client IP to rate limit on.
//
// The TCP peer is the only address the client cannot forge, so it is the key
// unless the peer is a configured trusted proxy. Then X-Forwarded-For is read
// from the right, skipping trusted proxies, and the first other address is
// the client. Entries further left were written by the client and are never
// used. Taking the left-most entry, as this used to, let any client pick its
// own bucket on every request, or exhaust someone else's.
func (rl *MemoryRateLimiter) extractRateLimitKey(r *http.Request) string {
	peer, ok := parseIPHost(r.RemoteAddr)
	if !ok {
		return r.RemoteAddr
	}

	if !rl.isTrustedProxy(peer) {
		return peer.String()
	}

	if xff := r.Header.Values("X-Forwarded-For"); len(xff) > 0 {
		hops := strings.Split(strings.Join(xff, ","), ",")
		for _, hop := range slices.Backward(hops) {
			hop, ok := parseIP(hop)
			if !ok {
				// A malformed entry appended by a trusted proxy. Do not
				// guess past it; limit on the proxy instead.
				return peer.String()
			}

			if !rl.isTrustedProxy(hop) {
				return hop.String()
			}
		}
	}

	if ip, ok := parseIP(r.Header.Get("X-Real-IP")); ok {
		return ip.String()
	}

	return peer.String()
}

// isTrustedProxy reports whether ip is one of the configured proxies.
func (rl *MemoryRateLimiter) isTrustedProxy(ip netip.Addr) bool {
	for _, p := range rl.trustedProxies {
		if p.Contains(ip) {
			return true
		}
	}

	return false
}

// parseTrustedProxies turns IPs and CIDR ranges into prefixes. A bare IP
// becomes a single-address prefix.
func parseTrustedProxies(entries []string, logger forge.Logger) []netip.Prefix {
	prefixes := make([]netip.Prefix, 0, len(entries))

	for _, e := range entries {
		e = strings.TrimSpace(e)
		if e == "" {
			continue
		}

		if p, err := netip.ParsePrefix(e); err == nil {
			prefixes = append(prefixes, p.Masked())

			continue
		}

		if ip, ok := parseIP(e); ok {
			prefixes = append(prefixes, netip.PrefixFrom(ip, ip.BitLen()))

			continue
		}

		if logger != nil {
			logger.Warn("ignoring invalid trusted proxy entry", forge.F("entry", e))
		}
	}

	return prefixes
}

// parseIP parses an IP address, trimming spaces and folding IPv4-mapped IPv6
// addresses to IPv4 so one client has one key.
func parseIP(s string) (netip.Addr, bool) {
	ip, err := netip.ParseAddr(strings.TrimSpace(s))
	if err != nil {
		return netip.Addr{}, false
	}

	return ip.Unmap().WithZone(""), true
}

// parseIPHost parses the host part of a "host:port" address, or a bare IP.
func parseIPHost(addr string) (netip.Addr, bool) {
	if host, _, err := net.SplitHostPort(addr); err == nil {
		return parseIP(host)
	}

	return parseIP(addr)
}

// WithRateLimitPerIP creates a rate limiter that limits by IP address.
func WithRateLimitPerIP(requests int, window time.Duration) ConfigOption {
	return func(c *Config) {
		c.RateLimit.Enabled = true
		c.RateLimit.RequestsPerWindow = requests
		c.RateLimit.Window = window
	}
}

// WithRateLimitPerUser creates a rate limiter that limits by user ID
// Note: This requires session middleware to be active.
func WithRateLimitPerUser(requests int, window time.Duration) ConfigOption {
	return func(c *Config) {
		c.RateLimit.Enabled = true
		c.RateLimit.RequestsPerWindow = requests
		c.RateLimit.Window = window
		// KeyFunc removed - use custom middleware wrapper for user-based rate limiting
	}
}

// WithRateLimitTrustedProxies sets the reverse proxies whose X-Forwarded-For
// and X-Real-IP headers the rate limiter believes.
func WithRateLimitTrustedProxies(proxies ...string) ConfigOption {
	return func(c *Config) {
		c.RateLimit.TrustedProxies = proxies
	}
}
