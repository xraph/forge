package gateway

import (
	"github.com/xraph/forge"
)

// GatewayMetrics manages Prometheus metrics for the gateway.
type GatewayMetrics struct {
	metrics forge.Metrics
	prefix  string
	enabled bool
}

// NewGatewayMetrics creates a new gateway metrics collector.
func NewGatewayMetrics(metrics forge.Metrics, config MetricsConfig) *GatewayMetrics {
	return &GatewayMetrics{
		metrics: metrics,
		prefix:  config.Prefix,
		enabled: config.Enabled,
	}
}

// RecordRequest records a request metric.
func (gm *GatewayMetrics) RecordRequest(routeID, method, status, upstream string) {
	if !gm.enabled || gm.metrics == nil {
		return
	}

	gm.metrics.Counter(gm.prefix + ".requests_total").Inc()
}

// RecordLatency records request latency.
func (gm *GatewayMetrics) RecordLatency(routeID, method, upstream string, seconds float64) {
	if !gm.enabled || gm.metrics == nil {
		return
	}

	gm.metrics.Histogram(gm.prefix + ".request_duration_seconds").Observe(seconds)
}

// SetActiveConnections sets the active connections gauge.
func (gm *GatewayMetrics) SetActiveConnections(protocol string, count float64) {
	if !gm.enabled || gm.metrics == nil {
		return
	}

	gm.metrics.Gauge(gm.prefix + ".active_connections." + protocol).Set(count)
}

// SetUpstreamHealth sets the upstream health gauge.
func (gm *GatewayMetrics) SetUpstreamHealth(targetID string, healthy bool) {
	if !gm.enabled || gm.metrics == nil {
		return
	}

	val := 0.0
	if healthy {
		val = 1.0
	}

	gm.metrics.Gauge(gm.prefix + ".upstream_health." + targetID).Set(val)
}

// SetCircuitBreakerState sets the circuit breaker state gauge.
func (gm *GatewayMetrics) SetCircuitBreakerState(targetID string, state CircuitState) {
	if !gm.enabled || gm.metrics == nil {
		return
	}

	val := 0.0

	switch state {
	case CircuitClosed:
		val = 0
	case CircuitHalfOpen:
		val = 1
	case CircuitOpen:
		val = 2
	}

	gm.metrics.Gauge(gm.prefix + ".circuit_breaker_state." + targetID).Set(val)
}

// RecordRetry records a retry attempt.
func (gm *GatewayMetrics) RecordRetry(routeID string, attempt int) {
	if !gm.enabled || gm.metrics == nil {
		return
	}

	gm.metrics.Counter(gm.prefix + ".retry_total").Inc()
}

// RecordCacheHit records a cache hit.
func (gm *GatewayMetrics) RecordCacheHit() {
	if !gm.enabled || gm.metrics == nil {
		return
	}

	gm.metrics.Counter(gm.prefix + ".cache_hits_total").Inc()
}

// RecordCacheMiss records a cache miss.
func (gm *GatewayMetrics) RecordCacheMiss() {
	if !gm.enabled || gm.metrics == nil {
		return
	}

	gm.metrics.Counter(gm.prefix + ".cache_misses_total").Inc()
}

// RecordRateLimited records a rate-limited request.
func (gm *GatewayMetrics) RecordRateLimited() {
	if !gm.enabled || gm.metrics == nil {
		return
	}

	gm.metrics.Counter(gm.prefix + ".rate_limited_total").Inc()
}

// SetDiscoveryRoutes sets the count of discovered routes by source.
func (gm *GatewayMetrics) SetDiscoveryRoutes(source string, count float64) {
	if !gm.enabled || gm.metrics == nil {
		return
	}

	gm.metrics.Gauge(gm.prefix + ".discovery_routes." + source).Set(count)
}
