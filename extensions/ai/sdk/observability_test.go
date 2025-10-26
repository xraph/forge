package sdk

import (
	"context"
	"testing"
	"time"
)

// Test Tracer

func TestNewTracer(t *testing.T) {
	tracer := NewTracer(nil, nil)

	if tracer == nil {
		t.Fatal("expected tracer to be created")
	}
}

func TestTracer_StartSpan(t *testing.T) {
	tracer := NewTracer(nil, nil)

	span := tracer.StartSpan(context.Background(), "test_operation")

	if span == nil {
		t.Fatal("expected span to be created")
	}

	if span.Name != "test_operation" {
		t.Errorf("expected name 'test_operation', got '%s'", span.Name)
	}

	if span.Status != SpanStatusOK {
		t.Errorf("expected status OK, got %s", span.Status)
	}
}

func TestSpan_Finish(t *testing.T) {
	tracer := NewTracer(nil, nil)
	span := tracer.StartSpan(context.Background(), "test")

	time.Sleep(10 * time.Millisecond)
	span.Finish()

	if span.EndTime.IsZero() {
		t.Error("expected end time to be set")
	}

	if span.Duration() < 10*time.Millisecond {
		t.Errorf("expected duration >= 10ms, got %v", span.Duration())
	}
}

func TestSpan_SetTag(t *testing.T) {
	tracer := NewTracer(nil, nil)
	span := tracer.StartSpan(context.Background(), "test")

	span.SetTag("user_id", "123")
	span.SetTag("operation", "generate")

	if span.Tags["user_id"] != "123" {
		t.Error("expected tag to be set")
	}
}

func TestSpan_SetError(t *testing.T) {
	tracer := NewTracer(nil, nil)
	span := tracer.StartSpan(context.Background(), "test")

	testErr := &TestError{}
	span.SetError(testErr)

	if span.Error != testErr {
		t.Error("expected error to be set")
	}

	if span.Status != SpanStatusError {
		t.Errorf("expected status error, got %s", span.Status)
	}
}

func TestSpan_LogEvent(t *testing.T) {
	tracer := NewTracer(nil, nil)
	span := tracer.StartSpan(context.Background(), "test")

	span.LogEvent("info", "Processing request", map[string]interface{}{
		"tokens": 1000,
	})

	if len(span.Logs) != 1 {
		t.Errorf("expected 1 log, got %d", len(span.Logs))
	}

	if span.Logs[0].Message != "Processing request" {
		t.Errorf("expected message 'Processing request', got '%s'", span.Logs[0].Message)
	}
}

func TestSpanContext(t *testing.T) {
	tracer := NewTracer(nil, nil)
	span := tracer.StartSpan(context.Background(), "test")

	ctx := ContextWithSpan(context.Background(), span)
	retrievedSpan := SpanFromContext(ctx)

	if retrievedSpan != span {
		t.Error("expected to retrieve same span from context")
	}
}

func TestSpanContext_NotFound(t *testing.T) {
	span := SpanFromContext(context.Background())

	if span != nil {
		t.Error("expected nil span from empty context")
	}
}

// Test Debugger

func TestNewDebugger(t *testing.T) {
	debugger := NewDebugger(nil)

	if debugger == nil {
		t.Fatal("expected debugger to be created")
	}
}

func TestDebugger_GetDebugInfo(t *testing.T) {
	debugger := NewDebugger(nil)

	info := debugger.GetDebugInfo()

	if info == nil {
		t.Fatal("expected debug info")
	}

	if info.Goroutines <= 0 {
		t.Error("expected positive goroutine count")
	}

	if info.MemoryStats.Alloc == 0 {
		t.Error("expected non-zero memory allocation")
	}
}

func TestDebugger_RecordError(t *testing.T) {
	debugger := NewDebugger(nil)

	testErr := &TestError{}
	debugger.RecordError(testErr, map[string]interface{}{
		"user_id": "123",
	})

	errors := debugger.GetRecentErrors(10)

	if len(errors) != 1 {
		t.Errorf("expected 1 error, got %d", len(errors))
	}

	if errors[0].Message != testErr.Error() {
		t.Errorf("expected message '%s', got '%s'", testErr.Error(), errors[0].Message)
	}
}

func TestDebugger_RecordError_Nil(t *testing.T) {
	debugger := NewDebugger(nil)

	debugger.RecordError(nil, nil)

	errors := debugger.GetRecentErrors(10)

	if len(errors) != 0 {
		t.Error("expected no errors for nil error")
	}
}

func TestDebugger_ClearErrors(t *testing.T) {
	debugger := NewDebugger(nil)

	debugger.RecordError(&TestError{}, nil)
	debugger.ClearErrors()

	errors := debugger.GetRecentErrors(10)

	if len(errors) != 0 {
		t.Errorf("expected 0 errors after clear, got %d", len(errors))
	}
}

// Test Profiler

func TestNewProfiler(t *testing.T) {
	profiler := NewProfiler(nil, nil)

	if profiler == nil {
		t.Fatal("expected profiler to be created")
	}
}

func TestProfiler_StartProfile(t *testing.T) {
	profiler := NewProfiler(nil, nil)

	session := profiler.StartProfile("test_op")

	if session == nil {
		t.Fatal("expected profile session to be created")
	}

	if session.name != "test_op" {
		t.Errorf("expected name 'test_op', got '%s'", session.name)
	}
}

