package testing

import (
	stdtesting "testing"

	"github.com/xraph/forge/internal/metrics"
	"github.com/xraph/forge/internal/metrics/vec"
)

func NewNoOpMetrics() metrics.Metrics {
	return metrics.NewNoOpMetrics()
}

// StrictMetrics makes a wrong number of label values in Vec.With fail the
// test instead of logging. Production code drops the sample silently.
func StrictMetrics(t stdtesting.TB) {
	t.Helper()

	restore := vec.SetArityHook(func(name string, want, got int) {
		t.Errorf("metric %q: With got %d label values, want %d", name, got, want)
	})
	t.Cleanup(restore)
}
