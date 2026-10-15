package grpc

import (
	"context"
	"testing"
	"time"

	"github.com/xraph/forge"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	grpccodes "google.golang.org/grpc/codes"
	"google.golang.org/grpc/health/grpc_health_v1"
	testgrpc "google.golang.org/grpc/interop/grpc_testing"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// installTracing swaps the OTel globals for an in-memory exporter. Tests that
// use it must not run in parallel.
func installTracing(t *testing.T) *tracetest.InMemoryExporter {
	t.Helper()

	exp := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp), sdktrace.WithSampler(sdktrace.AlwaysSample()))

	prevTP, prevProp := otel.GetTracerProvider(), otel.GetTextMapPropagator()

	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))

	t.Cleanup(func() {
		_ = tp.Shutdown(context.Background())

		otel.SetTracerProvider(prevTP)
		otel.SetTextMapPropagator(prevProp)
	})

	return exp
}

type codeService struct {
	testgrpc.UnimplementedTestServiceServer

	code   grpccodes.Code
	gotIDs chan [2]string
}

func (s *codeService) EmptyCall(ctx context.Context, _ *testgrpc.Empty) (*testgrpc.Empty, error) {
	if s.gotIDs != nil {
		s.gotIDs <- [2]string{forge.TraceIDFromContext(ctx), forge.SpanIDFromContext(ctx)}
	}

	if s.code != grpccodes.OK {
		return nil, status.Error(s.code, "nope")
	}

	return &testgrpc.Empty{}, nil
}

func callEmpty(ctx context.Context, t *testing.T, conn *grpc.ClientConn) error {
	t.Helper()

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	_, err := testgrpc.NewTestServiceClient(conn).EmptyCall(ctx, &testgrpc.Empty{}, grpc.WaitForReady(true))

	return err
}

func spanAttr(s tracetest.SpanStub, key string) attribute.Value {
	for _, kv := range s.Attributes {
		if string(kv.Key) == key {
			return kv.Value
		}
	}

	return attribute.Value{}
}

func serverSpans(exp *tracetest.InMemoryExporter) []tracetest.SpanStub {
	var out []tracetest.SpanStub

	for _, s := range exp.GetSpans() {
		if s.SpanKind == trace.SpanKindServer {
			out = append(out, s)
		}
	}

	return out
}

func registerCodeService(svc *codeService) func(GRPC) {
	return func(s GRPC) { _ = s.RegisterService(&testgrpc.TestService_ServiceDesc, svc) }
}

func TestServerSpanNameAndAttributes(t *testing.T) {
	exp := installTracing(t)
	svc := &codeService{gotIDs: make(chan [2]string, 1)}

	_, conn := startTestServer(t, DefaultConfig(), registerCodeService(svc))

	if err := callEmpty(t.Context(), t, conn); err != nil {
		t.Fatal(err)
	}

	ids := <-svc.gotIDs

	spans := serverSpans(exp)
	if len(spans) != 1 {
		t.Fatalf("server spans = %d, want 1", len(spans))
	}

	server := spans[0]

	if server.Name != "grpc.testing.TestService/EmptyCall" {
		t.Fatalf("name = %q", server.Name)
	}

	// otelgrpc v0.70.0 emits the current RPC semantic conventions: the system
	// is rpc.system.name and rpc.method carries the full method name.
	if spanAttr(server, "rpc.system.name").AsString() != "grpc" ||
		spanAttr(server, "rpc.method").AsString() != "grpc.testing.TestService/EmptyCall" {
		t.Fatalf("attrs = %v", server.Attributes)
	}

	if ids[0] != server.SpanContext.TraceID().String() || ids[1] != server.SpanContext.SpanID().String() {
		t.Fatalf("log ids %v do not match span %s/%s", ids, server.SpanContext.TraceID(), server.SpanContext.SpanID())
	}
}

func TestServerSpanStatusByCode(t *testing.T) {
	for code, wantErr := range map[grpccodes.Code]bool{grpccodes.NotFound: false, grpccodes.Internal: true} {
		exp := installTracing(t)

		_, conn := startTestServer(t, DefaultConfig(), registerCodeService(&codeService{code: code}))

		_ = callEmpty(t.Context(), t, conn)

		spans := serverSpans(exp)
		if len(spans) != 1 {
			t.Fatalf("code %v: server spans = %d, want 1", code, len(spans))
		}

		if got := spans[0].Status.Code == codes.Error; got != wantErr {
			t.Errorf("code %v: span error = %v, want %v", code, got, wantErr)
		}
	}
}

func TestServerJoinsIncomingTrace(t *testing.T) {
	exp := installTracing(t)

	_, conn := startTestServer(t, DefaultConfig(), registerCodeService(&codeService{}))

	ctx := metadata.AppendToOutgoingContext(t.Context(), "traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	if err := callEmpty(ctx, t, conn); err != nil {
		t.Fatal(err)
	}

	spans := serverSpans(exp)
	if len(spans) != 1 {
		t.Fatalf("server spans = %d, want 1", len(spans))
	}

	s := spans[0]
	if s.SpanContext.TraceID().String() != "4bf92f3577b34da6a3ce929d0e0e4736" || s.Parent.SpanID().String() != "00f067aa0ba902b7" {
		t.Fatalf("server span did not join the incoming trace: %s parent %s", s.SpanContext.TraceID(), s.Parent.SpanID())
	}
}

func TestHealthCheckHasNoSpan(t *testing.T) {
	exp := installTracing(t)

	cfg := DefaultConfig()
	cfg.EnableHealthCheck = true

	_, conn := startTestServer(t, cfg, nil)

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	if _, err := grpc_health_v1.NewHealthClient(conn).Check(ctx, &grpc_health_v1.HealthCheckRequest{}, grpc.WaitForReady(true)); err != nil {
		t.Fatal(err)
	}

	if n := len(exp.GetSpans()); n != 0 {
		t.Fatalf("health check produced %d spans", n)
	}
}

func TestTracingDisabledNoSpans(t *testing.T) {
	exp := installTracing(t)

	cfg := DefaultConfig()
	cfg.EnableTracing = false

	_, conn := startTestServer(t, cfg, registerCodeService(&codeService{}))

	if err := callEmpty(t.Context(), t, conn); err != nil {
		t.Fatal(err)
	}

	if n := len(exp.GetSpans()); n != 0 {
		t.Fatalf("EnableTracing=false produced %d spans", n)
	}
}

func TestCustomInterceptorSeesSpan(t *testing.T) {
	installTracing(t)

	seen := make(chan bool, 1)

	_, conn := startTestServer(t, DefaultConfig(), func(s GRPC) {
		s.AddUnaryInterceptor(func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, h grpc.UnaryHandler) (any, error) {
			seen <- trace.SpanContextFromContext(ctx).IsValid()

			return h(ctx, req)
		})

		_ = s.RegisterService(&testgrpc.TestService_ServiceDesc, &codeService{})
	})

	if err := callEmpty(t.Context(), t, conn); err != nil {
		t.Fatal(err)
	}

	if !<-seen {
		t.Fatal("custom interceptor did not see the server span")
	}
}
