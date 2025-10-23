package discovery

import (
	"context"
	"time"

	"github.com/xraph/forge/v0/pkg/common"
)

// Discovery defines the interface for node discovery
type Discovery interface {
	// GetNodes returns all discovered nodes
	GetNodes(ctx context.Context) ([]NodeInfo, error)

	// GetNode returns information about a specific node
	GetNode(ctx context.Context, nodeID string) (*NodeInfo, error)

	// Register registers a node with the discovery service
	Register(ctx context.Context, node NodeInfo) error

	// Unregister removes a node from the discovery service
	Unregister(ctx context.Context, nodeID string) error

	// UpdateNode updates node information
	UpdateNode(ctx context.Context, node NodeInfo) error

	// WatchNodes watches for node changes
	WatchNodes(ctx context.Context, callback NodeChangeCallback) error

	// HealthCheck performs a health check on the discovery service
	HealthCheck(ctx context.Context) error

	// GetStats returns discovery statistics
	GetStats(ctx context.Context) DiscoveryStats

	// Close closes the discovery service
	Close(ctx context.Context) error
}

// NodeInfo represents information about a node
type NodeInfo struct {
	ID           string                 `json:"id"`
	Address      string                 `json:"address"`
	Port         int                    `json:"port"`
	Role         string                 `json:"role"`
	Status       NodeStatus             `json:"status"`
	Metadata     map[string]interface{} `json:"metadata"`
	Tags         []string               `json:"tags"`
	LastSeen     time.Time              `json:"last_seen"`
	RegisteredAt time.Time              `json:"registered_at"`
	UpdatedAt    time.Time              `json:"updated_at"`
	TTL          time.Duration          `json:"ttl"`
}

// NodeStatus represents the status of a node
type NodeStatus string

const (
	NodeStatusActive      NodeStatus = "active"
	NodeStatusInactive    NodeStatus = "inactive"
	NodeStatusSuspected   NodeStatus = "suspected"
	NodeStatusFailed      NodeStatus = "failed"
	NodeStatusMaintenance NodeStatus = "maintenance"
)

// NodeChangeType represents the type of node change
type NodeChangeType string

const (
	NodeChangeTypeAdded   NodeChangeType = "added"
	NodeChangeTypeUpdated NodeChangeType = "updated"
	NodeChangeTypeRemoved NodeChangeType = "removed"
)

// NodeChangeEvent represents a node change event
type NodeChangeEvent struct {
	Type      NodeChangeType `json:"type"`
	Node      NodeInfo       `json:"node"`
	OldNode   *NodeInfo      `json:"old_node,omitempty"`
	Timestamp time.Time      `json:"timestamp"`
}

// NodeChangeCallback is called when nodes change
type NodeChangeCallback func(event NodeChangeEvent) error

// DiscoveryStats contains statistics about the discovery service
type DiscoveryStats struct {
	TotalNodes     int           `json:"total_nodes"`
	ActiveNodes    int           `json:"active_nodes"`
	InactiveNodes  int           `json:"inactive_nodes"`
	FailedNodes    int           `json:"failed_nodes"`
	LastUpdate     time.Time     `json:"last_update"`
	Uptime         time.Duration `json:"uptime"`
	WatcherCount   int           `json:"watcher_count"`
	OperationCount int64         `json:"operation_count"`
	ErrorCount     int64         `json:"error_count"`
	AverageLatency time.Duration `json:"average_latency"`
}

// DiscoveryConfig contains configuration for discovery services
type DiscoveryConfig struct {
	Type            string                 `json:"type"`
	Endpoints       []string               `json:"endpoints"`
	Nodes           []string               `json:"nodes"`
	Namespace       string                 `json:"namespace"`
	NodeTTL         time.Duration          `json:"node_ttl"`
	RefreshInterval time.Duration          `json:"refresh_interval"`
	Timeout         time.Duration          `json:"timeout"`
	Retries         int                    `json:"retries"`
	Options         map[string]interface{} `json:"options"`
}

