package router

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"time"

	json "github.com/json-iterator/go"
	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/logger"
	"github.com/xraph/forge/pkg/middleware"
	"github.com/xraph/forge/pkg/streaming"
	"github.com/xraph/steel"
)

type Router = common.Router
type Controller = common.Controller

// RouteEntry represents a registered route
type RouteEntry struct {
	common.RouteDefinition
	RouteID  string `json:"route_id"`
	PluginID string `json:"plugin_id"`
}

// ForgeRouter implements the common.Router interface
type ForgeRouter struct {
	adapter           RouterAdapter
	container         common.Container
	logger            common.Logger
	metrics           common.Metrics
	config            common.ConfigManager
	healthChecker     common.HealthChecker
	errorHandler      common.ErrorHandler
	middlewareManager *middleware.Manager
	// pluginManager     plugins.PluginManager
	openAPIGenerator *OpenAPIGenerator

	routes          map[string]*RouteEntry       // routeID -> RouteEntry
	pluginRoutes    map[string][]string          // pluginID -> []routeIDs
	pathIndex       map[string]map[string]string // method -> path -> routeIDs
	nextRouteID     int64
	currentPluginID string            // Current plugin being configured
	routeToPlugin   map[string]string // routeID -> pluginID
	routeIDCounter  int64
	groupPrefix     string

	// Streaming integration
	streamingIntegration *StreamingIntegration

	// Handler collections
	serviceHandlers     map[string]*common.RouteHandlerInfo
	controllers         map[string]common.Controller
	opinionatedHandlers map[string]*OpinionatedHandlerInfo

	// Statistics
	routeStats map[string]*common.RouteStats
	mu         sync.RWMutex
	started    bool
}

// OpinionatedHandlerInfo contains information about opinionated handlers
type OpinionatedHandlerInfo struct {
	Method       string
	Path         string
	Handler      interface{}
	RequestType  reflect.Type
	ResponseType reflect.Type
	ServiceTypes []reflect.Type // Support multiple services
	ServiceNames []string       // Service names for logging/dependencies
	Pattern      string         // "opinionated" or "service-aware"
	Tags         map[string]string
	RegisteredAt time.Time
	CallCount    int64
	ErrorCount   int64
	LastCalled   time.Time
	LastError    error
}

type OpinionatedHandlerStats struct {
	Method       string    `json:"method"`
	Path         string    `json:"path"`
	Pattern      string    `json:"pattern"`       // "opinionated" or "service-aware"
	ServiceCount int       `json:"service_count"` // Number of services injected
	ServiceNames []string  `json:"service_names"` // Names of injected services
	RequestType  string    `json:"request_type"`
	ResponseType string    `json:"response_type"`
	CallCount    int64     `json:"call_count"`
	ErrorCount   int64     `json:"error_count"`
	LastCalled   time.Time `json:"last_called"`
	RegisteredAt time.Time `json:"registered_at"`
}

// ForgeRouterConfig contains configuration for the enhanced router
type ForgeRouterConfig struct {
	Container     common.Container
	Logger        common.Logger
	Metrics       common.Metrics
	Config        common.ConfigManager
	HealthChecker common.HealthChecker
	ErrorHandler  common.ErrorHandler
	RouterOptions steel.RouterOptions
	OpenAPI       *common.OpenAPIConfig
	AsyncAPI      *common.AsyncAPIConfig
	Adapter       RouterAdapter
}

// NewForgeRouter creates a new enhanced router implementing common.Router
func NewForgeRouter(config ForgeRouterConfig) common.Router {
	// baseRouter := steel.NewRouter()
	//
	// // Configure base router options
	// if config.RouterOptions.OpenAPITitle != "" {
	// 	baseRouter.OpenAPI().SetTitle(config.RouterOptions.OpenAPITitle)
	// }
	// if config.RouterOptions.OpenAPIVersion != "" {
	// 	baseRouter.OpenAPI().SetVersion(config.RouterOptions.OpenAPIVersion)
	// }
	// if config.RouterOptions.OpenAPIDescription != "" {
	// 	baseRouter.OpenAPI().SetDescription(config.RouterOptions.OpenAPIDescription)
	// }
	if config.Adapter == nil {
		config.Adapter = NewHttpRouterAdapterWithDefaults(config.Logger, "")
	}

	router := &ForgeRouter{
		container:           config.Container,
		logger:              config.Logger,
		metrics:             config.Metrics,
		adapter:             config.Adapter,
		config:              config.Config,
		healthChecker:       config.HealthChecker,
		errorHandler:        config.ErrorHandler,
		serviceHandlers:     make(map[string]*common.RouteHandlerInfo),
		controllers:         make(map[string]common.Controller),
		opinionatedHandlers: make(map[string]*OpinionatedHandlerInfo),
		routeStats:          make(map[string]*common.RouteStats),
		pluginRoutes:        make(map[string][]string),
		routeToPlugin:       make(map[string]string),
		pathIndex:           make(map[string]map[string]string),
		routes:              make(map[string]*RouteEntry),
	}

	// Initialize robust middleware manager
	router.middlewareManager = middleware.NewManager(
		config.Container,
		config.Logger,
		config.Metrics,
		config.Config,
	)

	// Initialize OpenAPI generator if config provided
	if config.OpenAPI != nil {
		// router.initOpenAPI(*config.OpenAPI)
	}

	// Initialize AsyncAPI generator if config provided
	if config.AsyncAPI != nil {
		router.EnableAsyncAPI(*config.AsyncAPI)
	}

	// Initialize ForgeContext middleware automatically
	if err := router.initializeMiddleware(); err != nil {
		// Handle error appropriately
	}

	return router
}

// SetCurrentPlugin sets the current plugin context for route registration
func (r *ForgeRouter) SetCurrentPlugin(pluginID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.currentPluginID = pluginID
}

// ClearCurrentPlugin clears the plugin context
func (r *ForgeRouter) ClearCurrentPlugin() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.currentPluginID = ""
}

// MiddlewareManager clears the plugin context
func (r *ForgeRouter) MiddlewareManager() *middleware.Manager {
	return r.middlewareManager
}

// =============================================================================
// SERVICE-AWARE ROUTE REGISTRATION
// =============================================================================

func (r *ForgeRouter) GET(path string, handler interface{}, options ...common.HandlerOption) error {
	if r.currentPluginID != "" {
		return r.registerPluginRoutesHandler("GET", path, handler, options...)
	}
	return r.registerServiceHandler("GET", path, handler, options...)
}

func (r *ForgeRouter) POST(path string, handler interface{}, options ...common.HandlerOption) error {
	if r.currentPluginID != "" {
		return r.registerPluginRoutesHandler("POST", path, handler, options...)
	}
	return r.registerServiceHandler("POST", path, handler, options...)
}

func (r *ForgeRouter) PUT(path string, handler interface{}, options ...common.HandlerOption) error {
	if r.currentPluginID != "" {
		return r.registerPluginRoutesHandler("PUT", path, handler, options...)
	}
	return r.registerServiceHandler("PUT", path, handler, options...)
}

func (r *ForgeRouter) DELETE(path string, handler interface{}, options ...common.HandlerOption) error {
	if r.currentPluginID != "" {
		return r.registerPluginRoutesHandler("DELETE", path, handler, options...)
	}
	return r.registerServiceHandler("DELETE", path, handler, options...)
}

func (r *ForgeRouter) PATCH(path string, handler interface{}, options ...common.HandlerOption) error {
	if r.currentPluginID != "" {
		return r.registerPluginRoutesHandler("PATCH", path, handler, options...)
	}
	return r.registerServiceHandler("PATCH", path, handler, options...)
}

func (r *ForgeRouter) OPTIONS(path string, handler interface{}, options ...common.HandlerOption) error {
	if r.currentPluginID != "" {
		return r.registerPluginRoutesHandler("OPTIONS", path, handler, options...)
	}
	return r.registerServiceHandler("OPTIONS", path, handler, options...)
}

func (r *ForgeRouter) HEAD(path string, handler interface{}, options ...common.HandlerOption) error {
	if r.currentPluginID != "" {
		return r.registerPluginRoutesHandler("HEAD", path, handler, options...)
	}
	return r.registerServiceHandler("HEAD", path, handler, options...)
}

// =============================================================================
// CONTROLLER REGISTRATION
// =============================================================================

func (r *ForgeRouter) RegisterController(controller Controller) error {
	r.mu.Lock()

	if r.started {
		r.mu.Unlock()
		return common.ErrLifecycleError("register_controller", fmt.Errorf("cannot register controller after router has started"))
	}

	controllerName := controller.Name()
	if _, exists := r.controllers[controllerName]; exists {
		r.mu.Unlock()
		return common.ErrServiceAlreadyExists(controllerName)
	}

	// Initialize controller if not already done
	if err := controller.Initialize(r.container); err != nil {
		r.mu.Unlock()
		return common.ErrContainerError("initialize_controller", err)
	}

	// Create a sub-router with controller prefix and middleware
	prefix := controller.Prefix()
	controllerRouter := r.Group(prefix)

	// Add controller middleware to the sub-router
	for _, mw := range controller.Middleware() {
		if err := controllerRouter.Use(mw); err != nil {
			r.mu.Unlock()
			return fmt.Errorf("failed to register controller middleware: %w", err)
		}
	}

	// Release the lock before calling ConfigureRoutes to avoid deadlock
	r.mu.Unlock()

	// Let the controller configure its own routes (without holding the main lock)
	if err := controller.ConfigureRoutes(controllerRouter); err != nil {
		return fmt.Errorf("failed to configure controller routes: %w", err)
	}

	// Re-acquire lock to update internal state
	r.mu.Lock()
	defer r.mu.Unlock()

	// Double-check controller wasn't registered while we didn't hold the lock
	if _, exists := r.controllers[controllerName]; exists {
		return common.ErrServiceAlreadyExists(controllerName)
	}

	r.controllers[controllerName] = controller

	if r.logger != nil {
		r.logger.Info("controller registered",
			logger.String("controller", controllerName),
			logger.String("prefix", prefix),
			logger.Int("middleware", len(controller.Middleware())),
		)
	}

	if r.metrics != nil {
		r.metrics.Counter("forge.router.controllers_registered").Inc()
	}

	return nil
}

