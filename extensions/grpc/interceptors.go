package grpc

import (
	"context"
	"time"

	"github.com/xraph/forge"
	"google.golang.org/grpc"
	"google.golang.org/grpc/status"
)

// buildUnaryInterceptors creates chain of unary interceptors
func (s *grpcServer) buildUnaryInterceptors() []grpc.UnaryServerInterceptor {
	var interceptors []grpc.UnaryServerInterceptor

	// Logging interceptor
	if s.config.EnableLogging {
		interceptors = append(interceptors, s.loggingUnaryInterceptor())
	}

	// Metrics interceptor
	if s.config.EnableMetrics {
		interceptors = append(interceptors, s.metricsUnaryInterceptor())
	}

	// Tracing interceptor
	if s.config.EnableTracing {
		interceptors = append(interceptors, s.tracingUnaryInterceptor())
	}

	// Add custom interceptors
	s.mu.RLock()
	interceptors = append(interceptors, s.customUnaryInterceptors...)
	s.mu.RUnlock()

	return interceptors
}

// buildStreamInterceptors creates chain of stream interceptors
func (s *grpcServer) buildStreamInterceptors() []grpc.StreamServerInterceptor {
	var interceptors []grpc.StreamServerInterceptor

	// Logging interceptor
	if s.config.EnableLogging {
		interceptors = append(interceptors, s.loggingStreamInterceptor())
	}

	// Metrics interceptor
	if s.config.EnableMetrics {
		interceptors = append(interceptors, s.metricsStreamInterceptor())
	}

	// Tracing interceptor
	if s.config.EnableTracing {
		interceptors = append(interceptors, s.tracingStreamInterceptor())
	}

	// Add custom interceptors
	s.mu.RLock()
	interceptors = append(interceptors, s.customStreamInterceptors...)
	s.mu.RUnlock()

	return interceptors
}

// loggingUnaryInterceptor logs all unary RPC calls
func (s *grpcServer) loggingUnaryInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		start := time.Now()

		s.logger.Debug("grpc unary call start",
			forge.F("method", info.FullMethod),
		)

		resp, err := handler(ctx, req)

		duration := time.Since(start)

		if err != nil {
			s.logger.Error("grpc unary call failed",
				forge.F("method", info.FullMethod),
				forge.F("duration", duration),
				forge.F("error", err),
			)
		} else {
			s.logger.Debug("grpc unary call complete",
				forge.F("method", info.FullMethod),
				forge.F("duration", duration),
			)
		}

		return resp, err
	}
}

// metricsUnaryInterceptor records metrics for unary RPCs
func (s *grpcServer) metricsUnaryInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		start := time.Now()

		// Increment started counter
		s.mu.Lock()
		s.stats.RPCsStarted++
		s.mu.Unlock()

		if s.metrics != nil {
			s.metrics.Counter("grpc_unary_started_total",
				"method", info.FullMethod,
			).Inc()
		}

		resp, err := handler(ctx, req)

		duration := time.Since(start)

		// Record duration
		if s.metrics != nil {
			s.metrics.Histogram("grpc_unary_duration_seconds",
				"method", info.FullMethod,
			).Observe(duration.Seconds())
		}

		// Increment success/failure counter
		if err != nil {
			s.mu.Lock()
			s.stats.RPCsFailed++
			s.mu.Unlock()

			if s.metrics != nil {
				code := status.Code(err)
				s.metrics.Counter("grpc_unary_failed_total",
					"method", info.FullMethod,
					"code", code.String(),
				).Inc()
			}
		} else {
			s.mu.Lock()
			s.stats.RPCsSucceeded++
			s.mu.Unlock()

			if s.metrics != nil {
				s.metrics.Counter("grpc_unary_succeeded_total",
					"method", info.FullMethod,
				).Inc()
			}
		}

		return resp, err
	}
}

// tracingUnaryInterceptor adds tracing to unary RPCs
func (s *grpcServer) tracingUnaryInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		// Extract trace context from metadata
		// Add trace span
		// TODO: Integrate with Forge's tracing system when available
		s.logger.Debug("grpc tracing interceptor", forge.F("method", info.FullMethod))

		return handler(ctx, req)
	}
}

// loggingStreamInterceptor logs stream RPCs
func (s *grpcServer) loggingStreamInterceptor() grpc.StreamServerInterceptor {
	return func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		start := time.Now()

		s.logger.Debug("grpc stream call start",
			forge.F("method", info.FullMethod),
			forge.F("client_stream", info.IsClientStream),
			forge.F("server_stream", info.IsServerStream),
		)

		err := handler(srv, ss)

		duration := time.Since(start)

		if err != nil {
			s.logger.Error("grpc stream call failed",
				forge.F("method", info.FullMethod),
				forge.F("duration", duration),
				forge.F("error", err),
			)
		} else {
			s.logger.Debug("grpc stream call complete",
				forge.F("method", info.FullMethod),
				forge.F("duration", duration),
			)
		}

		return err
	}
}

// metricsStreamInterceptor records metrics for stream RPCs
func (s *grpcServer) metricsStreamInterceptor() grpc.StreamServerInterceptor {
	return func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		start := time.Now()

		// Track active streams
		s.mu.Lock()
		s.stats.ActiveStreams++
		activeStreams := s.stats.ActiveStreams
		s.mu.Unlock()

		if s.metrics != nil {
			s.metrics.Gauge("grpc_stream_active",
				"method", info.FullMethod,
			).Set(float64(activeStreams))
		}

		err := handler(srv, ss)

		// Decrement active streams
		s.mu.Lock()
		s.stats.ActiveStreams--
		s.mu.Unlock()

		duration := time.Since(start)

		if s.metrics != nil {
			s.metrics.Histogram("grpc_stream_duration_seconds",
				"method", info.FullMethod,
			).Observe(duration.Seconds())

			if err != nil {
				code := status.Code(err)
				s.metrics.Counter("grpc_stream_failed_total",
					"method", info.FullMethod,
					"code", code.String(),
				).Inc()
			} else {
				s.metrics.Counter("grpc_stream_succeeded_total",
					"method", info.FullMethod,
				).Inc()
			}
		}

		return err
	}
}

// tracingStreamInterceptor adds tracing to stream RPCs
func (s *grpcServer) tracingStreamInterceptor() grpc.StreamServerInterceptor {
	return func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		// TODO: Integrate with Forge's tracing system
		s.logger.Debug("grpc stream tracing", forge.F("method", info.FullMethod))
		return handler(srv, ss)
	}
}
