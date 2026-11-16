package streaming

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/xraph/forge/extensions/streaming/coordinator"
)

// blockingTracker is a coordinator whose user tracking calls block until
// released, standing in for a slow Redis.
type blockingTracker struct {
	coordinator.StreamCoordinator

	release chan struct{}
	entered chan string

	mu      sync.Mutex
	tracked map[string]bool
	sawCtx  []bool // whether each call's context had a deadline
}

func newBlockingTracker() *blockingTracker {
	return &blockingTracker{
		release: make(chan struct{}),
		entered: make(chan string, 16),
		tracked: make(map[string]bool),
	}
}

func (b *blockingTracker) record(ctx context.Context, userID string, on bool) error {
	b.entered <- userID

	<-b.release

	_, hasDeadline := ctx.Deadline()

	b.mu.Lock()
	defer b.mu.Unlock()

	b.tracked[userID] = on
	b.sawCtx = append(b.sawCtx, hasDeadline)

	return nil
}

func (b *blockingTracker) BroadcastToRoom(context.Context, string, *Message) error { return nil }

func (b *blockingTracker) TrackUserNode(ctx context.Context, userID, _ string) error {
	return b.record(ctx, userID, true)
}

func (b *blockingTracker) UntrackUserNode(ctx context.Context, userID, _ string) error {
	return b.record(ctx, userID, false)
}

// Register and Unregister held the manager's write lock across the
// coordinator's Redis SADD/SREM, with context.Background(). One slow round
// trip stalled every connect, disconnect and broadcast on the node.
func TestRegister_CoordinatorCallDoesNotHoldManagerLock(t *testing.T) {
	tracker := newBlockingTracker()
	m, _, _ := realManager(t, WithCoordinator(tracker), WithManagerNodeID("node-1"))

	slow := newTestConn("slow", "alice")

	registered := make(chan error, 1)

	go func() { registered <- m.Register(slow) }()

	select {
	case <-tracker.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("Register never reached the coordinator")
	}

	// Alice's Register is now parked in the coordinator call. The rest of the
	// node must carry on.
	done := make(chan struct{})

	go func() {
		defer close(done)

		_ = m.ConnectionCount()
		_ = m.GetAllConnections()
		_ = m.BroadcastToRoom(context.Background(), "any", &Message{Type: MessageTypeMessage, RoomID: "any"})

		if err := m.Register(newTestConn("anon", "")); err != nil {
			t.Error(err)
		}
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("manager blocked behind a pending coordinator call")
	}

	close(tracker.release)

	if err := <-registered; err != nil {
		t.Fatal(err)
	}

	assertNoError(t, m.Unregister(slow.ID()))

	tracker.mu.Lock()
	defer tracker.mu.Unlock()

	if tracker.tracked["alice"] {
		t.Fatal("alice still tracked on this node after her last connection left")
	}

	for i, hasDeadline := range tracker.sawCtx {
		if !hasDeadline {
			t.Errorf("coordinator call %d ran with no deadline", i)
		}
	}
}

// With the calls outside the lock, connects and disconnects of one user race
// to the coordinator. Whatever the interleaving, the final record must match
// the registry.
func TestRegister_CoordinatorTrackingConvergesUnderRace(t *testing.T) {
	tracker := newBlockingTracker()
	close(tracker.release)

	m, _, _ := realManager(t, WithCoordinator(tracker), WithManagerNodeID("node-1"))

	go func() {
		for range tracker.entered {
		}
	}()

	var wg sync.WaitGroup

	for i := range 50 {
		wg.Go(func() {
			c := newTestConn(fmt.Sprintf("c%d", i), "bob")
			if err := m.Register(c); err != nil {
				return // per-user limit; fine
			}

			if i%3 != 0 {
				_ = m.Unregister(c.ID())
			}
		})
	}

	wg.Wait()

	connected := len(m.GetUserConnections("bob")) > 0

	tracker.mu.Lock()
	defer tracker.mu.Unlock()

	if tracker.tracked["bob"] != connected {
		t.Fatalf("coordinator says tracked=%v, registry says connected=%v", tracker.tracked["bob"], connected)
	}
}
