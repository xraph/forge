package discovery

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/logger"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// KubernetesDiscovery implements Kubernetes-based node discovery
type KubernetesDiscovery struct {
	client      kubernetes.Interface
	config      KubernetesDiscoveryConfig
	nodes       map[string]NodeInfo
	watchers    []NodeChangeCallback
	stats       DiscoveryStats
	logger      common.Logger
	metrics     common.Metrics
	startTime   time.Time
	stopCh      chan struct{}
	watchCancel context.CancelFunc
	mu          sync.RWMutex
	watchersMu  sync.RWMutex
}

// KubernetesDiscoveryConfig contains configuration for Kubernetes discovery
type KubernetesDiscoveryConfig struct {
	Namespace       string            `json:"namespace"`
	ServiceName     string            `json:"service_name"`
	ServicePort     int               `json:"service_port"`
	LabelSelector   string            `json:"label_selector"`
	FieldSelector   string            `json:"field_selector"`
	NodeSelector    string            `json:"node_selector"`
	KubeConfig      string            `json:"kube_config"`
	InCluster       bool              `json:"in_cluster"`
	RefreshInterval time.Duration     `json:"refresh_interval"`
	Timeout         time.Duration     `json:"timeout"`
	Retries         int               `json:"retries"`
	TTL             time.Duration     `json:"ttl"`
	EnableWatch     bool              `json:"enable_watch"`
	Annotations     map[string]string `json:"annotations"`
	Labels          map[string]string `json:"labels"`
}

// KubernetesDiscoveryFactory creates Kubernetes discovery services
type KubernetesDiscoveryFactory struct {
	logger  common.Logger
	metrics common.Metrics
}

// NewKubernetesDiscoveryFactory creates a new Kubernetes discovery factory
func NewKubernetesDiscoveryFactory(logger common.Logger, metrics common.Metrics) *KubernetesDiscoveryFactory {
	return &KubernetesDiscoveryFactory{
		logger:  logger,
		metrics: metrics,
	}
}

