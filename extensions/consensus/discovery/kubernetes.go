package discovery

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/consensus/internal"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// KubernetesDiscovery implements Kubernetes-based service discovery
type KubernetesDiscovery struct {
	clientset     *kubernetes.Clientset
	namespace     string
	serviceName   string
	labelSelector string
	port          int
	logger        forge.Logger

	// Caching
	cachedPeers []internal.NodeInfo
	cacheMu     sync.RWMutex
	cacheExpiry time.Time
	cacheTTL    time.Duration

	// Lifecycle
	started       bool
	ctx           context.Context
	cancel        context.CancelFunc
	wg            sync.WaitGroup
	mu            sync.RWMutex
	checkInterval time.Duration
}

// KubernetesDiscoveryConfig contains Kubernetes discovery configuration
type KubernetesDiscoveryConfig struct {
	Namespace     string
	ServiceName   string
	LabelSelector string
	Port          int
	Kubeconfig    string
	CheckInterval time.Duration
	CacheTTL      time.Duration
}

// NewKubernetesDiscovery creates a new Kubernetes service discovery
func NewKubernetesDiscovery(config KubernetesDiscoveryConfig, logger forge.Logger) (*KubernetesDiscovery, error) {
	var clientConfig *rest.Config
	var err error

	// Try to use in-cluster config first
	clientConfig, err = rest.InClusterConfig()
	if err != nil {
		// Fall back to kubeconfig file
		kubeconfigPath := config.Kubeconfig
		if kubeconfigPath == "" {
			kubeconfigPath = os.Getenv("KUBECONFIG")
		}
		if kubeconfigPath == "" {
			home := os.Getenv("HOME")
			if home != "" {
				kubeconfigPath = home + "/.kube/config"
			}
		}

		clientConfig, err = clientcmd.BuildConfigFromFlags("", kubeconfigPath)
		if err != nil {
			return nil, fmt.Errorf("failed to create kubernetes client config: %w", err)
		}
	}

	// Create clientset
	clientset, err := kubernetes.NewForConfig(clientConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes clientset: %w", err)
	}

	if config.Namespace == "" {
		config.Namespace = "default"
	}

	if config.ServiceName == "" {
		config.ServiceName = "forge-consensus"
	}

	if config.CheckInterval == 0 {
		config.CheckInterval = 30 * time.Second
	}

	if config.CacheTTL == 0 {
		config.CacheTTL = 10 * time.Second
	}

	return &KubernetesDiscovery{
		clientset:     clientset,
		namespace:     config.Namespace,
		serviceName:   config.ServiceName,
		labelSelector: config.LabelSelector,
		port:          config.Port,
		logger:        logger,
		cacheTTL:      config.CacheTTL,
		checkInterval: config.CheckInterval,
	}, nil
}

// Start starts the discovery service
func (kd *KubernetesDiscovery) Start(ctx context.Context) error {
	kd.mu.Lock()
	defer kd.mu.Unlock()

	if kd.started {
		return internal.ErrAlreadyStarted
	}

	kd.ctx, kd.cancel = context.WithCancel(ctx)
	kd.started = true

	// Start peer discovery loop
	kd.wg.Add(1)
	go kd.discoveryLoop()

	kd.logger.Info("kubernetes discovery started",
		forge.F("namespace", kd.namespace),
		forge.F("service_name", kd.serviceName),
		forge.F("label_selector", kd.labelSelector),
	)

	return nil
}

// Stop stops the discovery service
func (kd *KubernetesDiscovery) Stop(ctx context.Context) error {
	kd.mu.Lock()
	if !kd.started {
		kd.mu.Unlock()
		return internal.ErrNotStarted
	}
	kd.mu.Unlock()

	if kd.cancel != nil {
		kd.cancel()
	}

	// Wait for goroutines
	done := make(chan struct{})
	go func() {
		kd.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		kd.logger.Info("kubernetes discovery stopped")
	case <-ctx.Done():
		kd.logger.Warn("kubernetes discovery stop timed out")
	}

	return nil
}

// DiscoverPeers discovers peer nodes from Kubernetes endpoints
func (kd *KubernetesDiscovery) DiscoverPeers(ctx context.Context) ([]internal.NodeInfo, error) {
	// Check cache first
	kd.cacheMu.RLock()
	if time.Now().Before(kd.cacheExpiry) && len(kd.cachedPeers) > 0 {
		peers := make([]internal.NodeInfo, len(kd.cachedPeers))
		copy(peers, kd.cachedPeers)
		kd.cacheMu.RUnlock()
		return peers, nil
	}
	kd.cacheMu.RUnlock()

	// Discover using pods
	if kd.labelSelector != "" {
		return kd.discoverViaPods(ctx)
	}

	// Discover using service endpoints
	return kd.discoverViaEndpoints(ctx)
}

