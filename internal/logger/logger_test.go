package logger_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/xraph/forge/internal/logger"
)

// BenchmarkLogger compares performance between different logger implementations.
func BenchmarkLogger(b *testing.B) {
	// Test data
	ctx := context.Background()
	ctx = logger.WithRequestID(ctx, "test-request-123")
	ctx = logger.WithUserID(ctx, "test-user-456")

	testFields := []logger.Field{
		logger.String("operation", "benchmark_test"),
		logger.Int("iteration", 1000),
		logger.Duration("elapsed", 100*time.Millisecond),
		logger.Bool("success", true),
	}

	b.Run("NoopLogger", func(b *testing.B) {
		noopLog := logger.NewNoopLogger()
		contextLog := noopLog.WithContext(ctx)

		b.ResetTimer()

		for range b.N {
			contextLog.Info("Benchmark test message", testFields...)
			contextLog.Error("Benchmark error message", append(testFields, logger.Error(errors.New("test error")))...)
		}
	})

	b.Run("ProductionLogger", func(b *testing.B) {
		prodLog := logger.NewProductionLogger()
		contextLog := prodLog.WithContext(ctx)

		b.ResetTimer()

		for range b.N {
			contextLog.Info("Benchmark test message", testFields...)
			contextLog.Error("Benchmark error message", append(testFields, logger.Error(errors.New("test error")))...)
		}

		prodLog.Sync()
	})
}

// TestNoopLogger ensures noop logger implements interface correctly.
func TestNoopLogger(t *testing.T) {
	noopLog := logger.NewNoopLogger()

	// Verify it implements the Logger interface
	var _ logger.Logger = noopLog

	// Test all methods don't panic
	t.Run("BasicLogging", func(t *testing.T) {
		noopLog.Debug("debug message")
		noopLog.Info("info message")
		noopLog.Warn("warn message")
		noopLog.Error("error message")
		// Skip Fatal as it would terminate test

		noopLog.Debugf("debug %s", "formatted")
		noopLog.Infof("info %d", 42)
		noopLog.Warnf("warn %v", true)
		noopLog.Errorf("error %s", "test")
	})

	t.Run("WithMethods", func(t *testing.T) {
		ctx := context.Background()
		ctx = logger.WithRequestID(ctx, "test-123")

		withFieldsLog := noopLog.With(logger.String("key", "value"))
		withContextLog := noopLog.WithContext(ctx)
		namedLog := noopLog.Named("test")

		// Verify they return loggers (should be noop instances)
		var (
			_ logger.Logger = withFieldsLog
			_ logger.Logger = withContextLog
			_ logger.Logger = namedLog
		)

		// Test chaining
		chainedLog := noopLog.With(logger.String("k1", "v1")).
			WithContext(ctx).
			Named("chained").
			With(logger.String("k2", "v2"))

		chainedLog.Info("This won't log anything")
	})

	t.Run("Sugar", func(t *testing.T) {
		sugar := noopLog.Sugar()

		var _ logger.SugarLogger = sugar

		sugar.Infow("info with fields", "key1", "value1", "key2", 42)
		sugar.Errorw("error with fields", "error", "test error")

		chainedSugar := sugar.With("persistent", "field")
		chainedSugar.Debugw("debug message", "additional", "field")
	})

	t.Run("Sync", func(t *testing.T) {
		err := noopLog.Sync()
		if err != nil {
			t.Errorf("Sync should not return error, got: %v", err)
		}
	})
}

// TestLoggerInterface ensures all logger implementations satisfy the interface.
func TestLoggerInterface(t *testing.T) {
	testCases := []struct {
		name   string
		logger logger.Logger
	}{
		{"NoopLogger", logger.NewNoopLogger()},
		{"DevelopmentLogger", logger.NewDevelopmentLogger()},
		{"ProductionLogger", logger.NewProductionLogger()},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Verify interface compliance
			var _ logger.Logger = tc.logger

			// Test that methods don't panic (except Fatal)
			tc.logger.Debug("test debug")
			tc.logger.Info("test info")
			tc.logger.Warn("test warn")
			tc.logger.Error("test error")

			tc.logger.Debugf("test debug %s", "formatted")
			tc.logger.Infof("test info %d", 42)
			tc.logger.Warnf("test warn %v", true)
			tc.logger.Errorf("test error %s", "formatted")

			// Test With methods
			withFields := tc.logger.With(logger.String("test", "value"))

			var _ logger.Logger = withFields

			ctx := context.Background()
			withContext := tc.logger.WithContext(ctx)

			var _ logger.Logger = withContext

			named := tc.logger.Named("test")

			var _ logger.Logger = named

			// Test Sugar
			sugar := tc.logger.Sugar()

			var _ logger.SugarLogger = sugar

			// Test Sync
			err := tc.logger.Sync()
			// Only check error for non-noop loggers
			if tc.name != "NoopLogger" && err != nil {
				t.Logf("Sync returned error (may be expected): %v", err)
			}
		})
	}
}

