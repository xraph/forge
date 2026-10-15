package exporters

import (
	"cmp"
	"slices"
	"time"
)

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

	// Unit is the metric's unit as registered ("s", "By", "{request}").
	Unit string

	// Exemplars holds at most one exemplar per bucket, histograms only.
	Exemplars []Exemplar

	// Raw carries a value a custom collector produced. Custom collectors report
	// untyped maps, so those keep the old path.
	Raw any
}

// Exemplar links one histogram observation to the trace that produced it.
type Exemplar struct {
	Value     float64
	TraceID   string
	SpanID    string
	Timestamp time.Time
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

// rawSample reads a value a custom collector reported. Custom collectors
// return untyped values, so the Prometheus bridge and the OTLP producer share
// one reading of the shapes they use: a bare number, a counter map, a
// histogram map with per-bucket counts and a timer map.
func rawSample(v any) (Sample, bool) {
	switch n := v.(type) {
	case float64:
		return Sample{Kind: SampleGauge, Value: n}, true
	case int64:
		return Sample{Kind: SampleGauge, Value: float64(n)}, true
	case uint64:
		return Sample{Kind: SampleGauge, Value: float64(n)}, true
	case map[string]any:
		return rawMapSample(n)
	default:
		return Sample{}, false
	}
}

func rawMapSample(v map[string]any) (Sample, bool) {
	if t, _ := v["_type"].(string); t == "counter" {
		val, ok := toFloat(v["value"])

		return Sample{Kind: SampleCounter, Value: val}, ok
	}

	if raw, ok := v["buckets"].(map[float64]uint64); ok {
		count, countOK := toUint64(v["count"])
		if !countOK {
			return Sample{}, false
		}

		sum, _ := toFloat(v["sum"])

		sample := Sample{Kind: SampleHistogram, Count: count, Sum: sum, Buckets: make([]Bucket, 0, len(raw))}
		for bound, n := range raw {
			sample.Buckets = append(sample.Buckets, Bucket{UpperBound: bound, Count: n})
		}

		slices.SortFunc(sample.Buckets, func(a, b Bucket) int {
			return cmp.Compare(a.UpperBound, b.UpperBound)
		})

		return sample, true
	}

	if _, ok := v["count"]; ok {
		count, _ := toUint64(v["count"])
		sample := Sample{Kind: SampleTimer, Count: count}

		if mean, ok := durationSeconds(v["mean"]); ok {
			sample.Sum = mean * float64(count)
		}

		sample.P50, _ = durationSeconds(v["p50"])
		sample.P95, _ = durationSeconds(v["p95"])
		sample.P99, _ = durationSeconds(v["p99"])

		return sample, true
	}

	return Sample{}, false
}
