package internal

import (
	"context"
	"slices"
	"sync"
	"time"
)

// HealthAggregator aggregates health check results into an overall health status.
type HealthAggregator struct {
	criticalServices   map[string]bool
	degradedThreshold  float64
	unhealthyThreshold float64
	enableDependencies bool
	dependencyGraph    map[string][]string
	weights            map[string]float64
	mu                 sync.RWMutex
}

// AggregatorConfig contains configuration for the health aggregator.
type AggregatorConfig struct {
	CriticalServices   []string
	DegradedThreshold  float64 // Percentage of services that can be degraded before overall is degraded
	UnhealthyThreshold float64 // Percentage of services that can be unhealthy before overall is unhealthy
	EnableDependencies bool
	Weights            map[string]float64 // Weight for each service (default 1.0)
}

// DefaultAggregatorConfig returns default configuration for the health aggregator.
func DefaultAggregatorConfig() *AggregatorConfig {
	return &AggregatorConfig{
		CriticalServices:   []string{},
		DegradedThreshold:  0.1,  // 10% of services can be degraded
		UnhealthyThreshold: 0.05, // 5% of services can be unhealthy
		EnableDependencies: true,
		Weights:            make(map[string]float64),
	}
}

// NewHealthAggregator creates a new health aggregator.
func NewHealthAggregator(config *AggregatorConfig) *HealthAggregator {
	if config == nil {
		config = DefaultAggregatorConfig()
	}

	criticalServices := make(map[string]bool)
	for _, service := range config.CriticalServices {
		criticalServices[service] = true
	}

	return &HealthAggregator{
		criticalServices:   criticalServices,
		degradedThreshold:  config.DegradedThreshold,
		unhealthyThreshold: config.UnhealthyThreshold,
		enableDependencies: config.EnableDependencies,
		dependencyGraph:    make(map[string][]string),
		weights:            config.Weights,
	}
}

// AddCriticalService adds a service to the critical services list.
func (ha *HealthAggregator) AddCriticalService(serviceName string) {
	ha.mu.Lock()
	defer ha.mu.Unlock()

	ha.criticalServices[serviceName] = true
}

// RemoveCriticalService removes a service from the critical services list.
func (ha *HealthAggregator) RemoveCriticalService(serviceName string) {
	ha.mu.Lock()
	defer ha.mu.Unlock()

	delete(ha.criticalServices, serviceName)
}

// IsCriticalService returns true if the service is marked as critical.
func (ha *HealthAggregator) IsCriticalService(serviceName string) bool {
	ha.mu.RLock()
	defer ha.mu.RUnlock()

	return ha.criticalServices[serviceName]
}

// SetDependency sets a dependency relationship between services.
func (ha *HealthAggregator) SetDependency(service string, dependencies []string) {
	ha.mu.Lock()
	defer ha.mu.Unlock()

	ha.dependencyGraph[service] = dependencies
}

// GetDependencies returns the dependencies for a service.
func (ha *HealthAggregator) GetDependencies(service string) []string {
	ha.mu.RLock()
	defer ha.mu.RUnlock()

	return ha.dependencyGraph[service]
}

// SetWeight sets the weight for a service.
func (ha *HealthAggregator) SetWeight(serviceName string, weight float64) {
	ha.mu.Lock()
	defer ha.mu.Unlock()

	ha.weights[serviceName] = weight
}

// GetWeight returns the weight for a service (default 1.0).
func (ha *HealthAggregator) GetWeight(serviceName string) float64 {
	ha.mu.RLock()
	defer ha.mu.RUnlock()

	if weight, exists := ha.weights[serviceName]; exists {
		return weight
	}

	return 1.0
}

// Aggregate aggregates multiple health results into an overall health status.
func (ha *HealthAggregator) Aggregate(results map[string]*HealthResult) *HealthReport {
	ha.mu.RLock()
	defer ha.mu.RUnlock()

	report := NewHealthReport()

	if len(results) == 0 {
		report.Overall = HealthStatusUnknown

		return report
	}

	// Add all results to the report using the map key as the canonical name.
	// This ensures the report reflects the registered check name
	// even if result.Name was not set by the check function.
	for name, result := range results {
		if result.Name == "" {
			result.Name = name
		}
		report.Services[name] = result
	}
	report.CalculateStats()

	// Calculate overall status
	overall := ha.calculateOverallStatus(results)
	report.Overall = overall

	return report
}

