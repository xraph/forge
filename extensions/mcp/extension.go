package mcp

import (
	"context"
	"fmt"
	"net/http"

	"github.com/xraph/forge"
	"github.com/xraph/go-utils/metrics"
	"github.com/xraph/vessel"
)

// Extension implements forge.Extension for MCP (Model Context Protocol) server.
// The extension is now a lightweight facade that loads config and registers services.
type Extension struct {
	*forge.BaseExtension

	config Config
	// No longer storing server - Vessel manages it
}

// NewExtension creates a new MCP extension with functional options.
// Config is loaded from ConfigManager by default, with options providing overrides.
//
// Example:
//
//	// Load from ConfigManager (tries "extensions.mcp", then "mcp")
//	mcp.NewExtension()
//
//	// Override specific fields
//	mcp.NewExtension(
//	    mcp.WithEnabled(true),
//	    mcp.WithBasePath("/api/mcp"),
//	)
//
//	// Require config from ConfigManager
//	mcp.NewExtension(mcp.WithRequireConfig(true))
func NewExtension(opts ...ConfigOption) forge.Extension {
	config := DefaultConfig()
	for _, opt := range opts {
		opt(&config)
	}

	base := forge.NewBaseExtension("mcp", "2.0.0", "Model Context Protocol Server")

	return &Extension{
		BaseExtension: base,
		config:        config,
	}
}

// NewExtensionWithConfig creates a new MCP extension with a complete config.
// This is for backward compatibility or when config is fully known at initialization.
func NewExtensionWithConfig(config Config) forge.Extension {
	return NewExtension(WithConfig(config))
}

// Register registers the MCP extension with the app.
// This method loads configuration and registers service constructors.
func (e *Extension) Register(app forge.App) error {
	// Call base registration (sets logger, metrics)
	if err := e.BaseExtension.Register(app); err != nil {
		return err
	}

	// Load config from ConfigManager with dual-key support
	programmaticConfig := e.config

	finalConfig := DefaultConfig()
	if err := e.LoadConfig("mcp", &finalConfig, programmaticConfig, DefaultConfig(), programmaticConfig.RequireConfig); err != nil {
		if programmaticConfig.RequireConfig {
			return fmt.Errorf("mcp: failed to load required config: %w", err)
		}

		e.Logger().Warn("mcp: using default/programmatic config",
			forge.F("error", err.Error()),
		)
	}

	e.config = finalConfig

	// Validate config
	if err := e.config.Validate(); err != nil {
		return fmt.Errorf("mcp config validation failed: %w", err)
	}

	if !e.config.Enabled {
		e.Logger().Info("mcp extension disabled")
		return nil
	}

	// Set server name/version from app if not configured
	if finalConfig.ServerName == "" {
		finalConfig.ServerName = app.Name()
	}

	if finalConfig.ServerVersion == "" {
		finalConfig.ServerVersion = app.Version()
	}

	// Register MCPService constructor with Vessel using vessel.WithAliases for backward compatibility
	if err := e.RegisterConstructor(func(logger forge.Logger, metrics forge.Metrics) (*MCPService, error) {
		return NewMCPService(finalConfig, logger, metrics)
	}, vessel.WithAliases(ServiceKey)); err != nil {
		return fmt.Errorf("failed to register mcp service: %w", err)
	}

	e.Logger().Info("mcp extension registered",
		forge.F("base_path", finalConfig.BasePath),
		forge.F("auto_expose", finalConfig.AutoExposeRoutes),
	)

	return nil
}

// Start starts the MCP extension.
func (e *Extension) Start(ctx context.Context) error {
	if !e.config.Enabled {
		return nil
	}

	e.Logger().Info("starting mcp extension")

	// Resolve MCP service from DI
	mcpService, err := forge.InjectType[*MCPService](e.App().Container())
	if err != nil {
		return fmt.Errorf("failed to resolve mcp service: %w", err)
	}

	// Register MCP endpoints
	e.registerEndpoints(mcpService)

	// Auto-expose routes as MCP tools
	if e.config.AutoExposeRoutes {
		e.exposeRoutesAsTools(mcpService)
	}

	e.MarkStarted()
	e.Logger().Info("mcp extension started",
		forge.F("tools", len(mcpService.Server().ListTools())),
	)

	return nil
}

