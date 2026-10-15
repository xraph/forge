package grpc

import (
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
)

// ClientDialOptions returns dial options that trace outbound calls and carry
// the caller's trace context. It reads the OpenTelemetry globals when called,
// so call it after forge.New and keep the result.
func ClientDialOptions() []grpc.DialOption {
	return []grpc.DialOption{grpc.WithStatsHandler(otelgrpc.NewClientHandler(otelOptions()...))}
}
