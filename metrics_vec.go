package forge

import (
	"github.com/xraph/forge/internal/metrics/vec"
	gometrics "github.com/xraph/go-utils/metrics"
)

// Vec is a metric declared with label names. Fetch a child with With,
// passing values in the order the names were declared.
type Vec[T any] = vec.Vec[T]

// HistogramOpts configures HistogramVec.
type HistogramOpts struct {
	Buckets []float64
	Help    string
}

// CounterVec declares a counter with label names.
//
//	orders := forge.CounterVec(app.Metrics(), "orders_created_total", "tier", "region")
//	orders.With("pro", "eu").Inc()
func CounterVec(m Metrics, name string, labelNames ...string) *Vec[Counter] {
	return vec.New(name, labelNames, func(l map[string]string) Counter {
		return m.Counter(name, gometrics.WithLabels(l))
	}, Counter(gometrics.NewCounter(name+"_dropped")))
}

// GaugeVec declares a gauge with label names.
func GaugeVec(m Metrics, name string, labelNames ...string) *Vec[Gauge] {
	return vec.New(name, labelNames, func(l map[string]string) Gauge {
		return m.Gauge(name, gometrics.WithLabels(l))
	}, Gauge(gometrics.NewGauge(name+"_dropped")))
}

// HistogramVec declares a histogram with label names.
func HistogramVec(m Metrics, name string, opts HistogramOpts, labelNames ...string) *Vec[Histogram] {
	base := []gometrics.MetricOption{}
	if len(opts.Buckets) > 0 {
		base = append(base, gometrics.WithBuckets(opts.Buckets...))
	}

	if opts.Help != "" {
		base = append(base, gometrics.WithDescription(opts.Help))
	}

	return vec.New(name, labelNames, func(l map[string]string) Histogram {
		return m.Histogram(name, append(base, gometrics.WithLabels(l))...)
	}, Histogram(gometrics.NewHistogram(name+"_dropped", base...)))
}
