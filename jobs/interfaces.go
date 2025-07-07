package jobs

import (
	"context"
	"fmt"
	"time"
)

// Processor represents the main job processing interface
type Processor interface {
	// Job management
	Enqueue(ctx context.Context, job Job) error
	EnqueueIn(ctx context.Context, job Job, delay time.Duration) error
	EnqueueAt(ctx context.Context, job Job, at time.Time) error
	EnqueueBatch(ctx context.Context, jobs []Job) error

	// Job execution
	Process(ctx context.Context) error
	ProcessQueue(ctx context.Context, queueName string) error

	// Handler registration
	RegisterHandler(jobType string, handler JobHandler) error
	UnregisterHandler(jobType string) error

	// Queue management
	CreateQueue(name string, config QueueConfig) error
	DeleteQueue(name string) error
	PauseQueue(name string) error
	ResumeQueue(name string) error
	PurgeQueue(name string) error

	// Job control
	GetJob(ctx context.Context, jobID string) (*JobInfo, error)
	CancelJob(ctx context.Context, jobID string) error
	RetryJob(ctx context.Context, jobID string) error
	DeleteJob(ctx context.Context, jobID string) error

	// Statistics and monitoring
	GetStats(ctx context.Context) (*Stats, error)
	GetQueueStats(ctx context.Context, queueName string) (*QueueStats, error)

	// Lifecycle
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	Shutdown(ctx context.Context) error

	// Health check
	Health(ctx context.Context) error
}

// Queue represents a job queue interface
type Queue interface {
	// Queue operations
	Name() string
	Size(ctx context.Context) (int64, error)
	Push(ctx context.Context, job Job) error
	Pop(ctx context.Context) (*Job, error)
	Peek(ctx context.Context) (*Job, error)

	// Batch operations
	PushBatch(ctx context.Context, jobs []Job) error
	PopBatch(ctx context.Context, count int) ([]Job, error)

	// Queue control
	Pause(ctx context.Context) error
	Resume(ctx context.Context) error
	Clear(ctx context.Context) error
	IsPaused(ctx context.Context) bool

	// Dead letter queue
	GetDeadJobs(ctx context.Context) ([]Job, error)
	RequeueDeadJob(ctx context.Context, jobID string) error
	PurgeDeadJobs(ctx context.Context) error

	// Statistics
	Stats(ctx context.Context) (*QueueStats, error)

	// Lifecycle
	Close() error
}

// Scheduler represents a job scheduler interface
type Scheduler interface {
	// Schedule management
	Schedule(ctx context.Context, schedule Schedule) error
	Unschedule(ctx context.Context, scheduleID string) error
	UpdateSchedule(ctx context.Context, scheduleID string, schedule Schedule) error

	// Schedule control
	EnableSchedule(ctx context.Context, scheduleID string) error
	DisableSchedule(ctx context.Context, scheduleID string) error

	// Schedule queries
	GetSchedule(ctx context.Context, scheduleID string) (*ScheduleInfo, error)
	ListSchedules(ctx context.Context) ([]ScheduleInfo, error)
	GetNextRun(ctx context.Context, scheduleID string) (*time.Time, error)

	// Execution history
	GetExecutionHistory(ctx context.Context, scheduleID string, limit int) ([]ExecutionHistory, error)

	// Lifecycle
	Start(ctx context.Context) error
	Stop(ctx context.Context) error

	// Health check
	Health(ctx context.Context) error
}

// JobHandler represents a job handler interface
type JobHandler interface {
	Handle(ctx context.Context, job Job) (interface{}, error)
	CanHandle(jobType string) bool
	GetTimeout() time.Duration
	GetRetryPolicy() RetryPolicy
}

// JobHandlerFunc is a function adapter for JobHandler
type JobHandlerFunc func(ctx context.Context, job Job) (interface{}, error)

func (f JobHandlerFunc) Handle(ctx context.Context, job Job) (interface{}, error) {
	return f(ctx, job)
}

func (f JobHandlerFunc) CanHandle(jobType string) bool {
	return true
}

func (f JobHandlerFunc) GetTimeout() time.Duration {
	return 30 * time.Second
}

func (f JobHandlerFunc) GetRetryPolicy() RetryPolicy {
	return DefaultRetryPolicy()
}

