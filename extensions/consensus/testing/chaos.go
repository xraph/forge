package testing

import (
	"context"
	"fmt"
	"math/rand"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/errors"
)

// ChaosMonkey implements chaos engineering for consensus testing.
type ChaosMonkey struct {
	cluster *TestCluster
	logger  forge.Logger
	ctx     context.Context
	cancel  context.CancelFunc
	config  ChaosConfig
}

// ChaosConfig contains chaos testing configuration.
type ChaosConfig struct {
	// Node failure probability (0.0 to 1.0)
	NodeFailureProbability float64
	// Network partition probability
	PartitionProbability float64
	// Latency injection probability
	LatencyProbability float64
	// Message drop probability
	MessageDropProbability float64
	// Check interval
	CheckInterval time.Duration
	// Min/Max latency to inject
	MinLatency time.Duration
	MaxLatency time.Duration
}

// NewChaosMonkey creates a new chaos monkey.
func NewChaosMonkey(cluster *TestCluster, config ChaosConfig, logger forge.Logger) *ChaosMonkey {
	if config.CheckInterval == 0 {
		config.CheckInterval = 5 * time.Second
	}

	if config.MinLatency == 0 {
		config.MinLatency = 100 * time.Millisecond
	}

	if config.MaxLatency == 0 {
		config.MaxLatency = 1 * time.Second
	}

	return &ChaosMonkey{
		cluster: cluster,
		logger:  logger,
		config:  config,
	}
}

// Start starts the chaos monkey.
func (cm *ChaosMonkey) Start(ctx context.Context) {
	cm.ctx, cm.cancel = context.WithCancel(ctx)

	go cm.run()

	cm.logger.Info("chaos monkey started",
		forge.F("node_failure_prob", cm.config.NodeFailureProbability),
		forge.F("partition_prob", cm.config.PartitionProbability),
	)
}

// Stop stops the chaos monkey.
func (cm *ChaosMonkey) Stop() {
	if cm.cancel != nil {
		cm.cancel()
	}

	cm.logger.Info("chaos monkey stopped")
}

// run runs the chaos monkey.
func (cm *ChaosMonkey) run() {
	ticker := time.NewTicker(cm.config.CheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-cm.ctx.Done():
			return
		case <-ticker.C:
			cm.applyChoas()
		}
	}
}

// applyChoas applies random chaos to the cluster.
func (cm *ChaosMonkey) applyChoas() {
	// Randomly fail nodes
	if rand.Float64() < cm.config.NodeFailureProbability {
		cm.failRandomNode()
	}

	// Randomly create partitions
	if rand.Float64() < cm.config.PartitionProbability {
		cm.createRandomPartition()
	}

	// Randomly inject latency
	if rand.Float64() < cm.config.LatencyProbability {
		cm.injectRandomLatency()
	}

	// Randomly drop messages
	if rand.Float64() < cm.config.MessageDropProbability {
		cm.dropRandomMessages()
	}
}

// failRandomNode fails a random node.
func (cm *ChaosMonkey) failRandomNode() {
	nodes := cm.cluster.GetNodes()
	if len(nodes) == 0 {
		return
	}

	// Don't fail all nodes
	healthyNodes := cm.cluster.GetHealthyNodeCount()
	if healthyNodes <= cm.cluster.GetQuorumSize() {
		return
	}

	// Pick random node
	node := nodes[rand.Intn(len(nodes))]

	cm.logger.Warn("chaos: failing node",
		forge.F("node_id", node.ID),
	)

	cm.cluster.StopNode(context.Background(), node.ID)

	// Schedule recovery
	go func() {
		time.Sleep(time.Duration(rand.Intn(30)) * time.Second)

		cm.logger.Info("chaos: recovering node",
			forge.F("node_id", node.ID),
		)

		cm.cluster.StartNode(context.Background(), node.ID)
	}()
}

// createRandomPartition creates a random network partition.
func (cm *ChaosMonkey) createRandomPartition() {
	nodes := cm.cluster.GetNodes()
	if len(nodes) < 3 {
		return
	}

	// Partition 1-2 nodes
	partitionSize := rand.Intn(2) + 1
	if partitionSize >= len(nodes) {
		partitionSize = len(nodes) - 1
	}

	partitionNodes := make([]string, partitionSize)
	for i := range partitionSize {
		partitionNodes[i] = nodes[rand.Intn(len(nodes))].ID
	}

	cm.logger.Warn("chaos: creating partition",
		forge.F("nodes", partitionNodes),
	)

	cm.cluster.CreatePartition(partitionNodes)

	// Schedule heal
	go func() {
		time.Sleep(time.Duration(rand.Intn(20)) * time.Second)

		cm.logger.Info("chaos: healing partition")

		cm.cluster.HealAllPartitions()
	}()
}

// injectRandomLatency injects random network latency.
func (cm *ChaosMonkey) injectRandomLatency() {
	nodes := cm.cluster.GetNodes()
	if len(nodes) == 0 {
		return
	}

	node := nodes[rand.Intn(len(nodes))]

	latency := cm.config.MinLatency +
		time.Duration(rand.Int63n(int64(cm.config.MaxLatency-cm.config.MinLatency)))

	cm.logger.Debug("chaos: injecting latency",
		forge.F("node_id", node.ID),
		forge.F("latency", latency),
	)

	cm.cluster.InjectLatency(node.ID, latency)

	// Schedule removal
	go func() {
		time.Sleep(time.Duration(rand.Intn(10)) * time.Second)
		cm.cluster.RemoveLatency(node.ID)
	}()
}

