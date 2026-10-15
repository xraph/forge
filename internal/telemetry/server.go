package telemetry

import (
	"context"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/xraph/forge/internal/logger"
	"github.com/xraph/forge/internal/shared"
)

const instrumentationName = "github.com/xraph/forge"

var knownMethods = map[string]bool{
	http.MethodGet: true, http.MethodHead: true, http.MethodPost: true,
	http.MethodPut: true, http.MethodPatch: true, http.MethodDelete: true,
	http.MethodConnect: true, http.MethodOptions: true, http.MethodTrace: true,
}

// NormalizeMethod returns the semconv method value and, for a method outside
// the standard nine, the original. Arbitrary methods would otherwise create
// one span name and one metric series per value a client invents.
func NormalizeMethod(m string) (method, original string) {
	if knownMethods[m] {
		return m, ""
	}

	return "_OTHER", m
}

// ServerMiddleware starts a server span for every request whose path is not
// excluded. The span is renamed to "METHOD /route/pattern" once the router has
// matched, which it reports through shared.RouteMatch.
func ServerMiddleware(tp trace.TracerProvider, s Settings) func(http.Handler) http.Handler {
	tracer := tp.Tracer(instrumentationName)
	responseHeader := http.CanonicalHeaderKey(s.ResponseHeader)
	serverKind := trace.WithSpanKind(trace.SpanKindServer)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if Excluded(r.URL.Path, s.ExcludePaths) {
				next.ServeHTTP(w, r)

				return
			}

			ctx := otel.GetTextMapPropagator().Extract(r.Context(), headerCarrier(r.Header))
			ctx, match := shared.NewRouteMatch(ctx)

			method, original := NormalizeMethod(r.Method)

			spanName := method
			if method == "_OTHER" {
				spanName = "HTTP"
			}

			// One allocation holds the request's tracing state, the status
			// recorder and the start options included. The options live here
			// because a variadic slice passed through the Tracer interface
			// escapes. The request attributes are captured as strings and only
			// built in finish, once the span is known to be recording, so an
			// unsampled request never builds or copies them.
			st := &serverSpan{match: match, method: method}
			st.rec.ResponseWriter = w
			st.captureRequest(r, original)
			st.startOpts = [...]trace.SpanStartOption{serverKind}

			ctx, st.span = tracer.Start(ctx, spanName, st.startOpts[:]...) //nolint:spancheck // st.finish or st.recoverPanic ends it

			var labelSpanID string

			sc := st.span.SpanContext()
			if sc.IsValid() {
				header, traceID, spanID := traceparentIDs(sc)

				ctx = logger.WithTraceID(ctx, traceID)
				ctx = logger.WithSpanID(ctx, spanID)

				if responseHeader != "" {
					w.Header()[responseHeader] = []string{header}
				}

				// Only a sampled span reaches Tempo, so only it can link to a profile.
				if s.SpanLabels && sc.IsSampled() {
					labelSpanID = spanID

					st.profileID = spanID
				}
			}

			// A stream route ends the span from the router's establishment
			// callback, so it covers the handshake and not the connection's
			// lifetime.
			match.OnEstablished(st.finish)

			defer st.recoverPanic()

			if labelSpanID != "" {
				serveLabelled(ctx, labelSpanID, method, match, func(c context.Context) {
					next.ServeHTTP(&st.rec, r.WithContext(c))
				})
			} else {
				next.ServeHTTP(&st.rec, r.WithContext(ctx))
			}

			if match.Established() {
				return // the span ended at the handshake
			}

			st.finish(st.rec.Status())
		})
	}
}

// serverSpan is the tracing middleware's per-request state. Closures for
// finishing the span and recovering a panic, plus a separate status recorder,
// cost one allocation each.
type serverSpan struct {
	span   trace.Span
	match  *shared.RouteMatch
	method string
	rec    StatusRecorder

	startOpts [1]trace.SpanStartOption

	// Request values captured at start, turned into attributes only when
	// the span is recording.
	original, scheme, path, proto, host, userAgent, profileID string
}

// finish names and ends the span.
func (st *serverSpan) finish(status int) {
	span, match := st.span, st.match

	// An unsampled span drops everything set on it, so skip the work.
	if !span.IsRecording() {
		span.End()

		return
	}

	attrs := st.appendRequestAttributes(make([]attribute.KeyValue, 0, 12))

	if match.Pattern != "" {
		if st.method != "_OTHER" {
			span.SetName(spanName(st.method, match.Pattern))
		}

		attrs = append(attrs, attribute.String("http.route", match.Pattern))
	}

	// The router stamps the kind before it checks the upgrade, so a rejected
	// one (a disallowed Origin, say) still carries it. Only a stream that
	// actually opened is a stream span.
	if match.Kind != shared.KindHTTP && match.Established() {
		attrs = append(attrs, attribute.String("forge.stream.kind", match.Kind.String()))
	}

	attrs = append(attrs, attribute.Int("http.response.status_code", status))

	if status >= http.StatusInternalServerError {
		span.SetStatus(codes.Error, "")

		attrs = append(attrs, attribute.String("error.type", strconv.Itoa(status)))
	}

	span.SetAttributes(attrs...)
	span.End()
}

