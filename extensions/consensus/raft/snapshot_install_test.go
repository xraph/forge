package raft

import (
	"context"
	"fmt"
	"net"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/consensus/internal"
	"github.com/xraph/forge/extensions/consensus/statemachine"
	"github.com/xraph/forge/extensions/consensus/storage"
	"github.com/xraph/forge/extensions/consensus/transport"
)

// restartableMember is a TCP cluster member that can be stopped and started
// again on the same port and storage, like a process restart.
type restartableMember struct {
	id    string
	port  int
	store *storage.MemoryStorage
	node  *Node
	tr    *transport.TCPTransport
	sm    *statemachine.MemoryStateMachine
}

type restartableCluster struct {
	t       *testing.T
	members []*restartableMember
}

func (c *restartableCluster) start(m *restartableMember) {
	c.t.Helper()

	lg := forge.NewNoopLogger()
	m.tr = transport.NewTCPTransport(transport.TCPTransportConfig{NodeID: m.id, Address: "127.0.0.1", Port: m.port}, lg)

	if err := m.tr.Start(context.Background()); err != nil {
		c.t.Fatal(err)
	}

	_, p, _ := net.SplitHostPort(m.tr.GetAddress())
	m.port, _ = strconv.Atoi(p)

	m.sm = statemachine.NewMemoryStateMachine(statemachine.MemoryStateMachineConfig{}, lg)

	node, err := NewNode(Config{
		NodeID:             m.id,
		HeartbeatInterval:  50 * time.Millisecond,
		ElectionTimeoutMin: 300 * time.Millisecond,
		ElectionTimeoutMax: 600 * time.Millisecond,
		SnapshotChunkSize:  40, // force many chunks
	}, lg, m.sm, m.tr, m.store)
	if err != nil {
		c.t.Fatal(err)
	}

	m.node = node
}

func (c *restartableCluster) connect(m *restartableMember) {
	c.t.Helper()

	for _, other := range c.members {
		if other == m {
			continue
		}

		if err := m.tr.AddPeer(other.id, "127.0.0.1", other.port); err != nil {
			c.t.Fatal(err)
		}

		m.node.AddPeer(other.id)
	}

	if err := m.node.Start(context.Background()); err != nil {
		c.t.Fatal(err)
	}
}

func (c *restartableCluster) stop(m *restartableMember) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_ = m.node.Stop(ctx)
	_ = m.tr.Stop(ctx)
}

func (c *restartableCluster) leader() *restartableMember {
	c.t.Helper()

	deadline := time.Now().Add(15 * time.Second)

	for time.Now().Before(deadline) {
		var leaders []*restartableMember

		for _, m := range c.members {
			if m.node != nil && m.node.IsLeader() {
				leaders = append(leaders, m)
			}
		}

		if len(leaders) == 1 {
			return leaders[0]
		}

		time.Sleep(20 * time.Millisecond)
	}

	c.t.Fatal("no single leader elected")

	return nil
}

func propose(t *testing.T, n *Node, key, value string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := fmt.Sprintf(`{"type":"set","payload":{"key":%q,"value":%q}}`, key, value)
	if err := n.Propose(ctx, []byte(cmd)); err != nil {
		t.Fatalf("propose %s: %v", key, err)
	}
}

func waitApplied(t *testing.T, n *Node, index uint64) {
	t.Helper()

	deadline := time.Now().Add(15 * time.Second)

	for {
		n.mu.RLock()
		applied := n.lastApplied
		n.mu.RUnlock()

		if applied >= index {
			return
		}

		if time.Now().After(deadline) {
			t.Fatalf("%s applied up to %d, want %d", n.id, applied, index)
		}

		time.Sleep(20 * time.Millisecond)
	}
}