// calculateOverallStatus calculates the overall health status based on individual results.
func (ha *HealthAggregator) calculateOverallStatus(results map[string]*HealthResult) HealthStatus {
	// First check critical services
	if ha.hasCriticalFailures(results) {
		return HealthStatusUnhealthy
	}

	// Check dependency failures
	if ha.enableDependencies && ha.hasDependencyFailures(results) {
		return HealthStatusUnhealthy
	}

	// Calculate weighted status
	return ha.calculateWeightedStatus(results)
}

// hasCriticalFailures checks if any critical service has failed.
func (ha *HealthAggregator) hasCriticalFailures(results map[string]*HealthResult) bool {
	for serviceName, result := range results {
		if ha.criticalServices[serviceName] && result.IsUnhealthy() {
			return true
		}
	}

	return false
}

// hasDependencyFailures checks for dependency-related failures.
func (ha *HealthAggregator) hasDependencyFailures(results map[string]*HealthResult) bool {
	for serviceName, result := range results {
		if result.IsUnhealthy() {
			// Check if any service depends on this failed service
			for _, dependencies := range ha.dependencyGraph {
				if slices.Contains(dependencies, serviceName) {
					return true
				}
			}
		}
	}

	return false
}

// calculateWeightedStatus calculates the overall status based on weighted results.
func (ha *HealthAggregator) calculateWeightedStatus(results map[string]*HealthResult) HealthStatus {
	var (
		totalWeight     float64
		healthyWeight   float64
		degradedWeight  float64
		unhealthyWeight float64
	)

	for serviceName, result := range results {
		weight := ha.GetWeight(serviceName)
		totalWeight += weight

		switch result.Status {
		case HealthStatusHealthy:
			healthyWeight += weight
		case HealthStatusDegraded:
			degradedWeight += weight
		case HealthStatusUnhealthy:
			unhealthyWeight += weight
		}
	}

	if totalWeight == 0 {
		return HealthStatusUnknown
	}

	// Calculate percentages
	unhealthyPercentage := unhealthyWeight / totalWeight
	degradedPercentage := degradedWeight / totalWeight

	// Determine overall status based on thresholds
	if unhealthyPercentage > ha.unhealthyThreshold {
		return HealthStatusUnhealthy
	}

	if degradedPercentage > ha.degradedThreshold {
		return HealthStatusDegraded
	}

	// If there are any unhealthy services but below threshold, consider degraded
	if unhealthyWeight > 0 {
		return HealthStatusDegraded
	}

	// If there are any degraded services, overall is degraded
	if degradedWeight > 0 {
		return HealthStatusDegraded
	}

	return HealthStatusHealthy
}

// AggregateWithContext aggregates health results with context information.
func (ha *HealthAggregator) AggregateWithContext(ctx context.Context, results map[string]*HealthResult) *HealthReport {
	report := ha.Aggregate(results)

	// Add context information
	report.WithVersion(getVersionFromContext(ctx))
	report.WithEnvironment(getEnvironmentFromContext(ctx))
	report.WithHostname(getHostnameFromContext(ctx))
	report.WithUptime(getUptimeFromContext(ctx))

	return report
}

// SmartAggregator implements advanced aggregation logic with machine learning-like features.
type SmartAggregator struct {
	*HealthAggregator

	history            []HealthStatusSnapshot
	maxHistorySize     int
	trendsEnabled      bool
	adaptiveThresholds bool
	mu                 sync.RWMutex
}

// HealthStatusSnapshot represents a point-in-time health status.
type HealthStatusSnapshot struct {
	Timestamp time.Time
	Overall   HealthStatus
	Services  map[string]HealthStatus
}

// NewSmartAggregator creates a new smart health aggregator.
func NewSmartAggregator(config *AggregatorConfig) *SmartAggregator {
	return &SmartAggregator{
		HealthAggregator:   NewHealthAggregator(config),
		history:            make([]HealthStatusSnapshot, 0),
		maxHistorySize:     100,
		trendsEnabled:      true,
		adaptiveThresholds: true,
	}
}

// SetMaxHistorySize sets the maximum number of snapshots to keep in history.
func (sa *SmartAggregator) SetMaxHistorySize(size int) {
	sa.mu.Lock()
	defer sa.mu.Unlock()

	sa.maxHistorySize = size
}

// EnableTrends enables/disables trend analysis.
func (sa *SmartAggregator) EnableTrends(enabled bool) {
	sa.mu.Lock()
	defer sa.mu.Unlock()

	sa.trendsEnabled = enabled
}

// EnableAdaptiveThresholds enables/disables adaptive thresholds.
func (sa *SmartAggregator) EnableAdaptiveThresholds(enabled bool) {
	sa.mu.Lock()
	defer sa.mu.Unlock()

	sa.adaptiveThresholds = enabled
}

