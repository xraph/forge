package cron

import (
	"context"
	"fmt"
	"net/http"

	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/database"
	eventscore "github.com/xraph/forge/pkg/events/core"
	"github.com/xraph/forge/pkg/router"
)

// CronPlugin provides plugin integration for cron functionality
type CronPlugin struct {
	service *CronService
}

// NewCronPlugin creates a new cron plugin
func NewCronPlugin() *CronPlugin {
	return &CronPlugin{}
}

// ID returns the plugin ID
func (cp *CronPlugin) ID() string {
	return "cron-plugin"
}

// Name returns the plugin name
func (cp *CronPlugin) Name() string {
	return "cron-plugin"
}

// Version returns the plugin version
func (cp *CronPlugin) Version() string {
	return "1.0.0"
}

// Description returns the plugin description
func (cp *CronPlugin) Description() string {
	return "Distributed cron job scheduling plugin"
}

// Author returns the plugin author
func (cp *CronPlugin) Author() string {
	return "Forge Framework"
}

// License returns the plugin license
func (cp *CronPlugin) License() string {
	return "MIT"
}

// Initialize initializes the plugin
func (cp *CronPlugin) Initialize(ctx context.Context, container common.Container) error {
	// Resolve dependencies
	logger, err := container.Resolve((*common.Logger)(nil))
	if err != nil {
		return common.ErrDependencyNotFound("cron-plugin", "logger")
	}

	metrics, err := container.Resolve((*common.Metrics)(nil))
	if err != nil {
		return common.ErrDependencyNotFound("cron-plugin", "metrics")
	}

	eventBus, err := container.Resolve((*eventscore.EventBus)(nil))
	if err != nil {
		return common.ErrDependencyNotFound("cron-plugin", "event-bus")
	}

	healthChecker, err := container.Resolve((*common.HealthChecker)(nil))
	if err != nil {
		return common.ErrDependencyNotFound("cron-plugin", "health-checker")
	}

	config, err := container.Resolve((*common.ConfigManager)(nil))
	if err != nil {
		return common.ErrDependencyNotFound("cron-plugin", "config-manager")
	}

	db, err := container.Resolve((*database.Connection)(nil))
	if err != nil {
		return common.ErrDependencyNotFound("cron-plugin", "database")
	}

	// Create service
	service, err := NewCronService(
		db.(database.Connection),
		logger.(common.Logger),
		metrics.(common.Metrics),
		eventBus.(eventscore.EventBus),
		healthChecker.(common.HealthChecker),
		config.(common.ConfigManager),
	)
	if err != nil {
		return common.ErrPluginInitFailed("cron-plugin", err)
	}

	cp.service = service.(*CronService)
	return nil
}

// OnStart starts the plugin
func (cp *CronPlugin) OnStart(ctx context.Context) error {
	if cp.service == nil {
		return common.ErrPluginInitFailed("cron-plugin", fmt.Errorf("service not initialized"))
	}
	return cp.service.OnStart(ctx)
}

// OnStop stops the plugin
func (cp *CronPlugin) OnStop(ctx context.Context) error {
	if cp.service == nil {
		return nil
	}
	return cp.service.OnStop(ctx)
}

// Cleanup performs cleanup
func (cp *CronPlugin) Cleanup(ctx context.Context) error {
	return nil
}

// Type returns the plugin type
func (cp *CronPlugin) Type() common.PluginType {
	return common.PluginTypeService
}

// Capabilities returns plugin capabilities
func (cp *CronPlugin) Capabilities() []common.PluginCapability {
	return []common.PluginCapability{
		{
			Name:        "cron-scheduling",
			Version:     "1.0.0",
			Description: "Distributed cron job scheduling",
			Interface:   "CronManager",
			Methods:     []string{"RegisterJob", "UnregisterJob", "TriggerJob", "GetJobs", "GetJobHistory"},
		},
	}
}

// Dependencies returns plugin dependencies
func (cp *CronPlugin) Dependencies() []common.PluginDependency {
	return []common.PluginDependency{
		{Name: "database-manager", Version: "1.0.0", Type: "service", Required: true},
		{Name: "event-bus", Version: "1.0.0", Type: "service", Required: true},
		{Name: "metrics-collector", Version: "1.0.0", Type: "service", Required: true},
		{Name: "health-checker", Version: "1.0.0", Type: "service", Required: true},
		{Name: "config-manager", Version: "1.0.0", Type: "service", Required: true},
	}
}

