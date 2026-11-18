package raft

import (
	"github.com/xraph/forge/errors"
)

// ErrProposalDropped is returned by Apply and Propose when the entry was
// removed from the log after a leadership change, before it committed. The
// entry was never applied, so the request can be retried.
var ErrProposalDropped = errors.New("raft: the proposed entry was dropped by a leadership change before it committed; it was not applied")

// errSupersededBySnapshot is returned when a snapshot from a new leader
// replaced the log around a pending entry, so its outcome cannot be told.
var errSupersededBySnapshot = errors.New("raft: entry was superseded by a snapshot from the leader; its result is unknown")

// waiterKey identifies one proposed entry. The term is part of the key: after
// a leadership change a different entry can occupy the same index, and it
// must not complete, or replace, the waiter of the entry it displaced.
type waiterKey struct {
	index uint64
	term  uint64
}

// addWaiterLocked registers an Apply call waiting for the entry at index and
// term. The caller holds n.mu.
func (n *Node) addWaiterLocked(index, term uint64) chan error {
	ch := make(chan error, 1)
	n.applyWaiters[waiterKey{index: index, term: term}] = ch

	return ch
}

// removeWaiterLocked forgets a waiter that gave up. The caller holds n.mu.
func (n *Node) removeWaiterLocked(index, term uint64, ch chan error) {
	key := waiterKey{index: index, term: term}
	if n.applyWaiters[key] == ch {
		delete(n.applyWaiters, key)
	}
}

// resolveAppliedLocked completes the waiter for the entry just applied and
// fails any waiter for a different entry that had the same index. The caller
// holds n.mu.
func (n *Node) resolveAppliedLocked(index, term uint64, applyErr error) {
	for key, ch := range n.applyWaiters {
		if key.index != index {
			continue
		}

		if key.term == term {
			ch <- applyErr
		} else {
			ch <- ErrProposalDropped
		}

		delete(n.applyWaiters, key)
	}
}

// failWaitersLocked fails every waiter whose key matches with err. The caller
// holds n.mu.
func (n *Node) failWaitersLocked(match func(waiterKey) bool, err error) {
	for key, ch := range n.applyWaiters {
		if match(key) {
			ch <- err

			delete(n.applyWaiters, key)
		}
	}
}

// failTruncatedWaitersLocked fails the waiters of entries removed from the
// log after index. The caller holds n.mu.
func (n *Node) failTruncatedWaitersLocked(after uint64) {
	n.failWaitersLocked(func(k waiterKey) bool { return k.index > after }, ErrProposalDropped)
}