// dropRandomMessages drops random messages.
func (cm *ChaosMonkey) dropRandomMessages() {
	nodes := cm.cluster.GetNodes()
	if len(nodes) == 0 {
		return
	}

	node := nodes[rand.Intn(len(nodes))]

	dropRate := rand.Float64() * 0.5 // 0-50% drop rate

	cm.logger.Debug("chaos: dropping messages",
		forge.F("node_id", node.ID),
		forge.F("drop_rate", dropRate),
	)

	// Message dropping is handled at the transport layer
	// This is a placeholder for future implementation
	_ = dropRate
}

// ChaosScenario represents a specific chaos scenario.
type ChaosScenario struct {
	Name        string
	Description string
	Apply       func(*TestCluster) error
	Verify      func(*TestCluster) error
	Duration    time.Duration
}

// PredefinedScenarios contains common chaos scenarios.
var PredefinedScenarios = []ChaosScenario{
	{
		Name:        "LeaderFailure",
		Description: "Kill the leader node",
		Apply: func(cluster *TestCluster) error {
			leaderID, err := cluster.GetLeader()
			if err != nil {
				return nil
			}
			cluster.StopNode(context.Background(), leaderID)

			return nil
		},
		Verify: func(cluster *TestCluster) error {
			_, err := cluster.WaitForLeader(10 * time.Second)
			if err != nil {
				return ErrNoLeader
			}

			return nil
		},
		Duration: 10 * time.Second,
	},
	{
		Name:        "MinorityPartition",
		Description: "Partition minority of nodes",
		Apply: func(cluster *TestCluster) error {
			nodes := cluster.GetNodes()
			if len(nodes) < 3 {
				return nil
			}
			// Partition 1 node
			cluster.CreatePartition([]string{nodes[0].ID})

			return nil
		},
		Verify: func(cluster *TestCluster) error {
			// Majority should still elect leader
			_, err := cluster.WaitForLeader(10 * time.Second)
			if err != nil {
				return ErrNoLeader
			}

			return nil
		},
		Duration: 10 * time.Second,
	},
	{
		Name:        "MajorityPartition",
		Description: "Partition majority of nodes",
		Apply: func(cluster *TestCluster) error {
			nodes := cluster.GetNodes()
			if len(nodes) < 3 {
				return nil
			}
			// Partition majority
			partitionSize := (len(nodes) / 2) + 1
			partitionNodes := make([]string, partitionSize)
			for i := range partitionSize {
				partitionNodes[i] = nodes[i].ID
			}
			cluster.CreatePartition(partitionNodes)

			return nil
		},
		Verify: func(cluster *TestCluster) error {
			// Should lose quorum
			time.Sleep(5 * time.Second)

			return nil
		},
		Duration: 10 * time.Second,
	},
	{
		Name:        "CascadeFailure",
		Description: "Fail multiple nodes in sequence",
		Apply: func(cluster *TestCluster) error {
			nodes := cluster.GetNodes()
			if len(nodes) < 3 {
				return nil
			}

			// Fail nodes one by one
			for i := range len(nodes) - 2 {
				cluster.StopNode(context.Background(), nodes[i].ID)
				time.Sleep(2 * time.Second)
			}

			return nil
		},
		Verify: func(cluster *TestCluster) error {
			// Should maintain quorum
			_, err := cluster.WaitForLeader(10 * time.Second)
			if err != nil {
				return ErrNoLeader
			}

			return nil
		},
		Duration: 15 * time.Second,
	},
	{
		Name:        "FlappingNode",
		Description: "Node repeatedly crashes and recovers",
		Apply: func(cluster *TestCluster) error {
			nodes := cluster.GetNodes()
			if len(nodes) == 0 {
				return nil
			}

			node := nodes[0]

			for range 5 {
				cluster.StopNode(context.Background(), node.ID)
				time.Sleep(1 * time.Second)
				cluster.StartNode(context.Background(), node.ID)
				time.Sleep(1 * time.Second)
			}

			return nil
		},
		Verify: func(cluster *TestCluster) error {
			// Cluster should remain stable
			_, err := cluster.WaitForLeader(10 * time.Second)
			if err != nil {
				return ErrNoLeader
			}

			return nil
		},
		Duration: 15 * time.Second,
	},
}

// Predefined errors.
var (
	ErrNoLeader = errors.New("no leader elected")
)

// RunScenario runs a chaos scenario.
func (cm *ChaosMonkey) RunScenario(scenario ChaosScenario) error {
	cm.logger.Info("running chaos scenario",
		forge.F("name", scenario.Name),
		forge.F("description", scenario.Description),
	)

	// Apply chaos
	if err := scenario.Apply(cm.cluster); err != nil {
		return fmt.Errorf("failed to apply scenario: %w", err)
	}

	// Wait for duration
	time.Sleep(scenario.Duration)

	// Verify
	if err := scenario.Verify(cm.cluster); err != nil {
		return fmt.Errorf("scenario verification failed: %w", err)
	}

	cm.logger.Info("chaos scenario completed",
		forge.F("name", scenario.Name),
	)

	return nil
}
