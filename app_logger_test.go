package forge

import (
	"testing"

	forgelogger "github.com/xraph/forge/internal/logger"
)

// The old switch sent every environment other than "development" and
// "production" to a noop logger, so ENVIRONMENT=staging silently discarded
// every log line.
func TestUnknownEnvironmentStillGetsARealLogger(t *testing.T) {
	// Force the format. Inside a test binary the logger deliberately resolves
	// to noop (or to pretty under -v), so without this the assertion below
	// would be testing how the suite was invoked rather than the environment
	// switch, and would pass under `go test -v` while failing under plain
	// `go test`. An explicit FORGE_LOG_FORMAT outranks the test-silence rule.
	t.Setenv("FORGE_LOG_FORMAT", "json")

	for _, env := range []string{"staging", "qa", "preview", "", "Production", "DEVELOPMENT"} {
		t.Run(env, func(t *testing.T) {
			app := NewApp(AppConfig{Name: "t", Environment: env})
			l := app.Logger()
			if l == nil {
				t.Fatal("Logger() returned nil")
			}
			if l == NewNoopLogger() {
				t.Errorf("environment %q got a noop logger", env)
			}
			// Must be usable without panicking.
			l.Info("smoke", String("env", env))
		})
	}
}

func TestExplicitLoggerIsNotOverridden(t *testing.T) {
	// Use a capture logger rather than a noop as the sentinel. noopLogger is
	// a zero-field struct, so every noop compares equal to every other one
	// and the assertion could not tell "my instance survived" from "it was
	// replaced by a different noop". The capture logger is a pointer, so
	// identity means something.
	want := forgelogger.NewTestLogger()

	app := NewApp(AppConfig{Name: "t", Environment: "production", Logger: want})
	if got := app.Logger(); got != want {
		t.Errorf("Logger() = %T, want the injected logger", got)
	}
}
