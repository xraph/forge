package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/xraph/forge/v0/pkg/cache"
	"github.com/xraph/forge/v0/pkg/integration"
	"github.com/xraph/forge/v0/pkg/logger"
	"github.com/xraph/forge/v0/pkg/metrics"
	"github.com/xraph/forge/v0/pkg/performance"
	"github.com/xraph/forge/v0/pkg/pool"
	"github.com/xraph/forge/v0/pkg/resilience"
	"github.com/xraph/forge/v0/pkg/security"
)

func main() {
	fmt.Println("🚀 Forge Framework - Performance & Integration Example")
	fmt.Println("======================================================")

	// Initialize logger
	logger := logger.NewLogger(logger.LoggingConfig{
		Level:       "info",
		Format:      "json",
		Environment: "development",
	})

	// Initialize metrics
	metricsService := metrics.NewService(nil, logger)

	// Initialize advanced caching system
	advancedCache := cache.NewAdvancedCache(cache.AdvancedCacheConfig{
		DefaultBackend: "memory",
		Backends: map[string]cache.BackendConfig{
			"memory": {
				Type: "memory",
			},
			"redis": {
				Type:     "redis",
				Host:     "localhost",
				Port:     6379,
				Database: 0,
				PoolSize: 10,
			},
		},
		EnableMetrics:     true,
		EnableCompression: true,
		EnableEncryption:  false,
		DefaultTTL:        1 * time.Hour,
		MaxSize:           1000000,
		CleanupInterval:   5 * time.Minute,
		Logger:            logger,
		Metrics:           metricsService,
	})

	// Initialize connection pooling
	connectionPool := pool.NewConnectionPool(pool.PoolConfig{
		Name:                "api-pool",
		MaxConnections:      100,
		MinConnections:      5,
		MaxIdleTime:         30 * time.Minute,
		MaxLifetime:         1 * time.Hour,
		AcquireTimeout:      30 * time.Second,
		ReleaseTimeout:      5 * time.Second,
		HealthCheckInterval: 1 * time.Minute,
		EnableMetrics:       true,
		EnableHealthCheck:   true,
		Logger:              logger,
		Metrics:             metricsService,
	})

	// Initialize performance profiler
	profiler := performance.NewProfiler(performance.ProfilerConfig{
		EnableCPUProfiling:       true,
		EnableMemoryProfiling:    true,
		EnableGoroutineProfiling: true,
		EnableBlockProfiling:     true,
		EnableMutexProfiling:     true,
		ProfileInterval:          30 * time.Second,
		ProfileDuration:          10 * time.Second,
		EnableMetrics:            true,
		EnableAlerts:             true,
		CPUThreshold:             80.0,
		MemoryThreshold:          80.0,
		GoroutineThreshold:       1000,
		Logger:                   logger,
		Metrics:                  metricsService,
	})

	// Initialize resilience components
	circuitBreaker := resilience.NewCircuitBreaker(resilience.CircuitBreakerConfig{
		Name:             "api-circuit-breaker",
		MaxRequests:      100,
		Timeout:          60 * time.Second,
		MaxFailures:      10,
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

	// Initialize security components
	authManager, err := security.NewAuthManager(security.AuthConfig{
		JWTSecret:     "your-secret-key-here",
		PasswordSalt:  "your-salt-here",
		JWTExpiration: 24 * time.Hour,
		EnableMFA:     true,
		EnableSSO:     true,
		Logger:        logger,
		Metrics:       metricsService,
	})
	if err != nil {
		log.Fatalf("Failed to create auth manager: %v", err)
	}

	rateLimiter := security.NewRateLimiter(security.RateLimitConfig{
		DefaultLimit:    1000,
		DefaultWindow:   1 * time.Minute,
		MaxLimiters:     1000,
		CleanupInterval: 5 * time.Minute,
		EnableMetrics:   true,
		Logger:          logger,
		Metrics:         metricsService,
	})

	// Create HTTP server with all features
	mux := http.NewServeMux()

	// Health check endpoint
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `{"status":"healthy","timestamp":"%s"}`, time.Now().Format(time.RFC3339))
	})

	// Performance test endpoint
	mux.HandleFunc("/performance", func(w http.ResponseWriter, r *http.Request) {
		// Use circuit breaker and retry
		result, err := circuitBreaker.Execute(r.Context(), func() (interface{}, error) {
			return retry.Execute(r.Context(), func() (interface{}, error) {
				return performIntensiveOperation(r.Context())
			})
		})

		if err != nil {
			http.Error(w, "Operation failed", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `{"result":%v,"timestamp":"%s"}`, result, time.Now().Format(time.RFC3339))
	})

	// Caching endpoint
	mux.HandleFunc("/cache", func(w http.ResponseWriter, r *http.Request) {
		key := r.URL.Query().Get("key")
		if key == "" {
			key = "default"
		}

		// Try to get from cache
		cached, err := advancedCache.Get(r.Context(), key)
		if err != nil {
			// Generate new data
			data := generateData(key)

			// Store in cache
			if err := advancedCache.Set(r.Context(), key, data, 1*time.Hour); err != nil {
				logger.Error("failed to cache data", logger.String("error", err.Error()))
			}

			cached = data
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `{"data":%v,"cached":%t,"timestamp":"%s"}`, cached, err == nil, time.Now().Format(time.RFC3339))
	})

	// Connection pooling endpoint
	mux.HandleFunc("/pool", func(w http.ResponseWriter, r *http.Request) {
		// Acquire connection from pool
		conn, err := connectionPool.Acquire(r.Context())
		if err != nil {
			http.Error(w, "Failed to acquire connection", http.StatusServiceUnavailable)
			return
		}
		defer connectionPool.Release(conn)

		// Use connection
		if err := conn.Ping(); err != nil {
			http.Error(w, "Connection ping failed", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `{"connection_id":"%s","status":"connected","timestamp":"%s"}`, conn.ID(), time.Now().Format(time.RFC3339))
	})

	// Performance metrics endpoint
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		// Get performance metrics
		metrics := profiler.GetMetrics()
		profiles := profiler.GetProfiles()

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `{
			"metrics": %v,
			"profiles": %v,
			"timestamp": "%s"
		}`, metrics, profiles, time.Now().Format(time.RFC3339))
	})

	// Cache statistics endpoint
	mux.HandleFunc("/cache/stats", func(w http.ResponseWriter, r *http.Request) {
		stats := advancedCache.GetStats()
		backendStats := advancedCache.GetBackendStats()

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `{
			"cache_stats": %v,
			"backend_stats": %v,
			"timestamp": "%s"
		}`, stats, backendStats, time.Now().Format(time.RFC3339))
	})

	// Pool statistics endpoint
	mux.HandleFunc("/pool/stats", func(w http.ResponseWriter, r *http.Request) {
		stats := connectionPool.GetStats()
		connectionStats := connectionPool.GetConnectionStats()

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `{
			"pool_stats": %v,
			"connection_stats": %v,
			"timestamp": "%s"
		}`, stats, connectionStats, time.Now().Format(time.RFC3339))
	})

	// Integration test endpoint
	mux.HandleFunc("/integration/test", func(w http.ResponseWriter, r *http.Request) {
		// Run integration test
		testSuite := integration.NewIntegrationTestSuite(integration.TestSuiteConfig{
			TestName:            "performance-integration-test",
			TestDuration:        1 * time.Minute,
			ConcurrentUsers:     10,
			RequestRate:         100,
			EnableSecurity:      true,
			EnableObservability: true,
			EnableResilience:    true,
			EnableCaching:       true,
			EnablePooling:       true,
			EnableProfiling:     true,
			Logger:              logger,
			Metrics:             metricsService,
		})

		result, err := testSuite.RunComprehensiveTest(r.Context())
		if err != nil {
			http.Error(w, "Integration test failed", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `{
			"test_result": %v,
			"timestamp": "%s"
		}`, result, time.Now().Format(time.RFC3339))
	})

	// Create HTTP server
	server := &http.Server{
		Addr:         ":8080",
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start server
	fmt.Println("🌐 Starting HTTP server on :8080")
	fmt.Println("📊 Performance features enabled:")
	fmt.Println("  - Advanced Caching (Memory, Redis)")
	fmt.Println("  - Connection Pooling")
	fmt.Println("  - Performance Profiling")
	fmt.Println("  - Circuit Breaker & Retry")
	fmt.Println("  - Rate Limiting")
	fmt.Println("  - Integration Testing")
	fmt.Println()
	fmt.Println("📝 Example requests:")
	fmt.Println("  GET  /health          - Health check")
	fmt.Println("  GET  /performance      - Performance test with resilience")
	fmt.Println("  GET  /cache?key=test   - Caching test")
	fmt.Println("  GET  /pool            - Connection pooling test")
	fmt.Println("  GET  /metrics         - Performance metrics")
	fmt.Println("  GET  /cache/stats     - Cache statistics")
	fmt.Println("  GET  /pool/stats      - Pool statistics")
	fmt.Println("  GET  /integration/test - Integration test")
	fmt.Println()

	// Start monitoring goroutines
	go func() {
		for {
			time.Sleep(30 * time.Second)

			// Record performance metrics
			metricsService.IncrementCounter("performance_checks_total", map[string]string{
				"component": "monitoring",
			})

			// Check cache statistics
			cacheStats := advancedCache.GetStats()
			fmt.Printf("💾 Cache Stats: %d hits, %d misses, %.2f%% hit rate\n",
				cacheStats.HitRequests, cacheStats.MissRequests, cacheStats.HitRate*100)

			// Check pool statistics
			poolStats := connectionPool.GetStats()
			fmt.Printf("🔗 Pool Stats: %d active, %d idle, %d total\n",
				poolStats.ActiveConnections, poolStats.IdleConnections, poolStats.TotalConnections)

			// Check circuit breaker state
			state := circuitBreaker.GetState()
			fmt.Printf("⚡ Circuit Breaker State: %s\n", state.String())
		}
	}()

	// Start server
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("Server failed to start: %v", err)
	}
}

// performIntensiveOperation simulates an intensive operation
func performIntensiveOperation(ctx context.Context) (interface{}, error) {
	// Simulate CPU-intensive operation
	start := time.Now()
	for time.Since(start) < 100*time.Millisecond {
		// Busy wait
	}

	return map[string]interface{}{
		"operation": "intensive",
		"duration":  time.Since(start).String(),
		"timestamp": time.Now().Format(time.RFC3339),
	}, nil
}

// generateData generates sample data
func generateData(key string) map[string]interface{} {
	return map[string]interface{}{
		"key":       key,
		"data":      []string{"item1", "item2", "item3"},
		"timestamp": time.Now().Format(time.RFC3339),
		"random":    time.Now().UnixNano() % 1000,
	}
}