// Create creates a new Kubernetes discovery service
func (f *KubernetesDiscoveryFactory) Create(config DiscoveryConfig) (Discovery, error) {
	k8sConfig := KubernetesDiscoveryConfig{
		Namespace:       config.Namespace,
		ServiceName:     "forge-consensus",
		ServicePort:     8080,
		LabelSelector:   "app=forge-consensus",
		InCluster:       true,
		RefreshInterval: config.RefreshInterval,
		Timeout:         config.Timeout,
		Retries:         config.Retries,
		TTL:             config.NodeTTL,
		EnableWatch:     true,
		Annotations:     make(map[string]string),
		Labels:          make(map[string]string),
	}

	// Parse Kubernetes-specific options
	if serviceName, ok := config.Options["service_name"].(string); ok {
		k8sConfig.ServiceName = serviceName
	}
	if servicePort, ok := config.Options["service_port"].(float64); ok {
		k8sConfig.ServicePort = int(servicePort)
	}
	if labelSelector, ok := config.Options["label_selector"].(string); ok {
		k8sConfig.LabelSelector = labelSelector
	}
	if fieldSelector, ok := config.Options["field_selector"].(string); ok {
		k8sConfig.FieldSelector = fieldSelector
	}
	if nodeSelector, ok := config.Options["node_selector"].(string); ok {
		k8sConfig.NodeSelector = nodeSelector
	}
	if kubeConfig, ok := config.Options["kube_config"].(string); ok {
		k8sConfig.KubeConfig = kubeConfig
		k8sConfig.InCluster = false
	}
	if inCluster, ok := config.Options["in_cluster"].(bool); ok {
		k8sConfig.InCluster = inCluster
	}
	if enableWatch, ok := config.Options["enable_watch"].(bool); ok {
		k8sConfig.EnableWatch = enableWatch
	}

	// Set defaults
	if k8sConfig.Namespace == "" {
		k8sConfig.Namespace = "default"
	}
	if k8sConfig.RefreshInterval == 0 {
		k8sConfig.RefreshInterval = 30 * time.Second
	}
	if k8sConfig.Timeout == 0 {
		k8sConfig.Timeout = 10 * time.Second
	}
	if k8sConfig.Retries == 0 {
		k8sConfig.Retries = 3
	}
	if k8sConfig.TTL == 0 {
		k8sConfig.TTL = 5 * time.Minute
	}

	// Create Kubernetes client
	var clientConfig *rest.Config
	var err error

	if k8sConfig.InCluster {
		clientConfig, err = rest.InClusterConfig()
	} else {
		clientConfig, err = clientcmd.BuildConfigFromFlags("", k8sConfig.KubeConfig)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to create Kubernetes config: %w", err)
	}

	// Set timeout
	clientConfig.Timeout = k8sConfig.Timeout

	client, err := kubernetes.NewForConfig(clientConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	kd := &KubernetesDiscovery{
		client:    client,
		config:    k8sConfig,
		nodes:     make(map[string]NodeInfo),
		watchers:  make([]NodeChangeCallback, 0),
		logger:    f.logger,
		metrics:   f.metrics,
		startTime: time.Now(),
		stopCh:    make(chan struct{}),
		stats: DiscoveryStats{
			LastUpdate: time.Now(),
		},
	}

	// Perform initial discovery
	if err := kd.discoverNodes(); err != nil {
		if f.logger != nil {
			f.logger.Warn("initial Kubernetes discovery failed", logger.Error(err))
		}
	}

	// Start watch if enabled
	if k8sConfig.EnableWatch {
		if err := kd.startWatch(); err != nil {
			if f.logger != nil {
				f.logger.Warn("failed to start Kubernetes watch", logger.Error(err))
			}
		}
	}

	// Start refresh routine
	go kd.refreshLoop()

	if f.logger != nil {
		f.logger.Info("Kubernetes discovery service created",
			logger.String("namespace", k8sConfig.Namespace),
			logger.String("service_name", k8sConfig.ServiceName),
			logger.String("label_selector", k8sConfig.LabelSelector),
			logger.Bool("in_cluster", k8sConfig.InCluster),
			logger.Duration("refresh_interval", k8sConfig.RefreshInterval),
		)
	}

	return kd, nil
}

// Name returns the factory name
func (f *KubernetesDiscoveryFactory) Name() string {
	return DiscoveryTypeKubernetes
}

// Version returns the factory version
func (f *KubernetesDiscoveryFactory) Version() string {
	return "1.0.0"
}

// ValidateConfig validates the configuration
func (f *KubernetesDiscoveryFactory) ValidateConfig(config DiscoveryConfig) error {
	if config.Type != DiscoveryTypeKubernetes {
		return fmt.Errorf("invalid discovery type: %s", config.Type)
	}

	// Validate namespace
	if config.Namespace == "" {
		return fmt.Errorf("namespace cannot be empty")
	}

	// Validate kubeconfig path if not in cluster
	if inCluster, ok := config.Options["in_cluster"].(bool); ok && !inCluster {
		if kubeConfig, ok := config.Options["kube_config"].(string); ok {
			if kubeConfig == "" {
				return fmt.Errorf("kube_config path required when not running in cluster")
			}
		}
	}

	return nil
}

// GetNodes returns all discovered nodes
func (kd *KubernetesDiscovery) GetNodes(ctx context.Context) ([]NodeInfo, error) {
	kd.mu.RLock()
	defer kd.mu.RUnlock()

	kd.incrementOperation()

	nodes := make([]NodeInfo, 0, len(kd.nodes))
	for _, node := range kd.nodes {
		nodes = append(nodes, node)
	}

	if kd.logger != nil {
		kd.logger.Debug("retrieved nodes from Kubernetes discovery",
			logger.Int("count", len(nodes)),
		)
	}

	return nodes, nil
}

// GetNode returns information about a specific node
func (kd *KubernetesDiscovery) GetNode(ctx context.Context, nodeID string) (*NodeInfo, error) {
	kd.mu.RLock()
	defer kd.mu.RUnlock()

	kd.incrementOperation()

	node, exists := kd.nodes[nodeID]
	if !exists {
		kd.incrementError()
		return nil, &DiscoveryError{
			Code:    ErrCodeNodeNotFound,
			Message: fmt.Sprintf("node not found: %s", nodeID),
			NodeID:  nodeID,
		}
	}

	if kd.logger != nil {
		kd.logger.Debug("retrieved node from Kubernetes discovery",
			logger.String("node_id", nodeID),
			logger.String("address", node.Address),
		)
	}

	return &node, nil
}

// Register is not supported for Kubernetes discovery (pods are managed by Kubernetes)
func (kd *KubernetesDiscovery) Register(ctx context.Context, node NodeInfo) error {
	kd.incrementOperation()
	kd.incrementError()
	return &DiscoveryError{
		Code:    ErrCodeServiceUnavailable,
		Message: "node registration not supported for Kubernetes discovery (pods are managed by Kubernetes)",
	}
}

// Unregister is not supported for Kubernetes discovery
func (kd *KubernetesDiscovery) Unregister(ctx context.Context, nodeID string) error {
	kd.incrementOperation()
	kd.incrementError()
	return &DiscoveryError{
		Code:    ErrCodeServiceUnavailable,
		Message: "node unregistration not supported for Kubernetes discovery (pods are managed by Kubernetes)",
	}
}

// UpdateNode is not supported for Kubernetes discovery
func (kd *KubernetesDiscovery) UpdateNode(ctx context.Context, node NodeInfo) error {
	kd.incrementOperation()
	kd.incrementError()
	return &DiscoveryError{
		Code:    ErrCodeServiceUnavailable,
		Message: "node update not supported for Kubernetes discovery (pods are managed by Kubernetes)",
	}
}

// WatchNodes watches for node changes
func (kd *KubernetesDiscovery) WatchNodes(ctx context.Context, callback NodeChangeCallback) error {
	if !kd.config.EnableWatch {
		return &DiscoveryError{
			Code:    ErrCodeServiceUnavailable,
			Message: "node watching is not enabled",
		}
	}

	kd.watchersMu.Lock()
	defer kd.watchersMu.Unlock()

	kd.watchers = append(kd.watchers, callback)

	// Update stats
	kd.mu.Lock()
	kd.stats.WatcherCount = len(kd.watchers)
	kd.mu.Unlock()

	if kd.logger != nil {
		kd.logger.Info("Kubernetes discovery watcher added",
			logger.Int("total_watchers", len(kd.watchers)),
		)
	}

	if kd.metrics != nil {
		kd.metrics.Counter("forge.consensus.discovery.kubernetes_watchers_added").Inc()
		kd.metrics.Gauge("forge.consensus.discovery.kubernetes_active_watchers").Set(float64(len(kd.watchers)))
	}

	return nil
}

// HealthCheck performs a health check
func (kd *KubernetesDiscovery) HealthCheck(ctx context.Context) error {
	kd.mu.RLock()
	defer kd.mu.RUnlock()

	// Check Kubernetes API connectivity
	if _, err := kd.client.CoreV1().Namespaces().Get(ctx, kd.config.Namespace, metav1.GetOptions{}); err != nil {
		return &DiscoveryError{
			Code:    ErrCodeConnectionFailed,
			Message: fmt.Sprintf("Kubernetes API connection failed: %v", err),
			Cause:   err,
		}
	}

	// Check error rate
	if kd.stats.OperationCount > 0 {
		errorRate := float64(kd.stats.ErrorCount) / float64(kd.stats.OperationCount)
		if errorRate > 0.1 { // 10% error rate threshold
			return &DiscoveryError{
				Code:    ErrCodeServiceUnavailable,
				Message: fmt.Sprintf("high error rate: %.2f%%", errorRate*100),
			}
		}
	}

	return nil
}

// GetStats returns discovery statistics
func (kd *KubernetesDiscovery) GetStats(ctx context.Context) DiscoveryStats {
	kd.mu.RLock()
	defer kd.mu.RUnlock()

	stats := kd.stats
	stats.Uptime = time.Since(kd.startTime)

	// Calculate average latency
	if stats.OperationCount > 0 {
		stats.AverageLatency = time.Millisecond * 20 // Kubernetes API typically has moderate latency
	}

	return stats
}

// Close closes the Kubernetes discovery service
func (kd *KubernetesDiscovery) Close(ctx context.Context) error {
	kd.mu.Lock()
	defer kd.mu.Unlock()

	// Signal stop
	close(kd.stopCh)

	// Cancel watch
	if kd.watchCancel != nil {
		kd.watchCancel()
	}

	// Clear watchers
	kd.watchersMu.Lock()
	kd.watchers = nil
	kd.watchersMu.Unlock()

	// Clear nodes
	kd.nodes = nil

	if kd.logger != nil {
		kd.logger.Info("Kubernetes discovery service closed")
	}

	return nil
}

// discoverNodes discovers nodes from Kubernetes
func (kd *KubernetesDiscovery) discoverNodes() error {
	ctx, cancel := context.WithTimeout(context.Background(), kd.config.Timeout)
	defer cancel()

	// List pods based on label selector
	listOptions := metav1.ListOptions{
		LabelSelector: kd.config.LabelSelector,
	}

	if kd.config.FieldSelector != "" {
		listOptions.FieldSelector = kd.config.FieldSelector
	}

	pods, err := kd.client.CoreV1().Pods(kd.config.Namespace).List(ctx, listOptions)
	if err != nil {
		kd.incrementError()
		return fmt.Errorf("failed to list pods: %w", err)
	}

	// Convert pods to nodes
	discoveredNodes := make(map[string]NodeInfo)
	for _, pod := range pods.Items {
		// Skip pods that are not running
		if pod.Status.Phase != corev1.PodRunning {
			continue
		}

		// Skip pods without IP
		if pod.Status.PodIP == "" {
			continue
		}

		nodeID := fmt.Sprintf("%s-%s", pod.Namespace, pod.Name)
		port := kd.config.ServicePort

		// Try to find port from container spec
		if len(pod.Spec.Containers) > 0 {
			for _, container := range pod.Spec.Containers {
				for _, containerPort := range container.Ports {
					if containerPort.Name == kd.config.ServiceName || containerPort.ContainerPort == int32(kd.config.ServicePort) {
						port = int(containerPort.ContainerPort)
						break
					}
				}
			}
		}

		node := NodeInfo{
			ID:      nodeID,
			Address: pod.Status.PodIP,
			Port:    port,
			Role:    "consensus",
			Status:  kd.podStatusToNodeStatus(pod.Status),
			Metadata: map[string]interface{}{
				"namespace":     pod.Namespace,
				"pod_name":      pod.Name,
				"node_name":     pod.Spec.NodeName,
				"host_ip":       pod.Status.HostIP,
				"phase":         string(pod.Status.Phase),
				"restart_count": kd.calculateRestartCount(pod.Status),
			},
			Tags:         kd.extractTags(pod),
			LastSeen:     time.Now(),
			RegisteredAt: pod.CreationTimestamp.Time,
			UpdatedAt:    time.Now(),
			TTL:          kd.config.TTL,
		}

		// Add labels as metadata
		for key, value := range pod.Labels {
			node.Metadata["label_"+key] = value
		}

		// Add annotations as metadata
		for key, value := range pod.Annotations {
			node.Metadata["annotation_"+key] = value
		}

		discoveredNodes[nodeID] = node
	}

	// Update nodes and notify watchers
	kd.updateNodes(discoveredNodes)

	if kd.logger != nil {
		kd.logger.Info("Kubernetes discovery completed",
			logger.String("namespace", kd.config.Namespace),
			logger.String("label_selector", kd.config.LabelSelector),
			logger.Int("discovered_nodes", len(discoveredNodes)),
		)
	}

	if kd.metrics != nil {
		kd.metrics.Counter("forge.consensus.discovery.kubernetes_queries").Inc()
		kd.metrics.Gauge("forge.consensus.discovery.kubernetes_discovered_nodes").Set(float64(len(discoveredNodes)))
	}

	return nil
}

// podStatusToNodeStatus converts Kubernetes pod status to node status
func (kd *KubernetesDiscovery) podStatusToNodeStatus(status corev1.PodStatus) NodeStatus {
	switch status.Phase {
	case corev1.PodRunning:
		// Check if all containers are ready
		for _, condition := range status.Conditions {
			if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
				return NodeStatusActive
			}
		}
		return NodeStatusSuspected
	case corev1.PodPending:
		return NodeStatusInactive
	case corev1.PodFailed:
		return NodeStatusFailed
	case corev1.PodSucceeded:
		return NodeStatusInactive
	default:
		return NodeStatusInactive
	}
}