// DiscoveryFactory creates discovery services
type DiscoveryFactory interface {
	// Create creates a new discovery service
	Create(config DiscoveryConfig) (Discovery, error)

	// Name returns the factory name
	Name() string

	// Version returns the factory version
	Version() string

	// ValidateConfig validates the configuration
	ValidateConfig(config DiscoveryConfig) error
}

// DiscoveryManager manages multiple discovery services
type DiscoveryManager interface {
	// RegisterFactory registers a discovery factory
	RegisterFactory(factory DiscoveryFactory) error

	// CreateDiscovery creates a discovery service
	CreateDiscovery(config DiscoveryConfig) (Discovery, error)

	// GetDiscovery returns a discovery service by name
	GetDiscovery(name string) (Discovery, error)

	// GetFactories returns all registered factories
	GetFactories() map[string]DiscoveryFactory

	// Close closes all discovery services
	Close(ctx context.Context) error
}

// NodeFilter filters nodes based on criteria
type NodeFilter interface {
	// Filter filters nodes based on criteria
	Filter(nodes []NodeInfo) []NodeInfo

	// Matches checks if a node matches the filter criteria
	Matches(node NodeInfo) bool
}

// NodeSelector selects nodes based on criteria
type NodeSelector interface {
	// Select selects nodes based on criteria
	Select(nodes []NodeInfo) []NodeInfo

	// SelectOne selects a single node based on criteria
	SelectOne(nodes []NodeInfo) (*NodeInfo, error)
}

// DiscoveryError represents a discovery error
type DiscoveryError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	NodeID  string `json:"node_id,omitempty"`
	Cause   error  `json:"cause,omitempty"`
}

func (e *DiscoveryError) Error() string {
	if e.Cause != nil {
		return e.Message + ": " + e.Cause.Error()
	}
	return e.Message
}

func (e *DiscoveryError) Unwrap() error {
	return e.Cause
}

// Common error codes
const (
	ErrCodeNodeNotFound       = "node_not_found"
	ErrCodeNodeExists         = "node_exists"
	ErrCodeInvalidConfig      = "invalid_config"
	ErrCodeConnectionFailed   = "connection_failed"
	ErrCodeTimeout            = "timeout"
	ErrCodeUnauthorized       = "unauthorized"
	ErrCodeServiceUnavailable = "service_unavailable"
)

// NewDiscoveryError creates a new discovery error
func NewDiscoveryError(code, message string) *DiscoveryError {
	return &DiscoveryError{
		Code:    code,
		Message: message,
	}
}

// Common discovery types
const (
	DiscoveryTypeStatic     = "static"
	DiscoveryTypeDNS        = "dns"
	DiscoveryTypeConsul     = "consul"
	DiscoveryTypeEtcd       = "etcd"
	DiscoveryTypeKubernetes = "kubernetes"
	DiscoveryTypeZookeeper  = "zookeeper"
)

// NodeInfoBuilder builds node information
type NodeInfoBuilder struct {
	node NodeInfo
}

// NewNodeInfoBuilder creates a new node info builder
func NewNodeInfoBuilder() *NodeInfoBuilder {
	return &NodeInfoBuilder{
		node: NodeInfo{
			Status:       NodeStatusActive,
			Metadata:     make(map[string]interface{}),
			Tags:         make([]string, 0),
			RegisteredAt: time.Now(),
			UpdatedAt:    time.Now(),
			TTL:          5 * time.Minute,
		},
	}
}

// WithID sets the node ID
func (b *NodeInfoBuilder) WithID(id string) *NodeInfoBuilder {
	b.node.ID = id
	return b
}

// WithAddress sets the node address
func (b *NodeInfoBuilder) WithAddress(address string) *NodeInfoBuilder {
	b.node.Address = address
	return b
}

// WithPort sets the node port
func (b *NodeInfoBuilder) WithPort(port int) *NodeInfoBuilder {
	b.node.Port = port
	return b
}

// WithRole sets the node role
func (b *NodeInfoBuilder) WithRole(role string) *NodeInfoBuilder {
	b.node.Role = role
	return b
}

// WithStatus sets the node status
func (b *NodeInfoBuilder) WithStatus(status NodeStatus) *NodeInfoBuilder {
	b.node.Status = status
	return b
}

