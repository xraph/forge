package internal

import "github.com/xraph/go-utils/metrics"

// durationBuckets are the bounds, in seconds, of every health duration
// histogram. They match the HTTP server collector's.
var durationBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.075, 0.1, 0.25, 0.5, 0.75, 1, 2.5, 5, 7.5, 10}

// DurationBuckets gives a histogram that observes seconds its buckets. Without
// it the histogram gets go-utils' defaults, which run from 0 to 10000 and put
// nearly every observation in the first bucket.
func DurationBuckets() metrics.MetricOption {
	return metrics.WithBuckets(durationBuckets...)
}