// Aggregate aggregates health results with smart analysis.
func (sa *SmartAggregator) Aggregate(results map[string]*HealthResult) *HealthReport {
	sa.mu.Lock()
	defer sa.mu.Unlock()

	// Perform basic aggregation
	report := sa.HealthAggregator.Aggregate(results)

	// Add snapshot to history
	snapshot := HealthStatusSnapshot{
		Timestamp: time.Now(),
		Overall:   report.Overall,
		Services:  make(map[string]HealthStatus),
	}

	for serviceName, result := range results {
		snapshot.Services[serviceName] = result.Status
	}

	sa.addSnapshot(snapshot)

	// Apply smart analysis
	if sa.trendsEnabled {
		sa.analyzetrends(report)
	}

	if sa.adaptiveThresholds {
		sa.adaptThresholds(results)
	}

	return report
}

// addSnapshot adds a snapshot to the history.
func (sa *SmartAggregator) addSnapshot(snapshot HealthStatusSnapshot) {
	sa.history = append(sa.history, snapshot)

	// Trim history if needed
	if len(sa.history) > sa.maxHistorySize {
		sa.history = sa.history[1:]
	}
}

// analyzetrends analyzes health trends and adjusts status accordingly.
func (sa *SmartAggregator) analyzetrends(report *HealthReport) {
	if len(sa.history) < 3 {
		return // Not enough history for trend analysis
	}

	// Analyze recent trend
	recent := sa.history[len(sa.history)-3:]
	deteriorating := true
	improving := true

	for i := 1; i < len(recent); i++ {
		if recent[i].Overall.Severity() <= recent[i-1].Overall.Severity() {
			deteriorating = false
		}

		if recent[i].Overall.Severity() >= recent[i-1].Overall.Severity() {
			improving = false
		}
	}

	// Adjust status based on trends
	if deteriorating && report.Overall == HealthStatusHealthy {
		report.Overall = HealthStatusDegraded
		report.WithMetadata(map[string]any{
			"trend_adjustment": "degraded_due_to_deteriorating_trend",
		})
	} else if improving && report.Overall == HealthStatusDegraded {
		// Keep degraded status but add trend information
		report.WithMetadata(map[string]any{
			"trend_info": "improving_trend_detected",
		})
	}
}

// adaptThresholds adapts thresholds based on historical data.
func (sa *SmartAggregator) adaptThresholds(results map[string]*HealthResult) {
	if len(sa.history) < 10 {
		return // Not enough history for adaptation
	}

	// Calculate average failure rates
	totalChecks := len(sa.history)
	failureRate := 0.0

	for _, snapshot := range sa.history {
		if snapshot.Overall == HealthStatusUnhealthy {
			failureRate += 1.0
		}
	}

	failureRate /= float64(totalChecks)

	// Adapt thresholds based on failure rate
	if failureRate > 0.1 { // High failure rate
		sa.unhealthyThreshold = minFloat(sa.unhealthyThreshold*1.1, 0.2)
		sa.degradedThreshold = minFloat(sa.degradedThreshold*1.1, 0.3)
	} else if failureRate < 0.01 { // Low failure rate
		sa.unhealthyThreshold = maxFloat(sa.unhealthyThreshold*0.9, 0.01)
		sa.degradedThreshold = maxFloat(sa.degradedThreshold*0.9, 0.05)
	}
}

// GetTrends returns health trends over time.
func (sa *SmartAggregator) GetTrends() []HealthStatusSnapshot {
	sa.mu.RLock()
	defer sa.mu.RUnlock()

	trends := make([]HealthStatusSnapshot, len(sa.history))
	copy(trends, sa.history)

	return trends
}

// GetServiceTrends returns trends for a specific service.
func (sa *SmartAggregator) GetServiceTrends(serviceName string) []HealthStatusSnapshot {
	sa.mu.RLock()
	defer sa.mu.RUnlock()

	var trends []HealthStatusSnapshot

	for _, snapshot := range sa.history {
		if status, exists := snapshot.Services[serviceName]; exists {
			trends = append(trends, HealthStatusSnapshot{
				Timestamp: snapshot.Timestamp,
				Overall:   status,
				Services:  map[string]HealthStatus{serviceName: status},
			})
		}
	}

	return trends
}

// GetStabilityScore returns a stability score (0-1) based on recent health history.
func (sa *SmartAggregator) GetStabilityScore() float64 {
	sa.mu.RLock()
	defer sa.mu.RUnlock()

	if len(sa.history) < 2 {
		return 0.0
	}

	// Calculate stability based on status changes
	changes := 0

	for i := 1; i < len(sa.history); i++ {
		if sa.history[i].Overall != sa.history[i-1].Overall {
			changes++
		}
	}

	return 1.0 - float64(changes)/float64(len(sa.history)-1)
}

