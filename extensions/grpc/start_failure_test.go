package grpc

import (
	"net"
	"testing"

	"github.com/xraph/forge"
	testgrpc "google.golang.org/grpc/interop/grpc_testing"
)

func TestStartListenFailureLeavesNoServer(t *testing.T) {
	taken, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = taken.Close() })

	server := NewGRPCServer(DefaultConfig(), forge.NewNoopLogger(), forge.NewNoOpMetrics())
	_ = server.RegisterService(&testgrpc.TestService_ServiceDesc, &codeService{})

	if err := server.Start(t.Context(), taken.Addr().String()); err == nil {
		t.Fatal("Start on a taken address succeeded")
	}

	if server.GetServer() != nil {
		t.Error("GetServer() is not nil after a failed Start")
	}

	if server.IsRunning() {
		t.Error("IsRunning() is true after a failed Start")
	}

	if got := server.GetServices(); len(got) != 0 {
		t.Errorf("GetServices() = %v after a failed Start, want none", got)
	}
}
