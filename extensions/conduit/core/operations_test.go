package core

import (
	"context"
	"errors"
	"testing"
)

func TestBackfillCheckpointAndResume(t *testing.T) {
	binding := Binding{Identity: Identity{Namespace: "test", ServiceID: "billing", InstanceID: "one"}, Subscription: SubscriptionConfig{ID: "work", MessageType: "orders.placed.v1"}}
	job := Backfill{Input: BackfillInput{ID: "operation", Subscription: "work", Start: 1, End: 3}, Next: 1}
	read := func(_ context.Context, sequence uint64) (Envelope, error) {
		if sequence == 3 {
			return Envelope{}, ErrNotFound
		}

		return Envelope{ID: "original", Type: "orders.placed.v1"}, nil
	}
	count := 0

	var saved Backfill

	fail := true
	publish := func(_ context.Context, msg Envelope, _ string) error {
		if msg.ID != "original" || msg.TargetConsumer != binding.ConsumerID() {
			t.Fatal("lost original identity")
		}

		count++
		if count == 2 && fail {
			return ErrOutcomeUnknown
		}

		return nil
	}
	save := func(_ context.Context, job Backfill) error {
		saved = job

		return nil
	}

	failed, err := ExecuteBackfill(t.Context(), binding, job, read, publish, save)
	if !errors.Is(err, ErrOutcomeUnknown) || failed.Next != 2 || saved.State != "failed" {
		t.Fatalf("checkpoint=%+v %v", saved, err)
	}

	fail = false

	complete, err := ExecuteBackfill(t.Context(), binding, saved, read, publish, save)
	if err != nil || complete.Published != 2 || complete.Skipped != 1 || complete.State != "complete" {
		t.Fatalf("resume=%+v %v", complete, err)
	}
}
