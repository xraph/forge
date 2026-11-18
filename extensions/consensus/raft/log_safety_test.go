package raft

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/consensus/internal"
	"github.com/xraph/forge/extensions/consensus/statemachine"
	"github.com/xraph/forge/extensions/consensus/storage"
	"github.com/xraph/forge/extensions/consensus/transport"
)

func newMemStorage(t testing.TB) *storage.MemoryStorage {
	t.Helper()

	st := storage.NewMemoryStorage(storage.MemoryStorageConfig{}, forge.NewNoopLogger())
	if err := st.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	return st
}

func newTestNode(t *testing.T, id string, st internal.Storage) *Node {
	t.Helper()

	lg := forge.NewNoopLogger()
	tr := transport.NewLocalTransport(transport.LocalTransportConfig{NodeID: id}, lg)
	sm := statemachine.NewMemoryStateMachine(statemachine.MemoryStateMachineConfig{}, lg)

	n, err := NewNode(Config{NodeID: id}, lg, sm, tr, st)
	if err != nil {
		t.Fatal(err)
	}

	return n
}

// voteReady makes the candidates voting members and forgets the last leader
// contact, so vote requests are judged on term and log alone.
func voteReady(n *Node, candidates ...string) {
	for _, c := range candidates {
		n.AddPeer(c)
	}

	n.mu.Lock()
	n.lastLeaderContact = time.Time{}
	n.mu.Unlock()
}

func ents(term, from, to uint64) []internal.LogEntry {
	var out []internal.LogEntry
	for i := from; i <= to; i++ {
		out = append(out, internal.LogEntry{Index: i, Term: term, Data: []byte("x")})
	}

	return out
}

func logIndexes(t *testing.T, n *Node) []uint64 {
	t.Helper()

	got, err := n.log.GetEntriesAfter(0, 1000)
	if err != nil {
		t.Fatal(err)
	}

	idx := make([]uint64, 0, len(got))
	for _, e := range got {
		idx = append(idx, e.Index)
	}

	return idx
}

func assertLog(t *testing.T, n *Node, wantLast uint64) {
	t.Helper()

	idx := logIndexes(t, n)
	if uint64(len(idx)) != wantLast || n.log.LastIndex() != wantLast {
		t.Fatalf("log = %v (LastIndex %d), want indexes 1..%d", idx, n.log.LastIndex(), wantLast)
	}

	for i, v := range idx {
		if v != uint64(i+1) {
			t.Fatalf("log = %v, want contiguous indexes 1..%d", idx, wantLast)
		}
	}
}

// A retransmitted AppendEntries must not duplicate entries.
func TestAppendEntriesRetransmitIsIdempotent(t *testing.T) {
	n := newTestNode(t, "f", newMemStorage(t))
	req := internal.AppendEntriesRequest{Term: 1, LeaderID: "l", Entries: ents(1, 1, 3)}

	for range 2 {
		if resp := n.handleAppendEntries(req); !resp.Success {
			t.Fatalf("AppendEntries rejected: %+v", resp)
		}
	}

	assertLog(t, n, 3)
}

// A delayed duplicate of an old, shorter AppendEntries must not move the log
// backwards, and the node must keep refusing votes to candidates that miss
// committed entries.
func TestDelayedDuplicateDoesNotShrinkLog(t *testing.T) {
	n := newTestNode(t, "f", newMemStorage(t))

	if resp := n.handleAppendEntries(internal.AppendEntriesRequest{
		Term: 2, LeaderID: "l", Entries: ents(2, 1, 10), LeaderCommit: 10,
	}); !resp.Success {
		t.Fatalf("AppendEntries rejected: %+v", resp)
	}

	resp := n.handleAppendEntries(internal.AppendEntriesRequest{Term: 2, LeaderID: "l", Entries: ents(2, 1, 3)})
	if !resp.Success || resp.MatchIndex != 3 {
		t.Fatalf("duplicate response = %+v, want success with match index 3", resp)
	}

	assertLog(t, n, 10)

	if n.commitIndex != 10 {
		t.Fatalf("commitIndex = %d, want 10", n.commitIndex)
	}

	voteReady(n, "c")

	vote, err := n.RequestVote(context.Background(), &internal.RequestVoteRequest{
		Term: 3, CandidateID: "c", LastLogIndex: 5, LastLogTerm: 2,
	})
	if err != nil {
		t.Fatal(err)
	}

	if vote.VoteGranted {
		t.Fatal("granted a vote to a candidate missing committed entries 6..10")
	}
}

