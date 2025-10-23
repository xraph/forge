package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/xraph/forge/v0/pkg/common"
	"github.com/xraph/forge/v0/pkg/logger"
	"github.com/xraph/forge/v0/pkg/observability"
)

func main() {
	// Create logger
	logger := logger.NewLogger(logger.LoggingConfig{Level: "info"})

	// Create observability configuration
	config := observability.CreateDefaultConfig()
	config.Logger = logger

	// Enable OTLP export for tracing
	config.Tracer.EnableOTLP = true
	config.Tracer.OTLPEndpoint = "http://localhost:4318/v1/traces"

	// Enable Prometheus metrics
	config.Prometheus.EnableMetrics = true
	config.Prometheus.ListenAddress = ":9090"

	// Create observability instance
	obs, err := observability.NewObservability(config)
	if err != nil {
		log.Fatalf("Failed to create observability: %v", err)
	}

	// Register alert handler
	alertHandler := &LogAlertHandler{logger: logger}
	obs.RegisterAlertHandler(alertHandler)

	// Create sample alert
	alert := &observability.Alert{
		ID:          "high-cpu-usage",
		Name:        "High CPU Usage",
		Description: "CPU usage is above 80%",
		Severity:    observability.AlertSeverityCritical,
		Condition:   "cpu_usage",
		Threshold:   80.0,
		Duration:    5 * time.Minute,
		Labels: map[string]string{
			"service": "example",
			"env":     "production",
		},
	}

	if err := obs.AddAlert(alert); err != nil {
		log.Printf("Failed to add alert: %v", err)
	}

	// Start HTTP server for metrics
	http.Handle("/metrics", obs.GetPrometheusHandler())
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		health := obs.GetHealthStatus()
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"status":"%s","message":"%s"}`, health.Status, health.Message)
	})

	go func() {
		log.Println("Starting HTTP server on :8080")
		if err := http.ListenAndServe(":8080", nil); err != nil {
			log.Printf("HTTP server error: %v", err)
		}
	}()

	// Demonstrate observability features
	ctx := context.Background()

	// Example 1: Basic tracing
	log.Println("=== Example 1: Basic Tracing ===")
	obs.WithSpan(ctx, "example-operation", func(ctx context.Context, span interface{}) error {
		log.Println("Executing example operation...")
		time.Sleep(100 * time.Millisecond)
		return nil
	})

	// Example 2: Request observation
	log.Println("=== Example 2: Request Observation ===")
	err = obs.ObserveRequest(ctx, "api-call", func(ctx context.Context) error {
		log.Println("Making API call...")
		time.Sleep(200 * time.Millisecond)
		return nil
	})
	if err != nil {
		log.Printf("Request failed: %v", err)
	}

	// Example 3: Metrics recording
	log.Println("=== Example 3: Metrics Recording ===")
	obs.RecordMetric(&observability.Metric{
		Name:        "custom_metric",
		Type:        observability.MetricTypeCounter,
		Value:       42.0,
		Unit:        "count",
		Labels:      map[string]string{"type": "example"},
		Description: "A custom metric example",
	})

	// Example 4: Error recording
	log.Println("=== Example 4: Error Recording ===")
	obs.RecordError(ctx, fmt.Errorf("example error"), map[string]string{
		"operation": "example",
		"severity":  "warning",
	})

	// Example 5: Metrics with function execution
	log.Println("=== Example 5: Metrics with Function Execution ===")
	err = obs.WithMetrics("database_query", map[string]string{"table": "users"}, func() error {
		log.Println("Executing database query...")
		time.Sleep(150 * time.Millisecond)
		return nil
	})
	if err != nil {
		log.Printf("Database query failed: %v", err)
	}

	// Example 6: Simulate some load to generate metrics
	log.Println("=== Example 6: Simulating Load ===")
	for i := 0; i < 10; i++ {
		go func(id int) {
			obs.ObserveRequest(ctx, "background-task", func(ctx context.Context) error {
				log.Printf("Background task %d executing...", id)
				time.Sleep(time.Duration(100+id*10) * time.Millisecond)
				return nil
			})
		}(i)
	}

	// Wait a bit for background tasks
	time.Sleep(2 * time.Second)

	// Print statistics
	log.Println("=== Observability Statistics ===")
	stats := obs.GetStats()
	for component, stat := range stats {
		log.Printf("%s: %+v", component, stat)
	}

	// Keep running for a while to see metrics
	log.Println("Observability example running...")
	log.Println("Check metrics at: http://localhost:9090/metrics")
	log.Println("Check health at: http://localhost:8080/health")
	log.Println("Press Ctrl+C to stop")

	// Wait for interrupt
	select {}
}

// LogAlertHandler implements AlertHandler for logging alerts
type LogAlertHandler struct {
	logger common.Logger
}

func (h *LogAlertHandler) HandleAlert(ctx context.Context, alert *observability.Alert) error {
	h.logger.Warn("Alert fired",
		logger.String("alert_id", alert.ID),
		logger.String("alert_name", alert.Name),
		logger.String("severity", alert.Severity.String()),
		logger.String("condition", alert.Condition),
		logger.Float64("threshold", alert.Threshold))
	return nil
}

func (h *LogAlertHandler) GetName() string {
	return "log"
}
