package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/ai/sdk"
)

// Server provides HTTP API endpoints for the SDK
type Server struct {
	llmManager sdk.LLMManager
	logger     forge.Logger
	metrics    forge.Metrics
	router     forge.Router

	// Optional components
	vectorStore sdk.VectorStore
	stateStore  sdk.StateStore
	cacheStore  sdk.CacheStore
	costManager sdk.CostManager

	// Configuration
	config ServerConfig
}

// ServerConfig configures the SDK API server
type ServerConfig struct {
	BasePath          string
	EnableAuth        bool
	APIKey            string
	RateLimit         int
	Timeout           time.Duration
	EnableCORS        bool
	EnableDocs        bool
	EnableMetrics     bool
	EnableHealthCheck bool
}

// DefaultServerConfig returns default configuration
func DefaultServerConfig() ServerConfig {
	return ServerConfig{
		BasePath:          "/api/ai/sdk",
		EnableAuth:        false,
		RateLimit:         100,
		Timeout:           30 * time.Second,
		EnableCORS:        true,
		EnableDocs:        true,
		EnableMetrics:     true,
		EnableHealthCheck: true,
	}
}

// NewServer creates a new SDK API server
func NewServer(llmManager sdk.LLMManager, logger forge.Logger, metrics forge.Metrics, config ServerConfig) *Server {
	return &Server{
		llmManager: llmManager,
		logger:     logger,
		metrics:    metrics,
		config:     config,
	}
}

// WithVectorStore adds vector store support
func (s *Server) WithVectorStore(store sdk.VectorStore) *Server {
	s.vectorStore = store
	return s
}

// WithStateStore adds state store support
func (s *Server) WithStateStore(store sdk.StateStore) *Server {
	s.stateStore = store
	return s
}

// WithCacheStore adds cache store support
func (s *Server) WithCacheStore(store sdk.CacheStore) *Server {
	s.cacheStore = store
	return s
}

// WithCostManager adds cost manager support
func (s *Server) WithCostManager(manager sdk.CostManager) *Server {
	s.costManager = manager
	return s
}

// MountRoutes mounts the SDK API routes to a Forge router
func (s *Server) MountRoutes(router forge.Router) error {
	s.router = router

	// API Group
	apiGroup := router.Group(s.config.BasePath)

	// Health check
	if s.config.EnableHealthCheck {
		apiGroup.GET("/health", s.handleHealth,
			forge.WithSummary("Health Check"),
			forge.WithDescription("Check API health status"),
		)
	}

	// Generation endpoints
	apiGroup.POST("/generate", s.handleGenerate,
		forge.WithSummary("Text Generation"),
		forge.WithDescription("Generate text using LLM"),
		forge.WithTags("generation"),
	)

	apiGroup.POST("/generate/stream", s.handleGenerateStream,
		forge.WithSummary("Streaming Text Generation"),
		forge.WithDescription("Generate text with streaming"),
		forge.WithTags("generation"),
	)

	apiGroup.POST("/generate/object", s.handleGenerateObject,
		forge.WithSummary("Structured Output Generation"),
		forge.WithDescription("Generate structured JSON output"),
		forge.WithTags("generation"),
	)

	// Multi-modal endpoints
	apiGroup.POST("/multimodal", s.handleMultiModal,
		forge.WithSummary("Multi-Modal Generation"),
		forge.WithDescription("Process images, audio, video with AI"),
		forge.WithTags("multimodal"),
	)

	// Agent endpoints
	if s.stateStore != nil {
		agentGroup := apiGroup.Group("/agents")

		agentGroup.POST("", s.handleCreateAgent,
			forge.WithSummary("Create Agent"),
		)

		agentGroup.POST("/:id/execute", s.handleAgentExecute,
			forge.WithSummary("Execute Agent"),
		)

		agentGroup.GET("/:id/state", s.handleGetAgentState,
			forge.WithSummary("Get Agent State"),
		)

		agentGroup.DELETE("/:id", s.handleDeleteAgent,
			forge.WithSummary("Delete Agent"),
		)
	}

	// RAG endpoints
	if s.vectorStore != nil {
		ragGroup := apiGroup.Group("/rag")

		ragGroup.POST("/index", s.handleRAGIndex,
			forge.WithSummary("Index Document"),
		)

		ragGroup.POST("/query", s.handleRAGQuery,
			forge.WithSummary("Query Documents"),
		)
	}

	// Cost management
	if s.costManager != nil {
		costGroup := apiGroup.Group("/cost")

		costGroup.GET("/insights", s.handleCostInsights,
			forge.WithSummary("Get Cost Insights"),
		)

		costGroup.GET("/budget", s.handleCheckBudget,
			forge.WithSummary("Check Budget"),
		)
	}

	// Metrics endpoint
	if s.config.EnableMetrics {
		apiGroup.GET("/metrics", s.handleMetrics,
			forge.WithSummary("Get Metrics"),
		)
	}

	if s.logger != nil {
		s.logger.Info("SDK API routes mounted",
			forge.F("base_path", s.config.BasePath),
			forge.F("auth_enabled", s.config.EnableAuth),
		)
	}

	// Apply middleware to the API group based on configuration
	middlewares := s.buildMiddleware()
	for _, mw := range middlewares {
		apiGroup.Use(mw)
	}

	return nil
}

