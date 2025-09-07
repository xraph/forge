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

// ForgeRouter implements the common.Router interface
type ForgeRouter struct {
	*steel.SteelRouter
	container         common.Container
	logger            common.Logger
	metrics           common.Metrics
	config            common.ConfigManager
	healthChecker     common.HealthChecker
	errorHandler      common.ErrorHandler
	middlewareManager *middleware.Manager // Updated to use robust middleware manager
	pluginManager     *PluginManager
	openAPIGenerator  *OpenAPIGenerator

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
}

// NewForgeRouter creates a new enhanced router implementing common.Router
func NewForgeRouter(config ForgeRouterConfig) common.Router {
	baseRouter := steel.NewRouter()

	// Configure base router options
	if config.RouterOptions.OpenAPITitle != "" {
		baseRouter.OpenAPI().SetTitle(config.RouterOptions.OpenAPITitle)
	}
	if config.RouterOptions.OpenAPIVersion != "" {
		baseRouter.OpenAPI().SetVersion(config.RouterOptions.OpenAPIVersion)
	}
	if config.RouterOptions.OpenAPIDescription != "" {
		baseRouter.OpenAPI().SetDescription(config.RouterOptions.OpenAPIDescription)
	}

	router := &ForgeRouter{
		SteelRouter:         baseRouter,
		container:           config.Container,
		logger:              config.Logger,
		metrics:             config.Metrics,
		config:              config.Config,
		healthChecker:       config.HealthChecker,
		errorHandler:        config.ErrorHandler,
		serviceHandlers:     make(map[string]*common.RouteHandlerInfo),
		controllers:         make(map[string]common.Controller),
		opinionatedHandlers: make(map[string]*OpinionatedHandlerInfo),
		routeStats:          make(map[string]*common.RouteStats),
	}

	// Initialize robust middleware manager
	router.middlewareManager = middleware.NewManager(
		config.Container,
		config.Logger,
		config.Metrics,
		config.Config,
	)

	// Initialize plugin manager
	router.pluginManager = NewPluginManager(router)

	// Initialize OpenAPI generator if config provided
	if config.OpenAPI != nil {
		router.initOpenAPI(*config.OpenAPI)
	}

	// Initialize AsyncAPI generator if config provided
	if config.AsyncAPI != nil {
		router.EnableAsyncAPI(*config.AsyncAPI)
	}

	return router
}

// =============================================================================
// SERVICE-AWARE ROUTE REGISTRATION
// =============================================================================

func (r *ForgeRouter) GET(path string, handler interface{}, options ...common.HandlerOption) error {
	return r.registerServiceHandler("GET", path, handler, options...)
}

func (r *ForgeRouter) POST(path string, handler interface{}, options ...common.HandlerOption) error {
	return r.registerServiceHandler("POST", path, handler, options...)
}

func (r *ForgeRouter) PUT(path string, handler interface{}, options ...common.HandlerOption) error {
	return r.registerServiceHandler("PUT", path, handler, options...)
}

func (r *ForgeRouter) DELETE(path string, handler interface{}, options ...common.HandlerOption) error {
	return r.registerServiceHandler("DELETE", path, handler, options...)
}

func (r *ForgeRouter) PATCH(path string, handler interface{}, options ...common.HandlerOption) error {
	return r.registerServiceHandler("PATCH", path, handler, options...)
}

func (r *ForgeRouter) OPTIONS(path string, handler interface{}, options ...common.HandlerOption) error {
	return r.registerServiceHandler("OPTIONS", path, handler, options...)
}

func (r *ForgeRouter) HEAD(path string, handler interface{}, options ...common.HandlerOption) error {
	return r.registerServiceHandler("HEAD", path, handler, options...)
}

// =============================================================================
// CONTROLLER REGISTRATION
// =============================================================================

