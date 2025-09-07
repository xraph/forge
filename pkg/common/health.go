package common

import (
	"context"
	"time"

	json "github.com/json-iterator/go"
)

// HealthChecker defines the interface for health checking
type HealthChecker interface {
	Name() string
	Dependencies() []string
	OnStart(ctx context.Context) error
	OnStop(ctx context.Context) error
	OnHealthCheck(ctx context.Context) error
	RegisterCheck(name string, check HealthCheck) error
	UnregisterCheck(name string) error
	Check(ctx context.Context) *HealthReport
	CheckAll(ctx context.Context) *HealthReport
	CheckOne(ctx context.Context, name string) *HealthResult
	GetStatus() HealthStatus
	Subscribe(callback HealthCallback) error
	GetLastReport() *HealthReport
	GetChecks() map[string]HealthCheck
	GetStats() *HealthCheckerStats
	SetEnvironment(name string)
	SetVersion(version string)
	SetHostname(hostname string)
	Environment() string
	Hostname() string
	Version() string
	StartTime() time.Time
}

// HealthCheck defines the interface for health checks
type HealthCheck interface {
	Name() string
	Check(ctx context.Context) *HealthResult
	Timeout() time.Duration
	Critical() bool
	Dependencies() []string
}

// HealthStatus represents the health status of a service or component
type HealthStatus string

const (
	HealthStatusHealthy   HealthStatus = "healthy"
	HealthStatusDegraded  HealthStatus = "degraded"
	HealthStatusUnhealthy HealthStatus = "unhealthy"
	HealthStatusUnknown   HealthStatus = "unknown"
)

// String returns the string representation of the health status
func (hs HealthStatus) String() string {
	return string(hs)
}

// IsHealthy returns true if the status is healthy
func (hs HealthStatus) IsHealthy() bool {
	return hs == HealthStatusHealthy
}

// IsDegraded returns true if the status is degraded
func (hs HealthStatus) IsDegraded() bool {
	return hs == HealthStatusDegraded
}

// IsUnhealthy returns true if the status is unhealthy
func (hs HealthStatus) IsUnhealthy() bool {
	return hs == HealthStatusUnhealthy
}

// IsUnknown returns true if the status is unknown
func (hs HealthStatus) IsUnknown() bool {
	return hs == HealthStatusUnknown
}

// Severity returns a numeric severity level for comparison
func (hs HealthStatus) Severity() int {
	switch hs {
	case HealthStatusHealthy:
		return 0
	case HealthStatusDegraded:
		return 1
	case HealthStatusUnhealthy:
		return 2
	case HealthStatusUnknown:
		return 3
	default:
		return 3
	}
}

// HealthResult represents the result of a health check
type HealthResult struct {
	Name      string                 `json:"name"`
	Status    HealthStatus           `json:"status"`
	Message   string                 `json:"message"`
	Details   map[string]interface{} `json:"details"`
	Timestamp time.Time              `json:"timestamp"`
	Duration  time.Duration          `json:"duration"`
	Error     string                 `json:"error,omitempty"`
	Critical  bool                   `json:"critical"`
	Tags      map[string]string      `json:"tags,omitempty"`
}

// NewHealthResult creates a new health result
func NewHealthResult(name string, status HealthStatus, message string) *HealthResult {
	return &HealthResult{
		Name:      name,
		Status:    status,
		Message:   message,
		Details:   make(map[string]interface{}),
		Timestamp: time.Now(),
		Tags:      make(map[string]string),
	}
}

// WithDetails adds details to the health result
func (hr *HealthResult) WithDetails(details map[string]interface{}) *HealthResult {
	for k, v := range details {
		hr.Details[k] = v
	}
	return hr
}

// WithDetail adds a single detail to the health result
func (hr *HealthResult) WithDetail(key string, value interface{}) *HealthResult {
	hr.Details[key] = value
	return hr
}

// WithError adds an error to the health result
func (hr *HealthResult) WithError(err error) *HealthResult {
	if err != nil {
		hr.Error = err.Error()
		if hr.Status == HealthStatusHealthy {
			hr.Status = HealthStatusUnhealthy
		}
	}
	return hr
}

