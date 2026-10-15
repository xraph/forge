package grpc

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/xraph/forge"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health/grpc_health_v1"
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

func TestRegisterServiceRejectsWhatGRPCWouldFatalOn(t *testing.T) {
	tests := []struct {
		name string
		desc *grpc.ServiceDesc
		impl any
	}{
		{name: "nil descriptor", desc: nil, impl: testService{}},
		{name: "wrong implementation", desc: &testgrpc.TestService_ServiceDesc, impl: struct{}{}},
		{name: "duplicate", desc: &testgrpc.TestService_ServiceDesc, impl: testService{}},
		{name: "health service", desc: &grpc_health_v1.Health_ServiceDesc, impl: nil},
		{
			name: "reflection service",
			desc: &grpc.ServiceDesc{ServiceName: reflectionV1, HandlerType: (*any)(nil)},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := NewGRPCServer(DefaultConfig(), forge.NewNoopLogger(), forge.NewNoOpMetrics())
			if err := server.RegisterService(&testgrpc.TestService_ServiceDesc, testService{}); err != nil {
				t.Fatal(err)
			}

			err := server.RegisterService(tt.desc, tt.impl)
			if !errors.Is(err, ErrInvalidService) {
				t.Fatalf("RegisterService = %v, want ErrInvalidService", err)
			}
		})
	}
}

func TestRegisterServiceAllowsHealthNameWhenHealthIsOff(t *testing.T) {
	cfg := DefaultConfig()
	cfg.EnableHealthCheck = false

	server := NewGRPCServer(cfg, forge.NewNoopLogger(), forge.NewNoOpMetrics())
	if err := server.RegisterService(&grpc_health_v1.Health_ServiceDesc, nil); err != nil {
		t.Fatalf("RegisterService = %v, want nil with health checks off", err)
	}
}
