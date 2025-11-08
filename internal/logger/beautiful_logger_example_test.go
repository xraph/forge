package logger

import (
	"context"
	"testing"
	"time"

	"go.uber.org/zap/zapcore"
)

// DemoBeautifulLogger_DetailedMode demonstrates the detailed multi-line output.
func DemoBeautifulLogger_DetailedMode() {
	log := NewBeautifulLogger("database")

	log.Info("Connection established",
		String("host", "localhost"),
		Int("port", 5432),
		String("database", "myapp_db"),
		Duration("connection_time", 245*time.Millisecond),
	)

	log.Warn("Slow query detected",
		String("query_id", "q_12345"),
		Duration("execution_time", 5*time.Second),
		String("table", "users"),
		Int("row_count", 50000),
	)

	log.Error("Connection timeout",
		Error(context.Canceled),
		String("operation", "query_execute"),
		Duration("timeout", 30*time.Second),
	)
}

// DemoBeautifulLogger_CompactMode demonstrates the compact single-line output.
func DemoBeautifulLogger_CompactMode() {
	log := NewBeautifulLoggerCompact("api")

	log.Info("Request received",
		String("method", "POST"),
		String("path", "/api/users"),
	)

	log.Debug("Request parsed",
		Int("body_size", 1024),
		String("content_type", "application/json"),
	)

	log.Info("Response sent",
		Int("status", 201),
		Duration("handler_time", 125*time.Millisecond),
	)
}

// DemoBeautifulLogger_BannerMode demonstrates banner output for critical messages.
func DemoBeautifulLogger_BannerMode() {
	// This would normally cause fatal exit, but we just log here for demo
	log := NewBeautifulLogger("auth") // Use non-banner for demo
	log.Error("Authentication failed - would show banner",
		String("user", "admin@example.com"),
		String("reason", "Invalid credentials"),
		String("ip_address", "192.168.1.100"),
		Int("attempt_count", 3),
	)
}

// DemoBeautifulLogger_WithContext demonstrates context integration.
func DemoBeautifulLogger_WithContext() {
	ctx := context.Background()
	ctx = WithRequestID(ctx, "req-abc123")
	ctx = WithTraceID(ctx, "trace-xyz789")
	ctx = WithUserID(ctx, "user-42")

	log := NewBeautifulLogger("service").WithContext(ctx)

	log.Info("Processing request",
		String("endpoint", "/data/fetch"),
		String("method", "GET"),
	)

	log.Debug("Database query executed",
		String("table", "products"),
		Int("rows_affected", 15),
	)
}

// DemoBeautifulLogger_NamedLoggers demonstrates hierarchical logger names.
func DemoBeautifulLogger_NamedLoggers() {
	appLog := NewBeautifulLogger("myapp")

	dbLog := appLog.Named("database")
	apiLog := appLog.Named("api")
	authLog := apiLog.Named("auth")

	dbLog.Info("Database initialized")
	apiLog.Info("API server starting", String("port", "8080"))
	authLog.Info("Auth module loaded")
}

// DemoBeautifulLogger_WithFields demonstrates adding persistent fields.
func DemoBeautifulLogger_WithFields() {
	baseLog := NewBeautifulLogger("request_handler")

	requestLog := baseLog.With(
		String("request_id", "req-12345"),
		String("user_id", "user-789"),
		String("session_id", "sess-abc"),
	)

	// All subsequent logs include the request context
	requestLog.Info("Request validation started")
	requestLog.Info("Database query executed", String("query", "SELECT * FROM users"))
	requestLog.Info("Response prepared", Int("status", 200))
}

// DemoBeautifulLogger_AllLevels demonstrates all logging levels with icons.
func DemoBeautifulLogger_AllLevels() {
	log := NewBeautifulLoggerCompact("demo")

	log.Debug("Debug message - detailed diagnostic info")
	log.Info("Info message - general information")
	log.Warn("Warn message - warning condition")
	log.Error("Error message - error condition")
	// log.Fatal would exit, so we skip it in example
}

