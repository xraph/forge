package cluster

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/consensus/internal"
)

// RebalanceManager manages cluster rebalancing operations
type RebalanceManager struct {
	manager  *Manager
	topology *TopologyManager
	quorum   *QuorumManager
	logger   forge.Logger

	// Rebalance configuration
	enabled                bool
	autoRebalance          bool
	rebalanceThreshold     float64
	rebalanceCheckInterval time.Duration
}

// RebalanceConfig contains rebalance configuration
type RebalanceConfig struct {
	Enabled       bool
	AutoRebalance bool
	Threshold     float64 // Imbalance threshold (0.0 - 1.0)
	CheckInterval time.Duration
}

// RebalanceResult contains the result of a rebalance operation
type RebalanceResult struct {
	StartTime    time.Time
	EndTime      time.Time
	Duration     time.Duration
	ActionsToken int
	Success      bool
	Error        error
	Actions      []RebalanceAction
}

// RebalanceAction represents a single rebalance action
type RebalanceAction struct {
	Type        RebalanceActionType
	SourceNode  string
	TargetNode  string
	Description string
	Applied     bool
	Error       error
}

// RebalanceActionType represents the type of rebalance action
type RebalanceActionType int

const (
	// RebalanceActionMoveLeader moves leadership
	RebalanceActionMoveLeader RebalanceActionType = iota
	// RebalanceActionAddReplica adds a replica
	RebalanceActionAddReplica
	// RebalanceActionRemoveReplica removes a replica
	RebalanceActionRemoveReplica
	// RebalanceActionTransferSnapshot transfers a snapshot
	RebalanceActionTransferSnapshot
)

// NewRebalanceManager creates a new rebalance manager
func NewRebalanceManager(
	manager *Manager,
	topology *TopologyManager,
	quorum *QuorumManager,
	config RebalanceConfig,
	logger forge.Logger,
) *RebalanceManager {
	if config.Threshold == 0 {
		config.Threshold = 0.2 // 20% default
	}
	if config.CheckInterval == 0 {
		config.CheckInterval = 5 * time.Minute
	}

	return &RebalanceManager{
		manager:                manager,
		topology:               topology,
		quorum:                 quorum,
		logger:                 logger,
		enabled:                config.Enabled,
		autoRebalance:          config.AutoRebalance,
		rebalanceThreshold:     config.Threshold,
		rebalanceCheckInterval: config.CheckInterval,
	}
}

// Start starts the rebalance manager
func (rm *RebalanceManager) Start(ctx context.Context) error {
	if !rm.enabled {
		rm.logger.Info("rebalance manager disabled")
		return nil
	}

	if rm.autoRebalance {
		// Start auto-rebalance goroutine
		go rm.autoRebalanceLoop(ctx)
		rm.logger.Info("auto-rebalance enabled",
			forge.F("check_interval", rm.rebalanceCheckInterval),
		)
	}

	return nil
}

