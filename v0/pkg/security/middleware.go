package security

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/logger"
)

// SecurityMiddleware provides security middleware for HTTP requests
type SecurityMiddleware struct {
	authManager  *AuthManager
	authzManager *AuthzManager
	rateLimiter  *RateLimiter
	logger       common.Logger
	config       SecurityConfig
}

// SecurityConfig contains security middleware configuration
type SecurityConfig struct {
	EnableAuth            bool          `yaml:"enable_auth" default:"true"`
	EnableAuthz           bool          `yaml:"enable_authz" default:"true"`
	EnableRateLimit       bool          `yaml:"enable_rate_limit" default:"true"`
	EnableCORS            bool          `yaml:"enable_cors" default:"true"`
	EnableCSRF            bool          `yaml:"enable_csrf" default:"true"`
	EnableSecurityHeaders bool          `yaml:"enable_security_headers" default:"true"`
	TokenHeader           string        `yaml:"token_header" default:"Authorization"`
	TokenPrefix           string        `yaml:"token_prefix" default:"Bearer"`
	RateLimitKey          string        `yaml:"rate_limit_key" default:"ip"`
	AllowedOrigins        []string      `yaml:"allowed_origins"`
	AllowedMethods        []string      `yaml:"allowed_methods" default:"GET,POST,PUT,DELETE,OPTIONS"`
	AllowedHeaders        []string      `yaml:"allowed_headers"`
	MaxAge                time.Duration `yaml:"max_age" default:"86400s"`
	Logger                common.Logger `yaml:"-"`
}

// SecurityContext represents the security context for a request
type SecurityContext struct {
	User        *User             `json:"user"`
	Claims      *Claims           `json:"claims"`
	Permissions []string          `json:"permissions"`
	Attributes  map[string]string `json:"attributes"`
	IP          string            `json:"ip"`
	UserAgent   string            `json:"user_agent"`
	RequestID   string            `json:"request_id"`
}

// NewSecurityMiddleware creates a new security middleware
func NewSecurityMiddleware(
	authManager *AuthManager,
	authzManager *AuthzManager,
	rateLimiter *RateLimiter,
	config SecurityConfig,
) *SecurityMiddleware {
	if config.Logger == nil {
		config.Logger = logger.NewLogger(logger.LoggingConfig{Level: "info"})
	}

	return &SecurityMiddleware{
		authManager:  authManager,
		authzManager: authzManager,
		rateLimiter:  rateLimiter,
		logger:       config.Logger,
		config:       config,
	}
}

// AuthMiddleware provides authentication middleware
func (sm *SecurityMiddleware) AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !sm.config.EnableAuth {
			next.ServeHTTP(w, r)
			return
		}

		// Extract token from header
		token := sm.extractToken(r)
		if token == "" {
			sm.writeErrorResponse(w, http.StatusUnauthorized, "missing authentication token")
			return
		}

		// Validate token
		claims, err := sm.authManager.ValidateToken(r.Context(), token)
		if err != nil {
			sm.logger.Warn("authentication failed", logger.String("error", err.Error()))
			sm.writeErrorResponse(w, http.StatusUnauthorized, "invalid authentication token")
			return
		}

		// Create user object from claims
		user := &User{
			ID:          claims.UserID,
			Username:    claims.Username,
			Email:       claims.Email,
			Roles:       claims.Roles,
			Permissions: claims.Permissions,
			Attributes:  claims.Attributes,
		}

		// Create security context
		securityCtx := &SecurityContext{
			User:      user,
			Claims:    claims,
			IP:        sm.getClientIP(r),
			UserAgent: r.UserAgent(),
			RequestID: sm.getRequestID(r),
		}

		// Add security context to request context
		ctx := context.WithValue(r.Context(), "security", securityCtx)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// AuthzMiddleware provides authorization middleware