// Middleware returns plugin middleware
func (cp *CronPlugin) Middleware() []any {
	return []any{
		cp.cronJobHeaderMiddleware(),
	}
}

// ConfigureRoutes configures plugin routes using opinionated handlers
func (cp *CronPlugin) ConfigureRoutes(r common.Router) error {
	// GET /cron/jobs - List all jobs
	if err := r.RegisterOpinionatedHandler("GET", "/cron/jobs", cp.GetJobs,
		router.WithSummary("List all cron jobs"),
		router.WithOpenAPITags("Cron"),
		router.WithDescription("Retrieves a list of all registered cron jobs"),
	); err != nil {
		return err
	}

	// POST /cron/jobs - Create a new job
	if err := r.RegisterOpinionatedHandler("POST", "/cron/jobs", cp.CreateJob,
		router.WithSummary("Create a new cron job"),
		router.WithOpenAPITags("Cron"),
		router.WithDescription("Registers a new cron job with the scheduler"),
	); err != nil {
		return err
	}

	// GET /cron/jobs/:id - Get a specific job
	if err := r.RegisterOpinionatedHandler("GET", "/cron/jobs/:id", cp.GetJob,
		router.WithSummary("Get cron job details"),
		router.WithOpenAPITags("Cron"),
		router.WithDescription("Retrieves detailed information about a specific cron job"),
	); err != nil {
		return err
	}

	// PUT /cron/jobs/:id - Update a job
	if err := r.RegisterOpinionatedHandler("PUT", "/cron/jobs/:id", cp.UpdateJob,
		router.WithSummary("Update a cron job"),
		router.WithOpenAPITags("Cron"),
		router.WithDescription("Updates the configuration of an existing cron job"),
	); err != nil {
		return err
	}

	// DELETE /cron/jobs/:id - Delete a job
	if err := r.RegisterOpinionatedHandler("DELETE", "/cron/jobs/:id", cp.DeleteJob,
		router.WithSummary("Delete a cron job"),
		router.WithOpenAPITags("Cron"),
		router.WithDescription("Removes a cron job from the scheduler"),
	); err != nil {
		return err
	}

	// POST /cron/jobs/:id/trigger - Manually trigger a job
	if err := r.RegisterOpinionatedHandler("POST", "/cron/jobs/:id/trigger", cp.TriggerJob,
		router.WithSummary("Trigger a cron job"),
		router.WithOpenAPITags("Cron"),
		router.WithDescription("Manually triggers execution of a cron job"),
	); err != nil {
		return err
	}

	// GET /cron/jobs/:id/history - Get job execution history
	if err := r.RegisterOpinionatedHandler("GET", "/cron/jobs/:id/history", cp.GetJobHistory,
		router.WithSummary("Get job execution history"),
		router.WithOpenAPITags("Cron"),
		router.WithDescription("Retrieves the execution history for a specific cron job"),
	); err != nil {
		return err
	}

	// GET /cron/stats - Get cron statistics
	if err := r.RegisterOpinionatedHandler("GET", "/cron/stats", cp.GetStats,
		router.WithSummary("Get cron statistics"),
		router.WithOpenAPITags("Cron"),
		router.WithDescription("Retrieves overall statistics for the cron system"),
	); err != nil {
		return err
	}

	return nil
}

// Services returns plugin services
func (cp *CronPlugin) Services() []common.ServiceDefinition {
	return []common.ServiceDefinition{
		{
			Name:         "cron-service",
			Type:         (*CronService)(nil),
			Instance:     cp.service,
			Singleton:    true,
			Dependencies: []string{"database-manager", "event-bus", "metrics-collector", "health-checker", "config-manager"},
		},
	}
}

// Controllers returns plugin controllers
func (cp *CronPlugin) Controllers() []common.Controller {
	return []common.Controller{}
}

// Commands returns plugin CLI commands
func (cp *CronPlugin) Commands() []common.CLICommand {
	return []common.CLICommand{}
}

// Hooks returns plugin hooks
func (cp *CronPlugin) Hooks() []common.Hook {
	return []common.Hook{}
}