func (r *ForgeRouter) UnregisterController(controllerName string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.started {
		return common.ErrLifecycleError("unregister_controller", fmt.Errorf("cannot unregister controller after router has started"))
	}

	if _, exists := r.controllers[controllerName]; !exists {
		return common.ErrServiceNotFound(controllerName)
	}

	delete(r.controllers, controllerName)

	if r.logger != nil {
		r.logger.Info("controller unregistered", logger.String("controller", controllerName))
	}

	return nil
}

// =============================================================================
// OPINIONATED HANDLER REGISTRATION
// =============================================================================

func (r *ForgeRouter) combinePathForOpenAPI(prefix, path string) string {
	if prefix == "" || prefix == "/" {
		return path
	}

	// Ensure prefix doesn't end with slash
	prefix = strings.TrimSuffix(prefix, "/")

	if path == "" || path == "/" {
		return prefix
	}

	// Ensure path starts with slash
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}

	return prefix + path
}

func (r *ForgeRouter) RegisterOpinionatedHandler(method, path string, handler interface{}, options ...common.HandlerOption) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.started {
		return common.ErrLifecycleError("register_opinionated_handler", fmt.Errorf("cannot register handler after router has started"))
	}

	if r.logger != nil {
		r.logger.Info("Registering opinionated handler",
			logger.String("method", method),
			logger.String("path", path),
		)
	}

	// Validate handler signature
	handlerType := reflect.TypeOf(handler)
	if handlerType.Kind() != reflect.Func {
		return common.ErrInvalidConfig("handler", fmt.Errorf("handler must be a function"))
	}

	// Get input/output parameter counts
	numIn := handlerType.NumIn()
	numOut := handlerType.NumOut()

	// Must have at least 2 inputs (ctx, request) and exactly 2 outputs (response, error)
	if numIn < 2 {
		return common.ErrInvalidConfig("handler", fmt.Errorf("handler must have at least 2 parameters: (ctx, request) or (ctx, ...services, request)"))
	}

	if numOut != 2 {
		return common.ErrInvalidConfig("handler", fmt.Errorf("handler must return (*ResponseType, error)"))
	}

	// Validate first parameter (context)
	contextType := handlerType.In(0)
	expectedContextType := reflect.TypeOf((*common.Context)(nil)).Elem()
	if contextType != expectedContextType {
		return common.ErrInvalidConfig("handler", fmt.Errorf("first parameter must be core.Context"))
	}

	// Validate last output parameter (error)
	errorType := handlerType.Out(1)
	expectedErrorType := reflect.TypeOf((*error)(nil)).Elem()
	if errorType != expectedErrorType {
		return common.ErrInvalidConfig("handler", fmt.Errorf("second return value must be error"))
	}

	// Extract types based on flexible pattern
	requestType := handlerType.In(numIn - 1) // Last input parameter is always request
	responseType := handlerType.Out(0)       // First output is response

	// Extract service types (all parameters between context and request)
	var serviceTypes []reflect.Type
	var serviceNames []string
	var handlerPattern string

	if numIn == 2 {
		// Pure opinionated: func(ctx, request)
		handlerPattern = "opinionated"
	} else {
		// Service-aware: func(ctx, service1, service2, ..., request)
		handlerPattern = "service-aware"
		serviceTypes = make([]reflect.Type, numIn-2)
		serviceNames = make([]string, numIn-2)

		for i := 1; i < numIn-1; i++ { // Skip ctx (index 0) and request (last index)
			serviceType := handlerType.In(i)
			serviceTypes[i-1] = serviceType

			serviceName := serviceType.Name()
			if serviceName == "" {
				serviceName = serviceType.String()
			}
			serviceNames[i-1] = serviceName
		}
	}

	// Remove pointer from response type for reflection
	if responseType.Kind() == reflect.Ptr {
		responseType = responseType.Elem()
	}

	openAPIPath := path
	if r.groupPrefix != "" {
		openAPIPath = r.combinePathForOpenAPI(r.groupPrefix, path)
	}

	// Generate OpenAPI documentation BEFORE creating wrappers
	if r.openAPIGenerator != nil {
		if r.logger != nil {
			r.logger.Debug("Attempting to add OpenAPI operation",
				logger.String("method", method),
				logger.String("path", openAPIPath),
				logger.Bool("auto_update", r.openAPIGenerator.autoUpdate),
			)
		}

		// Always try to add the operation (don't check autoUpdate here)
		if err := r.openAPIGenerator.AddOperation(method, openAPIPath, handler, options...); err != nil {
			if r.logger != nil {
				r.logger.Warn("Failed to generate OpenAPI documentation for opinionated handler",
					logger.String("method", method),
					logger.String("path", path),
					logger.Error(err),
				)
			}
			// Don't fail the registration, just log the warning
		} else {
			if r.logger != nil {
				r.logger.Info("OpenAPI operation added successfully",
					logger.String("method", method),
					logger.String("path", path),
				)
			}
		}
	} else {
		if r.logger != nil {
			r.logger.Warn("OpenAPI generator not initialized",
				logger.String("method", method),
				logger.String("path", path),
			)
		}
	}

	// Create handler info
	info := &OpinionatedHandlerInfo{
		Method:       method,
		Path:         path,
		Handler:      handler,
		RequestType:  requestType,
		ResponseType: responseType,
		ServiceTypes: serviceTypes,
		ServiceNames: serviceNames,
		Pattern:      handlerPattern,
		Tags:         make(map[string]string),
		RegisteredAt: time.Now(),
	}

	// Apply options
	handlerInfo := &common.RouteHandlerInfo{
		Method:       method,
		Path:         path,
		Tags:         make(map[string]string),
		Opinionated:  true,
		RequestType:  requestType,
		ResponseType: responseType,
		Dependencies: serviceNames,
		Middleware:   make([]any, 0),
	}

	for _, option := range options {
		option(handlerInfo)
	}

	// Copy tags from handler info
	for k, v := range handlerInfo.Tags {
		info.Tags[k] = v
	}

	var routeSpecificMiddleware []common.Middleware
	if len(handlerInfo.Middleware) > 0 {
		routeSpecificMiddleware = r.processRouteMiddleware(handlerInfo.Middleware)
	}

	// Process middleware from options using the robust middleware manager
	for _, mw := range handlerInfo.Middleware {
		if err := r.middlewareManager.RegisterAny(mw); err != nil {
			if r.logger != nil {
				r.logger.Warn("Failed to register handler middleware",
					logger.String("method", method),
					logger.String("path", path),
					logger.Error(err),
				)
			}
		}
	}

	// Create appropriate wrapper handler based on detected pattern
	var wrapper func(http.ResponseWriter, *http.Request)
	if handlerPattern == "service-aware" {
		wrapper = r.createServiceAwareOpinionatedWrapper(handler, serviceTypes, requestType, responseType, info, routeSpecificMiddleware)
	} else {
		wrapper = r.createOpinionatedHandlerWrapper(handler, requestType, responseType, info, routeSpecificMiddleware)
	}

	// Register with base router
	switch strings.ToUpper(method) {
	case "GET":
		r.adapter.GET(path, wrapper)
	case "POST":
		r.adapter.POST(path, wrapper)
	case "PUT":
		r.adapter.PUT(path, wrapper)
	case "DELETE":
		r.adapter.DELETE(path, wrapper)
	case "PATCH":
		r.adapter.PATCH(path, wrapper)
	case "OPTIONS":
		r.adapter.OPTIONS(path, wrapper)
	case "HEAD":
		r.adapter.HEAD(path, wrapper)
	default:
		return common.ErrInvalidConfig("method", fmt.Errorf("unsupported HTTP method: %s", method))
	}

	// Store handler info
	handlerKey := fmt.Sprintf("%s %s", method, path)
	r.opinionatedHandlers[handlerKey] = info

	// Initialize route stats
	r.routeStats[handlerKey] = &common.RouteStats{
		Method:     method,
		Path:       path,
		MinLatency: time.Hour,
	}

	// Enhanced logging
	if r.logger != nil {
		logFields := []logger.Field{
			logger.String("method", method),
			logger.String("path", path),
			logger.String("pattern", handlerPattern),
			logger.String("request_type", requestType.String()),
			logger.String("response_type", responseType.String()),
			logger.Int("service_count", len(serviceTypes)),
		}

		if len(serviceNames) > 0 {
			logFields = append(logFields, logger.String("services", strings.Join(serviceNames, ", ")))
		}

		r.logger.Info("opinionated handler registered successfully", logFields...)
	}

	if r.metrics != nil {
		r.metrics.Counter("forge.router.opinionated_handlers_registered").Inc()
		r.metrics.Gauge("forge.router.service_injections_per_handler").Set(float64(len(serviceTypes)))

		if handlerPattern == "service-aware" {
			r.metrics.Counter("forge.router.flexible_service_aware_handlers").Inc()
		} else {
			r.metrics.Counter("forge.router.pure_opinionated_handlers").Inc()
		}
	}

	return nil
}

