package grpc

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/xraph/forge"
	"github.com/xraph/go-utils/log"
	"google.golang.org/grpc"
	grpccodes "google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	testgrpc "google.golang.org/grpc/interop/grpc_testing"
)

// startLoggedServer starts a server whose own log lines land in the returned
// TestLogger.
func startLoggedServer(t *testing.T, svc *codeService) (*log.TestLogger, *grpc.ClientConn) {
	t.Helper()

	cfg := DefaultConfig()
	cfg.Address = "127.0.0.1:0"

	tl, ok := log.NewTestLogger().(*log.TestLogger)
	if !ok {
		t.Fatal("log.NewTestLogger is not a *log.TestLogger")
	}

	server := NewGRPCServer(cfg, tl, forge.NewNoOpMetrics())
	_ = server.RegisterService(&testgrpc.TestService_ServiceDesc, svc)

	if err := server.Start(t.Context(), cfg.Address); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = server.Stop(context.Background()) })

	conn, err := grpc.NewClient(server.(*grpcServer).listener.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = conn.Close() })

	return tl, conn
}

func failedEntry(t *testing.T, tl *log.TestLogger, msg string) log.LogEntry {
	t.Helper()

	for _, e := range tl.GetLogsByLevel("ERROR") {
		if e.Message == msg {
			return e
		}
	}

	t.Fatalf("no error log %q in %v", msg, tl.GetLogs())

	return log.LogEntry{}
}

func TestUnaryFailureLogCarriesTraceIDs(t *testing.T) {
	installTracing(t)

	tl, conn := startLoggedServer(t, &codeService{code: grpccodes.NotFound, gotIDs: make(chan [2]string, 1)})

	if err := callEmpty(t.Context(), t, conn); err == nil {
		t.Fatal("expected an error")
	}

	entry := failedEntry(t, tl, "grpc unary call failed")

	for _, key := range []string{"trace_id", "span_id"} {
		v, ok := entry.Field(key)
		if !ok || v == "" {
			t.Errorf("failed-call log line has no %s: %+v", key, entry.Fields)
		}
	}
}

func TestStreamFailureLogCarriesTraceIDs(t *testing.T) {
	installTracing(t)

	tl, conn := startLoggedServer(t, &codeService{})

	// codeService leaves FullDuplexCall to the embedded Unimplemented server,
	// so the stream fails after the interceptors have run.
	stream, err := testgrpc.NewTestServiceClient(conn).FullDuplexCall(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	if _, err := stream.Recv(); err == nil || errors.Is(err, io.EOF) {
		t.Fatalf("Recv err = %v, want a status error", err)
	}

	entry := failedEntry(t, tl, "grpc stream call failed")

	for _, key := range []string{"trace_id", "span_id"} {
		v, ok := entry.Field(key)
		if !ok || v == "" {
			t.Errorf("failed-stream log line has no %s: %+v", key, entry.Fields)
		}
	}
}
