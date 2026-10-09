package conduit_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xraph/forge/extensions/conduit"
)

func TestConsumerControlsAndTargetedBackfill(t *testing.T) {
	for _, kind := range []string{"memory", "jetstream"} {
		t.Run(kind, func(t *testing.T) {
			provider := rpcProviders(t, kind)
			counts := map[string]*atomic.Int32{"a": {}, "b": {}}
			members := map[string]*conduit.Runtime{}

			for _, instance := range []string{"a", "b"} {
				cfg := config("billing", instance, kind == "jetstream", conduit.Broadcast)
				r := runtimeFor(t, cfg, provider())

				members[instance] = r
				if err := conduit.Subscribe(r, placed, func(context.Context, conduit.Message[order]) error {
					counts[instance].Add(1)

					return nil
				}, conduit.Consumer("process")); err != nil {
					t.Fatal(err)
				}

				start(t, r)
			}

			if err := members["a"].PauseSubscription(t.Context(), "process", true); err != nil {
				t.Fatal(err)
			}

			if _, err := conduit.Publish(t.Context(), members["b"], placed, order{ID: "original"}, conduit.MessageID("original")); err != nil {
				t.Fatal(err)
			}

			wait(t, func() bool { return counts["b"].Load() == 1 })
			time.Sleep(50 * time.Millisecond)

			if counts["a"].Load() != 0 {
				t.Fatal("paused subscriber handled an event")
			}

			rows, err := members["a"].Consumers(t.Context())
			if err != nil || len(rows) != 1 || !rows[0].Paused || rows[0].Pending != 1 {
				t.Fatalf("paused info=%+v %v", rows, err)
			}

			if err := members["a"].PauseSubscription(t.Context(), "process", false); err != nil {
				t.Fatal(err)
			}

			wait(t, func() bool { return counts["a"].Load() == 1 })

			in := conduit.BackfillInput{ID: "recovery-1", Subscription: "process", Start: 1, End: 1}

			job, err := members["a"].Backfill(t.Context(), in)
			if err != nil || job.State != "complete" || job.Published != 1 || job.Persisted != (kind == "jetstream") {
				t.Fatalf("job=%+v %v", job, err)
			}

			wait(t, func() bool { return counts["a"].Load() == 2 })
			time.Sleep(50 * time.Millisecond)

			if counts["b"].Load() != 1 {
				t.Fatal("backfill escaped target subscriber")
			}

			job, err = members["a"].Backfill(t.Context(), in)
			if err != nil || job.Published != 1 {
				t.Fatalf("idempotent job=%+v %v", job, err)
			}

			in.End = 2
			if _, err := members["a"].Backfill(t.Context(), in); !errors.Is(err, conduit.ErrConflict) {
				t.Fatalf("changed operation=%v", err)
			}

			jobs, _, err := members["b"].Backfills(t.Context(), "broker", "", 25)
			if err != nil || len(jobs) != 1 {
				t.Fatalf("service history=%v %v", jobs, err)
			}

			rows, err = members["a"].Consumers(t.Context())
			if err != nil || rows[0].Processing.Count < 2 || rows[0].Delivery.Count < 2 {
				t.Fatalf("latency=%+v %v", rows, err)
			}
		})
	}
}