// Add to ForgeRouter struct (in the streaming section):
func (r *ForgeRouter) RegisterWebSocket(path string, handler interface{}, options ...common.StreamingHandlerInfo) error {
	if r.streamingIntegration == nil {
		return common.ErrValidationError("streaming", fmt.Errorf("streaming not enabled - call EnableStreaming() first"))
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.started {
		return common.ErrLifecycleError("register_websocket", fmt.Errorf("cannot register WebSocket handler after router has started"))
	}

	return r.streamingIntegration.RegisterWebSocket(path, handler, options...)
}

func (r *ForgeRouter) RegisterSSE(path string, handler interface{}, options ...common.StreamingHandlerInfo) error {
	if r.streamingIntegration == nil {
		return common.ErrValidationError("streaming", fmt.Errorf("streaming not enabled - call EnableStreaming() first"))
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.started {
		return common.ErrLifecycleError("register_sse", fmt.Errorf("cannot register SSE handler after router has started"))
	}

	return r.streamingIntegration.RegisterSSE(path, handler, options...)
}

// EnableAsyncAPI enables AsyncAPI documentation generation for streaming endpoints
func (r *ForgeRouter) EnableAsyncAPI(config common.AsyncAPIConfig) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.streamingIntegration != nil && r.streamingIntegration.asyncAPIGen != nil {

		// Update AsyncAPI configuration
		r.streamingIntegration.asyncAPIGen.SetInfo(config.Title, config.Version, config.Description)

		// Add AsyncAPI JSON spec endpoint
		r.adapter.GET(config.SpecPath, func(w http.ResponseWriter, req *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Access-Control-Allow-Origin", "*")

			spec, err := r.streamingIntegration.GetAsyncAPISpecJSON()
			if err != nil {
				if r.logger != nil {
					r.logger.Error("Failed to generate AsyncAPI spec JSON", logger.Error(err))
				}
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			if r.logger != nil {
				r.logger.Debug("Serving AsyncAPI JSON spec",
					logger.Int("spec_size", len(spec)),
				)
			}

			w.Write(spec)
		})

		// Add AsyncAPI YAML spec endpoint
		// Derive YAML path from JSON path by replacing .json with .yaml
		yamlPath := strings.Replace(config.SpecPath, ".json", ".yaml", 1)
		if yamlPath == config.SpecPath {
			// If no .json found, append .yaml to the path
			yamlPath = config.SpecPath + ".yaml"
		}

		r.adapter.GET(yamlPath, func(w http.ResponseWriter, req *http.Request) {
			w.Header().Set("Content-Type", "application/x-yaml")
			w.Header().Set("Access-Control-Allow-Origin", "*")

			spec, err := r.streamingIntegration.GetAsyncAPISpecYAML()
			if err != nil {
				if r.logger != nil {
					r.logger.Error("Failed to generate AsyncAPI spec YAML", logger.Error(err))
				}
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			if r.logger != nil {
				r.logger.Debug("Serving AsyncAPI YAML spec",
					logger.Int("spec_size", len(spec)),
				)
			}

			w.Write(spec)
		})

		// Add AsyncAPI UI endpoint if enabled
		if config.EnableUI {
			r.addAsyncAPIUI(config.UIPath, config.SpecPath)
		}

		if r.logger != nil {
			r.logger.Info("AsyncAPI enabled",
				logger.String("title", config.Title),
				logger.String("version", config.Version),
				logger.String("spec_path_json", config.SpecPath),
				logger.String("spec_path_yaml", yamlPath),
				logger.String("ui_path", config.UIPath),
				logger.Bool("ui_enabled", config.EnableUI),
			)
		}

		if r.metrics != nil {
			r.metrics.Counter("forge.router.asyncapi_enabled").Inc()
		}
	}
}

// EnableStreaming enables streaming support with the given manager
func (r *ForgeRouter) EnableStreaming(streamingManager streaming.StreamingManager) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.started {
		return common.ErrLifecycleError("enable_streaming", fmt.Errorf("cannot enable streaming after router has started"))
	}

	if r.streamingIntegration != nil {
		return common.ErrValidationError("streaming", fmt.Errorf("streaming already enabled"))
	}

	r.streamingIntegration = NewStreamingIntegration(r, streamingManager)

	if r.logger != nil {
		r.logger.Info("streaming enabled",
			logger.String("router", "forge"),
		)
	}

	if r.metrics != nil {
		r.metrics.Counter("forge.router.streaming_enabled").Inc()
	}

	return nil
}

// GetStreamingStats returns streaming-related statistics
func (r *ForgeRouter) GetStreamingStats() map[string]interface{} {
	if r.streamingIntegration == nil {
		return nil
	}

	stats := map[string]interface{}{
		"websocket_handlers": r.streamingIntegration.GetWebSocketHandlers(),
		"sse_handlers":       r.streamingIntegration.GetSSEHandlers(),
		"asyncapi_enabled":   r.streamingIntegration.asyncAPIGen != nil,
	}

	if r.streamingIntegration.asyncAPIGen != nil {
		asyncSpec := r.streamingIntegration.GetAsyncAPISpec()
		if asyncSpec != nil {
			stats["asyncapi_info"] = map[string]interface{}{
				"title":       asyncSpec.Info.Title,
				"version":     asyncSpec.Info.Version,
				"description": asyncSpec.Info.Description,
				"channels":    len(asyncSpec.Channels),
				"operations":  len(asyncSpec.Operations),
				"messages":    len(asyncSpec.Components.Messages),
			}
		}
	}

	return stats
}

func (r *ForgeRouter) createServiceAwareOpinionatedWrapper(
	handler interface{},
	serviceTypes []reflect.Type,
	requestType, responseType reflect.Type,
	info *OpinionatedHandlerInfo,
	routeMiddleware []common.Middleware,
) func(http.ResponseWriter, *http.Request) {
	handlerValue := reflect.ValueOf(handler)

	return func(w http.ResponseWriter, req *http.Request) {
		startTime := time.Now()
		handlerKey := fmt.Sprintf("%s %s", info.Method, info.Path)

		// Create Context
		forgeCtx := common.NewForgeContext(req.Context(), r.container, r.logger, r.metrics, r.config)
		forgeCtx = forgeCtx.WithRequest(req).WithResponseWriter(w)
		forgeCtx = forgeCtx.(*common.ForgeContext).SetRoutePattern(info.Path)

		if pathParams := r.extractPathParameters(req, info.Path); len(pathParams) > 0 {
			forgeCtx = forgeCtx.(*common.ForgeContext).SetPathParams(pathParams)
		}

		r.updateOpinionatedCallStats(handlerKey, startTime)

		// CRITICAL FIX: Apply route-specific middleware in order
		var finalHandler http.Handler = http.HandlerFunc(func(w http.ResponseWriter, req2 *http.Request) {
			// Resolve all services from container
			serviceInstances := make([]reflect.Value, len(serviceTypes))
			for i, serviceType := range serviceTypes {
				var serviceInstance interface{}
				var err error

				if serviceType.Kind() == reflect.Interface {
					interfacePtr := reflect.Zero(reflect.PtrTo(serviceType)).Interface()
					serviceInstance, err = r.container.Resolve(interfacePtr)
				} else {
					serviceInstance, err = r.container.Resolve(reflect.New(serviceType).Interface())
				}

				if err != nil {
					r.updateOpinionatedErrorStats(handlerKey, err)
					serviceName := serviceType.Name()
					if serviceName == "" {
						serviceName = serviceType.String()
					}
					r.writeErrorResponse(w, req2, common.ErrDependencyNotFound(serviceName, serviceType.String()).WithCause(err))
					return
				}

				serviceInstances[i] = reflect.ValueOf(serviceInstance)
			}

			// Create request instance
			requestInstance := reflect.New(requestType)

			// Bind request parameters
			if err := r.bindRequestParameters(forgeCtx, requestInstance.Interface(), requestType); err != nil {
				r.updateOpinionatedErrorStats(handlerKey, err)
				r.writeErrorResponse(w, req2, common.ErrInvalidConfig("request_binding", err))
				return
			}

			// Build call arguments: ctx + services + request
			callArgs := make([]reflect.Value, 1+len(serviceInstances)+1)
			callArgs[0] = reflect.ValueOf(forgeCtx)
			copy(callArgs[1:], serviceInstances)
			callArgs[len(callArgs)-1] = requestInstance.Elem()

			// Call the handler
			results := handlerValue.Call(callArgs)

			// Handle results
			r.handleOpinionatedResponse(w, req2, results, handlerKey, time.Since(startTime))
		})

		// Apply route-specific middleware in reverse order
		for i := len(routeMiddleware) - 1; i >= 0; i-- {
			md := routeMiddleware[i]
			finalHandler = md(finalHandler)
		}

		// Execute the middleware chain
		finalHandler.ServeHTTP(w, req)
	}
}

func (r *ForgeRouter) GetOpinionatedHandlerStats() map[string]OpinionatedHandlerStats {
	r.mu.RLock()
	defer r.mu.RUnlock()

	stats := make(map[string]OpinionatedHandlerStats)
	for key, info := range r.opinionatedHandlers {
		stats[key] = OpinionatedHandlerStats{
			Method:       info.Method,
			Path:         info.Path,
			Pattern:      info.Pattern,
			ServiceCount: len(info.ServiceTypes),
			ServiceNames: info.ServiceNames,
			RequestType:  info.RequestType.String(),
			ResponseType: info.ResponseType.String(),
			CallCount:    info.CallCount,
			ErrorCount:   info.ErrorCount,
			LastCalled:   info.LastCalled,
			RegisteredAt: info.RegisteredAt,
		}
	}

	return stats
}

// extractPathParametersFromSteelRouter attempts to extract path parameters using Steel router's API
func (r *ForgeRouter) extractPathParameters(req *http.Request, routePattern string) map[string]string {
	// Use adapter's parameter extraction
	params := r.adapter.ExtractParams(req)

	// Fallback to manual extraction if adapter returns empty
	if len(params) == 0 {
		return r.manuallyExtractPathParameters(req.URL.Path, routePattern, r.adapter.ParamFormat())
	}

	return params
}

// manuallyExtractPathParameters manually extracts path parameters by comparing patterns
func (r *ForgeRouter) manuallyExtractPathParameters(requestPath, routePattern string, paramFormat ParamFormat) map[string]string {
	params := make(map[string]string)

	routeParts := strings.Split(strings.Trim(routePattern, "/"), "/")
	pathParts := strings.Split(strings.Trim(requestPath, "/"), "/")

	if len(routeParts) != len(pathParts) {
		return params
	}

	for i, routePart := range routeParts {
		var paramName string
		var isParam bool

		switch paramFormat {
		case ParamFormatColon: // Steel, Gin format
			if strings.HasPrefix(routePart, ":") {
				paramName = routePart[1:]
				isParam = true
			}
		case ParamFormatBrace: // Chi, Gorilla format
			if strings.HasPrefix(routePart, "{") && strings.HasSuffix(routePart, "}") {
				paramName = routePart[1 : len(routePart)-1]
				isParam = true
			}
		case ParamFormatStar: // Some routers use * for wildcards
			if strings.HasPrefix(routePart, "*") {
				paramName = routePart[1:]
				isParam = true
			}
		}

		if isParam && i < len(pathParts) {
			params[paramName] = pathParts[i]
		}
	}

	return params
}

func (r *ForgeRouter) Use(middleware any) error {
	var err error
	switch m := middleware.(type) {
	case common.Middleware:
		name := fmt.Sprintf("func_middleware_%d", time.Now().UnixNano())
		err = r.middlewareManager.RegisterFunction(name, 50, m)
		if err != nil {
			return err
		}

		r.adapter.Use(func(next http.Handler) http.Handler { return m(next) })
		return err
	case common.NamedMiddleware:
		err = r.middlewareManager.Register(m)
		if err != nil {
			return err
		}
		r.adapter.Use(func(next http.Handler) http.Handler {
			return m.Handler()(next)
		})
		return err
	case common.StatefulMiddleware:
		err = r.middlewareManager.Register(m)
		if err != nil {
			return err
		}

		r.adapter.Use(func(next http.Handler) http.Handler { return m.Handler()(next) })
		return err
	default:
		return common.ErrInvalidConfig("middleware", fmt.Errorf("unsupported middleware type: %T", middleware))
	}
}

// UseNamedMiddleware adds middleware with explicit name and priority
func (r *ForgeRouter) UseNamedMiddleware(name string, priority int, handler common.Middleware) error {
	return r.middlewareManager.RegisterFunction(name, priority, handler)
}

func (r *ForgeRouter) RemoveMiddleware(middlewareName string) error {
	return r.middlewareManager.Unregister(middlewareName)
}

func (r *ForgeRouter) UseMiddleware(handler func(http.Handler) http.Handler) {
	r.adapter.Use(handler)
}

// Updated router initialization to inject ForgeContext middleware automatically
func (r *ForgeRouter) initializeMiddleware() error {
	// Always add ForgeContext middleware first (highest priority)
	forgeContextMiddleware := common.ForgeContextMiddleware(
		r.container,
		r.logger,
		r.metrics,
		r.config,
	)

	return r.middlewareManager.RegisterFunction("forge-context", 1, forgeContextMiddleware)
}

// =============================================================================
// PLUGIN SUPPORT
// =============================================================================

func (r *ForgeRouter) AddPlugin(plugin common.Plugin) error {
	// return r.pluginManager.AddPlugin(context.Background(), plugin)
	return nil
}

func (r *ForgeRouter) RemovePlugin(pluginName string) error {
	// return r.pluginManager.RemovePlugin(pluginName)
	return nil
}

// =============================================================================
// OPENAPI SUPPORT
// =============================================================================

func (r *ForgeRouter) EnableOpenAPI(config common.OpenAPIConfig) {
	r.initOpenAPI(config)
}

func (r *ForgeRouter) GetOpenAPISpec() *common.OpenAPISpec {
	if r.openAPIGenerator == nil {
		return nil
	}
	return r.openAPIGenerator.GetSpec()
}

func (r *ForgeRouter) UpdateOpenAPISpec(updater func(*common.OpenAPISpec)) {
	if r.openAPIGenerator != nil {
		r.openAPIGenerator.UpdateSpec(updater)
	}
}

// =============================================================================
// LIFECYCLE MANAGEMENT (Updated to use robust middleware manager)
// =============================================================================

func (r *ForgeRouter) Start(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.started {
		return common.ErrLifecycleError("start", fmt.Errorf("router already started"))
	}

	// Validate all handlers can resolve their dependencies
	if r.container != nil {
		validator := r.container.GetValidator()
		if validator != nil {
			if err := validator.ValidateAllWithHandlers(r.serviceHandlers); err != nil {
				return common.ErrContainerError("validate_handlers", err)
			}
		}
	}

	// OnStart the robust middleware manager
	if err := r.middlewareManager.OnStart(ctx); err != nil {
		return fmt.Errorf("failed to start middleware manager: %w", err)
	}

	// Apply middleware to router
	if err := r.middlewareManager.Apply(r); err != nil {
		return common.ErrLifecycleError("apply_middleware", err)
	}

	r.started = true

	if r.logger != nil {
		r.logger.Info("router started",
			logger.Int("service_handlers", len(r.serviceHandlers)),
			logger.Int("opinionated_handlers", len(r.opinionatedHandlers)),
			logger.Int("controllers", len(r.controllers)),
			logger.Int("middleware", len(r.middlewareManager.GetAllMiddleware())),
			// logger.Int("plugins", r.pluginManager.Count()),
			logger.Bool("openapi_enabled", r.openAPIGenerator != nil),
		)
	}

	if r.metrics != nil {
		r.metrics.Counter("forge.router.started").Inc()
	}

	return nil
}

func (r *ForgeRouter) Stop(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.started {
		return common.ErrLifecycleError("stop", fmt.Errorf("router not started"))
	}

	// OnStop the robust middleware manager
	if err := r.middlewareManager.OnStop(ctx); err != nil {
		if r.logger != nil {
			r.logger.Error("failed to stop middleware manager", logger.Error(err))
		}
	}

	r.started = false

	if r.logger != nil {
		r.logger.Info("router stopped")
	}

	if r.metrics != nil {
		r.metrics.Counter("forge.router.stopped").Inc()
	}

	return nil
}

func (r *ForgeRouter) HealthCheck(ctx context.Context) error {
	if !r.started {
		return common.ErrHealthCheckFailed("router", fmt.Errorf("router not started"))
	}

	// Check robust middleware manager
	if err := r.middlewareManager.OnHealthCheck(ctx); err != nil {
		return err
	}

	return nil
}

// =============================================================================
// HTTP SERVER INTEGRATION
// =============================================================================

func (r *ForgeRouter) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.adapter.ServeHTTP(w, req)
}

func (r *ForgeRouter) Handler() http.Handler {
	return r.adapter
}

// UnregisterRoute unregisters a route by ID
func (r *ForgeRouter) UnregisterRoute(ctx context.Context, routeID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	entry, exists := r.routes[routeID]
	if !exists {
		return fmt.Errorf("route not found: %s", routeID)
	}

	// Unregister from main router
	if err := r.unregisterFromMainRouter(entry); err != nil {
		r.logger.Warn("failed to unregister from main router",
			logger.String("route_id", routeID),
			logger.Error(err),
		)
	}

	// Remove from routes map
	delete(r.routes, routeID)

	// Remove from plugin routes mapping
	pluginRoutes := r.pluginRoutes[entry.PluginID]
	for i, id := range pluginRoutes {
		if id == routeID {
			r.pluginRoutes[entry.PluginID] = append(pluginRoutes[:i], pluginRoutes[i+1:]...)
			break
		}
	}

	// Clean up empty plugin mapping
	if len(r.pluginRoutes[entry.PluginID]) == 0 {
		delete(r.pluginRoutes, entry.PluginID)
	}

	// Remove from path index
	if methodMap, exists := r.pathIndex[entry.Method]; exists {
		delete(methodMap, entry.Pattern)
		if len(methodMap) == 0 {
			delete(r.pathIndex, entry.Method)
		}
	}

	r.logger.Info("plugin route unregistered",
		logger.String("plugin_id", entry.PluginID),
		logger.String("route_id", routeID),
		logger.String("method", entry.Method),
		logger.String("path", entry.Pattern),
	)

	if r.metrics != nil {
		r.metrics.Counter("forge.router.plugin_route_unregistered", "plugin_id", entry.PluginID, "method", entry.Method).Inc()
		r.metrics.Gauge("forge.router.plugin_routes_total").Set(float64(len(r.routes)))
	}

	return nil
}

func (r *ForgeRouter) unregisterFromMainRouter(entry *RouteEntry) error {
	// This would need to interface with the main router's unregistration mechanism
	// Implementation depends on ForgeRouter's internal structure
	return nil
}

// =============================================================================
// STATISTICS (Updated to include robust middleware manager stats)
// =============================================================================

func (r *ForgeRouter) GetStats() common.RouterStats {
	r.mu.RLock()
	defer r.mu.RUnlock()

	stats := common.RouterStats{
		HandlersRegistered: len(r.serviceHandlers) + len(r.opinionatedHandlers),
		ControllersCount:   len(r.controllers),
		MiddlewareCount:    len(r.middlewareManager.GetAllMiddleware()),
		// PluginCount:        r.pluginManager.GetStats().TotalPlugins,
		RouteStats:     r.routeStats,
		Started:        r.started,
		OpenAPIEnabled: r.openAPIGenerator != nil,
	}

	// Add streaming stats if available
	if r.streamingIntegration != nil {
		streamingStats := r.GetStreamingStats()
		if wsHandlers, ok := streamingStats["websocket_handlers"].(map[string]*common.WSHandlerInfo); ok {
			stats.WebSocketHandlers = len(wsHandlers)
		}
		if sseHandlers, ok := streamingStats["sse_handlers"].(map[string]*common.SSEHandlerInfo); ok {
			stats.SSEHandlers = len(sseHandlers)
		}
	}

	return stats
}

func (r *ForgeRouter) GetRouteStats(method, path string) (*common.RouteStats, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	handlerKey := fmt.Sprintf("%s %s", method, path)
	if stats, exists := r.routeStats[handlerKey]; exists {
		return stats, nil
	}

	return nil, common.ErrServiceNotFound(handlerKey)
}

// GetMiddlewareStats returns detailed middleware statistics
func (r *ForgeRouter) GetMiddlewareStats() map[string]middleware.MiddlewareStats {
	return r.middlewareManager.GetStats()
}

// GetMiddlewareHealth returns middleware health status
func (r *ForgeRouter) GetMiddlewareHealth(ctx context.Context) error {
	return r.middlewareManager.OnHealthCheck(ctx)
}

// =============================================================================
// INTERNAL HANDLER CREATION METHODS
// =============================================================================

// RegisterPluginRoute implementation
func (r *ForgeRouter) RegisterPluginRoute(pluginID string, route common.RouteDefinition) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Generate unique route ID
	r.routeIDCounter++
	routeID := fmt.Sprintf("plugin_%s_%d", pluginID, r.routeIDCounter)

	// Validate route
	if err := r.validateRoute(route); err != nil {
		return "", err
	}

	// Register with base router
	if err := r.registerControllerRoute(route); err != nil {
		return "", err
	}

	// Track plugin route
	if r.pluginRoutes[pluginID] == nil {
		r.pluginRoutes[pluginID] = make([]string, 0)
	}
	r.pluginRoutes[pluginID] = append(r.pluginRoutes[pluginID], routeID)
	r.routeToPlugin[routeID] = pluginID

	r.logger.Debug("plugin route registered",
		logger.String("plugin_id", pluginID),
		logger.String("route_id", routeID),
		logger.String("method", route.Method),
		logger.String("path", route.Pattern),
	)

	return routeID, nil
}