func (sm *SecurityMiddleware) AuthzMiddleware(resource, action string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !sm.config.EnableAuthz {
				next.ServeHTTP(w, r)
				return
			}

			// Get security context
			securityCtx, ok := r.Context().Value("security").(*SecurityContext)
			if !ok {
				sm.writeErrorResponse(w, http.StatusUnauthorized, "authentication required")
				return
			}

			// Create authorization request
			authzReq := AuthzRequest{
				Subject:  securityCtx.User.ID,
				Resource: resource,
				Action:   action,
				Context: map[string]string{
					"ip":         securityCtx.IP,
					"user_agent": securityCtx.UserAgent,
					"method":     r.Method,
					"path":       r.URL.Path,
				},
				Attributes: securityCtx.User.Attributes,
			}

			// Check authorization
			result, err := sm.authzManager.Authorize(r.Context(), authzReq)
			if err != nil {
				sm.logger.Error("authorization failed", logger.String("error", err.Error()))
				sm.writeErrorResponse(w, http.StatusInternalServerError, "authorization check failed")
				return
			}

			if !result.Allowed {
				sm.logger.Warn("authorization denied",
					logger.String("user", securityCtx.User.ID),
					logger.String("resource", resource),
					logger.String("action", action),
					logger.String("reason", result.Reason))
				sm.writeErrorResponse(w, http.StatusForbidden, "access denied")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// RateLimitMiddleware provides rate limiting middleware
func (sm *SecurityMiddleware) RateLimitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !sm.config.EnableRateLimit {
			next.ServeHTTP(w, r)
			return
		}

		// Determine rate limit key
		key := sm.getRateLimitKey(r)

		// Create rate limit request
		rateLimitReq := RateLimitRequest{
			Key: key,
			Context: map[string]string{
				"ip":         sm.getClientIP(r),
				"user_agent": r.UserAgent(),
				"method":     r.Method,
				"path":       r.URL.Path,
			},
		}

		// Check rate limit
		result, err := sm.rateLimiter.CheckRateLimit(r.Context(), rateLimitReq)
		if err != nil {
			sm.logger.Error("rate limit check failed", logger.String("error", err.Error()))
			sm.writeErrorResponse(w, http.StatusInternalServerError, "rate limit check failed")
			return
		}

		if !result.Allowed {
			sm.logger.Warn("rate limit exceeded",
				logger.String("key", key),
				logger.String("limit", fmt.Sprintf("%d", result.Limit)),
				logger.String("retry_after", result.RetryAfter.String()))

			w.Header().Set("X-RateLimit-Limit", fmt.Sprintf("%d", result.Limit))
			w.Header().Set("X-RateLimit-Remaining", "0")
			w.Header().Set("X-RateLimit-Reset", fmt.Sprintf("%d", result.ResetTime.Unix()))
			w.Header().Set("Retry-After", fmt.Sprintf("%.0f", result.RetryAfter.Seconds()))

			sm.writeErrorResponse(w, http.StatusTooManyRequests, "rate limit exceeded")
			return
		}

		// Add rate limit headers
		w.Header().Set("X-RateLimit-Limit", fmt.Sprintf("%d", result.Limit))
		w.Header().Set("X-RateLimit-Remaining", fmt.Sprintf("%d", result.Remaining))
		w.Header().Set("X-RateLimit-Reset", fmt.Sprintf("%d", result.ResetTime.Unix()))

		next.ServeHTTP(w, r)
	})
}

// CORSMiddleware provides CORS middleware
func (sm *SecurityMiddleware) CORSMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !sm.config.EnableCORS {
			next.ServeHTTP(w, r)
			return
		}

		origin := r.Header.Get("Origin")

		// Check if origin is allowed
		if sm.isOriginAllowed(origin) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
		}

		// Set CORS headers
		w.Header().Set("Access-Control-Allow-Methods", strings.Join(sm.config.AllowedMethods, ","))
		w.Header().Set("Access-Control-Allow-Headers", strings.Join(sm.config.AllowedHeaders, ","))
		w.Header().Set("Access-Control-Max-Age", fmt.Sprintf("%.0f", sm.config.MaxAge.Seconds()))

		// Handle preflight requests
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// SecurityHeadersMiddleware provides security headers middleware
func (sm *SecurityMiddleware) SecurityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !sm.config.EnableSecurityHeaders {
			next.ServeHTTP(w, r)
			return
		}

		// Set security headers
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-XSS-Protection", "1; mode=block")
		w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Content-Security-Policy", "default-src 'self'")

		next.ServeHTTP(w, r)
	})
}

