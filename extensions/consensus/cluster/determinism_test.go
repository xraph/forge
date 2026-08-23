package cluster

import (
	"fmt"
	"slices"
	"testing"

	"github.com/xraph/forge"
)

// Manager.GetNodes backs the cluster view that the admin API serves and that
// the rebalancer reads. It walks a map, and Go randomises map iteration, so
// identical cluster state produced a different order on every call.
//
// Twelve nodes rather than two or three: a map small enough for one bucket
// (<= 8 entries) only rotates its iteration order.
const (
	determinismRuns = 64
	nodeCount       = 12
)

func seedManager(t *testing.T) *Manager {
	t.Helper()

	m := NewManager(ManagerConfig{NodeID: "node-00"}, forge.NewNoopLogger())

	// Added in an order that is neither sorted nor reverse sorted, so a
	// manager echoing insertion order would still fail.
	for i := range nodeCount {
		id := fmt.Sprintf("node-%02d", (i*7)%nodeCount)
		if err := m.AddNode(id, "127.0.0.1", 9000+i); err != nil {
			t.Fatalf("AddNode(%s): %v", id, err)
		}
	}

	return m
}

func TestManagerGetNodesIsDeterministic(t *testing.T) {
	m := seedManager(t)

	read := func() []string {
		nodes := m.GetNodes()

		ids := make([]string, 0, len(nodes))
		for _, n := range nodes {
			ids = append(ids, n.ID)
		}

		return ids
	}

	want := read()
	if len(want) != nodeCount {
		t.Fatalf("got %d nodes, want %d", len(want), nodeCount)
	}

	if !slices.IsSorted(want) {
		t.Errorf("GetNodes is not sorted: %v", want)
	}

	for run := range determinismRuns {
		if got := read(); !slices.Equal(got, want) {
			t.Fatalf("run %d: GetNodes order is not stable\n got: %v\nwant: %v", run, got, want)
		}
	}
}

// GetNodesByMatchIndex decides which nodes look most caught up. Nodes level
// with each other must not swap places between two calls over the same input.
func TestGetNodesByMatchIndexTieBreak(t *testing.T) {
	qm := &QuorumManager{}

	// Every node on the same match index, so the whole result is one big tie.
	matchIndexes := make(map[string]uint64, nodeCount)
	for i := range nodeCount {
		matchIndexes[fmt.Sprintf("node-%02d", (i*7)%nodeCount)] = 100
	}

	read := func() []string {
		rows := qm.GetNodesByMatchIndex(matchIndexes)

		ids := make([]string, 0, len(rows))
		for _, r := range rows {
			ids = append(ids, r.NodeID)
		}

		return ids
	}

	want := read()
	if !slices.IsSorted(want) {
		t.Errorf("tied nodes are not ordered by ID: %v", want)
	}

	for run := range determinismRuns {
		if got := read(); !slices.Equal(got, want) {
			t.Fatalf("run %d: tie order is not stable\n got: %v\nwant: %v", run, got, want)
		}
	}
}
