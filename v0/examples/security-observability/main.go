package main

import (
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/xraph/forge/pkg/logger"
	"github.com/xraph/forge/pkg/metrics"
	"github.com/xraph/forge/pkg/observability"
	"github.com/xraph/forge/pkg/security"
)

func main() {
	fmt.Println("🚀 Forge Framework - Security & Observability Example")
	fmt.Println("=====================================================")

	// Initialize logger
	logger := logger.NewLogger(logger.LoggingConfig{
		Level:       "info",
		Format:      "json",
		Environment: "development",
	})

	// Initialize metrics
	metricsService := metrics.NewService(nil, logger)

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

	authzManager := security.NewAuthzManager(security.AuthzConfig{
		EnableRBAC:     true,
		EnableABAC:     true,
		EnablePolicies: true,
		EnableAudit:    true,
		Logger:         logger,
		Metrics:        metricsService,
	})

	rateLimiter := security.NewRateLimiter(security.RateLimitConfig{
		DefaultLimit:    100,
		DefaultWindow:   1 * time.Minute,
		MaxLimiters:     1000,
		CleanupInterval: 5 * time.Minute,
		EnableMetrics:   true,
		Logger:          logger,
		Metrics:         metricsService,
	})

	// Initialize observability components
	tracer := observability.NewTracer(observability.TracingConfig{
		ServiceName:    "forge-example",
		ServiceVersion: "1.0.0",
		Environment:    "development",
		SamplingRate:   1.0,
		MaxSpans:       1000,
		FlushInterval:  5 * time.Second,
		EnableB3:       true,
		EnableW3C:      true,
		EnableJaeger:   false,
		EnableZipkin:   false,
		Logger:         logger,
	})

	monitor := observability.NewMonitor(observability.MonitoringConfig{
		EnableMetrics:     true,
		EnableAlerts:      true,
		EnableHealth:      true,
		EnableUptime:      true,
		EnablePerformance: true,
		CheckInterval:     30 * time.Second,
		AlertTimeout:      5 * time.Minute,
		RetentionPeriod:   7 * 24 * time.Hour,
		MaxMetrics:        10000,
		MaxAlerts:         1000,
		Logger:            logger,
	})

	// Register alert handlers
	logHandler := observability.NewLogAlertHandler(logger)
	monitor.RegisterAlertHandler(logHandler)

	// Create security middleware
	securityMiddleware := security.NewSecurityMiddleware(
		authManager,
		authzManager,
		rateLimiter,
		security.SecurityConfig{
			EnableAuth:            true,
			EnableAuthz:           true,
			EnableRateLimit:       true,
			EnableCORS:            true,
			EnableSecurityHeaders: true,
			TokenHeader:           "Authorization",
			TokenPrefix:           "Bearer",
			RateLimitKey:          "ip",
			AllowedOrigins:        []string{"*"},
			AllowedMethods:        []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
			AllowedHeaders:        []string{"*"},
			MaxAge:                24 * time.Hour,
			Logger:                logger,
		},
	)

	// Create HTTP server with security middleware
	mux := http.NewServeMux()

	// Public endpoints (no authentication required)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		// Start tracing span
		ctx, span := tracer.StartSpan(r.Context(), "health-check")
		defer tracer.EndSpan(span)

		// Record health metric
		monitor.RecordMetric(&observability.Metric{
			Name:        "health_check",
			Type:        observability.MetricTypeGauge,
			Value:       1.0,
			Unit:        "status",
			Labels:      map[string]string{"endpoint": "/health"},
			Description: "Health check endpoint",
		})

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `{"status":"healthy","timestamp":"%s"}`, time.Now().Format(time.RFC3339))
	})

	// Protected endpoints (authentication required)
	mux.HandleFunc("/api/users", func(w http.ResponseWriter, r *http.Request) {
		// Start tracing span
		ctx, span := tracer.StartSpan(r.Context(), "get-users")
		defer tracer.EndSpan(span)

		// Get security context
		securityCtx, err := security.RequireAuth(ctx)
		if err != nil {
			http.Error(w, "Authentication required", http.StatusUnauthorized)
			return
		}

		// Check permissions
		if err := security.RequirePermission(ctx, "users:read"); err != nil {
			http.Error(w, "Permission denied", http.StatusForbidden)
			return
		}

		// Record metrics
		monitor.RecordMetric(&observability.Metric{
			Name:        "api_requests",
			Type:        observability.MetricTypeCounter,
			Value:       1.0,
			Unit:        "requests",
			Labels:      map[string]string{"endpoint": "/api/users", "method": r.Method},
			Description: "API requests",
		})

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `{"users":[],"requested_by":"%s"}`, securityCtx.User.Username)
	})

	// Admin endpoints (admin role required)
	mux.HandleFunc("/api/admin", func(w http.ResponseWriter, r *http.Request) {
		// Start tracing span
		ctx, span := tracer.StartSpan(r.Context(), "admin-operation")
		defer tracer.EndSpan(span)

		// Get security context
		securityCtx, err := security.RequireAuth(ctx)
		if err != nil {
			http.Error(w, "Authentication required", http.StatusUnauthorized)
			return
		}

		// Check admin role
		if err := security.RequireRole(ctx, "admin"); err != nil {
			http.Error(w, "Admin role required", http.StatusForbidden)
			return
		}

		// Record metrics
		monitor.RecordMetric(&observability.Metric{
			Name:        "admin_operations",
			Type:        observability.MetricTypeCounter,
			Value:       1.0,
			Unit:        "operations",
			Labels:      map[string]string{"endpoint": "/api/admin", "user": securityCtx.User.Username},
			Description: "Admin operations",
		})

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `{"admin":"true","user":"%s"}`, securityCtx.User.Username)
	})

	// Apply security middleware
	handler := securityMiddleware.SecurityHeadersMiddleware(
		securityMiddleware.CORSMiddleware(
			securityMiddleware.RateLimitMiddleware(
				securityMiddleware.AuthMiddleware(mux),
			),
		),
	)

	// Create HTTP server
	server := &http.Server{
		Addr:         ":8080",
		Handler:      handler,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start server
	fmt.Println("🌐 Starting HTTP server on :8080")
	fmt.Println("📊 Security features enabled:")
	fmt.Println("  - Authentication (JWT)")
	fmt.Println("  - Authorization (RBAC/ABAC)")
	fmt.Println("  - Rate limiting")
	fmt.Println("  - CORS protection")
	fmt.Println("  - Security headers")
	fmt.Println("🔍 Observability features enabled:")
	fmt.Println("  - Distributed tracing")
	fmt.Println("  - Metrics collection")
	fmt.Println("  - Health monitoring")
	fmt.Println("  - Alerting")
	fmt.Println()
	fmt.Println("📝 Example requests:")
	fmt.Println("  GET  /health          - Health check (public)")
	fmt.Println("  GET  /api/users       - Get users (authenticated)")
	fmt.Println("  GET  /api/admin       - Admin operations (admin role)")
	fmt.Println()
	fmt.Println("🔑 To test authentication, include JWT token in Authorization header:")
	fmt.Println("  Authorization: Bearer <your-jwt-token>")
	fmt.Println()

	// Start monitoring goroutines
	go func() {
		for {
			time.Sleep(30 * time.Second)

			// Record system metrics
			monitor.RecordMetric(&observability.Metric{
				Name:        "system_uptime",
				Type:        observability.MetricTypeGauge,
				Value:       float64(time.Since(time.Now()).Seconds()),
				Unit:        "seconds",
				Labels:      map[string]string{"service": "forge-example"},
				Description: "System uptime",
			})

			// Check health status
			health := monitor.GetHealthStatus()
			fmt.Printf("🏥 Health Status: %s - %s\n", health.Status.String(), health.Message)
		}
	}()

	// Start server
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("Server failed to start: %v", err)
	}
}
