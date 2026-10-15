package telemetry

import (
	"net/http"

	"go.opentelemetry.io/otel/propagation"
)

// canonicalKeys maps the keys the W3C trace context and baggage propagators
// read to their canonical header form.
var canonicalKeys = map[string]string{
	"traceparent": "Traceparent",
	"tracestate":  "Tracestate",
	"baggage":     "Baggage",
}

// headerCarrier is propagation.HeaderCarrier without the per-lookup cost. The
// propagators ask for lowercase keys, and http.Header.Get allocates a
// canonicalized copy of a non-canonical key on every call: one or two
// allocations per request just to find out a header isn't there.
type headerCarrier http.Header

var _ propagation.TextMapCarrier = headerCarrier(nil)

func (c headerCarrier) Get(key string) string {
	if canonical, ok := canonicalKeys[key]; ok {
		if v := c[canonical]; len(v) > 0 {
			return v[0]
		}

		return ""
	}

	return http.Header(c).Get(key)
}

func (c headerCarrier) Set(key, value string) { http.Header(c).Set(key, value) }

func (c headerCarrier) Keys() []string { return propagation.HeaderCarrier(c).Keys() }