func (r *ForgeRouter) RegisterController(controller common.Controller) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.started {
		return common.ErrLifecycleError("register_controller", fmt.Errorf("cannot register controller after router has started"))
	}

	controllerName := controller.Name()
	if _, exists := r.controllers[controllerName]; exists {
		return common.ErrServiceAlreadyExists(controllerName)
	}

	// Initialize controller if not already done
	if err := controller.Initialize(r.container); err != nil {
		return common.ErrContainerError("initialize_controller", err)
	}

	// Register controller middleware with the robust middleware manager
	for _, mw := range controller.Middleware() {
		if err := r.middlewareManager.Register(mw); err != nil {
			return fmt.Errorf("failed to register controller middleware: %w", err)
		}
	}

	// Register controller routes
	for _, route := range controller.Routes() {
		if err := r.registerControllerRoute(controller, route); err != nil {
			return fmt.Errorf("failed to register controller route %s %s: %w", route.Method, route.Pattern, err)
		}
	}

	r.controllers[controllerName] = controller

	if r.logger != nil {
		r.logger.Info("controller registered",
			logger.String("controller", controllerName),
			logger.Int("routes", len(controller.Routes())),
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

	// Generate OpenAPI documentation BEFORE creating wrappers
	if r.openAPIGenerator != nil {
		if r.logger != nil {
			r.logger.Debug("Attempting to add OpenAPI operation",
				logger.String("method", method),
				logger.String("path", path),
				logger.Bool("auto_update", r.openAPIGenerator.autoUpdate),
			)
		}

		// Always try to add the operation (don't check autoUpdate here)
		if err := r.openAPIGenerator.AddOperation(method, path, handler, options...); err != nil {
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
	}

	for _, option := range options {
		option(handlerInfo)
	}

	// Copy tags from handler info
	for k, v := range handlerInfo.Tags {
		info.Tags[k] = v
	}

	// Process middleware from options using the robust middleware manager
	for _, mw := range handlerInfo.Middleware {
		if err := r.middlewareManager.Register(mw); err != nil {
			if r.logger != nil {
				r.logger.Warn("Failed to register handler middleware",
					logger.String("method", method),
					logger.String("path", path),
					logger.String("middleware", mw.Name()),
					logger.Error(err),
				)
			}
		}
	}

	// Create appropriate wrapper handler based on detected pattern
	var wrapper func(http.ResponseWriter, *http.Request)
	if handlerPattern == "service-aware" {
		wrapper = r.createServiceAwareOpinionatedWrapper(handler, serviceTypes, requestType, responseType, info)
	} else {
		wrapper = r.createOpinionatedHandlerWrapper(handler, requestType, responseType, info)
	}

	// Register with base router
	switch strings.ToUpper(method) {
	case "GET":
		r.SteelRouter.GET(path, wrapper)
	case "POST":
		r.SteelRouter.POST(path, wrapper)
	case "PUT":
		r.SteelRouter.PUT(path, wrapper)
	case "DELETE":
		r.SteelRouter.DELETE(path, wrapper)
	case "PATCH":
		r.SteelRouter.PATCH(path, wrapper)
	case "OPTIONS":
		r.SteelRouter.OPTIONS(path, wrapper)
	case "HEAD":
		r.SteelRouter.HEAD(path, wrapper)
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
		r.SteelRouter.GET(config.SpecPath, func(w http.ResponseWriter, req *http.Request) {
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

		r.SteelRouter.GET(yamlPath, func(w http.ResponseWriter, req *http.Request) {
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
) func(http.ResponseWriter, *http.Request) {
	handlerValue := reflect.ValueOf(handler)

	return func(w http.ResponseWriter, req *http.Request) {
		startTime := time.Now()
		handlerKey := fmt.Sprintf("%s %s", info.Method, info.Path)

		// Create Context with route pattern
		forgeCtx := common.NewForgeContext(req.Context(), r.container, r.logger, r.metrics, r.config)
		forgeCtx = forgeCtx.WithRequest(req).WithResponseWriter(w)
		// Set the route pattern for path parameter extraction
		forgeCtx = forgeCtx.(*common.ForgeContext).SetRoutePattern(info.Path)

		// Update call statistics
		r.updateOpinionatedCallStats(handlerKey, startTime)

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
				r.writeErrorResponse(w, req, common.ErrDependencyNotFound(serviceName, serviceType.String()).WithCause(err))
				return
			}

			serviceInstances[i] = reflect.ValueOf(serviceInstance)
		}

		// Create request instance
		requestInstance := reflect.New(requestType)

		// Enhanced parameter binding using the context's BindPath method
		if err := r.bindRequestParameters(forgeCtx, requestInstance.Interface(), requestType); err != nil {
			r.updateOpinionatedErrorStats(handlerKey, err)

			// Log the binding failure for debugging
			if r.logger != nil {
				r.logger.Error("Request parameter binding failed",
					logger.String("method", req.Method),
					logger.String("path", req.URL.Path),
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
				logger.String("request_type", requestType.Name()),
			)
		}

		// Build call arguments: ctx + services + request
		callArgs := make([]reflect.Value, 1+len(serviceInstances)+1)
		callArgs[0] = reflect.ValueOf(forgeCtx)            // Context first
		copy(callArgs[1:], serviceInstances)               // All services in middle
		callArgs[len(callArgs)-1] = requestInstance.Elem() // Request last

		// Call the handler with flexible signature: func(ctx, service1, service2, ..., request)
		results := handlerValue.Call(callArgs)

		// Handle results
		r.handleOpinionatedResponse(w, req, results, handlerKey, time.Since(startTime))
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

func (r *ForgeRouter) AddMiddleware(middleware middleware.Middleware) error {
	return r.middlewareManager.Register(middleware)
}

func (r *ForgeRouter) RemoveMiddleware(middlewareName string) error {
	return r.middlewareManager.Unregister(middlewareName)
}

func (r *ForgeRouter) UseMiddleware(handler func(http.Handler) http.Handler) {
	r.SteelRouter.Use(handler)
}

// =============================================================================
// PLUGIN SUPPORT
// =============================================================================

func (r *ForgeRouter) AddPlugin(plugin common.Plugin) error {
	return r.pluginManager.AddPlugin(plugin)
}

func (r *ForgeRouter) RemovePlugin(pluginName string) error {
	return r.pluginManager.RemovePlugin(pluginName)
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

	// Start the robust middleware manager
	if err := r.middlewareManager.OnStart(ctx); err != nil {
		return fmt.Errorf("failed to start middleware manager: %w", err)
	}

	// Apply middleware to router
	if err := r.middlewareManager.Apply(r); err != nil {
		return common.ErrLifecycleError("apply_middleware", err)
	}

	// Start plugin manager
	if err := r.pluginManager.Start(ctx); err != nil {
		return err
	}

	// Apply plugin routes and middleware
	if err := r.pluginManager.ApplyRoutes(); err != nil {
		return common.ErrLifecycleError("apply_plugin_routes", err)
	}

	if err := r.pluginManager.ApplyMiddleware(); err != nil {
		return common.ErrLifecycleError("apply_plugin_middleware", err)
	}

	r.started = true

	if r.logger != nil {
		r.logger.Info("router started",
			logger.Int("service_handlers", len(r.serviceHandlers)),
			logger.Int("opinionated_handlers", len(r.opinionatedHandlers)),
			logger.Int("controllers", len(r.controllers)),
			logger.Int("middleware", len(r.middlewareManager.GetAllMiddleware())),
			logger.Int("plugins", r.pluginManager.Count()),
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

	// Stop plugin manager
	if err := r.pluginManager.Stop(ctx); err != nil {
		if r.logger != nil {
			r.logger.Error("failed to stop plugin manager", logger.Error(err))
		}
	}

	// Stop the robust middleware manager
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

	// Check plugin manager
	if err := r.pluginManager.HealthCheck(ctx); err != nil {
		return err
	}

	return nil
}

// =============================================================================
// HTTP SERVER INTEGRATION
// =============================================================================

func (r *ForgeRouter) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.SteelRouter.ServeHTTP(w, req)
}

func (r *ForgeRouter) Handler() http.Handler {
	return r.SteelRouter
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
		PluginCount:        r.pluginManager.Count(),
		RouteStats:         r.routeStats,
		Started:            r.started,
		OpenAPIEnabled:     r.openAPIGenerator != nil,
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
		if err := r.middlewareManager.Register(mw); err != nil {
			if r.logger != nil {
				r.logger.Warn("Failed to register handler middleware",
					logger.String("method", method),
					logger.String("path", path),
					logger.String("middleware", mw.Name()),
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
		r.SteelRouter.GET(path, wrapper)
	case "POST":
		r.SteelRouter.POST(path, wrapper)
	case "PUT":
		r.SteelRouter.PUT(path, wrapper)
	case "DELETE":
		r.SteelRouter.DELETE(path, wrapper)
	case "PATCH":
		r.SteelRouter.PATCH(path, wrapper)
	case "OPTIONS":
		r.SteelRouter.OPTIONS(path, wrapper)
	case "HEAD":
		r.SteelRouter.HEAD(path, wrapper)
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

func (r *ForgeRouter) registerControllerRoute(controller common.Controller, route common.RouteDefinition) error {
	// Create instrumented handler for controller route
	wrapper := r.createControllerRouteWrapper(controller, route)

	// Register with base router
	switch strings.ToUpper(route.Method) {
	case "GET":
		r.SteelRouter.GET(route.Pattern, wrapper)
	case "POST":
		r.SteelRouter.POST(route.Pattern, wrapper)
	case "PUT":
		r.SteelRouter.PUT(route.Pattern, wrapper)
	case "DELETE":
		r.SteelRouter.DELETE(route.Pattern, wrapper)
	case "PATCH":
		r.SteelRouter.PATCH(route.Pattern, wrapper)
	case "OPTIONS":
		r.SteelRouter.OPTIONS(route.Pattern, wrapper)
	case "HEAD":
		r.SteelRouter.HEAD(route.Pattern, wrapper)
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

		// Create Context with route pattern
		forgeCtx := common.NewForgeContext(req.Context(), r.container, r.logger, r.metrics, r.config)
		forgeCtx = forgeCtx.WithRequest(req).WithResponseWriter(w)
		// Set the route pattern for path parameter extraction
		forgeCtx = forgeCtx.(*common.ForgeContext).SetRoutePattern(info.Path)

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

			// Log the binding failure for debugging
			if r.logger != nil {
				r.logger.Error("Request parameter binding failed",
					logger.String("method", req.Method),
					logger.String("path", req.URL.Path),
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

func (r *ForgeRouter) createOpinionatedHandlerWrapper(
	handler interface{},
	requestType, responseType reflect.Type,
	info *OpinionatedHandlerInfo,
) func(http.ResponseWriter, *http.Request) {
	handlerValue := reflect.ValueOf(handler)

	return func(w http.ResponseWriter, req *http.Request) {
		startTime := time.Now()
		handlerKey := fmt.Sprintf("%s %s", info.Method, info.Path)

		// Create Context with route pattern
		forgeCtx := common.NewForgeContext(req.Context(), r.container, r.logger, r.metrics, r.config)
		forgeCtx = forgeCtx.WithRequest(req).WithResponseWriter(w)
		// Set the route pattern for path parameter extraction
		forgeCtx = forgeCtx.(*common.ForgeContext).SetRoutePattern(info.Path)

		// Update call statistics
		r.updateOpinionatedCallStats(handlerKey, startTime)

		// Create request instance
		requestInstance := reflect.New(requestType)

		// Enhanced parameter binding using the context's BindPath method
		if err := r.bindRequestParameters(forgeCtx, requestInstance.Interface(), requestType); err != nil {
			r.updateOpinionatedErrorStats(handlerKey, err)

			// Log the binding failure for debugging
			if r.logger != nil {
				r.logger.Error("Request parameter binding failed",
					logger.String("method", req.Method),
					logger.String("path", req.URL.Path),
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
				logger.String("request_type", requestType.Name()),
			)
		}

		// Call the handler with opinionated signature: func(ctx, request)
		results := handlerValue.Call([]reflect.Value{
			reflect.ValueOf(forgeCtx),
			requestInstance.Elem(),
		})

		// Handle results
		r.handleOpinionatedResponse(w, req, results, handlerKey, time.Since(startTime))
	}
}

func (r *ForgeRouter) createControllerRouteWrapper(controller common.Controller, route common.RouteDefinition) func(http.ResponseWriter, *http.Request) {
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
		// Try JSON binding first
		bodyErr = ctx.BindJSON(request)
		if bodyErr != nil {
			// If JSON fails, try form binding
			bodyErr = ctx.BindForm(request)
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
		if field.Tag.Get("json") != "" || field.Tag.Get("body") != "" || field.Tag.Get("form") != "" {
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
	return NewRouteGroup(r, prefix)
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
			r.SteelRouter.GET(fullPattern, wrapper)
		case "POST":
			r.SteelRouter.POST(fullPattern, wrapper)
		case "PUT":
			r.SteelRouter.PUT(fullPattern, wrapper)
		case "DELETE":
			r.SteelRouter.DELETE(fullPattern, wrapper)
		case "PATCH":
			r.SteelRouter.PATCH(fullPattern, wrapper)
		case "HEAD":
			r.SteelRouter.HEAD(fullPattern, wrapper)
		case "OPTIONS":
			r.SteelRouter.OPTIONS(fullPattern, wrapper)
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

	// Add OpenAPI spec endpoint
	r.SteelRouter.GET(config.SpecPath, func(w http.ResponseWriter, req *http.Request) {
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
	r.SteelRouter.GET(uiPath, func(w http.ResponseWriter, req *http.Request) {
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
	r.SteelRouter.GET(fmt.Sprintf("/docs%s", uiPath), func(w http.ResponseWriter, req *http.Request) {
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

	r.SteelRouter.GET(uiPath, func(w http.ResponseWriter, req *http.Request) {
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