// UnregisterPluginRoute removes a specific route
func (r *ForgeRouter) UnregisterPluginRoute(routeID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	pluginID, exists := r.routeToPlugin[routeID]
	if !exists {
		return fmt.Errorf("route not found: %s", routeID)
	}

	// Remove from plugin routes
	routes := r.pluginRoutes[pluginID]
	for i, id := range routes {
		if id == routeID {
			r.pluginRoutes[pluginID] = append(routes[:i], routes[i+1:]...)
			break
		}
	}

	// Clean up empty plugin entry
	if len(r.pluginRoutes[pluginID]) == 0 {
		delete(r.pluginRoutes, pluginID)
	}

	delete(r.routeToPlugin, routeID)

	// TODO: Unregister from actual router (this depends on your router implementation)

	return nil
}

// GetPluginRoutes returns all route IDs for a plugin
func (r *ForgeRouter) GetPluginRoutes(pluginID string) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	routes := r.pluginRoutes[pluginID]
	result := make([]string, len(routes))
	copy(result, routes)
	return result
}

func (r *ForgeRouter) registerPluginRoutesHandler(method, path string, handler interface{}, options ...common.HandlerOption) error {
	// We're in plugin configuration mode - register as plugin route
	route := common.RouteDefinition{
		Method:  method,
		Pattern: path,
		Handler: handler,
		Tags:    make(map[string]string),
	}

	// Apply options
	info := &common.RouteHandlerInfo{Tags: make(map[string]string)}
	for _, option := range options {
		option(info)
	}
	route.Tags = info.Tags
	route.Middleware = info.Middleware
	route.Dependencies = info.Dependencies
	route.Config = info.Config
	route.Opinionated = info.Opinionated
	route.RequestType = info.RequestType
	route.ResponseType = info.ResponseType

	// Add plugin metadata
	route.Tags["plugin_id"] = r.currentPluginID

	// Register as plugin route
	err := r.registerPluginRouteInternal(r.currentPluginID, route)
	return err
}

