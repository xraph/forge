package plugins

import (
	"context"
	"time"

	"github.com/xraph/forge/jobs"
	"github.com/xraph/forge/logger"
)

type pluginJobHandler struct {
	plugin Plugin
	def    *JobDefinition
	l      logger.Logger
}

func newPluginJobHandler(plugin Plugin, jobDef *JobDefinition, l logger.Logger) jobs.JobHandler {
	return &pluginJobHandler{def: jobDef, l: l}
}

func (p *pluginJobHandler) Handle(ctx context.Context, job jobs.Job) (interface{}, error) {
	// Add plugin context
	ctx = context.WithValue(ctx, "plugin", p.plugin.Name())
	ctx = context.WithValue(ctx, "job", p.def.Name)

	// Track job execution
	start := time.Now()
	defer func() {
		duration := time.Since(start)
		p.l.Debug("Plugin job executed",
			logger.String("plugin", p.plugin.Name()),
			logger.String("job", p.def.Name),
			logger.Duration("duration", duration),
		)
	}()

	// Handle panics
	defer func() {
		if rec := recover(); rec != nil {
			p.l.Error("Plugin job panicked",
				logger.String("plugin", p.plugin.Name()),
				logger.String("job", p.def.Name),
				logger.Any("panic", rec),
			)

			if basePlugin, ok := p.plugin.(*BasePlugin); ok {
				basePlugin.IncrementErrorCount()
			}
		}
	}()

	// Execute job
	return p.def.Handler(ctx, job)
}

func (p *pluginJobHandler) CanHandle(jobType string) bool {
	return jobType == p.def.Type
}

func (p *pluginJobHandler) GetTimeout() time.Duration {
	return 1 * time.Minute
}

func (p *pluginJobHandler) GetRetryPolicy() jobs.RetryPolicy {
	return jobs.RetryPolicy{
		MaxRetries: 3,
	}
}