// ConfigSchema returns the configuration schema
func (cp *CronPlugin) ConfigSchema() common.ConfigSchema {
	return common.ConfigSchema{
		Version: "1.0.0",
		Type:    "object",
		Title:   "Cron Plugin Configuration",
		Properties: map[string]common.ConfigProperty{
			"node_id": {
				Type:        "string",
				Description: "Unique identifier for this node",
				Default:     "node-1",
			},
			"cluster_id": {
				Type:        "string",
				Description: "Cluster identifier",
				Default:     "cron-cluster",
			},
			"max_concurrent_jobs": {
				Type:        "integer",
				Description: "Maximum number of concurrent jobs",
				Default:     10,
			},
		},
		Required: []string{"node_id", "cluster_id"},
	}
}

// Configure configures the plugin
func (cp *CronPlugin) Configure(config interface{}) error {
	return nil
}

// GetConfig returns the plugin configuration
func (cp *CronPlugin) GetConfig() interface{} {
	if cp.service != nil {
		return cp.service.GetConfig()
	}
	return nil
}

// HealthCheck performs a health check
func (cp *CronPlugin) HealthCheck(ctx context.Context) error {
	if cp.service == nil {
		return common.ErrHealthCheckFailed("cron-plugin", fmt.Errorf("service not initialized"))
	}
	return cp.service.OnHealthCheck(ctx)
}

// GetMetrics returns plugin metrics
func (cp *CronPlugin) GetMetrics() common.PluginMetrics {
	if cp.service == nil {
		return common.PluginMetrics{}
	}

	stats := cp.service.GetManager().GetStats()

	return common.PluginMetrics{
		CallCount:      stats.TotalExecutions,
		RouteCount:     8,
		ErrorCount:     stats.FailedExecutions,
		AverageLatency: stats.AverageJobDuration,
		LastExecuted:   stats.LastUpdate,
		MemoryUsage:    0,
		CPUUsage:       0,
		HealthScore:    calculateHealthScore(stats),
		Uptime:         0,
	}
}

// cronJobHeaderMiddleware adds cron job headers to responses
func (cp *CronPlugin) cronJobHeaderMiddleware() common.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if cp.service != nil {
				w.Header().Set("X-Cron-Node", cp.service.config.NodeID)
				w.Header().Set("X-Cron-Cluster", cp.service.config.ClusterID)
			}
			next.ServeHTTP(w, r)
		})
	}
}

// =============================================================================
// REQUEST/RESPONSE TYPES
// =============================================================================

// GetJobsRequest represents the request for listing jobs
type GetJobsRequest struct {
	// No parameters needed - returns all jobs
}

// GetJobsResponse represents the response for listing jobs
type GetJobsResponse struct {
	Jobs  []Job `json:"jobs"`
	Count int   `json:"count"`
}

// CreateJobRequest represents the request for creating a job
type CreateJobRequest struct {
	JobDefinition JobDefinition `json:"job_definition" validate:"required"`
}

// CreateJobResponse represents the response for creating a job
type CreateJobResponse struct {
	Message string `json:"message"`
	JobID   string `json:"job_id"`
}

// GetJobRequest represents the request for getting a job
type GetJobRequest struct {
	ID string `path:"id" validate:"required"`
}

// UpdateJobRequest represents the request for updating a job
type UpdateJobRequest struct {
	ID            string        `path:"id" validate:"required"`
	JobDefinition JobDefinition `json:"job_definition" validate:"required"`
}

// UpdateJobResponse represents the response for updating a job
type UpdateJobResponse struct {
	Message string `json:"message"`
	JobID   string `json:"job_id"`
}

// DeleteJobRequest represents the request for deleting a job
type DeleteJobRequest struct {
	ID string `path:"id" validate:"required"`
}

// DeleteJobResponse represents the response for deleting a job
type DeleteJobResponse struct {
	Message string `json:"message"`
	JobID   string `json:"job_id"`
}

// TriggerJobRequest represents the request for triggering a job
type TriggerJobRequest struct {
	ID string `path:"id" validate:"required"`
}

// TriggerJobResponse represents the response for triggering a job
type TriggerJobResponse struct {
	Message string `json:"message"`
	JobID   string `json:"job_id"`
}

// GetJobHistoryRequest represents the request for getting job history
type GetJobHistoryRequest struct {
	ID    string `path:"id" validate:"required"`
	Limit int    `query:"limit" default:"50" validate:"min=1,max=1000"`
}

