package common

import "time"

// =============================================================================
// APPLICATION INTERFACE
// =============================================================================

// Application represents the main application interface for plugins
type Application interface {
	// Container returns the dependency injection container
	Container() Container

	// Router returns the HTTP router
	Router() Router

	// Config returns the configuration manager
	Config() ConfigManager

	// AddController registers a controller with the application
	AddController(controller Controller) error

	// AddService registers a service with the application
	AddService(service Service) error
}

// =============================================================================
// APPLICATION STATUS AND INFO
// =============================================================================

// ApplicationStatus represents the application status
type ApplicationStatus string

const (
	ApplicationStatusNotStarted ApplicationStatus = "not_started"
	ApplicationStatusStarting   ApplicationStatus = "starting"
	ApplicationStatusRunning    ApplicationStatus = "running"
	ApplicationStatusStopping   ApplicationStatus = "stopping"
	ApplicationStatusStopped    ApplicationStatus = "stopped"
	ApplicationStatusError      ApplicationStatus = "error"
)

// ApplicationInfo contains information about the application
type ApplicationInfo struct {
	Name              string            `json:"name"`
	Version           string            `json:"version"`
	Description       string            `json:"description"`
	Status            ApplicationStatus `json:"status"`
	StartTime         time.Time         `json:"start_time"`
	Uptime            time.Duration     `json:"uptime"`
	Services          int               `json:"services"`
	Controllers       int               `json:"controllers"`
	Plugins           int               `json:"plugins"`
	Routes            int               `json:"routes"`
	Middleware        int               `json:"middleware"`
	WebSocketHandlers int               `json:"websocket_handlers"`
	SSEHandlers       int               `json:"sse_handlers"`
	StreamingEnabled  bool              `json:"streaming_enabled"`
	ActiveConnections int64             `json:"active_connections"`
}
