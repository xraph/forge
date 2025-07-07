package jobs

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/xraph/forge/logger"
)

// JobUtils provides utility functions for job operations
type JobUtils struct {
	logger logger.Logger
}

// NewJobUtils creates a new job utils instance
func NewJobUtils(logger logger.Logger) *JobUtils {
	return &JobUtils{
		logger: logger.Named("job-utils"),
	}
}

// GenerateJobID generates a unique job ID
func GenerateJobID() string {
	// Generate a random 16-byte ID and encode as hex
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		// Fallback to timestamp-based ID
		return fmt.Sprintf("job_%d_%d", time.Now().UnixNano(), time.Now().Nanosecond()%1000000)
	}
	return fmt.Sprintf("job_%s", hex.EncodeToString(bytes))
}

// GenerateScheduleID generates a unique schedule ID
func GenerateScheduleID() string {
	// Generate a random 16-byte ID and encode as hex
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		// Fallback to timestamp-based ID
		return fmt.Sprintf("schedule_%d_%d", time.Now().UnixNano(), time.Now().Nanosecond()%1000000)
	}
	return fmt.Sprintf("schedule_%s", hex.EncodeToString(bytes))
}

// ValidateJob validates a job's required fields and constraints
func (u *JobUtils) ValidateJob(job Job) error {
	if job.Type == "" {
		return fmt.Errorf("job type is required")
	}

	if job.Queue == "" {
		return fmt.Errorf("job queue is required")
	}

	if job.MaxRetries < 0 {
		return fmt.Errorf("max retries cannot be negative")
	}

	if job.Timeout <= 0 {
		return fmt.Errorf("timeout must be positive")
	}

	if job.Priority < -100 || job.Priority > 100 {
		return fmt.Errorf("priority must be between -100 and 100")
	}

	// Validate payload can be serialized
	if job.Payload != nil {
		if _, err := json.Marshal(job.Payload); err != nil {
			return fmt.Errorf("job payload is not serializable: %w", err)
		}
	}

	// Validate metadata can be serialized
	if job.Metadata != nil {
		if _, err := json.Marshal(job.Metadata); err != nil {
			return fmt.Errorf("job metadata is not serializable: %w", err)
		}
	}

	return nil
}

// ValidateSchedule validates a schedule's required fields and constraints
func (u *JobUtils) ValidateSchedule(schedule Schedule) error {
	if schedule.Name == "" {
		return fmt.Errorf("schedule name is required")
	}

	if schedule.JobType == "" {
		return fmt.Errorf("job type is required")
	}

	if schedule.CronExpression == "" {
		return fmt.Errorf("cron expression is required")
	}

	// Validate cron expression format (basic validation)
	if err := u.validateCronExpression(schedule.CronExpression); err != nil {
		return fmt.Errorf("invalid cron expression: %w", err)
	}

	// Validate timezone
	if schedule.Timezone != "" {
		if _, err := time.LoadLocation(schedule.Timezone); err != nil {
			return fmt.Errorf("invalid timezone: %w", err)
		}
	}

	// Validate time constraints
	if schedule.StartTime != nil && schedule.EndTime != nil {
		if schedule.EndTime.Before(*schedule.StartTime) {
			return fmt.Errorf("end time must be after start time")
		}
	}

	if schedule.MaxRuns != nil && *schedule.MaxRuns <= 0 {
		return fmt.Errorf("max runs must be positive")
	}

	// Validate payload can be serialized
	if schedule.JobPayload != nil {
		if _, err := json.Marshal(schedule.JobPayload); err != nil {
			return fmt.Errorf("schedule job payload is not serializable: %w", err)
		}
	}

	return nil
}

