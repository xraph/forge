package handlers

import (
	"net/http"
	"strconv"
	"time"

	"github.com/xraph/forge"
	cronext "github.com/xraph/forge/extensions/cron"
)

// APIHandler provides REST API endpoints for cron job management.
type APIHandler struct {
	extension *cronext.Extension
	logger    forge.Logger
}

// NewAPIHandler creates a new API handler.
func NewAPIHandler(extension *cronext.Extension, logger forge.Logger) *APIHandler {
	return &APIHandler{
		extension: extension,
		logger:    logger,
	}
}

// RegisterRoutes registers API routes with the router.
func (h *APIHandler) RegisterRoutes(router forge.Router, prefix string) {
	// Job management
	router.GET(prefix+"/jobs", h.ListJobs)
	router.POST(prefix+"/jobs", h.CreateJob)
	router.GET(prefix+"/jobs/:id", h.GetJob)
	router.PUT(prefix+"/jobs/:id", h.UpdateJob)
	router.DELETE(prefix+"/jobs/:id", h.DeleteJob)
	router.POST(prefix+"/jobs/:id/trigger", h.TriggerJob)
	router.POST(prefix+"/jobs/:id/enable", h.EnableJob)
	router.POST(prefix+"/jobs/:id/disable", h.DisableJob)

	// Execution history
	router.GET(prefix+"/jobs/:id/executions", h.GetJobExecutions)
	router.GET(prefix+"/executions", h.ListExecutions)
	router.GET(prefix+"/executions/:id", h.GetExecution)

	// Statistics
	router.GET(prefix+"/stats", h.GetStats)
	router.GET(prefix+"/jobs/:id/stats", h.GetJobStats)

	// Health
	router.GET(prefix+"/health", h.Health)
}

// ListJobs lists all jobs.
func (h *APIHandler) ListJobs(ctx forge.Context) error {
	scheduler := h.extension.GetScheduler()

	jobs, err := scheduler.ListJobs()
	if err != nil {
		return ctx.JSON(http.StatusInternalServerError, map[string]interface{}{
			"error": err.Error(),
		})
	}

	return ctx.JSON(http.StatusOK, map[string]interface{}{
		"jobs":  jobs,
		"count": len(jobs),
	})
}

// CreateJob creates a new job.
func (h *APIHandler) CreateJob(ctx forge.Context) error {
	var job cronext.Job
	if err := ctx.BindRequest(&job); err != nil {
		return ctx.JSON(http.StatusBadRequest, map[string]interface{}{
			"error": err.Error(),
		})
	}

	if err := h.extension.CreateJob(ctx.Request().Context(), &job); err != nil {
		return ctx.JSON(http.StatusInternalServerError, map[string]interface{}{
			"error": err.Error(),
		})
	}

	return ctx.JSON(http.StatusCreated, map[string]interface{}{
		"job": job,
	})
}

// GetJob retrieves a job by ID.
func (h *APIHandler) GetJob(ctx forge.Context) error {
	jobID := ctx.Param("id")
	if jobID == "" {
		return ctx.JSON(http.StatusBadRequest, map[string]interface{}{
			"error": "job ID is required",
		})
	}

	scheduler := h.extension.GetScheduler()
	job, err := scheduler.GetJob(jobID)
	if err != nil {
		if err == cronext.ErrJobNotFound {
			return ctx.JSON(http.StatusNotFound, map[string]interface{}{
				"error": "job not found",
			})
		}
		return ctx.JSON(http.StatusInternalServerError, map[string]interface{}{
			"error": err.Error(),
		})
	}

	return ctx.JSON(http.StatusOK, map[string]interface{}{
		"job": job,
	})
}

// UpdateJob updates a job.
func (h *APIHandler) UpdateJob(ctx forge.Context) error {
	jobID := ctx.Param("id")
	if jobID == "" {
		return ctx.JSON(http.StatusBadRequest, map[string]interface{}{
			"error": "job ID is required",
		})
	}

	var update cronext.JobUpdate
	if err := ctx.BindRequest(&update); err != nil {
		return ctx.JSON(http.StatusBadRequest, map[string]interface{}{
			"error": err.Error(),
		})
	}

	if err := h.extension.UpdateJob(ctx.Request().Context(), jobID, &update); err != nil {
		if err == cronext.ErrJobNotFound {
			return ctx.JSON(http.StatusNotFound, map[string]interface{}{
				"error": "job not found",
			})
		}
		return ctx.JSON(http.StatusInternalServerError, map[string]interface{}{
			"error": err.Error(),
		})
	}

	return ctx.JSON(http.StatusOK, map[string]interface{}{
		"message": "job updated successfully",
	})
}

