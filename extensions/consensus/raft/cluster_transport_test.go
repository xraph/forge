package raft

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/consensus/internal"
	"github.com/xraph/forge/extensions/consensus/statemachine"
	"github.com/xraph/forge/extensions/consensus/transport"
)

type clusterMember struct {
	node *Node
	tr   internal.Transport
	sm   *statemachine.MemoryStateMachine
}

func startNetworkCluster(t *testing.T, kind string, size int) []*clusterMember {
	t.Helper()

	lg := forge.NewNoopLogger()
	ctx := context.Background()
	members := make([]*clusterMember, size)
	ports := make([]int, size)

	for i := range size {
		id := fmt.Sprintf("n%d", i+1)

		var tr internal.Transport

		switch kind {
		case "tcp":
			tr = transport.NewTCPTransport(transport.TCPTransportConfig{NodeID: id, Address: "127.0.0.1"}, lg)
		case "http":
			tr = transport.NewHTTPTransport(transport.HTTPTransportConfig{NodeID: id, Address: "127.0.0.1"}, lg)
		case "grpc":
			tr = transport.NewGRPCTransport(transport.GRPCTransportConfig{NodeID: id, Address: "127.0.0.1"}, lg)
		}

		if err := tr.Start(ctx); err != nil {
			t.Fatal(err)
		}

		_, p, _ := net.SplitHostPort(tr.GetAddress())
		ports[i], _ = strconv.Atoi(p)

		sm := statemachine.NewMemoryStateMachine(statemachine.MemoryStateMachineConfig{}, lg)

		node, err := NewNode(Config{
			NodeID:             id,
			HeartbeatInterval:  50 * time.Millisecond,
			ElectionTimeoutMin: 300 * time.Millisecond,
			ElectionTimeoutMax: 600 * time.Millisecond,
		}, lg, sm, tr, newMemStorage(t))
		if err != nil {
			t.Fatal(err)
		}

		members[i] = &clusterMember{node: node, tr: tr, sm: sm}
	}

	for i, m := range members {
		for j := range members {
			if i == j {
				continue
			}

			peerID := fmt.Sprintf("n%d", j+1)
			if err := m.tr.AddPeer(peerID, "127.0.0.1", ports[j]); err != nil {
				t.Fatal(err)
			}

			m.node.AddPeer(peerID)
		}
	}

	for _, m := range members {
		if err := m.node.Start(ctx); err != nil {
			t.Fatal(err)
		}
	}

	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		for _, m := range members {
			_ = m.node.Stop(stopCtx)
			_ = m.tr.Stop(stopCtx)
		}
	})

	return members
}

func waitForLeader(t *testing.T, members []*clusterMember) *clusterMember {
	t.Helper()

	deadline := time.Now().Add(15 * time.Second)

	for time.Now().Before(deadline) {
		var leaders []*clusterMember

		for _, m := range members {
			if m.node.IsLeader() {
				leaders = append(leaders, m)
			}
		}

		if len(leaders) == 1 {
			return leaders[0]
		}

		time.Sleep(20 * time.Millisecond)
	}

	t.Fatal("no single leader elected")

	return nil
}

func testClusterOverTransport(t *testing.T, kind string) {
	members := startNetworkCluster(t, kind, 3)
	leader := waitForLeader(t, members)

	for i := range 5 {
		cmd := fmt.Sprintf(`{"type":"set","payload":{"key":"k%d","value":"v%d"}}`, i, i)

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		err := leader.node.Propose(ctx, []byte(cmd))

		cancel()

		if err != nil {
			t.Fatalf("propose %d on %s: %v", i, leader.node.id, err)
		}
	}

	want := leader.node.GetCommitIndex()
	deadline := time.Now().Add(10 * time.Second)

	for _, m := range members {
		for {
			m.node.mu.RLock()
			applied := m.node.lastApplied
			m.node.mu.RUnlock()

			if applied >= want {
				break
			}

			if time.Now().After(deadline) {
				t.Fatalf("%s applied up to %d, leader committed %d", m.node.id, applied, want)
			}

			time.Sleep(20 * time.Millisecond)
		}

		state := m.sm.GetState()
		for i := range 5 {
			if got := state[fmt.Sprintf("k%d", i)]; got != fmt.Sprintf("v%d", i) {
				t.Fatalf("%s has k%d=%v", m.node.id, i, got)
			}
		}
	}
}

// A three node cluster elects one leader and replicates commands to every
// node over the HTTP transport.
func TestClusterElectsAndReplicatesOverHTTP(t *testing.T) {
	testClusterOverTransport(t, "http")
}

// The same over the TCP transport.
func TestClusterElectsAndReplicatesOverTCP(t *testing.T) {
	testClusterOverTransport(t, "tcp")
}

// The same over the gRPC transport.
func TestClusterElectsAndReplicatesOverGRPC(t *testing.T) {
	testClusterOverTransport(t, "grpc")
}
