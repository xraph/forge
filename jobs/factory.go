package jobs

import (
	"context"
	"fmt"
	"time"

	"github.com/xraph/forge/logger"
)

// ProcessorFactory creates and configures job processors
type ProcessorFactory struct {
	logger logger.Logger
}

// NewProcessorFactory creates a new processor factory
func NewProcessorFactory(logger logger.Logger) *ProcessorFactory {
	return &ProcessorFactory{
		logger: logger,
	}
}

// CreateProcessor creates a new job processor with the given configuration
func (f *ProcessorFactory) CreateProcessor(config Config) (Processor, error) {
	// Create storage
	storageFactory := NewStorageFactory(f.logger)
	storage, err := storageFactory.CreateStorage(config.Storage)
	if err != nil {
		return nil, fmt.Errorf("failed to create storage: %w", err)
	}

	// Create processor
	processor, err := NewProcessor(config, storage, f.logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create processor: %w", err)
	}

	f.logger.Info("Job processor created",
		logger.String("backend", config.Backend),
		logger.Int("concurrency", config.Concurrency),
		logger.Int("queues", len(config.Queues)),
	)

	return processor, nil
}

// CreateScheduler creates a new job scheduler with the given configuration
func (f *ProcessorFactory) CreateScheduler(config SchedulerConfig, processor Processor) (Scheduler, error) {
	// Create schedule storage
	scheduleStorage := NewMemoryScheduleStorage(f.logger)

	// Create scheduler
	scheduler := NewScheduler(config, processor, scheduleStorage, f.logger)

	f.logger.Info("Job scheduler created",
		logger.Duration("tick_interval", config.TickInterval),
		logger.Int("max_concurrent", config.MaxConcurrent),
	)

	return scheduler, nil
}

// JobSystem represents a complete job processing system
type JobSystem struct {
	processor Processor
	scheduler Scheduler
	eventBus  EventBus
	config    Config
	logger    logger.Logger
	started   bool
}

// NewJobSystem creates a new complete job system
func NewJobSystem(config Config, logger logger.Logger) (*JobSystem, error) {
	factory := NewProcessorFactory(logger)

	// Create processor
	processor, err := factory.CreateProcessor(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create processor: %w", err)
	}

	// Create scheduler if enabled
	var scheduler Scheduler
	if config.Scheduler.Enabled {
		scheduler, err = factory.CreateScheduler(config.Scheduler, processor)
		if err != nil {
			return nil, fmt.Errorf("failed to create scheduler: %w", err)
		}
	}

	// Create event bus
	eventBus := NewEventBus(logger)

	system := &JobSystem{
		processor: processor,
		scheduler: scheduler,
		eventBus:  eventBus,
		config:    config,
		logger:    logger.Named("job-system"),
	}

	// Set up default event handlers if monitoring is enabled
	if config.Monitoring.Enabled {
		system.setupMonitoring()
	}

	return system, nil
}

// Start starts the job system
func (js *JobSystem) Start(ctx context.Context) error {
	if js.started {
		return fmt.Errorf("job system already started")
	}

	// Start processor
	if err := js.processor.Start(ctx); err != nil {
		return fmt.Errorf("failed to start processor: %w", err)
	}

	// Start scheduler if available
	if js.scheduler != nil {
		if err := js.scheduler.Start(ctx); err != nil {
			js.processor.Stop(ctx) // Cleanup processor
			return fmt.Errorf("failed to start scheduler: %w", err)
		}
	}

	js.started = true
	js.logger.Info("Job system started")

	return nil
}

// Stop stops the job system
func (js *JobSystem) Stop(ctx context.Context) error {
	if !js.started {
		return nil
	}

	js.logger.Info("Stopping job system")

	// Stop scheduler first
	if js.scheduler != nil {
		if err := js.scheduler.Stop(ctx); err != nil {
			js.logger.Error("Failed to stop scheduler", logger.Error(err))
		}
	}

	// Stop processor
	if err := js.processor.Stop(ctx); err != nil {
		js.logger.Error("Failed to stop processor", logger.Error(err))
	}

	// Close event bus
	if eventBus, ok := js.eventBus.(*eventBus); ok {
		eventBus.Close()
	}

	js.started = false
	js.logger.Info("Job system stopped")

	return nil
}

// Processor returns the job processor
func (js *JobSystem) Processor() Processor {
	return js.processor
}

// Scheduler returns the job scheduler
func (js *JobSystem) Scheduler() Scheduler {
	return js.scheduler
}

