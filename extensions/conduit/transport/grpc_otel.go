package transport

import (
	"strings"

	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric/noop"
	"google.golang.org/grpc"
	"google.golang.org/grpc/stats"
)

// grpcInfraPrefixes lists health and reflection calls, which get no span.
// The list mirrors the one in the forge gRPC extension, which conduit cannot
// import.
var grpcInfraPrefixes = []string{
	"/grpc.health.v1.Health/",
	"/grpc.reflection.v1.ServerReflection/",
	"/grpc.reflection.v1alpha.ServerReflection/",
}

func isGRPCInfraMethod(fullMethod string) bool {
	for _, p := range grpcInfraPrefixes {
		if strings.HasPrefix(fullMethod, p) {
			return true
		}
	}

	return false
}

// grpcClientTracing returns the dial option that traces outbound calls. It
// reads the OpenTelemetry globals when called, and metrics stay off.
func grpcClientTracing() grpc.DialOption {
	return grpc.WithStatsHandler(otelgrpc.NewClientHandler(
		otelgrpc.WithTracerProvider(otel.GetTracerProvider()),
		otelgrpc.WithPropagators(otel.GetTextMapPropagator()),
		otelgrpc.WithMeterProvider(noop.NewMeterProvider()),
		otelgrpc.WithFilter(func(info *stats.RPCTagInfo) bool { return !isGRPCInfraMethod(info.FullMethodName) }),
	))
}
