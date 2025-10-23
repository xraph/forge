package middleware

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/xraph/forge/v0/pkg/common"
	"github.com/xraph/forge/v0/pkg/logger"
	streaming "github.com/xraph/forge/v0/pkg/streaming/core"
)

// AuthMiddleware provides authentication for streaming connections
type AuthMiddleware struct {
	authManager *streaming.AuthenticationManager
	logger      common.Logger
	metrics     common.Metrics
	config      AuthMiddlewareConfig
}

// AuthMiddlewareConfig contains configuration for authentication middleware
type AuthMiddlewareConfig struct {
	Enabled              bool          `yaml:"enabled" default:"true"`
	RequireAuth          bool          `yaml:"require_auth" default:"false"`
	AllowAnonymous       bool          `yaml:"allow_anonymous" default:"true"`
	TokenValidationCache bool          `yaml:"token_validation_cache" default:"true"`
	MaxRetries           int           `yaml:"max_retries" default:"3"`
	RetryDelay           time.Duration `yaml:"retry_delay" default:"100ms"`
	FailureAction        string        `yaml:"failure_action" default:"reject"` // reject, allow, throttle
	RateLimitFailures    bool          `yaml:"rate_limit_failures" default:"true"`
	LogFailedAttempts    bool          `yaml:"log_failed_attempts" default:"true"`
	EnableAudit          bool          `yaml:"enable_audit" default:"false"`
	AuditLogLevel        string        `yaml:"audit_log_level" default:"info"`
}

// DefaultAuthMiddlewareConfig returns default authentication middleware configuration
func DefaultAuthMiddlewareConfig() AuthMiddlewareConfig {
	return AuthMiddlewareConfig{
		Enabled:              true,
		RequireAuth:          false,
		AllowAnonymous:       true,
		TokenValidationCache: true,
		MaxRetries:           3,
		RetryDelay:           100 * time.Millisecond,
		FailureAction:        "reject",
		RateLimitFailures:    true,
		LogFailedAttempts:    true,
		EnableAudit:          false,
		AuditLogLevel:        "info",
	}
}

// NewAuthMiddleware creates a new authentication middleware
func NewAuthMiddleware(
	authManager *streaming.AuthenticationManager,
	config AuthMiddlewareConfig,
	logger common.Logger,
	metrics common.Metrics,
) *AuthMiddleware {
	return &AuthMiddleware{
		authManager: authManager,
		logger:      logger,
		metrics:     metrics,
		config:      config,
	}
}

// Handler returns the HTTP middleware handler
func (am *AuthMiddleware) Handler() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()

			// Skip authentication if disabled
			if !am.config.Enabled {
				next.ServeHTTP(w, r)
				return
			}

			// Start authentication process
			startTime := time.Now()

			// Extract room ID from request
			roomID := am.extractRoomID(r)

			// Authenticate the connection
			user, err := am.authenticateWithRetry(ctx, r, roomID)
			if err != nil {
				am.handleAuthenticationFailure(w, r, err, startTime)
				return
			}

			// Add user to context
			ctx = context.WithValue(ctx, "authenticated_user", user)
			r = r.WithContext(ctx)

			// Log successful authentication
			am.logSuccessfulAuth(user, r, time.Since(startTime))

			// Record metrics
			am.recordAuthMetrics(user, true, time.Since(startTime))

			// Continue to next handler
			next.ServeHTTP(w, r)
		})
	}
}

// WebSocketUpgradeHandler provides authentication for WebSocket upgrades
func (am *AuthMiddleware) WebSocketUpgradeHandler(
	upgradeHandler func(w http.ResponseWriter, r *http.Request),
) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		// Skip authentication if disabled
		if !am.config.Enabled {
			upgradeHandler(w, r)
			return
		}

		startTime := time.Now()
		roomID := am.extractRoomID(r)

		// Authenticate the WebSocket connection
		user, err := am.authenticateWithRetry(ctx, r, roomID)
		if err != nil {
			am.handleWebSocketAuthFailure(w, r, err, startTime)
			return
		}

		// Add user to context
		ctx = context.WithValue(ctx, "authenticated_user", user)
		r = r.WithContext(ctx)

		// Log successful authentication
		am.logSuccessfulAuth(user, r, time.Since(startTime))

		// Record metrics
		am.recordAuthMetrics(user, true, time.Since(startTime))

		// Proceed with WebSocket upgrade
		upgradeHandler(w, r)
	}
}

