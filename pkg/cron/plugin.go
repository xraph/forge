package cron

import (
	"context"
	"fmt"

	"github.com/xraph/forge/pkg/common"
	eventscore "github.com/xraph/forge/pkg/events/core"
)

// CronPlugin provides plugin integration for cron functionality
type CronPlugin struct {
	service *CronService
}

// NewCronPlugin creates a new cron plugin
func NewCronPlugin() *CronPlugin {
	return &CronPlugin{}
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

// Dependencies returns the plugin dependencies
func (cp *CronPlugin) Dependencies() []string {
	return []string{
		"database-manager",
		"event-bus",
		"metrics-collector",
		"health-checker",
		"config-manager",
	}
}

// Initialize initializes the plugin
func (cp *CronPlugin) Initialize(container common.Container) error {
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

	// Create service
	service, err := NewCronService(
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

// Middleware returns plugin middleware
func (cp *CronPlugin) Middleware() []common.MiddlewareDefinition {
	return []common.MiddlewareDefinition{
		{
			Name:     "cron-job-header",
			Priority: 100,
			Handler:  cp.cronJobHeaderMiddleware,
		},
	}
}

// Routes returns plugin routes
func (cp *CronPlugin) Routes() []common.RouteDefinition {
	return []common.RouteDefinition{
		{
			Method:  "GET",
			Pattern: "/cron/jobs",
			Handler: cp.getJobsHandler,
		},
		{
			Method:  "POST",
			Pattern: "/cron/jobs",
			Handler: cp.createJobHandler,
		},
		{
			Method:  "GET",
			Pattern: "/cron/jobs/:id",
			Handler: cp.getJobHandler,
		},
		{
			Method:  "PUT",
			Pattern: "/cron/jobs/:id",
			Handler: cp.updateJobHandler,
		},
		{
			Method:  "DELETE",
			Pattern: "/cron/jobs/:id",
			Handler: cp.deleteJobHandler,
		},
		{
			Method:  "POST",
			Pattern: "/cron/jobs/:id/trigger",
			Handler: cp.triggerJobHandler,
		},
		{
			Method:  "GET",
			Pattern: "/cron/jobs/:id/history",
			Handler: cp.getJobHistoryHandler,
		},
		{
			Method:  "GET",
			Pattern: "/cron/stats",
			Handler: cp.getStatsHandler,
		},
	}
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

// cronJobHeaderMiddleware adds cron job headers to responses
func (cp *CronPlugin) cronJobHeaderMiddleware(next eventscore.EventHandler) eventscore.EventHandler {
	return func(ctx common.Context) error {
		ctx.Header("X-Cron-Node", cp.service.config.NodeID)
		ctx.Header("X-Cron-Cluster", cp.service.config.ClusterID)
		return next(ctx)
	}
}

// getJobsHandler handles GET /cron/jobs
func (cp *CronPlugin) getJobsHandler(ctx common.Context) error {
	jobs := cp.service.manager.GetJobs()
	return ctx.JSON(200, map[string]interface{}{
		"jobs":  jobs,
		"count": len(jobs),
	})
}

// createJobHandler handles POST /cron/jobs
func (cp *CronPlugin) createJobHandler(ctx common.Context) error {
	var req struct {
		JobDefinition JobDefinition `json:"job_definition"`
	}

	if err := ctx.BindJSON(&req); err != nil {
		return ctx.JSON(400, map[string]interface{}{
			"error": "Invalid request body",
		})
	}

	if err := cp.service.manager.RegisterJob(req.JobDefinition); err != nil {
		return ctx.JSON(500, map[string]interface{}{
			"error": err.Error(),
		})
	}

	return ctx.JSON(201, map[string]interface{}{
		"message": "Job created successfully",
		"job_id":  req.JobDefinition.ID,
	})
}

// getJobHandler handles GET /cron/jobs/:id
func (cp *CronPlugin) getJobHandler(ctx common.Context) error {
	jobID := ctx.PathParam("id")
	if jobID == "" {
		return ctx.JSON(400, map[string]interface{}{
			"error": "Job ID is required",
		})
	}

	job, err := cp.service.manager.GetJob(jobID)
	if err != nil {
		return ctx.JSON(404, map[string]interface{}{
			"error": "Job not found",
		})
	}

	return ctx.JSON(200, job)
}

// updateJobHandler handles PUT /cron/jobs/:id
func (cp *CronPlugin) updateJobHandler(ctx common.Context) error {
	jobID := ctx.PathParam("id")
	if jobID == "" {
		return ctx.JSON(400, map[string]interface{}{
			"error": "Job ID is required",
		})
	}

	var req struct {
		JobDefinition JobDefinition `json:"job_definition"`
	}

	if err := ctx.BindJSON(&req); err != nil {
		return ctx.JSON(400, map[string]interface{}{
			"error": "Invalid request body",
		})
	}

	// Ensure job ID matches
	req.JobDefinition.ID = jobID

	// For now, we'll unregister and re-register
	if err := cp.service.manager.UnregisterJob(jobID); err != nil {
		return ctx.JSON(500, map[string]interface{}{
			"error": err.Error(),
		})
	}

	if err := cp.service.manager.RegisterJob(req.JobDefinition); err != nil {
		return ctx.JSON(500, map[string]interface{}{
			"error": err.Error(),
		})
	}

	return ctx.JSON(200, map[string]interface{}{
		"message": "Job updated successfully",
		"job_id":  jobID,
	})
}

// deleteJobHandler handles DELETE /cron/jobs/:id
func (cp *CronPlugin) deleteJobHandler(ctx common.Context) error {
	jobID := ctx.PathParam("id")
	if jobID == "" {
		return ctx.JSON(400, map[string]interface{}{
			"error": "Job ID is required",
		})
	}

	if err := cp.service.manager.UnregisterJob(jobID); err != nil {
		return ctx.JSON(500, map[string]interface{}{
			"error": err.Error(),
		})
	}

	return ctx.JSON(200, map[string]interface{}{
		"message": "Job deleted successfully",
		"job_id":  jobID,
	})
}

// triggerJobHandler handles POST /cron/jobs/:id/trigger
func (cp *CronPlugin) triggerJobHandler(ctx common.Context) error {
	jobID := ctx.PathParam("id")
	if jobID == "" {
		return ctx.JSON(400, map[string]interface{}{
			"error": "Job ID is required",
		})
	}

	if err := cp.service.manager.TriggerJob(ctx, jobID); err != nil {
		return ctx.JSON(500, map[string]interface{}{
			"error": err.Error(),
		})
	}

	return ctx.JSON(200, map[string]interface{}{
		"message": "Job triggered successfully",
		"job_id":  jobID,
	})
}

// getJobHistoryHandler handles GET /cron/jobs/:id/history
func (cp *CronPlugin) getJobHistoryHandler(ctx common.Context) error {
	jobID := ctx.PathParam("id")
	if jobID == "" {
		return ctx.JSON(400, map[string]interface{}{
			"error": "Job ID is required",
		})
	}

	limit := 50
	if limitStr := ctx.QueryParam("limit"); limitStr != "" {
		if parsedLimit, err := parseInt(limitStr); err == nil && parsedLimit > 0 && parsedLimit <= 1000 {
			limit = parsedLimit
		}
	}

	history, err := cp.service.manager.GetJobHistory(jobID, limit)
	if err != nil {
		return ctx.JSON(500, map[string]interface{}{
			"error": err.Error(),
		})
	}

	return ctx.JSON(200, map[string]interface{}{
		"job_id":     jobID,
		"executions": history,
		"count":      len(history),
	})
}

// getStatsHandler handles GET /cron/stats
func (cp *CronPlugin) getStatsHandler(ctx common.Context) error {
	stats := cp.service.manager.GetStats()
	return ctx.JSON(200, stats)
}

// parseInt parses an integer from a string
func parseInt(s string) (int, error) {
	var result int
	for _, char := range s {
		if char < '0' || char > '9' {
			return 0, fmt.Errorf("invalid integer: %s", s)
		}
		result = result*10 + int(char-'0')
	}
	return result, nil
}
