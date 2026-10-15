package grpc

import (
	"context"
	"strings"

	"github.com/xraph/forge"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/stats"
)

var infraPrefixes = []string{
	"/grpc.health.v1.Health/",
	"/grpc.reflection.v1.ServerReflection/",
	"/grpc.reflection.v1alpha.ServerReflection/",
}

// isInfraMethod reports health and reflection calls, which get no span and no
// metric, the way HTTP excludes /_/.
func isInfraMethod(fullMethod string) bool {
	for _, p := range infraPrefixes {
		if strings.HasPrefix(fullMethod, p) {
			return true
		}
	}

	return false
}

// otelOptions builds the otelgrpc options. otelgrpc's own metrics are off
// because the server records rpc.server.call.duration through the forge
// metrics registry.
func otelOptions() []otelgrpc.Option {
	return []otelgrpc.Option{
		otelgrpc.WithTracerProvider(otel.GetTracerProvider()),
		otelgrpc.WithPropagators(otel.GetTextMapPropagator()),
		otelgrpc.WithMeterProvider(noop.NewMeterProvider()),
		otelgrpc.WithFilter(func(info *stats.RPCTagInfo) bool { return !isInfraMethod(info.FullMethodName) }),
	}
}

// serverStatsHandler returns the otelgrpc handler that starts a server span
// for every served call. Stats handlers run before interceptors, so the span
// is on the context by the time any interceptor runs.
func serverStatsHandler() grpc.ServerOption {
	return grpc.StatsHandler(otelgrpc.NewServerHandler(otelOptions()...))
}

// withLogIDs puts the active span's IDs on ctx so a request-scoped logger
// carries trace_id and span_id, as forge's HTTP middleware does.
func withLogIDs(ctx context.Context) context.Context {
	sc := trace.SpanContextFromContext(ctx)
	if !sc.IsValid() {
		return ctx
	}

	ctx = forge.WithTraceID(ctx, sc.TraceID().String())

	return forge.WithSpanID(ctx, sc.SpanID().String())
}

func (s *grpcServer) logIDsUnaryInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		return handler(withLogIDs(ctx), req)
	}
}

type ctxStream struct {
	grpc.ServerStream

	ctx context.Context //nolint:containedctx // wraps a stream to replace its context
}

func (c *ctxStream) Context() context.Context { return c.ctx }

func (s *grpcServer) logIDsStreamInterceptor() grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, _ *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		return handler(srv, &ctxStream{ServerStream: ss, ctx: withLogIDs(ss.Context())})
	}
}