// buildMiddleware creates middleware stack based on server configuration
func (s *Server) buildMiddleware() []forge.Middleware {
	middlewares := []forge.Middleware{}

	// CORS middleware
	if s.config.EnableCORS {
		middlewares = append(middlewares, s.corsMiddleware)
	}

	// Auth middleware
	if s.config.EnableAuth {
		middlewares = append(middlewares, s.authMiddleware)
	}

	// Logging middleware
	middlewares = append(middlewares, s.loggingMiddleware)

	// Metrics middleware
	if s.config.EnableMetrics {
		middlewares = append(middlewares, s.metricsMiddleware)
	}

	return middlewares
}

// --- Middleware ---

func (s *Server) corsMiddleware(next forge.Handler) forge.Handler {
	return func(ctx forge.Context) error {
		w := ctx.Response()
		r := ctx.Request()

		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return nil
		}

		return next(ctx)
	}
}

func (s *Server) authMiddleware(next forge.Handler) forge.Handler {
	return func(ctx forge.Context) error {
		r := ctx.Request()

		apiKey := r.Header.Get("X-API-Key")
		if apiKey == "" {
			apiKey = r.Header.Get("Authorization")
			if len(apiKey) > 7 && apiKey[:7] == "Bearer " {
				apiKey = apiKey[7:]
			}
		}

		if s.config.APIKey != "" && apiKey != s.config.APIKey {
			return ctx.Status(http.StatusUnauthorized).JSON(map[string]interface{}{
				"error":     "Invalid API key",
				"status":    http.StatusUnauthorized,
				"timestamp": time.Now().Format(time.RFC3339),
			})
		}

		return next(ctx)
	}
}

func (s *Server) loggingMiddleware(next forge.Handler) forge.Handler {
	return func(ctx forge.Context) error {
		start := time.Now()
		r := ctx.Request()

		err := next(ctx)

		if s.logger != nil {
			s.logger.Info("API request",
				forge.F("method", r.Method),
				forge.F("path", r.URL.Path),
				forge.F("duration_ms", time.Since(start).Milliseconds()),
			)
		}

		return err
	}
}

func (s *Server) metricsMiddleware(next forge.Handler) forge.Handler {
	return func(ctx forge.Context) error {
		start := time.Now()
		r := ctx.Request()

		err := next(ctx)

		if s.metrics != nil {
			s.metrics.Counter("sdk.api.requests",
				"method", r.Method,
				"path", r.URL.Path,
			).Inc()
			s.metrics.Histogram("sdk.api.duration",
				"method", r.Method,
				"path", r.URL.Path,
			).Observe(time.Since(start).Seconds())
		}

		return err
	}
}

// --- Handlers ---

func (s *Server) handleHealth(ctx forge.Context) error {
	return ctx.Status(http.StatusOK).JSON(map[string]interface{}{
		"status": "healthy",
		"time":   time.Now().Format(time.RFC3339),
	})
}