// SSEHandler provides authentication for Server-Sent Events
func (am *AuthMiddleware) SSEHandler(
	sseHandler func(w http.ResponseWriter, r *http.Request),
) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		// Skip authentication if disabled
		if !am.config.Enabled {
			sseHandler(w, r)
			return
		}

		startTime := time.Now()
		roomID := am.extractRoomID(r)

		// Authenticate the SSE connection
		user, err := am.authenticateWithRetry(ctx, r, roomID)
		if err != nil {
			am.handleSSEAuthFailure(w, r, err, startTime)
			return
		}

		// Add user to context
		ctx = context.WithValue(ctx, "authenticated_user", user)
		r = r.WithContext(ctx)

		// Set SSE headers
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("Access-Control-Allow-Origin", "*")

		// Log successful authentication
		am.logSuccessfulAuth(user, r, time.Since(startTime))

		// Record metrics
		am.recordAuthMetrics(user, true, time.Since(startTime))

		// Proceed with SSE handler
		sseHandler(w, r)
	}
}

// ConnectionAuthMiddleware provides authentication for streaming connections
func (am *AuthMiddleware) ConnectionAuthMiddleware(
	conn streaming.Connection,
	message *streaming.Message,
) error {
	if !am.config.Enabled {
		return nil
	}

	// Get user from connection metadata
	user, exists := am.getUserFromConnection(conn)
	if !exists {
		return common.ErrValidationError("missing_user", fmt.Errorf("no authenticated user found in connection"))
	}

	// Validate user permissions for the action
	action := am.getActionFromMessage(message)
	resource := am.getResourceFromMessage(message)

	ctx := context.Background()
	if err := am.authManager.AuthorizeAction(ctx, user, action, resource); err != nil {
		if am.metrics != nil {
			am.metrics.Counter("streaming.auth.authorization_failures").Inc()
		}
		return common.ErrValidationError("authorization_failed", err)
	}

	return nil
}

// authenticateWithRetry attempts authentication with retry logic
func (am *AuthMiddleware) authenticateWithRetry(ctx context.Context, r *http.Request, roomID string) (*streaming.AuthenticatedUser, error) {
	var lastErr error

	for attempt := 0; attempt < am.config.MaxRetries; attempt++ {
		if attempt > 0 {
			time.Sleep(am.config.RetryDelay)
		}

		user, err := am.authManager.AuthenticateConnection(ctx, r, roomID)
		if err == nil {
			return user, nil
		}

		lastErr = err

		// Don't retry certain types of errors
		if am.shouldNotRetry(err) {
			break
		}

		if am.logger != nil {
			am.logger.Debug("authentication attempt failed, retrying",
				logger.Int("attempt", attempt+1),
				logger.Int("max_retries", am.config.MaxRetries),
				logger.Error(err),
			)
		}
	}

	return nil, lastErr
}

// shouldNotRetry determines if an error should not be retried
func (am *AuthMiddleware) shouldNotRetry(err error) bool {
	// Don't retry validation errors or permanent failures
	if forgeErr, ok := err.(*common.ForgeError); ok {
		switch forgeErr.Code {
		case common.ErrCodeValidationError:
			return true
		case "token_expired", "invalid_token", "user_disabled":
			return true
		}
	}
	return false
}

// extractRoomID extracts room ID from request
func (am *AuthMiddleware) extractRoomID(r *http.Request) string {
	// Try query parameter first
	if roomID := r.URL.Query().Get("room"); roomID != "" {
		return roomID
	}
	if roomID := r.URL.Query().Get("room_id"); roomID != "" {
		return roomID
	}

	// Try path parameter
	path := r.URL.Path
	if strings.Contains(path, "/room/") {
		parts := strings.Split(path, "/room/")
		if len(parts) > 1 {
			roomPart := strings.Split(parts[1], "/")
			if len(roomPart) > 0 {
				return roomPart[0]
			}
		}
	}

	// Try header
	if roomID := r.Header.Get("X-Room-ID"); roomID != "" {
		return roomID
	}

	return ""
}

// getUserFromConnection extracts user from connection metadata
func (am *AuthMiddleware) getUserFromConnection(conn streaming.Connection) (*streaming.AuthenticatedUser, bool) {
	metadata := conn.Metadata()
	if userInterface, exists := metadata["authenticated_user"]; exists {
		if user, ok := userInterface.(*streaming.AuthenticatedUser); ok {
			return user, true
		}
	}
	return nil, false
}

