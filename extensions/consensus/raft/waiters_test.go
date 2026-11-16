package raft

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/xraph/forge/extensions/consensus/internal"
)

// proposeAsync starts a Propose and returns its result channel once the
// entry is in the log.
func proposeAsync(t *testing.T, n *Node, want uint64) <-chan error {
	t.Helper()

	done := make(chan error, 1)

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		done <- n.Propose(ctx, []byte(`{"type":"set","payload":{"key":"k","value":"v"}}`))
	}()

	deadline := time.Now().Add(5 * time.Second)
	for n.log.LastIndex() < want {
		if time.Now().After(deadline) {
			t.Fatal("proposal never reached the log")
		}

		time.Sleep(time.Millisecond)
	}

	return done
}

func expectDropped(t *testing.T, done <-chan error) {
	t.Helper()

	select {
	case err := <-done:
		if !errors.Is(err, ErrProposalDropped) {
			t.Fatalf("Propose returned %v, want ErrProposalDropped", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Propose still waiting after its entry was truncated")
	}
}

// A leader proposes an entry, loses its term before the entry commits, and a
// new leader overwrites that index. The waiting Propose fails at once with
// ErrProposalDropped instead of hanging until its deadline.
func TestProposalDroppedByTruncationFailsPromptly(t *testing.T) {
	n, _ := newLeader(t) // term 2, log 1..10 in term 2, one peer "p"
	done := proposeAsync(t, n, 11)

	n.handleAppendEntries(internal.AppendEntriesRequest{
		Term: 3, LeaderID: "p", PrevLogIndex: 10, PrevLogTerm: 2, Entries: ents(3, 11, 11),
	})

	expectDropped(t, done)
}

// After losing an entry at index 11, the node leads again and a new entry
// lands at the same index. The old waiter must not be overwritten (and left
// hanging) or completed by the new entry; the new waiter completes normally.
func TestWaitersAreMatchedByIndexAndTerm(t *testing.T) {
	n, _ := newLeader(t)

	// The old leader's entry at 11 is still pending when a newer leader's
	// different entry is committed and applied at 11 (this node never saw
	// the truncation, for example because the new entry came by snapshot).
	n.mu.Lock()
	oldWaiter := n.addWaiterLocked(11, 2)
	newWaiter := n.addWaiterLocked(11, 4)
	n.resolveAppliedLocked(11, 4, nil)
	n.mu.Unlock()

	if err := <-oldWaiter; !errors.Is(err, ErrProposalDropped) {
		t.Fatalf("waiter for the displaced entry got %v, want ErrProposalDropped", err)
	}

	if err := <-newWaiter; err != nil {
		t.Fatalf("waiter for the applied entry got %v", err)
	}

	if len(n.applyWaiters) != 0 {
		t.Fatalf("%d waiters left behind", len(n.applyWaiters))
	}
}