// Stop marks the extension as stopped.
// The actual service is stopped by Vessel calling MCPService.Stop().
func (e *Extension) Stop(ctx context.Context) error {
	if !e.config.Enabled {
		return nil
	}

	e.MarkStopped()
	return nil
}

// Health checks the extension health.
// Service health is managed by Vessel through MCPService.Health().
func (e *Extension) Health(ctx context.Context) error {
	if !e.config.Enabled {
		return nil
	}

	// Health is now managed by Vessel through MCPService.Health()
	return nil
}

// registerEndpoints registers MCP HTTP endpoints.
func (e *Extension) registerEndpoints(mcpService *MCPService) {
	router := e.App().Router()
	basePath := e.config.BasePath

	// Server info endpoint
	router.GET(basePath+"/info", e.handleInfo)

	// Tools endpoints
	router.GET(basePath+"/tools", e.handleListTools)
	router.POST(basePath+"/tools/:name", e.handleCallTool)

	// Resources endpoints (if enabled)
	if e.config.EnableResources {
		router.GET(basePath+"/resources", e.handleListResources)
		router.POST(basePath+"/resources/read", e.handleReadResource)
	}

	// Prompts endpoints (if enabled)
	if e.config.EnablePrompts {
		router.GET(basePath+"/prompts", e.handleListPrompts)
		router.POST(basePath+"/prompts/:name", e.handleGetPrompt)
	}

	e.Logger().Debug("mcp: endpoints registered",
		forge.F("base_path", basePath),
	)
}

// exposeRoutesAsTools automatically exposes Forge routes as MCP tools.
func (e *Extension) exposeRoutesAsTools(mcpService *MCPService) {
	routes := e.App().Router().Routes()

	for _, route := range routes {
		// Skip MCP endpoints themselves
		if len(route.Path) >= len(e.config.BasePath) &&
			route.Path[:len(e.config.BasePath)] == e.config.BasePath {
			continue
		}

		// Check if route should be exposed
		if !e.config.ShouldExpose(route.Path) {
			continue
		}

		// Generate tool from route
		tool, err := mcpService.Server().GenerateToolFromRoute(route)
		if err != nil {
			e.Logger().Warn("mcp: failed to generate tool from route",
				forge.F("path", route.Path),
				forge.F("error", err),
			)

			continue
		}

		// Register tool
		if err := mcpService.Server().RegisterTool(tool); err != nil {
			e.Logger().Warn("mcp: failed to register tool",
				forge.F("tool", tool.Name),
				forge.F("error", err),
			)
		}
	}

	e.Logger().Info("mcp: routes exposed as tools",
		forge.F("total_routes", len(routes)),
		forge.F("tools", len(mcpService.Server().ListTools())),
	)
}

// Handler implementations

func (e *Extension) handleInfo(ctx forge.Context) error {
	mcpService, err := forge.InjectType[*MCPService](e.App().Container())
	if err != nil {
		return ctx.JSON(http.StatusInternalServerError, map[string]string{"error": "Service not available"})
	}

	info := mcpService.Server().GetServerInfo()

	return ctx.JSON(http.StatusOK, info)
}

func (e *Extension) handleListTools(ctx forge.Context) error {
	mcpService, err := forge.InjectType[*MCPService](e.App().Container())
	if err != nil {
		return ctx.JSON(http.StatusInternalServerError, map[string]string{"error": "Service not available"})
	}

	tools := mcpService.Server().ListTools()

	return ctx.JSON(http.StatusOK, ListToolsResponse{
		Tools: tools,
	})
}

