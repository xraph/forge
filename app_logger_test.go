package forge

import "testing"

// The old switch sent every environment other than "development" and
// "production" to a noop logger, so ENVIRONMENT=staging silently discarded
// every log line.
func TestUnknownEnvironmentStillGetsARealLogger(t *testing.T) {
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
	want := NewNoopLogger()
	app := NewApp(AppConfig{Name: "t", Environment: "production", Logger: want})
	if got := app.Logger(); got != want {
		t.Errorf("Logger() = %T, want the injected logger", got)
	}
}