// EventBus returns the event bus
func (js *JobSystem) EventBus() EventBus {
	return js.eventBus
}

// IsStarted returns whether the job system is started
func (js *JobSystem) IsStarted() bool {
	return js.started
}

// Health checks the health of the job system
func (js *JobSystem) Health(ctx context.Context) error {
	if !js.started {
		return fmt.Errorf("job system not started")
	}

	// Check processor health
	if err := js.processor.Health(ctx); err != nil {
		return fmt.Errorf("processor unhealthy: %w", err)
	}

	// Check scheduler health if available
	if js.scheduler != nil {
		if err := js.scheduler.Health(ctx); err != nil {
			return fmt.Errorf("scheduler unhealthy: %w", err)
		}
	}

	return nil
}

// RegisterHandler registers a job handler
func (js *JobSystem) RegisterHandler(jobType string, handler JobHandler) error {
	return js.processor.RegisterHandler(jobType, handler)
}

// RegisterHandlerFunc registers a job handler function
func (js *JobSystem) RegisterHandlerFunc(jobType string, handlerFunc func(context.Context, Job) (interface{}, error)) error {
	return js.processor.RegisterHandler(jobType, JobHandlerFunc(handlerFunc))
}

// Enqueue enqueues a job
func (js *JobSystem) Enqueue(ctx context.Context, job Job) error {
	return js.processor.Enqueue(ctx, job)
}

// EnqueueIn enqueues a job to be processed after a delay
func (js *JobSystem) EnqueueIn(ctx context.Context, job Job, delay time.Duration) error {
	return js.processor.EnqueueIn(ctx, job, delay)
}

// EnqueueAt enqueues a job to be processed at a specific time
func (js *JobSystem) EnqueueAt(ctx context.Context, job Job, at time.Time) error {
	return js.processor.EnqueueAt(ctx, job, at)
}

// Schedule creates a scheduled job
func (js *JobSystem) Schedule(ctx context.Context, schedule Schedule) error {
	if js.scheduler == nil {
		return fmt.Errorf("scheduler not enabled")
	}
	return js.scheduler.Schedule(ctx, schedule)
}

// GetStats returns job system statistics
func (js *JobSystem) GetStats(ctx context.Context) (*JobSystemStats, error) {
	stats := &JobSystemStats{
		Processor: &ProcessorStats{},
		Scheduler: &SchedulerStats{},
	}

	// Get processor stats
	if procStats, err := js.processor.GetStats(ctx); err == nil {
		stats.Processor.TotalJobs = procStats.TotalJobs
		stats.Processor.QueuedJobs = procStats.QueuedJobs
		stats.Processor.RunningJobs = procStats.RunningJobs
		stats.Processor.CompletedJobs = procStats.CompletedJobs
		stats.Processor.FailedJobs = procStats.FailedJobs
		stats.Processor.ActiveWorkers = procStats.ActiveWorkers
		stats.Processor.TotalWorkers = procStats.TotalWorkers
		stats.Processor.ProcessingRate = procStats.ProcessingRate
		stats.Processor.AverageWaitTime = procStats.AverageWaitTime
		stats.Processor.Uptime = procStats.Uptime
	}

	// Get scheduler stats if available
	if js.scheduler != nil {
		if schedules, err := js.scheduler.ListSchedules(ctx); err == nil {
			stats.Scheduler.TotalSchedules = len(schedules)
			for _, schedule := range schedules {
				if schedule.Enabled {
					stats.Scheduler.ActiveSchedules++
				}
				if schedule.IsRunning {
					stats.Scheduler.RunningExecutions++
				}
			}
		}
	}

	stats.LastUpdated = time.Now()
	return stats, nil
}

// setupMonitoring sets up default monitoring and event handlers
func (js *JobSystem) setupMonitoring() {
	// Set up metrics event handler
	metricsHandler := NewMetricsEventHandler(js.logger)

	// Subscribe to all events
	js.eventBus.Subscribe(EventJobEnqueued, metricsHandler)
	js.eventBus.Subscribe(EventJobStarted, metricsHandler)
	js.eventBus.Subscribe(EventJobCompleted, metricsHandler)
	js.eventBus.Subscribe(EventJobFailed, metricsHandler)
	js.eventBus.Subscribe(EventJobRetried, metricsHandler)
	js.eventBus.Subscribe(EventJobCancelled, metricsHandler)

	js.logger.Info("Job system monitoring enabled")
}

// JobSystemStats represents complete job system statistics
type JobSystemStats struct {
	Processor   *ProcessorStats `json:"processor"`
	Scheduler   *SchedulerStats `json:"scheduler"`
	LastUpdated time.Time       `json:"last_updated"`
}