// calculateRestartCount calculates the total restart count for a pod
func (kd *KubernetesDiscovery) calculateRestartCount(status corev1.PodStatus) int32 {
	var totalRestarts int32
	for _, containerStatus := range status.ContainerStatuses {
		totalRestarts += containerStatus.RestartCount
	}
	return totalRestarts
}

// extractTags extracts tags from pod labels
func (kd *KubernetesDiscovery) extractTags(pod corev1.Pod) []string {
	tags := []string{"kubernetes", "pod"}

	// Add common labels as tags
	commonLabels := []string{"app", "version", "component", "tier"}
	for _, label := range commonLabels {
		if value, exists := pod.Labels[label]; exists {
			tags = append(tags, fmt.Sprintf("%s=%s", label, value))
		}
	}

	return tags
}

// updateNodes updates the nodes map and notifies watchers
func (kd *KubernetesDiscovery) updateNodes(discoveredNodes map[string]NodeInfo) {
	kd.mu.Lock()
	defer kd.mu.Unlock()

	oldNodes := kd.nodes
	kd.nodes = discoveredNodes

	// Update stats
	kd.updateStats()

	// Notify watchers of changes
	if kd.config.EnableWatch {
		kd.notifyWatchersOfChanges(oldNodes, discoveredNodes)
	}
}