// getActionFromMessage determines the action from a message
func (am *AuthMiddleware) getActionFromMessage(message *streaming.Message) string {
	switch message.Type {
	case streaming.MessageTypeText:
		return streaming.ActionSend
	case streaming.MessageTypeEvent:
		return streaming.ActionSend
	case streaming.MessageTypePresence:
		return streaming.ActionSend
	case streaming.MessageTypeSystem:
		return streaming.ActionAdmin
	case streaming.MessageTypeBroadcast:
		return streaming.ActionSend
	case streaming.MessageTypePrivate:
		return streaming.ActionSend
	default:
		return streaming.ActionSend
	}
}

// getResourceFromMessage determines the resource from a message
func (am *AuthMiddleware) getResourceFromMessage(message *streaming.Message) string {
	if message.RoomID != "" {
		return "room:" + message.RoomID
	}
	return "global"
}

// handleAuthenticationFailure handles authentication failures
func (am *AuthMiddleware) handleAuthenticationFailure(w http.ResponseWriter, r *http.Request, err error, startTime time.Time) {
	duration := time.Since(startTime)

	// Log the failure
	if am.config.LogFailedAttempts && am.logger != nil {
		am.logger.Warn("streaming authentication failed",
			logger.String("remote_addr", r.RemoteAddr),
			logger.String("user_agent", r.UserAgent()),
			logger.String("path", r.URL.Path),
			logger.Duration("duration", duration),
			logger.Error(err),
		)
	}

	// Record metrics
	am.recordAuthMetrics(nil, false, duration)

	// Handle based on failure action
	switch am.config.FailureAction {
	case "reject":
		http.Error(w, "Authentication required", http.StatusUnauthorized)
	case "allow":
		// Continue but log the decision
		if am.logger != nil {
			am.logger.Info("allowing unauthenticated connection due to configuration",
				logger.String("remote_addr", r.RemoteAddr),
			)
		}
	case "throttle":
		// Add delay before rejecting
		time.Sleep(time.Second)
		http.Error(w, "Authentication required", http.StatusUnauthorized)
	default:
		http.Error(w, "Authentication required", http.StatusUnauthorized)
	}
}

// handleWebSocketAuthFailure handles WebSocket authentication failures
func (am *AuthMiddleware) handleWebSocketAuthFailure(w http.ResponseWriter, r *http.Request, err error, startTime time.Time) {
	duration := time.Since(startTime)

	// Log the failure
	if am.config.LogFailedAttempts && am.logger != nil {
		am.logger.Warn("WebSocket authentication failed",
			logger.String("remote_addr", r.RemoteAddr),
			logger.String("user_agent", r.UserAgent()),
			logger.Duration("duration", duration),
			logger.Error(err),
		)
	}

	// Record metrics
	am.recordAuthMetrics(nil, false, duration)

	// WebSocket specific failure response
	w.WriteHeader(http.StatusUnauthorized)
	w.Write([]byte("WebSocket authentication failed"))
}

// handleSSEAuthFailure handles SSE authentication failures
func (am *AuthMiddleware) handleSSEAuthFailure(w http.ResponseWriter, r *http.Request, err error, startTime time.Time) {
	duration := time.Since(startTime)

	// Log the failure
	if am.config.LogFailedAttempts && am.logger != nil {
		am.logger.Warn("SSE authentication failed",
			logger.String("remote_addr", r.RemoteAddr),
			logger.String("user_agent", r.UserAgent()),
			logger.Duration("duration", duration),
			logger.Error(err),
		)
	}

	// Record metrics
	am.recordAuthMetrics(nil, false, duration)

	// SSE specific failure response
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusUnauthorized)
	w.Write([]byte("event: error\ndata: Authentication failed\n\n"))
}

// logSuccessfulAuth logs successful authentication
func (am *AuthMiddleware) logSuccessfulAuth(user *streaming.AuthenticatedUser, r *http.Request, duration time.Duration) {
	if am.logger != nil {
		logLevel := am.config.AuditLogLevel
		fields := []logger.Field{
			logger.String("user_id", user.ID),
			logger.String("username", user.Username),
			logger.Bool("is_anonymous", user.IsAnonymous),
			logger.String("remote_addr", r.RemoteAddr),
			logger.String("user_agent", r.UserAgent()),
			logger.String("path", r.URL.Path),
			logger.Duration("duration", duration),
		}

		switch logLevel {
		case "debug":
			am.logger.Debug("streaming authentication successful", fields...)
		case "info":
			am.logger.Info("streaming authentication successful", fields...)
		case "warn":
			am.logger.Warn("streaming authentication successful", fields...)
		default:
			am.logger.Info("streaming authentication successful", fields...)
		}
	}
}

