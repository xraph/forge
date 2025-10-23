package testing

import (
	"context"
	"testing"
	"time"
)

func TestClusterElection(t *testing.T) {
	harness := NewTestHarness(t)
	ctx := context.Background()

	// Create a 3-node cluster
	if err := harness.CreateCluster(ctx, 3); err != nil {
		t.Fatalf("failed to create cluster: %v", err)
	}

	// Start cluster
	if err := harness.StartCluster(ctx); err != nil {
		t.Fatalf("failed to start cluster: %v", err)
	}
	defer harness.StopCluster(ctx)

	// Wait for leader election
	leaderID, err := harness.WaitForLeader(5 * time.Second)
	if err != nil {
		t.Fatalf("leader election failed: %v", err)
	}

	t.Logf("Leader elected: %s", leaderID)

	// Verify leader exists
	harness.AssertLeaderExists()
}

func TestLeaderFailover(t *testing.T) {
	harness := NewTestHarness(t)
	ctx := context.Background()

	// Create a 3-node cluster
	if err := harness.CreateCluster(ctx, 3); err != nil {
		t.Fatalf("failed to create cluster: %v", err)
	}

	// Start cluster
	if err := harness.StartCluster(ctx); err != nil {
		t.Fatalf("failed to start cluster: %v", err)
	}
	defer harness.StopCluster(ctx)

	// Wait for initial leader
	leaderID, err := harness.WaitForLeader(5 * time.Second)
	if err != nil {
		t.Fatalf("initial leader election failed: %v", err)
	}

	t.Logf("Initial leader: %s", leaderID)

	// Stop the leader
	if err := harness.StopNode(ctx, leaderID); err != nil {
		t.Fatalf("failed to stop leader: %v", err)
	}

	t.Logf("Stopped leader: %s", leaderID)

	// Wait a bit for detection
	time.Sleep(1 * time.Second)

	// Wait for new leader
	newLeaderID, err := harness.WaitForLeader(10 * time.Second)
	if err != nil {
		t.Fatalf("failover failed: %v", err)
	}

	if newLeaderID == leaderID {
		t.Fatalf("expected different leader, got same: %s", newLeaderID)
	}

	t.Logf("New leader elected: %s", newLeaderID)
}

func TestNetworkPartition(t *testing.T) {
	harness := NewTestHarness(t)
	ctx := context.Background()

	// Create a 3-node cluster
	if err := harness.CreateCluster(ctx, 3); err != nil {
		t.Fatalf("failed to create cluster: %v", err)
	}

	// Start cluster
	if err := harness.StartCluster(ctx); err != nil {
		t.Fatalf("failed to start cluster: %v", err)
	}
	defer harness.StopCluster(ctx)

	// Wait for initial leader
	leaderID, err := harness.WaitForLeader(5 * time.Second)
	if err != nil {
		t.Fatalf("initial leader election failed: %v", err)
	}

	t.Logf("Initial leader: %s", leaderID)

	// Partition one follower
	followerID := ""
	node1, _ := harness.GetNode("node-1")
	node2, _ := harness.GetNode("node-2")
	node3, _ := harness.GetNode("node-3")

	if node1.ID != leaderID {
		followerID = node1.ID
	} else if node2.ID != leaderID {
		followerID = node2.ID
	} else {
		followerID = node3.ID
	}

	if err := harness.PartitionNode(followerID); err != nil {
		t.Fatalf("failed to partition node: %v", err)
	}

	t.Logf("Partitioned follower: %s", followerID)

	// Leader should still exist (we have quorum)
	time.Sleep(2 * time.Second)
	harness.AssertLeaderExists()

	// Heal partition
	if err := harness.HealPartition(followerID); err != nil {
		t.Fatalf("failed to heal partition: %v", err)
	}

	t.Logf("Healed partition for: %s", followerID)

	// All nodes should eventually agree on leader
	time.Sleep(2 * time.Second)
	harness.AssertLeaderExists()
}

func TestLogReplication(t *testing.T) {
	harness := NewTestHarness(t)
	ctx := context.Background()

	// Create a 3-node cluster
	if err := harness.CreateCluster(ctx, 3); err != nil {
		t.Fatalf("failed to create cluster: %v", err)
	}

	// Start cluster
	if err := harness.StartCluster(ctx); err != nil {
		t.Fatalf("failed to start cluster: %v", err)
	}
	defer harness.StopCluster(ctx)

	// Wait for leader
	_, err := harness.WaitForLeader(5 * time.Second)
	if err != nil {
		t.Fatalf("leader election failed: %v", err)
	}

	// Submit commands
	commands := [][]byte{
		[]byte("set:key1:value1"),
		[]byte("set:key2:value2"),
		[]byte("set:key3:value3"),
	}

	for i, cmd := range commands {
		if err := harness.SubmitToLeader(ctx, cmd); err != nil {
			t.Fatalf("failed to submit command %d: %v", i, err)
		}
	}

	// Wait for all commands to be committed
	if err := harness.WaitForCommit(uint64(len(commands)), 10*time.Second); err != nil {
		t.Fatalf("log replication failed: %v", err)
	}

	t.Logf("All commands replicated successfully")
}

func TestSingleNodeCluster(t *testing.T) {
	harness := NewTestHarness(t)
	ctx := context.Background()

	// Create a 1-node cluster
	if err := harness.CreateCluster(ctx, 1); err != nil {
		t.Fatalf("failed to create cluster: %v", err)
	}

	// Start cluster
	if err := harness.StartCluster(ctx); err != nil {
		t.Fatalf("failed to start cluster: %v", err)
	}
	defer harness.StopCluster(ctx)

	// Single node should become leader immediately
	leaderID, err := harness.WaitForLeader(5 * time.Second)
	if err != nil {
		t.Fatalf("leader election failed: %v", err)
	}

	if leaderID != "node-1" {
		t.Fatalf("expected node-1 to be leader, got %s", leaderID)
	}

	t.Logf("Single node became leader: %s", leaderID)
}
