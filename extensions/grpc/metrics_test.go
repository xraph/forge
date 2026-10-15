package grpc

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/internal/shared"
	"google.golang.org/grpc"
	grpccodes "google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health/grpc_health_v1"
	testgrpc "google.golang.org/grpc/interop/grpc_testing"
)

// startWithApp starts a server wired to a real forge registry so tests can
// scrape what Prometheus would see.
func startWithApp(t *testing.T, cfg Config, register func(GRPC)) (forge.App, *grpc.ClientConn) {
	t.Helper()

	a := forge.New(forge.WithAppName("grpc-metrics"))
	cfg.Address = "127.0.0.1:0"
	server := NewGRPCServer(cfg, forge.NewNoopLogger(), a.Metrics())

	if register != nil {
		register(server)
	}

	if err := server.Start(t.Context(), cfg.Address); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = server.Stop(context.Background()) })

	conn, err := grpc.NewClient(server.(*grpcServer).listener.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = conn.Close() })

	return a, conn
}

func scrape(t *testing.T, a forge.App) string {
	t.Helper()

	rec := httptest.NewRecorder()
	a.Metrics().(shared.PrometheusProvider).PrometheusHandler().
		ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))

	return rec.Body.String()
}

func TestRPCDurationRecorded(t *testing.T) {
	a, conn := startWithApp(t, DefaultConfig(), registerCodeService(&codeService{}))

	if err := callEmpty(t.Context(), t, conn); err != nil {
		t.Fatal(err)
	}

	body := scrape(t, a)
	want := `rpc_server_call_duration_seconds_count{rpc_method="grpc.testing.TestService/EmptyCall",rpc_response_status_code="OK",rpc_system_name="grpc"} 1`

	if !strings.Contains(body, want) {
		t.Fatalf("missing %s in:\n%s", want, body)
	}

	if strings.Contains(body, "grpc_unary_") || strings.Contains(body, "grpc_stream_") {
		t.Fatalf("old grpc_* series still exported:\n%s", body)
	}
}

func TestRPCDurationErrorCode(t *testing.T) {
	a, conn := startWithApp(t, DefaultConfig(), registerCodeService(&codeService{code: grpccodes.Internal}))

	_ = callEmpty(t.Context(), t, conn)

	if !strings.Contains(scrape(t, a), `rpc_response_status_code="INTERNAL"`) {
		t.Fatal("Internal not recorded as INTERNAL")
	}
}

func TestRPCStatusCodeCanonicalNames(t *testing.T) {
	for code, want := range map[grpccodes.Code]string{
		grpccodes.OK:                 "OK",
		grpccodes.Canceled:           "CANCELLED",
		grpccodes.Unknown:            "UNKNOWN",
		grpccodes.InvalidArgument:    "INVALID_ARGUMENT",
		grpccodes.DeadlineExceeded:   "DEADLINE_EXCEEDED",
		grpccodes.NotFound:           "NOT_FOUND",
		grpccodes.AlreadyExists:      "ALREADY_EXISTS",
		grpccodes.PermissionDenied:   "PERMISSION_DENIED",
		grpccodes.ResourceExhausted:  "RESOURCE_EXHAUSTED",
		grpccodes.FailedPrecondition: "FAILED_PRECONDITION",
		grpccodes.Aborted:            "ABORTED",
		grpccodes.OutOfRange:         "OUT_OF_RANGE",
		grpccodes.Unimplemented:      "UNIMPLEMENTED",
		grpccodes.Internal:           "INTERNAL",
		grpccodes.Unavailable:        "UNAVAILABLE",
		grpccodes.DataLoss:           "DATA_LOSS",
		grpccodes.Unauthenticated:    "UNAUTHENTICATED",
		grpccodes.Code(99):           "CODE(99)",
	} {
		if got := canonicalCode(code); got != want {
			t.Errorf("canonicalCode(%v) = %q, want %q", code, got, want)
		}
	}
}

type streamService struct {
	testgrpc.UnimplementedTestServiceServer
}