// extractToken extracts the authentication token from the request
func (sm *SecurityMiddleware) extractToken(r *http.Request) string {
	authHeader := r.Header.Get(sm.config.TokenHeader)
	if authHeader == "" {
		return ""
	}

	// Check for token prefix
	if sm.config.TokenPrefix != "" {
		prefix := sm.config.TokenPrefix + " "
		if !strings.HasPrefix(authHeader, prefix) {
			return ""
		}
		return strings.TrimPrefix(authHeader, prefix)
	}

	return authHeader
}

// getClientIP gets the client IP address
func (sm *SecurityMiddleware) getClientIP(r *http.Request) string {
	// Check for forwarded headers
	if ip := r.Header.Get("X-Forwarded-For"); ip != "" {
		return strings.Split(ip, ",")[0]
	}
	if ip := r.Header.Get("X-Real-IP"); ip != "" {
		return ip
	}

	// Fall back to remote address
	ip := r.RemoteAddr
	if colon := strings.LastIndex(ip, ":"); colon != -1 {
		ip = ip[:colon]
	}
	return ip
}

// getRequestID gets the request ID from headers or generates one
func (sm *SecurityMiddleware) getRequestID(r *http.Request) string {
	requestID := r.Header.Get("X-Request-ID")
	if requestID == "" {
		requestID = fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return requestID
}

// getRateLimitKey gets the rate limit key for the request
func (sm *SecurityMiddleware) getRateLimitKey(r *http.Request) string {
	switch sm.config.RateLimitKey {
	case "ip":
		return sm.getClientIP(r)
	case "user":
		if securityCtx, ok := r.Context().Value("security").(*SecurityContext); ok {
			return securityCtx.User.ID
		}
		return sm.getClientIP(r)
	case "ip_user":
		if securityCtx, ok := r.Context().Value("security").(*SecurityContext); ok {
			return fmt.Sprintf("%s:%s", sm.getClientIP(r), securityCtx.User.ID)
		}
		return sm.getClientIP(r)
	default:
		return sm.getClientIP(r)
	}
}

// isOriginAllowed checks if an origin is allowed
func (sm *SecurityMiddleware) isOriginAllowed(origin string) bool {
	if len(sm.config.AllowedOrigins) == 0 {
		return true
	}

	for _, allowedOrigin := range sm.config.AllowedOrigins {
		if allowedOrigin == "*" || allowedOrigin == origin {
			return true
		}
	}

	return false
}

// writeErrorResponse writes an error response
func (sm *SecurityMiddleware) writeErrorResponse(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	response := map[string]interface{}{
		"error":   http.StatusText(status),
		"message": message,
		"status":  status,
	}

	json.NewEncoder(w).Encode(response)
}

// GetSecurityContext gets the security context from the request context
func GetSecurityContext(ctx context.Context) (*SecurityContext, bool) {
	securityCtx, ok := ctx.Value("security").(*SecurityContext)
	return securityCtx, ok
}

// RequireAuth is a helper function to check if authentication is required
func RequireAuth(ctx context.Context) (*SecurityContext, error) {
	securityCtx, ok := GetSecurityContext(ctx)
	if !ok {
		return nil, fmt.Errorf("authentication required")
	}
	return securityCtx, nil
}

// RequirePermission is a helper function to check if a user has a specific permission
func RequirePermission(ctx context.Context, permission string) error {
	securityCtx, err := RequireAuth(ctx)
	if err != nil {
		return err
	}

	for _, perm := range securityCtx.User.Permissions {
		if perm == permission || perm == "*" {
			return nil
		}
	}

	return fmt.Errorf("permission %s required", permission)
}

// RequireRole is a helper function to check if a user has a specific role
func RequireRole(ctx context.Context, role string) error {
	securityCtx, err := RequireAuth(ctx)
	if err != nil {
		return err
	}

	for _, userRole := range securityCtx.User.Roles {
		if userRole == role {
			return nil
		}
	}

	return fmt.Errorf("role %s required", role)
}
