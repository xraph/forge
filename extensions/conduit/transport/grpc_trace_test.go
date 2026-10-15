package transport_test

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/xraph/forge/extensions/conduit/core"
	"github.com/xraph/forge/extensions/conduit/discovery"
	"github.com/xraph/forge/extensions/conduit/transport"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	testgrpc "google.golang.org/grpc/interop/grpc_testing"
)

type emptyService struct {
	testgrpc.UnimplementedTestServiceServer
}

func (emptyService) EmptyCall(context.Context, *testgrpc.Empty) (*testgrpc.Empty, error) {
	return &testgrpc.Empty{}, nil
}

func installTracing(t *testing.T) *tracetest.InMemoryExporter {
	t.Helper()

	exp := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp), sdktrace.WithSampler(sdktrace.AlwaysSample()))
	prevTP, prevProp := otel.GetTracerProvider(), otel.GetTextMapPropagator()

	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})

	t.Cleanup(func() {
		_ = tp.Shutdown(context.Background())

		otel.SetTracerProvider(prevTP)
		otel.SetTextMapPropagator(prevProp)
	})

	return exp
}

func TestGRPCClientTraceLinksServerSpan(t *testing.T) {
	exp := installTracing(t)

	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	srv := grpc.NewServer(grpc.StatsHandler(otelgrpc.NewServerHandler()))

	testgrpc.RegisterTestServiceServer(srv, emptyService{})

	go func() { _ = srv.Serve(listener) }()

	t.Cleanup(srv.Stop)

	registry := discovery.NewStatic(core.Instance{Identity: core.Identity{Namespace: "prod", ServiceID: "billing", InstanceID: "one"}, Ready: true, Endpoints: []core.Endpoint{{Protocol: "grpc", URL: "grpc://" + listener.Addr().String()}}})
	services := &transport.Services{Identity: core.Identity{Namespace: "prod"}, Resolver: registry}

	conn, err := services.GRPC("billing", insecure.NewCredentials())
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = conn.Close() })

	ctx, parent := otel.Tracer("test").Start(t.Context(), "parent")
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)

	defer cancel()

	if _, err := testgrpc.NewTestServiceClient(conn).EmptyCall(ctx, &testgrpc.Empty{}, grpc.WaitForReady(true)); err != nil {
		t.Fatal(err)
	}

	parent.End()

	var clientID, clientTrace, serverParent, serverTrace string

	for _, s := range exp.GetSpans() {
		switch s.SpanKind {
		case trace.SpanKindClient:
			clientID = s.SpanContext.SpanID().String()
			clientTrace = s.SpanContext.TraceID().String()
		case trace.SpanKindServer:
			serverParent = s.Parent.SpanID().String()
			serverTrace = s.SpanContext.TraceID().String()
		}
	}

	if clientID == "" {
		t.Fatal("conduit's dialer produced no client span")
	}

	want := parent.SpanContext().TraceID().String()
	if clientTrace != want || serverTrace != want {
		t.Fatalf("trace ids client=%s server=%s, want %s", clientTrace, serverTrace, want)
	}

	if serverParent != clientID {
		t.Fatalf("server span parent = %s, want the client span %s", serverParent, clientID)
	}
}