// validateCronExpression performs basic validation of cron expressions
func (u *JobUtils) validateCronExpression(expr string) error {
	// Basic cron expression validation
	parts := strings.Fields(expr)
	if len(parts) < 5 || len(parts) > 6 {
		return fmt.Errorf("cron expression must have 5 or 6 fields")
	}

	// Define valid ranges for each field
	ranges := []struct {
		name     string
		min, max int
	}{
		{"second", 0, 59}, // optional seconds field
		{"minute", 0, 59},
		{"hour", 0, 23},
		{"day", 1, 31},
		{"month", 1, 12},
		{"weekday", 0, 6},
	}

	start := 0
	if len(parts) == 6 {
		// Include seconds field validation
		start = 0
	} else {
		// Skip seconds field validation
		start = 1
		ranges = ranges[1:]
	}

	fmt.Println(start)

	for i, part := range parts {
		if err := u.validateCronField(part, ranges[i].name, ranges[i].min, ranges[i].max); err != nil {
			return err
		}
	}

	return nil
}

// validateCronField validates a single field in a cron expression
func (u *JobUtils) validateCronField(field, name string, min, max int) error {
	// Handle special characters
	if field == "*" || field == "?" {
		return nil
	}

	// Handle ranges (e.g., "1-5")
	if strings.Contains(field, "-") {
		parts := strings.Split(field, "-")
		if len(parts) != 2 {
			return fmt.Errorf("invalid range in %s field: %s", name, field)
		}

		start, err := strconv.Atoi(parts[0])
		if err != nil {
			return fmt.Errorf("invalid start value in %s range: %s", name, parts[0])
		}

		end, err := strconv.Atoi(parts[1])
		if err != nil {
			return fmt.Errorf("invalid end value in %s range: %s", name, parts[1])
		}

		if start < min || start > max || end < min || end > max {
			return fmt.Errorf("%s range values must be between %d and %d", name, min, max)
		}

		if start >= end {
			return fmt.Errorf("start value must be less than end value in %s range", name)
		}

		return nil
	}

	// Handle step values (e.g., "*/5")
	if strings.Contains(field, "/") {
		parts := strings.Split(field, "/")
		if len(parts) != 2 {
			return fmt.Errorf("invalid step in %s field: %s", name, field)
		}

		step, err := strconv.Atoi(parts[1])
		if err != nil {
			return fmt.Errorf("invalid step value in %s field: %s", name, parts[1])
		}

		if step <= 0 {
			return fmt.Errorf("step value must be positive in %s field", name)
		}

		// Validate the base part (before /)
		if parts[0] != "*" {
			return u.validateCronField(parts[0], name, min, max)
		}

		return nil
	}

	// Handle comma-separated lists (e.g., "1,3,5")
	if strings.Contains(field, ",") {
		values := strings.Split(field, ",")
		for _, value := range values {
			if err := u.validateCronField(strings.TrimSpace(value), name, min, max); err != nil {
				return err
			}
		}
		return nil
	}

	// Handle single numeric values
	value, err := strconv.Atoi(field)
	if err != nil {
		return fmt.Errorf("invalid %s value: %s", name, field)
	}

	if value < min || value > max {
		return fmt.Errorf("%s value must be between %d and %d", name, min, max)
	}

	return nil
}

// JobPayloadExtractor extracts and validates job payload fields
type JobPayloadExtractor struct {
	payload map[string]interface{}
}

// NewJobPayloadExtractor creates a new payload extractor
func NewJobPayloadExtractor(payload map[string]interface{}) *JobPayloadExtractor {
	return &JobPayloadExtractor{payload: payload}
}

// GetString extracts a string value from the payload
func (e *JobPayloadExtractor) GetString(key string) (string, error) {
	value, exists := e.payload[key]
	if !exists {
		return "", fmt.Errorf("required field '%s' not found", key)
	}

	str, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("field '%s' is not a string", key)
	}

	return str, nil
}

// GetStringOptional extracts an optional string value from the payload
func (e *JobPayloadExtractor) GetStringOptional(key, defaultValue string) string {
	if value, err := e.GetString(key); err == nil {
		return value
	}
	return defaultValue
}