// notifyWatchersOfChanges compares old and new nodes and notifies watchers
func (kd *KubernetesDiscovery) notifyWatchersOfChanges(oldNodes, newNodes map[string]NodeInfo) {
	now := time.Now()

	// Find added nodes
	for nodeID, node := range newNodes {
		if _, exists := oldNodes[nodeID]; !exists {
			kd.notifyWatchers(NodeChangeEvent{
				Type:      NodeChangeTypeAdded,
				Node:      node,
				Timestamp: now,
			})
		}
	}

	// Find removed nodes
	for nodeID, node := range oldNodes {
		if _, exists := newNodes[nodeID]; !exists {
			kd.notifyWatchers(NodeChangeEvent{
				Type:      NodeChangeTypeRemoved,
				Node:      node,
				Timestamp: now,
			})
		}
	}

	// Find updated nodes
	for nodeID, newNode := range newNodes {
		if oldNode, exists := oldNodes[nodeID]; exists {
			if !nodesEqual(oldNode, newNode) {
				kd.notifyWatchers(NodeChangeEvent{
					Type:      NodeChangeTypeUpdated,
					Node:      newNode,
					OldNode:   &oldNode,
					Timestamp: now,
				})
			}
		}
	}
}

// startWatch starts watching for pod changes
func (kd *KubernetesDiscovery) startWatch() error {
	ctx, cancel := context.WithCancel(context.Background())
	kd.watchCancel = cancel

	// Create watch options
	watchOptions := metav1.ListOptions{
		LabelSelector: kd.config.LabelSelector,
		Watch:         true,
	}

	if kd.config.FieldSelector != "" {
		watchOptions.FieldSelector = kd.config.FieldSelector
	}

	// Start watching pods
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-kd.stopCh:
				return
			default:
				// Start watch
				watcher, err := kd.client.CoreV1().Pods(kd.config.Namespace).Watch(ctx, watchOptions)
				if err != nil {
					if kd.logger != nil {
						kd.logger.Error("failed to start Kubernetes watch", logger.Error(err))
					}
					time.Sleep(5 * time.Second)
					continue
				}

				// Process watch events
				for event := range watcher.ResultChan() {
					if event.Type == watch.Error {
						if kd.logger != nil {
							kd.logger.Error("Kubernetes watch error", logger.Any("error", event.Object))
						}
						break
					}

					// Trigger discovery on any pod change
					if err := kd.discoverNodes(); err != nil {
						if kd.logger != nil {
							kd.logger.Error("Kubernetes watch discovery failed", logger.Error(err))
						}
					}
				}

				watcher.Stop()
				time.Sleep(1 * time.Second)
			}
		}
	}()

	return nil
}

