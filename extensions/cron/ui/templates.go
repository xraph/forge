package ui

import (
	"net/http"

	"github.com/xraph/forge"
	cronext "github.com/xraph/forge/extensions/cron"
)

// UIHandler provides a web UI for cron job management.
// This is a placeholder implementation that needs to be completed.
type UIHandler struct {
	extension *cronext.Extension
	logger    forge.Logger
}

// NewUIHandler creates a new UI handler.
// TODO: Implement full web UI with HTML templates and assets.
func NewUIHandler(extension *cronext.Extension, logger forge.Logger) *UIHandler {
	return &UIHandler{
		extension: extension,
		logger:    logger,
	}
}

// RegisterRoutes registers UI routes.
func (h *UIHandler) RegisterRoutes(router forge.Router, prefix string) {
	router.GET(prefix, h.Index)
	router.GET(prefix+"/jobs", h.Jobs)
	router.GET(prefix+"/executions", h.Executions)

	h.logger.Warn("Web UI support is minimal - full implementation pending")
}

// Index serves the main dashboard page.
func (h *UIHandler) Index(ctx forge.Context) error {
	html := `<!DOCTYPE html>
<html>
<head>
    <title>Cron Jobs Dashboard</title>
    <style>
        body { font-family: Arial, sans-serif; margin: 20px; }
        h1 { color: #333; }
        .info { background: #f0f0f0; padding: 10px; border-radius: 5px; }
    </style>
</head>
<body>
    <h1>Cron Jobs Dashboard</h1>
    <div class="info">
        <p>Web UI is under development.</p>
        <p>Please use the REST API at <a href="/api/cron/jobs">/api/cron/jobs</a></p>
        <ul>
            <li><a href="/api/cron/jobs">Jobs</a></li>
            <li><a href="/api/cron/executions">Executions</a></li>
            <li><a href="/api/cron/stats">Statistics</a></li>
        </ul>
    </div>
</body>
</html>`

	ctx.Response().Header().Set("Content-Type", "text/html; charset=utf-8")
	return ctx.String(http.StatusOK, html)
}

// Jobs lists all jobs.
func (h *UIHandler) Jobs(ctx forge.Context) error {
	return ctx.Redirect(http.StatusFound, "/api/cron/jobs")
}

// Executions lists all executions.
func (h *UIHandler) Executions(ctx forge.Context) error {
	return ctx.Redirect(http.StatusFound, "/api/cron/executions")
}
