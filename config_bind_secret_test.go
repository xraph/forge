package forge

import (
	"fmt"
	"strings"
	"testing"
)

// A value that fails to bind is often a secret in the wrong place, such as a
// bearer token where a headers map belongs. The warning used to print it.
func TestBindConfigDoesNotLogInvalidValues(t *testing.T) {
	const secret = "sk_live_s3cr3t"

	cm := NewDefaultConfigManager(captureLogger(t), NewNoOpMetrics(), nil)
	cm.Set("tracing", map[string]any{
		"headers":      "authorization=Bearer " + secret,
		"sample_ratio": secret + "_in_wrong_key",
	})

	log := captureLogger(t)

	var cfg TracingConfig

	bad, err := bindConfig(cm, "tracing", &cfg, log)
	if err != nil {
		t.Fatal(err)
	}

	if !bad["tracing.sample_ratio"] {
		t.Fatalf("bad = %v, want tracing.sample_ratio reported", bad)
	}

	warnings := 0

	for _, e := range log.GetLogs() {
		if e.Message == invalidValueMsg {
			warnings++
		}

		for _, f := range e.Fields {
			if strings.Contains(fmt.Sprint(f.Value()), secret) {
				t.Errorf("log %q field %q carries the invalid value", e.Message, f.Key())
			}
		}
	}

	if warnings != len(bad) {
		t.Errorf("got %d invalid-value warnings for %d bad keys", warnings, len(bad))
	}
}
