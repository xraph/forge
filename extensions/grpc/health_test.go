package grpc

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc/health/grpc_health_v1"
)

// toggleChecker fails while down is set.
type toggleChecker struct {
	down atomic.Bool
}

func (c *toggleChecker) Check(context.Context) error {
	if c.down.Load() {
		return errors.New("down")
	}

	return nil
}

func TestHealthWatchSendsChangesAndEndsOnStop(t *testing.T) {
	old := healthWatchInterval
	healthWatchInterval = 10 * time.Millisecond

	t.Cleanup(func() { healthWatchInterval = old })

	checker := &toggleChecker{}

	server, conn := startTestServer(t, DefaultConfig(), func(s GRPC) {
		s.RegisterHealthChecker("db", checker)
	})

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	stream, err := grpc_health_v1.NewHealthClient(conn).Watch(ctx, &grpc_health_v1.HealthCheckRequest{Service: "db"})
	if err != nil {
		t.Fatal(err)
	}

	next := func(want grpc_health_v1.HealthCheckResponse_ServingStatus) {
		t.Helper()

		resp, err := stream.Recv()
		if err != nil {
			t.Fatalf("Recv: %v", err)
		}

		if resp.GetStatus() != want {
			t.Fatalf("status = %v, want %v", resp.GetStatus(), want)
		}
	}

	next(grpc_health_v1.HealthCheckResponse_SERVING)

	checker.down.Store(true)
	next(grpc_health_v1.HealthCheckResponse_NOT_SERVING)

	checker.down.Store(false)
	next(grpc_health_v1.HealthCheckResponse_SERVING)

	stopped := make(chan error, 1)

	go func() { stopped <- server.GracefulStop(context.Background()) }()

	next(grpc_health_v1.HealthCheckResponse_NOT_SERVING)

	select {
	case err := <-stopped:
		if err != nil {
			t.Fatalf("GracefulStop: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("an open Watch kept GracefulStop from returning")
	}
}

func TestHealthListReportsEveryChecker(t *testing.T) {
	failing := &toggleChecker{}
	failing.down.Store(true)

	_, conn := startTestServer(t, DefaultConfig(), func(s GRPC) {
		s.RegisterHealthChecker("db", &toggleChecker{})
		s.RegisterHealthChecker("cache", failing)
	})

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	resp, err := grpc_health_v1.NewHealthClient(conn).List(ctx, &grpc_health_v1.HealthListRequest{})
	if err != nil {
		t.Fatal(err)
	}

	want := map[string]grpc_health_v1.HealthCheckResponse_ServingStatus{
		"":      grpc_health_v1.HealthCheckResponse_SERVING,
		"db":    grpc_health_v1.HealthCheckResponse_SERVING,
		"cache": grpc_health_v1.HealthCheckResponse_NOT_SERVING,
	}

	got := resp.GetStatuses()
	if len(got) != len(want) {
		t.Fatalf("List returned %d services, want %d: %v", len(got), len(want), got)
	}

	for service, status := range want {
		if got[service].GetStatus() != status {
			t.Errorf("List[%q] = %v, want %v", service, got[service].GetStatus(), status)
		}
	}
}
