package config

import (
	"github.com/xraph/forge/v2/internal/config/core"
	"github.com/xraph/forge/v2/internal/errors"
	"github.com/xraph/forge/v2/internal/shared"
)

// ConfigManager interface (matches v2.ConfigManagerInterface)
type ConfigManager = core.ConfigManager

// =============================================================================
// ERROR TYPES - Re-exported from internal/errors
// =============================================================================

// Re-export error types and constructors for backward compatibility
type ForgeError = errors.ForgeError

var (
	ErrConfigError     = errors.ErrConfigError
	ErrLifecycleError  = errors.ErrLifecycleError
	ErrValidationError = errors.ErrValidationError
)

// =============================================================================
// CONSTANTS
// =============================================================================

// ConfigKey is the service key for configuration manager
const ConfigKey = shared.ConfigKey
