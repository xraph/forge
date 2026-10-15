package collectors

import (
	"context"
	"encoding/hex"
	"net/http"
	"strconv"
	"time"

	"github.com/xraph/go-utils/metrics"
	"go.opentelemetry.io/otel/trace"

	"github.com/xraph/forge/internal/metrics/vec"
	"github.com/xraph/forge/internal/shared"
	"github.com/xraph/forge/internal/telemetry"
)

var (
	durationBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.075, 0.1, 0.25, 0.5, 0.75, 1, 2.5, 5, 7.5, 10}
	sizeBuckets     = []float64{64, 256, 1024, 4096, 16384, 65536, 262144, 1048576, 4194304, 16777216}

	requestLabels = []string{"http.request.method", "http.route", "http.response.status_code", "url.scheme", "error.type"}
	activeLabels  = []string{"http.request.method", "url.scheme"}
)

// HTTPServerOptions configures the HTTP server metrics.
type HTTPServerOptions struct {
	// ExcludePaths lists path prefixes that are not recorded at all.
	ExcludePaths []string

	// BodySizes turns on http.server.request.body.size and
	// http.server.response.body.size.
	BodySizes bool

	// FreshRouteMatch makes the middleware install its own shared.RouteMatch
	// holder instead of reusing one already on the context. Set it when this
	// middleware is the outermost one (tracing disabled), so a nested
	// in-process dispatch never reuses the outer request's holder. Leave it
	// false behind the tracing middleware, which installs the holder both
	// read.
	FreshRouteMatch bool
}

// HTTPServerMetrics records the OTel HTTP server semantic-convention metrics.
type HTTPServerMetrics struct {
	opts     HTTPServerOptions
	duration *vec.Vec[metrics.Histogram]
	active   *vec.Vec[metrics.Gauge]
	reqSize  *vec.Vec[metrics.Histogram]
	respSize *vec.Vec[metrics.Histogram]
}

// NewHTTPServerMetrics declares the metrics on m.
func NewHTTPServerMetrics(m metrics.Metrics, o HTTPServerOptions) *HTTPServerMetrics {
	h := &HTTPServerMetrics{opts: o}

	h.duration = histogramVec(m, "http.server.request.duration", "s", "Duration of HTTP server requests.", durationBuckets)
	h.active = vec.New("http.server.active_requests", activeLabels, func(l map[string]string) metrics.Gauge {
		return m.Gauge("http.server.active_requests", metrics.WithLabels(l), metrics.WithUnit("{request}"),
			metrics.WithDescription("Number of active HTTP server requests."))
	}, metrics.Gauge(metrics.NewGauge("http.server.active_requests.dropped")))

	if o.BodySizes {
		h.reqSize = histogramVec(m, "http.server.request.body.size", "By", "Size of HTTP server request bodies.", sizeBuckets)
		h.respSize = histogramVec(m, "http.server.response.body.size", "By", "Size of HTTP server response bodies.", sizeBuckets)
	}

	return h
}

func histogramVec(m metrics.Metrics, name, unit, help string, buckets []float64) *vec.Vec[metrics.Histogram] {
	return vec.New(name, requestLabels, func(l map[string]string) metrics.Histogram {
		return m.Histogram(name, metrics.WithLabels(l), metrics.WithUnit(unit),
			metrics.WithBuckets(buckets...), metrics.WithDescription(help))
	}, metrics.Histogram(metrics.NewHistogram(name+".dropped")))
}