// A follower that was offline while the leader compacted its log catches up
// through InstallSnapshot, sent in many chunks, and ends with the same state
// machine as the rest of the cluster.
func TestOfflineFollowerCatchesUpViaSnapshot(t *testing.T) {
	c := &restartableCluster{t: t}

	for i := range 3 {
		c.members = append(c.members, &restartableMember{id: fmt.Sprintf("n%d", i+1), store: newMemStorage(t)})
	}

	for _, m := range c.members {
		c.start(m)
	}

	for _, m := range c.members {
		c.connect(m)
	}

	t.Cleanup(func() {
		for _, m := range c.members {
			c.stop(m)
		}
	})

	leader := c.leader()
	propose(t, leader.node, "before", "offline")

	var follower *restartableMember

	for _, m := range c.members {
		if m != leader {
			follower = m

			break
		}
	}

	waitApplied(t, follower.node, leader.node.GetCommitIndex())
	c.stop(follower)

	for i := range 20 {
		propose(t, leader.node, fmt.Sprintf("k%d", i), fmt.Sprintf("v%d", i))
	}

	// Compact every running node, so whichever of them leads once the
	// follower is back (its return can trigger an election) has to send a
	// snapshot.
	for _, m := range c.members {
		if m == follower {
			continue
		}

		waitApplied(t, m.node, leader.node.GetCommitIndex())

		if err := m.node.Snapshot(); err != nil {
			t.Fatalf("%s snapshot: %v", m.id, err)
		}

		if m.node.log.SnapshotIndex() == 0 {
			t.Fatalf("%s did not compact its log", m.id)
		}
	}

	// Entries after the snapshot reach the follower through AppendEntries.
	propose(t, leader.node, "after", "snapshot")

	c.start(follower)
	c.connect(follower)

	leader = c.leader()
	commit := leader.node.GetCommitIndex()
	waitApplied(t, follower.node, commit)

	if follower.node.log.SnapshotIndex() == 0 {
		t.Fatal("the follower caught up without installing a snapshot")
	}

	for _, m := range c.members {
		waitApplied(t, m.node, commit)
	}

	want := leader.sm.GetState()
	for _, m := range c.members {
		if got := m.sm.GetState(); !reflect.DeepEqual(got, want) {
			t.Fatalf("%s state %v differs from leader %v", m.id, got, want)
		}
	}

	if len(want) != 22 {
		t.Fatalf("leader state has %d keys, want 22", len(want))
	}

	// The installed snapshot survives a restart of the follower.
	c.stop(follower)
	c.start(follower)

	if follower.node.log.SnapshotIndex() == 0 || len(follower.sm.GetState()) == 0 {
		t.Fatal("follower lost its installed snapshot on restart")
	}
}

// Chunks that do not continue the snapshot being assembled are refused.
func TestSnapshotChunksMustArriveInOrder(t *testing.T) {
	n := newTestNode(t, "f", newMemStorage(t))

	resp := n.handleInstallSnapshot(internal.InstallSnapshotRequest{
		Term: 1, LeaderID: "l", LastIncludedIndex: 5, LastIncludedTerm: 1, Offset: 4, Data: []byte("{}"),
	})
	if resp.Success {
		t.Fatal("accepted a chunk at offset 4 with nothing before it")
	}

	first := n.handleInstallSnapshot(internal.InstallSnapshotRequest{
		Term: 1, LeaderID: "l", LastIncludedIndex: 5, LastIncludedTerm: 1, Data: []byte(`{"a":`),
	})
	gap := n.handleInstallSnapshot(internal.InstallSnapshotRequest{
		Term: 1, LeaderID: "l", LastIncludedIndex: 5, LastIncludedTerm: 1, Offset: 9, Data: []byte(`1}`), Done: true,
	})

	if !first.Success || gap.Success {
		t.Fatalf("first=%v gap=%v, want the gap refused", first.Success, gap.Success)
	}

	if n.log.SnapshotIndex() != 0 {
		t.Fatal("a refused snapshot was installed")
	}
}

// Installing a snapshot keeps the log suffix that agrees with it and drops
// everything when it does not.
func TestLogInstallSnapshotKeepsMatchingSuffix(t *testing.T) {
	st := newMemStorage(t)

	l, err := NewLog(forge.NewNoopLogger(), st, 0)
	if err != nil {
		t.Fatal(err)
	}

	_ = l.AppendBatch(ents(2, 1, 10))

	if err := l.InstallSnapshot(6, 2); err != nil {
		t.Fatal(err)
	}

	if l.SnapshotIndex() != 6 || l.LastIndex() != 10 || l.Count() != 4 {
		t.Fatalf("after matching install: %s", l)
	}

	if err := l.InstallSnapshot(8, 5); err != nil {
		t.Fatal(err)
	}

	if l.SnapshotIndex() != 8 || l.LastIndex() != 8 || l.Count() != 0 {
		t.Fatalf("after conflicting install: %s", l)
	}

	if _, err := st.Get(logKey(10)); err == nil {
		t.Fatal("discarded entry 10 is still in storage")
	}
}
