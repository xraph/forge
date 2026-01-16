package orpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/xraph/forge"
	"github.com/xraph/vessel"
)

// Extension implements forge.Extension for oRPC (JSON-RPC 2.0 / OpenRPC) functionality.
// The extension is now a lightweight facade that loads config and registers services.
type Extension struct {
	*forge.BaseExtension

	config Config
	// No longer storing server - Vessel manages it
}

// NewExtension creates a new oRPC extension with functional options.
// Config is loaded from ConfigManager by default, with options providing overrides.
//
// Example:
//
//	// Load from ConfigManager (tries "extensions.orpc", then "orpc")
//	orpc.NewExtension()
//
//	// Override specific fields
//	orpc.NewExtension(
//	    orpc.WithEnabled(true),
//	    orpc.WithEndpoint("/rpc"),
//	    orpc.WithAutoExposeRoutes(true),
//	)
//
//	// Require config from ConfigManager
//	orpc.NewExtension(orpc.WithRequireConfig(true))
func NewExtension(opts ...ConfigOption) forge.Extension {
	config := DefaultConfig()
	for _, opt := range opts {
		opt(&config)
	}

	base := forge.NewBaseExtension("orpc", "2.0.0", "JSON-RPC 2.0 / OpenRPC Server")

	return &Extension{
		BaseExtension: base,
		config:        config,
	}
}

// NewExtensionWithConfig creates a new oRPC extension with a complete config.
// This is for backward compatibility or when config is fully known at initialization.
func NewExtensionWithConfig(config Config) forge.Extension {
	return NewExtension(WithConfig(config))
}

// Register registers the oRPC extension with the app.
// This method loads configuration and registers service constructors.
func (e *Extension) Register(app forge.App) error {
	// Call base registration (sets logger, metrics)
	if err := e.BaseExtension.Register(app); err != nil {
		return err
	}

	// Load config from ConfigManager with dual-key support
	programmaticConfig := e.config

	finalConfig := DefaultConfig()
	if err := e.LoadConfig("orpc", &finalConfig, programmaticConfig, DefaultConfig(), programmaticConfig.RequireConfig); err != nil {
		if programmaticConfig.RequireConfig {
			return fmt.Errorf("orpc: failed to load required config: %w", err)
		}

		e.Logger().Warn("orpc: using default/programmatic config",
			forge.F("error", err.Error()),
		)
	}

	e.config = finalConfig

	// Validate config
	if err := e.config.Validate(); err != nil {
		return fmt.Errorf("orpc config validation failed: %w", err)
	}

	if !e.config.Enabled {
		e.Logger().Info("orpc extension disabled")
		return nil
	}

	// Set server name/version from app if not configured
	if finalConfig.ServerName == "" {
		finalConfig.ServerName = app.Name()
	}

	if finalConfig.ServerVersion == "" {
		finalConfig.ServerVersion = app.Version()
	}

	// Register ORPCService constructor with Vessel using vessel.WithAliases for backward compatibility
	if err := e.RegisterConstructor(func(logger forge.Logger, metrics forge.Metrics) (*ORPCService, error) {
		return NewORPCService(finalConfig, logger, metrics)
	}, vessel.WithAliases(ServiceKey)); err != nil {
		return fmt.Errorf("failed to register orpc service: %w", err)
	}

	e.Logger().Info("orpc extension registered",
		forge.F("endpoint", finalConfig.Endpoint),
		forge.F("auto_expose", finalConfig.AutoExposeRoutes),
	)

	return nil
}

// Start starts the oRPC extension and registers routes.
func (e *Extension) Start(ctx context.Context) error {
	if !e.config.Enabled {
		return nil
	}

	e.Logger().Info("starting orpc extension")

	// Resolve ORPC service from DI
	orpcService, err := forge.InjectType[*ORPCService](e.App().Container())
	if err != nil {
		return fmt.Errorf("failed to resolve orpc service: %w", err)
	}

	// Set router reference for executing routes
	orpcService.Server().SetRouter(e.App().Router())

	// Register oRPC endpoints
	e.registerEndpoints(orpcService)

	// Auto-expose routes as JSON-RPC methods
	if e.config.AutoExposeRoutes {
		e.exposeRoutesAsMethods(orpcService)
	}

	e.MarkStarted()
	e.Logger().Info("orpc extension started",
		forge.F("methods", len(orpcService.Server().ListMethods())),
	)

	return nil
}

