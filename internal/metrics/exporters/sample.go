package exporters

// Sample carries one metric's current value in the shape Prometheus exposition
// needs.
//
// The Prometheus bridge used to read a map[string]any snapshot in which every
// counter, histogram and timer was itself a map[string]any and every gauge was a
// boxed float64. Building that snapshot allocated per metric, and the bridge
// then immediately tore each map apart to rebuild its own. An app running many
// extensions registers thousands of metrics between them, so the whole
// structure was built and discarded on every scrape.
//
// A Sample is a value: streaming one costs nothing for a counter or gauge, and
// for a histogram or timer costs only the bucket slice Prometheus itself
// requires.
type Sample struct {
	Kind SampleKind

	// Value is the current value of a counter or gauge.
	Value float64

	// Count and Sum describe the observations behind a histogram or timer.
	Count uint64
	Sum   float64

	// Buckets holds a histogram's per-bucket counts, ascending by upper bound.
	// These are per-bucket, not cumulative; the bridge accumulates them.
	Buckets []Bucket

	// P50, P95 and P99 are a timer's quantiles in seconds.
	P50, P95, P99 float64

	// Raw carries a value a custom collector produced. Custom collectors report
	// untyped maps, so those keep the old path.
	Raw any
}

// SampleKind identifies which fields of a Sample carry meaning.
type SampleKind uint8

const (
	// SampleUnknown is a metric the registry could not classify. The bridge
	// skips it.
	SampleUnknown SampleKind = iota
	SampleCounter
	SampleGauge
	SampleHistogram
	SampleTimer

	// SampleRaw is a value from a custom collector, carried in Sample.Raw.
	SampleRaw
)

// Bucket is one histogram bucket's upper bound and its own count.
type Bucket struct {
	UpperBound float64
	Count      uint64
}

// StreamFunc walks the current metrics, calling yield once per metric. Returning
// false from yield stops the walk.
//
// Implementations must not hold a lock across yield: the bridge writes to a
// channel that Prometheus drains, and blocking there while holding the registry
// lock would stall metric registration.
type StreamFunc func(yield func(key string, sample Sample) bool)