// recoverPanic records a handler panic on the span and panics on. It must be
// deferred directly, or recover returns nil.
func (st *serverSpan) recoverPanic() {
	p := recover()
	if p == nil {
		return
	}

	if st.match.Established() {
		// The span ended at the handshake.
		panic(p)
	}

	// The request attributes go on at the end, so a panicking request needs
	// them here too.
	if st.span.IsRecording() {
		st.span.SetAttributes(st.appendRequestAttributes(make([]attribute.KeyValue, 0, 8))...)
	}

	st.span.RecordError(fmt.Errorf("panic: %v", p), trace.WithStackTrace(true))
	st.span.SetStatus(codes.Error, "panic")
	st.span.End()

	panic(p)
}

// PropagationMiddleware only extracts the incoming trace context, so a
// traceparent still reaches outbound calls made with HTTPClient when tracing
// is turned off. It starts no span and writes no response header.
func PropagationMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := otel.GetTextMapPropagator().Extract(r.Context(), headerCarrier(r.Header))

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RouteMatchMiddleware only installs a fresh shared.RouteMatch holder, so the
// router can report the matched route to the stream observer. forge.New uses
// it when metrics are on but neither the tracing nor the HTTP metrics
// middleware is installed, since those install the holder otherwise. A path
// under excludePaths gets no holder, the same as the tracing and HTTP metrics
// middleware skip it, so a stream served there is not recorded.
func RouteMatchMiddleware(excludePaths []string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if Excluded(r.URL.Path, excludePaths) {
				next.ServeHTTP(w, r)

				return
			}

			ctx, _ := shared.NewRouteMatch(r.Context())

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func (st *serverSpan) captureRequest(r *http.Request, original string) {
	st.original = original
	st.scheme = "http"

	if r.TLS != nil {
		st.scheme = "https"
	}

	st.path = r.URL.Path
	st.proto = strings.TrimPrefix(r.Proto, "HTTP/")
	st.host = serverAddress(r.Host)
	st.userAgent = r.UserAgent()
}

func (st *serverSpan) appendRequestAttributes(attrs []attribute.KeyValue) []attribute.KeyValue {
	attrs = append(attrs,
		attribute.String("http.request.method", st.method),
		attribute.String("url.scheme", st.scheme),
		attribute.String("url.path", st.path),
		attribute.String("network.protocol.version", st.proto),
	)

	if st.original != "" {
		attrs = append(attrs, attribute.String("http.request.method_original", st.original))
	}

	if st.host != "" {
		attrs = append(attrs, attribute.String("server.address", st.host))
	}

	if st.userAgent != "" {
		attrs = append(attrs, attribute.String("user_agent.original", st.userAgent))
	}

	if st.profileID != "" {
		attrs = append(attrs, attribute.String(profileIDAttribute, st.profileID))
	}

	return attrs
}

// serverAddress drops the port from a Host header value. net.SplitHostPort
// allocates an error for a host with no port, the usual case behind a proxy,
// so it only runs when a port can be present. A value it can't split is kept
// as sent.
func serverAddress(host string) string {
	if !strings.Contains(host, ":") || strings.HasSuffix(host, "]") {
		return host
	}

	if h, _, err := net.SplitHostPort(host); err == nil {
		return h
	}

	return host
}

// traceparentIDs builds the W3C traceparent for a valid span context, the
// same bytes propagation.TraceContext would inject, and returns the trace and
// span IDs as slices of it: one allocation where three strings cost three.
func traceparentIDs(sc trace.SpanContext) (header, traceID, spanID string) {
	tid, sid := sc.TraceID(), sc.SpanID()

	// "00-" trace "-" span "-" flags.
	var buf [3 + 32 + 1 + 16 + 3]byte

	copy(buf[:], "00-")
	hex.Encode(buf[3:35], tid[:])
	buf[35] = '-'
	hex.Encode(buf[36:52], sid[:])
	copy(buf[52:], "-00")

	if sc.IsSampled() {
		buf[54] = '1'
	}

	header = string(buf[:])

	return header, header[3:35], header[36:52]
}

// Excluded reports whether path starts with any of prefixes. The tracing and
// HTTP metrics middleware use it to skip paths such as "/_/".
func Excluded(path string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(path, p) {
			return true
		}
	}

	return false
}
