# Observability Module

The observability module provides comprehensive monitoring, tracing, and health checking capabilities for Forge applications. It includes built-in support for OpenTelemetry tracing, Prometheus metrics, and flexible health checks.

## Features

### 🔍 Distributed Tracing
- **OpenTelemetry Integration**: Full OpenTelemetry support with automatic instrumentation
- **Multiple Exporters**: Jaeger, OTLP (HTTP/gRPC), Zipkin, and stdout exporters
- **Flexible Sampling**: Configurable sampling rates and strategies
- **Automatic Propagation**: Context propagation across service boundaries
- **Custom Spans**: Easy creation of custom spans with attributes and events

### 📊 Metrics Collection
- **Prometheus Integration**: Built-in Prometheus metrics with automatic collection
- **HTTP Metrics**: Request duration, count, size, and active connections
- **Database Metrics**: Connection pool stats, query performance, and error rates
- **Cache Metrics**: Hit/miss rates, operation counts, and cache sizes
- **Custom Metrics**: Support for counters, gauges, histograms, and summaries
- **Automatic Labeling**: Intelligent labeling with configurable default labels

### 🏥 Health Checks
- **Multiple Check Types**: Database, Redis, HTTP, filesystem, memory, and disk space
- **Kubernetes Ready**: Liveness and readiness probe endpoints
- **Configurable Timeouts**: Per-check timeout configuration
- **Structured Responses**: JSON responses with detailed check information
- **Custom Checks**: Easy creation of custom health check implementations

## Quick Start

### Basic Setup

```go
package main

import (
    "context"
    "log"
    "github.com/xraph/forge/observability"
)

func main() {
    // Initialize tracing
    tracingConfig := observability.TracingConfig{
        Enabled:     true,
        ServiceName: "my-service",
        SampleRate:  0.1,
        Exporters: []observability.ExporterConfig{
            {Type: "jaeger", Endpoint: "http://localhost:14268/api/traces"},
        },
    }
    
    tracer, err := observability.NewTracer(tracingConfig)
    if err != nil {
        log.Fatal(err)
    }
    defer tracer.Shutdown(context.Background())
    
    // Initialize metrics
    metricsConfig := observability.MetricsConfig{
        Enabled:     true,
        ServiceName: "my-service",
        Port:        9090,
        EnableHTTP:  true,
    }
    
    metrics, err := observability.NewMetrics(metricsConfig)
    if err != nil {
        log.Fatal(err)
    }
    
    // Initialize health checks
    healthConfig := observability.HealthConfig{
        Enabled: true,
        Port:    8080,
        Path:    "/health",
    }
    
    health := observability.NewHealth(healthConfig)
    
    // Register health checks
    health.RegisterCheck("database", observability.NewDatabaseHealthCheck("db", db))
    health.RegisterCheck("redis", observability.NewRedisHealthCheck("cache", redisClient))
}
```

### Using with HTTP Handlers

```go
// Add tracing to HTTP handlers
http.HandleFunc("/api/users", func(w http.ResponseWriter, r *http.Request) {
    ctx, span := tracer.StartSpan(r.Context(), "get_users")
    defer span.End()
    
    // Add custom attributes
    span.SetAttribute("user.id", userID)
    span.SetAttribute("request.method", r.Method)
    
    // Your handler logic here
})

// Add metrics middleware
handler := metrics.HTTPMiddleware()(http.DefaultServeMux)
http.ListenAndServe(":8080", handler)
```

## Configuration

### Tracing Configuration

```yaml
observability:
  tracing:
    enabled: true
    service_name: "my-service"
    service_version: "1.0.0"
    environment: "production"
    sample_rate: 0.1
    exporters:
      - type: "jaeger"
        endpoint: "http://jaeger:14268/api/traces"
      - type: "otlp"
        endpoint: "https://otel-collector:4317"
        headers:
          authorization: "Bearer your-token"
        compression: "gzip"
    resource_attributes:
      service.instance.id: "instance-1"
      service.region: "us-east-1"
    batch_timeout: "5s"
    export_timeout: "30s"
    max_export_batch_size: 512
    max_queue_size: 2048
```

### Metrics Configuration

```yaml
observability:
  metrics:
    enabled: true
    service_name: "my-service"
    namespace: "myapp"
    subsystem: "api"
    host: "0.0.0.0"
    port: 9090
    path: "/metrics"
    interval: "15s"
    timeout: "10s"
    enable_go: true
    enable_process: true
    enable_http: true
    enable_database: true
    enable_cache: true
    default_labels:
      environment: "production"
      version: "1.0.0"
```

