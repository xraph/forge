package grpc

import (
	"context"
	"errors"
	"testing"
	"time"

	testgrpc "google.golang.org/grpc/interop/grpc_testing"
)

// blockingService holds UnaryCall open until release is closed or the call is
// cancelled.
type blockingService struct {
	testgrpc.UnimplementedTestServiceServer

	entered chan struct{}
	release chan struct{}
}

func (b *blockingService) UnaryCall(ctx context.Context, _ *testgrpc.SimpleRequest) (*testgrpc.SimpleResponse, error) {
	close(b.entered)

	select {
	case <-b.release:
		return &testgrpc.SimpleResponse{}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func TestGracefulStopDrainsInFlightCall(t *testing.T) {
	svc := &blockingService{entered: make(chan struct{}), release: make(chan struct{})}

	server, conn := startTestServer(t, DefaultConfig(), func(s GRPC) {
		if err := s.RegisterService(&testgrpc.TestService_ServiceDesc, svc); err != nil {
			t.Fatal(err)
		}
	})

	callErr := make(chan error, 1)

	go func() {
		_, err := testgrpc.NewTestServiceClient(conn).UnaryCall(context.Background(), &testgrpc.SimpleRequest{})
		callErr <- err
	}()

	<-svc.entered

	stopped := make(chan error, 1)

	go func() { stopped <- server.GracefulStop(context.Background()) }()

	// Let GracefulStop start draining, then let the call finish.
	time.Sleep(50 * time.Millisecond)
	close(svc.release)

	select {
	case err := <-stopped:
		if err != nil {
			t.Fatalf("GracefulStop: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("GracefulStop did not return after the in-flight call finished")
	}

	if err := <-callErr; err != nil {
		t.Errorf("in-flight call failed during GracefulStop: %v", err)
	}
}

// startBlockedCall starts a server with a blockingService and returns once a
// UnaryCall is inside the handler. The handler stays blocked until the test
// ends.
func startBlockedCall(t *testing.T) GRPC {
	t.Helper()

	svc := &blockingService{entered: make(chan struct{}), release: make(chan struct{})}

	t.Cleanup(func() { close(svc.release) })

	server, conn := startTestServer(t, DefaultConfig(), func(s GRPC) {
		if err := s.RegisterService(&testgrpc.TestService_ServiceDesc, svc); err != nil {
			t.Fatal(err)
		}
	})

	go func() {
		_, _ = testgrpc.NewTestServiceClient(conn).UnaryCall(context.Background(), &testgrpc.SimpleRequest{})
	}()

	<-svc.entered

	return server
}

func TestGracefulStopForcesStopWhenContextEnds(t *testing.T) {
	server := startBlockedCall(t)

	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()

	stopped := make(chan error, 1)

	go func() { stopped <- server.GracefulStop(ctx) }()

	select {
	case err := <-stopped:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("GracefulStop = %v, want context.DeadlineExceeded", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("GracefulStop ignored its context and kept draining")
	}

	if server.IsRunning() {
		t.Error("IsRunning() is true after GracefulStop returned")
	}
}

func TestStopCutsGracefulStopShort(t *testing.T) {
	server := startBlockedCall(t)

	stopped := make(chan error, 1)

	go func() { stopped <- server.GracefulStop(context.Background()) }()

	// Wait for the drain to start before stopping hard.
	deadline := time.Now().Add(5 * time.Second)
	for server.IsRunning() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}

	if err := server.Stop(context.Background()); err != nil {
		t.Fatalf("Stop during GracefulStop = %v, want nil", err)
	}

	select {
	case err := <-stopped:
		if err != nil {
			t.Fatalf("GracefulStop = %v, want nil after Stop", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("GracefulStop kept draining after Stop")
	}

	if err := server.Stop(context.Background()); !errors.Is(err, ErrNotStarted) {
		t.Errorf("second Stop = %v, want ErrNotStarted", err)
	}
}
