package grpc

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/internal/logger"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	testgrpc "google.golang.org/grpc/interop/grpc_testing"
)

// startApp runs an app with only the gRPC extension, built from opts plus a
// random port, and returns the app and a client connection to its server.
func startApp(t *testing.T, opts ...ConfigOption) (forge.App, *grpc.ClientConn) {
	t.Helper()

	app := forge.New(
		forge.WithAppName("grpc-services-test"),
		forge.WithAppVersion("1.0.0"),
		forge.WithAppLogger(logger.NewNoopLogger()),
		forge.WithConfig(forge.DefaultAppConfig()),
		forge.WithExtensions(NewExtension(append(opts, WithAddress("127.0.0.1:0"))...)),
	)

	if err := app.Start(t.Context()); err != nil {
		t.Fatalf("app.Start: %v", err)
	}

	t.Cleanup(func() { _ = app.Stop(context.Background()) })

	svc, err := forge.Inject[*GRPCService](app.Container())
	if err != nil {
		t.Fatal(err)
	}

	addr := svc.Server().(*grpcServer).listener.Addr().String()

	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = conn.Close() })

	return app, conn
}

func emptyCall(t *testing.T, conn *grpc.ClientConn) error {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	_, err := testgrpc.NewTestServiceClient(conn).EmptyCall(ctx, &testgrpc.Empty{}, grpc.WaitForReady(true))

	return err
}

func TestAppServesServiceFromWithService(t *testing.T) {
	_, conn := startApp(t, WithService(&testgrpc.TestService_ServiceDesc, testService{}))

	if err := emptyCall(t, conn); err != nil {
		t.Fatalf("EmptyCall: %v", err)
	}
}

func TestAppRunsServiceRegistrarBeforeStart(t *testing.T) {
	var (
		calls       atomic.Int32
		intercepted atomic.Int32
		gotApp      forge.App
	)

	app, conn := startApp(t, WithServiceRegistrar(func(app forge.App, server GRPC) error {
		calls.Add(1)

		gotApp = app

		if server.IsRunning() {
			t.Error("server is already running inside the registrar")
		}

		server.AddUnaryInterceptor(func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
			intercepted.Add(1)

			return handler(ctx, req)
		})

		return server.RegisterService(&testgrpc.TestService_ServiceDesc, testService{})
	}))

	if err := emptyCall(t, conn); err != nil {
		t.Fatalf("EmptyCall: %v", err)
	}

	if got := calls.Load(); got != 1 {
		t.Errorf("registrar ran %d times, want 1", got)
	}

	if gotApp != app {
		t.Error("registrar did not get the app the extension is registered with")
	}

	if intercepted.Load() == 0 {
		t.Error("interceptor added by the registrar was never called")
	}
}

func TestAppRegisterAfterStartFails(t *testing.T) {
	app, _ := startApp(t)

	server, err := forge.Inject[GRPC](app.Container())
	if err != nil {
		t.Fatal(err)
	}

	err = server.RegisterService(&testgrpc.TestService_ServiceDesc, testService{})
	if !errors.Is(err, ErrRegisterAfterStart) {
		t.Fatalf("RegisterService after app.Start = %v, want ErrRegisterAfterStart", err)
	}
}

func TestAppStartFailsWhenRegistrarFails(t *testing.T) {
	errBoom := errors.New("boom")

	app := forge.New(
		forge.WithAppName("grpc-services-test"),
		forge.WithAppVersion("1.0.0"),
		forge.WithAppLogger(logger.NewNoopLogger()),
		forge.WithConfig(forge.DefaultAppConfig()),
		forge.WithExtensions(NewExtension(
			WithAddress("127.0.0.1:0"),
			WithServiceRegistrar(func(forge.App, GRPC) error { return errBoom }),
		)),
	)

	t.Cleanup(func() { _ = app.Stop(context.Background()) })

	if err := app.Start(t.Context()); !errors.Is(err, errBoom) {
		t.Fatalf("app.Start = %v, want the registrar's error", err)
	}
}

func TestAppStartFailsOnInvalidService(t *testing.T) {
	app := forge.New(
		forge.WithAppName("grpc-services-test"),
		forge.WithAppVersion("1.0.0"),
		forge.WithAppLogger(logger.NewNoopLogger()),
		forge.WithConfig(forge.DefaultAppConfig()),
		forge.WithExtensions(NewExtension(
			WithAddress("127.0.0.1:0"),
			WithService(&testgrpc.TestService_ServiceDesc, struct{}{}),
		)),
	)

	t.Cleanup(func() { _ = app.Stop(context.Background()) })

	if err := app.Start(t.Context()); !errors.Is(err, ErrInvalidService) {
		t.Fatalf("app.Start = %v, want ErrInvalidService", err)
	}
}

func TestWithConfigDropsEarlierServices(t *testing.T) {
	cfg := DefaultConfig()
	for _, opt := range []ConfigOption{
		WithService(&testgrpc.TestService_ServiceDesc, testService{}),
		WithConfig(DefaultConfig()),
		WithService(&testgrpc.TestService_ServiceDesc, testService{}),
	} {
		opt(&cfg)
	}

	if got := len(cfg.Services); got != 1 {
		t.Fatalf("len(Services) = %d, want 1", got)
	}
}
