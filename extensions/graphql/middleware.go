package graphql

import (
	"context"
	"time"

	"github.com/99designs/gqlgen/graphql"
	"github.com/xraph/forge"
)

// observabilityMiddleware adds logging and metrics to GraphQL operations
func observabilityMiddleware(logger forge.Logger, metrics forge.Metrics, config Config) graphql.OperationMiddleware {
	return func(ctx context.Context, next graphql.OperationHandler) graphql.ResponseHandler {
		start := time.Now()
		oc := graphql.GetOperationContext(ctx)

		if config.EnableLogging {
			logger.Debug("graphql operation start",
				forge.F("operation", oc.OperationName),
				forge.F("type", oc.Operation.Operation),
			)
		}

		resp := next(ctx)

		duration := time.Since(start)

		// Record metrics
		if config.EnableMetrics && metrics != nil {
			metrics.Histogram("graphql_operation_duration_seconds",
				"operation", oc.OperationName,
				"type", string(oc.Operation.Operation),
			).Observe(duration.Seconds())

			metrics.Counter("graphql_operation_total",
				"operation", oc.OperationName,
				"type", string(oc.Operation.Operation),
			).Inc()
		}

		if config.EnableLogging {
			logger.Debug("graphql operation complete",
				forge.F("operation", oc.OperationName),
				forge.F("duration", duration),
			)
		}

		return resp
	}
}

// responseMiddleware processes GraphQL responses (e.g., logging slow queries)
func responseMiddleware(config Config, logger forge.Logger) graphql.ResponseMiddleware {
	return func(ctx context.Context, next graphql.ResponseHandler) *graphql.Response {
		resp := next(ctx)

		// Log slow queries
		if config.LogSlowQueries {
			oc := graphql.GetOperationContext(ctx)
			duration := time.Since(oc.Stats.OperationStart)
			if duration > config.SlowQueryThreshold {
				logger.Warn("slow query detected",
					forge.F("operation", oc.OperationName),
					forge.F("duration", duration),
					forge.F("query", oc.RawQuery),
				)
			}
		}

		// Log errors
		if config.EnableLogging && resp != nil && len(resp.Errors) > 0 {
			oc := graphql.GetOperationContext(ctx)
			for _, err := range resp.Errors {
				logger.Error("graphql error",
					forge.F("operation", oc.OperationName),
					forge.F("error", err.Message),
					forge.F("path", err.Path),
				)
			}
		}

		return resp
	}
}
