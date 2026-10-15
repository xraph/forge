package metrics

import (
	"errors"
	"testing"

	"github.com/xraph/go-utils/metrics"

	forgeerrors "github.com/xraph/forge/errors"
	"github.com/xraph/forge/internal/logger"
)

func TestInfluxAndStatsDFormatsAreGone(t *testing.T) {
	m := New(DefaultCollectorConfig(), logger.NewNoopLogger())

	for _, f := range []metrics.ExportFormat{metrics.ExportFormatInflux, metrics.ExportFormatStatsD} {
		_, err := m.Export(f)
		if err == nil {
			t.Fatalf("Export(%s) succeeded; the formatter should be deleted", f)
		}

		// ErrServiceNotFound builds an error that carries CodeServiceNotFound, and
		// errors.Is matches it against the sentinel by code.
		if !errors.Is(err, forgeerrors.ErrServiceNotFoundSentinel) {
			t.Fatalf("Export(%s) = %v, want a service-not-found error", f, err)
		}
	}

	if _, err := m.Export(metrics.ExportFormatPrometheus); err != nil {
		t.Fatalf("Export(prometheus) = %v", err)
	}
}
