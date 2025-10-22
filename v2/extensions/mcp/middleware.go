package mcp

import (
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/xraph/forge/v2"
)

// AuthMiddleware creates middleware for MCP authentication
func AuthMiddleware(config Config, logger forge.Logger) forge.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Skip if auth not required
			if !config.RequireAuth {
				next.ServeHTTP(w, r)
				return
			}

			// Get auth header
			authHeader := r.Header.Get(config.AuthHeader)
			if authHeader == "" {
				logger.Warn("mcp: missing auth header", forge.F("path", r.URL.Path))
				http.Error(w, "Unauthorized: missing authentication", http.StatusUnauthorized)
				return
			}

			// Extract token (handle "Bearer <token>" format)
			token := authHeader
			if strings.HasPrefix(authHeader, "Bearer ") {
				token = strings.TrimPrefix(authHeader, "Bearer ")
			}

			// Validate token
			valid := false
			for _, validToken := range config.AuthTokens {
				if token == validToken {
					valid = true
					break
				}
			}

			if !valid {
				logger.Warn("mcp: invalid auth token", forge.F("path", r.URL.Path))
				http.Error(w, "Unauthorized: invalid token", http.StatusUnauthorized)
				return
			}

			logger.Debug("mcp: auth successful", forge.F("path", r.URL.Path))
			next.ServeHTTP(w, r)
		})
	}
}

// RateLimiter tracks request rates
type RateLimiter struct {
	config    Config
	logger    forge.Logger
	requests  map[string]*requestInfo
	mu        sync.RWMutex
	cleanupCh chan struct{}
}

type requestInfo struct {
	count     int
	resetTime time.Time
}

// NewRateLimiter creates a new rate limiter
func NewRateLimiter(config Config, logger forge.Logger) *RateLimiter {
	rl := &RateLimiter{
		config:    config,
		logger:    logger,
		requests:  make(map[string]*requestInfo),
		cleanupCh: make(chan struct{}),
	}

	// Start cleanup goroutine
	go rl.cleanup()

	return rl
}

// RateLimitMiddleware creates middleware for rate limiting
func RateLimitMiddleware(config Config, logger forge.Logger, metrics forge.Metrics) forge.Middleware {
	// Skip if rate limiting not configured
	if config.RateLimitPerMinute <= 0 {
		return func(next http.Handler) http.Handler {
			return next
		}
	}

	limiter := NewRateLimiter(config, logger)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Get client identifier (IP or auth token)
			clientID := getClientID(r, config)

			// Check rate limit
			if !limiter.Allow(clientID) {
				logger.Warn("mcp: rate limit exceeded",
					forge.F("client", clientID),
					forge.F("path", r.URL.Path),
				)

				if metrics != nil {
					metrics.Counter("mcp_rate_limit_exceeded_total").Inc()
				}

				w.Header().Set("X-RateLimit-Limit", string(rune(config.RateLimitPerMinute)))
				w.Header().Set("Retry-After", "60")
				http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
				return
			}

			// Set rate limit headers
			info := limiter.GetInfo(clientID)
			if info != nil {
				w.Header().Set("X-RateLimit-Limit", string(rune(config.RateLimitPerMinute)))
				w.Header().Set("X-RateLimit-Remaining", string(rune(config.RateLimitPerMinute-info.count)))
				w.Header().Set("X-RateLimit-Reset", string(rune(info.resetTime.Unix())))
			}

			next.ServeHTTP(w, r)
		})
	}
}

// Allow checks if a request is allowed
func (rl *RateLimiter) Allow(clientID string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()

	// Get or create request info
	info, exists := rl.requests[clientID]
	if !exists || now.After(info.resetTime) {
		// New window
		rl.requests[clientID] = &requestInfo{
			count:     1,
			resetTime: now.Add(time.Minute),
		}
		return true
	}

	// Check limit
	if info.count >= rl.config.RateLimitPerMinute {
		return false
	}

	// Increment counter
	info.count++
	return true
}

// GetInfo returns rate limit info for a client
func (rl *RateLimiter) GetInfo(clientID string) *requestInfo {
	rl.mu.RLock()
	defer rl.mu.RUnlock()

	return rl.requests[clientID]
}

// cleanup periodically removes expired entries
func (rl *RateLimiter) cleanup() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			rl.mu.Lock()
			now := time.Now()
			for clientID, info := range rl.requests {
				if now.After(info.resetTime) {
					delete(rl.requests, clientID)
				}
			}
			rl.mu.Unlock()
		case <-rl.cleanupCh:
			return
		}
	}
}

// Stop stops the rate limiter cleanup goroutine
func (rl *RateLimiter) Stop() {
	close(rl.cleanupCh)
}

// getClientID extracts a client identifier from the request
func getClientID(r *http.Request, config Config) string {
	// Try auth token first (more stable than IP)
	if config.RequireAuth {
		authHeader := r.Header.Get(config.AuthHeader)
		if authHeader != "" {
			token := authHeader
			if strings.HasPrefix(authHeader, "Bearer ") {
				token = strings.TrimPrefix(authHeader, "Bearer ")
			}
			return "token:" + token
		}
	}

	// Fall back to IP address
	ip := r.RemoteAddr
	if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
		ip = strings.Split(forwarded, ",")[0]
	}
	return "ip:" + ip
}