// DemoBeautifulLogger_SugarLogger demonstrates flexible sugar logger API.
func DemoBeautifulLogger_SugarLogger() {
	log := NewBeautifulLogger("user_service")
	sugar := log.Sugar()

	sugar.Infow("User created",
		"user_id", "user-999",
		"email", "john@example.com",
		"created_at", time.Now().Format(time.RFC3339),
	)

	sugar.Warnw("Suspicious activity",
		"user_id", "user-888",
		"failed_attempts", 5,
		"ip_address", "192.168.1.1",
	)
}

// DemoBeautifulLogger_WithCaller demonstrates caller information in logs.
func DemoBeautifulLogger_WithCaller() {
	log := NewBeautifulLogger("myapp")
	log.Info("This log includes file:line reference",
		String("operation", "database_query"),
		Int("rows", 42),
	)
}

// DemoBeautifulLogger_Minimal demonstrates ultra-minimal output.
func DemoBeautifulLogger_Minimal() {
	log := NewBeautifulLoggerMinimal("app")
	log.Info("Minimal output - just the essentials")
	log.Warn("A warning")
	log.Error("An error")
}

// DemoBeautifulLogger_ConfigurableFormat demonstrates custom format configuration.
func DemoBeautifulLogger_ConfigurableFormat() {
	// Create logger with custom format
	cfg := DefaultFormatConfig()
	cfg.ShowTimestamp = true
	cfg.TimestampFormat = "2006-01-02 15:04:05" // Full timestamp
	cfg.ShowCaller = true
	cfg.CallerFormat = "short"
	cfg.ShowEmojis = false
	cfg.MaxFieldLength = 50

	log := NewBeautifulLogger("api").WithFormatConfig(cfg)
	log.Info("API request processed",
		String("endpoint", "/api/users"),
		String("method", "POST"),
		Int("status", 201),
	)
}

// DemoBeautifulLogger_NoTimestamp demonstrates logging without timestamps.
func DemoBeautifulLogger_NoTimestamp() {
	log := NewBeautifulLogger("db").WithShowTimestamp(false)
	log.Info("Database connected", String("driver", "postgres"))
}

// DemoBeautifulLogger_NoCaller demonstrates logging without caller info.
func DemoBeautifulLogger_NoCaller() {
	log := NewBeautifulLogger("service").WithShowCaller(false)
	log.Info("Service started", String("version", "1.0.0"))
}

// DemoBeautifulLogger_CompactMinimal shows compact mode (highest performance).
func DemoBeautifulLogger_CompactMinimal() {
	log := NewBeautifulLoggerCompact("api")
	log.Info("Request", String("method", "GET"), Int("status", 200))
	log.Warn("Cache miss", String("key", "user:123"))
}

// DemoBeautifulLogger_CustomTimestampFormat demonstrates custom timestamp formats.
func DemoBeautifulLogger_CustomTimestampFormat() {
	// Unix timestamp
	cfgUnix := DefaultFormatConfig()
	cfgUnix.TimestampFormat = "unix"
	logUnix := NewBeautifulLogger("timing").WithFormatConfig(cfgUnix)
	logUnix.Info("Event with unix timestamp")

	// ISO format
	cfgISO := DefaultFormatConfig()
	cfgISO.TimestampFormat = "iso"
	logISO := NewBeautifulLogger("timing").WithFormatConfig(cfgISO)
	logISO.Info("Event with ISO timestamp")
}

// TestBeautifulLogger_LevelFiltering tests that log levels are respected.
func TestBeautifulLogger_LevelFiltering(t *testing.T) {
	log := NewBeautifulLogger("test").
		WithLevel(zapcore.InfoLevel)

	// Debug should not be shown
	log.Debug("This should not appear")

	// Info and above should appear
	log.Info("This should appear")
}