// DeleteJob deletes a job.
func (h *APIHandler) DeleteJob(ctx forge.Context) error {
	jobID := ctx.Param("id")
	if jobID == "" {
		return ctx.JSON(http.StatusBadRequest, map[string]interface{}{
			"error": "job ID is required",
		})
	}

	if err := h.extension.DeleteJob(ctx.Request().Context(), jobID); err != nil {
		if err == cronext.ErrJobNotFound {
			return ctx.JSON(http.StatusNotFound, map[string]interface{}{
				"error": "job not found",
			})
		}
		return ctx.JSON(http.StatusInternalServerError, map[string]interface{}{
			"error": err.Error(),
		})
	}

	return ctx.JSON(http.StatusOK, map[string]interface{}{
		"message": "job deleted successfully",
	})
}

// TriggerJob manually triggers a job execution.
func (h *APIHandler) TriggerJob(ctx forge.Context) error {
	jobID := ctx.Param("id")
	if jobID == "" {
		return ctx.JSON(http.StatusBadRequest, map[string]interface{}{
			"error": "job ID is required",
		})
	}

	executionID, err := h.extension.TriggerJob(ctx.Request().Context(), jobID)
	if err != nil {
		if err == cronext.ErrJobNotFound {
			return ctx.JSON(http.StatusNotFound, map[string]interface{}{
				"error": "job not found",
			})
		}
		if err == cronext.ErrJobDisabled {
			return ctx.JSON(http.StatusBadRequest, map[string]interface{}{
				"error": "job is disabled",
			})
		}
		if err == cronext.ErrJobRunning {
			return ctx.JSON(http.StatusConflict, map[string]interface{}{
				"error": "job is already running",
			})
		}
		return ctx.JSON(http.StatusInternalServerError, map[string]interface{}{
			"error": err.Error(),
		})
	}

	return ctx.JSON(http.StatusOK, map[string]interface{}{
		"message":      "job triggered successfully",
		"execution_id": executionID,
	})
}

// EnableJob enables a job.
func (h *APIHandler) EnableJob(ctx forge.Context) error {
	jobID := ctx.Param("id")
	if jobID == "" {
		return ctx.JSON(http.StatusBadRequest, map[string]interface{}{
			"error": "job ID is required",
		})
	}

	enabled := true
	update := &cronext.JobUpdate{
		Enabled: &enabled,
	}

	if err := h.extension.UpdateJob(ctx.Request().Context(), jobID, update); err != nil {
		if err == cronext.ErrJobNotFound {
			return ctx.JSON(http.StatusNotFound, map[string]interface{}{
				"error": "job not found",
			})
		}
		return ctx.JSON(http.StatusInternalServerError, map[string]interface{}{
			"error": err.Error(),
		})
	}

	return ctx.JSON(http.StatusOK, map[string]interface{}{
		"message": "job enabled successfully",
	})
}

// DisableJob disables a job.
func (h *APIHandler) DisableJob(ctx forge.Context) error {
	jobID := ctx.Param("id")
	if jobID == "" {
		return ctx.JSON(http.StatusBadRequest, map[string]interface{}{
			"error": "job ID is required",
		})
	}

	disabled := false
	update := &cronext.JobUpdate{
		Enabled: &disabled,
	}

	if err := h.extension.UpdateJob(ctx.Request().Context(), jobID, update); err != nil {
		if err == cronext.ErrJobNotFound {
			return ctx.JSON(http.StatusNotFound, map[string]interface{}{
				"error": "job not found",
			})
		}
		return ctx.JSON(http.StatusInternalServerError, map[string]interface{}{
			"error": err.Error(),
		})
	}

	return ctx.JSON(http.StatusOK, map[string]interface{}{
		"message": "job disabled successfully",
	})
}