// ProcessorStats represents processor-specific statistics
type ProcessorStats struct {
	TotalJobs       int64         `json:"total_jobs"`
	QueuedJobs      int64         `json:"queued_jobs"`
	RunningJobs     int64         `json:"running_jobs"`
	CompletedJobs   int64         `json:"completed_jobs"`
	FailedJobs      int64         `json:"failed_jobs"`
	ActiveWorkers   int           `json:"active_workers"`
	TotalWorkers    int           `json:"total_workers"`
	ProcessingRate  float64       `json:"processing_rate"`
	AverageWaitTime time.Duration `json:"average_wait_time"`
	Uptime          time.Duration `json:"uptime"`
}

// Builder pattern for creating job systems

// JobSystemBuilder provides a fluent interface for building job systems
type JobSystemBuilder struct {
	config    Config
	logger    logger.Logger
	handlers  map[string]JobHandler
	schedules []Schedule
}

// NewJobSystemBuilder creates a new job system builder
func NewJobSystemBuilder(logger logger.Logger) *JobSystemBuilder {
	return &JobSystemBuilder{
		config:    DefaultConfig(),
		logger:    logger,
		handlers:  make(map[string]JobHandler),
		schedules: make([]Schedule, 0),
	}
}

// WithConfig sets the configuration
func (b *JobSystemBuilder) WithConfig(config Config) *JobSystemBuilder {
	b.config = config
	return b
}

// WithStorage sets the storage configuration
func (b *JobSystemBuilder) WithStorage(storageType string, options map[string]string) *JobSystemBuilder {
	b.config.Storage.Type = storageType
	if options != nil {
		b.config.Storage.Options = options
	}
	return b
}

// WithConcurrency sets the concurrency level
func (b *JobSystemBuilder) WithConcurrency(concurrency int) *JobSystemBuilder {
	b.config.Concurrency = concurrency
	return b
}

// WithQueue adds a queue configuration
func (b *JobSystemBuilder) WithQueue(name string, config QueueConfig) *JobSystemBuilder {
	if b.config.Queues == nil {
		b.config.Queues = make(map[string]QueueConfig)
	}
	b.config.Queues[name] = config
	return b
}

// WithScheduler enables and configures the scheduler
func (b *JobSystemBuilder) WithScheduler(config SchedulerConfig) *JobSystemBuilder {
	b.config.Scheduler = config
	b.config.Scheduler.Enabled = true
	return b
}

// EnableMonitoring enables monitoring
func (b *JobSystemBuilder) EnableMonitoring() *JobSystemBuilder {
	b.config.Monitoring.Enabled = true
	return b
}

// DisableMonitoring disables monitoring
func (b *JobSystemBuilder) DisableMonitoring() *JobSystemBuilder {
	b.config.Monitoring.Enabled = false
	return b
}

// RegisterHandler registers a job handler
func (b *JobSystemBuilder) RegisterHandler(jobType string, handler JobHandler) *JobSystemBuilder {
	b.handlers[jobType] = handler
	return b
}

// RegisterHandlerFunc registers a job handler function
func (b *JobSystemBuilder) RegisterHandlerFunc(jobType string, handlerFunc func(context.Context, Job) (interface{}, error)) *JobSystemBuilder {
	b.handlers[jobType] = JobHandlerFunc(handlerFunc)
	return b
}

// AddSchedule adds a schedule to be created on startup
func (b *JobSystemBuilder) AddSchedule(schedule Schedule) *JobSystemBuilder {
	b.schedules = append(b.schedules, schedule)
	return b
}

// Build creates the job system
func (b *JobSystemBuilder) Build() (*JobSystem, error) {
	// Create job system
	system, err := NewJobSystem(b.config, b.logger)
	if err != nil {
		return nil, err
	}

	// Register handlers
	for jobType, handler := range b.handlers {
		if err := system.RegisterHandler(jobType, handler); err != nil {
			return nil, fmt.Errorf("failed to register handler for %s: %w", jobType, err)
		}
	}

	return system, nil
}

// BuildAndStart creates and starts the job system
func (b *JobSystemBuilder) BuildAndStart(ctx context.Context) (*JobSystem, error) {
	system, err := b.Build()
	if err != nil {
		return nil, err
	}

	if err := system.Start(ctx); err != nil {
		return nil, fmt.Errorf("failed to start job system: %w", err)
	}

	// Create schedules if any
	if system.scheduler != nil && len(b.schedules) > 0 {
		for _, schedule := range b.schedules {
			if err := system.Schedule(ctx, schedule); err != nil {
				b.logger.Error("Failed to create schedule",
					logger.String("schedule_name", schedule.Name),
					logger.Error(err),
				)
			}
		}
	}

	return system, nil
}

