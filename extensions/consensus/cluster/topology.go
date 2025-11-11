package cluster

import (
	"fmt"
	"slices"
	"sort"
	"sync"

	"github.com/xraph/forge"
	"github.com/xraph/forge/errors"
	"github.com/xraph/forge/extensions/consensus/internal"
)

// TopologyManager manages cluster topology and node relationships.
type TopologyManager struct {
	manager *Manager
	logger  forge.Logger

	// Topology state
	regions map[string][]string // region -> node IDs
	zones   map[string][]string // zone -> node IDs
	racks   map[string][]string // rack -> node IDs
	mu      sync.RWMutex
}

// NodeLocation represents a node's physical/logical location.
type NodeLocation struct {
	NodeID string
	Region string
	Zone   string
	Rack   string
}

// TopologyView represents a view of the cluster topology.
type TopologyView struct {
	TotalNodes  int
	RegionCount int
	ZoneCount   int
	RackCount   int
	Regions     map[string]RegionInfo
	Zones       map[string]ZoneInfo
	Racks       map[string]RackInfo
}

// RegionInfo contains information about a region.
type RegionInfo struct {
	Name      string
	NodeCount int
	ZoneCount int
	Nodes     []string
	Healthy   int
	Unhealthy int
}

// ZoneInfo contains information about a zone.
type ZoneInfo struct {
	Name      string
	Region    string
	NodeCount int
	RackCount int
	Nodes     []string
	Healthy   int
	Unhealthy int
}

// RackInfo contains information about a rack.
type RackInfo struct {
	Name      string
	Zone      string
	NodeCount int
	Nodes     []string
	Healthy   int
	Unhealthy int
}

// NewTopologyManager creates a new topology manager.
func NewTopologyManager(manager *Manager, logger forge.Logger) *TopologyManager {
	return &TopologyManager{
		manager: manager,
		logger:  logger,
		regions: make(map[string][]string),
		zones:   make(map[string][]string),
		racks:   make(map[string][]string),
	}
}

// RegisterNodeLocation registers a node's location.
func (tm *TopologyManager) RegisterNodeLocation(location NodeLocation) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	// Validate location
	if location.NodeID == "" {
		return errors.New("node ID is required")
	}

	// Register in region
	if location.Region != "" {
		if !contains(tm.regions[location.Region], location.NodeID) {
			tm.regions[location.Region] = append(tm.regions[location.Region], location.NodeID)
		}
	}

	// Register in zone
	if location.Zone != "" {
		if !contains(tm.zones[location.Zone], location.NodeID) {
			tm.zones[location.Zone] = append(tm.zones[location.Zone], location.NodeID)
		}
	}

	// Register in rack
	if location.Rack != "" {
		if !contains(tm.racks[location.Rack], location.NodeID) {
			tm.racks[location.Rack] = append(tm.racks[location.Rack], location.NodeID)
		}
	}

	tm.logger.Info("registered node location",
		forge.F("node_id", location.NodeID),
		forge.F("region", location.Region),
		forge.F("zone", location.Zone),
		forge.F("rack", location.Rack),
	)

	return nil
}

// UnregisterNode removes a node from topology.
func (tm *TopologyManager) UnregisterNode(nodeID string) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	// Remove from all regions
	for region := range tm.regions {
		tm.regions[region] = removeString(tm.regions[region], nodeID)
		if len(tm.regions[region]) == 0 {
			delete(tm.regions, region)
		}
	}

	// Remove from all zones
	for zone := range tm.zones {
		tm.zones[zone] = removeString(tm.zones[zone], nodeID)
		if len(tm.zones[zone]) == 0 {
			delete(tm.zones, zone)
		}
	}

	// Remove from all racks
	for rack := range tm.racks {
		tm.racks[rack] = removeString(tm.racks[rack], nodeID)
		if len(tm.racks[rack]) == 0 {
			delete(tm.racks, rack)
		}
	}

	tm.logger.Info("unregistered node from topology",
		forge.F("node_id", nodeID),
	)
}

