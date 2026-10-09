package conduit_test

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xraph/forge/extensions/conduit"
	"github.com/xraph/forge/extensions/conduit/providers/jetstream"
	"github.com/xraph/forge/extensions/conduit/providers/memory"
)

type rpcInput struct {
	Value int `json:"value"`
}
type rpcOutput struct {
	Value    int    `json:"value"`
	Instance string `json:"instance"`
}

var double = conduit.Procedure[rpcInput, rpcOutput]("billing.double.v1")

func rpcProviders(t *testing.T, kind string) func() conduit.Provider {
	t.Helper()

	if kind == "jetstream" {
		url := brokerServer(t, t.TempDir(), -1).ClientURL()

		return func() conduit.Provider { return jetstream.New(jetstream.Options{URL: url}) }
	}

	broker := memory.New()

	return func() conduit.Provider { return broker }
}
func rpcRuntime(t *testing.T, service, instance string, p conduit.Provider, limits conduit.RPCConfig) *conduit.Runtime {
	t.Helper()

	return runtimeFor(t, conduit.Config{Identity: conduit.Identity{Namespace: "test", ServiceID: service, InstanceID: instance}, RPC: limits}, p)
}
func TestRPCReplicasAndPublicErrors(t *testing.T) {
	for _, kind := range []string{"memory", "jetstream"} {
		t.Run(kind, func(t *testing.T) {
			provider := rpcProviders(t, kind)
			calls := map[string]*atomic.Int32{"a": {}, "b": {}}
			members := map[string]*conduit.Runtime{}

			for _, instance := range []string{"a", "b"} {
				r := rpcRuntime(t, "billing", instance, provider(), conduit.RPCConfig{})

				members[instance] = r
				if err := conduit.Handle(r, double, func(_ context.Context, request conduit.Request[rpcInput]) (rpcOutput, error) {
					calls[instance].Add(1)

					if request.Envelope.Source.ServiceID != "client" || request.Envelope.CorrelationID != "trace" {
						return rpcOutput{}, errors.New("missing correlation")
					}

					switch request.Data.Value {
					case -1:
						return rpcOutput{}, errors.New("secret=password")
					case -2:
						return rpcOutput{}, &conduit.RPCError{Code: conduit.RPCPermissionDenied, Message: "Billing access required"}
					}

					return rpcOutput{Value: request.Data.Value * 2, Instance: instance}, nil
				}); err != nil {
					t.Fatal(err)
				}

				start(t, r)
			}

			client := rpcRuntime(t, "client", "caller", provider(), conduit.RPCConfig{})
			start(t, client)

			seen := map[string]bool{}

			for i := range 40 {
				reply, err := conduit.Call(t.Context(), client, "billing", double, rpcInput{Value: i}, conduit.Correlation("trace"))
				if err != nil || reply.Value != i*2 {
					t.Fatalf("reply=%+v error=%v", reply, err)
				}

				seen[reply.Instance] = true
			}

			if len(seen) != 2 || calls["a"].Load()+calls["b"].Load() != 40 {
				t.Fatalf("distribution=%v", seen)
			}

			for _, test := range []struct {
				value int
				code  conduit.RPCCode
			}{{-1, conduit.RPCInternal}, {-2, conduit.RPCPermissionDenied}} {
				_, err := conduit.Call(t.Context(), client, "billing", double, rpcInput{Value: test.value}, conduit.Correlation("trace"))

				var public *conduit.RPCError
				if !errors.As(err, &public) || public.Code != test.code || strings.Contains(err.Error(), "secret") {
					t.Fatalf("error=%v", err)
				}
			}

			if _, err := conduit.Call(t.Context(), client, "missing", double, rpcInput{}); !errors.Is(err, conduit.ErrNotFound) {
				t.Fatalf("missing=%v", err)
			}

			if err := members["a"].Stop(t.Context()); err != nil {
				t.Fatal(err)
			}

			reply, err := conduit.Call(t.Context(), client, "billing", double, rpcInput{Value: 3}, conduit.Correlation("trace"))
			if err != nil || reply.Instance != "b" {
				t.Fatalf("departure=%+v %v", reply, err)
			}
		})
	}
}
func TestRPCCancellationOverloadAndDrain(t *testing.T) {
	for _, kind := range []string{"memory", "jetstream"} {
		t.Run(kind, func(t *testing.T) {
			provider := rpcProviders(t, kind)
			r := rpcRuntime(t, "billing", "a", provider(), conduit.RPCConfig{Concurrency: 1, MaxInFlight: 1})
			entered := make(chan struct{}, 4)
			canceled := make(chan struct{}, 4)
			release := make(chan struct{})

			if err := conduit.Handle(r, double, func(ctx context.Context, request conduit.Request[rpcInput]) (rpcOutput, error) {
				entered <- struct{}{}

				select {
				case <-ctx.Done():
					canceled <- struct{}{}

					return rpcOutput{}, ctx.Err()
				case <-release:
					return rpcOutput{Value: request.Data.Value * 2}, nil
				}
			}); err != nil {
				t.Fatal(err)
			}

			start(t, r)
			client := rpcRuntime(t, "client", "caller", provider(), conduit.RPCConfig{})
			start(t, client)
			ctx, cancel := context.WithCancel(t.Context())
			first := make(chan error, 1)

			go func() { _, err := conduit.Call(ctx, client, "billing", double, rpcInput{}); first <- err }()

			<-entered

			_, err := conduit.Call(t.Context(), client, "billing", double, rpcInput{})

			var public *conduit.RPCError
			if !errors.As(err, &public) || public.Code != conduit.RPCUnavailable {
				t.Fatalf("overload=%v", err)
			}

			cancel()

			if err := <-first; !errors.Is(err, context.Canceled) {
				t.Fatalf("cancel=%v", err)
			}

			select {
			case <-canceled:
			case <-time.After(time.Second):
				t.Fatal("server ignored cancellation")
			}
			// The response must settle before its concurrency credit is released.
			time.Sleep(30 * time.Millisecond)

			deadline, stopDeadline := context.WithTimeout(t.Context(), 50*time.Millisecond)
			defer stopDeadline()

			_, err = conduit.Call(deadline, client, "billing", double, rpcInput{})
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("deadline=%v", err)
			}

			<-entered

			select {
			case <-canceled:
			case <-time.After(time.Second):
				t.Fatal("server ignored deadline")
			}

			time.Sleep(30 * time.Millisecond)

			final := make(chan error, 1)

			go func() {
				reply, err := conduit.Call(t.Context(), client, "billing", double, rpcInput{Value: 4})
				if err == nil && reply.Value != 8 {
					err = errors.New("bad drained reply")
				}

				final <- err
			}()

			<-entered

			stopped := make(chan error, 1)
			go func() { stopped <- r.Stop(t.Context()) }()

			wait(t, func() bool {
				snapshot, err := r.Snapshot(t.Context())

				return err == nil && !snapshot.Running
			})
			close(release)

			if err := <-final; err != nil {
				t.Fatalf("drained reply=%v", err)
			}

			if err := <-stopped; err != nil {
				t.Fatal(err)
			}
		})
	}
}