// WithDuration sets the duration of the health check
func (hr *HealthResult) WithDuration(duration time.Duration) *HealthResult {
	hr.Duration = duration
	return hr
}

// WithCritical marks the health check as critical
func (hr *HealthResult) WithCritical(critical bool) *HealthResult {
	hr.Critical = critical
	return hr
}

func (hr *HealthResult) WithStatus(status HealthStatus) *HealthResult {
	hr.Status = status
	return hr
}

func (hr *HealthResult) WithMessage(message string) *HealthResult {
	hr.Message = message
	return hr
}

// WithTags adds tags to the health result
func (hr *HealthResult) WithTags(tags map[string]string) *HealthResult {
	for k, v := range tags {
		hr.Tags[k] = v
	}
	return hr
}

// WithTag adds a single tag to the health result
func (hr *HealthResult) WithTag(key, value string) *HealthResult {
	hr.Tags[key] = value
	return hr
}

func (hr *HealthResult) WithTimestamp(timestamp time.Time) *HealthResult {
	hr.Timestamp = timestamp
	return hr
}

func (hr *HealthResult) WithTimestampNow() *HealthResult {
	hr.Timestamp = time.Now()
	return hr
}

func (hr *HealthResult) String() string {
	return hr.Name + ": " + hr.Status.String() + " - " + hr.Message
}

// IsHealthy returns true if the health result is healthy
func (hr *HealthResult) IsHealthy() bool {
	return hr.Status.IsHealthy()
}

// IsDegraded returns true if the health result is degraded
func (hr *HealthResult) IsDegraded() bool {
	return hr.Status.IsDegraded()
}

// IsUnhealthy returns true if the health result is unhealthy
func (hr *HealthResult) IsUnhealthy() bool {
	return hr.Status.IsUnhealthy()
}

// IsCritical returns true if the health check is critical
func (hr *HealthResult) IsCritical() bool {
	return hr.Critical
}

// HealthReport represents a comprehensive health report
type HealthReport struct {
	Overall     HealthStatus             `json:"overall"`
	Services    map[string]*HealthResult `json:"services"`
	Timestamp   time.Time                `json:"timestamp"`
	Duration    time.Duration            `json:"duration"`
	Version     string                   `json:"version"`
	Environment string                   `json:"environment"`
	Hostname    string                   `json:"hostname"`
	Uptime      time.Duration            `json:"uptime"`
	Metadata    map[string]interface{}   `json:"metadata,omitempty"`
}

// NewHealthReport creates a new health report
func NewHealthReport() *HealthReport {
	return &HealthReport{
		Overall:   HealthStatusUnknown,
		Services:  make(map[string]*HealthResult),
		Timestamp: time.Now(),
		Metadata:  make(map[string]interface{}),
	}
}

// AddResult adds a health result to the report
func (hr *HealthReport) AddResult(result *HealthResult) {
	hr.Services[result.Name] = result
}

// AddResults adds multiple health results to the report
func (hr *HealthReport) AddResults(results []*HealthResult) {
	for _, result := range results {
		hr.AddResult(result)
	}
}

// WithVersion sets the version information
func (hr *HealthReport) WithVersion(version string) *HealthReport {
	hr.Version = version
	return hr
}

// WithEnvironment sets the environment information
func (hr *HealthReport) WithEnvironment(environment string) *HealthReport {
	hr.Environment = environment
	return hr
}

// WithHostname sets the hostname information
func (hr *HealthReport) WithHostname(hostname string) *HealthReport {
	hr.Hostname = hostname
	return hr
}

// WithUptime sets the application uptime
func (hr *HealthReport) WithUptime(uptime time.Duration) *HealthReport {
	hr.Uptime = uptime
	return hr
}

// WithDuration sets the duration of the health check
func (hr *HealthReport) WithDuration(duration time.Duration) *HealthReport {
	hr.Duration = duration
	return hr
}

// WithMetadata adds metadata to the health report
func (hr *HealthReport) WithMetadata(metadata map[string]interface{}) *HealthReport {
	for k, v := range metadata {
		hr.Metadata[k] = v
	}
	return hr
}

