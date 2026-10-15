package forge

import (
	"context"
	"os"
	"slices"

	"github.com/xraph/forge/internal/profiling"
	"github.com/xraph/forge/internal/shared"
)

// ProfilingConfig configures continuous profiling: pushing profiles to
// Pyroscope, and pprof span labels that link a trace to its CPU profile.
type ProfilingConfig = shared.ProfilingConfig

// buildProfiling resolves the profiling config. Like tracing, it never stops
// the app from booting, and nothing runs until Start.
func buildProfiling(config AppConfig, cm ConfigManager, log Logger, tracingEnabled bool) *profiling.Profiler {
	var fromFile shared.ProfilingConfig
	if cm != nil {
		if err := cm.Bind("profiling", &fromFile); err != nil && cm.HasKey("profiling") {
			log.Warn("profiling: could not read profiling config, using defaults", F("error", err))

			fromFile = shared.ProfilingConfig{}
		}
	}

	settings := profiling.Resolve(profiling.Inputs{
		Code:           config.ProfilingConfig,
		File:           fromFile,
		AppName:        config.Name,
		AppVersion:     config.Version,
		Environment:    config.Environment,
		TracingEnabled: tracingEnabled,
		Getenv:         os.Getenv,
	})

	for _, w := range settings.Warnings {
		log.Warn(w)
	}

	return profiling.New(settings, log)
}

// pprofCPUConflictWarning is logged when push profiling and the pprof
// endpoints are both on. Go has one CPU profiler per process.
const pprofCPUConflictWarning = "profiling: push holds the CPU profiler, so the pprof /profile endpoint answers \"cpu profiling already in use\""

// startProfiler starts push profiling. A failure is logged, never returned:
// telemetry doesn't stop an app from booting.
func (a *app) startProfiler() {
	if a.profiler == nil || !a.profiler.Settings().Push {
		return
	}

	if a.config.EnablePprof && slices.Contains(a.profiler.Settings().ProfileTypes, "cpu") {
		a.logger.Warn(pprofCPUConflictWarning)
	}

	if err := a.profiler.Start(); err != nil {
		a.logger.Error("profiling: push disabled", F("error", err))
	}
}

// stopProfiler uploads the last profiles and stops.
func (a *app) stopProfiler(ctx context.Context) {
	if a.profiler == nil {
		return
	}

	if err := a.profiler.Stop(ctx); err != nil {
		a.logger.Warn("profiling: final upload did not finish", F("error", err))
	}
}
