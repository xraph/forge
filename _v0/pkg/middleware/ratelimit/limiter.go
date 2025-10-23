package ratelimit

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/xraph/forge/v0/pkg/common"
	"github.com/xraph/forge/v0/pkg/logger"
	"github.com/xraph/forge/v0/pkg/middleware"
)

// Limiter defines the interface for rate limiting implementations
type Limiter interface {
	// Allow checks if a request should be allowed
	Allow(ctx context.Context, key string) (bool, *LimitInfo, error)
	// Reset resets the limit for a key
	Reset(ctx context.Context, key string) error
	// Close closes the limiter
	Close() error
}

// LimitInfo contains information about the current limit status
type LimitInfo struct {
	Limit      int           `json:"limit"`
	Remaining  int           `json:"remaining"`
	ResetTime  time.Time     `json:"reset_time"`
	RetryAfter time.Duration `json:"retry_after"`
}

// Config contains configuration for rate limiting
type Config struct {
	// Rate limiting configuration
	RequestsPerSecond int           `yaml:"requests_per_second" json:"requests_per_second" default:"100"`
	RequestsPerMinute int           `yaml:"requests_per_minute" json:"requests_per_minute" default:"1000"`
	RequestsPerHour   int           `yaml:"requests_per_hour" json:"requests_per_hour" default:"10000"`
	BurstSize         int           `yaml:"burst_size" json:"burst_size" default:"10"`
	WindowSize        time.Duration `yaml:"window_size" json:"window_size" default:"1m"`

	// Key generation
	KeyGenerator string   `yaml:"key_generator" json:"key_generator" default:"ip"` // ip, user, custom
	CustomHeader string   `yaml:"custom_header" json:"custom_header"`
	SkipPaths    []string `yaml:"skip_paths" json:"skip_paths"`

	// Storage backend
	Backend string `yaml:"backend" json:"backend" default:"memory"` // memory, redis
	Redis   struct {
		Addr     string `yaml:"addr" json:"addr"`
		Password string `yaml:"password" json:"password"`
		DB       int    `yaml:"db" json:"db"`
	} `yaml:"redis" json:"redis"`

	// Response configuration
	IncludeHeaders bool   `yaml:"include_headers" json:"include_headers" default:"true"`
	ErrorMessage   string `yaml:"error_message" json:"error_message" default:"Rate limit exceeded"`
	ErrorCode      string `yaml:"error_code" json:"error_code" default:"RATE_LIMIT_EXCEEDED"`

	// Advanced options
	SkipSuccessfulRequests bool     `yaml:"skip_successful_requests" json:"skip_successful_requests"`
	SkipFailedRequests     bool     `yaml:"skip_failed_requests" json:"skip_failed_requests"`
	TrustedProxies         []string `yaml:"trusted_proxies" json:"trusted_proxies"`
}

// RateLimitMiddleware implements rate limiting middleware
type RateLimitMiddleware struct {
	*middleware.BaseServiceMiddleware
	config  Config
	limiter Limiter
}

// NewRateLimitMiddleware creates a new rate limiting middleware
func NewRateLimitMiddleware(config Config) *RateLimitMiddleware {
	return &RateLimitMiddleware{
		BaseServiceMiddleware: middleware.NewBaseServiceMiddleware("rate-limit", 5, []string{"config-manager"}),
		config:                config,
	}
}

// Initialize initializes the rate limiting middleware
func (rl *RateLimitMiddleware) Initialize(container common.Container) error {
	if err := rl.BaseServiceMiddleware.Initialize(container); err != nil {
		return err
	}

	// Load configuration from container if needed
	if configManager, err := container.Resolve((*common.ConfigManager)(nil)); err == nil {
		var rateLimitConfig Config
		if err := configManager.(common.ConfigManager).Bind("middleware.ratelimit", &rateLimitConfig); err == nil {
			rl.config = rateLimitConfig
		}
	}

	// Set defaults
	if rl.config.RequestsPerSecond == 0 {
		rl.config.RequestsPerSecond = 100
	}
	if rl.config.RequestsPerMinute == 0 {
		rl.config.RequestsPerMinute = 1000
	}
	if rl.config.RequestsPerHour == 0 {
		rl.config.RequestsPerHour = 10000
	}
	if rl.config.BurstSize == 0 {
		rl.config.BurstSize = 10
	}
	if rl.config.WindowSize == 0 {
		rl.config.WindowSize = time.Minute
	}
	if rl.config.KeyGenerator == "" {
		rl.config.KeyGenerator = "ip"
	}
	if rl.config.Backend == "" {
		rl.config.Backend = "memory"
	}
	if rl.config.ErrorMessage == "" {
		rl.config.ErrorMessage = "Rate limit exceeded"
	}
	if rl.config.ErrorCode == "" {
		rl.config.ErrorCode = "RATE_LIMIT_EXCEEDED"
	}

	// Initialize limiter based on backend
	var err error
	switch rl.config.Backend {
	case "memory":
		rl.limiter, err = NewMemoryLimiter(rl.config)
	case "redis":
		rl.limiter, err = NewRedisLimiter(rl.config)
	default:
		return fmt.Errorf("unsupported rate limit backend: %s", rl.config.Backend)
	}

	return err
}

// OnStop stops the rate limiting middleware
func (rl *RateLimitMiddleware) OnStop(ctx context.Context) error {
	if rl.limiter != nil {
		rl.limiter.Close()
	}
	return rl.BaseServiceMiddleware.OnStop(ctx)
}