// GetInt extracts an integer value from the payload
func (e *JobPayloadExtractor) GetInt(key string) (int, error) {
	value, exists := e.payload[key]
	if !exists {
		return 0, fmt.Errorf("required field '%s' not found", key)
	}

	switch v := value.(type) {
	case int:
		return v, nil
	case float64:
		return int(v), nil
	case string:
		if i, err := strconv.Atoi(v); err == nil {
			return i, nil
		}
		return 0, fmt.Errorf("field '%s' cannot be converted to int", key)
	default:
		return 0, fmt.Errorf("field '%s' is not a number", key)
	}
}

// GetIntOptional extracts an optional integer value from the payload
func (e *JobPayloadExtractor) GetIntOptional(key string, defaultValue int) int {
	if value, err := e.GetInt(key); err == nil {
		return value
	}
	return defaultValue
}

// GetBool extracts a boolean value from the payload
func (e *JobPayloadExtractor) GetBool(key string) (bool, error) {
	value, exists := e.payload[key]
	if !exists {
		return false, fmt.Errorf("required field '%s' not found", key)
	}

	switch v := value.(type) {
	case bool:
		return v, nil
	case string:
		return strconv.ParseBool(v)
	default:
		return false, fmt.Errorf("field '%s' is not a boolean", key)
	}
}

// GetBoolOptional extracts an optional boolean value from the payload
func (e *JobPayloadExtractor) GetBoolOptional(key string, defaultValue bool) bool {
	if value, err := e.GetBool(key); err == nil {
		return value
	}
	return defaultValue
}

// GetTime extracts a time value from the payload
func (e *JobPayloadExtractor) GetTime(key string) (time.Time, error) {
	value, exists := e.payload[key]
	if !exists {
		return time.Time{}, fmt.Errorf("required field '%s' not found", key)
	}

	switch v := value.(type) {
	case string:
		// Try multiple time formats
		formats := []string{
			time.RFC3339,
			time.RFC3339Nano,
			"2006-01-02T15:04:05",
			"2006-01-02 15:04:05",
			"2006-01-02",
		}

		for _, format := range formats {
			if t, err := time.Parse(format, v); err == nil {
				return t, nil
			}
		}
		return time.Time{}, fmt.Errorf("field '%s' has invalid time format", key)
	case float64:
		// Assume Unix timestamp
		return time.Unix(int64(v), 0), nil
	default:
		return time.Time{}, fmt.Errorf("field '%s' is not a valid time", key)
	}
}

// GetDuration extracts a duration value from the payload
func (e *JobPayloadExtractor) GetDuration(key string) (time.Duration, error) {
	value, exists := e.payload[key]
	if !exists {
		return 0, fmt.Errorf("required field '%s' not found", key)
	}

	switch v := value.(type) {
	case string:
		return time.ParseDuration(v)
	case float64:
		// Assume seconds
		return time.Duration(v) * time.Second, nil
	case int:
		// Assume seconds
		return time.Duration(v) * time.Second, nil
	default:
		return 0, fmt.Errorf("field '%s' is not a valid duration", key)
	}
}

// GetSlice extracts a slice value from the payload
func (e *JobPayloadExtractor) GetSlice(key string) ([]interface{}, error) {
	value, exists := e.payload[key]
	if !exists {
		return nil, fmt.Errorf("required field '%s' not found", key)
	}

	slice, ok := value.([]interface{})
	if !ok {
		return nil, fmt.Errorf("field '%s' is not a slice", key)
	}

	return slice, nil
}

// GetStringSlice extracts a string slice value from the payload
func (e *JobPayloadExtractor) GetStringSlice(key string) ([]string, error) {
	slice, err := e.GetSlice(key)
	if err != nil {
		return nil, err
	}

	strings := make([]string, len(slice))
	for i, item := range slice {
		str, ok := item.(string)
		if !ok {
			return nil, fmt.Errorf("field '%s' contains non-string values", key)
		}
		strings[i] = str
	}

	return strings, nil
}

// GetMap extracts a map value from the payload
func (e *JobPayloadExtractor) GetMap(key string) (map[string]interface{}, error) {
	value, exists := e.payload[key]
	if !exists {
		return nil, fmt.Errorf("required field '%s' not found", key)
	}

	m, ok := value.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("field '%s' is not a map", key)
	}

	return m, nil
}

