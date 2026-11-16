package raft

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"reflect"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/consensus/internal"
	"github.com/xraph/forge/extensions/consensus/statemachine"
	"github.com/xraph/forge/extensions/consensus/storage"
	"github.com/xraph/forge/extensions/consensus/transport"
)

// mcMember is a TCP cluster member that can be stopped, restarted on the
// same port and storage, or started in join mode.
type mcMember struct {
	id      string
	port    int
	store   *storage.MemoryStorage
	node    *Node
	tr      *transport.TCPTransport
	sm      *statemachine.MemoryStateMachine
	running bool
}

type memberCluster struct {
	t       *testing.T
	mu      sync.Mutex
	members map[string]*mcMember
}

func newMemberCluster(t *testing.T, size int) *memberCluster {
	t.Helper()

	c := &memberCluster{t: t, members: map[string]*mcMember{}}

	var initial []*mcMember
	for i := range size {
		initial = append(initial, c.add(fmt.Sprintf("n%d", i+1)))
	}

	for _, m := range initial {
		c.open(m, false)
	}

	for _, m := range initial {
		for _, other := range initial {
			if other != m {
				m.node.AddPeerAddress(other.id, "127.0.0.1", other.port)
			}
		}

		c.run(m)
	}

	t.Cleanup(func() {
		for _, m := range c.all() {
			c.stop(m)
		}
	})

	return c
}

func (c *memberCluster) add(id string) *mcMember {
	m := &mcMember{id: id, store: newMemStorage(c.t)}

	c.mu.Lock()
	c.members[id] = m
	c.mu.Unlock()

	return m
}

func (c *memberCluster) all() []*mcMember {
	c.mu.Lock()
	defer c.mu.Unlock()

	out := make([]*mcMember, 0, len(c.members))
	for _, m := range c.members {
		out = append(out, m)
	}

	sort.Slice(out, func(i, j int) bool { return out[i].id < out[j].id })

	return out
}

// open creates the member's transport and node without starting the node.
func (c *memberCluster) open(m *mcMember, join bool) {
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
		Address:            "127.0.0.1",
		Port:               m.port,
		Join:               join,
		HeartbeatInterval:  50 * time.Millisecond,
		ElectionTimeoutMin: 300 * time.Millisecond,
		ElectionTimeoutMax: 600 * time.Millisecond,
	}, lg, m.sm, m.tr, m.store)
	if err != nil {
		c.t.Fatal(err)
	}

	m.node = node
}

// run gives the transport a route to every other running member and starts
// the node. Membership itself comes from the bootstrap peers or the log.
func (c *memberCluster) run(m *mcMember) {
	c.t.Helper()

	for _, other := range c.all() {
		if other != m && other.port != 0 {
			_ = m.tr.AddPeer(other.id, "127.0.0.1", other.port)
		}
	}

	if err := m.node.Start(context.Background()); err != nil {
		c.t.Fatal(err)
	}

	m.running = true
}

// join starts a new server in join mode, ready to be added by the leader.
func (c *memberCluster) join(id string) *mcMember {
	m := c.add(id)
	c.open(m, true)
	c.run(m)

	return m
}

// restart stops a member and starts it again from its storage.
func (c *memberCluster) restart(m *mcMember) {
	c.stop(m)
	c.open(m, false)

	for _, other := range c.all() {
		if other != m {
			m.node.AddPeerAddress(other.id, "127.0.0.1", other.port)
		}
	}

	c.run(m)
}