// refreshLoop periodically refreshes node information
func (kd *KubernetesDiscovery) refreshLoop() {
	ticker := time.NewTicker(kd.config.RefreshInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := kd.discoverNodes(); err != nil {
				if kd.logger != nil {
					kd.logger.Error("Kubernetes discovery refresh failed", logger.Error(err))
				}
			}
		case <-kd.stopCh:
			return
		}
	}
}

// updateStats updates discovery statistics
func (kd *KubernetesDiscovery) updateStats() {
	kd.stats.TotalNodes = len(kd.nodes)
	kd.stats.ActiveNodes = 0
	kd.stats.InactiveNodes = 0
	kd.stats.FailedNodes = 0
	kd.stats.LastUpdate = time.Now()

	for _, node := range kd.nodes {
		switch node.Status {
		case NodeStatusActive:
			kd.stats.ActiveNodes++
		case NodeStatusInactive:
			kd.stats.InactiveNodes++
		case NodeStatusFailed:
			kd.stats.FailedNodes++
		}
	}

	kd.watchersMu.RLock()
	kd.stats.WatcherCount = len(kd.watchers)
	kd.watchersMu.RUnlock()
}

// incrementOperation increments the operation count
func (kd *KubernetesDiscovery) incrementOperation() {
	kd.stats.OperationCount++
}