// Middleware records one observation per request. It reads the matched route
// from shared.RouteMatch and the span, for exemplars, from the context the
// tracing middleware set up.
func (h *HTTPServerMetrics) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if telemetry.Excluded(r.URL.Path, h.opts.ExcludePaths) {
				next.ServeHTTP(w, r)

				return
			}

			ctx, match := h.routeMatch(r)
			method, _ := telemetry.NormalizeMethod(r.Method)

			scheme := "http"
			if r.TLS != nil {
				scheme = "https"
			}

			// One allocation holds everything the request needs, the status
			// recorder included. Separate closures for decrementing and
			// recording cost one allocation each.
			req := &serverRequest{
				h:      h,
				r:      r,
				match:  match,
				method: method,
				scheme: scheme,
				active: h.active.With(method, scheme),
				rec:    telemetry.StatusRecorder{ResponseWriter: w},
			}

			req.active.Inc()
			defer req.finishActive()

			req.start = time.Now()

			// A stream route is recorded once, at the handshake. Its
			// connection can stay open for hours, so the request neither
			// counts as active nor reports a duration after that.
			match.OnEstablished(req.established)

			// Behind the tracing middleware the holder is already on the
			// context, and copying the request would buy nothing.
			inner := r
			if ctx != r.Context() {
				inner = r.WithContext(ctx)
			}

			next.ServeHTTP(&req.rec, inner)

			if match.Established() {
				return
			}

			req.record(req.rec.Status(), req.rec.BytesWritten())
		})
	}
}

// serverRequest is the per-request state of the HTTP server metrics.
type serverRequest struct {
	h      *HTTPServerMetrics
	r      *http.Request
	match  *shared.RouteMatch
	method string
	scheme string
	active metrics.Gauge
	start  time.Time
	rec    telemetry.StatusRecorder

	decremented bool
}

// finishActive takes the request off the active gauge, once.
func (s *serverRequest) finishActive() {
	if !s.decremented {
		s.decremented = true

		s.active.Dec()
	}
}

func (s *serverRequest) established(status int) {
	s.record(status, 0)
	s.finishActive()
}

func (s *serverRequest) record(status int, bytesWritten int64) {
	elapsed := time.Since(s.start).Seconds()

	code := statusCode(status)

	errType := ""
	if status >= http.StatusInternalServerError {
		errType = code
	}

	values := [...]string{s.method, s.match.Pattern, code, s.scheme, errType}

	observe(s.h.duration.With(values[:]...), elapsed, s.r)

	if s.h.reqSize != nil {
		if s.r.ContentLength > 0 {
			s.h.reqSize.With(values[:]...).Observe(float64(s.r.ContentLength))
		}

		s.h.respSize.With(values[:]...).Observe(float64(bytesWritten))
	}
}

// statusCodes holds the decimal text of every status from 100 to 599, so
// labelling a response doesn't format an integer on every request.
var statusCodes = func() (t [600]string) {
	for code := 100; code < len(t); code++ {
		t[code] = strconv.Itoa(code)
	}

	return t
}()

func statusCode(status int) string {
	if status >= 100 && status < len(statusCodes) {
		return statusCodes[status]
	}

	return strconv.Itoa(status)
}

func (h *HTTPServerMetrics) routeMatch(r *http.Request) (context.Context, *shared.RouteMatch) {
	if h.opts.FreshRouteMatch {
		return shared.NewRouteMatch(r.Context())
	}

	return shared.EnsureRouteMatch(r.Context())
}

func observe(hist metrics.Histogram, v float64, r *http.Request) {
	sc := trace.SpanContextFromContext(r.Context())
	if !sc.IsSampled() {
		hist.Observe(v)

		return
	}

	traceID, spanID := exemplarIDs(sc)

	// ObserveWithExemplar stamps Value and Timestamp itself.
	hist.ObserveWithExemplar(v, metrics.Exemplar{TraceID: traceID, SpanID: spanID})
}

// exemplarIDs hex-encodes both IDs into one string and slices it, one
// allocation where TraceID.String and SpanID.String cost one each.
func exemplarIDs(sc trace.SpanContext) (traceID, spanID string) {
	tid, sid := sc.TraceID(), sc.SpanID()

	var buf [2*len(tid) + 2*len(sid)]byte

	hex.Encode(buf[:2*len(tid)], tid[:])
	hex.Encode(buf[2*len(tid):], sid[:])

	ids := string(buf[:])

	return ids[:2*len(tid)], ids[2*len(tid):]
}