// WithMetadata adds metadata
func (b *NodeInfoBuilder) WithMetadata(key string, value interface{}) *NodeInfoBuilder {
	b.node.Metadata[key] = value
	return b
}

// WithTag adds a tag
func (b *NodeInfoBuilder) WithTag(tag string) *NodeInfoBuilder {
	b.node.Tags = append(b.node.Tags, tag)
	return b
}

// WithTags sets all tags
func (b *NodeInfoBuilder) WithTags(tags []string) *NodeInfoBuilder {
	b.node.Tags = make([]string, len(tags))
	copy(b.node.Tags, tags)
	return b
}

// WithTTL sets the TTL
func (b *NodeInfoBuilder) WithTTL(ttl time.Duration) *NodeInfoBuilder {
	b.node.TTL = ttl
	return b
}

// Build builds the node info
func (b *NodeInfoBuilder) Build() NodeInfo {
	return b.node
}

// NodeFilterBuilder builds node filters
type NodeFilterBuilder struct {
	filters []NodeFilter
}

// NewNodeFilterBuilder creates a new node filter builder
func NewNodeFilterBuilder() *NodeFilterBuilder {
	return &NodeFilterBuilder{
		filters: make([]NodeFilter, 0),
	}
}

// WithStatus filters by status
func (b *NodeFilterBuilder) WithStatus(status NodeStatus) *NodeFilterBuilder {
	b.filters = append(b.filters, &StatusFilter{Status: status})
	return b
}

// WithRole filters by role
func (b *NodeFilterBuilder) WithRole(role string) *NodeFilterBuilder {
	b.filters = append(b.filters, &RoleFilter{Role: role})
	return b
}

// WithTag filters by tag
func (b *NodeFilterBuilder) WithTag(tag string) *NodeFilterBuilder {
	b.filters = append(b.filters, &TagFilter{Tag: tag})
	return b
}

// WithCustomFilter adds a custom filter
func (b *NodeFilterBuilder) WithCustomFilter(filter NodeFilter) *NodeFilterBuilder {
	b.filters = append(b.filters, filter)
	return b
}

// Build builds the composite filter
func (b *NodeFilterBuilder) Build() NodeFilter {
	return &CompositeFilter{Filters: b.filters}
}

// Built-in filters

// StatusFilter filters nodes by status
type StatusFilter struct {
	Status NodeStatus
}

// Filter filters nodes by status
func (f *StatusFilter) Filter(nodes []NodeInfo) []NodeInfo {
	var filtered []NodeInfo
	for _, node := range nodes {
		if f.Matches(node) {
			filtered = append(filtered, node)
		}
	}
	return filtered
}

// Matches checks if a node matches the status
func (f *StatusFilter) Matches(node NodeInfo) bool {
	return node.Status == f.Status
}

// RoleFilter filters nodes by role
type RoleFilter struct {
	Role string
}

// Filter filters nodes by role
func (f *RoleFilter) Filter(nodes []NodeInfo) []NodeInfo {
	var filtered []NodeInfo
	for _, node := range nodes {
		if f.Matches(node) {
			filtered = append(filtered, node)
		}
	}
	return filtered
}

// Matches checks if a node matches the role
func (f *RoleFilter) Matches(node NodeInfo) bool {
	return node.Role == f.Role
}

// TagFilter filters nodes by tag
type TagFilter struct {
	Tag string
}

// Filter filters nodes by tag
func (f *TagFilter) Filter(nodes []NodeInfo) []NodeInfo {
	var filtered []NodeInfo
	for _, node := range nodes {
		if f.Matches(node) {
			filtered = append(filtered, node)
		}
	}
	return filtered
}

// Matches checks if a node has the tag
func (f *TagFilter) Matches(node NodeInfo) bool {
	for _, tag := range node.Tags {
		if tag == f.Tag {
			return true
		}
	}
	return false
}

// CompositeFilter combines multiple filters
type CompositeFilter struct {
	Filters []NodeFilter
}

// Filter filters nodes using all filters
func (f *CompositeFilter) Filter(nodes []NodeInfo) []NodeInfo {
	filtered := nodes
	for _, filter := range f.Filters {
		filtered = filter.Filter(filtered)
	}
	return filtered
}