// AnalyzeBalance analyzes cluster balance
func (rm *RebalanceManager) AnalyzeBalance() BalanceAnalysis {
	nodes := rm.manager.GetNodes()

	analysis := BalanceAnalysis{
		TotalNodes:  len(nodes),
		Balanced:    true,
		Imbalance:   0.0,
		Suggestions: []string{},
	}

	if len(nodes) == 0 {
		return analysis
	}

	// Calculate load per node (simplified - in production, use actual metrics)
	loadPerNode := make(map[string]float64)
	totalLoad := 0.0

	for _, node := range nodes {
		// Simplified load calculation
		load := 1.0 // Base load
		if node.Role == internal.RoleLeader {
			load = 3.0 // Leader has more load
		}
		loadPerNode[node.ID] = load
		totalLoad += load
	}

	if totalLoad == 0 {
		return analysis
	}

	// Calculate average and variance
	avgLoad := totalLoad / float64(len(nodes))
	var variance float64

	for _, load := range loadPerNode {
		diff := load - avgLoad
		variance += diff * diff
	}
	variance /= float64(len(nodes))

	// Calculate imbalance factor
	analysis.Imbalance = variance / (avgLoad * avgLoad)
	analysis.Balanced = analysis.Imbalance < rm.rebalanceThreshold

	// Generate suggestions
	if !analysis.Balanced {
		analysis.Suggestions = append(analysis.Suggestions,
			fmt.Sprintf("Cluster is imbalanced (factor: %.2f)", analysis.Imbalance))

		// Find most and least loaded nodes
		var maxLoad, minLoad float64
		var maxNode, minNode string

		for nodeID, load := range loadPerNode {
			if maxLoad == 0 || load > maxLoad {
				maxLoad = load
				maxNode = nodeID
			}
			if minLoad == 0 || load < minLoad {
				minLoad = load
				minNode = nodeID
			}
		}

		analysis.Suggestions = append(analysis.Suggestions,
			fmt.Sprintf("Consider moving load from %s (%.2f) to %s (%.2f)",
				maxNode, maxLoad, minNode, minLoad))
	}

	// Check topology balance
	if rm.topology.IsTopologyAware() {
		view := rm.topology.GetTopologyView()

		if view.RegionCount > 1 {
			// Check region balance
			regionLoads := make([]int, 0, len(view.Regions))
			for _, region := range view.Regions {
				regionLoads = append(regionLoads, region.NodeCount)
			}
			sort.Ints(regionLoads)

			if len(regionLoads) > 1 {
				maxRegion := regionLoads[len(regionLoads)-1]
				minRegion := regionLoads[0]

				if float64(maxRegion-minRegion)/float64(maxRegion) > rm.rebalanceThreshold {
					analysis.Balanced = false
					analysis.Suggestions = append(analysis.Suggestions,
						"Regions are imbalanced - consider adding nodes to underprovisioned regions")
				}
			}
		}
	}

	return analysis
}

// BalanceAnalysis contains balance analysis results
type BalanceAnalysis struct {
	TotalNodes  int
	Balanced    bool
	Imbalance   float64
	Suggestions []string
}

// Rebalance performs a rebalance operation
func (rm *RebalanceManager) Rebalance(ctx context.Context) (*RebalanceResult, error) {
	if !rm.enabled {
		return nil, fmt.Errorf("rebalancing is disabled")
	}

	rm.logger.Info("starting cluster rebalance")

	result := &RebalanceResult{
		StartTime: time.Now(),
		Actions:   []RebalanceAction{},
	}

	// Check if we have quorum
	if !rm.quorum.HasQuorum() {
		result.Success = false
		result.Error = fmt.Errorf("cannot rebalance without quorum")
		result.EndTime = time.Now()
		result.Duration = result.EndTime.Sub(result.StartTime)
		return result, result.Error
	}

	// Analyze balance
	analysis := rm.AnalyzeBalance()

	if analysis.Balanced {
		rm.logger.Info("cluster is already balanced")
		result.Success = true
		result.EndTime = time.Now()
		result.Duration = result.EndTime.Sub(result.StartTime)
		return result, nil
	}

	// Generate and execute rebalance actions
	actions := rm.generateRebalanceActions(analysis)

	for _, action := range actions {
		rm.logger.Info("executing rebalance action",
			forge.F("type", action.Type),
			forge.F("description", action.Description),
		)

		err := rm.executeAction(ctx, action)
		action.Applied = err == nil
		action.Error = err

		result.Actions = append(result.Actions, action)
		result.ActionsToken++

		if err != nil {
			rm.logger.Error("rebalance action failed",
				forge.F("action", action.Description),
				forge.F("error", err),
			)
		}
	}

	result.Success = true
	result.EndTime = time.Now()
	result.Duration = result.EndTime.Sub(result.StartTime)

	rm.logger.Info("cluster rebalance completed",
		forge.F("duration", result.Duration),
		forge.F("actions", result.ActionsToken),
	)

	return result, nil
}