// Internal method to register plugin routes with proper tracking
func (r *ForgeRouter) registerPluginRouteInternal(pluginID string, route common.RouteDefinition) error {
	// Generate unique route ID
	r.routeIDCounter++
	routeID := fmt.Sprintf("plugin_%s_%d", pluginID, r.routeIDCounter)

	// Validate route
	if err := r.validateRoute(route); err != nil {
		return err
	}

	// Register with base router
	if err := r.registerControllerRoute(route); err != nil {
		return err
	}

	// Track plugin route
	if r.pluginRoutes[pluginID] == nil {
		r.pluginRoutes[pluginID] = make([]string, 0)
	}
	r.pluginRoutes[pluginID] = append(r.pluginRoutes[pluginID], routeID)
	r.routeToPlugin[routeID] = pluginID

	if r.logger != nil {
		r.logger.Debug("plugin route registered",
			logger.String("plugin_id", pluginID),
			logger.String("route_id", routeID),
			logger.String("method", route.Method),
			logger.String("path", route.Pattern),
		)
	}

	return nil
}
func (r *ForgeRouter) registerServiceHandler(method, path string, handler interface{}, options ...common.HandlerOption) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.started {
		return common.ErrLifecycleError("register_handler", fmt.Errorf("cannot register handlers after router has started"))
	}

	// Validate handler signature using reflection
	handlerType := reflect.TypeOf(handler)
	if handlerType.Kind() != reflect.Func {
		return common.ErrInvalidConfig("handler", fmt.Errorf("handler must be a function"))
	}

	// Check function signature: func(core.Context, ServiceType, RequestType) (*ResponseType, error)
	if handlerType.NumIn() != 3 || handlerType.NumOut() != 2 {
		return common.ErrInvalidConfig("handler", fmt.Errorf("service handler must have signature func(core.Context, ServiceType, RequestType) (*ResponseType, error)"))
	}

	// Validate parameter types
	contextType := handlerType.In(0)
	serviceType := handlerType.In(1)
	requestType := handlerType.In(2)
	responseType := handlerType.Out(0)
	errorType := handlerType.Out(1)

	// Validate context type
	expectedContextType := reflect.TypeOf((*common.Context)(nil)).Elem()
	if contextType != expectedContextType {
		return common.ErrInvalidConfig("handler", fmt.Errorf("first parameter must be core.Context"))
	}

	// Validate error type
	expectedErrorType := reflect.TypeOf((*error)(nil)).Elem()
	if errorType != expectedErrorType {
		return common.ErrInvalidConfig("handler", fmt.Errorf("second return value must be error"))
	}

	// Remove pointer from response type for reflection
	if responseType.Kind() == reflect.Ptr {
		responseType = responseType.Elem()
	}

	// Create handler info
	info := &common.RouteHandlerInfo{
		ServiceName:  serviceType.Name(),
		ServiceType:  serviceType,
		HandlerFunc:  handler,
		Method:       method,
		Path:         path,
		RegisteredAt: time.Now(),
		Dependencies: []string{serviceType.Name()},
		Tags:         make(map[string]string),
		RequestType:  requestType,
		ResponseType: responseType,
	}

	// Apply options
	for _, option := range options {
		option(info)
	}

	// Process middleware from options using the robust middleware manager
	for _, mw := range info.Middleware {
		if err := r.middlewareManager.RegisterAny(mw); err != nil {
			if r.logger != nil {
				r.logger.Warn("Failed to register handler middleware",
					logger.String("method", method),
					logger.String("path", path),
					logger.Error(err),
				)
			}
		}
	}

	// Generate OpenAPI documentation if enabled
	if r.openAPIGenerator != nil && r.openAPIGenerator.autoUpdate {
		if err := r.openAPIGenerator.AddOperation(method, path, handler, options...); err != nil {
			if r.logger != nil {
				r.logger.Warn("failed to generate OpenAPI documentation",
					logger.String("method", method),
					logger.String("path", path),
					logger.Error(err),
				)
			}
		}
	}

	// Create wrapper handler for steel
	wrapper := r.createServiceHandlerWrapper(handler, serviceType, requestType, responseType, info)

	// Register with base router
	switch strings.ToUpper(method) {
	case "GET":
		r.adapter.GET(path, wrapper)
	case "POST":
		r.adapter.POST(path, wrapper)
	case "PUT":
		r.adapter.PUT(path, wrapper)
	case "DELETE":
		r.adapter.DELETE(path, wrapper)
	case "PATCH":
		r.adapter.PATCH(path, wrapper)
	case "OPTIONS":
		r.adapter.OPTIONS(path, wrapper)
	case "HEAD":
		r.adapter.HEAD(path, wrapper)
	default:
		return common.ErrInvalidConfig("method", fmt.Errorf("unsupported HTTP method: %s", method))
	}

	// Store handler info
	handlerKey := fmt.Sprintf("%s %s", method, path)
	r.serviceHandlers[handlerKey] = info

	// Initialize route stats
	r.routeStats[handlerKey] = &common.RouteStats{
		Method:     method,
		Path:       path,
		MinLatency: time.Hour, // Initialize with high value
	}

	if r.logger != nil {
		r.logger.Info("service handler registered",
			logger.String("method", method),
			logger.String("path", path),
			logger.String("service", info.ServiceName),
			logger.String("handler", fmt.Sprintf("%T", handler)),
		)
	}

	if r.metrics != nil {
		r.metrics.Counter("forge.router.service_handlers_registered").Inc()
	}

	return nil
}

func (r *ForgeRouter) registerControllerRoute(route common.RouteDefinition) error {
	// Create instrumented handler for controller route
	wrapper := r.createControllerRouteWrapper(route)

	// Register with base router
	switch strings.ToUpper(route.Method) {
	case "GET":
		r.adapter.GET(route.Pattern, wrapper)
	case "POST":
		r.adapter.POST(route.Pattern, wrapper)
	case "PUT":
		r.adapter.PUT(route.Pattern, wrapper)
	case "DELETE":
		r.adapter.DELETE(route.Pattern, wrapper)
	case "PATCH":
		r.adapter.PATCH(route.Pattern, wrapper)
	case "OPTIONS":
		r.adapter.OPTIONS(route.Pattern, wrapper)
	case "HEAD":
		r.adapter.HEAD(route.Pattern, wrapper)
	default:
		return common.ErrInvalidConfig("method", fmt.Errorf("unsupported HTTP method: %s", route.Method))
	}

	// Initialize route stats
	handlerKey := fmt.Sprintf("%s %s", route.Method, route.Pattern)
	r.routeStats[handlerKey] = &common.RouteStats{
		Method:     route.Method,
		Path:       route.Pattern,
		MinLatency: time.Hour,
	}

	return nil
}