// Validate checks if all required fields are present
func (e *JobPayloadExtractor) Validate(requiredFields []string) error {
	for _, field := range requiredFields {
		if _, exists := e.payload[field]; !exists {
			return fmt.Errorf("required field '%s' is missing", field)
		}
	}
	return nil
}

// Has checks if a field exists in the payload
func (e *JobPayloadExtractor) Has(key string) bool {
	_, exists := e.payload[key]
	return exists
}

// JobMetrics provides metrics and monitoring utilities for jobs
type JobMetrics struct {
	logger         logger.Logger
	jobCounts      map[string]int64
	executionTimes map[string][]time.Duration
	errorRates     map[string]float64
}

// NewJobMetrics creates a new job metrics collector
func NewJobMetrics(logger logger.Logger) *JobMetrics {
	return &JobMetrics{
		logger:         logger.Named("job-metrics"),
		jobCounts:      make(map[string]int64),
		executionTimes: make(map[string][]time.Duration),
		errorRates:     make(map[string]float64),
	}
}

// RecordJobExecution records a job execution
func (m *JobMetrics) RecordJobExecution(jobType string, duration time.Duration, success bool) {
	m.jobCounts[jobType]++

	if m.executionTimes[jobType] == nil {
		m.executionTimes[jobType] = make([]time.Duration, 0)
	}

	m.executionTimes[jobType] = append(m.executionTimes[jobType], duration)

	// Keep only the last 100 execution times
	if len(m.executionTimes[jobType]) > 100 {
		m.executionTimes[jobType] = m.executionTimes[jobType][len(m.executionTimes[jobType])-100:]
	}

	// Calculate error rate (simplified)
	if !success {
		m.errorRates[jobType] = (m.errorRates[jobType]*float64(m.jobCounts[jobType]-1) + 1) / float64(m.jobCounts[jobType])
	} else {
		m.errorRates[jobType] = (m.errorRates[jobType] * float64(m.jobCounts[jobType]-1)) / float64(m.jobCounts[jobType])
	}
}

// GetAverageExecutionTime returns the average execution time for a job type
func (m *JobMetrics) GetAverageExecutionTime(jobType string) time.Duration {
	times := m.executionTimes[jobType]
	if len(times) == 0 {
		return 0
	}

	var total time.Duration
	for _, t := range times {
		total += t
	}

	return total / time.Duration(len(times))
}

// GetJobCount returns the total count of executions for a job type
func (m *JobMetrics) GetJobCount(jobType string) int64 {
	return m.jobCounts[jobType]
}

// GetErrorRate returns the error rate for a job type
func (m *JobMetrics) GetErrorRate(jobType string) float64 {
	return m.errorRates[jobType]
}

// JobTypeValidator validates job types
type JobTypeValidator struct {
	allowedTypes map[string]bool
	pattern      *regexp.Regexp
}

// NewJobTypeValidator creates a new job type validator
func NewJobTypeValidator(allowedTypes []string, pattern string) (*JobTypeValidator, error) {
	validator := &JobTypeValidator{
		allowedTypes: make(map[string]bool),
	}

	for _, t := range allowedTypes {
		validator.allowedTypes[t] = true
	}

	if pattern != "" {
		regex, err := regexp.Compile(pattern)
		if err != nil {
			return nil, fmt.Errorf("invalid job type pattern: %w", err)
		}
		validator.pattern = regex
	}

	return validator, nil
}

// Validate validates a job type
func (v *JobTypeValidator) Validate(jobType string) error {
	// Check if type is in allowed list (if specified)
	if len(v.allowedTypes) > 0 {
		if !v.allowedTypes[jobType] {
			return fmt.Errorf("job type '%s' is not allowed", jobType)
		}
	}

	// Check if type matches pattern (if specified)
	if v.pattern != nil {
		if !v.pattern.MatchString(jobType) {
			return fmt.Errorf("job type '%s' does not match required pattern", jobType)
		}
	}

	return nil
}