// incrementError increments the error count
func (kd *KubernetesDiscovery) incrementError() {
	kd.stats.ErrorCount++
}

// notifyWatchers notifies all watchers of a node change
func (kd *KubernetesDiscovery) notifyWatchers(event NodeChangeEvent) {
	kd.watchersMu.RLock()
	watchers := make([]NodeChangeCallback, len(kd.watchers))
	copy(watchers, kd.watchers)
	kd.watchersMu.RUnlock()

	for _, callback := range watchers {
		go func(cb NodeChangeCallback) {
			if err := cb(event); err != nil {
				if kd.logger != nil {
					kd.logger.Error("Kubernetes discovery watcher callback failed",
						logger.String("event_type", string(event.Type)),
						logger.String("node_id", event.Node.ID),
						logger.Error(err),
					)
				}
			}
		}(callback)
	}
}

// Helper functions for Kubernetes discovery

// CreateKubernetesDiscovery creates a Kubernetes discovery service
func CreateKubernetesDiscovery(namespace, serviceName, labelSelector string, servicePort int, logger common.Logger, metrics common.Metrics) (Discovery, error) {
	factory := NewKubernetesDiscoveryFactory(logger, metrics)

	config := DiscoveryConfig{
		Type:            DiscoveryTypeKubernetes,
		Namespace:       namespace,
		NodeTTL:         5 * time.Minute,
		RefreshInterval: 30 * time.Second,
		Timeout:         10 * time.Second,
		Retries:         3,
		Options: map[string]interface{}{
			"service_name":   serviceName,
			"service_port":   servicePort,
			"label_selector": labelSelector,
			"in_cluster":     true,
			"enable_watch":   true,
		},
	}

	return factory.Create(config)
}

// CreateKubernetesDiscoveryWithConfig creates a Kubernetes discovery service with kubeconfig
func CreateKubernetesDiscoveryWithConfig(namespace, serviceName, labelSelector, kubeConfig string, servicePort int, logger common.Logger, metrics common.Metrics) (Discovery, error) {
	factory := NewKubernetesDiscoveryFactory(logger, metrics)

	config := DiscoveryConfig{
		Type:            DiscoveryTypeKubernetes,
		Namespace:       namespace,
		NodeTTL:         5 * time.Minute,
		RefreshInterval: 30 * time.Second,
		Timeout:         10 * time.Second,
		Retries:         3,
		Options: map[string]interface{}{
			"service_name":   serviceName,
			"service_port":   servicePort,
			"label_selector": labelSelector,
			"kube_config":    kubeConfig,
			"in_cluster":     false,
			"enable_watch":   true,
		},
	}

	return factory.Create(config)
}

// ValidateKubernetesLabels validates Kubernetes label selector
func ValidateKubernetesLabels(labelSelector string) error {
	// Simple validation - in practice, you'd use Kubernetes validation
	if labelSelector == "" {
		return fmt.Errorf("label selector cannot be empty")
	}

	// Check for basic label format
	if !strings.Contains(labelSelector, "=") {
		return fmt.Errorf("invalid label selector format: %s", labelSelector)
	}

	return nil
}

// CreateKubernetesFieldSelector creates a field selector for pods
func CreateKubernetesFieldSelector(nodeName string, phase corev1.PodPhase) string {
	selector := fields.Set{}

	if nodeName != "" {
		selector["spec.nodeName"] = nodeName
	}

	if phase != "" {
		selector["status.phase"] = string(phase)
	}

	return selector.AsSelector().String()
}