func (r *ForgeRouter) createServiceHandlerWrapper(
	handler interface{},
	serviceType, requestType, responseType reflect.Type,
	info *common.RouteHandlerInfo,
) func(http.ResponseWriter, *http.Request) {
	handlerValue := reflect.ValueOf(handler)

	return func(w http.ResponseWriter, req *http.Request) {
		startTime := time.Now()
		handlerKey := fmt.Sprintf("%s %s", info.Method, info.Path)

		// Create Context with route pattern - ENHANCED
		forgeCtx := common.NewForgeContext(req.Context(), r.container, r.logger, r.metrics, r.config)
		forgeCtx = forgeCtx.WithRequest(req).WithResponseWriter(w)

		// Set the route pattern for path parameter extraction - CRITICAL FIX
		forgeCtx = forgeCtx.(*common.ForgeContext).SetRoutePattern(info.Path)

		// Try to extract path parameters immediately and store them
		if pathParams := r.extractPathParameters(req, info.Path); len(pathParams) > 0 {
			forgeCtx = forgeCtx.(*common.ForgeContext).SetPathParams(pathParams)
		}

		// Update call statistics
		r.updateCallStats(handlerKey, startTime)

		// Resolve service from container
		var serviceInstance interface{}
		var err error

		if serviceType.Kind() == reflect.Interface {
			interfacePtr := reflect.Zero(reflect.PtrTo(serviceType)).Interface()
			serviceInstance, err = r.container.Resolve(interfacePtr)
		} else {
			serviceInstance, err = r.container.Resolve(reflect.New(serviceType).Interface())
		}

		if err != nil {
			r.updateErrorStats(handlerKey, err)
			r.writeErrorResponse(w, req, common.ErrDependencyNotFound(info.ServiceName, serviceType.Name()).WithCause(err))
			return
		}

		// Create request instance
		requestInstance := reflect.New(requestType)

		// Enhanced parameter binding using the context's BindPath method
		if err := r.bindRequestParameters(forgeCtx, requestInstance.Interface(), requestType); err != nil {
			r.updateErrorStats(handlerKey, err)

			// Enhanced logging for debugging
			if r.logger != nil {
				r.logger.Error("Request parameter binding failed",
					logger.String("method", req.Method),
					logger.String("path", req.URL.Path),
					logger.String("route_pattern", info.Path),
					logger.String("request_type", requestType.Name()),
					logger.Error(err),
				)
			}

			r.writeErrorResponse(w, req, common.ErrInvalidConfig("request_binding", err))
			return
		}

		// Log successful binding for debugging
		if r.logger != nil {
			r.logger.Debug("Request parameters bound successfully",
				logger.String("method", req.Method),
				logger.String("path", req.URL.Path),
				logger.String("route_pattern", info.Path),
				logger.String("request_type", requestType.Name()),
			)
		}

		// Call the handler with proper arguments
		results := handlerValue.Call([]reflect.Value{
			reflect.ValueOf(forgeCtx),
			reflect.ValueOf(serviceInstance),
			requestInstance.Elem(),
		})

		// Handle results
		r.handleServiceResponse(w, req, results, handlerKey, time.Since(startTime))
	}
}

func (r *ForgeRouter) processRouteMiddleware(middlewareList []any) []common.Middleware {
	var processed []common.Middleware

	for _, mw := range middlewareList {
		switch m := mw.(type) {
		case common.Middleware:
			processed = append(processed, m)
		case common.NamedMiddleware:
			processed = append(processed, m.Handler())
		case common.StatefulMiddleware:
			processed = append(processed, m.Handler())
		default:
			if r.logger != nil {
				r.logger.Warn("Unsupported route middleware type, skipping",
					logger.String("type", fmt.Sprintf("%T", mw)),
				)
			}
		}
	}

	return processed
}

func (r *ForgeRouter) createOpinionatedHandlerWrapper(
	handler interface{},
	requestType, responseType reflect.Type,
	info *OpinionatedHandlerInfo,
	routeMiddleware []common.Middleware,
) func(http.ResponseWriter, *http.Request) {
	handlerValue := reflect.ValueOf(handler)

	return func(w http.ResponseWriter, req *http.Request) {
		startTime := time.Now()
		handlerKey := fmt.Sprintf("%s %s", info.Method, info.Path)

		// Create Context
		forgeCtx := common.NewForgeContext(req.Context(), r.container, r.logger, r.metrics, r.config)
		forgeCtx = forgeCtx.WithRequest(req).WithResponseWriter(w)
		forgeCtx = forgeCtx.(*common.ForgeContext).SetRoutePattern(info.Path)

		if pathParams := r.extractPathParameters(req, info.Path); len(pathParams) > 0 {
			forgeCtx = forgeCtx.(*common.ForgeContext).SetPathParams(pathParams)
		}

		r.updateOpinionatedCallStats(handlerKey, startTime)

		// CRITICAL FIX: Apply route-specific middleware in order
		var finalHandler http.Handler = http.HandlerFunc(func(w http.ResponseWriter, req2 *http.Request) {
			// Create request instance
			requestInstance := reflect.New(requestType)

			// Bind request parameters
			if err := r.bindRequestParameters(forgeCtx, requestInstance.Interface(), requestType); err != nil {
				r.updateOpinionatedErrorStats(handlerKey, err)
				r.writeErrorResponse(w, req2, common.ErrInvalidConfig("request_binding", err))
				return
			}

			// Call the actual handler
			results := handlerValue.Call([]reflect.Value{
				reflect.ValueOf(forgeCtx),
				requestInstance.Elem(),
			})

			// Handle results
			r.handleOpinionatedResponse(w, req2, results, handlerKey, time.Since(startTime))
		})

		// Apply route-specific middleware in reverse order (like middleware chains)
		for i := len(routeMiddleware) - 1; i >= 0; i-- {
			md := routeMiddleware[i]
			finalHandler = md(finalHandler)
		}

		// Execute the middleware chain
		finalHandler.ServeHTTP(w, req)
	}
}

func (r *ForgeRouter) createControllerRouteWrapper(route common.RouteDefinition) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, req *http.Request) {
		startTime := time.Now()
		handlerKey := fmt.Sprintf("%s %s", route.Method, route.Pattern)

		// Create Context
		forgeCtx := common.NewForgeContext(req.Context(), r.container, r.logger, r.metrics, r.config)
		forgeCtx = forgeCtx.WithRequest(req).WithResponseWriter(w)

		// Update call statistics
		r.updateCallStats(handlerKey, startTime)

		// Call the controller handler
		if controllerHandler, ok := route.Handler.(common.ControllerHandler); ok {
			err := controllerHandler(forgeCtx)
			if err != nil {
				r.updateErrorStats(handlerKey, err)
				r.writeErrorResponse(w, req, err)
				return
			}
		} else {
			r.updateErrorStats(handlerKey, fmt.Errorf("invalid controller handler signature"))
			r.writeErrorResponse(w, req, common.ErrInvalidConfig("handler", fmt.Errorf("invalid controller handler signature")))
			return
		}

		// Record success metrics
		latency := time.Since(startTime)
		r.updateLatencyStats(handlerKey, latency)

		if r.metrics != nil {
			r.metrics.Counter("forge.router.requests_success").Inc()
			r.metrics.Histogram("forge.router.request_duration").Observe(latency.Seconds())
		}
	}
}

// =============================================================================
// RESPONSE HANDLING METHODS
// =============================================================================

func (r *ForgeRouter) handleServiceResponse(w http.ResponseWriter, req *http.Request, results []reflect.Value, handlerKey string, latency time.Duration) {
	result := results[0]
	errResult := results[1]

	// Update latency statistics
	r.updateLatencyStats(handlerKey, latency)

	// Check for errors
	if !errResult.IsNil() {
		err := errResult.Interface().(error)
		r.updateErrorStats(handlerKey, err)
		r.writeErrorResponse(w, req, err)
		return
	}

	// Handle successful response
	if !result.IsNil() {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		// Extract body content if response has Body field
		responseData := r.extractResponseBody(result.Interface())

		if err := r.writeJSONResponse(w, responseData); err != nil {
			r.updateErrorStats(handlerKey, err)
			r.writeErrorResponse(w, req, common.ErrInternalError("json_encoding", err))
			return
		}
	} else {
		w.WriteHeader(http.StatusNoContent)
	}

	// Record success metrics
	if r.metrics != nil {
		r.metrics.Counter("forge.router.requests_success").Inc()
		r.metrics.Histogram("forge.router.request_duration").Observe(latency.Seconds())
	}
}

// extractResponseBody checks if the response has a Body field and extracts it
func (r *ForgeRouter) extractResponseBody(response interface{}) interface{} {
	if response == nil {
		return nil
	}

	responseValue := reflect.ValueOf(response)

	// Handle pointer
	if responseValue.Kind() == reflect.Ptr {
		if responseValue.IsNil() {
			return nil
		}
		responseValue = responseValue.Elem()
	}

	// Only process structs
	if responseValue.Kind() != reflect.Struct {
		return response
	}

	// Look for Body field
	bodyField := responseValue.FieldByName("Body")
	if bodyField.IsValid() && bodyField.CanInterface() {
		if r.logger != nil {
			r.logger.Debug("Extracting Body field from response",
				logger.String("response_type", responseValue.Type().Name()),
				logger.String("body_type", bodyField.Type().String()),
			)
		}
		return bodyField.Interface()
	}

	// No Body field found, return entire response
	return response
}

func (r *ForgeRouter) handleOpinionatedResponse(w http.ResponseWriter, req *http.Request, results []reflect.Value, handlerKey string, latency time.Duration) {
	result := results[0]
	errResult := results[1]

	// Update latency statistics
	r.updateOpinionatedLatencyStats(handlerKey, latency)

	// Check for errors
	if !errResult.IsNil() {
		err := errResult.Interface().(error)
		r.updateOpinionatedErrorStats(handlerKey, err)
		r.writeErrorResponse(w, req, err)
		return
	}

	// Handle successful response
	if !result.IsNil() {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		// Extract body content if response has Body field
		responseData := r.extractResponseBody(result.Interface())

		if err := r.writeJSONResponse(w, responseData); err != nil {
			r.updateOpinionatedErrorStats(handlerKey, err)
			r.writeErrorResponse(w, req, common.ErrInternalError("json_encoding", err))
			return
		}
	} else {
		w.WriteHeader(http.StatusNoContent)
	}

	// Record success metrics
	if r.metrics != nil {
		r.metrics.Counter("forge.router.opinionated_requests_success").Inc()
		r.metrics.Histogram("forge.router.opinionated_request_duration").Observe(latency.Seconds())
	}
}

// =============================================================================
// STATISTICS UPDATE METHODS
// =============================================================================