func (s *Server) handleGenerate(forgeCtx forge.Context) error {
	var req GenerateRequest
	if err := json.NewDecoder(forgeCtx.Request().Body).Decode(&req); err != nil {
		return forgeCtx.Status(http.StatusBadRequest).JSON(map[string]interface{}{
			"error":     "Invalid request body",
			"status":    http.StatusBadRequest,
			"timestamp": time.Now().Format(time.RFC3339),
		})
	}

	ctx, cancel := context.WithTimeout(forgeCtx.Context(), s.config.Timeout)
	defer cancel()

	// Build and execute generation
	builder := sdk.NewGenerateBuilder(ctx, s.llmManager, s.logger, s.metrics)
	builder.WithPrompt(req.Prompt)

	if req.Model != "" {
		builder.WithModel(req.Model)
	}
	if req.Temperature != nil {
		builder.WithTemperature(*req.Temperature)
	}
	if req.MaxTokens != nil {
		builder.WithMaxTokens(*req.MaxTokens)
	}
	if req.SystemPrompt != "" {
		builder.WithSystemPrompt(req.SystemPrompt)
	}

	result, err := builder.Execute()
	if err != nil {
		return forgeCtx.Status(http.StatusInternalServerError).JSON(map[string]interface{}{
			"error":     err.Error(),
			"status":    http.StatusInternalServerError,
			"timestamp": time.Now().Format(time.RFC3339),
		})
	}

	return forgeCtx.Status(http.StatusOK).JSON(GenerateResponse{
		Content: result.Content,
		Usage:   result.Usage,
	})
}

func (s *Server) handleGenerateStream(forgeCtx forge.Context) error {
	var req GenerateRequest
	if err := json.NewDecoder(forgeCtx.Request().Body).Decode(&req); err != nil {
		return forgeCtx.Status(http.StatusBadRequest).JSON(map[string]interface{}{
			"error":     "Invalid request body",
			"status":    http.StatusBadRequest,
			"timestamp": time.Now().Format(time.RFC3339),
		})
	}

	ctx, cancel := context.WithTimeout(forgeCtx.Context(), s.config.Timeout)
	defer cancel()

	w := forgeCtx.Response()

	// Set up SSE
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		return forgeCtx.Status(http.StatusInternalServerError).JSON(map[string]interface{}{
			"error":     "Streaming not supported",
			"status":    http.StatusInternalServerError,
			"timestamp": time.Now().Format(time.RFC3339),
		})
	}

	// Build streaming generation
	builder := sdk.NewStreamBuilder(ctx, s.llmManager, s.logger, s.metrics)
	builder.WithPrompt(req.Prompt)

	if req.Model != "" {
		builder.WithModel(req.Model)
	}

	builder.OnToken(func(token string) {
		fmt.Fprintf(w, "data: %s\n\n", token)
		flusher.Flush()
	})

	result, err := builder.Stream()
	if err != nil {
		fmt.Fprintf(w, "event: error\ndata: %s\n\n", err.Error())
		flusher.Flush()
		return nil
	}

	// Send final result
	finalData, _ := json.Marshal(map[string]interface{}{
		"content": result.Content,
		"usage":   result.Usage,
	})
	fmt.Fprintf(w, "event: done\ndata: %s\n\n", string(finalData))
	flusher.Flush()

	return nil
}

func (s *Server) handleGenerateObject(forgeCtx forge.Context) error {
	var req GenerateObjectRequest
	if err := json.NewDecoder(forgeCtx.Request().Body).Decode(&req); err != nil {
		return forgeCtx.Status(http.StatusBadRequest).JSON(map[string]interface{}{
			"error":     "Invalid request body",
			"status":    http.StatusBadRequest,
			"timestamp": time.Now().Format(time.RFC3339),
		})
	}

	// For simplicity, return the schema - in production, you'd use reflection
	return forgeCtx.Status(http.StatusOK).JSON(map[string]interface{}{
		"message": "Structured output generation",
		"schema":  req.Schema,
	})
}

func (s *Server) handleMultiModal(forgeCtx forge.Context) error {
	r := forgeCtx.Request()

	// Parse multipart form
	if err := r.ParseMultipartForm(32 << 20); err != nil { // 32 MB max
		return forgeCtx.Status(http.StatusBadRequest).JSON(map[string]interface{}{
			"error":     "Failed to parse form",
			"status":    http.StatusBadRequest,
			"timestamp": time.Now().Format(time.RFC3339),
		})
	}

	prompt := r.FormValue("prompt")
	if prompt == "" {
		return forgeCtx.Status(http.StatusBadRequest).JSON(map[string]interface{}{
			"error":     "Prompt is required",
			"status":    http.StatusBadRequest,
			"timestamp": time.Now().Format(time.RFC3339),
		})
	}

	ctx, cancel := context.WithTimeout(forgeCtx.Context(), s.config.Timeout)
	defer cancel()

	builder := sdk.NewMultiModalBuilder(ctx, s.llmManager, s.logger, s.metrics)
	builder.WithText(prompt)

	// Handle uploaded files
	if r.MultipartForm != nil && r.MultipartForm.File != nil {
		for _, files := range r.MultipartForm.File {
			for _, fileHeader := range files {
				file, err := fileHeader.Open()
				if err != nil {
					continue
				}
				defer file.Close()

				data, err := io.ReadAll(file)
				if err != nil {
					continue
				}

				// Determine content type
				contentType := fileHeader.Header.Get("Content-Type")
				if contentType == "" {
					contentType = http.DetectContentType(data)
				}

				// Add based on content type
				if len(contentType) > 5 && contentType[:5] == "image" {
					builder.WithImage(data, contentType)
				} else if len(contentType) > 5 && contentType[:5] == "audio" {
					builder.WithAudio(data, contentType)
				} else if len(contentType) > 5 && contentType[:5] == "video" {
					builder.WithVideo(data, contentType)
				}
			}
		}
	}

	result, err := builder.Execute()
	if err != nil {
		return forgeCtx.Status(http.StatusInternalServerError).JSON(map[string]interface{}{
			"error":     err.Error(),
			"status":    http.StatusInternalServerError,
			"timestamp": time.Now().Format(time.RFC3339),
		})
	}

	return forgeCtx.Status(http.StatusOK).JSON(map[string]interface{}{
		"text":  result.Text,
		"usage": result.Usage,
	})
}