// JobSanitizer sanitizes job data
type JobSanitizer struct {
	logger logger.Logger
}

// NewJobSanitizer creates a new job sanitizer
func NewJobSanitizer(logger logger.Logger) *JobSanitizer {
	return &JobSanitizer{
		logger: logger.Named("job-sanitizer"),
	}
}

// SanitizeJob sanitizes a job's data
func (s *JobSanitizer) SanitizeJob(job *Job) {
	// Sanitize job type
	job.Type = strings.TrimSpace(job.Type)
	job.Queue = strings.TrimSpace(job.Queue)

	// Ensure defaults
	if job.Queue == "" {
		job.Queue = "default"
	}

	if job.Timeout <= 0 {
		job.Timeout = 30 * time.Second
	}

	if job.MaxRetries < 0 {
		job.MaxRetries = 3
	}

	// Sanitize payload
	if job.Payload != nil {
		s.sanitizeMap(job.Payload)
	}

	// Sanitize metadata
	if job.Metadata != nil {
		s.sanitizeMap(job.Metadata)
	}

	// Sanitize tags
	job.Tags = s.sanitizeStringSlice(job.Tags)
}

// sanitizeMap recursively sanitizes a map
func (s *JobSanitizer) sanitizeMap(m map[string]interface{}) {
	for key, value := range m {
		switch v := value.(type) {
		case string:
			m[key] = strings.TrimSpace(v)
		case map[string]interface{}:
			s.sanitizeMap(v)
		case []interface{}:
			for i, item := range v {
				if str, ok := item.(string); ok {
					v[i] = strings.TrimSpace(str)
				}
			}
		}
	}
}

// sanitizeStringSlice sanitizes a string slice
func (s *JobSanitizer) sanitizeStringSlice(slice []string) []string {
	if slice == nil {
		return nil
	}

	result := make([]string, 0, len(slice))
	for _, str := range slice {
		trimmed := strings.TrimSpace(str)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}

	return result
}

// JobCloner provides deep cloning of job objects
type JobCloner struct{}

// NewJobCloner creates a new job cloner
func NewJobCloner() *JobCloner {
	return &JobCloner{}
}

// CloneJob creates a deep copy of a job
func (c *JobCloner) CloneJob(job Job) Job {
	// Create a new job with copied fields
	cloned := Job{
		ID:          job.ID,
		Type:        job.Type,
		Queue:       job.Queue,
		Priority:    job.Priority,
		MaxRetries:  job.MaxRetries,
		Timeout:     job.Timeout,
		Delay:       job.Delay,
		Status:      job.Status,
		Attempts:    job.Attempts,
		LastError:   job.LastError,
		Result:      job.Result,
		ProcessedBy: job.ProcessedBy,
		CreatedAt:   job.CreatedAt,
		UpdatedAt:   job.UpdatedAt,
		ScheduleID:  job.ScheduleID,
	}

	// Deep copy time pointers
	if job.RunAt != nil {
		runAt := *job.RunAt
		cloned.RunAt = &runAt
	}

	if job.StartedAt != nil {
		startedAt := *job.StartedAt
		cloned.StartedAt = &startedAt
	}

	if job.CompletedAt != nil {
		completedAt := *job.CompletedAt
		cloned.CompletedAt = &completedAt
	}

	if job.FailedAt != nil {
		failedAt := *job.FailedAt
		cloned.FailedAt = &failedAt
	}

	// Deep copy payload
	if job.Payload != nil {
		cloned.Payload = c.cloneMap(job.Payload)
	}

	// Deep copy metadata
	if job.Metadata != nil {
		cloned.Metadata = c.cloneMap(job.Metadata)
	}

	// Deep copy tags
	if job.Tags != nil {
		cloned.Tags = make([]string, len(job.Tags))
		copy(cloned.Tags, job.Tags)
	}

	// Deep copy dependencies
	if job.Dependencies != nil {
		cloned.Dependencies = make([]string, len(job.Dependencies))
		copy(cloned.Dependencies, job.Dependencies)
	}

	if job.DependsOn != nil {
		cloned.DependsOn = make([]string, len(job.DependsOn))
		copy(cloned.DependsOn, job.DependsOn)
	}

	// Deep copy retry policy
	if job.RetryPolicy != nil {
		retryPolicy := *job.RetryPolicy
		cloned.RetryPolicy = &retryPolicy
	}

	return cloned
}

