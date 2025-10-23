package router

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/logger"
	"github.com/xraph/forge/pkg/middleware"
)

// MiddlewareDefinitionAdapter adapts the old MiddlewareDefinition to the new Middleware interface
// This provides backward compatibility while using the robust middleware manager
type MiddlewareDefinitionAdapter struct {
	*middleware.BaseServiceMiddleware
	definition common.MiddlewareDefinition
	config     interface{}
}

func (mda *MiddlewareDefinitionAdapter) OnHealthCheck(ctx context.Context) error {
	return nil
}

// NewMiddlewareDefinitionAdapter creates a new adapter for backward compatibility
func NewMiddlewareDefinitionAdapter(definition common.MiddlewareDefinition) *MiddlewareDefinitionAdapter {
	return &MiddlewareDefinitionAdapter{
		BaseServiceMiddleware: middleware.NewBaseServiceMiddleware(
			definition.Name,
			definition.Priority,
			definition.Dependencies,
		),
		definition: definition,
		config:     definition.Config,
	}
}

// Handler returns the HTTP handler function
func (mda *MiddlewareDefinitionAdapter) Handler() func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			// Create a response wrapper to capture status codes
			wrapper := &responseWrapper{
				ResponseWriter: w,
				statusCode:     http.StatusOK,
			}

			// Update call count
			mda.UpdateStats(1, 0, 0, nil)

			// Handle panics
			defer func() {
				if recovered := recover(); recovered != nil {
					err := fmt.Errorf("panic in middleware %s: %v", mda.Name(), recovered)
					mda.UpdateStats(0, 1, time.Since(start), err)
					panic(recovered) // Re-panic to let recovery middleware handle it
				}
			}()

			// Call the original middleware handler
			originalHandler := mda.definition.Handler(next)
			originalHandler.ServeHTTP(wrapper, r)

			// Calculate latency and update stats
			latency := time.Since(start)

			// Check for errors based on status code
			if wrapper.statusCode >= 400 {
				err := fmt.Errorf("middleware %s returned error status: %d", mda.Name(), wrapper.statusCode)
				mda.UpdateStats(0, 1, latency, err)
			} else {
				mda.UpdateStats(0, 0, latency, nil)
			}
		})
	}
}

// Initialize initializes the adapter middleware
func (mda *MiddlewareDefinitionAdapter) Initialize(container common.Container) error {
	if err := mda.BaseServiceMiddleware.Initialize(container); err != nil {
		return err
	}

	// If the definition has config, try to load it from the config manager
	if configManager, err := container.Resolve((*common.ConfigManager)(nil)); err == nil {
		if cm, ok := configManager.(common.ConfigManager); ok {
			// Try to bind configuration if available
			configKey := fmt.Sprintf("middleware.%s", mda.Name())
			if err := cm.Bind(configKey, &mda.config); err != nil {
				// Config binding failed, but this is not critical
				if mda.Logger() != nil {
					mda.Logger().Debug("Failed to bind middleware config",
						logger.String("middleware", mda.Name()),
						logger.Error(err),
					)
				}
			}
		}
	}

	return nil
}

// OnStart starts the adapter middleware
func (mda *MiddlewareDefinitionAdapter) Start(ctx context.Context) error {
	if err := mda.BaseServiceMiddleware.OnStart(ctx); err != nil {
		return err
	}

	// Log configuration if available
	if mda.Logger() != nil && mda.config != nil {
		mda.Logger().Info("Middleware adapter started with config",
			logger.String("middleware", mda.Name()),
			logger.String("config", fmt.Sprintf("%+v", mda.config)),
		)
	}

	return nil
}

// GetConfig returns the middleware configuration
func (mda *MiddlewareDefinitionAdapter) GetConfig() interface{} {
	return mda.config
}

// SetConfig sets the middleware configuration
func (mda *MiddlewareDefinitionAdapter) SetConfig(config interface{}) {
	mda.config = config
}

// GetDefinition returns the original middleware definition
func (mda *MiddlewareDefinitionAdapter) GetDefinition() common.MiddlewareDefinition {
	return mda.definition
}

// responseWrapper wraps http.ResponseWriter to capture status codes
type responseWrapper struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWrapper) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *responseWrapper) Write(b []byte) (int, error) {
	return rw.ResponseWriter.Write(b)
}

// =============================================================================
// HELPER FUNCTIONS FOR MIDDLEWARE CONVERSION
// =============================================================================

// RegisterMiddlewareDefinitions registers multiple middleware definitions with the manager
func RegisterMiddlewareDefinitions(manager *middleware.Manager, definitions []common.MiddlewareDefinition) error {
	for _, def := range definitions {
		adapter := NewMiddlewareDefinitionAdapter(def)
		if err := manager.Register(adapter); err != nil {
			return fmt.Errorf("failed to register middleware %s: %w", def.Name, err)
		}
	}
	return nil
}

// =============================================================================
// BUILT-IN MIDDLEWARE ADAPTERS
// =============================================================================

// RecoveryMiddlewareDefinition creates a recovery middleware definition
func RecoveryMiddlewareDefinition() common.MiddlewareDefinition {
	return common.MiddlewareDefinition{
		Name:     "recovery",
		Priority: 1, // Highest priority to catch all panics
		Handler: func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				defer func() {
					if recovered := recover(); recovered != nil {
						w.WriteHeader(http.StatusInternalServerError)
						w.Write([]byte("Internal Server Error"))
					}
				}()
				next.ServeHTTP(w, r)
			})
		},
		Dependencies: []string{},
	}
}

