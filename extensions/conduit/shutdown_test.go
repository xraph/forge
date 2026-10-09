package conduit_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/xraph/forge/extensions/conduit"
)

func TestShutdownDrainsEventsAndDerivedPublication(t *testing.T) {
	for _, kind := range []string{"memory", "jetstream"} {
		t.Run(kind, func(t *testing.T) {
			provider := rpcProviders(t, kind)
			cfg := config("billing", "one", false, conduit.Broadcast)
			r := runtimeFor(t, cfg, provider())
			entered := make(chan struct{})
			release := make(chan struct{})
			effect := make(chan error, 1)

			if err := conduit.Subscribe(r, placed, func(ctx context.Context, _ conduit.Message[order]) error {
				close(entered)
				<-release

				_, err := conduit.Publish(ctx, r, placed, order{ID: "derived"})
				effect <- err

				return err
			}, conduit.Consumer("process")); err != nil {
				t.Fatal(err)
			}

			start(t, r)

			if _, err := conduit.Publish(t.Context(), r, placed, order{ID: "original"}); err != nil {
				t.Fatal(err)
			}

			<-entered

			stopped := make(chan error, 1)
			go func() { stopped <- r.Stop(t.Context()) }()

			wait(t, func() bool {
				snapshot, _ := r.Snapshot(t.Context())

				return !snapshot.Running
			})
			close(release)

			if err := <-effect; err != nil {
				t.Fatalf("draining publication=%v", err)
			}

			if err := <-stopped; err != nil {
				t.Fatal(err)
			}

			acknowledged := false

			for _, event := range r.RecentEvents() {
				if event.Stage == conduit.Acknowledged {
					acknowledged = true
				}

				if event.Stage == conduit.SettlementFailed {
					t.Fatal("accepted event lost settlement during shutdown")
				}
			}

			if !acknowledged {
				t.Fatal("event was not acknowledged before disconnect")
			}
		})
	}
}
func TestShutdownDeadlineCancelsRPCAndClosesProviders(t *testing.T) {
	for _, kind := range []string{"memory", "jetstream"} {
		t.Run(kind, func(t *testing.T) {
			provider := rpcProviders(t, kind)
			p := provider()
			r := rpcRuntime(t, "billing", "one", p, conduit.RPCConfig{})
			entered := make(chan struct{})
			canceled := make(chan struct{})

			if err := conduit.Handle(r, double, func(ctx context.Context, _ conduit.Request[rpcInput]) (rpcOutput, error) {
				close(entered)
				<-ctx.Done()
				close(canceled)

				return rpcOutput{}, ctx.Err()
			}); err != nil {
				t.Fatal(err)
			}

			start(t, r)
			client := rpcRuntime(t, "client", "one", provider(), conduit.RPCConfig{})
			start(t, client)

			caller, stopCaller := context.WithCancel(t.Context())
			defer stopCaller()

			result := make(chan error, 1)

			go func() { _, err := conduit.Call(caller, client, "billing", double, rpcInput{}); result <- err }()

			<-entered

			shutdown, stopShutdown := context.WithTimeout(t.Context(), 50*time.Millisecond)
			defer stopShutdown()

			if err := r.Stop(shutdown); !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("shutdown=%v", err)
			}

			select {
			case <-canceled:
			case <-time.After(time.Second):
				t.Fatal("shutdown ignored handler cancellation")
			}

			if kind == "jetstream" && p.Health(t.Context()) == nil {
				t.Fatal("provider leaked after shutdown deadline")
			}

			stopCaller()
			<-result
		})
	}
}