// PredictiveAggregator implements predictive health aggregation.
type PredictiveAggregator struct {
	*SmartAggregator

	predictionWindow   time.Duration
	predictionAccuracy float64
}

// NewPredictiveAggregator creates a new predictive health aggregator.
func NewPredictiveAggregator(config *AggregatorConfig) *PredictiveAggregator {
	return &PredictiveAggregator{
		SmartAggregator:    NewSmartAggregator(config),
		predictionWindow:   5 * time.Minute,
		predictionAccuracy: 0.8,
	}
}

// PredictHealth predicts the health status for the next prediction window.
func (pa *PredictiveAggregator) PredictHealth() *HealthPrediction {
	pa.mu.RLock()
	defer pa.mu.RUnlock()

	if len(pa.history) < 5 {
		return &HealthPrediction{
			PredictedStatus: HealthStatusUnknown,
			Confidence:      0.0,
			TimeWindow:      pa.predictionWindow,
		}
	}

	// Simple trend-based prediction
	recent := pa.history[len(pa.history)-5:]

	// Calculate trend
	healthyCount := 0
	degradedCount := 0
	unhealthyCount := 0

	for _, snapshot := range recent {
		switch snapshot.Overall {
		case HealthStatusHealthy:
			healthyCount++
		case HealthStatusDegraded:
			degradedCount++
		case HealthStatusUnhealthy:
			unhealthyCount++
		}
	}

	// Predict based on majority
	var (
		predictedStatus HealthStatus
		confidence      float64
	)

	if healthyCount >= degradedCount && healthyCount >= unhealthyCount { //nolint:gocritic // ifElseChain: health status comparison clearer with if-else
		predictedStatus = HealthStatusHealthy
		confidence = float64(healthyCount) / float64(len(recent))
	} else if degradedCount >= unhealthyCount {
		predictedStatus = HealthStatusDegraded
		confidence = float64(degradedCount) / float64(len(recent))
	} else {
		predictedStatus = HealthStatusUnhealthy
		confidence = float64(unhealthyCount) / float64(len(recent))
	}

	return &HealthPrediction{
		PredictedStatus: predictedStatus,
		Confidence:      confidence * pa.predictionAccuracy,
		TimeWindow:      pa.predictionWindow,
		Recommendations: pa.generateRecommendations(recent),
	}
}

// generateRecommendations generates recommendations based on health history.
func (pa *PredictiveAggregator) generateRecommendations(history []HealthStatusSnapshot) []string {
	var recommendations []string

	// Analyze patterns
	if len(history) >= 3 {
		// Check for recurring patterns
		if history[len(history)-1].Overall == HealthStatusUnhealthy &&
			history[len(history)-2].Overall == HealthStatusDegraded {
			recommendations = append(recommendations, "Consider scaling up resources before degradation leads to complete failure")
		}

		// Check for flapping
		changes := 0

		for i := 1; i < len(history); i++ {
			if history[i].Overall != history[i-1].Overall {
				changes++
			}
		}

		if changes >= len(history)-1 {
			recommendations = append(recommendations, "Health status is flapping - investigate intermittent issues")
		}
	}

	return recommendations
}

// HealthPrediction represents a health prediction.
type HealthPrediction struct {
	PredictedStatus HealthStatus  `json:"predicted_status"`
	Confidence      float64       `json:"confidence"`
	TimeWindow      time.Duration `json:"time_window"`
	Recommendations []string      `json:"recommendations"`
}

// Helper functions for context extraction.
func getVersionFromContext(ctx context.Context) string {
	if version := ctx.Value("version"); version != nil {
		if v, ok := version.(string); ok {
			return v
		}
	}

	return "unknown"
}

func getEnvironmentFromContext(ctx context.Context) string {
	if env := ctx.Value("environment"); env != nil {
		if e, ok := env.(string); ok {
			return e
		}
	}

	return "unknown"
}

func getHostnameFromContext(ctx context.Context) string {
	if hostname := ctx.Value("hostname"); hostname != nil {
		if h, ok := hostname.(string); ok {
			return h
		}
	}

	return "unknown"
}

func getUptimeFromContext(ctx context.Context) time.Duration {
	if uptime := ctx.Value("uptime"); uptime != nil {
		if u, ok := uptime.(time.Duration); ok {
			return u
		}
	}

	return 0
}

// Helper functions.
func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}

	return b
}

func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}

	return b
}
