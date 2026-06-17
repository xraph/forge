package forge

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/xraph/forge/internal/logger"
)

// recordingExtension records Register and Start calls into a shared log.
type recordingExtension struct {
	*BaseExtension
	log *[]string
	mu  *sync.Mutex
}

func newRecordingExtension(name string, log *[]string, mu *sync.Mutex) *recordingExtension {
	base := NewBaseExtension(name, "1.0.0", "Recording extension "+name)
	return &recordingExtension{
		BaseExtension: base,
		log:           log,
		mu:            mu,
	}
}

func (e *recordingExtension) Register(app App) error {
	e.mu.Lock()
	*e.log = append(*e.log, e.Name()+".register")
	e.mu.Unlock()
	return e.BaseExtension.Register(app)
}

func (e *recordingExtension) Start(ctx context.Context) error {
	e.mu.Lock()
	*e.log = append(*e.log, e.Name()+".start")
	e.mu.Unlock()
	e.MarkStarted()
	return nil
}

func (e *recordingExtension) Stop(ctx context.Context) error {
	e.MarkStopped()
	return nil
}

func (e *recordingExtension) Health(ctx context.Context) error {
	return nil
}

// TestCentralMigrations_SplitPhaseOrder verifies that with CentralMigrations=true,
// all Register calls happen first, then the PhaseAfterRegister hook fires exactly once,
// then all Start calls happen.
func TestCentralMigrations_SplitPhaseOrder(t *testing.T) {
	var log []string
	var mu sync.Mutex

	extA := newRecordingExtension("extA", &log, &mu)
	extB := newRecordingExtension("extB", &log, &mu)

	testLogger := logger.NewTestLogger()
	app := NewApp(AppConfig{
		Name:       "test-central",
		Logger:     testLogger,
		Extensions: []Extension{extA, extB},
		CentralMigrations: true,
	})

	// Register a PhaseAfterRegister probe hook
	err := app.RegisterHookFn(PhaseAfterRegister, "test-probe", func(ctx context.Context, a App) error {
		mu.Lock()
		log = append(log, "hook")
		mu.Unlock()
		return nil
	})
	assert.NoError(t, err)

	ctx := context.Background()
	err = app.Start(ctx)
	assert.NoError(t, err)
	defer app.Stop(ctx) //nolint:errcheck

	// Find partition boundaries
	hookIdx := -1
	for i, entry := range log {
		if entry == "hook" {
			hookIdx = i
			break
		}
	}

	assert.NotEqual(t, -1, hookIdx, "PhaseAfterRegister hook should have fired")

	// Count hook occurrences — must be exactly 1
	hookCount := 0
	for _, entry := range log {
		if entry == "hook" {
			hookCount++
		}
	}
	assert.Equal(t, 1, hookCount, "PhaseAfterRegister hook must fire exactly once")

	// All *.register entries must appear before the hook
	for i, entry := range log[:hookIdx] {
		_ = i
		assert.False(t, strings.HasSuffix(entry, ".start"),
			"No .start should appear before the hook; got %v at index %d (full log: %v)", entry, i, log)
	}

	// All *.start entries must appear after the hook
	for i, entry := range log[hookIdx+1:] {
		_ = i
		assert.False(t, strings.HasSuffix(entry, ".register"),
			"No .register should appear after the hook; got %v (full log: %v)", entry, log)
	}

	// Both extensions must have registered and started
	assert.Contains(t, log, "extA.register")
	assert.Contains(t, log, "extB.register")
	assert.Contains(t, log, "extA.start")
	assert.Contains(t, log, "extB.start")
}

// TestCentralMigrations_DefaultInterleaved verifies that without CentralMigrations,
// the legacy interleaved Register+Start order is preserved: each extension's .register
// is immediately followed by its own .start before the next extension registers.
func TestCentralMigrations_DefaultInterleaved(t *testing.T) {
	var log []string
	var mu sync.Mutex

	extA := newRecordingExtension("extA", &log, &mu)
	extB := newRecordingExtension("extB", &log, &mu)

	testLogger := logger.NewTestLogger()
	app := NewApp(AppConfig{
		Name:       "test-default",
		Logger:     testLogger,
		Extensions: []Extension{extA, extB},
		// CentralMigrations deliberately omitted (false)
	})

	// Register a PhaseAfterRegister probe hook
	err := app.RegisterHookFn(PhaseAfterRegister, "test-probe", func(ctx context.Context, a App) error {
		mu.Lock()
		log = append(log, "hook")
		mu.Unlock()
		return nil
	})
	assert.NoError(t, err)

	ctx := context.Background()
	err = app.Start(ctx)
	assert.NoError(t, err)
	defer app.Stop(ctx) //nolint:errcheck

	// Hook must fire exactly once
	hookCount := 0
	for _, entry := range log {
		if entry == "hook" {
			hookCount++
		}
	}
	assert.Equal(t, 1, hookCount, "PhaseAfterRegister hook must fire exactly once in default mode")

	// Legacy interleaving: a *.start should appear before the second extension's *.register.
	// Find the index of the second .register (whichever extension is second in order).
	secondRegisterIdx := -1
	registerCount := 0
	for i, entry := range log {
		if strings.HasSuffix(entry, ".register") {
			registerCount++
			if registerCount == 2 {
				secondRegisterIdx = i
				break
			}
		}
	}

	if secondRegisterIdx > 0 {
		// There must be at least one .start before the second .register
		startBeforeSecondRegister := false
		for _, entry := range log[:secondRegisterIdx] {
			if strings.HasSuffix(entry, ".start") {
				startBeforeSecondRegister = true
				break
			}
		}
		assert.True(t, startBeforeSecondRegister,
			"In default mode, first extension's .start should appear before second extension's .register (interleaved). log: %v", log)
	}

	// Both extensions must have registered and started
	assert.Contains(t, log, "extA.register")
	assert.Contains(t, log, "extB.register")
	assert.Contains(t, log, "extA.start")
	assert.Contains(t, log, "extB.start")
}

// TestCentralMigrationsEnabled verifies the accessor method.
func TestCentralMigrationsEnabled(t *testing.T) {
	t.Run("FalseByDefault", func(t *testing.T) {
		testLogger := logger.NewTestLogger()
		app := NewApp(AppConfig{Logger: testLogger})
		assert.False(t, app.CentralMigrationsEnabled())
	})

	t.Run("TrueWhenSet", func(t *testing.T) {
		testLogger := logger.NewTestLogger()
		app := NewApp(AppConfig{Logger: testLogger, CentralMigrations: true})
		assert.True(t, app.CentralMigrationsEnabled())
	})

	t.Run("TrueViaOption", func(t *testing.T) {
		testLogger := logger.NewTestLogger()
		config := DefaultAppConfig()
		config.Logger = testLogger
		WithCentralMigrations()(&config)
		app := NewApp(config)
		assert.True(t, app.CentralMigrationsEnabled())
	})
}
