package transport_test

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/xraph/forge/extensions/conduit/core"
	"github.com/xraph/forge/extensions/conduit/discovery"
	"github.com/xraph/forge/extensions/conduit/transport"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
)

func TestHTTPDiscoveryReplicasAndRemoval(t *testing.T) {
	registry := discovery.NewStatic()
	client := (&transport.Services{Resolver: registry, Identity: core.Identity{Namespace: "prod", ServiceID: "orders", InstanceID: "orders-1"}}).HTTP("billing", nil)

	for _, id := range []string{"one", "two"} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/pay" || r.URL.Query().Get("id") != "42" || r.Header.Get("Authorization") != "Bearer test" || r.Header.Get("X-Forge-Service-Id") != "orders" {
				t.Error("request data was lost")
			}

			_, _ = io.WriteString(w, id)
		}))
		t.Cleanup(srv.Close)

		if err := registry.Register(t.Context(), core.Instance{Identity: core.Identity{Namespace: "prod", ServiceID: "billing", InstanceID: id}, Ready: true, Endpoints: []core.Endpoint{{Protocol: "http", URL: srv.URL + "/api"}}}); err != nil {
			t.Fatal(err)
		}
	}

	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://billing/pay?id=42", nil)
	if err != nil {
		t.Fatal(err)
	}

	request.Header.Set("Authorization", "Bearer test")

	seen := map[string]bool{}

	for range 4 {
		response, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}

		body, err := io.ReadAll(response.Body)
		_ = response.Body.Close()

		if err != nil {
			t.Fatal(err)
		}

		seen[string(body)] = true
	}

	if len(seen) != 2 || request.URL.Host != "billing" {
		t.Fatal("replicas or request immutability failed")
	}

	if err := registry.Deregister(t.Context(), core.Identity{Namespace: "prod", ServiceID: "billing", InstanceID: "one"}); err != nil {
		t.Fatal(err)
	}

	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}

	body, err := io.ReadAll(response.Body)
	_ = response.Body.Close()

	if err != nil || string(body) != "two" {
		t.Fatal("removed instance remained discoverable")
	}

	if err := registry.Deregister(t.Context(), core.Identity{Namespace: "prod", ServiceID: "billing", InstanceID: "two"}); err != nil {
		t.Fatal(err)
	}

	if response, err := client.Do(request); err == nil {
		_ = response.Body.Close()

		t.Fatal("missing service should fail")
	}
}

func TestGRPCGeneratedClientByName(t *testing.T) {
	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	srv := grpc.NewServer()

	grpc_health_v1.RegisterHealthServer(srv, health.NewServer())
	go func() { _ = srv.Serve(listener) }()

	t.Cleanup(srv.Stop)

	registry := discovery.NewStatic(core.Instance{Identity: core.Identity{Namespace: "prod", ServiceID: "billing", InstanceID: "one"}, Ready: true, Endpoints: []core.Endpoint{{Protocol: "grpc", URL: "grpc://" + listener.Addr().String()}}})
	services := &transport.Services{Identity: core.Identity{Namespace: "prod"}, Resolver: registry}

	conn, err := services.GRPC("billing", insecure.NewCredentials())
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = conn.Close() })

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	result, err := grpc_health_v1.NewHealthClient(conn).Check(ctx, &grpc_health_v1.HealthCheckRequest{}, grpc.WaitForReady(true))
	if err != nil || result.GetStatus() != grpc_health_v1.HealthCheckResponse_SERVING {
		t.Fatalf("named generated client failed: %v", err)
	}

	if _, err := services.GRPC("billing", nil); err == nil {
		t.Fatal("credentials must be explicit")
	}

	if err := registry.Deregister(t.Context(), core.Identity{Namespace: "prod", ServiceID: "billing", InstanceID: "one"}); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		callCtx, callCancel := context.WithTimeout(t.Context(), 250*time.Millisecond)
		_, callErr := grpc_health_v1.NewHealthClient(conn).Check(callCtx, &grpc_health_v1.HealthCheckRequest{})

		callCancel()

		if callErr != nil {
			return
		}

		time.Sleep(50 * time.Millisecond)
	}

	t.Fatal("removed instance remained reachable through the named gRPC client")
}