// LoggingMiddlewareDefinition creates a logging middleware definition
func LoggingMiddlewareDefinition() common.MiddlewareDefinition {
	return common.MiddlewareDefinition{
		Name:     "logging",
		Priority: 10,
		Handler: func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				start := time.Now()

				wrapper := &responseWrapper{
					ResponseWriter: w,
					statusCode:     http.StatusOK,
				}

				next.ServeHTTP(wrapper, r)

				// Simple logging (in production, use proper logger)
				fmt.Printf("%s %s %d %v\n",
					r.Method,
					r.URL.Path,
					wrapper.statusCode,
					time.Since(start))
			})
		},
		Dependencies: []string{},
	}
}

// CORSMiddlewareDefinition creates a CORS middleware definition
func CORSMiddlewareDefinition(allowedOrigins []string) common.MiddlewareDefinition {
	return common.MiddlewareDefinition{
		Name:     "cors",
		Priority: 5,
		Handler: func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				origin := r.Header.Get("Origin")

				// Set CORS headers
				for _, allowedOrigin := range allowedOrigins {
					if allowedOrigin == "*" || allowedOrigin == origin {
						w.Header().Set("Access-Control-Allow-Origin", allowedOrigin)
						break
					}
				}

				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

				// Handle preflight requests
				if r.Method == "OPTIONS" {
					w.WriteHeader(http.StatusOK)
					return
				}

				next.ServeHTTP(w, r)
			})
		},
		Dependencies: []string{},
		Config: map[string]interface{}{
			"allowed_origins": allowedOrigins,
		},
	}
}

// RequestIDMiddlewareDefinition creates a request ID middleware definition
func RequestIDMiddlewareDefinition() common.MiddlewareDefinition {
	return common.MiddlewareDefinition{
		Name:     "request_id",
		Priority: 2,
		Handler: func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requestID := r.Header.Get("X-Request-ID")
				if requestID == "" {
					// Generate a simple request ID (in production, use proper UUID)
					requestID = fmt.Sprintf("req_%d", time.Now().UnixNano())
				}

				// Set request ID in response header
				w.Header().Set("X-Request-ID", requestID)

				// Add to context
				ctx := context.WithValue(r.Context(), "request_id", requestID)
				next.ServeHTTP(w, r.WithContext(ctx))
			})
		},
		Dependencies: []string{},
	}
}

// TimeoutMiddlewareDefinition creates a timeout middleware definition
func TimeoutMiddlewareDefinition(timeout time.Duration) common.MiddlewareDefinition {
	return common.MiddlewareDefinition{
		Name:     "timeout",
		Priority: 3,
		Handler: func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				ctx, cancel := context.WithTimeout(r.Context(), timeout)
				defer cancel()

				next.ServeHTTP(w, r.WithContext(ctx))
			})
		},
		Dependencies: []string{},
		Config: map[string]interface{}{
			"timeout": timeout,
		},
	}
}

// =============================================================================
// MIDDLEWARE REGISTRY FOR COMMON MIDDLEWARE
// =============================================================================

// MiddlewareRegistry provides easy access to common middleware definitions
type MiddlewareRegistry struct {
	registry map[string]func() common.MiddlewareDefinition
}

// NewMiddlewareRegistry creates a new middleware registry with built-in middleware
func NewMiddlewareRegistry() *MiddlewareRegistry {
	registry := &MiddlewareRegistry{
		registry: make(map[string]func() common.MiddlewareDefinition),
	}

	// Register built-in middleware
	registry.Register("recovery", func() common.MiddlewareDefinition {
		return RecoveryMiddlewareDefinition()
	})

	registry.Register("logging", func() common.MiddlewareDefinition {
		return LoggingMiddlewareDefinition()
	})

	registry.Register("request_id", func() common.MiddlewareDefinition {
		return RequestIDMiddlewareDefinition()
	})

	return registry
}

// Register registers a middleware factory function
func (mr *MiddlewareRegistry) Register(name string, factory func() common.MiddlewareDefinition) {
	mr.registry[name] = factory
}

// Get returns a middleware definition by name
func (mr *MiddlewareRegistry) Get(name string) (common.MiddlewareDefinition, error) {
	factory, exists := mr.registry[name]
	if !exists {
		return common.MiddlewareDefinition{}, fmt.Errorf("middleware '%s' not found in registry", name)
	}
	return factory(), nil
}

// GetAll returns all registered middleware definitions
func (mr *MiddlewareRegistry) GetAll() []common.MiddlewareDefinition {
	definitions := make([]common.MiddlewareDefinition, 0, len(mr.registry))
	for _, factory := range mr.registry {
		definitions = append(definitions, factory())
	}
	return definitions
}

// List returns the names of all registered middleware
func (mr *MiddlewareRegistry) List() []string {
	names := make([]string, 0, len(mr.registry))
	for name := range mr.registry {
		names = append(names, name)
	}
	return names
}

// CreateDefaultMiddlewareStack creates a default middleware stack for common use cases
func CreateDefaultMiddlewareStack() []common.MiddlewareDefinition {
	return []common.MiddlewareDefinition{
		RecoveryMiddlewareDefinition(),
		RequestIDMiddlewareDefinition(),
		TimeoutMiddlewareDefinition(30 * time.Second),
		CORSMiddlewareDefinition([]string{"*"}),
		LoggingMiddlewareDefinition(),
	}
}
