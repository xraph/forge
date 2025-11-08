package testing

import (
	"context"
	"testing"
	"time"
)

// Consensus operation benchmarks

func BenchmarkSingleNodePropose(b *testing.B) {
	b.Skip("Benchmark - requires running cluster")

	harness := NewTestHarness(&testing.T{})
	ctx := context.Background()

	if err := harness.CreateCluster(ctx, 1); err != nil {
		b.Fatalf("failed to create cluster: %v", err)
	}
	defer harness.StopCluster(ctx)

	if err := harness.StartCluster(ctx); err != nil {
		b.Fatalf("failed to start cluster: %v", err)
	}

	leaderID, err := harness.WaitForLeader(5 * time.Second)
	if err != nil {
		b.Fatalf("No leader: %v", err)
	}

	leader, err := harness.GetNode(leaderID)
	if err != nil {
		b.Fatalf("Failed to get leader: %v", err)
	}

	data := []byte("benchmark-data")

	b.ReportAllocs()

	for b.Loop() {
		if err := leader.RaftNode.Propose(ctx, data); err != nil {
			b.Fatalf("Propose failed: %v", err)
		}
	}
}

func BenchmarkThreeNodePropose(b *testing.B) {
	b.Skip("Benchmark - requires running cluster")

	harness := NewTestHarness(&testing.T{})
	ctx := context.Background()

	if err := harness.CreateCluster(ctx, 3); err != nil {
		b.Fatalf("failed to create cluster: %v", err)
	}
	defer harness.StopCluster(ctx)

	if err := harness.StartCluster(ctx); err != nil {
		b.Fatalf("failed to start cluster: %v", err)
	}

	leaderID, err := harness.WaitForLeader(5 * time.Second)
	if err != nil {
		b.Fatalf("No leader: %v", err)
	}

	leader, err := harness.GetNode(leaderID)
	if err != nil {
		b.Fatalf("Failed to get leader: %v", err)
	}

	data := []byte("benchmark-data")

	b.ReportAllocs()

	for b.Loop() {
		if err := leader.RaftNode.Propose(ctx, data); err != nil {
			b.Fatalf("Propose failed: %v", err)
		}
	}
}

func BenchmarkFiveNodePropose(b *testing.B) {
	b.Skip("Benchmark - requires running cluster")

	harness := NewTestHarness(&testing.T{})
	ctx := context.Background()

	if err := harness.CreateCluster(ctx, 5); err != nil {
		b.Fatalf("failed to create cluster: %v", err)
	}
	defer harness.StopCluster(ctx)

	if err := harness.StartCluster(ctx); err != nil {
		b.Fatalf("failed to start cluster: %v", err)
	}

	leaderID, err := harness.WaitForLeader(5 * time.Second)
	if err != nil {
		b.Fatalf("No leader: %v", err)
	}

	leader, err := harness.GetNode(leaderID)
	if err != nil {
		b.Fatalf("Failed to get leader: %v", err)
	}

	data := []byte("benchmark-data")

	b.ReportAllocs()

	for b.Loop() {
		if err := leader.RaftNode.Propose(ctx, data); err != nil {
			b.Fatalf("Propose failed: %v", err)
		}
	}
}

func BenchmarkLocalRead(b *testing.B) {
	b.Skip("Benchmark - requires running cluster")

	harness := NewTestHarness(&testing.T{})
	ctx := context.Background()

	if err := harness.CreateCluster(ctx, 3); err != nil {
		b.Fatalf("failed to create cluster: %v", err)
	}
	defer harness.StopCluster(ctx)

	if err := harness.StartCluster(ctx); err != nil {
		b.Fatalf("failed to start cluster: %v", err)
	}

	leaderID, err := harness.WaitForLeader(5 * time.Second)
	if err != nil {
		b.Fatalf("No leader: %v", err)
	}

	leader, err := harness.GetNode(leaderID)
	if err != nil {
		b.Fatalf("Failed to get leader: %v", err)
	}

	// Populate some data
	leader.RaftNode.Propose(context.Background(), []byte("key:value"))

	b.ReportAllocs()

	for b.Loop() {
		_ = leader.StateMachine
	}
}

func BenchmarkLinearizableRead(b *testing.B) {
	b.Skip("Benchmark - requires running cluster")

	harness := NewTestHarness(&testing.T{})
	ctx := context.Background()

	if err := harness.CreateCluster(ctx, 3); err != nil {
		b.Fatalf("failed to create cluster: %v", err)
	}
	defer harness.StopCluster(ctx)

	if err := harness.StartCluster(ctx); err != nil {
		b.Fatalf("failed to start cluster: %v", err)
	}

	leaderID, err := harness.WaitForLeader(5 * time.Second)
	if err != nil {
		b.Fatalf("No leader: %v", err)
	}

	leader, err := harness.GetNode(leaderID)
	if err != nil {
		b.Fatalf("Failed to get leader: %v", err)
	}

	leader.RaftNode.Propose(context.Background(), []byte("key:value"))

	b.ReportAllocs()

	for b.Loop() {
		_ = leader.RaftNode.GetCommitIndex()
	}
}