// Handler returns the rate limiting handler
func (rl *RateLimitMiddleware) Handler() func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			// Update call count
			rl.UpdateStats(1, 0, 0, nil)

			// Check if path should be skipped
			if rl.shouldSkipPath(r.URL.Path) {
				next.ServeHTTP(w, r)
				rl.UpdateStats(0, 0, time.Since(start), nil)
				return
			}

			// Generate rate limit key
			key := rl.generateKey(r)

			// Check rate limit
			allowed, limitInfo, err := rl.limiter.Allow(r.Context(), key)
			if err != nil {
				rl.UpdateStats(0, 1, time.Since(start), err)
				// Log error but don't block the request
				if rl.Logger() != nil {
					rl.Logger().Error("rate limit check failed", logger.Error(err))
				}
				next.ServeHTTP(w, r)
				return
			}

			// Add rate limit headers if configured
			if rl.config.IncludeHeaders {
				rl.addHeaders(w, limitInfo)
			}

			// Block request if rate limit exceeded
			if !allowed {
				rl.UpdateStats(0, 1, time.Since(start), fmt.Errorf("rate limit exceeded"))
				rl.writeRateLimitResponse(w, limitInfo)
				return
			}

			// Wrap response writer to capture status code
			wrapper := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

			// Continue to next handler
			next.ServeHTTP(wrapper, r)

			// Update statistics based on response
			latency := time.Since(start)
			if wrapper.statusCode >= 400 {
				// Optionally don't count failed requests against rate limit
				if rl.config.SkipFailedRequests {
					rl.limiter.Reset(r.Context(), key)
				}
			} else if rl.config.SkipSuccessfulRequests {
				// Optionally don't count successful requests
				rl.limiter.Reset(r.Context(), key)
			}

			rl.UpdateStats(0, 0, latency, nil)
		})
	}
}

// generateKey generates a rate limiting key based on configuration
func (rl *RateLimitMiddleware) generateKey(r *http.Request) string {
	switch rl.config.KeyGenerator {
	case "ip":
		return rl.getClientIP(r)
	case "user":
		// Try to get user ID from context
		if userID := r.Context().Value("user_id"); userID != nil {
			if uid, ok := userID.(string); ok && uid != "" {
				return fmt.Sprintf("user:%s", uid)
			}
		}
		// Fallback to IP if no user ID
		return rl.getClientIP(r)
	case "custom":
		if rl.config.CustomHeader != "" {
			if value := r.Header.Get(rl.config.CustomHeader); value != "" {
				return fmt.Sprintf("custom:%s", value)
			}
		}
		// Fallback to IP if no custom header
		return rl.getClientIP(r)
	default:
		return rl.getClientIP(r)
	}
}

// getClientIP extracts the client IP address from the request
func (rl *RateLimitMiddleware) getClientIP(r *http.Request) string {
	// Check trusted proxy headers
	if len(rl.config.TrustedProxies) > 0 {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			ips := strings.Split(xff, ",")
			if len(ips) > 0 {
				ip := strings.TrimSpace(ips[0])
				if rl.isTrustedProxy(r.RemoteAddr) {
					return fmt.Sprintf("ip:%s", ip)
				}
			}
		}

		if xri := r.Header.Get("X-Real-IP"); xri != "" {
			if rl.isTrustedProxy(r.RemoteAddr) {
				return fmt.Sprintf("ip:%s", strings.TrimSpace(xri))
			}
		}
	}

	// Use remote address
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		ip = r.RemoteAddr
	}

	return fmt.Sprintf("ip:%s", ip)
}

// isTrustedProxy checks if the request comes from a trusted proxy
func (rl *RateLimitMiddleware) isTrustedProxy(remoteAddr string) bool {
	ip, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		ip = remoteAddr
	}

	for _, trusted := range rl.config.TrustedProxies {
		if ip == trusted {
			return true
		}

		// Check CIDR ranges
		if strings.Contains(trusted, "/") {
			_, network, err := net.ParseCIDR(trusted)
			if err == nil {
				if parsedIP := net.ParseIP(ip); parsedIP != nil {
					if network.Contains(parsedIP) {
						return true
					}
				}
			}
		}
	}

	return false
}

// shouldSkipPath checks if the current path should skip rate limiting
func (rl *RateLimitMiddleware) shouldSkipPath(path string) bool {
	for _, skipPath := range rl.config.SkipPaths {
		if strings.HasPrefix(path, skipPath) {
			return true
		}
	}
	return false
}

// addHeaders adds rate limit headers to the response
func (rl *RateLimitMiddleware) addHeaders(w http.ResponseWriter, info *LimitInfo) {
	w.Header().Set("X-RateLimit-Limit", strconv.Itoa(info.Limit))
	w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(info.Remaining))
	w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(info.ResetTime.Unix(), 10))

	if info.RetryAfter > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(int(info.RetryAfter.Seconds())))
	}
}

// writeRateLimitResponse writes a rate limit exceeded response
func (rl *RateLimitMiddleware) writeRateLimitResponse(w http.ResponseWriter, info *LimitInfo) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusTooManyRequests)

	response := map[string]interface{}{
		"error": map[string]interface{}{
			"code":    rl.config.ErrorCode,
			"message": rl.config.ErrorMessage,
			"details": map[string]interface{}{
				"limit":       info.Limit,
				"remaining":   info.Remaining,
				"reset_time":  info.ResetTime.Unix(),
				"retry_after": int(info.RetryAfter.Seconds()),
			},
		},
	}

	json.NewEncoder(w).Encode(response)
}

// responseWriter wraps http.ResponseWriter to capture status code
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}
