package lb

import (
	"context"
	"testing"
)

func TestConsistentHashBalancer_SelectNode(t *testing.T) {
	store := NewInMemoryNodeStore()
	balancer := NewConsistentHashBalancer(150, store)
	ctx := context.Background()

	// Register nodes
	nodes := []*NodeInfo{
		{ID: "node1", Address: "10.0.1.1", Port: 8080, Healthy: true},
		{ID: "node2", Address: "10.0.1.2", Port: 8080, Healthy: true},
		{ID: "node3", Address: "10.0.1.3", Port: 8080, Healthy: true},
	}

	for _, node := range nodes {
		if err := balancer.RegisterNode(ctx, node); err != nil {
			t.Fatalf("RegisterNode() error = %v", err)
		}
	}

	// Test consistent hashing - same userID should get same node
	userID := "user123"

	node1, err := balancer.SelectNode(ctx, userID, nil)
	if err != nil {
		t.Fatalf("SelectNode() error = %v", err)
	}

	node2, err := balancer.SelectNode(ctx, userID, nil)
	if err != nil {
		t.Fatalf("SelectNode() error = %v", err)
	}

	if node1.ID != node2.ID {
		t.Errorf("same userID should get same node, got %s and %s", node1.ID, node2.ID)
	}
}

func TestConsistentHashBalancer_NodeFailover(t *testing.T) {
	store := NewInMemoryNodeStore()
	balancer := NewConsistentHashBalancer(150, store)
	ctx := context.Background()

	// Register nodes
	nodes := []*NodeInfo{
		{ID: "node1", Address: "10.0.1.1", Port: 8080, Healthy: true},
		{ID: "node2", Address: "10.0.1.2", Port: 8080, Healthy: true},
	}

	for _, node := range nodes {
		balancer.RegisterNode(ctx, node)
	}

	// Select node for user
	userID := "user123"
	originalNode, _ := balancer.SelectNode(ctx, userID, nil)

	// Mark node as unhealthy
	originalNode.Healthy = false

	// Should failover to another node
	newNode, err := balancer.SelectNode(ctx, userID, nil)
	if err != nil {
		t.Fatalf("SelectNode() error = %v", err)
	}

	if newNode.ID == originalNode.ID {
		t.Error("should failover to different node when original is unhealthy")
	}

	if !newNode.Healthy {
		t.Error("selected node should be healthy")
	}
}

func TestConsistentHashBalancer_Distribution(t *testing.T) {
	store := NewInMemoryNodeStore()
	balancer := NewConsistentHashBalancer(150, store)
	ctx := context.Background()

	// Register nodes
	nodes := []*NodeInfo{
		{ID: "node1", Address: "10.0.1.1", Port: 8080, Healthy: true},
		{ID: "node2", Address: "10.0.1.2", Port: 8080, Healthy: true},
		{ID: "node3", Address: "10.0.1.3", Port: 8080, Healthy: true},
	}

	for _, node := range nodes {
		balancer.RegisterNode(ctx, node)
	}

	// Test distribution across nodes
	distribution := make(map[string]int)
	numUsers := 1000

	for i := range numUsers {
		userID := string(rune(i))

		node, err := balancer.SelectNode(ctx, userID, nil)
		if err != nil {
			t.Fatalf("SelectNode() error = %v", err)
		}

		distribution[node.ID]++
	}

	// Check that distribution is reasonably balanced
	// With 3 nodes, each should get ~333 users (±20%)
	expectedPerNode := numUsers / len(nodes)
	tolerance := float64(expectedPerNode) * 0.4 // 40% tolerance

	for nodeID, count := range distribution {
		diff := float64(abs(count - expectedPerNode))
		if diff > tolerance {
			t.Errorf("unbalanced distribution for %s: got %d, expected ~%d", nodeID, count, expectedPerNode)
		}
	}
}

func abs(x int) int {
	if x < 0 {
		return -x
	}

	return x
}

func BenchmarkConsistentHashBalancer_SelectNode(b *testing.B) {
	store := NewInMemoryNodeStore()
	balancer := NewConsistentHashBalancer(150, store)
	ctx := context.Background()

	// Register 10 nodes
	for i := range 10 {
		node := &NodeInfo{
			ID:      string(rune('a' + i)),
			Address: "10.0.1.1",
			Port:    8080 + i,
			Healthy: true,
		}
		balancer.RegisterNode(ctx, node)
	}

	for i := 0; b.Loop(); i++ {
		userID := string(rune(i % 1000))
		_, _ = balancer.SelectNode(ctx, userID, nil)
	}
}