// GetJobHistoryResponse represents the response for getting job history
type GetJobHistoryResponse struct {
	JobID      string         `json:"job_id"`
	Executions []JobExecution `json:"executions"`
	Count      int            `json:"count"`
}

// GetStatsRequest represents the request for getting stats
type GetStatsRequest struct {
	// No parameters needed
}

// =============================================================================
// OPINIONATED HANDLER IMPLEMENTATIONS
// =============================================================================

// GetJobs handles GET /cron/jobs
func (cp *CronPlugin) GetJobs(ctx common.Context, req GetJobsRequest) (*GetJobsResponse, error) {
	jobs := cp.service.manager.GetJobs()
	return &GetJobsResponse{
		Jobs:  jobs,
		Count: len(jobs),
	}, nil
}

// CreateJob handles POST /cron/jobs
func (cp *CronPlugin) CreateJob(ctx common.Context, req CreateJobRequest) (*CreateJobResponse, error) {
	if err := cp.service.manager.RegisterJob(req.JobDefinition); err != nil {
		return nil, fmt.Errorf("failed to register job: %w", err)
	}

	return &CreateJobResponse{
		Message: "Job created successfully",
		JobID:   req.JobDefinition.ID,
	}, nil
}

// GetJob handles GET /cron/jobs/:id
func (cp *CronPlugin) GetJob(ctx common.Context, req GetJobRequest) (*Job, error) {
	job, err := cp.service.manager.GetJob(req.ID)
	if err != nil {
		return nil, fmt.Errorf("job not found: %w", err)
	}

	return job, nil
}

// UpdateJob handles PUT /cron/jobs/:id
func (cp *CronPlugin) UpdateJob(ctx common.Context, req UpdateJobRequest) (*UpdateJobResponse, error) {
	// Ensure job ID matches
	req.JobDefinition.ID = req.ID

	// Unregister and re-register
	if err := cp.service.manager.UnregisterJob(req.ID); err != nil {
		return nil, fmt.Errorf("failed to unregister job: %w", err)
	}

	if err := cp.service.manager.RegisterJob(req.JobDefinition); err != nil {
		return nil, fmt.Errorf("failed to register job: %w", err)
	}

	return &UpdateJobResponse{
		Message: "Job updated successfully",
		JobID:   req.ID,
	}, nil
}

// DeleteJob handles DELETE /cron/jobs/:id
func (cp *CronPlugin) DeleteJob(ctx common.Context, req DeleteJobRequest) (*DeleteJobResponse, error) {
	if err := cp.service.manager.UnregisterJob(req.ID); err != nil {
		return nil, fmt.Errorf("failed to delete job: %w", err)
	}

	return &DeleteJobResponse{
		Message: "Job deleted successfully",
		JobID:   req.ID,
	}, nil
}

// TriggerJob handles POST /cron/jobs/:id/trigger
func (cp *CronPlugin) TriggerJob(ctx common.Context, req TriggerJobRequest) (*TriggerJobResponse, error) {
	if err := cp.service.manager.TriggerJob(ctx, req.ID); err != nil {
		return nil, fmt.Errorf("failed to trigger job: %w", err)
	}

	return &TriggerJobResponse{
		Message: "Job triggered successfully",
		JobID:   req.ID,
	}, nil
}

// GetJobHistory handles GET /cron/jobs/:id/history
func (cp *CronPlugin) GetJobHistory(ctx common.Context, req GetJobHistoryRequest) (*GetJobHistoryResponse, error) {
	history, err := cp.service.manager.GetJobHistory(req.ID, req.Limit)
	if err != nil {
		return nil, fmt.Errorf("failed to get job history: %w", err)
	}

	return &GetJobHistoryResponse{
		JobID:      req.ID,
		Executions: history,
		Count:      len(history),
	}, nil
}

// GetStats handles GET /cron/stats
func (cp *CronPlugin) GetStats(ctx common.Context, req GetStatsRequest) (*CronStats, error) {
	stats := cp.service.manager.GetStats()
	return &stats, nil
}

// Helper functions

func calculateHealthScore(stats CronStats) float64 {
	if stats.TotalExecutions == 0 {
		return 1.0
	}

	score := stats.SuccessRate

	// Penalty for high failure rate
	if stats.FailureRate > 0.2 {
		score *= 0.5
	}

	// Penalty for unhealthy cluster
	if !stats.ClusterHealthy {
		score *= 0.7
	}

	return score
}