// A conflicting suffix is truncated and replaced, and commitIndex only moves
// up to the last entry the request carried, never to a stale tail.
func TestCommitIndexBoundedByLastNewEntry(t *testing.T) {
	n := newTestNode(t, "f", newMemStorage(t))

	n.handleAppendEntries(internal.AppendEntriesRequest{Term: 1, LeaderID: "old1", Entries: ents(1, 1, 3)})
	n.handleAppendEntries(internal.AppendEntriesRequest{
		Term: 2, LeaderID: "old2", PrevLogIndex: 3, PrevLogTerm: 1, Entries: ents(2, 4, 10),
	})

	// The new leader's first batch only covers 1..3, which this node has.
	resp := n.handleAppendEntries(internal.AppendEntriesRequest{
		Term: 3, LeaderID: "new", PrevLogIndex: 0, Entries: ents(1, 1, 3), LeaderCommit: 20,
	})
	if !resp.Success {
		t.Fatalf("AppendEntries rejected: %+v", resp)
	}

	if n.commitIndex != 3 {
		t.Fatalf("commitIndex = %d, want 3 (entries 4..10 are an uncommitted term 2 tail)", n.commitIndex)
	}

	// The next batch conflicts at 4 and replaces the stale tail.
	resp = n.handleAppendEntries(internal.AppendEntriesRequest{
		Term: 3, LeaderID: "new", PrevLogIndex: 3, PrevLogTerm: 1, Entries: ents(3, 4, 6), LeaderCommit: 20,
	})
	if !resp.Success {
		t.Fatalf("AppendEntries rejected: %+v", resp)
	}

	assertLog(t, n, 6)

	for i := uint64(4); i <= 6; i++ {
		if term, _ := n.log.Term(i); term != 3 {
			t.Fatalf("entry %d has term %d, want 3", i, term)
		}
	}

	if n.commitIndex != 6 {
		t.Fatalf("commitIndex = %d, want 6", n.commitIndex)
	}
}

// Entries that do not continue from prevLogIndex are rejected.
func TestAppendEntriesRejectsNonContiguousEntries(t *testing.T) {
	n := newTestNode(t, "f", newMemStorage(t))

	resp := n.handleAppendEntries(internal.AppendEntriesRequest{
		Term: 1, LeaderID: "l", Entries: []internal.LogEntry{{Index: 1, Term: 1}, {Index: 3, Term: 1}},
	})
	if resp.Success || n.log.LastIndex() != 0 {
		t.Fatalf("accepted non-contiguous entries: resp=%+v last=%d", resp, n.log.LastIndex())
	}
}

