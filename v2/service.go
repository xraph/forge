package forge

import (
	"github.com/xraph/forge/v2/internal/shared"
)

// Service is the standard interface for managed services
// Container auto-detects and calls these methods
type Service = shared.Service

// HealthChecker is optional for services that provide health checks
type HealthChecker = shared.HealthChecker

// Configurable is optional for services that need configuration
type Configurable = shared.Configurable

// Disposable is optional for scoped services that need cleanup
type Disposable = shared.Disposable
