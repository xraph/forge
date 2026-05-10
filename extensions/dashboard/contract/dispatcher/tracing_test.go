package dispatcher

import (
	"context"
	"encoding/json"
	"testing"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/xraph/forge/extensions/dashboard/contract"
)

func TestDispatcher_OpensSpanPerDispatch(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	defer func() { _ = tp.Shutdown(context.Background()) }()
	otel.SetTracerProvider(tp)
	tracer := tp.Tracer("test")

	d := NewWithOptions(NoopMetricsEmitter{}, WithTracer(tracer))
	_ = d.Register("c", "i", 1, func(_ context.Context, _ json.RawMessage, _ map[string]any, _ contract.Principal) (*Result, error) {
		return &Result{Data: json.RawMessage(`null`)}, nil
	})
	req := contract.Request{Envelope: "v1", Kind: contract.KindQuery, Contributor: "c", Intent: "i", IntentVersion: 1}
	_, _, _ = d.Dispatch(context.Background(), req, contract.Principal{})

	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	s := spans[0]
	if s.Name != "dispatch:c/i@1" {
		t.Errorf("span name = %q", s.Name)
	}
}

func TestDispatcher_SpanRecordsErrorCode(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	defer func() { _ = tp.Shutdown(context.Background()) }()
	tracer := tp.Tracer("test")

	d := NewWithOptions(NoopMetricsEmitter{}, WithTracer(tracer))
	_ = d.Register("c", "i", 1, func(_ context.Context, _ json.RawMessage, _ map[string]any, _ contract.Principal) (*Result, error) {
		return nil, &contract.Error{Code: contract.CodeConflict}
	})
	req := contract.Request{Envelope: "v1", Kind: contract.KindCommand, Contributor: "c", Intent: "i", IntentVersion: 1}
	_, _, _ = d.Dispatch(context.Background(), req, contract.Principal{})

	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span")
	}
	found := false
	for _, attr := range spans[0].Attributes {
		if string(attr.Key) == "forge.contract.error_code" && attr.Value.AsString() == "CONFLICT" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected error_code attribute, got attrs=%+v", spans[0].Attributes)
	}
}

func TestDispatcher_NilTracerIsNoop(t *testing.T) {
	d := NewWithOptions(NoopMetricsEmitter{}) // no tracer option
	_ = d.Register("c", "i", 1, func(_ context.Context, _ json.RawMessage, _ map[string]any, _ contract.Principal) (*Result, error) {
		return &Result{Data: json.RawMessage(`null`)}, nil
	})
	req := contract.Request{Envelope: "v1", Kind: contract.KindQuery, Contributor: "c", Intent: "i", IntentVersion: 1}
	_, _, err := d.Dispatch(context.Background(), req, contract.Principal{})
	if err != nil {
		t.Errorf("nil tracer should not affect dispatch: %v", err)
	}
}
