package grpc

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/xraph/forge"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// buildUnaryInterceptors creates chain of unary interceptors.
func (s *grpcServer) buildUnaryInterceptors() []grpc.UnaryServerInterceptor {
	var interceptors []grpc.UnaryServerInterceptor

	// Log correlation first, so logging, metrics and custom interceptors all
	// see the trace and span IDs.
	if s.config.EnableTracing {
		interceptors = append(interceptors, s.logIDsUnaryInterceptor())
	}

	// Logging interceptor
	if s.config.EnableLogging {
		interceptors = append(interceptors, s.loggingUnaryInterceptor())
	}

	// Metrics interceptor
	if s.config.EnableMetrics {
		interceptors = append(interceptors, s.metricsUnaryInterceptor())
	}

	// Add custom interceptors
	s.mu.RLock()
	interceptors = append(interceptors, s.customUnaryInterceptors...)
	s.mu.RUnlock()

	return interceptors
}

// buildStreamInterceptors creates chain of stream interceptors.
func (s *grpcServer) buildStreamInterceptors() []grpc.StreamServerInterceptor {
	var interceptors []grpc.StreamServerInterceptor

	// Log correlation first, so logging, metrics and custom interceptors all
	// see the trace and span IDs.
	if s.config.EnableTracing {
		interceptors = append(interceptors, s.logIDsStreamInterceptor())
	}

	// Logging interceptor
	if s.config.EnableLogging {
		interceptors = append(interceptors, s.loggingStreamInterceptor())
	}

	// Metrics interceptor
	if s.config.EnableMetrics {
		interceptors = append(interceptors, s.metricsStreamInterceptor())
	}

	// Add custom interceptors
	s.mu.RLock()
	interceptors = append(interceptors, s.customStreamInterceptors...)
	s.mu.RUnlock()

	return interceptors
}

// loggingUnaryInterceptor logs all unary RPC calls.
func (s *grpcServer) loggingUnaryInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		start := time.Now()
		logger := s.logger.WithContext(ctx)

		logger.Debug("grpc unary call start",
			forge.F("method", info.FullMethod),
		)

		resp, err := handler(ctx, req)

		duration := time.Since(start)

		if err != nil {
			logger.Error("grpc unary call failed",
				forge.F("method", info.FullMethod),
				forge.F("duration", duration),
				forge.F("error", err),
			)
		} else {
			logger.Debug("grpc unary call complete",
				forge.F("method", info.FullMethod),
				forge.F("duration", duration),
			)
		}

		return resp, err
	}
}

// metricsUnaryInterceptor records metrics for unary RPCs.
func (s *grpcServer) metricsUnaryInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		start := time.Now()

		s.mu.Lock()
		s.stats.RPCsStarted++
		s.mu.Unlock()

		resp, err := handler(ctx, req)

		s.recordCall(info.FullMethod, start, err)

		s.mu.Lock()
		if err != nil {
			s.stats.RPCsFailed++
		} else {
			s.stats.RPCsSucceeded++
		}
		s.mu.Unlock()

		return resp, err
	}
}

// loggingStreamInterceptor logs stream RPCs.
func (s *grpcServer) loggingStreamInterceptor() grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		start := time.Now()
		logger := s.logger.WithContext(ss.Context())

		logger.Debug("grpc stream call start",
			forge.F("method", info.FullMethod),
			forge.F("client_stream", info.IsClientStream),
			forge.F("server_stream", info.IsServerStream),
		)

		err := handler(srv, ss)

		duration := time.Since(start)

		if err != nil {
			logger.Error("grpc stream call failed",
				forge.F("method", info.FullMethod),
				forge.F("duration", duration),
				forge.F("error", err),
			)
		} else {
			logger.Debug("grpc stream call complete",
				forge.F("method", info.FullMethod),
				forge.F("duration", duration),
			)
		}

		return err
	}
}

// metricsStreamInterceptor records metrics for stream RPCs.
func (s *grpcServer) metricsStreamInterceptor() grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		start := time.Now()

		s.mu.Lock()
		s.stats.ActiveStreams++
		s.mu.Unlock()

		err := handler(srv, ss)

		s.mu.Lock()
		s.stats.ActiveStreams--
		s.mu.Unlock()

		s.recordCall(info.FullMethod, start, err)

		return err
	}
}

// recordCall observes rpc.server.call.duration for one finished call. The
// attributes follow OTel semconv v1.43 and match the server span, so a metric
// and its trace share rpc.method and the status code spelling.
func (s *grpcServer) recordCall(fullMethod string, start time.Time, err error) {
	if s.rpcDuration == nil || isInfraMethod(fullMethod) {
		return
	}

	s.rpcDuration.
		With("grpc", strings.TrimPrefix(fullMethod, "/"), canonicalCode(callCode(err))).
		Observe(time.Since(start).Seconds())
}

// callCode is the status code grpc-go sends for err. A handler that returns a
// bare context.Canceled or context.DeadlineExceeded is not a status error, but
// the server turns it into Canceled or DeadlineExceeded on the wire, so the
// metric does the same.
func callCode(err error) codes.Code {
	if err == nil {
		return codes.OK
	}

	if _, ok := status.FromError(err); ok {
		return status.Code(err)
	}

	return status.FromContextError(err).Code()
}

// canonicalCode spells a gRPC code the way semconv and otelgrpc do
// (CANCELLED, NOT_FOUND), which codes.Code.String does not (Canceled,
// NotFound). It mirrors otelgrpc's own mapping so the span and the metric
// carry the same rpc.response.status_code.
func canonicalCode(code codes.Code) string {
	switch code {
	case codes.OK:
		return "OK"
	case codes.Canceled:
		return "CANCELLED"
	case codes.Unknown:
		return "UNKNOWN"
	case codes.InvalidArgument:
		return "INVALID_ARGUMENT"
	case codes.DeadlineExceeded:
		return "DEADLINE_EXCEEDED"
	case codes.NotFound:
		return "NOT_FOUND"
	case codes.AlreadyExists:
		return "ALREADY_EXISTS"
	case codes.PermissionDenied:
		return "PERMISSION_DENIED"
	case codes.ResourceExhausted:
		return "RESOURCE_EXHAUSTED"
	case codes.FailedPrecondition:
		return "FAILED_PRECONDITION"
	case codes.Aborted:
		return "ABORTED"
	case codes.OutOfRange:
		return "OUT_OF_RANGE"
	case codes.Unimplemented:
		return "UNIMPLEMENTED"
	case codes.Internal:
		return "INTERNAL"
	case codes.Unavailable:
		return "UNAVAILABLE"
	case codes.DataLoss:
		return "DATA_LOSS"
	case codes.Unauthenticated:
		return "UNAUTHENTICATED"
	default:
		return "CODE(" + strconv.FormatInt(int64(code), 10) + ")"
	}
}
