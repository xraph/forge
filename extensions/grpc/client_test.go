package grpc

import (
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func TestClientDialOptionsLinkClientAndServerSpans(t *testing.T) {
	exp := installTracing(t)
	svc := &codeService{}

	server, _ := startTestServer(t, DefaultConfig(), registerCodeService(svc))

	addr := server.(*grpcServer).listener.Addr().String()

	conn, err := grpc.NewClient(addr, append(ClientDialOptions(), grpc.WithTransportCredentials(insecure.NewCredentials()))...)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = conn.Close() })

	ctx, parent := otel.Tracer("test").Start(t.Context(), "parent")

	if err := callEmpty(ctx, t, conn); err != nil {
		t.Fatal(err)
	}

	parent.End()

	var clients []trace.SpanKind

	var clientID, clientTrace, serverParent, serverTrace string

	for _, s := range exp.GetSpans() {
		switch s.SpanKind {
		case trace.SpanKindClient:
			clients = append(clients, s.SpanKind)
			clientID = s.SpanContext.SpanID().String()
			clientTrace = s.SpanContext.TraceID().String()

			if s.Parent.SpanID() != parent.SpanContext().SpanID() {
				t.Fatalf("client span parent = %s, want the caller's span %s", s.Parent.SpanID(), parent.SpanContext().SpanID())
			}
		case trace.SpanKindServer:
			serverParent = s.Parent.SpanID().String()
			serverTrace = s.SpanContext.TraceID().String()
		}
	}

	if len(clients) != 1 {
		t.Fatalf("client spans = %d, want 1", len(clients))
	}

	want := parent.SpanContext().TraceID().String()
	if clientTrace != want || serverTrace != want {
		t.Fatalf("trace ids client=%s server=%s, want %s", clientTrace, serverTrace, want)
	}

	if serverParent != clientID {
		t.Fatalf("server span parent = %s, want the client span %s", serverParent, clientID)
	}
}