// Matches checks if a node matches all filters
func (f *CompositeFilter) Matches(node NodeInfo) bool {
	for _, filter := range f.Filters {
		if !filter.Matches(node) {
			return false
		}
	}
	return true
}

// Built-in selectors

// RandomSelector selects nodes randomly
type RandomSelector struct {
	Count int
}

// Select selects nodes randomly
func (s *RandomSelector) Select(nodes []NodeInfo) []NodeInfo {
	if len(nodes) <= s.Count {
		return nodes
	}

	// Simple random selection (in practice, you'd use crypto/rand)
	selected := make([]NodeInfo, s.Count)
	for i := 0; i < s.Count; i++ {
		selected[i] = nodes[i]
	}
	return selected
}

// SelectOne selects a single node randomly
func (s *RandomSelector) SelectOne(nodes []NodeInfo) (*NodeInfo, error) {
	if len(nodes) == 0 {
		return nil, NewDiscoveryError(ErrCodeNodeNotFound, "no nodes available")
	}
	return &nodes[0], nil
}

// RoundRobinSelector selects nodes in round-robin fashion
type RoundRobinSelector struct {
	index int
}

// Select selects nodes in round-robin fashion
func (s *RoundRobinSelector) Select(nodes []NodeInfo) []NodeInfo {
	if len(nodes) == 0 {
		return nodes
	}

	// Simple round-robin selection
	selected := make([]NodeInfo, len(nodes))
	for i := 0; i < len(nodes); i++ {
		selected[i] = nodes[(s.index+i)%len(nodes)]
	}
	s.index = (s.index + 1) % len(nodes)
	return selected
}

// SelectOne selects a single node in round-robin fashion
func (s *RoundRobinSelector) SelectOne(nodes []NodeInfo) (*NodeInfo, error) {
	if len(nodes) == 0 {
		return nil, NewDiscoveryError(ErrCodeNodeNotFound, "no nodes available")
	}

	node := &nodes[s.index%len(nodes)]
	s.index = (s.index + 1) % len(nodes)
	return node, nil
}

// DiscoveryService provides discovery functionality
type DiscoveryService struct {
	discovery Discovery
	filters   []NodeFilter
	selector  NodeSelector
	logger    common.Logger
	metrics   common.Metrics
}

// NewDiscoveryService creates a new discovery service
func NewDiscoveryService(discovery Discovery, logger common.Logger, metrics common.Metrics) *DiscoveryService {
	return &DiscoveryService{
		discovery: discovery,
		filters:   make([]NodeFilter, 0),
		selector:  &RandomSelector{Count: 1},
		logger:    logger,
		metrics:   metrics,
	}
}

// WithFilter adds a filter
func (ds *DiscoveryService) WithFilter(filter NodeFilter) *DiscoveryService {
	ds.filters = append(ds.filters, filter)
	return ds
}

// WithSelector sets the selector
func (ds *DiscoveryService) WithSelector(selector NodeSelector) *DiscoveryService {
	ds.selector = selector
	return ds
}

// GetFilteredNodes returns filtered nodes
func (ds *DiscoveryService) GetFilteredNodes(ctx context.Context) ([]NodeInfo, error) {
	nodes, err := ds.discovery.GetNodes(ctx)
	if err != nil {
		return nil, err
	}

	// Apply filters
	filtered := nodes
	for _, filter := range ds.filters {
		filtered = filter.Filter(filtered)
	}

	return filtered, nil
}

// SelectNodes selects nodes based on criteria
func (ds *DiscoveryService) SelectNodes(ctx context.Context) ([]NodeInfo, error) {
	filtered, err := ds.GetFilteredNodes(ctx)
	if err != nil {
		return nil, err
	}

	return ds.selector.Select(filtered), nil
}

// SelectNode selects a single node based on criteria
func (ds *DiscoveryService) SelectNode(ctx context.Context) (*NodeInfo, error) {
	filtered, err := ds.GetFilteredNodes(ctx)
	if err != nil {
		return nil, err
	}

	return ds.selector.SelectOne(filtered)
}
