package messaging

import (
	"slices"

	gometrics "github.com/xraph/go-utils/metrics"

	"github.com/xraph/forge/internal/metrics/vec"
)

const (
	clientOperationDuration = "messaging.client.operation.duration"
	clientOperationHelp     = "Duration of messaging operation initiated by a producer or consumer client."
	processDuration         = "messaging.process.duration"
	processHelp             = "Duration of processing operation."
)

// labelNames is the one label set every extension records, so two
// extensions on one registry share series.
var labelNames = []string{
	"messaging.system", "messaging.destination.name", "messaging.operation.name",
	"messaging.operation.type", "error.type",
}

// durationBuckets are the bounds http.server.request.duration uses. Semconv
// recommends the same set for messaging durations.
var durationBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.075, 0.1, 0.25, 0.5, 0.75, 1, 2.5, 5, 7.5, 10}

// durationVec declares a duration histogram the way forge.HistogramVec does.
// The registry keys series by name and labels, so declaring it again on the
// same registry reuses the same series.
func durationVec(m gometrics.Metrics, name, help string) *vec.Vec[gometrics.Histogram] {
	base := slices.Clip([]gometrics.MetricOption{
		gometrics.WithBuckets(durationBuckets...),
		gometrics.WithDescription(help),
		gometrics.WithUnit("s"),
	})

	return vec.New(name, labelNames, func(l map[string]string) gometrics.Histogram {
		return m.Histogram(name, append(base, gometrics.WithLabels(l))...)
	}, gometrics.Histogram(gometrics.NewHistogram(name+"_dropped", base...)))
}