// Stop marks the extension as stopped.
// The actual service is stopped by Vessel calling ORPCService.Stop().
func (e *Extension) Stop(ctx context.Context) error {
	if !e.config.Enabled {
		return nil
	}

	e.MarkStopped()
	return nil
}

// Health checks the extension health.
// Service health is managed by Vessel through ORPCService.Health().
func (e *Extension) Health(ctx context.Context) error {
	if !e.config.Enabled {
		return nil
	}

	// Health is now managed by Vessel through ORPCService.Health()
	return nil
}

// registerEndpoints registers oRPC HTTP endpoints.
func (e *Extension) registerEndpoints(orpcService *ORPCService) {
	router := e.App().Router()

	// Main JSON-RPC endpoint (handles both single & batch)
	router.POST(e.config.Endpoint, e.handleJSONRPC,
		forge.WithName("orpc-endpoint"),
		forge.WithTags("api", "rpc"),
		forge.WithSummary("JSON-RPC 2.0 endpoint"),
		forge.WithDescription("Handles JSON-RPC 2.0 requests (both single and batch)"),
	)

	// OpenRPC schema endpoint
	if e.config.EnableOpenRPC {
		router.GET(e.config.OpenRPCEndpoint, e.handleOpenRPCSchema,
			forge.WithName("orpc-schema"),
			forge.WithTags("api", "rpc", "schema"),
			forge.WithSummary("OpenRPC schema document"),
			forge.WithDescription("Returns the OpenRPC schema for available RPC methods"),
		)
	}

	// Method discovery endpoint (optional)
	if e.config.EnableDiscovery {
		router.GET(e.config.Endpoint+"/methods", e.handleListMethods,
			forge.WithName("orpc-methods"),
			forge.WithTags("api", "rpc"),
			forge.WithSummary("List available RPC methods"),
		)
	}

	e.Logger().Debug("orpc: endpoints registered",
		forge.F("endpoint", e.config.Endpoint),
		forge.F("openrpc", e.config.EnableOpenRPC),
		forge.F("discovery", e.config.EnableDiscovery),
	)
}

// exposeRoutesAsMethods automatically exposes Forge routes as JSON-RPC methods.
func (e *Extension) exposeRoutesAsMethods(orpcService *ORPCService) {
	routes := e.App().Router().Routes()

	e.Logger().Debug("orpc: exposing routes",
		forge.F("total_routes", len(routes)),
	)

	skipped := 0
	excluded := 0
	exposed := 0

	for _, route := range routes {
		e.Logger().Debug("orpc: processing route",
			forge.F("method", route.Method),
			forge.F("path", route.Path),
			forge.F("name", route.Name),
		)

		// Skip oRPC endpoints themselves
		if e.shouldSkipRoute(route) {
			e.Logger().Debug("orpc: skipping oRPC endpoint",
				forge.F("path", route.Path),
			)

			skipped++

			continue
		}

		// Check if route should be exposed
		if !e.config.ShouldExpose(route.Path) {
			e.Logger().Debug("orpc: route excluded by config",
				forge.F("path", route.Path),
			)

			excluded++

			continue
		}

		// Generate JSON-RPC method from route
		method, err := orpcService.Server().GenerateMethodFromRoute(route)
		if err != nil {
			e.Logger().Warn("orpc: failed to generate method from route",
				forge.F("path", route.Path),
				forge.F("error", err),
			)

			continue
		}

		// Register method
		if err := orpcService.Server().RegisterMethod(method); err != nil {
			e.Logger().Warn("orpc: failed to register method",
				forge.F("method", method.Name),
				forge.F("error", err),
			)

			continue
		}

		e.Logger().Debug("orpc: method registered",
			forge.F("method_name", method.Name),
			forge.F("route_path", route.Path),
		)

		exposed++
	}

	e.Logger().Info("orpc: routes exposed as methods",
		forge.F("total_routes", len(routes)),
		forge.F("skipped", skipped),
		forge.F("excluded", excluded),
		forge.F("exposed", exposed),
		forge.F("methods", len(orpcService.Server().ListMethods())),
	)
}

// shouldSkipRoute checks if a route should be skipped from auto-exposure.
func (e *Extension) shouldSkipRoute(route forge.RouteInfo) bool {
	// Skip oRPC endpoints
	if len(route.Path) >= len(e.config.Endpoint) &&
		route.Path[:len(e.config.Endpoint)] == e.config.Endpoint {
		return true
	}

	return false
}

// Handler implementations