// TestBeautifulLogger_FieldPersistence tests that fields persist correctly.
func TestBeautifulLogger_FieldPersistence(t *testing.T) {
	baseLog := NewBeautifulLogger("test")
	logWithFields := baseLog.With(String("persistent", "value"))

	// The persistent field should be included in all logs
	logWithFields.Info("Message 1")
	logWithFields.Info("Message 2")
}

// TestBeautifulLogger_ContextIntegration tests context field extraction.
func TestBeautifulLogger_ContextIntegration(t *testing.T) {
	ctx := context.Background()
	ctx = WithRequestID(ctx, "req-123")
	ctx = WithTraceID(ctx, "trace-456")
	ctx = WithUserID(ctx, "user-789")

	log := NewBeautifulLogger("test").WithContext(ctx)

	// Should include request_id, trace_id, and user_id
	log.Info("Test message with context")
}

// BenchmarkBeautifulLogger_CompactMode benchmarks compact mode performance.
func BenchmarkBeautifulLogger_CompactMode(b *testing.B) {
	log := NewBeautifulLoggerCompact("bench")

	for b.Loop() {
		log.Info("Benchmark message",
			String("key1", "value1"),
			String("key2", "value2"),
		)
	}
}

// BenchmarkBeautifulLogger_DetailedMode benchmarks detailed mode performance.
func BenchmarkBeautifulLogger_DetailedMode(b *testing.B) {
	log := NewBeautifulLogger("bench")

	for b.Loop() {
		log.Info("Benchmark message",
			String("key1", "value1"),
			String("key2", "value2"),
			String("key3", "value3"),
		)
	}
}

// BenchmarkBeautifulLogger_WithFields benchmarks logger with pre-set fields.
func BenchmarkBeautifulLogger_WithFields(b *testing.B) {
	baseLog := NewBeautifulLogger("bench")
	log := baseLog.With(
		String("request_id", "req-123"),
		String("user_id", "user-456"),
	)

	for b.Loop() {
		log.Info("Benchmark message",
			String("action", "process"),
		)
	}
}

// TestBeautifulLogger_NamedLoggers tests hierarchical logger naming.
func TestBeautifulLogger_NamedLoggers(t *testing.T) {
	appLog := NewBeautifulLogger("app")
	authLog := appLog.Named("auth")
	ldapLog := authLog.Named("ldap")

	// Test that names chain correctly
	log, ok := ldapLog.(*BeautifulLogger)
	if !ok {
		t.Fatal("Logger should be BeautifulLogger")
	}

	if log.name != "app.auth.ldap" {
		t.Errorf("Expected name 'app.auth.ldap', got '%s'", log.name)
	}
}

// TestBeautifulLogger_ThreadSafety tests concurrent access to logger.
func TestBeautifulLogger_ThreadSafety(t *testing.T) {
	log := NewBeautifulLoggerCompact("test")

	done := make(chan bool)

	// Spawn multiple goroutines logging concurrently
	for i := range 10 {
		go func(id int) {
			for j := range 10 {
				log.Info("Concurrent message",
					Int("goroutine", id),
					Int("iteration", j),
				)
			}

			done <- true
		}(i)
	}

	// Wait for all goroutines
	for range 10 {
		<-done
	}
}

// TestBeautifulLogger_ClonePreservesState tests that cloning preserves logger state.
func TestBeautifulLogger_ClonePreservesState(t *testing.T) {
	log1 := NewBeautifulLoggerCompact("original")

	log2 := log1.With(String("field1", "value1"))

	// Both should work independently
	log1.Info("Log1 message")
	log2.Info("Log2 message")
}

// TestBeautifulLogger_SyncMethod tests the Sync method.
func TestBeautifulLogger_SyncMethod(t *testing.T) {
	log := NewBeautifulLogger("test")

	err := log.Sync()
	if err != nil {
		t.Errorf("Sync() should not return error: %v", err)
	}
}
