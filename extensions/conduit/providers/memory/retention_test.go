package memory

import (
	"context"
	"testing"
	"time"

	"github.com/xraph/forge/extensions/conduit/core"
)

func TestRetentionUsesBrokerAcceptanceAndPrunesInspection(t *testing.T) {
	broker := New()

	cfg := core.StreamConfig{Name: "orders", Subjects: []string{"orders.>"}, MaxAge: time.Hour, Replicas: 1}
	if err := broker.EnsureStream(t.Context(), "test", cfg); err != nil {
		t.Fatal(err)
	}

	identity := core.Identity{Namespace: "test", ServiceID: "billing", InstanceID: "first"}
	binding := core.Binding{Identity: identity, Stream: cfg, Subscription: core.SubscriptionConfig{ID: "process", MessageType: "orders.placed.v1", Mode: core.Competing, Timeout: time.Second, MaxInFlight: 1}}

	sub, err := broker.Subscribe(t.Context(), binding)
	if err != nil {
		t.Fatal(err)
	}

	msg := core.Envelope{ID: "old-original-id", Type: "orders.placed.v1", CreatedAt: time.Now().Add(-24 * time.Hour)}
	if _, err := broker.Publish(t.Context(), "test", cfg, msg); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	delivery, err := sub.Next(ctx)
	if err != nil || delivery.Message().ID != msg.ID {
		t.Fatalf("fresh acceptance must deliver an older envelope: %v", err)
	}

	broker.mu.Lock()
	stream := broker.streams[streamKey("test", cfg.Name)]
	stream.records[0].acceptedAt = time.Now().Add(-2 * time.Hour)
	broker.mu.Unlock()

	info, err := broker.Inspect(t.Context(), "test", cfg)
	if err != nil || info.Messages != 0 {
		t.Fatalf("expired records remained in inspection: %+v %v", info, err)
	}

	if len(stream.consumers[binding.ConsumerID()].pending) != 0 {
		t.Fatal("expired pending delivery retained")
	}
}