func TestProfiler_EndSession(t *testing.T) {
	profiler := NewProfiler(nil, nil)

	session := profiler.StartProfile("test_op")
	time.Sleep(10 * time.Millisecond)
	session.End()

	profile := profiler.GetProfile("test_op")

	if profile == nil {
		t.Fatal("expected profile to exist")
	}

	if profile.Count != 1 {
		t.Errorf("expected count 1, got %d", profile.Count)
	}

	if profile.MinTime < 10*time.Millisecond {
		t.Errorf("expected min time >= 10ms, got %v", profile.MinTime)
	}
}

func TestProfiler_MultipleRecords(t *testing.T) {
	profiler := NewProfiler(nil, nil)

	// Record multiple operations
	for i := 0; i < 5; i++ {
		session := profiler.StartProfile("test_op")
		time.Sleep(time.Duration(i+1) * time.Millisecond)
		session.End()
	}

	profile := profiler.GetProfile("test_op")

	if profile.Count != 5 {
		t.Errorf("expected count 5, got %d", profile.Count)
	}

	if profile.AvgTime == 0 {
		t.Error("expected non-zero average time")
	}
}

func TestProfiler_GetAllProfiles(t *testing.T) {
	profiler := NewProfiler(nil, nil)

	profiler.StartProfile("op1").End()
	profiler.StartProfile("op2").End()
	profiler.StartProfile("op3").End()

	profiles := profiler.GetAllProfiles()

	if len(profiles) != 3 {
		t.Errorf("expected 3 profiles, got %d", len(profiles))
	}
}

func TestProfiler_Reset(t *testing.T) {
	profiler := NewProfiler(nil, nil)

	profiler.StartProfile("test_op").End()
	profiler.Reset()

	profile := profiler.GetProfile("test_op")

	if profile != nil {
		t.Error("expected no profile after reset")
	}
}

func TestProfiler_ExportJSON(t *testing.T) {
	profiler := NewProfiler(nil, nil)

	profiler.StartProfile("test_op").End()

	json, err := profiler.ExportJSON()

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if len(json) == 0 {
		t.Error("expected non-empty JSON")
	}
}

// Test HealthChecker

func TestNewHealthChecker(t *testing.T) {
	hc := NewHealthChecker(nil)

	if hc == nil {
		t.Fatal("expected health checker to be created")
	}
}

func TestHealthChecker_RegisterCheck(t *testing.T) {
	hc := NewHealthChecker(nil)

	hc.RegisterCheck("test_check", func(ctx context.Context) error {
		return nil
	})

	if len(hc.checks) != 1 {
		t.Errorf("expected 1 check, got %d", len(hc.checks))
	}
}

func TestHealthChecker_RunChecks_Healthy(t *testing.T) {
	hc := NewHealthChecker(nil)

	hc.RegisterCheck("check1", func(ctx context.Context) error {
		return nil
	})

	hc.RegisterCheck("check2", func(ctx context.Context) error {
		return nil
	})

	results := hc.RunChecks(context.Background())

	if len(results) != 2 {
		t.Errorf("expected 2 results, got %d", len(results))
	}

	for _, result := range results {
		if result.Status != "healthy" {
			t.Errorf("expected status healthy, got %s", result.Status)
		}
	}
}

func TestHealthChecker_RunChecks_Unhealthy(t *testing.T) {
	hc := NewHealthChecker(nil)

	testErr := &TestError{}

	hc.RegisterCheck("failing_check", func(ctx context.Context) error {
		return testErr
	})

	results := hc.RunChecks(context.Background())

	result := results["failing_check"]

	if result.Status != "unhealthy" {
		t.Errorf("expected status unhealthy, got %s", result.Status)
	}

	if result.Error != testErr {
		t.Error("expected error to be set")
	}
}

func TestHealthChecker_GetOverallHealth_Healthy(t *testing.T) {
	hc := NewHealthChecker(nil)

	hc.RegisterCheck("check1", func(ctx context.Context) error {
		return nil
	})

	status := hc.GetOverallHealth(context.Background())

	if status != "healthy" {
		t.Errorf("expected status healthy, got %s", status)
	}
}

func TestHealthChecker_GetOverallHealth_Degraded(t *testing.T) {
	hc := NewHealthChecker(nil)

	hc.RegisterCheck("check1", func(ctx context.Context) error {
		return nil
	})

	hc.RegisterCheck("check2", func(ctx context.Context) error {
		return &TestError{}
	})

	status := hc.GetOverallHealth(context.Background())

	if status != "degraded" {
		t.Errorf("expected status degraded, got %s", status)
	}
}

func TestHealthChecker_GetOverallHealth_Unhealthy(t *testing.T) {
	hc := NewHealthChecker(nil)

	hc.RegisterCheck("check1", func(ctx context.Context) error {
		return &TestError{}
	})

	status := hc.GetOverallHealth(context.Background())

	if status != "unhealthy" {
		t.Errorf("expected status unhealthy, got %s", status)
	}
}

// Test helper
type TestError struct{}

func (e *TestError) Error() string {
	return "test error"
}

