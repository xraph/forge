package gateway

import (
	"hash/fnv"
	"math/rand"
	"sync/atomic"
)

// LoadBalancer selects a target from a list of healthy targets.
type LoadBalancer interface {
	// Select picks a target from the provided list.
	// The key is used for consistent hashing (may be empty for other strategies).
	Select(targets []*Target, key string) *Target
}

// NewLoadBalancer creates a load balancer for the given strategy.
func NewLoadBalancer(strategy LoadBalanceStrategy) LoadBalancer {
	switch strategy {
	case LBWeightedRoundRobin:
		return &weightedRoundRobinLB{}
	case LBRandom:
		return &randomLB{}
	case LBLeastConnections:
		return &leastConnectionsLB{}
	case LBConsistentHash:
		return &consistentHashLB{}
	default:
		return &roundRobinLB{}
	}
}

// filterHealthy returns only healthy, non-draining targets.
func filterHealthy(targets []*Target) []*Target {
	healthy := make([]*Target, 0, len(targets))

	for _, t := range targets {
		if t.Healthy && t.CircuitState != CircuitOpen && !t.IsDraining() {
			healthy = append(healthy, t)
		}
	}

	return healthy
}

// --- Round Robin ---

type roundRobinLB struct {
	counter atomic.Uint64
}

func (lb *roundRobinLB) Select(targets []*Target, _ string) *Target {
	healthy := filterHealthy(targets)
	if len(healthy) == 0 {
		return nil
	}

	idx := lb.counter.Add(1) - 1

	return healthy[int(idx)%len(healthy)]
}

// --- Weighted Round Robin (smooth weighted) ---

type weightedRoundRobinLB struct {
	counter atomic.Uint64
}

func (lb *weightedRoundRobinLB) Select(targets []*Target, _ string) *Target {
	healthy := filterHealthy(targets)
	if len(healthy) == 0 {
		return nil
	}

	// If all weights are equal, fall back to simple round-robin
	totalWeight := 0
	for _, t := range healthy {
		w := t.Weight
		if w <= 0 {
			w = 1
		}

		totalWeight += w
	}

	idx := lb.counter.Add(1) - 1
	point := int(idx) % totalWeight

	for _, t := range healthy {
		w := t.Weight
		if w <= 0 {
			w = 1
		}

		point -= w
		if point < 0 {
			return t
		}
	}

	return healthy[0]
}

// --- Random ---

type randomLB struct{}

func (lb *randomLB) Select(targets []*Target, _ string) *Target {
	healthy := filterHealthy(targets)
	if len(healthy) == 0 {
		return nil
	}

	return healthy[rand.Intn(len(healthy))]
}

// --- Least Connections ---

type leastConnectionsLB struct{}

func (lb *leastConnectionsLB) Select(targets []*Target, _ string) *Target {
	healthy := filterHealthy(targets)
	if len(healthy) == 0 {
		return nil
	}

	selected := healthy[0]
	minConns := selected.activeConns.Load()

	for _, t := range healthy[1:] {
		conns := t.activeConns.Load()
		if conns < minConns {
			minConns = conns
			selected = t
		}
	}

	return selected
}

// --- Consistent Hash ---

type consistentHashLB struct{}

func (lb *consistentHashLB) Select(targets []*Target, key string) *Target {
	healthy := filterHealthy(targets)
	if len(healthy) == 0 {
		return nil
	}

	if key == "" {
		// Fallback to round-robin behavior if no key
		return healthy[0]
	}

	h := fnv.New32a()
	_, _ = h.Write([]byte(key))
	idx := int(h.Sum32()) % len(healthy)

	return healthy[idx]
}