func (e *Extension) handleCallTool(ctx forge.Context) error {
	mcpService, err := forge.InjectType[*MCPService](e.App().Container())
	if err != nil {
		return ctx.JSON(http.StatusInternalServerError, map[string]string{"error": "Service not available"})
	}

	toolName := ctx.Param("name")

	// Get tool
	tool, err := mcpService.Server().GetTool(toolName)
	if err != nil {
		return ctx.JSON(http.StatusNotFound, map[string]string{
			"error": err.Error(),
		})
	}

	// Parse request
	var req CallToolRequest
	if err := ctx.Bind(&req); err != nil {
		return ctx.JSON(http.StatusBadRequest, map[string]string{
			"error": "invalid request: " + err.Error(),
		})
	}

	// Execute the tool by calling the underlying route
	result, err := mcpService.Server().ExecuteTool(ctx.Context(), tool, req.Arguments)
	if err != nil {
		response := CallToolResponse{
			Content: []Content{
				{
					Type: "text",
					Text: "Tool execution failed: " + err.Error(),
				},
			},
			IsError: true,
		}

		if e.Metrics() != nil {
			e.Metrics().Counter("mcp_tool_calls_total", metrics.WithLabel("status", "error")).Inc()
		}

		return ctx.JSON(http.StatusInternalServerError, response)
	}

	response := CallToolResponse{
		Content: []Content{
			{
				Type: "text",
				Text: result,
			},
		},
		IsError: false,
	}

	if e.Metrics() != nil {
		e.Metrics().Counter("mcp_tool_calls_total", metrics.WithLabel("status", "success")).Inc()
	}

	return ctx.JSON(http.StatusOK, response)
}

func (e *Extension) handleListResources(ctx forge.Context) error {
	mcpService, err := forge.InjectType[*MCPService](e.App().Container())
	if err != nil {
		return ctx.JSON(http.StatusInternalServerError, map[string]string{"error": "Service not available"})
	}

	resources := mcpService.Server().ListResources()

	return ctx.JSON(http.StatusOK, ListResourcesResponse{
		Resources: resources,
	})
}

func (e *Extension) handleReadResource(ctx forge.Context) error {
	mcpService, err := forge.InjectType[*MCPService](e.App().Container())
	if err != nil {
		return ctx.JSON(http.StatusInternalServerError, map[string]string{"error": "Service not available"})
	}

	var req ReadResourceRequest
	if err := ctx.Bind(&req); err != nil {
		return ctx.JSON(http.StatusBadRequest, map[string]string{
			"error": "invalid request: " + err.Error(),
		})
	}

	// Get resource
	resource, err := mcpService.Server().GetResource(req.URI)
	if err != nil {
		return ctx.JSON(http.StatusNotFound, map[string]string{
			"error": err.Error(),
		})
	}

	// Read resource content
	content, err := mcpService.Server().ReadResource(ctx.Context(), resource)
	if err != nil {
		return ctx.JSON(http.StatusInternalServerError, map[string]string{
			"error": "failed to read resource: " + err.Error(),
		})
	}

	response := ReadResourceResponse{
		Contents: []Content{content},
	}

	return ctx.JSON(http.StatusOK, response)
}

func (e *Extension) handleListPrompts(ctx forge.Context) error {
	mcpService, err := forge.InjectType[*MCPService](e.App().Container())
	if err != nil {
		return ctx.JSON(http.StatusInternalServerError, map[string]string{"error": "Service not available"})
	}

	prompts := mcpService.Server().ListPrompts()

	return ctx.JSON(http.StatusOK, ListPromptsResponse{
		Prompts: prompts,
	})
}

func (e *Extension) handleGetPrompt(ctx forge.Context) error {
	mcpService, err := forge.InjectType[*MCPService](e.App().Container())
	if err != nil {
		return ctx.JSON(http.StatusInternalServerError, map[string]string{"error": "Service not available"})
	}

	promptName := ctx.Param("name")

	// Get prompt
	prompt, err := mcpService.Server().GetPrompt(promptName)
	if err != nil {
		return ctx.JSON(http.StatusNotFound, map[string]string{
			"error": err.Error(),
		})
	}

	// Parse arguments from request body
	var req GetPromptRequest
	if err := ctx.Bind(&req); err != nil {
		// Arguments are optional, continue with empty map
		req.Arguments = make(map[string]any)
	}

	// Generate prompt messages
	messages, err := mcpService.Server().GeneratePrompt(ctx.Context(), prompt, req.Arguments)
	if err != nil {
		return ctx.JSON(http.StatusInternalServerError, map[string]string{
			"error": "failed to generate prompt: " + err.Error(),
		})
	}

	response := GetPromptResponse{
		Description: prompt.Description,
		Messages:    messages,
	}

	return ctx.JSON(http.StatusOK, response)
}