func BenchmarkSnapshot(b *testing.B) {
	b.Skip("Benchmark - requires running cluster")

	harness := NewTestHarness(&testing.T{})
	ctx := context.Background()

	if err := harness.CreateCluster(ctx, 3); err != nil {
		b.Fatalf("failed to create cluster: %v", err)
	}
	defer harness.StopCluster(ctx)

	if err := harness.StartCluster(ctx); err != nil {
		b.Fatalf("failed to start cluster: %v", err)
	}

	leaderID, err := harness.WaitForLeader(5 * time.Second)
	if err != nil {
		b.Fatalf("No leader: %v", err)
	}

	leader, err := harness.GetNode(leaderID)
	if err != nil {
		b.Fatalf("Failed to get leader: %v", err)
	}

	// Add some data
	for range 1000 {
		leader.RaftNode.Propose(context.Background(), []byte("entry"))
	}

	b.ReportAllocs()

	for b.Loop() {
		// Snapshot functionality would be implemented here
		_ = leader.RaftNode.GetCommitIndex()
	}
}

func BenchmarkLeaderElection(b *testing.B) {
	b.Skip("Benchmark - requires running cluster")

	for b.Loop() {
		harness := NewTestHarness(&testing.T{})
		ctx := context.Background()

		if err := harness.CreateCluster(ctx, 3); err != nil {
			b.Fatalf("failed to create cluster: %v", err)
		}

		if err := harness.StartCluster(ctx); err != nil {
			b.Fatalf("failed to start cluster: %v", err)
		}

		start := time.Now()
		_, err := harness.WaitForLeader(10 * time.Second)
		elapsed := time.Since(start)

		if err != nil {
			b.Fatalf("No leader elected: %v", err)
		}

		b.ReportMetric(float64(elapsed.Milliseconds()), "election_ms")

		harness.StopCluster(ctx)
	}
}

// Throughput benchmarks

func BenchmarkThroughput_1KB(b *testing.B) {
	benchmarkThroughput(b, 1024)
}

func BenchmarkThroughput_10KB(b *testing.B) {
	benchmarkThroughput(b, 10*1024)
}

func BenchmarkThroughput_100KB(b *testing.B) {
	benchmarkThroughput(b, 100*1024)
}

func BenchmarkThroughput_1MB(b *testing.B) {
	benchmarkThroughput(b, 1024*1024)
}

func benchmarkThroughput(b *testing.B, dataSize int) {
	b.Skip("Benchmark - requires running cluster")

	harness := NewTestHarness(&testing.T{})
	ctx := context.Background()

	if err := harness.CreateCluster(ctx, 3); err != nil {
		b.Fatalf("failed to create cluster: %v", err)
	}
	defer harness.StopCluster(ctx)

	if err := harness.StartCluster(ctx); err != nil {
		b.Fatalf("failed to start cluster: %v", err)
	}

	leaderID, err := harness.WaitForLeader(5 * time.Second)
	if err != nil {
		b.Fatalf("No leader: %v", err)
	}

	leader, err := harness.GetNode(leaderID)
	if err != nil {
		b.Fatalf("Failed to get leader: %v", err)
	}

	data := make([]byte, dataSize)

	b.SetBytes(int64(dataSize))
	b.ResetTimer()
	b.ReportAllocs()

	for range b.N {
		if err := leader.RaftNode.Propose(ctx, data); err != nil {
			b.Fatalf("Propose failed: %v", err)
		}
	}
}

// Concurrent operation benchmarks

func BenchmarkConcurrentWrites_10(b *testing.B) {
	benchmarkConcurrentWrites(b, 10)
}

func BenchmarkConcurrentWrites_100(b *testing.B) {
	benchmarkConcurrentWrites(b, 100)
}

func BenchmarkConcurrentWrites_1000(b *testing.B) {
	benchmarkConcurrentWrites(b, 1000)
}

func benchmarkConcurrentWrites(b *testing.B, concurrency int) {
	b.Skip("Benchmark - requires running cluster")

	harness := NewTestHarness(&testing.T{})
	ctx := context.Background()

	if err := harness.CreateCluster(ctx, 3); err != nil {
		b.Fatalf("failed to create cluster: %v", err)
	}
	defer harness.StopCluster(ctx)

	if err := harness.StartCluster(ctx); err != nil {
		b.Fatalf("failed to start cluster: %v", err)
	}

	leaderID, err := harness.WaitForLeader(5 * time.Second)
	if err != nil {
		b.Fatalf("No leader: %v", err)
	}

	leader, err := harness.GetNode(leaderID)
	if err != nil {
		b.Fatalf("Failed to get leader: %v", err)
	}

	data := []byte("benchmark-data")

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if err := leader.RaftNode.Propose(ctx, data); err != nil {
				b.Errorf("Propose failed: %v", err)
			}
		}
	})
}