func (streamService) StreamingOutputCall(_ *testgrpc.StreamingOutputCallRequest, stream testgrpc.TestService_StreamingOutputCallServer) error {
	for {
		if err := stream.Send(&testgrpc.StreamingOutputCallResponse{}); err != nil {
			return err
		}

		select {
		case <-stream.Context().Done():
			return stream.Context().Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func TestRPCStreamCanceledRecordedOnce(t *testing.T) {
	a, conn := startWithApp(t, DefaultConfig(), func(s GRPC) {
		_ = s.RegisterService(&testgrpc.TestService_ServiceDesc, streamService{})
	})

	ctx, cancel := context.WithCancel(t.Context())

	st, err := testgrpc.NewTestServiceClient(conn).StreamingOutputCall(ctx, &testgrpc.StreamingOutputCallRequest{}, grpc.WaitForReady(true))
	if err != nil {
		t.Fatal(err)
	}

	if _, err := st.Recv(); err != nil {
		t.Fatal(err)
	}

	cancel()

	want := `rpc_server_call_duration_seconds_count{rpc_method="grpc.testing.TestService/StreamingOutputCall",rpc_response_status_code="CANCELLED",rpc_system_name="grpc"} 1`

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(scrape(t, a), want) {
			return
		}

		time.Sleep(20 * time.Millisecond)
	}

	t.Fatalf("canceled stream not recorded once as CANCELLED:\n%s", scrape(t, a))
}

func TestMetricsDisabledNoSeries(t *testing.T) {
	cfg := DefaultConfig()
	cfg.EnableMetrics = false

	a, conn := startWithApp(t, cfg, registerCodeService(&codeService{}))

	if err := callEmpty(t.Context(), t, conn); err != nil {
		t.Fatal(err)
	}

	if strings.Contains(scrape(t, a), "rpc_server_call_duration_seconds") {
		t.Fatal("EnableMetrics=false still records rpc metrics")
	}
}

func TestHealthCheckHasNoMetric(t *testing.T) {
	cfg := DefaultConfig()
	cfg.EnableHealthCheck = true

	a, conn := startWithApp(t, cfg, registerCodeService(&codeService{}))

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	if _, err := grpc_health_v1.NewHealthClient(conn).Check(ctx, &grpc_health_v1.HealthCheckRequest{}, grpc.WaitForReady(true)); err != nil {
		t.Fatal(err)
	}

	// A real call proves the histogram is live, so the absence below means
	// the health call was filtered rather than the metric being off.
	if err := callEmpty(t.Context(), t, conn); err != nil {
		t.Fatal(err)
	}

	body := scrape(t, a)

	if !strings.Contains(body, `rpc_method="grpc.testing.TestService/EmptyCall"`) {
		t.Fatalf("EmptyCall not recorded:\n%s", body)
	}

	if strings.Contains(body, `rpc_method="grpc.health.v1.Health/`) {
		t.Fatalf("health check recorded as an rpc metric:\n%s", body)
	}
}

func TestStatsStillUpdated(t *testing.T) {
	server, conn := startTestServer(t, DefaultConfig(), registerCodeService(&codeService{}))

	if err := callEmpty(t.Context(), t, conn); err != nil {
		t.Fatal(err)
	}

	if st := server.GetStats(); st.RPCsStarted != 1 || st.RPCsSucceeded != 1 || st.RPCsFailed != 0 {
		t.Fatalf("stats = %+v", st)
	}
}

func TestIsInfraMethod(t *testing.T) {
	for method, want := range map[string]bool{
		"/grpc.health.v1.Health/Check":                                   true,
		"/grpc.reflection.v1.ServerReflection/ServerReflectionInfo":      true,
		"/grpc.reflection.v1alpha.ServerReflection/ServerReflectionInfo": true,
		"/grpc.testing.TestService/EmptyCall":                            false,
	} {
		if got := isInfraMethod(method); got != want {
			t.Errorf("isInfraMethod(%q) = %v, want %v", method, got, want)
		}
	}
}
