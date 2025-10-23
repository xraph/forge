package streaming

import (
	"context"

	"github.com/xraph/forge/v0/pkg/common"
	streamincore "github.com/xraph/forge/v0/pkg/streaming/core"
)

// AuthenticationProvider defines the interface for streaming authentication
type AuthenticationProvider = streamincore.AuthenticationProvider

// AuthenticatedUser represents an authenticated user
type AuthenticatedUser = streamincore.AuthenticatedUser

// AuthenticationConfig contains authentication configuration
type AuthenticationConfig = streamincore.AuthenticationConfig

// DefaultAuthenticationConfig returns default authentication configuration
func DefaultAuthenticationConfig() AuthenticationConfig {
	return streamincore.DefaultAuthenticationConfig()
}

// AuthenticationManager manages authentication for streaming connections
type AuthenticationManager = streamincore.AuthenticationManager

// NewAuthenticationManager creates a new authentication manager
func NewAuthenticationManager(
	provider AuthenticationProvider,
	config AuthenticationConfig,
	logger common.Logger,
	metrics common.Metrics,
) *AuthenticationManager {
	return streamincore.NewAuthenticationManager(provider, config, logger, metrics)
}

// GetAuthenticatedUser retrieves the authenticated user from context
func GetAuthenticatedUser(ctx context.Context) (*AuthenticatedUser, bool) {
	return streamincore.GetAuthenticatedUser(ctx)
}

// SimpleAuthenticationProvider provides a simple in-memory authentication provider
type SimpleAuthenticationProvider = streamincore.SimpleAuthenticationProvider

// NewSimpleAuthenticationProvider creates a new simple authentication provider
func NewSimpleAuthenticationProvider(logger common.Logger) *SimpleAuthenticationProvider {
	return streamincore.NewSimpleAuthenticationProvider(logger)
}

// Authentication actions
const (
	ActionConnect    = streamincore.ActionConnect
	ActionDisconnect = streamincore.ActionDisconnect
	ActionSend       = streamincore.ActionSend
	ActionReceive    = streamincore.ActionReceive
	ActionJoinRoom   = streamincore.ActionJoinRoom
	ActionLeaveRoom  = streamincore.ActionLeaveRoom
	ActionCreateRoom = streamincore.ActionCreateRoom
	ActionDeleteRoom = streamincore.ActionDeleteRoom
	ActionModerate   = streamincore.ActionModerate
	ActionAdmin      = streamincore.ActionAdmin
)

// Common permissions
const (
	PermissionConnect    = streamincore.PermissionConnect
	PermissionSend       = streamincore.PermissionSend
	PermissionReceive    = streamincore.PermissionReceive
	PermissionJoinRoom   = streamincore.PermissionJoinRoom
	PermissionLeaveRoom  = streamincore.PermissionLeaveRoom
	PermissionCreateRoom = streamincore.PermissionCreateRoom
	PermissionDeleteRoom = streamincore.PermissionDeleteRoom
	PermissionModerate   = streamincore.PermissionModerate
	PermissionAdmin      = streamincore.PermissionAdmin
	PermissionAll        = streamincore.PermissionAll
)