// Storage represents job storage interface
type Storage interface {
	// Job CRUD
	SaveJob(ctx context.Context, job Job) error
	GetJob(ctx context.Context, jobID string) (*Job, error)
	UpdateJob(ctx context.Context, job Job) error
	DeleteJob(ctx context.Context, jobID string) error

	// Job queries
	GetJobsByStatus(ctx context.Context, status JobStatus, limit int) ([]Job, error)
	GetJobsByQueue(ctx context.Context, queueName string, limit int) ([]Job, error)
	GetExpiredJobs(ctx context.Context) ([]Job, error)
	GetRetryableJobs(ctx context.Context) ([]Job, error)

	// Queue operations
	EnqueueJob(ctx context.Context, queueName string, job Job) error
	DequeueJob(ctx context.Context, queueName string) (*Job, error)
	GetQueueSize(ctx context.Context, queueName string) (int64, error)

	// Statistics
	GetStats(ctx context.Context) (*StorageStats, error)
	GetQueueStats(ctx context.Context, queueName string) (*QueueStats, error)

	// Cleanup
	CleanupJobs(ctx context.Context, olderThan time.Time) error
	CleanupQueue(ctx context.Context, queueName string) error

	// Lifecycle
	Close() error
}

// Core data types

// Job represents a job to be processed
type Job struct {
	ID       string                 `json:"id"`
	Type     string                 `json:"type"`
	Queue    string                 `json:"queue"`
	Payload  map[string]interface{} `json:"payload"`
	Priority int                    `json:"priority"`

	// Execution settings
	MaxRetries int           `json:"max_retries"`
	Timeout    time.Duration `json:"timeout"`
	Delay      time.Duration `json:"delay,omitempty"`

	// Scheduling
	RunAt      *time.Time `json:"run_at,omitempty"`
	ScheduleID string     `json:"schedule_id,omitempty"`

	// State
	Status      JobStatus `json:"status"`
	Attempts    int       `json:"attempts"`
	LastError   string    `json:"last_error,omitempty"`
	Result      string    `json:"result,omitempty"`
	ProcessedBy string    `json:"processed_by,omitempty"`

	// Timestamps
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	StartedAt   *time.Time `json:"started_at,omitempty"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	FailedAt    *time.Time `json:"failed_at,omitempty"`

	// Metadata
	Tags     []string               `json:"tags,omitempty"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`

	// Retry policy
	RetryPolicy *RetryPolicy `json:"retry_policy,omitempty"`

	// Dependencies
	Dependencies []string `json:"dependencies,omitempty"`
	DependsOn    []string `json:"depends_on,omitempty"`
}

// JobStatus represents the status of a job
type JobStatus string

const (
	JobStatusPending    JobStatus = "pending"
	JobStatusQueued     JobStatus = "queued"
	JobStatusRunning    JobStatus = "running"
	JobStatusCompleted  JobStatus = "completed"
	JobStatusFailed     JobStatus = "failed"
	JobStatusCancelled  JobStatus = "cancelled"
	JobStatusRetrying   JobStatus = "retrying"
	JobStatusScheduled  JobStatus = "scheduled"
	JobStatusDeadLetter JobStatus = "dead_letter"
)

// RetryPolicy defines how jobs should be retried
type RetryPolicy struct {
	MaxRetries      int           `json:"max_retries"`
	InitialInterval time.Duration `json:"initial_interval"`
	MaxInterval     time.Duration `json:"max_interval"`
	Multiplier      float64       `json:"multiplier"`
	RandomFactor    float64       `json:"random_factor,omitempty"`
}

// DefaultRetryPolicy returns a default retry policy
func DefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{
		MaxRetries:      3,
		InitialInterval: 1 * time.Second,
		MaxInterval:     300 * time.Second,
		Multiplier:      2.0,
		RandomFactor:    0.1,
	}
}

// Schedule represents a job schedule
type Schedule struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	JobType     string                 `json:"job_type"`
	JobPayload  map[string]interface{} `json:"job_payload"`
	Queue       string                 `json:"queue"`

	// Schedule configuration
	CronExpression string        `json:"cron_expression"`
	Timezone       string        `json:"timezone,omitempty"`
	Enabled        bool          `json:"enabled"`
	StartTime      *time.Time    `json:"start_time,omitempty"`
	EndTime        *time.Time    `json:"end_time,omitempty"`
	MaxRuns        *int          `json:"max_runs,omitempty"`
	Timeout        time.Duration `json:"timeout,omitempty"`

	// Retry configuration
	RetryPolicy *RetryPolicy `json:"retry_policy,omitempty"`

	// State
	LastRun    *time.Time `json:"last_run,omitempty"`
	NextRun    *time.Time `json:"next_run,omitempty"`
	RunCount   int        `json:"run_count"`
	ErrorCount int        `json:"error_count"`
	LastError  string     `json:"last_error,omitempty"`

	// Timestamps
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	// Metadata
	Tags     []string               `json:"tags,omitempty"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// Configuration types

// Config represents job system configuration
type Config struct {
	Enabled     bool                   `mapstructure:"enabled" yaml:"enabled" env:"FORGE_JOBS_ENABLED"`
	Backend     string                 `mapstructure:"backend" yaml:"backend" env:"FORGE_JOBS_BACKEND"`
	Concurrency int                    `mapstructure:"concurrency" yaml:"concurrency" env:"FORGE_JOBS_CONCURRENCY"`
	Queues      map[string]QueueConfig `mapstructure:"queues" yaml:"queues"`
	Storage     StorageConfig          `mapstructure:"storage" yaml:"storage"`
	Scheduler   SchedulerConfig        `mapstructure:"scheduler" yaml:"scheduler"`
	RetryPolicy RetryPolicy            `mapstructure:"retry_policy" yaml:"retry_policy"`
	Monitoring  MonitoringConfig       `mapstructure:"monitoring" yaml:"monitoring"`
}

// QueueConfig represents queue configuration
type QueueConfig struct {
	Workers     int           `mapstructure:"workers" yaml:"workers"`
	Priority    int           `mapstructure:"priority" yaml:"priority"`
	MaxSize     int64         `mapstructure:"max_size" yaml:"max_size"`
	Timeout     time.Duration `mapstructure:"timeout" yaml:"timeout"`
	RetryPolicy *RetryPolicy  `mapstructure:"retry_policy" yaml:"retry_policy,omitempty"`
	DeadLetter  bool          `mapstructure:"dead_letter" yaml:"dead_letter"`
}

// StorageConfig represents storage configuration
type StorageConfig struct {
	Type       string            `mapstructure:"type" yaml:"type"` // memory, redis, postgres, mysql
	URL        string            `mapstructure:"url" yaml:"url"`
	Database   string            `mapstructure:"database" yaml:"database"`
	TableName  string            `mapstructure:"table_name" yaml:"table_name"`
	KeyPrefix  string            `mapstructure:"key_prefix" yaml:"key_prefix"`
	TTL        time.Duration     `mapstructure:"ttl" yaml:"ttl"`
	CleanupTTL time.Duration     `mapstructure:"cleanup_ttl" yaml:"cleanup_ttl"`
	Options    map[string]string `mapstructure:"options" yaml:"options"`
}

// SchedulerConfig represents scheduler configuration
type SchedulerConfig struct {
	Enabled          bool          `mapstructure:"enabled" yaml:"enabled"`
	TickInterval     time.Duration `mapstructure:"tick_interval" yaml:"tick_interval"`
	MaxConcurrent    int           `mapstructure:"max_concurrent" yaml:"max_concurrent"`
	LockTimeout      time.Duration `mapstructure:"lock_timeout" yaml:"lock_timeout"`
	HistoryRetention time.Duration `mapstructure:"history_retention" yaml:"history_retention"`
}

// MonitoringConfig represents monitoring configuration
type MonitoringConfig struct {
	Enabled        bool          `mapstructure:"enabled" yaml:"enabled"`
	MetricsEnabled bool          `mapstructure:"metrics_enabled" yaml:"metrics_enabled"`
	StatsInterval  time.Duration `mapstructure:"stats_interval" yaml:"stats_interval"`
	HealthCheck    bool          `mapstructure:"health_check" yaml:"health_check"`
}

// Information and statistics types

// JobInfo represents detailed job information
type JobInfo struct {
	Job
	ExecutionHistory []ExecutionAttempt `json:"execution_history,omitempty"`
	QueuePosition    *int               `json:"queue_position,omitempty"`
	EstimatedRunTime *time.Time         `json:"estimated_run_time,omitempty"`
}

// ExecutionAttempt represents a job execution attempt
type ExecutionAttempt struct {
	Attempt     int                    `json:"attempt"`
	StartedAt   time.Time              `json:"started_at"`
	CompletedAt *time.Time             `json:"completed_at,omitempty"`
	Duration    time.Duration          `json:"duration"`
	Status      JobStatus              `json:"status"`
	Error       string                 `json:"error,omitempty"`
	Result      interface{}            `json:"result,omitempty"`
	ProcessedBy string                 `json:"processed_by"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// Stats represents overall job system statistics
type Stats struct {
	TotalJobs       int64                     `json:"total_jobs"`
	QueuedJobs      int64                     `json:"queued_jobs"`
	RunningJobs     int64                     `json:"running_jobs"`
	CompletedJobs   int64                     `json:"completed_jobs"`
	FailedJobs      int64                     `json:"failed_jobs"`
	DeadLetterJobs  int64                     `json:"dead_letter_jobs"`
	ActiveWorkers   int                       `json:"active_workers"`
	TotalWorkers    int                       `json:"total_workers"`
	Queues          map[string]*QueueStats    `json:"queues"`
	Schedules       map[string]*ScheduleStats `json:"schedules"`
	ProcessingRate  float64                   `json:"processing_rate"` // jobs per second
	AverageWaitTime time.Duration             `json:"average_wait_time"`
	Uptime          time.Duration             `json:"uptime"`
	LastUpdated     time.Time                 `json:"last_updated"`
}

// QueueStats represents queue-specific statistics
type QueueStats struct {
	Name           string        `json:"name"`
	Size           int64         `json:"size"`
	Workers        int           `json:"workers"`
	ActiveWorkers  int           `json:"active_workers"`
	PendingJobs    int64         `json:"pending_jobs"`
	RunningJobs    int64         `json:"running_jobs"`
	CompletedJobs  int64         `json:"completed_jobs"`
	FailedJobs     int64         `json:"failed_jobs"`
	DeadLetterJobs int64         `json:"dead_letter_jobs"`
	ProcessingRate float64       `json:"processing_rate"`
	AverageWait    time.Duration `json:"average_wait_time"`
	IsPaused       bool          `json:"is_paused"`
	LastActivity   time.Time     `json:"last_activity"`
}

// ScheduleInfo represents detailed schedule information
type ScheduleInfo struct {
	Schedule
	NextExecutions []time.Time        `json:"next_executions,omitempty"`
	RecentRuns     []ExecutionHistory `json:"recent_runs,omitempty"`
	IsRunning      bool               `json:"is_running"`
}

// ScheduleStats represents schedule-specific statistics
type ScheduleStats struct {
	ID             string        `json:"id"`
	Name           string        `json:"name"`
	Enabled        bool          `json:"enabled"`
	TotalRuns      int64         `json:"total_runs"`
	SuccessfulRuns int64         `json:"successful_runs"`
	FailedRuns     int64         `json:"failed_runs"`
	LastRun        *time.Time    `json:"last_run,omitempty"`
	NextRun        *time.Time    `json:"next_run,omitempty"`
	AverageRunTime time.Duration `json:"average_run_time"`
	LastError      string        `json:"last_error,omitempty"`
	IsOverdue      bool          `json:"is_overdue"`
}

// ExecutionHistory represents schedule execution history
type ExecutionHistory struct {
	ScheduleID  string                 `json:"schedule_id"`
	JobID       string                 `json:"job_id"`
	StartedAt   time.Time              `json:"started_at"`
	CompletedAt *time.Time             `json:"completed_at,omitempty"`
	Duration    time.Duration          `json:"duration"`
	Status      JobStatus              `json:"status"`
	Error       string                 `json:"error,omitempty"`
	Result      interface{}            `json:"result,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// StorageStats represents storage-specific statistics
type StorageStats struct {
	Backend        string            `json:"backend"`
	TotalJobs      int64             `json:"total_jobs"`
	ActiveJobs     int64             `json:"active_jobs"`
	CompletedJobs  int64             `json:"completed_jobs"`
	FailedJobs     int64             `json:"failed_jobs"`
	StorageSize    int64             `json:"storage_size"` // in bytes
	ConnectionPool map[string]int    `json:"connection_pool,omitempty"`
	Performance    map[string]string `json:"performance,omitempty"`
}

// Worker represents a job worker
type Worker interface {
	// Worker identification
	ID() string
	Queue() string
	Status() WorkerStatus

	// Worker control
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	Pause() error
	Resume() error

	// Job processing
	ProcessNext(ctx context.Context) error
	ProcessJob(ctx context.Context, job Job) error

	// Worker state
	CurrentJob() *Job
	IsIdle() bool
	IsProcessing() bool

	// Statistics
	Stats() WorkerStats
}

// WorkerStatus represents worker status
type WorkerStatus string

const (
	WorkerStatusIdle       WorkerStatus = "idle"
	WorkerStatusProcessing WorkerStatus = "processing"
	WorkerStatusPaused     WorkerStatus = "paused"
	WorkerStatusStopped    WorkerStatus = "stopped"
	WorkerStatusError      WorkerStatus = "error"
)

// WorkerStats represents worker statistics
type WorkerStats struct {
	ID             string        `json:"id"`
	Queue          string        `json:"queue"`
	Status         WorkerStatus  `json:"status"`
	ProcessedJobs  int64         `json:"processed_jobs"`
	FailedJobs     int64         `json:"failed_jobs"`
	AverageRunTime time.Duration `json:"average_run_time"`
	CurrentJob     *Job          `json:"current_job,omitempty"`
	LastActivity   time.Time     `json:"last_activity"`
	StartedAt      time.Time     `json:"started_at"`
	TotalUptime    time.Duration `json:"total_uptime"`
	ProcessingTime time.Duration `json:"processing_time"`
	IdleTime       time.Duration `json:"idle_time"`
}

// Event types for job system events

// Event represents a job system event
type Event struct {
	Type      EventType              `json:"type"`
	JobID     string                 `json:"job_id,omitempty"`
	Queue     string                 `json:"queue,omitempty"`
	WorkerID  string                 `json:"worker_id,omitempty"`
	Data      map[string]interface{} `json:"data,omitempty"`
	Timestamp time.Time              `json:"timestamp"`
	Error     string                 `json:"error,omitempty"`
}

// EventType represents the type of job event
type EventType string

const (
	EventJobEnqueued   EventType = "job.enqueued"
	EventJobStarted    EventType = "job.started"
	EventJobCompleted  EventType = "job.completed"
	EventJobFailed     EventType = "job.failed"
	EventJobRetried    EventType = "job.retried"
	EventJobCancelled  EventType = "job.cancelled"
	EventJobTimeout    EventType = "job.timeout"
	EventWorkerStarted EventType = "worker.started"
	EventWorkerStopped EventType = "worker.stopped"
	EventWorkerError   EventType = "worker.error"
	EventQueuePaused   EventType = "queue.paused"
	EventQueueResumed  EventType = "queue.resumed"
)

// EventHandler handles job system events
type EventHandler interface {
	HandleEvent(ctx context.Context, event Event) error
	CanHandle(eventType EventType) bool
}

// EventBus manages job system events
type EventBus interface {
	Subscribe(eventType EventType, handler EventHandler) error
	Unsubscribe(eventType EventType, handler EventHandler) error
	Publish(ctx context.Context, event Event) error
	PublishAsync(ctx context.Context, event Event) error
}

// Error types
var (
	ErrJobNotFound         = fmt.Errorf("job not found")
	ErrJobAlreadyExists    = fmt.Errorf("job already exists")
	ErrQueueNotFound       = fmt.Errorf("queue not found")
	ErrQueueFull           = fmt.Errorf("queue is full")
	ErrQueuePaused         = fmt.Errorf("queue is paused")
	ErrHandlerNotFound     = fmt.Errorf("handler not found")
	ErrInvalidJobType      = fmt.Errorf("invalid job type")
	ErrInvalidCron         = fmt.Errorf("invalid cron expression")
	ErrScheduleNotFound    = fmt.Errorf("schedule not found")
	ErrWorkerBusy          = fmt.Errorf("worker is busy")
	ErrTimeout             = fmt.Errorf("job timeout")
	ErrCancelled           = fmt.Errorf("job cancelled")
	ErrRetryExhausted      = fmt.Errorf("retry attempts exhausted")
	ErrInvalidConfig       = fmt.Errorf("invalid configuration")
	ErrStorageUnavailable  = fmt.Errorf("storage unavailable")
	ErrProcessorNotStarted = fmt.Errorf("processor not started")
)

// Factory functions

// NewJob creates a new job with default values
func NewJob(jobType string, payload map[string]interface{}) Job {
	return Job{
		ID:          generateJobID(),
		Type:        jobType,
		Queue:       "default",
		Payload:     payload,
		Priority:    0,
		MaxRetries:  3,
		Timeout:     30 * time.Second,
		Status:      JobStatusPending,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
		RetryPolicy: &[]RetryPolicy{DefaultRetryPolicy()}[0],
	}
}

// NewSchedule creates a new schedule with default values
func NewSchedule(name, jobType, cronExpr string, payload map[string]interface{}) Schedule {
	return Schedule{
		ID:             generateScheduleID(),
		Name:           name,
		JobType:        jobType,
		JobPayload:     payload,
		Queue:          "default",
		CronExpression: cronExpr,
		Enabled:        true,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
		RetryPolicy:    &[]RetryPolicy{DefaultRetryPolicy()}[0],
	}
}

// Utility functions

// generateJobID generates a unique job ID
func generateJobID() string {
	// Implementation would use UUID or similar
	return fmt.Sprintf("job_%d", time.Now().UnixNano())
}

// generateScheduleID generates a unique schedule ID
func generateScheduleID() string {
	// Implementation would use UUID or similar
	return fmt.Sprintf("schedule_%d", time.Now().UnixNano())
}

// JobBuilder provides a fluent interface for building jobs
type JobBuilder struct {
	job Job
}

// NewJobBuilder creates a new job builder
func NewJobBuilder(jobType string) *JobBuilder {
	return &JobBuilder{
		job: NewJob(jobType, make(map[string]interface{})),
	}
}

func (b *JobBuilder) WithPayload(payload map[string]interface{}) *JobBuilder {
	b.job.Payload = payload
	return b
}

func (b *JobBuilder) WithQueue(queue string) *JobBuilder {
	b.job.Queue = queue
	return b
}

func (b *JobBuilder) WithPriority(priority int) *JobBuilder {
	b.job.Priority = priority
	return b
}

func (b *JobBuilder) WithMaxRetries(maxRetries int) *JobBuilder {
	b.job.MaxRetries = maxRetries
	return b
}

func (b *JobBuilder) WithTimeout(timeout time.Duration) *JobBuilder {
	b.job.Timeout = timeout
	return b
}

func (b *JobBuilder) WithDelay(delay time.Duration) *JobBuilder {
	b.job.Delay = delay
	return b
}

func (b *JobBuilder) WithRunAt(runAt time.Time) *JobBuilder {
	b.job.RunAt = &runAt
	return b
}

func (b *JobBuilder) WithTags(tags []string) *JobBuilder {
	b.job.Tags = tags
	return b
}

func (b *JobBuilder) WithMetadata(metadata map[string]interface{}) *JobBuilder {
	b.job.Metadata = metadata
	return b
}

func (b *JobBuilder) WithRetryPolicy(policy RetryPolicy) *JobBuilder {
	b.job.RetryPolicy = &policy
	return b
}

func (b *JobBuilder) Build() Job {
	return b.job
}

// ScheduleBuilder provides a fluent interface for building schedules
type ScheduleBuilder struct {
	schedule Schedule
}

// NewScheduleBuilder creates a new schedule builder
func NewScheduleBuilder(name, jobType, cronExpr string) *ScheduleBuilder {
	return &ScheduleBuilder{
		schedule: NewSchedule(name, jobType, cronExpr, make(map[string]interface{})),
	}
}

func (b *ScheduleBuilder) WithDescription(description string) *ScheduleBuilder {
	b.schedule.Description = description
	return b
}

func (b *ScheduleBuilder) WithPayload(payload map[string]interface{}) *ScheduleBuilder {
	b.schedule.JobPayload = payload
	return b
}

func (b *ScheduleBuilder) WithQueue(queue string) *ScheduleBuilder {
	b.schedule.Queue = queue
	return b
}

func (b *ScheduleBuilder) WithTimezone(timezone string) *ScheduleBuilder {
	b.schedule.Timezone = timezone
	return b
}

func (b *ScheduleBuilder) WithStartTime(startTime time.Time) *ScheduleBuilder {
	b.schedule.StartTime = &startTime
	return b
}

func (b *ScheduleBuilder) WithEndTime(endTime time.Time) *ScheduleBuilder {
	b.schedule.EndTime = &endTime
	return b
}

func (b *ScheduleBuilder) WithMaxRuns(maxRuns int) *ScheduleBuilder {
	b.schedule.MaxRuns = &maxRuns
	return b
}

func (b *ScheduleBuilder) WithTimeout(timeout time.Duration) *ScheduleBuilder {
	b.schedule.Timeout = timeout
	return b
}

func (b *ScheduleBuilder) WithRetryPolicy(policy RetryPolicy) *ScheduleBuilder {
	b.schedule.RetryPolicy = &policy
	return b
}

func (b *ScheduleBuilder) WithTags(tags []string) *ScheduleBuilder {
	b.schedule.Tags = tags
	return b
}

func (b *ScheduleBuilder) WithMetadata(metadata map[string]interface{}) *ScheduleBuilder {
	b.schedule.Metadata = metadata
	return b
}

func (b *ScheduleBuilder) Build() Schedule {
	return b.schedule
}
