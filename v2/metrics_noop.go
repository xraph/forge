package forge

import "time"

// noOpMetrics implements Metrics interface with no-op operations
type noOpMetrics struct{}

// NewNoOpMetrics creates a no-op metrics instance for testing
func NewNoOpMetrics() Metrics {
	return &noOpMetrics{}
}

func (m *noOpMetrics) Counter(name string, labels ...string) Counter {
	return &noOpCounter{}
}

func (m *noOpMetrics) Gauge(name string, labels ...string) Gauge {
	return &noOpGauge{}
}

func (m *noOpMetrics) Histogram(name string, labels ...string) Histogram {
	return &noOpHistogram{}
}

func (m *noOpMetrics) Export() ([]byte, error) {
	return []byte{}, nil
}

// noOpCounter implements Counter with no-op operations
type noOpCounter struct{}

func (c *noOpCounter) Inc()                                        {}
func (c *noOpCounter) Add(delta float64)                           {}
func (c *noOpCounter) WithLabels(labels map[string]string) Counter { return c }

// noOpGauge implements Gauge with no-op operations
type noOpGauge struct{}

func (g *noOpGauge) Set(value float64)                         {}
func (g *noOpGauge) Inc()                                      {}
func (g *noOpGauge) Dec()                                      {}
func (g *noOpGauge) Add(delta float64)                         {}
func (g *noOpGauge) WithLabels(labels map[string]string) Gauge { return g }

// noOpHistogram implements Histogram with no-op operations
type noOpHistogram struct{}

func (h *noOpHistogram) Observe(value float64)                         {}
func (h *noOpHistogram) ObserveDuration(start time.Time)               {}
func (h *noOpHistogram) WithLabels(labels map[string]string) Histogram { return h }
