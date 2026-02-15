package gateway

import (
	"testing"
)

func TestNewLoadBalancer(t *testing.T) {
	strategies := []LoadBalanceStrategy{
		LBRoundRobin,
		LBWeightedRoundRobin,
		LBRandom,
		LBLeastConnections,
		LBConsistentHash,
	}

	for _, strategy := range strategies {
		t.Run(string(strategy), func(t *testing.T) {
			lb := NewLoadBalancer(strategy)
			if lb == nil {
				t.Fatal("expected load balancer, got nil")
			}
		})
	}
}

func TestLoadBalancer_RoundRobin(t *testing.T) {
	lb := NewLoadBalancer(LBRoundRobin)

	targets := []*Target{
		{ID: "t1", URL: "http://localhost:8080", Healthy: true, Weight: 1},
		{ID: "t2", URL: "http://localhost:8081", Healthy: true, Weight: 1},
		{ID: "t3", URL: "http://localhost:8082", Healthy: true, Weight: 1},
	}

	// Should cycle through targets in order
	expected := []string{"t1", "t2", "t3", "t1", "t2", "t3"}

	for i, exp := range expected {
		target := lb.Select(targets, "")
		if target == nil {
			t.Fatalf("iteration %d: expected target, got nil", i)
		}
		if target.ID != exp {
			t.Errorf("iteration %d: expected target %s, got %s", i, exp, target.ID)
		}
	}
}

func TestLoadBalancer_RoundRobin_OnlyHealthy(t *testing.T) {
	lb := NewLoadBalancer(LBRoundRobin)

	targets := []*Target{
		{ID: "t1", URL: "http://localhost:8080", Healthy: true},
		{ID: "t2", URL: "http://localhost:8081", Healthy: false},
		{ID: "t3", URL: "http://localhost:8082", Healthy: true},
	}

	// Should only select healthy targets
	for i := 0; i < 10; i++ {
		target := lb.Select(targets, "")
		if target == nil {
			t.Fatalf("iteration %d: expected target, got nil", i)
		}
		if target.ID == "t2" {
			t.Errorf("iteration %d: selected unhealthy target t2", i)
		}
	}
}

func TestLoadBalancer_WeightedRoundRobin(t *testing.T) {
	lb := NewLoadBalancer(LBWeightedRoundRobin)

	targets := []*Target{
		{ID: "t1", URL: "http://localhost:8080", Healthy: true, Weight: 3},
		{ID: "t2", URL: "http://localhost:8081", Healthy: true, Weight: 1},
	}

	// Track selections
	counts := make(map[string]int)
	iterations := 100

	for i := 0; i < iterations; i++ {
		target := lb.Select(targets, "")
		if target == nil {
			t.Fatal("expected target, got nil")
		}
		counts[target.ID]++
	}

	// t1 should be selected ~75% of the time (weight 3 out of 4)
	// t2 should be selected ~25% of the time (weight 1 out of 4)
	ratio := float64(counts["t1"]) / float64(counts["t2"])
	if ratio < 2.0 || ratio > 4.0 {
		t.Errorf("expected ratio ~3.0, got %.2f (t1=%d, t2=%d)", ratio, counts["t1"], counts["t2"])
	}
}

func TestLoadBalancer_Random(t *testing.T) {
	lb := NewLoadBalancer(LBRandom)

	targets := []*Target{
		{ID: "t1", URL: "http://localhost:8080", Healthy: true},
		{ID: "t2", URL: "http://localhost:8081", Healthy: true},
		{ID: "t3", URL: "http://localhost:8082", Healthy: true},
	}

	// Track selections
	counts := make(map[string]int)
	iterations := 300

	for i := 0; i < iterations; i++ {
		target := lb.Select(targets, "")
		if target == nil {
			t.Fatal("expected target, got nil")
		}
		counts[target.ID]++
	}

	// Each target should be selected roughly 1/3 of the time
	// Allow for 20-40% per target (accounts for randomness)
	for id, count := range counts {
		percentage := float64(count) / float64(iterations)
		if percentage < 0.20 || percentage > 0.40 {
			t.Errorf("target %s selected %.1f%% of time, expected ~33%%", id, percentage*100)
		}
	}
}

