// Command demo serves a local Conduit dashboard contract backed by real runtime deliveries.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/xraph/forge/extensions/conduit"
	conduitcontract "github.com/xraph/forge/extensions/conduit/contract"
	"github.com/xraph/forge/extensions/conduit/discovery"
	"github.com/xraph/forge/extensions/conduit/providers/jetstream"
	"github.com/xraph/forge/extensions/conduit/providers/memory"
	dash "github.com/xraph/forge/extensions/dashboard/contract"
	"github.com/xraph/forge/extensions/dashboard/contract/dispatcher"
	dashtransport "github.com/xraph/forge/extensions/dashboard/contract/transport"
	"github.com/xraph/forge/extensions/dashboard/security"
)

func main() {
	if err := run(); err != nil {
		slog.Error("Conduit demo stopped", "error", err)
		os.Exit(1)
	}
}
func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	broker := memory.New()
	registry := discovery.NewStatic()

	var runtimes []*conduit.Runtime

	event := conduit.Event[map[string]string]("orders.placed.v1")

	for _, instance := range []string{"billing-01", "billing-02"} {
		var (
			provider conduit.Provider = broker
			resolver conduit.Registry = registry
		)

		durable := false

		if address := os.Getenv("CONDUIT_DEMO_NATS"); address != "" {
			natsProvider := jetstream.New(jetstream.Options{URL: address})
			provider, resolver, durable = natsProvider, natsProvider, true
		}

		r, err := conduit.New(conduit.Config{Identity: conduit.Identity{Namespace: "demo", ServiceID: "billing", InstanceID: instance}, Version: "1.0.0", Endpoints: []conduit.Endpoint{{Protocol: "http", URL: "http://127.0.0.1:8098"}}, Streams: map[string]conduit.StreamConfig{"orders": {Provider: "events", Subjects: []string{"orders.>"}}}, Subscriptions: map[string]conduit.SubscriptionConfig{"process-orders": {Stream: "orders", Mode: conduit.Competing, Durable: durable, MaxAttempts: 2, RetryDelay: 10 * time.Millisecond}, "refresh-cache": {Stream: "orders", Mode: conduit.Broadcast}}}, conduit.WithProvider("events", provider), conduit.WithRegistry(resolver))
		if err != nil {
			return err
		}

		if err := conduit.Subscribe(r, event, func(_ context.Context, msg conduit.Message[map[string]string]) error {
			if msg.Envelope.TargetConsumer == "" {
				return errors.New("demo payment unavailable")
			}

			return nil
		}, conduit.Consumer("process-orders")); err != nil {
			return err
		}

		if err := conduit.Subscribe(r, event, func(context.Context, conduit.Message[map[string]string]) error { return nil }, conduit.Consumer("refresh-cache")); err != nil {
			return err
		}

		if err := r.Start(ctx); err != nil {
			return err
		}

		runtimes = append(runtimes, r)
	}

	defer func() {
		shutdown, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()

		for _, r := range runtimes {
			_ = r.Stop(shutdown)
		}
	}()

	if _, err := conduit.Publish(ctx, runtimes[0], event, map[string]string{"orderID": "order-demo"}); err != nil {
		return err
	}

	reg, wardens, disp := dash.NewRegistry(), dash.NewWardenRegistry(), dispatcher.New(nil)
	if err := conduitcontract.Register(disp, reg, wardens, conduitcontract.Deps{Runtime: func() *conduit.Runtime { return runtimes[0] }}); err != nil {
		return err
	}

	csrf := security.NewCSRFManager()
	mux := http.NewServeMux()
	base := "/dashboard/api/dashboard/v1"
	mux.Handle(base, dashtransport.NewHandlerWithCSRF(reg, wardens, disp, nil, csrf))
	mux.Handle(base+"/capabilities", dashtransport.NewCapabilitiesHandler(reg, []string{"v1"}, nil))
	mux.Handle(base+"/csrf", dashtransport.NewCSRFTokenHandler(csrf, time.Hour))
	mux.HandleFunc(base+"/principal", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"authenticated": false})
	})
	srv := &http.Server{Addr: "127.0.0.1:8098", Handler: mux, ReadHeaderTimeout: 5 * time.Second}

	go func() {
		<-ctx.Done()

		shutdown, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()

		_ = srv.Shutdown(shutdown)
	}()

	slog.Info("Conduit demo listening", "address", srv.Addr, "durable", os.Getenv("CONDUIT_DEMO_NATS") != "")

	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}

	return nil
}