func (e *Extension) handleJSONRPC(ctx forge.Context) error {
	// Check request size
	if ctx.Request().ContentLength > e.config.MaxRequestSize {
		response := NewErrorResponse(nil, ErrServerError, "Request too large")

		return ctx.JSON(http.StatusOK, response) // JSON-RPC always returns 200
	}

	// Read body
	body, err := io.ReadAll(io.LimitReader(ctx.Request().Body, e.config.MaxRequestSize))
	if err != nil {
		response := NewErrorResponse(nil, ErrParseError, "Failed to read request body")

		return ctx.JSON(http.StatusOK, response)
	}

	// Detect if batch or single request
	var req any
	if err := json.Unmarshal(body, &req); err != nil {
		response := NewErrorResponse(nil, ErrParseError, "Invalid JSON")

		return ctx.JSON(http.StatusOK, response)
	}

	// Check if batch request (array)
	switch req.(type) {
	case []any:
		// Batch request
		if !e.config.EnableBatch {
			response := NewErrorResponse(nil, ErrServerError, "Batch requests are disabled")

			return ctx.JSON(http.StatusOK, response)
		}

		requests, err := parseBatchRequest(body)
		if err != nil {
			response := NewErrorResponse(nil, ErrInvalidRequest, err.Error())

			return ctx.JSON(http.StatusOK, response)
		}

		// Resolve service
		orpcService, err := forge.InjectType[*ORPCService](e.App().Container())
		if err != nil {
			response := NewErrorResponse(nil, ErrServerError, "Service not available")
			return ctx.JSON(http.StatusInternalServerError, response)
		}

		responses := orpcService.Server().HandleBatch(ctx.Context(), requests)

		return ctx.JSON(http.StatusOK, responses)

	case map[string]any:
		// Single request
		var request Request
		if err := json.Unmarshal(body, &request); err != nil {
			response := NewErrorResponse(nil, ErrInvalidRequest, "Invalid request format")

			return ctx.JSON(http.StatusOK, response)
		}

		// Resolve service
		orpcService, err := forge.InjectType[*ORPCService](e.App().Container())
		if err != nil {
			response := NewErrorResponse(nil, ErrServerError, "Service not available")
			return ctx.JSON(http.StatusInternalServerError, response)
		}

		response := orpcService.Server().HandleRequest(ctx.Context(), &request)

		return ctx.JSON(http.StatusOK, response)

	default:
		response := NewErrorResponse(nil, ErrInvalidRequest, "Invalid request format")

		return ctx.JSON(http.StatusOK, response)
	}
}

func (e *Extension) handleOpenRPCSchema(ctx forge.Context) error {
	orpcService, err := forge.InjectType[*ORPCService](e.App().Container())
	if err != nil {
		return ctx.JSON(http.StatusInternalServerError, map[string]string{"error": "Service not available"})
	}

	doc := orpcService.Server().OpenRPCDocument()

	return ctx.JSON(http.StatusOK, doc)
}

func (e *Extension) handleListMethods(ctx forge.Context) error {
	orpcService, err := forge.InjectType[*ORPCService](e.App().Container())
	if err != nil {
		return ctx.JSON(http.StatusInternalServerError, map[string]string{"error": "Service not available"})
	}

	methods := orpcService.Server().ListMethods()

	// Format method list
	methodList := make([]map[string]any, 0, len(methods))
	for _, method := range methods {
		methodList = append(methodList, map[string]any{
			"name":        method.Name,
			"description": method.Description,
			"tags":        method.Tags,
			"deprecated":  method.Deprecated,
		})
	}

	return ctx.JSON(http.StatusOK, map[string]any{
		"jsonrpc": "2.0",
		"methods": methodList,
		"total":   len(methods),
	})
}

// parseBatchRequest parses a batch of JSON-RPC requests.
func parseBatchRequest(body []byte) ([]*Request, error) {
	var rawRequests []json.RawMessage
	if err := json.Unmarshal(body, &rawRequests); err != nil {
		return nil, fmt.Errorf("invalid batch format: %w", err)
	}

	if len(rawRequests) == 0 {
		return nil, errors.New("empty batch")
	}

	requests := make([]*Request, len(rawRequests))
	for i, raw := range rawRequests {
		var req Request
		if err := json.Unmarshal(raw, &req); err != nil {
			return nil, fmt.Errorf("invalid request at index %d: %w", i, err)
		}

		requests[i] = &req
	}

	return requests, nil
}

// Server returns the oRPC server instance by resolving from DI.
func (e *Extension) Server() ORPC {
	svc, _ := forge.InjectType[*ORPCService](e.App().Container())
	if svc != nil {
		return svc
	}
	return nil
}