### Health Configuration

```yaml
observability:
  health:
    enabled: true
    host: "0.0.0.0"
    port: 8080
    path: "/health"
    timeout: "30s"
    interval: "30s"
    liveness_path: "/health/live"
    readiness_path: "/health/ready"
    format: "json"
    include_details: true
```

## Advanced Usage

### Custom Tracing

```go
// Create custom spans
ctx, span := tracer.StartSpan(ctx, "custom_operation",
    observability.WithSpanKind(observability.SpanKindClient),
    observability.WithAttributes(
        observability.Attribute{Key: "operation.type", Value: "database"},
        observability.Attribute{Key: "operation.name", Value: "user.create"},
    ),
)
defer span.End()

// Add events and errors
span.AddEvent("validation.started")
if err != nil {
    span.RecordError(err)
    span.SetStatus(observability.StatusCodeError, "Validation failed")
}
```

### Custom Metrics

```go
// Create custom metrics
counter := metrics.Counter("api_requests_total",
    observability.Label{Name: "method", Value: "GET"},
    observability.Label{Name: "endpoint", Value: "/users"},
)
counter.Inc()

// Use histograms for timing
histogram := metrics.Histogram("operation_duration_seconds", 
    []float64{0.1, 0.5, 1.0, 2.5, 5.0})
timer := histogram.Timer()
defer timer.ObserveDuration()

// Gauge for current values
gauge := metrics.Gauge("active_connections")
gauge.Set(42)
```

### Custom Health Checks

```go
type CustomHealthCheck struct {
    name string
    client *http.Client
}

func (c *CustomHealthCheck) Check(ctx context.Context) error {
    req, _ := http.NewRequestWithContext(ctx, "GET", "https://api.example.com/health", nil)
    resp, err := c.client.Do(req)
    if err != nil {
        return err
    }
    defer resp.Body.Close()
    
    if resp.StatusCode != http.StatusOK {
        return fmt.Errorf("service unhealthy: %d", resp.StatusCode)
    }
    return nil
}

func (c *CustomHealthCheck) Name() string { return c.name }
func (c *CustomHealthCheck) Description() string { return "External API health check" }
func (c *CustomHealthCheck) Timeout() time.Duration { return 10 * time.Second }

// Register the custom health check
health.RegisterCheck("external_api", &CustomHealthCheck{
    name: "external_api",
    client: &http.Client{Timeout: 5 * time.Second},
})
```

## Built-in Health Checks

### Database Health Check
```go
dbCheck := observability.NewDatabaseHealthCheck("primary_db", db)
health.RegisterCheck("database", dbCheck)
```

### Redis Health Check
```go
redisCheck := observability.NewRedisHealthCheck("cache", redisClient)
health.RegisterCheck("redis", redisCheck)
```

### HTTP Health Check
```go
httpCheck := observability.NewHTTPHealthCheck("api_service", "https://api.example.com/health", http.StatusOK)
health.RegisterCheck("external_api", httpCheck)
```

### File System Health Check
```go
fsCheck := observability.NewFileSystemHealthCheck("storage", "/data")
health.RegisterCheck("filesystem", fsCheck)
```

### Memory Health Check
```go
memCheck := observability.NewMemoryHealthCheck("memory", 1<<30) // 1GB threshold
health.RegisterCheck("memory", memCheck)
```

### Disk Space Health Check
```go
diskCheck := observability.NewDiskSpaceHealthCheck("disk", "/", 1<<30) // 1GB minimum
health.RegisterCheck("disk", diskCheck)
```

## Instrumentation Helpers

### HTTP Instrumentation
```go
// Automatic HTTP instrumentation
instrumentation := observability.NewHTTPInstrumentation(metrics)
handler := instrumentation.Middleware()(yourHandler)

// Manual instrumentation
instrumentedHandler := instrumentation.InstrumentHandler("api_handler", yourHandler)
```

### Database Instrumentation
```go
// Database instrumentation
dbInstrumentation := observability.NewDatabaseInstrumentation(metrics)
finishFunc := dbInstrumentation.InstrumentSQL("SELECT * FROM users WHERE id = ?")
defer finishFunc(err)

// Connection pool metrics
poolMetrics := dbInstrumentation.PoolMetrics("primary")
poolMetrics.SetMaxOpenConnections(100)
poolMetrics.SetOpenConnections(50)
```