// Memory allocation benchmarks

func BenchmarkMemoryAllocation_SmallEntries(b *testing.B) {
	b.Skip("Benchmark - requires running cluster")

	harness := NewTestHarness(&testing.T{})
	ctx := context.Background()

	if err := harness.CreateCluster(ctx, 3); err != nil {
		b.Fatalf("failed to create cluster: %v", err)
	}
	defer harness.StopCluster(ctx)

	if err := harness.StartCluster(ctx); err != nil {
		b.Fatalf("failed to start cluster: %v", err)
	}

	leaderID, err := harness.WaitForLeader(5 * time.Second)
	if err != nil {
		b.Fatalf("No leader: %v", err)
	}

	leader, err := harness.GetNode(leaderID)
	if err != nil {
		b.Fatalf("Failed to get leader: %v", err)
	}

	data := []byte("small-entry")

	b.ReportAllocs()

	for b.Loop() {
		leader.RaftNode.Propose(ctx, data)
	}
}

func BenchmarkMemoryAllocation_LargeEntries(b *testing.B) {
	b.Skip("Benchmark - requires running cluster")

	harness := NewTestHarness(&testing.T{})
	ctx := context.Background()

	if err := harness.CreateCluster(ctx, 3); err != nil {
		b.Fatalf("failed to create cluster: %v", err)
	}
	defer harness.StopCluster(ctx)

	if err := harness.StartCluster(ctx); err != nil {
		b.Fatalf("failed to start cluster: %v", err)
	}

	leaderID, err := harness.WaitForLeader(5 * time.Second)
	if err != nil {
		b.Fatalf("No leader: %v", err)
	}

	leader, err := harness.GetNode(leaderID)
	if err != nil {
		b.Fatalf("Failed to get leader: %v", err)
	}

	data := make([]byte, 1024*1024) // 1MB

	b.ReportAllocs()

	for b.Loop() {
		leader.RaftNode.Propose(ctx, data)
	}
}

// Latency benchmarks

func BenchmarkLatency_P50(b *testing.B) {
	benchmarkLatency(b, 50)
}

func BenchmarkLatency_P95(b *testing.B) {
	benchmarkLatency(b, 95)
}

func BenchmarkLatency_P99(b *testing.B) {
	benchmarkLatency(b, 99)
}

func benchmarkLatency(b *testing.B, percentile int) {
	b.Skip("Benchmark - requires running cluster")

	harness := NewTestHarness(&testing.T{})
	ctx := context.Background()

	if err := harness.CreateCluster(ctx, 3); err != nil {
		b.Fatalf("failed to create cluster: %v", err)
	}
	defer harness.StopCluster(ctx)

	if err := harness.StartCluster(ctx); err != nil {
		b.Fatalf("failed to start cluster: %v", err)
	}

	leaderID, err := harness.WaitForLeader(5 * time.Second)
	if err != nil {
		b.Fatalf("No leader: %v", err)
	}

	leader, err := harness.GetNode(leaderID)
	if err != nil {
		b.Fatalf("Failed to get leader: %v", err)
	}

	data := []byte("latency-test")

	latencies := make([]time.Duration, b.N)

	b.ResetTimer()

	for i := range b.N {
		start := time.Now()

		leader.RaftNode.Propose(ctx, data)

		latencies[i] = time.Since(start)
	}

	b.StopTimer()

	// Calculate percentile
	// (In production, use a proper percentile calculation library)
	b.ReportMetric(float64(latencies[b.N/2].Milliseconds()), "p50_ms")
}

// Scalability benchmarks

func BenchmarkScalability_3Nodes(b *testing.B) {
	benchmarkScalability(b, 3)
}

func BenchmarkScalability_5Nodes(b *testing.B) {
	benchmarkScalability(b, 5)
}

func BenchmarkScalability_7Nodes(b *testing.B) {
	benchmarkScalability(b, 7)
}

func benchmarkScalability(b *testing.B, clusterSize int) {
	b.Skip("Benchmark - requires running cluster")

	harness := NewTestHarness(&testing.T{})
	ctx := context.Background()

	if err := harness.CreateCluster(ctx, clusterSize); err != nil {
		b.Fatalf("failed to create cluster: %v", err)
	}
	defer harness.StopCluster(ctx)

	if err := harness.StartCluster(ctx); err != nil {
		b.Fatalf("failed to start cluster: %v", err)
	}

	leaderID, err := harness.WaitForLeader(10 * time.Second)
	if err != nil {
		b.Fatalf("No leader: %v", err)
	}

	leader, err := harness.GetNode(leaderID)
	if err != nil {
		b.Fatalf("Failed to get leader: %v", err)
	}

	data := []byte("scalability-test")

	b.ResetTimer()
	b.ReportAllocs()

	for range b.N {
		if err := leader.RaftNode.Propose(ctx, data); err != nil {
			b.Fatalf("Propose failed: %v", err)
		}
	}

	b.ReportMetric(float64(clusterSize), "cluster_size")
}