// GetTopologyView returns a complete view of the cluster topology.
func (tm *TopologyManager) GetTopologyView() TopologyView {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	view := TopologyView{
		Regions: make(map[string]RegionInfo),
		Zones:   make(map[string]ZoneInfo),
		Racks:   make(map[string]RackInfo),
	}

	allNodes := tm.manager.GetNodes()

	nodeHealthMap := make(map[string]bool)
	for _, node := range allNodes {
		nodeHealthMap[node.ID] = node.Status == internal.StatusActive
	}

	view.TotalNodes = len(allNodes)

	// Build region info
	for region, nodes := range tm.regions {
		healthy, unhealthy := 0, 0

		for _, nodeID := range nodes {
			if nodeHealthMap[nodeID] {
				healthy++
			} else {
				unhealthy++
			}
		}

		view.Regions[region] = RegionInfo{
			Name:      region,
			NodeCount: len(nodes),
			Nodes:     append([]string{}, nodes...),
			Healthy:   healthy,
			Unhealthy: unhealthy,
		}
	}

	view.RegionCount = len(view.Regions)

	// Build zone info
	for zone, nodes := range tm.zones {
		healthy, unhealthy := 0, 0

		for _, nodeID := range nodes {
			if nodeHealthMap[nodeID] {
				healthy++
			} else {
				unhealthy++
			}
		}

		view.Zones[zone] = ZoneInfo{
			Name:      zone,
			NodeCount: len(nodes),
			Nodes:     append([]string{}, nodes...),
			Healthy:   healthy,
			Unhealthy: unhealthy,
		}
	}

	view.ZoneCount = len(view.Zones)

	// Build rack info
	for rack, nodes := range tm.racks {
		healthy, unhealthy := 0, 0

		for _, nodeID := range nodes {
			if nodeHealthMap[nodeID] {
				healthy++
			} else {
				unhealthy++
			}
		}

		view.Racks[rack] = RackInfo{
			Name:      rack,
			NodeCount: len(nodes),
			Nodes:     append([]string{}, nodes...),
			Healthy:   healthy,
			Unhealthy: unhealthy,
		}
	}

	view.RackCount = len(view.Racks)

	return view
}

// GetNodesInRegion returns all nodes in a region.
func (tm *TopologyManager) GetNodesInRegion(region string) []string {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	nodes := tm.regions[region]
	result := make([]string, len(nodes))
	copy(result, nodes)

	return result
}

// GetNodesInZone returns all nodes in a zone.
func (tm *TopologyManager) GetNodesInZone(zone string) []string {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	nodes := tm.zones[zone]
	result := make([]string, len(nodes))
	copy(result, nodes)

	return result
}

// GetNodesInRack returns all nodes in a rack.
func (tm *TopologyManager) GetNodesInRack(rack string) []string {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	nodes := tm.racks[rack]
	result := make([]string, len(nodes))
	copy(result, nodes)

	return result
}

// IsTopologyAware returns true if topology information is being tracked.
func (tm *TopologyManager) IsTopologyAware() bool {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	return len(tm.regions) > 0 || len(tm.zones) > 0 || len(tm.racks) > 0
}

// GetTopologyDistribution returns distribution statistics.
func (tm *TopologyManager) GetTopologyDistribution() map[string]any {
	view := tm.GetTopologyView()

	// Calculate distribution
	regionSizes := make([]int, 0, len(view.Regions))
	for _, region := range view.Regions {
		regionSizes = append(regionSizes, region.NodeCount)
	}

	sort.Ints(regionSizes)

	zoneSizes := make([]int, 0, len(view.Zones))
	for _, zone := range view.Zones {
		zoneSizes = append(zoneSizes, zone.NodeCount)
	}

	sort.Ints(zoneSizes)

	rackSizes := make([]int, 0, len(view.Racks))
	for _, rack := range view.Racks {
		rackSizes = append(rackSizes, rack.NodeCount)
	}

	sort.Ints(rackSizes)

	return map[string]any{
		"total_nodes":         view.TotalNodes,
		"region_count":        view.RegionCount,
		"zone_count":          view.ZoneCount,
		"rack_count":          view.RackCount,
		"region_distribution": regionSizes,
		"zone_distribution":   zoneSizes,
		"rack_distribution":   rackSizes,
		"topology_aware":      tm.IsTopologyAware(),
	}
}

// ValidateTopology validates cluster topology for fault tolerance.
func (tm *TopologyManager) ValidateTopology() []string {
	var warnings []string

	view := tm.GetTopologyView()

	// Check if we have topology information
	if !tm.IsTopologyAware() {
		warnings = append(warnings, "No topology information configured - all nodes considered in same location")

		return warnings
	}

	// Check region distribution
	if view.RegionCount == 1 {
		warnings = append(warnings, "All nodes in single region - no regional fault tolerance")
	}

	// Check for unbalanced regions
	if view.RegionCount > 1 {
		var maxNodes, minNodes int
		for _, region := range view.Regions {
			if maxNodes == 0 || region.NodeCount > maxNodes {
				maxNodes = region.NodeCount
			}

			if minNodes == 0 || region.NodeCount < minNodes {
				minNodes = region.NodeCount
			}
		}

		if maxNodes > minNodes*2 {
			warnings = append(warnings, fmt.Sprintf(
				"Unbalanced region distribution (max: %d, min: %d)", maxNodes, minNodes))
		}
	}

	// Check zone distribution
	if view.ZoneCount < 3 {
		warnings = append(warnings, "Less than 3 zones - limited zone-level fault tolerance")
	}

	return warnings
}

// Helper functions.
func contains(slice []string, item string) bool {
	return slices.Contains(slice, item)
}

func removeString(slice []string, item string) []string {
	result := make([]string, 0, len(slice))
	for _, s := range slice {
		if s != item {
			result = append(result, s)
		}
	}

	return result
}
