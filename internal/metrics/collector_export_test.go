package metrics

import (
	"testing"

	"github.com/xraph/go-utils/metrics"

	"github.com/xraph/forge/internal/logger"
)

func TestInfluxAndStatsDFormatsAreGone(t *testing.T) {
	m := New(DefaultCollectorConfig(), logger.NewNoopLogger())

	for _, f := range []metrics.ExportFormat{metrics.ExportFormatInflux, metrics.ExportFormatStatsD} {
		if _, err := m.Export(f); err == nil {
			t.Fatalf("Export(%s) succeeded; the formatter should be deleted", f)
		}
	}

	if _, err := m.Export(metrics.ExportFormatPrometheus); err != nil {
		t.Fatalf("Export(prometheus) = %v", err)
	}
}
