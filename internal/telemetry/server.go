package telemetry

import (
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
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

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if excluded(r.URL.Path, s.ExcludePaths) {
				next.ServeHTTP(w, r)

				return
			}

			ctx := otel.GetTextMapPropagator().Extract(r.Context(), propagation.HeaderCarrier(r.Header))
			ctx, match := shared.EnsureRouteMatch(ctx)

			method, original := NormalizeMethod(r.Method)

			spanName := method
			if method == "_OTHER" {
				spanName = "HTTP"
			}

			ctx, span := tracer.Start(ctx, spanName,
				trace.WithSpanKind(trace.SpanKindServer),
				trace.WithAttributes(requestAttributes(r, method, original)...),
			)

			sc := span.SpanContext()
			if sc.IsValid() {
				ctx = logger.WithTraceID(ctx, sc.TraceID().String())
				ctx = logger.WithSpanID(ctx, sc.SpanID().String())

				if s.ResponseHeader != "" {
					carrier := propagation.MapCarrier{}
					propagation.TraceContext{}.Inject(ctx, carrier)

					if v := carrier.Get("traceparent"); v != "" {
						w.Header().Set(s.ResponseHeader, v)
					}
				}
			}

			rec := newStatusRecorder(w)

			defer func() {
				if p := recover(); p != nil {
					span.RecordError(fmt.Errorf("panic: %v", p), trace.WithStackTrace(true))
					span.SetStatus(codes.Error, "panic")
					span.End()

					panic(p)
				}
			}()

			next.ServeHTTP(rec, r.WithContext(ctx))

			status := rec.Status()

			if match.Pattern != "" {
				if method != "_OTHER" {
					span.SetName(method + " " + match.Pattern)
				}

				span.SetAttributes(attribute.String("http.route", match.Pattern))
			}

			span.SetAttributes(attribute.Int("http.response.status_code", status))

			if status >= http.StatusInternalServerError {
				span.SetStatus(codes.Error, "")
				span.SetAttributes(attribute.String("error.type", strconv.Itoa(status)))
			}

			span.End()
		})
	}
}

func requestAttributes(r *http.Request, method, original string) []attribute.KeyValue {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}

	attrs := []attribute.KeyValue{
		attribute.String("http.request.method", method),
		attribute.String("url.scheme", scheme),
		attribute.String("url.path", r.URL.Path),
		attribute.String("network.protocol.version", strings.TrimPrefix(r.Proto, "HTTP/")),
	}

	if original != "" {
		attrs = append(attrs, attribute.String("http.request.method_original", original))
	}

	if host := r.Host; host != "" {
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}

		attrs = append(attrs, attribute.String("server.address", host))
	}

	if ua := r.UserAgent(); ua != "" {
		attrs = append(attrs, attribute.String("user_agent.original", ua))
	}

	return attrs
}

func excluded(path string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(path, p) {
			return true
		}
	}

	return false
}
