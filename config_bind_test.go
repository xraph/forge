package forge

import (
	"context"
	"reflect"
	"testing"

	"github.com/xraph/forge/internal/logger"
)

const invalidValueMsg = "config: ignoring invalid value, using the default"

func captureLogger(t *testing.T) *logger.TestLogger {
	t.Helper()

	log, ok := logger.NewTestLogger().(*logger.TestLogger)
	if !ok {
		t.Fatal("logger.NewTestLogger did not return a *logger.TestLogger")
	}

	return log
}

// warnedKeys returns the key of every invalid-value warning, in order.
func warnedKeys(log *logger.TestLogger) []string {
	var keys []string

	for _, e := range log.GetLogsByLevel("WARN") {
		if e.Message != invalidValueMsg {
			continue
		}

		k, _ := e.Field("key")
		keys = append(keys, k.(string))
	}

	return keys
}

func TestBindConfigWarnsAndReportsInvalidValues(t *testing.T) {
	cmLog := captureLogger(t)
	cm := NewDefaultConfigManager(cmLog, NewNoOpMetrics(), nil)
	cm.Set("x", map[string]any{"max_size": "big", "tool_prefix": "shop_"})

	log := captureLogger(t)
	cfg := legacyConfig{MaxSize: 10}

	bad, err := bindConfig(cm, "x", &cfg, log)
	if err != nil {
		t.Fatal(err)
	}

	if !bad["x.max_size"] || len(bad) != 1 {
		t.Fatalf("bad = %v, want only x.max_size", bad)
	}

	if cfg.MaxSize != 10 || cfg.ToolPrefix != "shop_" {
		t.Fatalf("bound %+v", cfg)
	}

	warns := log.GetLogsByLevel("WARN")
	if len(warns) != 1 || warns[0].Message != invalidValueMsg {
		t.Fatalf("warnings = %+v", warns)
	}

	for field, want := range map[string]any{"key": "x.max_size", "value": "big", "expected": "int"} {
		if v, _ := warns[0].Field(field); v != want {
			t.Fatalf("%s = %#v, want %#v", field, v, want)
		}
	}

	// confy's own warning must not fire as well: one warning per key.
	if n := len(cmLog.GetLogsByLevel("WARN")); n != 0 {
		t.Fatalf("confy also logged %d warnings", n)
	}
}

// plainManager hides BindStrict, like a mock ConfigManager would.
type plainManager struct{ ConfigManager }

func TestBindConfigFallsBackToBind(t *testing.T) {
	cm := NewDefaultConfigManager(logger.NewTestLogger(), NewNoOpMetrics(), nil)
	cm.Set("x", map[string]any{"max_size": 50})

	cfg := legacyConfig{MaxSize: 10}

	bad, err := bindConfig(plainManager{cm}, "x", &cfg, captureLogger(t))
	if err != nil || len(bad) != 0 || cfg.MaxSize != 50 {
		t.Fatalf("bad=%v err=%v cfg=%+v", bad, err, cfg)
	}
}

func TestBindConfigPassesThroughOtherErrors(t *testing.T) {
	cm := NewDefaultConfigManager(logger.NewTestLogger(), NewNoOpMetrics(), nil)

	var cfg legacyConfig

	if _, err := bindConfig(cm, "missing", &cfg, captureLogger(t)); err == nil {
		t.Fatal("a missing key should still return Bind's error")
	}
}

func TestHealthConfigWarnsOnInvalidValue(t *testing.T) {
	cm := NewDefaultConfigManager(logger.NewTestLogger(), NewNoOpMetrics(), nil)
	cm.Set("health", map[string]any{"enabled": true, "performance": map[string]any{"history_size": "lots"}})

	log := captureLogger(t)
	app := New(WithAppName("bad-health"), WithAppLogger(log), WithAppConfigManager(cm))

	want := []string{"health.performance.history_size"}
	if keys := warnedKeys(log); !reflect.DeepEqual(keys, want) {
		t.Fatalf("after New: warned for %v, want exactly %v", keys, want)
	}

	// Start re-reads the same config through reloadConfigsFromManager. It
	// must not warn about the same value a second time.
	if err := app.Start(t.Context()); err != nil {
		t.Fatalf("app.Start: %v", err)
	}

	t.Cleanup(func() { _ = app.Stop(context.Background()) })

	if keys := warnedKeys(log); !reflect.DeepEqual(keys, want) {
		t.Fatalf("after Start: warned for %v, want exactly %v", keys, want)
	}
}

func TestProfilingIgnoresInvalidSpanLabels(t *testing.T) {
	cm := NewDefaultConfigManager(logger.NewTestLogger(), NewNoOpMetrics(), nil)
	cm.Set("profiling", map[string]any{"span_labels": "maybe", "server_address": "http://pyroscope:4040"})

	log := captureLogger(t)
	settings := buildProfiling(AppConfig{Name: "svc"}, cm, log, true).Settings()

	if settings.ServerAddress != "http://pyroscope:4040" {
		t.Fatalf("ServerAddress = %q, the good key in the same block should still apply", settings.ServerAddress)
	}

	if !settings.SpanLabels {
		t.Fatal("SpanLabels = false, want the default (on with push and tracing), not a bad value read as false")
	}

	if keys := warnedKeys(log); !reflect.DeepEqual(keys, []string{"profiling.span_labels"}) {
		t.Fatalf("warned for %v, want [profiling.span_labels]", keys)
	}
}