### Cache Instrumentation
```go
// Cache instrumentation
cacheInstrumentation := observability.NewCacheInstrumentation(metrics)
cacheInstrumentation.Hit("user_cache")
cacheInstrumentation.Miss("user_cache")
cacheInstrumentation.ObserveDuration("user_cache", "get", 50*time.Millisecond)
```

## Monitoring and Alerting

### Prometheus Queries

```promql
# HTTP request rate
rate(http_requests_total[5m])

# HTTP error rate
rate(http_requests_total{status_code=~"5.."}[5m]) / rate(http_requests_total[5m])

# Database connection pool usage
database_connections{state="in_use"} / database_connections{state="max_open"}

# Cache hit rate
rate(cache_hits_total[5m]) / (rate(cache_hits_total[5m]) + rate(cache_misses_total[5m]))
```

### Grafana Dashboard

The module provides metrics that work well with standard Grafana dashboards:

- **RED Metrics**: Rate, Errors, Duration for HTTP requests
- **USE Metrics**: Utilization, Saturation, Errors for resources
- **Database Metrics**: Connection pool stats, query performance
- **Cache Metrics**: Hit ratios, operation counts
- **System Metrics**: Memory, CPU, disk usage

### Alerting Rules

```yaml
groups:
  - name: application
    rules:
      - alert: HighErrorRate
        expr: rate(http_requests_total{status_code=~"5.."}[5m]) > 0.1
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "High error rate detected"
          
      - alert: HighResponseTime
        expr: histogram_quantile(0.95, rate(http_request_duration_seconds_bucket[5m])) > 1
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "High response time detected"
```

## Best Practices

### Tracing
- Use meaningful span names that describe the operation
- Add relevant attributes to spans for better observability
- Keep sampling rates reasonable for production (1-10%)
- Use span events for important milestones within operations
- Always set span status for error conditions

### Metrics
- Use consistent naming conventions for metrics
- Include relevant labels but avoid high cardinality
- Use histograms for timing and size measurements
- Use counters for event counts
- Use gauges for current state values

### Health Checks
- Keep health checks lightweight and fast
- Include checks for all critical dependencies
- Use different endpoints for liveness vs readiness
- Implement circuit breakers for external dependencies
- Return detailed error information for debugging

### Performance
- Configure appropriate batch sizes for trace exports
- Use sampling to reduce overhead in high-traffic applications
- Consider metric collection intervals based on your needs
- Implement health check timeouts to prevent hanging
- Monitor the observability infrastructure itself

## Integration with Kubernetes

### Deployment Configuration

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: my-app
spec:
  template:
    spec:
      containers:
      - name: app
        image: my-app:latest
        ports:
        - containerPort: 8080
        - containerPort: 9090
        livenessProbe:
          httpGet:
            path: /health/live
            port: 8080
          initialDelaySeconds: 30
          periodSeconds: 10
        readinessProbe:
          httpGet:
            path: /health/ready
            port: 8080
          initialDelaySeconds: 5
          periodSeconds: 5
        env:
        - name: FORGE_TRACING_ENABLED
          value: "true"
        - name: FORGE_METRICS_ENABLED
          value: "true"
```

### Service Monitor for Prometheus

```yaml
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: my-app
spec:
  selector:
    matchLabels:
      app: my-app
  endpoints:
  - port: metrics
    path: /metrics
    interval: 15s
```

## Troubleshooting

### Common Issues

1. **High cardinality metrics**: Avoid using user IDs or other high-cardinality values as labels
2. **Trace export failures**: Check network connectivity and exporter configuration
3. **Health check timeouts**: Ensure health checks complete within the configured timeout
4. **Memory usage**: Monitor batch sizes and sampling rates in high-traffic applications

### Debug Mode

Enable debug logging to troubleshoot issues:

```go
tracingConfig := observability.TracingConfig{
    Enabled: true,
    // ... other config
    Exporters: []observability.ExporterConfig{
        {Type: "stdout"}, // Debug output to stdout
    },
}
```

## Contributing

When adding new features to the observability module:

1. Follow the existing interface patterns
2. Add comprehensive tests
3. Update documentation
4. Consider backward compatibility
5. Add examples for new functionality