func (r *ForgeRouter) updateCallStats(handlerKey string, startTime time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if stats, exists := r.routeStats[handlerKey]; exists {
		stats.CallCount++
		stats.LastCalled = startTime
	}

	if info, exists := r.serviceHandlers[handlerKey]; exists {
		info.CallCount++
		info.LastCalled = startTime
	}
}

func (r *ForgeRouter) updateOpinionatedCallStats(handlerKey string, startTime time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if stats, exists := r.routeStats[handlerKey]; exists {
		stats.CallCount++
		stats.LastCalled = startTime
	}

	if info, exists := r.opinionatedHandlers[handlerKey]; exists {
		info.CallCount++
		info.LastCalled = startTime
	}
}

func (r *ForgeRouter) updateLatencyStats(handlerKey string, latency time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if stats, exists := r.routeStats[handlerKey]; exists {
		stats.TotalLatency += latency
		stats.AverageLatency = stats.TotalLatency / time.Duration(stats.CallCount)

		if latency < stats.MinLatency {
			stats.MinLatency = latency
		}
		if latency > stats.MaxLatency {
			stats.MaxLatency = latency
		}
	}

	if info, exists := r.serviceHandlers[handlerKey]; exists {
		info.AverageLatency = (info.AverageLatency*time.Duration(info.CallCount-1) + latency) / time.Duration(info.CallCount)
	}
}

func (r *ForgeRouter) updateOpinionatedLatencyStats(handlerKey string, latency time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if stats, exists := r.routeStats[handlerKey]; exists {
		stats.TotalLatency += latency
		stats.AverageLatency = stats.TotalLatency / time.Duration(stats.CallCount)

		if latency < stats.MinLatency {
			stats.MinLatency = latency
		}
		if latency > stats.MaxLatency {
			stats.MaxLatency = latency
		}
	}
}

func (r *ForgeRouter) updateErrorStats(handlerKey string, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if stats, exists := r.routeStats[handlerKey]; exists {
		stats.ErrorCount++
		stats.LastError = err
	}

	if info, exists := r.serviceHandlers[handlerKey]; exists {
		info.ErrorCount++
		info.LastError = err
	}

	if r.metrics != nil {
		r.metrics.Counter("forge.router.requests_error").Inc()
	}
}

func (r *ForgeRouter) updateOpinionatedErrorStats(handlerKey string, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if stats, exists := r.routeStats[handlerKey]; exists {
		stats.ErrorCount++
		stats.LastError = err
	}

	if info, exists := r.opinionatedHandlers[handlerKey]; exists {
		info.ErrorCount++
		info.LastError = err
	}

	if r.metrics != nil {
		r.metrics.Counter("forge.router.opinionated_requests_error").Inc()
	}
}

// =============================================================================
// UTILITY METHODS
// =============================================================================

func (r *ForgeRouter) bindRequestParameters(ctx common.Context, request interface{}, requestType reflect.Type) error {
	// Always try path parameter binding first - these are required for the route to match
	pathErr := ctx.BindPath(request)

	// Always try query parameter binding - these are often combined with path params
	queryErr := ctx.BindQuery(request)

	// Always try header parameter binding - these can be present on any request
	headerErr := ctx.BindHeaders(request)

	// For requests with bodies (POST, PUT, PATCH), try body binding
	method := ctx.Request().Method
	var bodyErr error
	if method == "POST" || method == "PUT" || method == "PATCH" {
		// Check if there's a dedicated Body field
		bodyField := r.extractRequestBodyField(requestType, request)

		if bodyField.IsValid() {
			// Bind to the Body field specifically
			bodyErr = r.bindToBodyField(ctx, request, bodyField)
		} else {
			// Try JSON binding first
			bodyErr = ctx.BindJSON(request)
			if bodyErr != nil {
				// If JSON fails, try form binding
				bodyErr = ctx.BindForm(request)
			}
		}
	}

	// Log binding results for debugging
	if r.logger != nil {
		r.logger.Debug("Parameter binding results",
			logger.String("method", method),
			logger.Bool("path_success", pathErr == nil),
			logger.Bool("query_success", queryErr == nil),
			logger.Bool("header_success", headerErr == nil),
			logger.Bool("body_success", bodyErr == nil),
		)
	}

	// Check if we have any critical binding failures
	// Path parameters are critical if they exist in the struct
	if hasPathParameters(requestType) && pathErr != nil {
		return common.ErrInvalidConfig("path_binding", pathErr)
	}

	// For body parameters, only fail if the request should have a body
	if hasBodyParameters(requestType) && (method == "POST" || method == "PUT" || method == "PATCH") && bodyErr != nil {
		return common.ErrInvalidConfig("body_binding", bodyErr)
	}

	return nil
}

// extractRequestBodyField finds and returns the Body field from a request struct
func (r *ForgeRouter) extractRequestBodyField(requestType reflect.Type, request interface{}) reflect.Value {
	if requestType.Kind() == reflect.Ptr {
		requestType = requestType.Elem()
	}

	if requestType.Kind() != reflect.Struct {
		return reflect.Value{}
	}

	requestValue := reflect.ValueOf(request)
	if requestValue.Kind() == reflect.Ptr {
		requestValue = requestValue.Elem()
	}

	// Look for a field with body tag or named "Body"
	for i := 0; i < requestType.NumField(); i++ {
		field := requestType.Field(i)
		if !field.IsExported() {
			continue
		}

		// Check for explicit body tag or field named "Body"
		hasBodyTag := field.Tag.Get("body") != ""
		if hasBodyTag || field.Name == "Body" {
			fieldValue := requestValue.Field(i)
			if fieldValue.IsValid() && fieldValue.CanSet() {
				return fieldValue
			}
		}
	}

	return reflect.Value{}
}

// bindToBodyField binds request body data to a specific Body field
func (r *ForgeRouter) bindToBodyField(ctx common.Context, request interface{}, bodyField reflect.Value) error {
	if !bodyField.IsValid() || !bodyField.CanSet() {
		return fmt.Errorf("invalid body field")
	}

	// Create an instance of the body field's type
	bodyType := bodyField.Type()
	if bodyType.Kind() == reflect.Ptr {
		// If it's a pointer type, create a new instance
		if bodyField.IsNil() {
			bodyField.Set(reflect.New(bodyType.Elem()))
		}
		bodyInstance := bodyField.Interface()

		// Try JSON binding first
		if err := ctx.BindJSON(bodyInstance); err == nil {
			return nil
		}

		// If JSON fails, try form binding
		return ctx.BindForm(bodyInstance)
	} else {
		// If it's a value type, we need to bind to its address
		if bodyField.CanAddr() {
			bodyInstance := bodyField.Addr().Interface()

			// Try JSON binding first
			if err := ctx.BindJSON(bodyInstance); err == nil {
				return nil
			}

			// If JSON fails, try form binding
			return ctx.BindForm(bodyInstance)
		}

		return fmt.Errorf("cannot bind to non-addressable body field")
	}
}

// Helper function to check if struct has path parameters
func hasPathParameters(requestType reflect.Type) bool {
	if requestType.Kind() == reflect.Ptr {
		requestType = requestType.Elem()
	}

	if requestType.Kind() != reflect.Struct {
		return false
	}

	for i := 0; i < requestType.NumField(); i++ {
		field := requestType.Field(i)
		if field.Tag.Get("path") != "" {
			return true
		}
	}
	return false
}

// Helper function to check if struct has body parameters
func hasBodyParameters(requestType reflect.Type) bool {
	if requestType.Kind() == reflect.Ptr {
		requestType = requestType.Elem()
	}

	if requestType.Kind() != reflect.Struct {
		return false
	}

	for i := 0; i < requestType.NumField(); i++ {
		field := requestType.Field(i)

		// Check for body tag, json tag, form tag, or Body field name
		if field.Tag.Get("body") != "" ||
			field.Tag.Get("json") != "" ||
			field.Tag.Get("form") != "" ||
			field.Name == "Body" {
			return true
		}
	}
	return false
}

func (r *ForgeRouter) writeJSONResponse(w http.ResponseWriter, data interface{}) error {
	encoder := json.NewEncoder(w)
	return encoder.Encode(data)
}

func (r *ForgeRouter) writeErrorResponse(w http.ResponseWriter, req *http.Request, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusInternalServerError)

	errorResponse := map[string]interface{}{
		"error": map[string]interface{}{
			"message": err.Error(),
			"path":    req.URL.Path,
			"method":  req.Method,
		},
	}

	json.NewEncoder(w).Encode(errorResponse)
}

// Group creates a new route group with empty prefix
func (r *ForgeRouter) Group(prefix string) common.Router {
	// Log for debugging
	if r.logger != nil {
		r.logger.Debug("Creating route group",
			logger.String("prefix", prefix),
			logger.Bool("adapter_supports_groups", r.adapter.SupportsGroups()),
		)
	}

	if !r.adapter.SupportsGroups() {
		// Fallback to wrapper-based grouping
		group := NewRouteGroup(r, prefix)
		if r.logger != nil {
			r.logger.Debug("Created wrapper-based route group",
				logger.String("prefix", prefix),
			)
		}
		return group
	}

	// Use adapter's native grouping
	subAdapter := r.adapter.Group(prefix)
	fmt.Println("---- Groupsss subAdapter", prefix)
	return r.createSubRouter(subAdapter, prefix)
}

func (r *ForgeRouter) createSubRouter(adapter RouterAdapter, prefix string) common.Router {
	return &ForgeRouter{
		adapter:             adapter,
		container:           r.container,
		logger:              r.logger,
		metrics:             r.metrics,
		config:              r.config,
		healthChecker:       r.healthChecker,
		errorHandler:        r.errorHandler,
		middlewareManager:   r.middlewareManager,
		openAPIGenerator:    r.openAPIGenerator,
		serviceHandlers:     make(map[string]*common.RouteHandlerInfo),
		controllers:         make(map[string]common.Controller),
		opinionatedHandlers: make(map[string]*OpinionatedHandlerInfo),
		routeStats:          make(map[string]*common.RouteStats),
		groupPrefix:         prefix,
	}
}