// cloneMap creates a deep copy of a map
func (c *JobCloner) cloneMap(original map[string]interface{}) map[string]interface{} {
	clone := make(map[string]interface{})

	for key, value := range original {
		clone[key] = c.cloneValue(value)
	}

	return clone
}

// cloneValue creates a deep copy of an interface{} value
func (c *JobCloner) cloneValue(original interface{}) interface{} {
	if original == nil {
		return nil
	}

	originalValue := reflect.ValueOf(original)

	switch originalValue.Kind() {
	case reflect.Map:
		clone := make(map[string]interface{})
		for _, key := range originalValue.MapKeys() {
			clone[key.String()] = c.cloneValue(originalValue.MapIndex(key).Interface())
		}
		return clone

	case reflect.Slice:
		clone := make([]interface{}, originalValue.Len())
		for i := 0; i < originalValue.Len(); i++ {
			clone[i] = c.cloneValue(originalValue.Index(i).Interface())
		}
		return clone

	default:
		// For basic types, return the value directly (they are copied by value)
		return original
	}
}

// JobFormatter provides formatting utilities for jobs
type JobFormatter struct{}

// NewJobFormatter creates a new job formatter
func NewJobFormatter() *JobFormatter {
	return &JobFormatter{}
}

// FormatDuration formats a duration in a human-readable way
func (f *JobFormatter) FormatDuration(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}

	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}

	if d < time.Hour {
		return fmt.Sprintf("%.1fm", d.Minutes())
	}

	return fmt.Sprintf("%.1fh", d.Hours())
}

// FormatJobStatus formats a job status with color codes (for terminal output)
func (f *JobFormatter) FormatJobStatus(status JobStatus) string {
	switch status {
	case JobStatusCompleted:
		return fmt.Sprintf("\033[32m%s\033[0m", status) // Green
	case JobStatusFailed:
		return fmt.Sprintf("\033[31m%s\033[0m", status) // Red
	case JobStatusRunning:
		return fmt.Sprintf("\033[33m%s\033[0m", status) // Yellow
	case JobStatusCancelled:
		return fmt.Sprintf("\033[35m%s\033[0m", status) // Magenta
	default:
		return string(status)
	}
}

// FormatJobSummary creates a one-line summary of a job
func (f *JobFormatter) FormatJobSummary(job Job) string {
	status := f.FormatJobStatus(job.Status)
	duration := ""

	if job.StartedAt != nil && job.CompletedAt != nil {
		duration = fmt.Sprintf(" (%s)", f.FormatDuration(job.CompletedAt.Sub(*job.StartedAt)))
	}

	return fmt.Sprintf("[%s] %s/%s %s%s", job.ID[:8], job.Queue, job.Type, status, duration)
}

// DefaultJobHandlerOptions provides default options for job handlers
type DefaultJobHandlerOptions struct {
	Timeout     time.Duration
	RetryPolicy RetryPolicy
	Logger      logger.Logger
}

// NewDefaultJobHandlerOptions creates default job handler options
func NewDefaultJobHandlerOptions(logger logger.Logger) DefaultJobHandlerOptions {
	return DefaultJobHandlerOptions{
		Timeout:     30 * time.Second,
		RetryPolicy: DefaultRetryPolicy(),
		Logger:      logger,
	}
}

// WithTimeout sets the timeout for job handlers
func (o DefaultJobHandlerOptions) WithTimeout(timeout time.Duration) DefaultJobHandlerOptions {
	o.Timeout = timeout
	return o
}