// TestContextFields tests context field extraction.
func TestContextFields(t *testing.T) {
	ctx := context.Background()
	ctx = logger.WithRequestID(ctx, "req-123")
	ctx = logger.WithTraceID(ctx, "trace-456")
	ctx = logger.WithUserID(ctx, "user-789")

	// Test field extraction
	requestID := logger.RequestIDFromContext(ctx)
	if requestID != "req-123" {
		t.Errorf("Expected request ID 'req-123', got '%s'", requestID)
	}

	traceID := logger.TraceIDFromContext(ctx)
	if traceID != "trace-456" {
		t.Errorf("Expected trace ID 'trace-456', got '%s'", traceID)
	}

	userID := logger.UserIDFromContext(ctx)
	if userID != "user-789" {
		t.Errorf("Expected user ID 'user-789', got '%s'", userID)
	}

	// Test with nil context
	emptyRequestID := logger.RequestIDFromContext(nil)
	if emptyRequestID != "" {
		t.Errorf("Expected empty request ID from nil context, got '%s'", emptyRequestID)
	}
}

// TestPerformanceMonitor tests performance monitoring with noop logger.
func TestPerformanceMonitor(t *testing.T) {
	noopLog := logger.NewNoopLogger()

	t.Run("BasicMonitoring", func(t *testing.T) {
		pm := logger.NewPerformanceMonitor(noopLog, "test_operation")
		pm.WithField(logger.String("test", "value"))

		time.Sleep(10 * time.Millisecond)

		// Should not panic
		pm.Finish()
	})

	t.Run("ErrorMonitoring", func(t *testing.T) {
		pm := logger.NewPerformanceMonitor(noopLog, "test_operation_with_error")

		time.Sleep(5 * time.Millisecond)

		// Should not panic
		pm.FinishWithError(errors.New("test error"))
	})
}

// TestStructuredLogging tests structured logging with noop logger.
func TestStructuredLogging(t *testing.T) {
	noopLog := logger.NewNoopLogger()

	t.Run("BasicStructured", func(t *testing.T) {
		structured := logger.NewStructuredLog(noopLog)
		structured.WithField(logger.String("key1", "value1")).
			WithFields(logger.String("key2", "value2"), logger.Int("key3", 42)).
			Info("Test message")
	})

	t.Run("WithGroups", func(t *testing.T) {
		structured := logger.NewStructuredLog(noopLog)

		httpGroup := logger.HTTPRequestGroup("GET", "/api/test", "TestAgent/1.0", 200)
		structured.WithGroup(httpGroup).Info("HTTP request")

		dbGroup := logger.DatabaseQueryGroup("SELECT * FROM test", "test_table", 100, 50*time.Millisecond)
		structured.WithGroup(dbGroup).Info("Database query")

		serviceGroup := logger.ServiceInfoGroup("test-service", "1.0.0", "test")
		structured.WithGroup(serviceGroup).Info("Service info")
	})

	t.Run("WithContext", func(t *testing.T) {
		ctx := context.Background()
		ctx = logger.WithRequestID(ctx, "test-req-123")

		structured := logger.NewStructuredLog(noopLog)
		structured.WithContext(ctx).Info("Context message")
	})
}

// BenchmarkFieldCreation compares field creation performance.
func BenchmarkFieldCreation(b *testing.B) {
	b.Run("BasicFields", func(b *testing.B) {
		for i := range b.N {
			fields := []logger.Field{
				logger.String("operation", "benchmark"),
				logger.Int("iteration", i),
				logger.Bool("success", true),
				logger.Duration("elapsed", time.Millisecond),
			}
			_ = fields
		}
	})

	b.Run("LazyFields", func(b *testing.B) {
		for i := range b.N {
			fields := []logger.Field{
				logger.Lazy("timestamp", func() any {
					return time.Now().Unix()
				}),
				logger.Lazy("random", func() any {
					return i * 42
				}),
			}
			_ = fields
		}
	})

	b.Run("ConditionalFields", func(b *testing.B) {
		for i := range b.N {
			fields := []logger.Field{
				logger.Conditional(i%2 == 0, "even", true),
				logger.Conditional(i%3 == 0, "divisible_by_three", true),
				logger.Nullable("value", func() any {
					if i > 100 {
						return i
					}

					return nil
				}()),
			}
			_ = fields
		}
	})
}