// discoverViaEndpoints discovers peers using Kubernetes service endpoints
func (kd *KubernetesDiscovery) discoverViaEndpoints(ctx context.Context) ([]internal.NodeInfo, error) {
	endpoints, err := kd.clientset.CoreV1().Endpoints(kd.namespace).Get(ctx, kd.serviceName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get endpoints: %w", err)
	}

	var peers []internal.NodeInfo
	for _, subset := range endpoints.Subsets {
		port := kd.port
		if port == 0 && len(subset.Ports) > 0 {
			port = int(subset.Ports[0].Port)
		}

		for _, addr := range subset.Addresses {
			nodeID := addr.IP // Use IP as node ID if not available
			if addr.TargetRef != nil {
				nodeID = addr.TargetRef.Name
			}

			peers = append(peers, internal.NodeInfo{
				ID:      nodeID,
				Address: addr.IP,
				Port:    port,
				Role:    internal.RoleFollower,
				Status:  internal.StatusActive,
			})
		}
	}

	// Update cache
	kd.cacheMu.Lock()
	kd.cachedPeers = peers
	kd.cacheExpiry = time.Now().Add(kd.cacheTTL)
	kd.cacheMu.Unlock()

	kd.logger.Debug("discovered peers via endpoints",
		forge.F("count", len(peers)),
	)

	return peers, nil
}

// discoverViaPods discovers peers using Kubernetes pod labels
func (kd *KubernetesDiscovery) discoverViaPods(ctx context.Context) ([]internal.NodeInfo, error) {
	pods, err := kd.clientset.CoreV1().Pods(kd.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: kd.labelSelector,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list pods: %w", err)
	}

	var peers []internal.NodeInfo
	for _, pod := range pods.Items {
		// Only include running pods
		if pod.Status.Phase != v1.PodRunning {
			continue
		}

		// Skip pods without IP
		if pod.Status.PodIP == "" {
			continue
		}

		peers = append(peers, internal.NodeInfo{
			ID:      pod.Name,
			Address: pod.Status.PodIP,
			Port:    kd.port,
			Role:    internal.RoleFollower,
			Status:  internal.StatusActive,
		})
	}

	// Update cache
	kd.cacheMu.Lock()
	kd.cachedPeers = peers
	kd.cacheExpiry = time.Now().Add(kd.cacheTTL)
	kd.cacheMu.Unlock()

	kd.logger.Debug("discovered peers via pods",
		forge.F("count", len(peers)),
	)

	return peers, nil
}

// discoveryLoop periodically discovers peers
func (kd *KubernetesDiscovery) discoveryLoop() {
	defer kd.wg.Done()

	ticker := time.NewTicker(kd.checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// Pre-fetch peers to refresh cache
			if _, err := kd.DiscoverPeers(kd.ctx); err != nil {
				kd.logger.Error("failed to discover peers",
					forge.F("error", err),
				)
			}

		case <-kd.ctx.Done():
			return
		}
	}
}

// GetNodeInfo returns information about a specific node
func (kd *KubernetesDiscovery) GetNodeInfo(nodeID string) (*internal.NodeInfo, error) {
	peers, err := kd.DiscoverPeers(kd.ctx)
	if err != nil {
		return nil, err
	}

	for _, peer := range peers {
		if peer.ID == nodeID {
			return &peer, nil
		}
	}

	return nil, internal.ErrNodeNotFound
}

// WatchPeers watches for peer changes using Kubernetes watch API
func (kd *KubernetesDiscovery) WatchPeers(ctx context.Context) (<-chan []internal.NodeInfo, error) {
	ch := make(chan []internal.NodeInfo, 1)

	kd.wg.Add(1)
	go func() {
		defer kd.wg.Done()
		defer close(ch)

		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()

		var lastPeers []internal.NodeInfo

		for {
			select {
			case <-ticker.C:
				peers, err := kd.DiscoverPeers(ctx)
				if err != nil {
					kd.logger.Error("failed to watch peers",
						forge.F("error", err),
					)
					continue
				}

				// Check if peers changed
				if !peersEqual(lastPeers, peers) {
					lastPeers = peers
					select {
					case ch <- peers:
					case <-ctx.Done():
						return
					}
				}

			case <-ctx.Done():
				return
			case <-kd.ctx.Done():
				return
			}
		}
	}()

	return ch, nil
}