// A restarted node reloads its log, term and vote from storage, and entries
// removed by truncation stay removed.
func TestRestartReloadsLogAndState(t *testing.T) {
	st := newMemStorage(t)
	n := newTestNode(t, "f", st)

	n.handleAppendEntries(internal.AppendEntriesRequest{Term: 2, LeaderID: "l", Entries: ents(2, 1, 7)})
	// A new leader replaces 6..7.
	n.handleAppendEntries(internal.AppendEntriesRequest{
		Term: 3, LeaderID: "l2", PrevLogIndex: 5, PrevLogTerm: 2, Entries: ents(3, 6, 6), LeaderCommit: 6,
	})

	voteReady(n, "c", "other")

	if _, err := n.RequestVote(context.Background(), &internal.RequestVoteRequest{
		Term: 4, CandidateID: "c", LastLogIndex: 6, LastLogTerm: 3,
	}); err != nil {
		t.Fatal(err)
	}

	n2 := newTestNode(t, "f", st) // same storage, new process
	voteReady(n2, "c", "other")

	if n2.log.LastIndex() != 6 || n2.log.LastTerm() != 3 {
		t.Fatalf("after restart LastIndex=%d LastTerm=%d, want 6 and 3", n2.log.LastIndex(), n2.log.LastTerm())
	}

	if _, err := n2.log.Get(7); err == nil {
		t.Fatal("truncated entry 7 came back after restart")
	}

	if n2.currentTerm != 4 || n2.votedFor != "c" {
		t.Fatalf("after restart term=%d votedFor=%q, want 4 and c", n2.currentTerm, n2.votedFor)
	}

	// A different candidate in the same term must be refused.
	vote, _ := n2.RequestVote(context.Background(), &internal.RequestVoteRequest{
		Term: 4, CandidateID: "other", LastLogIndex: 100, LastLogTerm: 4,
	})
	if vote.VoteGranted {
		t.Fatal("restarted node voted twice in term 4")
	}

	resp := n2.handleAppendEntries(internal.AppendEntriesRequest{Term: 4, LeaderID: "c", PrevLogIndex: 6, PrevLogTerm: 3})
	if !resp.Success {
		t.Fatalf("heartbeat with prev=6 rejected after restart: %+v", resp)
	}
}

// flakyStorage fails writes on demand.
type flakyStorage struct {
	*storage.MemoryStorage

	failWrites atomic.Bool
}

var errInjected = errors.New("injected storage failure")

func (f *flakyStorage) Set(key, value []byte) error {
	if f.failWrites.Load() {
		return errInjected
	}

	return f.MemoryStorage.Set(key, value)
}

func (f *flakyStorage) Batch(ops []internal.BatchOp) error {
	if f.failWrites.Load() {
		return errInjected
	}

	return f.MemoryStorage.Batch(ops)
}

// A follower must not acknowledge entries it failed to persist.
func TestAppendEntriesPersistFailureIsRejected(t *testing.T) {
	st := &flakyStorage{MemoryStorage: newMemStorage(t)}
	n := newTestNode(t, "f", st)

	st.failWrites.Store(true)

	resp := n.handleAppendEntries(internal.AppendEntriesRequest{Term: 1, LeaderID: "l", Entries: ents(1, 1, 3), LeaderCommit: 3})
	if resp.Success {
		t.Fatal("acknowledged entries whose storage write failed")
	}

	if n.log.LastIndex() != 0 || n.commitIndex != 0 {
		t.Fatalf("failed write left LastIndex=%d commitIndex=%d", n.log.LastIndex(), n.commitIndex)
	}

	st.failWrites.Store(false)

	if resp := n.handleAppendEntries(internal.AppendEntriesRequest{Term: 1, LeaderID: "l", Entries: ents(1, 1, 3)}); !resp.Success {
		t.Fatalf("retry after storage recovered rejected: %+v", resp)
	}
}

// A vote that cannot be persisted must not be granted.
func TestVoteNotGrantedWhenPersistFails(t *testing.T) {
	st := &flakyStorage{MemoryStorage: newMemStorage(t)}
	n := newTestNode(t, "f", st)

	voteReady(n, "c")
	st.failWrites.Store(true)

	vote, _ := n.RequestVote(context.Background(), &internal.RequestVoteRequest{Term: 1, CandidateID: "c"})
	if vote.VoteGranted {
		t.Fatal("granted a vote that was not persisted")
	}
}

// No candidate gets a vote just because of its name.
func TestNoCandidateIsFavoured(t *testing.T) {
	n := newTestNode(t, "f", newMemStorage(t))
	n.handleAppendEntries(internal.AppendEntriesRequest{Term: 2, LeaderID: "l", Entries: ents(2, 1, 5)})
	voteReady(n, "node-1")

	vote, _ := n.RequestVote(context.Background(), &internal.RequestVoteRequest{
		Term: 3, CandidateID: "node-1", LastLogIndex: 1, LastLogTerm: 1,
	})
	if vote.VoteGranted {
		t.Fatal("node-1 was granted a vote with an out of date log")
	}
}
