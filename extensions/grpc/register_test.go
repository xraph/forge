package grpc

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/xraph/forge"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	testgrpc "google.golang.org/grpc/interop/grpc_testing"
)

type testService struct {
	testgrpc.UnimplementedTestServiceServer
}

func (testService) EmptyCall(context.Context, *testgrpc.Empty) (*testgrpc.Empty, error) {
	return &testgrpc.Empty{}, nil
}

func startTestServer(t *testing.T, cfg Config, register func(GRPC)) (GRPC, *grpc.ClientConn) {
	t.Helper()

	cfg.Address = "127.0.0.1:0"
	server := NewGRPCServer(cfg, forge.NewNoopLogger(), forge.NewNoOpMetrics())

	if register != nil {
		register(server)
	}

	if err := server.Start(t.Context(), cfg.Address); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = server.Stop(context.Background()) })

	addr := server.(*grpcServer).listener.Addr().String()

	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = conn.Close() })

	return server, conn
}

func TestServiceRegisteredBeforeStartIsServed(t *testing.T) {
	_, conn := startTestServer(t, DefaultConfig(), func(s GRPC) {
		if err := s.RegisterService(&testgrpc.TestService_ServiceDesc, testService{}); err != nil {
			t.Fatal(err)
		}
	})

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	if _, err := testgrpc.NewTestServiceClient(conn).EmptyCall(ctx, &testgrpc.Empty{}, grpc.WaitForReady(true)); err != nil {
		t.Fatalf("EmptyCall: %v", err)
	}
}

func TestRegisterAfterStartFails(t *testing.T) {
	server, _ := startTestServer(t, DefaultConfig(), nil)

	err := server.RegisterService(&testgrpc.TestService_ServiceDesc, testService{})
	if !errors.Is(err, ErrRegisterAfterStart) {
		t.Fatalf("RegisterService after Start = %v, want ErrRegisterAfterStart", err)
	}
}