func (c *memberCluster) stop(m *mcMember) {
	if !m.running {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_ = m.node.Stop(ctx)
	_ = m.tr.Stop(ctx)
	m.running = false
}

func (c *memberCluster) leader() *mcMember {
	c.t.Helper()

	deadline := time.Now().Add(15 * time.Second)

	for time.Now().Before(deadline) {
		var leaders []*mcMember

		for _, m := range c.all() {
			if m.running && m.node.IsLeader() {
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

func voters(cfg internal.ClusterConfiguration) []string {
	v := cfg.Voters()
	sort.Strings(v)

	return v
}

// waitVoters waits until every listed running member sees exactly want as
// its voters.
func (c *memberCluster) waitVoters(want []string, members ...*mcMember) {
	c.t.Helper()

	deadline := time.Now().Add(15 * time.Second)

	for _, m := range members {
		for !reflect.DeepEqual(voters(m.node.Configuration()), want) {
			if time.Now().After(deadline) {
				c.t.Fatalf("%s voters %v, want %v", m.id, voters(m.node.Configuration()), want)
			}

			time.Sleep(20 * time.Millisecond)
		}
	}
}

// proposer keeps proposing to whoever leads until stopped.
type proposer struct {
	stop    chan struct{}
	done    chan struct{}
	applied atomic.Int64
}

func (c *memberCluster) startProposer() *proposer {
	p := &proposer{stop: make(chan struct{}), done: make(chan struct{})}

	go func() {
		defer close(p.done)

		for i := 0; ; i++ {
			select {
			case <-p.stop:
				return
			default:
			}

			for _, m := range c.all() {
				if !m.running || !m.node.IsLeader() {
					continue
				}

				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				cmd := fmt.Sprintf(`{"type":"set","payload":{"key":"bg%d","value":"v"}}`, i)

				if m.node.Propose(ctx, []byte(cmd)) == nil {
					p.applied.Add(1)
				}

				cancel()
			}

			time.Sleep(10 * time.Millisecond)
		}
	}()

	return p
}

func (p *proposer) halt() {
	close(p.stop)
	<-p.done
}

// waitSameState waits until every listed member has applied the leader's
// commit index and holds the same state.
func (c *memberCluster) waitSameState(leader *mcMember, members ...*mcMember) {
	c.t.Helper()

	commit := leader.node.GetCommitIndex()
	for _, m := range members {
		waitApplied(c.t, m.node, commit)
	}

	want := leader.sm.GetState()
	for _, m := range members {
		if got := m.sm.GetState(); !reflect.DeepEqual(got, want) {
			c.t.Fatalf("%s has %d keys, leader %s has %d", m.id, len(got), leader.id, len(want))
		}
	}
}

// A three node cluster grows to five and shrinks back to three while a
// client keeps writing, and every member ends with the same state.
func TestMembershipGrowAndShrinkWhileReplicating(t *testing.T) {
	c := newMemberCluster(t, 3)
	c.leader()

	p := c.startProposer()

	n4 := c.join("n4")
	n5 := c.join("n5")

	for _, m := range []*mcMember{n4, n5} {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		err := c.leader().node.AddNode(ctx, m.id, "127.0.0.1", m.port)

		cancel()

		if err != nil {
			t.Fatalf("add %s: %v", m.id, err)
		}
	}

	all := c.all()
	c.waitVoters([]string{"n1", "n2", "n3", "n4", "n5"}, all...)

	for _, id := range []string{"n5", "n4"} {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		err := c.leader().node.RemoveNode(ctx, id)

		cancel()

		if err != nil {
			t.Fatalf("remove %s: %v", id, err)
		}
	}

	p.halt()

	if p.applied.Load() == 0 {
		t.Fatal("no client write succeeded during the membership changes")
	}

	core := []*mcMember{c.members["n1"], c.members["n2"], c.members["n3"]}
	c.waitVoters([]string{"n1", "n2", "n3"}, core...)

	c.stop(n4)
	c.stop(n5)

	leader := c.leader()
	propose(t, leader.node, "after", "shrink")
	c.waitSameState(leader, core...)
}

// A second change is refused while the first has not finished.
func TestMembershipOneChangeAtATime(t *testing.T) {
	c := newMemberCluster(t, 3)
	leader := c.leader()

	// Nothing listens for "ghost", so it never catches up.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	first := make(chan error, 1)

	go func() { first <- leader.node.AddNode(ctx, "ghost", "127.0.0.1", 1) }()

	deadline := time.Now().Add(5 * time.Second)

	for {
		if _, ok := leader.node.Configuration().Member("ghost"); ok {
			break
		}

		if time.Now().After(deadline) {
			t.Fatal("ghost was never added as a learner")
		}

		time.Sleep(5 * time.Millisecond)
	}

	if err := leader.node.RemoveNode(context.Background(), "n3"); !errors.Is(err, ErrConfigChangeInProgress) {
		t.Fatalf("concurrent RemoveNode returned %v, want ErrConfigChangeInProgress", err)
	}

	if err := <-first; err == nil {
		t.Fatal("adding a server that never caught up succeeded")
	}

	// The learner was rolled back and it never voted.
	if _, ok := leader.node.Configuration().Member("ghost"); ok {
		t.Fatal("the learner that never caught up is still a member")
	}

	c.waitVoters([]string{"n1", "n2", "n3"}, c.all()...)
}

// Removing the leader commits without it, after which it steps down, the
// others elect a new leader, and the old one never disrupts them.
func TestMembershipRemoveLeader(t *testing.T) {
	c := newMemberCluster(t, 3)
	old := c.leader()
	propose(t, old.node, "before", "removal")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := old.node.RemoveNode(ctx, old.id); err != nil {
		t.Fatalf("remove leader: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for old.node.IsLeader() {
		if time.Now().After(deadline) {
			t.Fatal("removed leader did not step down")
		}

		time.Sleep(10 * time.Millisecond)
	}

	var rest []*mcMember

	for _, m := range c.all() {
		if m != old {
			rest = append(rest, m)
		}
	}

	c.waitVoters(voters(rest[0].node.Configuration()), rest...)

	if voters := rest[0].node.Configuration().Voters(); len(voters) != 2 {
		t.Fatalf("voters after removal: %v", voters)
	}

	leader := c.leader()
	if leader == old {
		t.Fatal("the removed server leads again")
	}

	propose(t, leader.node, "after", "removal")
	c.waitSameState(leader, rest...)

	term := leader.node.GetTerm()

	time.Sleep(1500 * time.Millisecond)

	if leader.node.GetTerm() != term || !leader.node.IsLeader() {
		t.Fatal("the removed server disrupted the new leader")
	}
}

// A server removed while it was down never learns of its removal. When it
// comes back it times out and campaigns, but members ignore it, so the
// leader and term stay put.
func TestRemovedServerCannotDisrupt(t *testing.T) {
	c := newMemberCluster(t, 3)
	leader := c.leader()

	var victim *mcMember

	for _, m := range c.all() {
		if m != leader {
			victim = m

			break
		}
	}

	c.stop(victim)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := leader.node.RemoveNode(ctx, victim.id); err != nil {
		t.Fatalf("remove %s: %v", victim.id, err)
	}

	term := leader.node.GetTerm()

	c.restart(victim)

	// The victim still believes it is a voter and keeps raising its term.
	deadline := time.Now().Add(3 * time.Second)
	for victim.node.GetTerm() <= term+1 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}

	if victim.node.GetTerm() <= term {
		t.Fatal("the removed server never campaigned; the test proves nothing")
	}

	if !leader.node.IsLeader() || leader.node.GetTerm() != term {
		t.Fatalf("leader term moved from %d to %d (leader=%v)", term, leader.node.GetTerm(), leader.node.IsLeader())
	}

	propose(t, leader.node, "still", "leading")
}

// Membership survives a restart and reaches a server that was down while the
// log was compacted, through InstallSnapshot.
func TestMembershipSurvivesRestartAndSnapshot(t *testing.T) {
	c := newMemberCluster(t, 3)
	leader := c.leader()

	var offline *mcMember

	for _, m := range c.all() {
		if m != leader {
			offline = m

			break
		}
	}

	c.stop(offline)

	n4 := c.join("n4")

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := leader.node.AddNode(ctx, "n4", "127.0.0.1", n4.port); err != nil {
		t.Fatalf("add n4: %v", err)
	}

	for i := range 10 {
		propose(t, leader.node, fmt.Sprintf("k%d", i), "v")
	}

	want := []string{"n1", "n2", "n3", "n4"}

	// Compact every running member, so the configuration entries only
	// survive in snapshots.
	for _, m := range c.all() {
		if !m.running {
			continue
		}

		waitApplied(t, m.node, leader.node.GetCommitIndex())

		if err := m.node.Snapshot(); err != nil {
			t.Fatalf("%s snapshot: %v", m.id, err)
		}

		if len(m.node.log.ConfigEntries()) != 0 {
			t.Fatalf("%s still holds configuration entries after compaction", m.id)
		}
	}

	// A restart rebuilds the configuration from the snapshot.
	c.restart(n4)

	if got := voters(n4.node.Configuration()); !reflect.DeepEqual(got, want) {
		t.Fatalf("n4 voters after restart %v, want %v", got, want)
	}

	// The server that missed the change gets it through InstallSnapshot.
	c.restart(offline)
	c.waitVoters(want, offline)

	leader = c.leader()
	propose(t, leader.node, "final", "write")
	c.waitSameState(leader, c.all()...)
}

// Leadership moves to the chosen server, and a transfer to a server that is
// down is abandoned after an election timeout with the old leader intact.
func TestLeadershipTransfer(t *testing.T) {
	c := newMemberCluster(t, 3)
	leader := c.leader()

	var target, down *mcMember

	for _, m := range c.all() {
		switch {
		case m == leader:
		case target == nil:
			target = m
		default:
			down = m
		}
	}

	propose(t, leader.node, "before", "transfer")

	if err := leader.node.TransferLeadership(context.Background(), target.id); err != nil {
		t.Fatalf("transfer to %s: %v", target.id, err)
	}

	if got := c.leader(); got != target {
		t.Fatalf("leader is %s, want %s", got.id, target.id)
	}

	propose(t, target.node, "after", "transfer")

	c.stop(down)

	start := time.Now()

	err := target.node.TransferLeadership(context.Background(), down.id)
	if !errors.Is(err, ErrLeadershipTransferFailed) {
		t.Fatalf("transfer to a stopped server returned %v", err)
	}

	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("abandoning the transfer took %v", elapsed)
	}

	if !target.node.IsLeader() {
		t.Fatal("the leader lost leadership after an abandoned transfer")
	}

	propose(t, target.node, "after", "abort")
}

// Client writes are refused while a transfer is under way.
func TestTransferBlocksClientWrites(t *testing.T) {
	n, _ := newLeader(t)

	n.mu.Lock()
	n.transferTarget = "p"
	n.mu.Unlock()

	if err := n.Propose(context.Background(), []byte("{}")); !errors.Is(err, ErrLeadershipTransferInProgress) {
		t.Fatalf("Propose during transfer returned %v", err)
	}

	if err := n.RemoveNode(context.Background(), "p"); !errors.Is(err, ErrLeadershipTransferInProgress) {
		t.Fatalf("RemoveNode during transfer returned %v", err)
	}
}

// A follower that appended a configuration entry and then has it truncated
// by a new leader returns to the configuration before it.
func TestTruncatedConfigurationIsReverted(t *testing.T) {
	n := newTestNode(t, "f", newMemStorage(t))
	n.AddPeer("l")
	n.AddPeer("l2")

	base := voters(n.Configuration())

	grown := n.Configuration()
	grown.Members = append(grown.Members, internal.ClusterMember{ID: "x", Voter: true})

	data := mustJSON(t, grown)
	n.handleAppendEntries(internal.AppendEntriesRequest{
		Term: 1, LeaderID: "l",
		Entries: []internal.LogEntry{{Index: 1, Term: 1, Type: internal.EntryConfig, Data: data}},
	})

	if !n.Configuration().IsVoter("x") {
		t.Fatal("configuration did not take effect on append")
	}

	// A new leader overwrites index 1.
	n.handleAppendEntries(internal.AppendEntriesRequest{Term: 2, LeaderID: "l2", Entries: ents(2, 1, 1)})

	if got := voters(n.Configuration()); !reflect.DeepEqual(got, base) {
		t.Fatalf("voters after truncation %v, want %v", got, base)
	}
}

func mustJSON(t *testing.T, cfg internal.ClusterConfiguration) []byte {
	t.Helper()

	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}

	return data
}