// Convenience functions for common use cases

// NewSimpleJobSystem creates a simple in-memory job system
func NewSimpleJobSystem(logger logger.Logger) (*JobSystem, error) {
	return NewJobSystemBuilder(logger).
		WithStorage("memory", nil).
		WithConcurrency(5).
		EnableMonitoring().
		Build()
}

// NewRedisJobSystem creates a Redis-backed job system
func NewRedisJobSystem(redisURL string, logger logger.Logger) (*JobSystem, error) {
	return NewJobSystemBuilder(logger).
		WithStorage("redis", map[string]string{
			"url": redisURL,
		}).
		WithConcurrency(10).
		WithScheduler(DefaultSchedulerConfig()).
		EnableMonitoring().
		Build()
}

// NewProductionJobSystem creates a production-ready job system
func NewProductionJobSystem(config Config, logger logger.Logger) (*JobSystem, error) {
	return NewJobSystemBuilder(logger).
		WithConfig(config).
		EnableMonitoring().
		Build()
}

// Example job handlers

// EmailJobHandler handles email sending jobs
type EmailJobHandler struct {
	logger logger.Logger
	// emailService EmailService
}

func NewEmailJobHandler(logger logger.Logger) *EmailJobHandler {
	return &EmailJobHandler{
		logger: logger.Named("email-handler"),
	}
}

func (h *EmailJobHandler) Handle(ctx context.Context, job Job) (interface{}, error) {
	to, ok := job.Payload["to"].(string)
	if !ok {
		return nil, fmt.Errorf("missing 'to' field in email job")
	}

	subject, ok := job.Payload["subject"].(string)
	if !ok {
		return nil, fmt.Errorf("missing 'subject' field in email job")
	}

	body, ok := job.Payload["body"].(string)
	if !ok {
		return nil, fmt.Errorf("missing 'body' field in email job")
	}

	h.logger.Info("Sending email",
		logger.String("to", to),
		logger.String("subject", subject),
		logger.String("body", body),
	)

	// Simulate email sending
	time.Sleep(100 * time.Millisecond)

	// TODO: Implement actual email sending
	// return h.emailService.Send(ctx, to, subject, body)

	return map[string]interface{}{
		"sent_to": to,
		"sent_at": time.Now(),
	}, nil
}

func (h *EmailJobHandler) CanHandle(jobType string) bool {
	return jobType == "send_email"
}

func (h *EmailJobHandler) GetTimeout() time.Duration {
	return 30 * time.Second
}

func (h *EmailJobHandler) GetRetryPolicy() RetryPolicy {
	return RetryPolicy{
		MaxRetries:      3,
		InitialInterval: 5 * time.Second,
		MaxInterval:     60 * time.Second,
		Multiplier:      2.0,
	}
}

// ReportJobHandler handles report generation jobs
type ReportJobHandler struct {
	logger logger.Logger
}

func NewReportJobHandler(logger logger.Logger) *ReportJobHandler {
	return &ReportJobHandler{
		logger: logger.Named("report-handler"),
	}
}

func (h *ReportJobHandler) Handle(ctx context.Context, job Job) (interface{}, error) {
	reportType, ok := job.Payload["type"].(string)
	if !ok {
		return nil, fmt.Errorf("missing 'type' field in report job")
	}

	h.logger.Info("Generating report",
		logger.String("type", reportType),
	)

	// Simulate report generation
	time.Sleep(2 * time.Second)

	// TODO: Implement actual report generation
	// return h.reportService.Generate(ctx, reportType, job.Payload)

	return map[string]interface{}{
		"report_type":  reportType,
		"generated_at": time.Now(),
		"file_path":    fmt.Sprintf("/reports/%s_%d.pdf", reportType, time.Now().Unix()),
	}, nil
}

func (h *ReportJobHandler) CanHandle(jobType string) bool {
	return jobType == "generate_report"
}

func (h *ReportJobHandler) GetTimeout() time.Duration {
	return 5 * time.Minute
}

func (h *ReportJobHandler) GetRetryPolicy() RetryPolicy {
	return RetryPolicy{
		MaxRetries:      2,
		InitialInterval: 30 * time.Second,
		MaxInterval:     300 * time.Second,
		Multiplier:      2.0,
	}
}