func (s *Server) handleCreateAgent(ctx forge.Context) error {
	return ctx.Status(http.StatusOK).JSON(map[string]string{
		"message": "Agent creation endpoint",
	})
}

func (s *Server) handleAgentExecute(ctx forge.Context) error {
	return ctx.Status(http.StatusOK).JSON(map[string]string{
		"message": "Agent execution endpoint",
	})
}

func (s *Server) handleGetAgentState(ctx forge.Context) error {
	return ctx.Status(http.StatusOK).JSON(map[string]string{
		"message": "Get agent state endpoint",
	})
}

func (s *Server) handleDeleteAgent(ctx forge.Context) error {
	ctx.Response().WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) handleRAGIndex(ctx forge.Context) error {
	return ctx.Status(http.StatusOK).JSON(map[string]string{
		"message": "RAG indexing endpoint",
	})
}

func (s *Server) handleRAGQuery(ctx forge.Context) error {
	return ctx.Status(http.StatusOK).JSON(map[string]string{
		"message": "RAG query endpoint",
	})
}

func (s *Server) handleCostInsights(ctx forge.Context) error {
	if s.costManager == nil {
		return ctx.Status(http.StatusNotImplemented).JSON(map[string]interface{}{
			"error":     "Cost manager not configured",
			"status":    http.StatusNotImplemented,
			"timestamp": time.Now().Format(time.RFC3339),
		})
	}

	insights := s.costManager.GetInsights()
	return ctx.Status(http.StatusOK).JSON(insights)
}

func (s *Server) handleCheckBudget(forgeCtx forge.Context) error {
	if s.costManager == nil {
		return forgeCtx.Status(http.StatusNotImplemented).JSON(map[string]interface{}{
			"error":     "Cost manager not configured",
			"status":    http.StatusNotImplemented,
			"timestamp": time.Now().Format(time.RFC3339),
		})
	}

	err := s.costManager.CheckBudget(forgeCtx.Context())
	if err != nil {
		return forgeCtx.Status(http.StatusOK).JSON(map[string]interface{}{
			"within_budget": false,
			"error":         err.Error(),
		})
	}

	return forgeCtx.Status(http.StatusOK).JSON(map[string]interface{}{
		"within_budget": true,
	})
}

func (s *Server) handleMetrics(ctx forge.Context) error {
	return ctx.Status(http.StatusOK).JSON(map[string]string{
		"message": "Metrics endpoint",
	})
}

// --- Request/Response Types ---

// GenerateRequest represents a generation request
type GenerateRequest struct {
	Prompt       string   `json:"prompt"`
	Model        string   `json:"model,omitempty"`
	Temperature  *float64 `json:"temperature,omitempty"`
	MaxTokens    *int     `json:"max_tokens,omitempty"`
	SystemPrompt string   `json:"system_prompt,omitempty"`
}

// GenerateResponse represents a generation response
type GenerateResponse struct {
	Content string     `json:"content"`
	Usage   *sdk.Usage `json:"usage,omitempty"`
}

// GenerateObjectRequest represents a structured generation request
type GenerateObjectRequest struct {
	Prompt string                 `json:"prompt"`
	Schema map[string]interface{} `json:"schema"`
	Model  string                 `json:"model,omitempty"`
}

// --- Helper Methods ---
// (All helpers removed - now using forge.Context methods directly)