func TestLoadBalancer_LeastConnections(t *testing.T) {
	lb := NewLoadBalancer(LBLeastConnections)

	t1 := &Target{ID: "t1", URL: "http://localhost:8080", Healthy: true}
	t2 := &Target{ID: "t2", URL: "http://localhost:8081", Healthy: true}
	t3 := &Target{ID: "t3", URL: "http://localhost:8082", Healthy: true}

	// Set active connections via atomic counters (IncrConns).
	// t1: 5 conns, t2: 2 conns, t3: 10 conns.
	for i := 0; i < 5; i++ {
		t1.IncrConns()
	}
	for i := 0; i < 2; i++ {
		t2.IncrConns()
	}
	for i := 0; i < 10; i++ {
		t3.IncrConns()
	}

	targets := []*Target{t1, t2, t3}

	// Should select t2 (least connections).
	target := lb.Select(targets, "")
	if target == nil {
		t.Fatal("expected target, got nil")
	}
	if target.ID != "t2" {
		t.Errorf("expected target t2 (least connections), got %s", target.ID)
	}

	// Bump t2 to 22 total conns so t1 (5) is now least.
	for i := 0; i < 20; i++ {
		t2.IncrConns()
	}

	target = lb.Select(targets, "")
	if target == nil {
		t.Fatal("expected target, got nil")
	}
	if target.ID != "t1" {
		t.Errorf("expected target t1 (now least connections), got %s", target.ID)
	}
}

func TestLoadBalancer_ConsistentHash(t *testing.T) {
	lb := NewLoadBalancer(LBConsistentHash)

	targets := []*Target{
		{ID: "t1", URL: "http://localhost:8080", Healthy: true},
		{ID: "t2", URL: "http://localhost:8081", Healthy: true},
		{ID: "t3", URL: "http://localhost:8082", Healthy: true},
	}

	// Same key should always return same target
	key1 := "user-123"
	target1 := lb.Select(targets, key1)
	if target1 == nil {
		t.Fatal("expected target, got nil")
	}

	for i := 0; i < 10; i++ {
		target := lb.Select(targets, key1)
		if target == nil {
			t.Fatal("expected target, got nil")
		}
		if target.ID != target1.ID {
			t.Errorf("consistent hash failed: expected %s, got %s", target1.ID, target.ID)
		}
	}

	// Different key should potentially return different target
	key2 := "user-456"
	target2 := lb.Select(targets, key2)
	if target2 == nil {
		t.Fatal("expected target, got nil")
	}
	// Note: target2 might be the same as target1, that's okay

	// But same key should consistently return same target
	for i := 0; i < 10; i++ {
		target := lb.Select(targets, key2)
		if target == nil {
			t.Fatal("expected target, got nil")
		}
		if target.ID != target2.ID {
			t.Errorf("consistent hash failed for key2: expected %s, got %s", target2.ID, target.ID)
		}
	}
}

func TestLoadBalancer_NoHealthyTargets(t *testing.T) {
	strategies := []LoadBalanceStrategy{
		LBRoundRobin,
		LBWeightedRoundRobin,
		LBRandom,
		LBLeastConnections,
		LBConsistentHash,
	}

	targets := []*Target{
		{ID: "t1", URL: "http://localhost:8080", Healthy: false},
		{ID: "t2", URL: "http://localhost:8081", Healthy: false},
	}

	for _, strategy := range strategies {
		t.Run(string(strategy), func(t *testing.T) {
			lb := NewLoadBalancer(strategy)
			target := lb.Select(targets, "")
			if target != nil {
				t.Errorf("expected nil for no healthy targets, got %v", target)
			}
		})
	}
}

func TestLoadBalancer_EmptyTargets(t *testing.T) {
	strategies := []LoadBalanceStrategy{
		LBRoundRobin,
		LBWeightedRoundRobin,
		LBRandom,
		LBLeastConnections,
		LBConsistentHash,
	}

	targets := []*Target{}

	for _, strategy := range strategies {
		t.Run(string(strategy), func(t *testing.T) {
			lb := NewLoadBalancer(strategy)
			target := lb.Select(targets, "")
			if target != nil {
				t.Errorf("expected nil for empty targets, got %v", target)
			}
		})
	}
}

func TestLoadBalancer_SingleTarget(t *testing.T) {
	strategies := []LoadBalanceStrategy{
		LBRoundRobin,
		LBWeightedRoundRobin,
		LBRandom,
		LBLeastConnections,
		LBConsistentHash,
	}

	targets := []*Target{
		{ID: "t1", URL: "http://localhost:8080", Healthy: true},
	}

	for _, strategy := range strategies {
		t.Run(string(strategy), func(t *testing.T) {
			lb := NewLoadBalancer(strategy)
			for i := 0; i < 10; i++ {
				target := lb.Select(targets, "")
				if target == nil {
					t.Fatal("expected target, got nil")
				}
				if target.ID != "t1" {
					t.Errorf("expected t1, got %s", target.ID)
				}
			}
		})
	}
}

func TestLoadBalancer_ConcurrentAccess(t *testing.T) {
	lb := NewLoadBalancer(LBRoundRobin)

	targets := []*Target{
		{ID: "t1", URL: "http://localhost:8080", Healthy: true},
		{ID: "t2", URL: "http://localhost:8081", Healthy: true},
		{ID: "t3", URL: "http://localhost:8082", Healthy: true},
	}

	done := make(chan bool)

	// Concurrent selections
	for i := 0; i < 100; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				_ = lb.Select(targets, "")
			}
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 100; i++ {
		<-done
	}

	// Should not panic or race
}