// GetJobExecutions retrieves execution history for a job.
func (h *APIHandler) GetJobExecutions(ctx forge.Context) error {
	jobID := ctx.Param("id")
	if jobID == "" {
		return ctx.JSON(http.StatusBadRequest, map[string]interface{}{
			"error": "job ID is required",
		})
	}

	limit := 50
	if limitStr := ctx.Query("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}
	}

	storage := h.extension.GetStorage()
	filter := &cronext.ExecutionFilter{
		JobID: jobID,
		Limit: limit,
	}

	executions, err := storage.ListExecutions(ctx.Request().Context(), filter)
	if err != nil {
		return ctx.JSON(http.StatusInternalServerError, map[string]interface{}{
			"error": err.Error(),
		})
	}

	return ctx.JSON(http.StatusOK, map[string]interface{}{
		"executions": executions,
		"count":      len(executions),
	})
}

// ListExecutions lists all executions.
func (h *APIHandler) ListExecutions(ctx forge.Context) error {
	limit := 50
	if limitStr := ctx.Query("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}
	}

	filter := &cronext.ExecutionFilter{
		Limit: limit,
	}

	// Parse status filter
	if statusStr := ctx.Query("status"); statusStr != "" {
		filter.Status = []cronext.ExecutionStatus{cronext.ExecutionStatus(statusStr)}
	}

	storage := h.extension.GetStorage()
	executions, err := storage.ListExecutions(ctx.Request().Context(), filter)
	if err != nil {
		return ctx.JSON(http.StatusInternalServerError, map[string]interface{}{
			"error": err.Error(),
		})
	}

	return ctx.JSON(http.StatusOK, map[string]interface{}{
		"executions": executions,
		"count":      len(executions),
	})
}

// GetExecution retrieves a single execution.
func (h *APIHandler) GetExecution(ctx forge.Context) error {
	executionID := ctx.Param("id")
	if executionID == "" {
		return ctx.JSON(http.StatusBadRequest, map[string]interface{}{
			"error": "execution ID is required",
		})
	}

	storage := h.extension.GetStorage()
	execution, err := storage.GetExecution(ctx.Request().Context(), executionID)
	if err != nil {
		if err == cronext.ErrExecutionNotFound {
			return ctx.JSON(http.StatusNotFound, map[string]interface{}{
				"error": "execution not found",
			})
		}
		return ctx.JSON(http.StatusInternalServerError, map[string]interface{}{
			"error": err.Error(),
		})
	}

	return ctx.JSON(http.StatusOK, map[string]interface{}{
		"execution": execution,
	})
}

// GetStats retrieves scheduler statistics.
func (h *APIHandler) GetStats(ctx forge.Context) error {
	stats, err := h.extension.GetStats(ctx.Request().Context())
	if err != nil {
		return ctx.JSON(http.StatusInternalServerError, map[string]interface{}{
			"error": err.Error(),
		})
	}

	return ctx.JSON(http.StatusOK, stats)
}

// GetJobStats retrieves statistics for a specific job.
func (h *APIHandler) GetJobStats(ctx forge.Context) error {
	jobID := ctx.Param("id")
	if jobID == "" {
		return ctx.JSON(http.StatusBadRequest, map[string]interface{}{
			"error": "job ID is required",
		})
	}

	storage := h.extension.GetStorage()
	stats, err := storage.GetJobStats(ctx.Request().Context(), jobID)
	if err != nil {
		if err == cronext.ErrJobNotFound {
			return ctx.JSON(http.StatusNotFound, map[string]interface{}{
				"error": "job not found",
			})
		}
		return ctx.JSON(http.StatusInternalServerError, map[string]interface{}{
			"error": err.Error(),
		})
	}

	return ctx.JSON(http.StatusOK, stats)
}

// Health checks the health of the cron extension.
func (h *APIHandler) Health(ctx forge.Context) error {
	err := h.extension.Health(ctx.Request().Context())
	if err != nil {
		return ctx.JSON(http.StatusServiceUnavailable, map[string]interface{}{
			"status": "unhealthy",
			"error":  err.Error(),
		})
	}

	return ctx.JSON(http.StatusOK, map[string]interface{}{
		"status":    "healthy",
		"timestamp": time.Now().Format(time.RFC3339),
	})
}

