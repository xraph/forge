package metrics

import (
	"maps"

	metrics "github.com/xraph/go-utils/metrics"
)

// The registry returns these instead of the bare go-utils metrics. go-utils'
// own WithLabels builds a fresh metric that nothing registers, so its values
// never reach a scrape. These route WithLabels back through the registry,
// which caches the child, exports it and applies the series cap.

func childOpts(opts []metrics.MetricOption, labels map[string]string) []metrics.MetricOption {
	merged := maps.Clone(metrics.ParseTagsOptions(nil, opts...))
	if merged == nil {
		merged = make(map[string]string, len(labels))
	}

	maps.Copy(merged, labels)

	return append(append([]metrics.MetricOption{}, opts...), metrics.WithLabels(merged))
}

type regCounter struct {
	metrics.Counter

	reg  *registry
	name string
	opts []metrics.MetricOption
}

func (c *regCounter) WithLabels(labels map[string]string) metrics.Counter {
	return c.reg.GetOrCreateCounter(c.name, childOpts(c.opts, labels)...)
}

type regGauge struct {
	metrics.Gauge

	reg  *registry
	name string
	opts []metrics.MetricOption
}

func (g *regGauge) WithLabels(labels map[string]string) metrics.Gauge {
	return g.reg.GetOrCreateGauge(g.name, childOpts(g.opts, labels)...)
}

type regHistogram struct {
	metrics.Histogram

	reg  *registry
	name string
	opts []metrics.MetricOption
}

func (h *regHistogram) WithLabels(labels map[string]string) metrics.Histogram {
	return h.reg.GetOrCreateHistogram(h.name, childOpts(h.opts, labels)...)
}

type regTimer struct {
	metrics.Timer

	reg  *registry
	name string
	opts []metrics.MetricOption
}

func (t *regTimer) WithLabels(labels map[string]string) metrics.Timer {
	return t.reg.GetOrCreateTimer(t.name, childOpts(t.opts, labels)...)
}