// generateRebalanceActions generates rebalance actions
func (rm *RebalanceManager) generateRebalanceActions(analysis BalanceAnalysis) []RebalanceAction {
	var actions []RebalanceAction

	// For now, generate simple actions
	// In production, this would be much more sophisticated

	nodes := rm.manager.GetNodes()

	// Find overloaded and underloaded nodes
	loadMap := make(map[string]float64)
	for _, node := range nodes {
		load := 1.0
		if node.Role == internal.RoleLeader {
			load = 3.0
		}
		loadMap[node.ID] = load
	}

	// Sort by load
	type nodeLoad struct {
		nodeID string
		load   float64
	}

	var sortedNodes []nodeLoad
	for id, load := range loadMap {
		sortedNodes = append(sortedNodes, nodeLoad{id, load})
	}

	sort.Slice(sortedNodes, func(i, j int) bool {
		return sortedNodes[i].load > sortedNodes[j].load
	})

	// Generate action to move leadership from most loaded to least loaded
	if len(sortedNodes) >= 2 {
		mostLoaded := sortedNodes[0]
		leastLoaded := sortedNodes[len(sortedNodes)-1]

		if mostLoaded.load > leastLoaded.load*1.5 {
			actions = append(actions, RebalanceAction{
				Type:        RebalanceActionMoveLeader,
				SourceNode:  mostLoaded.nodeID,
				TargetNode:  leastLoaded.nodeID,
				Description: fmt.Sprintf("Move leadership from %s to %s", mostLoaded.nodeID, leastLoaded.nodeID),
			})
		}
	}

	return actions
}

// executeAction executes a rebalance action
func (rm *RebalanceManager) executeAction(ctx context.Context, action RebalanceAction) error {
	switch action.Type {
	case RebalanceActionMoveLeader:
		// In a real implementation, this would trigger leadership transfer
		rm.logger.Info("would transfer leadership",
			forge.F("from", action.SourceNode),
			forge.F("to", action.TargetNode),
		)
		return nil

	case RebalanceActionAddReplica:
		rm.logger.Info("would add replica",
			forge.F("to", action.TargetNode),
		)
		return nil

	case RebalanceActionRemoveReplica:
		rm.logger.Info("would remove replica",
			forge.F("from", action.SourceNode),
		)
		return nil

	default:
		return fmt.Errorf("unknown rebalance action type: %d", action.Type)
	}
}

// autoRebalanceLoop runs automatic rebalancing
func (rm *RebalanceManager) autoRebalanceLoop(ctx context.Context) {
	ticker := time.NewTicker(rm.rebalanceCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return

		case <-ticker.C:
			analysis := rm.AnalyzeBalance()

			if !analysis.Balanced {
				rm.logger.Info("cluster imbalance detected, initiating rebalance",
					forge.F("imbalance", analysis.Imbalance),
				)

				if _, err := rm.Rebalance(ctx); err != nil {
					rm.logger.Error("auto-rebalance failed",
						forge.F("error", err),
					)
				}
			}
		}
	}
}

// Enable enables rebalancing
func (rm *RebalanceManager) Enable() {
	rm.enabled = true
	rm.logger.Info("rebalancing enabled")
}

// Disable disables rebalancing
func (rm *RebalanceManager) Disable() {
	rm.enabled = false
	rm.logger.Info("rebalancing disabled")
}

// IsEnabled returns whether rebalancing is enabled
func (rm *RebalanceManager) IsEnabled() bool {
	return rm.enabled
}

// String returns string representation of action type
func (t RebalanceActionType) String() string {
	switch t {
	case RebalanceActionMoveLeader:
		return "move_leader"
	case RebalanceActionAddReplica:
		return "add_replica"
	case RebalanceActionRemoveReplica:
		return "remove_replica"
	case RebalanceActionTransferSnapshot:
		return "transfer_snapshot"
	default:
		return "unknown"
	}
}
