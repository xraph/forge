package spanerror

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

type quotingError struct{ dsn string }

func (e *quotingError) Error() string { return "bad dsn " + e.dsn }

func TestErrorClass(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{"nil", nil, ""},
		{"plain", errors.New("x"), "*errors.errorString"},
		{"wrapped", fmt.Errorf("open: %w", &quotingError{dsn: "SECRET"}), "*spanerror.quotingError"},
		{"twice wrapped", fmt.Errorf("a: %w", fmt.Errorf("b: %w", &quotingError{})), "*spanerror.quotingError"},
		{"canceled", fmt.Errorf("stop: %w", context.Canceled), "context.Canceled"},
		{"deadline", fmt.Errorf("stop: %w", context.DeadlineExceeded), "context.DeadlineExceeded"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ErrorClass(tc.err); got != tc.want {
				t.Fatalf("ErrorClass = %q, want %q", got, tc.want)
			}
		})
	}
}

func recordOne(t *testing.T, fn func(sdktrace.ReadWriteSpan)) tracetest.SpanStub {
	t.Helper()

	exp := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp))

	t.Cleanup(func() { _ = tp.Shutdown(t.Context()) })

	_, span := tp.Tracer("t").Start(t.Context(), "op")
	fn(span.(sdktrace.ReadWriteSpan))
	span.End()

	return exp.GetSpans()[0]
}

func TestRecordErrorKeepsOnlyTheClass(t *testing.T) {
	s := recordOne(t, func(span sdktrace.ReadWriteSpan) {
		RecordError(span, fmt.Errorf("open: %w", &quotingError{dsn: "postgres://u:SECRET@h/db"}))
	})

	if s.Status.Code != codes.Error || s.Status.Description != "*spanerror.quotingError" {
		t.Fatalf("status = %v %q", s.Status.Code, s.Status.Description)
	}

	if len(s.Events) != 1 || s.Events[0].Name != "exception" || len(s.Events[0].Attributes) != 1 {
		t.Fatalf("events = %+v, want one exception event with one attribute", s.Events)
	}

	if kv := s.Events[0].Attributes[0]; kv.Key != "exception.type" || kv.Value.AsString() != "*spanerror.quotingError" {
		t.Fatalf("event attribute = %v", kv)
	}
}

func TestRecordErrorNilDoesNothing(t *testing.T) {
	s := recordOne(t, func(span sdktrace.ReadWriteSpan) { RecordError(span, nil) })

	if s.Status.Code != codes.Unset || len(s.Events) != 0 {
		t.Fatalf("status %v events %v", s.Status, s.Events)
	}
}

func TestRecordPanicWithStack(t *testing.T) {
	s := recordOne(t, func(span sdktrace.ReadWriteSpan) { RecordPanic(span, true) })

	if s.Status.Code != codes.Error || s.Status.Description != PanicClass {
		t.Fatalf("status = %v %q", s.Status.Code, s.Status.Description)
	}

	attrs := map[string]string{}
	for _, kv := range s.Events[0].Attributes {
		attrs[string(kv.Key)] = kv.Value.AsString()
	}

	if attrs["exception.type"] != PanicClass || !strings.Contains(attrs["exception.stacktrace"], "TestRecordPanicWithStack") {
		t.Fatalf("event attributes = %v", attrs)
	}

	if _, ok := attrs["exception.message"]; ok {
		t.Fatal("panic event carries exception.message")
	}
}
