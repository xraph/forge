package profiling

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"time"

	"github.com/grafana/pyroscope-go"

	"github.com/xraph/forge/internal/logger"
)

// The rates Pyroscope's docs suggest. Without them the mutex and block
// profiles stay empty. They are process-wide runtime settings.
const (
	mutexProfileFraction = 5
	blockProfileRate     = 5
)

// Profiler pushes profiles to Pyroscope while the app runs.
type Profiler struct {
	settings   Settings
	log        logger.Logger
	uploadRate time.Duration // zero means the SDK's 15 seconds; tests shorten it

	mu      sync.Mutex
	running *pyroscope.Profiler
}

// New returns a Profiler for s. Nothing runs until Start.
func New(s Settings, log logger.Logger) *Profiler {
	return &Profiler{settings: s, log: log}
}

// Settings returns the resolved settings.
func (p *Profiler) Settings() Settings { return p.settings }

// Running reports whether push is active.
func (p *Profiler) Running() bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.running != nil
}

// Start begins pushing profiles. It does nothing when push isn't configured or
// the profiler already runs. An unreachable Pyroscope is not an error here:
// the SDK uploads in the background and logs what fails.
func (p *Profiler) Start() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.settings.Push || p.running != nil {
		return nil
	}

	types := make([]pyroscope.ProfileType, 0, len(p.settings.ProfileTypes))

	for _, t := range p.settings.ProfileTypes {
		types = append(types, pyroscope.ProfileType(t))

		switch t {
		case "mutex_count", "mutex_duration":
			runtime.SetMutexProfileFraction(mutexProfileFraction)
		case "block_count", "block_duration":
			runtime.SetBlockProfileRate(blockProfileRate)
		}
	}

	running, err := pyroscope.Start(pyroscope.Config{
		ApplicationName:   p.settings.ApplicationName,
		Tags:              p.settings.Tags,
		ServerAddress:     p.settings.ServerAddress,
		BasicAuthUser:     p.settings.BasicAuthUser,
		BasicAuthPassword: p.settings.BasicAuthPassword,
		TenantID:          p.settings.TenantID,
		UploadRate:        p.uploadRate,
		Logger:            newSDKLogger(p.log),
		ProfileTypes:      types,
	})
	if err != nil {
		return fmt.Errorf("profiling: start: %w", err)
	}

	p.running = running

	return nil
}

// Stop uploads what is left and stops. pyroscope's Stop takes no context, so
// it runs on its own goroutine, and Stop returns ctx.Err() if ctx ends first.
// The upload then gives up on the SDK's 30 second HTTP timeout.
func (p *Profiler) Stop(ctx context.Context) error {
	p.mu.Lock()
	running := p.running
	p.running = nil
	p.mu.Unlock()

	if running == nil {
		return nil
	}

	done := make(chan error, 1)

	go func() { done <- running.Stop() }()

	select {
	case err := <-done:
		if err != nil {
			return fmt.Errorf("profiling: stop: %w", err)
		}

		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