// WithRetryPolicy sets the retry policy for job handlers
func (o DefaultJobHandlerOptions) WithRetryPolicy(policy RetryPolicy) DefaultJobHandlerOptions {
	o.RetryPolicy = policy
	return o
}

// Common job types constants
const (
	JobTypeEmail     = "send_email"
	JobTypeSMS       = "send_sms"
	JobTypeReport    = "generate_report"
	JobTypeCleanup   = "cleanup_data"
	JobTypeBackup    = "backup_data"
	JobTypeSync      = "sync_data"
	JobTypeWebhook   = "send_webhook"
	JobTypeImage     = "process_image"
	JobTypeVideo     = "process_video"
	JobTypeImport    = "import_data"
	JobTypeExport    = "export_data"
	JobTypeAnalytics = "compute_analytics"
)

// Common queue names constants
const (
	QueueDefault    = "default"
	QueueCritical   = "critical"
	QueueBackground = "background"
	QueueEmail      = "email"
	QueueReports    = "reports"
	QueueMedia      = "media"
	QueueData       = "data"
)

// Helper functions for common operations

// IsTerminalStatus checks if a job status is terminal (won't change)
func IsTerminalStatus(status JobStatus) bool {
	return status == JobStatusCompleted ||
		status == JobStatusFailed ||
		status == JobStatusCancelled
}

// IsActiveStatus checks if a job status indicates an active job
func IsActiveStatus(status JobStatus) bool {
	return status == JobStatusPending ||
		status == JobStatusQueued ||
		status == JobStatusRunning ||
		status == JobStatusRetrying
}

// IsErrorStatus checks if a job status indicates an error
func IsErrorStatus(status JobStatus) bool {
	return status == JobStatusFailed ||
		status == JobStatusCancelled ||
		status == JobStatusDeadLetter
}

// GetStatusPriority returns a priority value for sorting job statuses
func GetStatusPriority(status JobStatus) int {
	priorities := map[JobStatus]int{
		JobStatusRunning:    0,
		JobStatusFailed:     1,
		JobStatusRetrying:   2,
		JobStatusQueued:     3,
		JobStatusPending:    4,
		JobStatusScheduled:  5,
		JobStatusCancelled:  6,
		JobStatusCompleted:  7,
		JobStatusDeadLetter: 8,
	}

	if priority, exists := priorities[status]; exists {
		return priority
	}

	return 9 // Unknown status gets lowest priority
}

// CalculateRetryDelay calculates the delay for a retry attempt
func CalculateRetryDelay(attempt int, policy RetryPolicy) time.Duration {
	if attempt <= 0 {
		return policy.InitialInterval
	}

	delay := policy.InitialInterval
	for i := 1; i < attempt; i++ {
		delay = time.Duration(float64(delay) * policy.Multiplier)
		if delay > policy.MaxInterval {
			delay = policy.MaxInterval
			break
		}
	}

	// Add jitter if configured
	if policy.RandomFactor > 0 {
		// Simple jitter calculation (in production, use proper random)
		maxJitter := time.Duration(float64(delay) * policy.RandomFactor)
		jitter := maxJitter / 2 // Simplified jitter
		delay += jitter
	}

	return delay
}

// ShouldRetry determines if a job should be retried based on the retry policy
func ShouldRetry(attempts int, policy RetryPolicy) bool {
	return attempts < policy.MaxRetries
}

// EstimateQueueWaitTime estimates wait time based on queue size and processing rate
func EstimateQueueWaitTime(queueSize int64, processingRate float64) time.Duration {
	if processingRate <= 0 {
		return 0
	}

	waitSeconds := float64(queueSize) / processingRate
	return time.Duration(waitSeconds * float64(time.Second))
}

// GetJobPriorityDescription returns a human-readable description of job priority
func GetJobPriorityDescription(priority int) string {
	switch {
	case priority >= 50:
		return "Critical"
	case priority >= 25:
		return "High"
	case priority >= 0:
		return "Normal"
	case priority >= -25:
		return "Low"
	default:
		return "Background"
	}
}