// recordAuthMetrics records authentication metrics
func (am *AuthMiddleware) recordAuthMetrics(user *streaming.AuthenticatedUser, success bool, duration time.Duration) {
	if am.metrics == nil {
		return
	}

	if success {
		am.metrics.Counter("streaming.auth.successes").Inc()
		if user != nil && user.IsAnonymous {
			am.metrics.Counter("streaming.auth.anonymous_successes").Inc()
		}
	} else {
		am.metrics.Counter("streaming.auth.failures").Inc()
	}

	am.metrics.Histogram("streaming.auth.duration").Observe(duration.Seconds())
}

// IsEnabled returns whether authentication is enabled
func (am *AuthMiddleware) IsEnabled() bool {
	return am.config.Enabled
}

// UpdateConfig updates the middleware configuration
func (am *AuthMiddleware) UpdateConfig(config AuthMiddlewareConfig) {
	am.config = config
}

// GetConfig returns the current configuration
func (am *AuthMiddleware) GetConfig() AuthMiddlewareConfig {
	return am.config
}

// GetStats returns middleware statistics
func (am *AuthMiddleware) GetStats() AuthMiddlewareStats {
	// This would typically gather stats from metrics
	return AuthMiddlewareStats{
		Enabled:        am.config.Enabled,
		RequireAuth:    am.config.RequireAuth,
		AllowAnonymous: am.config.AllowAnonymous,
		MaxRetries:     am.config.MaxRetries,
		FailureAction:  am.config.FailureAction,
	}
}

// AuthMiddlewareStats contains statistics about the auth middleware
type AuthMiddlewareStats struct {
	Enabled        bool   `json:"enabled"`
	RequireAuth    bool   `json:"require_auth"`
	AllowAnonymous bool   `json:"allow_anonymous"`
	MaxRetries     int    `json:"max_retries"`
	FailureAction  string `json:"failure_action"`
}

// AuthorizationMiddleware provides message-level authorization
type AuthorizationMiddleware struct {
	authManager *streaming.AuthenticationManager
	logger      common.Logger
	metrics     common.Metrics
	config      AuthorizationConfig
}

// AuthorizationConfig contains configuration for authorization middleware
type AuthorizationConfig struct {
	Enabled            bool     `yaml:"enabled" default:"true"`
	DefaultPermissions []string `yaml:"default_permissions"`
	AdminRoles         []string `yaml:"admin_roles"`
	ModeratorRoles     []string `yaml:"moderator_roles"`
	StrictMode         bool     `yaml:"strict_mode" default:"false"`
}

// NewAuthorizationMiddleware creates a new authorization middleware
func NewAuthorizationMiddleware(
	authManager *streaming.AuthenticationManager,
	config AuthorizationConfig,
	logger common.Logger,
	metrics common.Metrics,
) *AuthorizationMiddleware {
	return &AuthorizationMiddleware{
		authManager: authManager,
		logger:      logger,
		metrics:     metrics,
		config:      config,
	}
}

// AuthorizeMessage checks if a user is authorized to send a message
func (am *AuthorizationMiddleware) AuthorizeMessage(
	ctx context.Context,
	user *streaming.AuthenticatedUser,
	message *streaming.Message,
) error {
	if !am.config.Enabled {
		return nil
	}

	action := am.getActionFromMessage(message)
	resource := am.getResourceFromMessage(message)

	return am.authManager.AuthorizeAction(ctx, user, action, resource)
}

// getActionFromMessage determines the action from a message
func (am *AuthorizationMiddleware) getActionFromMessage(message *streaming.Message) string {
	switch message.Type {
	case streaming.MessageTypeSystem:
		return streaming.ActionAdmin
	case streaming.MessageTypeBroadcast:
		return streaming.ActionModerate
	default:
		return streaming.ActionSend
	}
}

// getResourceFromMessage determines the resource from a message
func (am *AuthorizationMiddleware) getResourceFromMessage(message *streaming.Message) string {
	if message.RoomID != "" {
		return "room:" + message.RoomID
	}
	return "global"
}
