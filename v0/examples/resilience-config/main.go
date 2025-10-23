package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/xraph/forge/pkg/cli"
	"github.com/xraph/forge/pkg/config"
	"github.com/xraph/forge/pkg/logger"
	"github.com/xraph/forge/pkg/metrics"
	"github.com/xraph/forge/pkg/resilience"
)

func main() {
	fmt.Println("🚀 Forge Framework - Resilience & Configuration Example")
	fmt.Println("========================================================")

	// Initialize logger
	logger := logger.NewLogger(logger.LoggingConfig{
		Level:       "info",
		Format:      "json",
		Environment: "development",
	})

	// Initialize metrics
	metricsService := metrics.NewService(nil, logger)

	// Initialize configuration manager
	configManager := config.NewConfigManager(config.ManagerConfig{
		EnableHotReload:  true,
		WatchInterval:    1 * time.Second,
		ReloadTimeout:    30 * time.Second,
		EnableValidation: true,
		EnableSecrets:    true,
		SecretsProvider:  "env",
		Logger:           logger,
		Metrics:          metricsService,
	})

	// Register configuration sources
	fileSource := config.NewFileConfigSource("app", "./config.yaml", "yaml")
	envSource := config.NewEnvironmentConfigSource("env", "FORGE_", []string{
		"LOG_LEVEL", "ENVIRONMENT", "PORT", "DATABASE_URL",
	})

	configManager.RegisterSource(fileSource)
	configManager.RegisterSource(envSource)

	// Load configuration
	appConfig, err := configManager.LoadConfig(context.Background())
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// Initialize resilience components
	circuitBreaker := resilience.NewCircuitBreaker(resilience.CircuitBreakerConfig{
		Name:             "api-circuit-breaker",
		MaxRequests:      10,
		Timeout:          60 * time.Second,
		MaxFailures:      5,
		FailureThreshold: 0.5,
		SuccessThreshold: 0.8,
		RecoveryTimeout:  30 * time.Second,
		Logger:           logger,
		Metrics:          metricsService,
	})

	retry := resilience.NewRetry(resilience.RetryConfig{
		Name:            "api-retry",
		MaxAttempts:     3,
		InitialDelay:    100 * time.Millisecond,
		MaxDelay:        5 * time.Second,
		Multiplier:      2.0,
		Jitter:          true,
		BackoffStrategy: resilience.BackoffStrategyExponential,
		Logger:          logger,
		Metrics:         metricsService,
	})

	gracefulDegradation := resilience.NewGracefulDegradation(resilience.DegradationConfig{
		Name:                   "api-degradation",
		EnableFallbacks:        true,
		FallbackTimeout:        5 * time.Second,
		EnableCircuitBreaker:   true,
		EnableRetry:            true,
		MaxConcurrentFallbacks: 10,
		Logger:                 logger,
		Metrics:                metricsService,
	})

	// Register fallback handlers
	cacheFallback := resilience.NewCacheFallbackHandler("cache-fallback", 1)
	staticFallback := resilience.NewStaticFallbackHandler("static-fallback", 2, map[string]interface{}{
		"message": "Service temporarily unavailable",
		"status":  "degraded",
	})

	gracefulDegradation.RegisterFallback(cacheFallback)
	gracefulDegradation.RegisterFallback(staticFallback)

	// Initialize CLI tools
	cliTools := cli.NewCLITools(cli.CLIConfig{
		EnableLogging: true,
		EnableMetrics: true,
		LogLevel:      "info",
		LogFormat:     "text",
		OutputDir:     "./output",
		TemplatesDir:  "./templates",
		Timeout:       30 * time.Second,
		Logger:        logger,
		Metrics:       metricsService,
	})

	// Create HTTP server with resilience features
	mux := http.NewServeMux()

	// Health check endpoint
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `{"status":"healthy","timestamp":"%s"}`, time.Now().Format(time.RFC3339))
	})

	// API endpoint with circuit breaker and retry
	mux.HandleFunc("/api/data", func(w http.ResponseWriter, r *http.Request) {
		// Use circuit breaker and retry
		result, err := circuitBreaker.Execute(r.Context(), func() (interface{}, error) {
			return retry.Execute(r.Context(), func() (interface{}, error) {
				// Simulate API call
				return simulateAPICall(r.Context())
			})
		})

		if err != nil {
			// Use graceful degradation
			degradationResult, degradationErr := gracefulDegradation.Execute(r.Context(), &APIPrimaryHandler{}, map[string]interface{}{
				"request": r.URL.Path,
				"method":  r.Method,
			})

			if degradationErr != nil {
				http.Error(w, "Service unavailable", http.StatusServiceUnavailable)
				return
			}

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, `{"data":%v,"degraded":true,"handler":"%s"}`, degradationResult.Result, degradationResult.Handler)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `{"data":%v,"degraded":false}`, result)
	})

	// Configuration endpoint
	mux.HandleFunc("/api/config", func(w http.ResponseWriter, r *http.Request) {
		stats := configManager.GetStats()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `{"config":%v,"stats":%v}`, appConfig, stats)
	})

	// Resilience stats endpoint
	mux.HandleFunc("/api/resilience", func(w http.ResponseWriter, r *http.Request) {
		circuitStats := circuitBreaker.GetStats()
		retryStats := retry.GetStats()
		degradationStats := gracefulDegradation.GetStats()

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `{
			"circuit_breaker": {
				"state": "%s",
				"stats": %v
			},
			"retry": {
				"stats": %v
			},
			"graceful_degradation": {
				"stats": %v
			}
		}`, circuitBreaker.GetState().String(), circuitStats, retryStats, degradationStats)
	})

	// CLI tools endpoint
	mux.HandleFunc("/api/tools", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `{
			"tools": {
				"code_generator": "Generate code from templates",
				"test_runner": "Run tests and generate reports",
				"benchmark_runner": "Run benchmarks and generate reports",
				"deployer": "Deploy application to various targets"
			}
		}`)
	})

	// Create HTTP server
	server := &http.Server{
		Addr:         ":8080",
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start configuration watching
	if err := configManager.StartWatching(context.Background()); err != nil {
		log.Fatalf("Failed to start configuration watching: %v", err)
	}

	// Start server
	fmt.Println("🌐 Starting HTTP server on :8080")
	fmt.Println("📊 Resilience features enabled:")
	fmt.Println("  - Circuit Breaker (API protection)")
	fmt.Println("  - Retry Logic (Exponential backoff)")
	fmt.Println("  - Graceful Degradation (Fallback handlers)")
	fmt.Println("  - Configuration Management (Hot reloading)")
	fmt.Println("  - CLI Tools (Code generation, testing, deployment)")
	fmt.Println()
	fmt.Println("📝 Example requests:")
	fmt.Println("  GET  /health          - Health check")
	fmt.Println("  GET  /api/data        - API with resilience features")
	fmt.Println("  GET  /api/config      - Configuration information")
	fmt.Println("  GET  /api/resilience  - Resilience statistics")
	fmt.Println("  GET  /api/tools       - CLI tools information")
	fmt.Println()

	// Start monitoring goroutines
	go func() {
		for {
			time.Sleep(30 * time.Second)

			// Record resilience metrics
			metricsService.IncrementCounter("resilience_checks_total", map[string]string{
				"component": "monitoring",
			})

			// Check circuit breaker state
			state := circuitBreaker.GetState()
			fmt.Printf("🔧 Circuit Breaker State: %s\n", state.String())

			// Check configuration stats
			configStats := configManager.GetStats()
			fmt.Printf("📋 Configuration Sources: %d active, %d total\n",
				configStats.ActiveSources, configStats.TotalSources)
		}
	}()

	// Start server
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("Server failed to start: %v", err)
	}
}

// simulateAPICall simulates an API call with potential failures
func simulateAPICall(ctx context.Context) (interface{}, error) {
	// Simulate random failures
	if time.Now().UnixNano()%3 == 0 {
		return nil, fmt.Errorf("simulated API failure")
	}

	// Simulate processing time
	time.Sleep(100 * time.Millisecond)

	return map[string]interface{}{
		"message":   "API call successful",
		"timestamp": time.Now().Format(time.RFC3339),
		"data":      []string{"item1", "item2", "item3"},
	}, nil
}

// APIPrimaryHandler implements FallbackHandler for API calls
type APIPrimaryHandler struct{}

func (h *APIPrimaryHandler) Execute(ctx context.Context, request interface{}) (interface{}, error) {
	// Simulate primary API call
	return map[string]interface{}{
		"message":   "Primary API response",
		"request":   request,
		"timestamp": time.Now().Format(time.RFC3339),
	}, nil
}

func (h *APIPrimaryHandler) GetName() string {
	return "api-primary"
}

func (h *APIPrimaryHandler) GetPriority() int {
	return 0
}

func (h *APIPrimaryHandler) IsAvailable(ctx context.Context) bool {
	// Simulate availability check
	return true
}
