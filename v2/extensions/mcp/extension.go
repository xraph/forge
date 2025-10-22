package mcp

import (
	"context"
	"fmt"
	"net/http"

	"github.com/xraph/forge/v2"
)

// Extension implements forge.Extension for MCP (Model Context Protocol) server
type Extension struct {
	*forge.BaseExtension
	config Config
	server *Server
	app    forge.App
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

// Register registers the MCP extension with the app
func (e *Extension) Register(app forge.App) error {
	// Call base registration (sets logger, metrics)
	if err := e.BaseExtension.Register(app); err != nil {
		return err
	}

	e.app = app

	// Load config from ConfigManager with dual-key support
	// Tries "extensions.mcp", then "mcp", with programmatic config overrides
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
	if e.config.ServerName == "" {
		e.config.ServerName = app.Name()
	}
	if e.config.ServerVersion == "" {
		e.config.ServerVersion = app.Version()
	}

	// Create MCP server
	e.server = NewServer(e.config, e.Logger(), e.Metrics())

	// Register MCP server with DI
	if err := forge.RegisterSingleton(app.Container(), "mcp", func(c forge.Container) (*Server, error) {
		return e.server, nil
	}); err != nil {
		return fmt.Errorf("failed to register MCP server: %w", err)
	}

	e.Logger().Info("mcp extension registered",
		forge.F("base_path", e.config.BasePath),
		forge.F("auto_expose", e.config.AutoExposeRoutes),
	)

	return nil
}

// Start starts the MCP extension
func (e *Extension) Start(ctx context.Context) error {
	if !e.config.Enabled {
		return nil
	}

	e.Logger().Info("starting mcp extension")

	// Register MCP endpoints
	e.registerEndpoints()

	// Auto-expose routes as MCP tools
	if e.config.AutoExposeRoutes {
		e.exposeRoutesAsTools()
	}

	e.MarkStarted()
	e.Logger().Info("mcp extension started",
		forge.F("tools", len(e.server.tools)),
	)

	return nil
}

// Stop stops the MCP extension
func (e *Extension) Stop(ctx context.Context) error {
	if !e.config.Enabled {
		return nil
	}

	e.Logger().Info("stopping mcp extension")

	if e.server != nil {
		e.server.Clear()
	}

	e.MarkStopped()
	e.Logger().Info("mcp extension stopped")

	return nil
}

// Health checks if the MCP extension is healthy
func (e *Extension) Health(ctx context.Context) error {
	if !e.config.Enabled {
		return nil
	}

	if e.server == nil {
		return fmt.Errorf("mcp server not initialized")
	}

	return nil
}

// registerEndpoints registers MCP HTTP endpoints
func (e *Extension) registerEndpoints() {
	router := e.app.Router()
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

// exposeRoutesAsTools automatically exposes Forge routes as MCP tools
func (e *Extension) exposeRoutesAsTools() {
	routes := e.app.Router().Routes()

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
		tool, err := e.server.GenerateToolFromRoute(route)
		if err != nil {
			e.Logger().Warn("mcp: failed to generate tool from route",
				forge.F("path", route.Path),
				forge.F("error", err),
			)
			continue
		}

		// Register tool
		if err := e.server.RegisterTool(tool); err != nil {
			e.Logger().Warn("mcp: failed to register tool",
				forge.F("tool", tool.Name),
				forge.F("error", err),
			)
		}
	}

	e.Logger().Info("mcp: routes exposed as tools",
		forge.F("total_routes", len(routes)),
		forge.F("tools", len(e.server.tools)),
	)
}

// Handler implementations

func (e *Extension) handleInfo(ctx forge.Context) error {
	info := e.server.GetServerInfo()
	return ctx.JSON(http.StatusOK, info)
}

func (e *Extension) handleListTools(ctx forge.Context) error {
	tools := e.server.ListTools()
	return ctx.JSON(http.StatusOK, ListToolsResponse{
		Tools: tools,
	})
}

func (e *Extension) handleCallTool(ctx forge.Context) error {
	toolName := ctx.Param("name")

	// Get tool
	tool, err := e.server.GetTool(toolName)
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
	result, err := e.server.ExecuteTool(ctx.Context(), tool, req.Arguments)
	if err != nil {
		response := CallToolResponse{
			Content: []Content{
				{
					Type: "text",
					Text: fmt.Sprintf("Tool execution failed: %s", err.Error()),
				},
			},
			IsError: true,
		}

		if e.Metrics() != nil {
			e.Metrics().Counter("mcp_tool_calls_total", "status", "error").Inc()
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
		e.Metrics().Counter("mcp_tool_calls_total", "status", "success").Inc()
	}

	return ctx.JSON(http.StatusOK, response)
}

func (e *Extension) handleListResources(ctx forge.Context) error {
	resources := e.server.ListResources()
	return ctx.JSON(http.StatusOK, ListResourcesResponse{
		Resources: resources,
	})
}

func (e *Extension) handleReadResource(ctx forge.Context) error {
	var req ReadResourceRequest
	if err := ctx.Bind(&req); err != nil {
		return ctx.JSON(http.StatusBadRequest, map[string]string{
			"error": "invalid request: " + err.Error(),
		})
	}

	// Get resource
	resource, err := e.server.GetResource(req.URI)
	if err != nil {
		return ctx.JSON(http.StatusNotFound, map[string]string{
			"error": err.Error(),
		})
	}

	// Read resource content
	content, err := e.server.ReadResource(ctx.Context(), resource)
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
	prompts := e.server.ListPrompts()
	return ctx.JSON(http.StatusOK, ListPromptsResponse{
		Prompts: prompts,
	})
}

func (e *Extension) handleGetPrompt(ctx forge.Context) error {
	promptName := ctx.Param("name")

	// Get prompt
	prompt, err := e.server.GetPrompt(promptName)
	if err != nil {
		return ctx.JSON(http.StatusNotFound, map[string]string{
			"error": err.Error(),
		})
	}

	// Parse arguments from request body
	var req GetPromptRequest
	if err := ctx.Bind(&req); err != nil {
		// Arguments are optional, continue with empty map
		req.Arguments = make(map[string]interface{})
	}

	// Generate prompt messages
	messages, err := e.server.GeneratePrompt(ctx.Context(), prompt, req.Arguments)
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

// Server returns the MCP server instance
func (e *Extension) Server() *Server {
	return e.server
}
