package exporters

import (
	"reflect"
	"testing"
	"time"
)

func TestRawSampleShapes(t *testing.T) {
	tests := []struct {
		name string
		in   any
		want Sample
		ok   bool
	}{
		{"float", 2.5, Sample{Kind: SampleGauge, Value: 2.5}, true},
		{"int64", int64(3), Sample{Kind: SampleGauge, Value: 3}, true},
		{"uint64", uint64(4), Sample{Kind: SampleGauge, Value: 4}, true},
		{"counter map", map[string]any{"_type": "counter", "value": 7.0}, Sample{Kind: SampleCounter, Value: 7}, true},
		{
			"histogram map",
			map[string]any{"count": uint64(3), "sum": 1.5, "buckets": map[float64]uint64{1: 1, 0.5: 2}},
			Sample{Kind: SampleHistogram, Count: 3, Sum: 1.5, Buckets: []Bucket{
				{UpperBound: 0.5, Count: 2},
				{UpperBound: 1, Count: 1},
			}},
			true,
		},
		{
			"timer map",
			map[string]any{
				"count": int64(2), "mean": time.Second,
				"p50": time.Second, "p95": 2 * time.Second, "p99": 3 * time.Second,
			},
			Sample{Kind: SampleTimer, Count: 2, Sum: 2, P50: 1, P95: 2, P99: 3},
			true,
		},
		{"counter map with a string value", map[string]any{"_type": "counter", "value": "seven"}, Sample{}, false},
		{"histogram map without a count", map[string]any{"sum": 1.5, "buckets": map[float64]uint64{1: 1}}, Sample{}, false},
		{"string", "x", Sample{}, false},
		{"empty map", map[string]any{}, Sample{}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := rawSample(tt.in)
			if ok != tt.ok || !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("rawSample(%v) = %+v, %v; want %+v, %v", tt.in, got, ok, tt.want, tt.ok)
			}
		})
	}
}