// GetHealthyCount returns the number of healthy services
func (hr *HealthReport) GetHealthyCount() int {
	count := 0
	for _, result := range hr.Services {
		if result.IsHealthy() {
			count++
		}
	}
	return count
}

// GetDegradedCount returns the number of degraded services
func (hr *HealthReport) GetDegradedCount() int {
	count := 0
	for _, result := range hr.Services {
		if result.IsDegraded() {
			count++
		}
	}
	return count
}

// GetUnhealthyCount returns the number of unhealthy services
func (hr *HealthReport) GetUnhealthyCount() int {
	count := 0
	for _, result := range hr.Services {
		if result.IsUnhealthy() {
			count++
		}
	}
	return count
}

// GetCriticalCount returns the number of critical services
func (hr *HealthReport) GetCriticalCount() int {
	count := 0
	for _, result := range hr.Services {
		if result.IsCritical() {
			count++
		}
	}
	return count
}

// GetFailedCriticalCount returns the number of failed critical services
func (hr *HealthReport) GetFailedCriticalCount() int {
	count := 0
	for _, result := range hr.Services {
		if result.IsCritical() && result.IsUnhealthy() {
			count++
		}
	}
	return count
}

// GetServicesByStatus returns services filtered by status
func (hr *HealthReport) GetServicesByStatus(status HealthStatus) []*HealthResult {
	var results []*HealthResult
	for _, result := range hr.Services {
		if result.Status == status {
			results = append(results, result)
		}
	}
	return results
}

// GetCriticalServices returns all critical services
func (hr *HealthReport) GetCriticalServices() []*HealthResult {
	var results []*HealthResult
	for _, result := range hr.Services {
		if result.IsCritical() {
			results = append(results, result)
		}
	}
	return results
}

// IsHealthy returns true if the overall health is healthy
func (hr *HealthReport) IsHealthy() bool {
	return hr.Overall.IsHealthy()
}

// IsDegraded returns true if the overall health is degraded
func (hr *HealthReport) IsDegraded() bool {
	return hr.Overall.IsDegraded()
}

// IsUnhealthy returns true if the overall health is unhealthy
func (hr *HealthReport) IsUnhealthy() bool {
	return hr.Overall.IsUnhealthy()
}

// ToJSON converts the health report to JSON
func (hr *HealthReport) ToJSON() ([]byte, error) {
	return json.MarshalIndent(hr, "", "  ")
}

// FromJSON creates a health report from JSON
func FromJSON(data []byte) (*HealthReport, error) {
	var report HealthReport
	err := json.Unmarshal(data, &report)
	return &report, err
}

// Summary returns a summary of the health report
func (hr *HealthReport) Summary() map[string]interface{} {
	return map[string]interface{}{
		"overall":         hr.Overall,
		"total_services":  len(hr.Services),
		"healthy_count":   hr.GetHealthyCount(),
		"degraded_count":  hr.GetDegradedCount(),
		"unhealthy_count": hr.GetUnhealthyCount(),
		"critical_count":  hr.GetCriticalCount(),
		"failed_critical": hr.GetFailedCriticalCount(),
		"timestamp":       hr.Timestamp,
		"duration":        hr.Duration,
		"version":         hr.Version,
		"environment":     hr.Environment,
		"hostname":        hr.Hostname,
		"uptime":          hr.Uptime,
	}
}

// HealthCallback is a callback function for health status changes
type HealthCallback func(result *HealthResult)

// HealthReportCallback is a callback function for health report changes
type HealthReportCallback func(report *HealthReport)

// HealthCheckerStats contains statistics about the health checker
type HealthCheckerStats struct {
	RegisteredChecks int           `json:"registered_checks"`
	Subscribers      int           `json:"subscribers"`
	Started          bool          `json:"started"`
	Uptime           time.Duration `json:"uptime"`
	LastReportTime   time.Time     `json:"last_report_time"`
	OverallStatus    HealthStatus  `json:"overall_status"`
	LastReport       *HealthReport `json:"last_report,omitempty"`
}