// GroupFunc creates a route group and calls the provided function with it
func (r *ForgeRouter) GroupFunc(fn func(r common.Router)) common.Router {
	group := NewRouteGroup(r, "")
	fn(group)
	return group
}

// Route creates a route group with a prefix
func (r *ForgeRouter) Route(pattern string, fn func(r common.Router)) common.Router {
	group := NewRouteGroup(r, pattern)
	fn(group)
	return group
}

// Mount mounts a handler at the given pattern
func (r *ForgeRouter) Mount(pattern string, handler http.Handler) {
	// Create a wrapper that preserves the original path for the mounted handler
	mountHandler := http.StripPrefix(pattern, handler)

	// Ensure pattern ends with slash for proper mounting
	if !strings.HasSuffix(pattern, "/") {
		pattern += "/"
	}
	fullPattern := pattern + "*filepath"

	// Create wrapper function
	wrapper := func(w http.ResponseWriter, req *http.Request) {
		// Restore the path for the mounted handler
		originalPath := req.URL.Path

		// Call the mounted handler
		mountHandler.ServeHTTP(w, req)

		// Restore original path (in case it's needed)
		req.URL.Path = originalPath
	}

	// Mount should work for any method
	methods := []string{"GET", "POST", "PUT", "DELETE", "PATCH", "HEAD", "OPTIONS"}
	for _, method := range methods {
		switch method {
		case "GET":
			r.adapter.GET(fullPattern, wrapper)
		case "POST":
			r.adapter.POST(fullPattern, wrapper)
		case "PUT":
			r.adapter.PUT(fullPattern, wrapper)
		case "DELETE":
			r.adapter.DELETE(fullPattern, wrapper)
		case "PATCH":
			r.adapter.PATCH(fullPattern, wrapper)
		case "HEAD":
			r.adapter.HEAD(fullPattern, wrapper)
		case "OPTIONS":
			r.adapter.OPTIONS(fullPattern, wrapper)
		}
	}

	if r.logger != nil {
		r.logger.Info("handler mounted",
			logger.String("pattern", pattern),
			logger.String("handler_type", fmt.Sprintf("%T", handler)),
		)
	}

	if r.metrics != nil {
		r.metrics.Counter("forge.router.handlers_mounted").Inc()
	}
}

func (r *ForgeRouter) initOpenAPI(config common.OpenAPIConfig) {
	if config.Title == "" {
		config.Title = "Forge API"
	}
	if config.Version == "" {
		config.Version = "1.0.0"
	}
	if config.SpecPath == "" {
		config.SpecPath = "/openapi.json"
	}
	if config.UIPath == "" {
		config.UIPath = "/docs"
	}

	// ⭐ CRITICAL: Ensure AutoUpdate is enabled
	if config.AutoUpdate == false {
		if r.logger != nil {
			r.logger.Warn("OpenAPI AutoUpdate is disabled - operations may not appear in spec")
		}
	}

	// Create OpenAPI generator
	r.openAPIGenerator = NewOpenAPIGenerator(r, config)

	if r.logger != nil {
		r.logger.Info("OpenAPI generator initialized",
			logger.String("title", config.Title),
			logger.String("version", config.Version),
			logger.Bool("auto_update", config.AutoUpdate),
			logger.String("spec_path", config.SpecPath),
			logger.String("ui_path", config.UIPath),
		)
	}

	fmt.Println("----Here")
	// Add OpenAPI spec endpoint
	r.adapter.GET(config.SpecPath, func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")

		spec, err := r.openAPIGenerator.GetSpecJSON()
		if err != nil {
			if r.logger != nil {
				r.logger.Error("Failed to generate OpenAPI spec JSON", logger.Error(err))
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		if r.logger != nil {
			r.logger.Debug("Serving OpenAPI spec",
				logger.Int("spec_size", len(spec)),
			)
		}

		w.Write(spec)
	})

	// Add Swagger UI endpoint if enabled
	if config.EnableUI {
		r.addSwaggerUI(config.UIPath, config.SpecPath)
	}

	if r.metrics != nil {
		r.metrics.Counter("forge.router.openapi_enabled").Inc()
	}
}

func (r *ForgeRouter) addSwaggerUI(uiPath, specPath string) {
	r.adapter.GET(uiPath, func(w http.ResponseWriter, req *http.Request) {
		html := fmt.Sprintf(`
<!DOCTYPE html>
<html>
<head>
    <title>API Documentation</title>
    <link rel="stylesheet" type="text/css" href="https://unpkg.com/swagger-ui-dist@5.9.0/swagger-ui.css" />
    <style>
        html { box-sizing: border-box; overflow: -moz-scrollbars-vertical; overflow-y: scroll; }
        *, *:before, *:after { box-sizing: inherit; }
        body { margin:0; background: #fafafa; }
    </style>
</head>
<body>
    <div id="swagger-ui"></div>
    <script src="https://unpkg.com/swagger-ui-dist@5.9.0/swagger-ui-bundle.js"></script>
    <script src="https://unpkg.com/swagger-ui-dist@5.9.0/swagger-ui-standalone-preset.js"></script>
    <script>
        window.onload = function() {
            const ui = SwaggerUIBundle({
                url: '%s',
                dom_id: '#swagger-ui',
                deepLinking: true,
                presets: [
                    SwaggerUIBundle.presets.apis,
                    SwaggerUIStandalonePreset
                ],
                plugins: [
                    SwaggerUIBundle.plugins.DownloadUrl
                ],
                layout: "StandaloneLayout"
            });
        }
    </script>
</body>
</html>`, specPath)

		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(html))
	})
}

// GetAsyncAPISpec returns the AsyncAPI specification
func (r *ForgeRouter) GetAsyncAPISpec() *common.AsyncAPISpec {
	if r.streamingIntegration == nil {
		return nil
	}
	return r.streamingIntegration.GetAsyncAPISpec()
}

// UpdateAsyncAPISpec updates the AsyncAPI specification
func (r *ForgeRouter) UpdateAsyncAPISpec(updater func(*common.AsyncAPISpec)) {
	if r.streamingIntegration != nil && r.streamingIntegration.asyncAPIGen != nil {
		r.streamingIntegration.asyncAPIGen.UpdateSpec(updater)
	}
}

// addAsyncAPIUI adds AsyncAPI documentation UI
func (r *ForgeRouter) addAsyncAPIUI(uiPath, specPath string) {
	r.adapter.GET(fmt.Sprintf("/docs%s", uiPath), func(w http.ResponseWriter, req *http.Request) {
		// Get the full URL for the spec
		scheme := "http"
		if req.TLS != nil {
			scheme = "https"
		}
		fullSpecURL := fmt.Sprintf("%s://%s%s", scheme, req.Host, specPath)

		// Redirect to AsyncAPI Studio with the spec URL
		studioURL := fmt.Sprintf("https://studio.asyncapi.com/?load=%s",
			url.QueryEscape(fullSpecURL))

		http.Redirect(w, req, studioURL, http.StatusTemporaryRedirect)
	})

	r.adapter.GET(uiPath, func(w http.ResponseWriter, req *http.Request) {
		html := fmt.Sprintf(`
<!DOCTYPE html>
<html>
<head>
    <title>AsyncAPI Documentation</title>
    <link rel="stylesheet" type="text/css" href="https://unpkg.com/@asyncapi/react-component@1.0.0-next.39/styles/default.min.css">
    <style>
        html { box-sizing: border-box; overflow: -moz-scrollbars-vertical; overflow-y: scroll; }
        *, *:before, *:after { box-sizing: inherit; }
        body { margin:0; background: #fafafa; }
        #asyncapi { margin: 20px; }
    </style>
</head>
<body>
    <div id="asyncapi"></div>
    <script src="https://unpkg.com/@asyncapi/react-component@1.0.0-next.39/browser/standalone/index.js"></script>
    <script>
        fetch('%s')
            .then(response => response.json())
            .then(schema => {
                AsyncApiStandalone.render({
                    schema: schema,
                    config: {
                        show: {
                            sidebar: true,
                            info: true,
                            servers: true,
                            operations: true,
                            messages: true,
                            schemas: true,
                            errors: true
                        },
                        expand: {
                            messageExamples: true
                        },
                        sidebar: {
                            showServers: true,
                            showOperations: true
                        }
                    }
                }, document.getElementById('asyncapi'));
            })
            .catch(error => {
                console.error('Error loading AsyncAPI spec:', error);
                document.getElementById('asyncapi').innerHTML = 
                    '<div style="color: red; padding: 20px;">Error loading AsyncAPI specification: ' + error.message + '</div>';
            });
    </script>
</body>
</html>`, specPath)

		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(html))
	})
}

func (r *ForgeRouter) validateRoute(route common.RouteDefinition) error {
	if route.Pattern == "" {
		return fmt.Errorf("path cannot be empty")
	}
	if route.Method == "" {
		return fmt.Errorf("method cannot be empty")
	}
	if route.Handler == nil {
		return fmt.Errorf("handler cannot be nil")
	}

	// Validate HTTP method
	validMethods := []string{"GET", "POST", "PUT", "DELETE", "PATCH", "HEAD", "OPTIONS"}
	valid := false
	for _, method := range validMethods {
		if route.Method == method {
			valid = true
			break
		}
	}
	if !valid {
		return fmt.Errorf("invalid HTTP method: %s", route.Method)
	}

	return nil
}

func (r *ForgeRouter) checkRouteConflicts(method, path string) error {
	if methodMap, exists := r.pathIndex[method]; exists {
		if _, exists := methodMap[path]; exists {
			return fmt.Errorf("route %s %s already exists", method, path)
		}
	}
	return nil
}

func (r *ForgeRouter) generateRouteID(pluginID, method, path string) string {
	r.nextRouteID++
	return fmt.Sprintf("plugin_%s_%s_%s_%d", pluginID, method, sanitizePath(path), r.nextRouteID)
}

func sanitizePath(path string) string {
	// Replace special characters for ID generation
	result := ""
	for _, r := range path {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			result += string(r)
		} else {
			result += "_"
		}
	}
	return result
}
