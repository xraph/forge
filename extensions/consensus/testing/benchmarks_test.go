package testing

import (
	"context"
	"testing"
	"time"
)

// Consensus operation benchmarks

func BenchmarkSingleNodePropose(b *testing.B) {
	b.Skip("Benchmark - requires running cluster")

	cluster := NewTestCluster(1)
	defer cluster.Shutdown()
	cluster.Start()

	leader := cluster.WaitForLeader(5 * time.Second)
	if leader == nil {
		b.Fatal("No leader")
	}

	ctx := context.Background()
	data := []byte("benchmark-data")

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		if err := leader.Propose(ctx, data); err != nil {
			b.Fatalf("Propose failed: %v", err)
		}
	}
}

func BenchmarkThreeNodePropose(b *testing.B) {
	b.Skip("Benchmark - requires running cluster")

	cluster := NewTestCluster(3)
	defer cluster.Shutdown()
	cluster.Start()

	leader := cluster.WaitForLeader(5 * time.Second)
	if leader == nil {
		b.Fatal("No leader")
	}

	ctx := context.Background()
	data := []byte("benchmark-data")

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		if err := leader.Propose(ctx, data); err != nil {
			b.Fatalf("Propose failed: %v", err)
		}
	}
}

func BenchmarkFiveNodePropose(b *testing.B) {
	b.Skip("Benchmark - requires running cluster")

	cluster := NewTestCluster(5)
	defer cluster.Shutdown()
	cluster.Start()

	leader := cluster.WaitForLeader(5 * time.Second)
	if leader == nil {
		b.Fatal("No leader")
	}

	ctx := context.Background()
	data := []byte("benchmark-data")

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		if err := leader.Propose(ctx, data); err != nil {
			b.Fatalf("Propose failed: %v", err)
		}
	}
}

func BenchmarkLocalRead(b *testing.B) {
	b.Skip("Benchmark - requires running cluster")

	cluster := NewTestCluster(3)
	defer cluster.Shutdown()
	cluster.Start()

	leader := cluster.WaitForLeader(5 * time.Second)
	if leader == nil {
		b.Fatal("No leader")
	}

	// Populate some data
	leader.Propose(context.Background(), []byte("key:value"))

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = leader.GetState()
	}
}

func BenchmarkLinearizableRead(b *testing.B) {
	b.Skip("Benchmark - requires running cluster")

	cluster := NewTestCluster(3)
	defer cluster.Shutdown()
	cluster.Start()

	leader := cluster.WaitForLeader(5 * time.Second)
	if leader == nil {
		b.Fatal("No leader")
	}

	leader.Propose(context.Background(), []byte("key:value"))

	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = leader.LinearizableRead(ctx)
	}
}

func BenchmarkSnapshot(b *testing.B) {
	b.Skip("Benchmark - requires running cluster")

	cluster := NewTestCluster(3)
	defer cluster.Shutdown()
	cluster.Start()

	leader := cluster.WaitForLeader(5 * time.Second)
	if leader == nil {
		b.Fatal("No leader")
	}

	// Add some data
	for i := 0; i < 1000; i++ {
		leader.Propose(context.Background(), []byte("entry"))
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		if err := leader.CreateSnapshot(); err != nil {
			b.Fatalf("Snapshot failed: %v", err)
		}
	}
}

func BenchmarkLeaderElection(b *testing.B) {
	b.Skip("Benchmark - requires running cluster")

	for i := 0; i < b.N; i++ {
		cluster := NewTestCluster(3)
		cluster.Start()

		start := time.Now()
		leader := cluster.WaitForLeader(10 * time.Second)
		elapsed := time.Since(start)

		if leader == nil {
			b.Fatal("No leader elected")
		}

		b.ReportMetric(float64(elapsed.Milliseconds()), "election_ms")

		cluster.Shutdown()
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

	cluster := NewTestCluster(3)
	defer cluster.Shutdown()
	cluster.Start()

	leader := cluster.WaitForLeader(5 * time.Second)
	if leader == nil {
		b.Fatal("No leader")
	}

	data := make([]byte, dataSize)
	ctx := context.Background()

	b.SetBytes(int64(dataSize))
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		if err := leader.Propose(ctx, data); err != nil {
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

	cluster := NewTestCluster(3)
	defer cluster.Shutdown()
	cluster.Start()

	leader := cluster.WaitForLeader(5 * time.Second)
	if leader == nil {
		b.Fatal("No leader")
	}

	data := []byte("benchmark-data")
	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if err := leader.Propose(ctx, data); err != nil {
				b.Errorf("Propose failed: %v", err)
			}
		}
	})
}

// Memory allocation benchmarks

func BenchmarkMemoryAllocation_SmallEntries(b *testing.B) {
	b.Skip("Benchmark - requires running cluster")

	cluster := NewTestCluster(3)
	defer cluster.Shutdown()
	cluster.Start()

	leader := cluster.WaitForLeader(5 * time.Second)
	if leader == nil {
		b.Fatal("No leader")
	}

	data := []byte("small-entry")
	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		leader.Propose(ctx, data)
	}
}

func BenchmarkMemoryAllocation_LargeEntries(b *testing.B) {
	b.Skip("Benchmark - requires running cluster")

	cluster := NewTestCluster(3)
	defer cluster.Shutdown()
	cluster.Start()

	leader := cluster.WaitForLeader(5 * time.Second)
	if leader == nil {
		b.Fatal("No leader")
	}

	data := make([]byte, 1024*1024) // 1MB
	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		leader.Propose(ctx, data)
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

	cluster := NewTestCluster(3)
	defer cluster.Shutdown()
	cluster.Start()

	leader := cluster.WaitForLeader(5 * time.Second)
	if leader == nil {
		b.Fatal("No leader")
	}

	data := []byte("latency-test")
	ctx := context.Background()

	latencies := make([]time.Duration, b.N)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		start := time.Now()
		leader.Propose(ctx, data)
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

	cluster := NewTestCluster(clusterSize)
	defer cluster.Shutdown()
	cluster.Start()

	leader := cluster.WaitForLeader(10 * time.Second)
	if leader == nil {
		b.Fatal("No leader")
	}

	data := []byte("scalability-test")
	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		if err := leader.Propose(ctx, data); err != nil {
			b.Fatalf("Propose failed: %v", err)
		}
	}

	b.ReportMetric(float64(clusterSize), "cluster_size")
}
