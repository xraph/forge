package backends

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"strings"
	"sync"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
)

// EtcdBackend implements service discovery using etcd
type EtcdBackend struct {
	client     *clientv3.Client
	config     EtcdConfig
	leaseID    clientv3.LeaseID
	watchers   map[string][]func([]*ServiceInstance)
	mu         sync.RWMutex
	stopCh     chan struct{}
	wg         sync.WaitGroup
	leaseTTL   int64
}

// NewEtcdBackend creates a new etcd backend
func NewEtcdBackend(config EtcdConfig) (*EtcdBackend, error) {
	// Create etcd client configuration
	clientConfig := clientv3.Config{
		Endpoints:   config.Endpoints,
		DialTimeout: config.DialTimeout,
	}

	// Authentication
	if config.Username != "" {
		clientConfig.Username = config.Username
		clientConfig.Password = config.Password
	}

	// TLS configuration
	if config.TLSEnabled {
		// TODO: Add TLS configuration
		// tlsConfig := &tls.Config{...}
		// clientConfig.TLS = tlsConfig
	}

	// Create client
	client, err := clientv3.New(clientConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create etcd client: %w", err)
	}

	// Set default key prefix
	if config.KeyPrefix == "" {
		config.KeyPrefix = "/services"
	}

	return &EtcdBackend{
		client:   client,
		config:   config,
		watchers: make(map[string][]func([]*ServiceInstance)),
		stopCh:   make(chan struct{}),
		leaseTTL: 30, // 30 second lease TTL
	}, nil
}

// Name returns the backend name
func (b *EtcdBackend) Name() string {
	return "etcd"
}

// Initialize initializes the backend
func (b *EtcdBackend) Initialize(ctx context.Context) error {
	// Test connection
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	_, err := b.client.Status(ctx, b.config.Endpoints[0])
	if err != nil {
		return fmt.Errorf("failed to connect to etcd: %w", err)
	}

	// Create lease for service registration
	lease, err := b.client.Grant(ctx, b.leaseTTL)
	if err != nil {
		return fmt.Errorf("failed to create etcd lease: %w", err)
	}

	b.leaseID = lease.ID

	// Start lease keep-alive
	b.wg.Add(1)
	go b.keepAliveLease()

	return nil
}

// Register registers a service instance with etcd
func (b *EtcdBackend) Register(ctx context.Context, instance *ServiceInstance) error {
	if instance.ID == "" {
		return fmt.Errorf("service instance ID is required")
	}
	if instance.Name == "" {
		return fmt.Errorf("service name is required")
	}

	// Serialize instance to JSON
	data, err := json.Marshal(instance)
	if err != nil {
		return fmt.Errorf("failed to serialize service instance: %w", err)
	}

	// Build key: /services/{service-name}/{instance-id}
	key := b.buildServiceKey(instance.Name, instance.ID)

	// Put with lease
	_, err = b.client.Put(ctx, key, string(data), clientv3.WithLease(b.leaseID))
	if err != nil {
		return fmt.Errorf("failed to register service with etcd: %w", err)
	}

	// Notify watchers
	b.notifyWatchers(instance.Name)

	return nil
}

// Deregister deregisters a service instance from etcd
func (b *EtcdBackend) Deregister(ctx context.Context, serviceID string) error {
	// Find and delete the service key
	// We need to search for the key since we don't know the service name
	prefix := b.config.KeyPrefix
	resp, err := b.client.Get(ctx, prefix, clientv3.WithPrefix())
	if err != nil {
		return fmt.Errorf("failed to search for service: %w", err)
	}

	var foundKey string
	for _, kv := range resp.Kvs {
		var instance ServiceInstance
		if err := json.Unmarshal(kv.Value, &instance); err != nil {
			continue
		}

		if instance.ID == serviceID {
			foundKey = string(kv.Key)
			break
		}
	}

	if foundKey == "" {
		return fmt.Errorf("service instance not found: %s", serviceID)
	}

	// Delete the key
	_, err = b.client.Delete(ctx, foundKey)
	if err != nil {
		return fmt.Errorf("failed to deregister service from etcd: %w", err)
	}

	return nil
}

// Discover discovers service instances by name from etcd
func (b *EtcdBackend) Discover(ctx context.Context, serviceName string) ([]*ServiceInstance, error) {
	// Get all instances for this service
	prefix := b.buildServicePrefix(serviceName)
	resp, err := b.client.Get(ctx, prefix, clientv3.WithPrefix())
	if err != nil {
		return nil, fmt.Errorf("failed to discover service from etcd: %w", err)
	}

	// Deserialize instances
	instances := make([]*ServiceInstance, 0, len(resp.Kvs))
	for _, kv := range resp.Kvs {
		var instance ServiceInstance
		if err := json.Unmarshal(kv.Value, &instance); err != nil {
			continue // Skip malformed entries
		}
		instances = append(instances, &instance)
	}

	return instances, nil
}

// DiscoverWithTags discovers service instances by name and tags from etcd
func (b *EtcdBackend) DiscoverWithTags(ctx context.Context, serviceName string, tags []string) ([]*ServiceInstance, error) {
	instances, err := b.Discover(ctx, serviceName)
	if err != nil {
		return nil, err
	}

	if len(tags) == 0 {
		return instances, nil
	}

	// Filter instances by tags
	filtered := make([]*ServiceInstance, 0, len(instances))
	for _, instance := range instances {
		if instance.HasAllTags(tags) {
			filtered = append(filtered, instance)
		}
	}

	return filtered, nil
}

// Watch watches for changes to a service in etcd
func (b *EtcdBackend) Watch(ctx context.Context, serviceName string, onChange func([]*ServiceInstance)) error {
	b.mu.Lock()
	b.watchers[serviceName] = append(b.watchers[serviceName], onChange)
	b.mu.Unlock()

	// Send initial state
	instances, err := b.Discover(ctx, serviceName)
	if err == nil {
		onChange(instances)
	}

	// Start watch goroutine
	b.wg.Add(1)
	go b.watchService(serviceName)

	return nil
}

// watchService watches a service for changes using etcd watch
func (b *EtcdBackend) watchService(serviceName string) {
	defer b.wg.Done()

	prefix := b.buildServicePrefix(serviceName)
	watchChan := b.client.Watch(context.Background(), prefix, clientv3.WithPrefix())

	for {
		select {
		case <-b.stopCh:
			return
		case watchResp := <-watchChan:
			if watchResp.Err() != nil {
				continue
			}

			// Service changed, fetch current state
			instances, err := b.Discover(context.Background(), serviceName)
			if err != nil {
				continue
			}

			// Notify watchers
			b.mu.RLock()
			watchers := b.watchers[serviceName]
			b.mu.RUnlock()

			for _, watcher := range watchers {
				go watcher(instances)
			}
		}
	}
}

// ListServices lists all registered services in etcd
func (b *EtcdBackend) ListServices(ctx context.Context) ([]string, error) {
	// Get all service keys
	resp, err := b.client.Get(ctx, b.config.KeyPrefix, clientv3.WithPrefix(), clientv3.WithKeysOnly())
	if err != nil {
		return nil, fmt.Errorf("failed to list services from etcd: %w", err)
	}

	// Extract unique service names
	serviceSet := make(map[string]bool)
	for _, kv := range resp.Kvs {
		serviceName := b.extractServiceName(string(kv.Key))
		if serviceName != "" {
			serviceSet[serviceName] = true
		}
	}

	services := make([]string, 0, len(serviceSet))
	for name := range serviceSet {
		services = append(services, name)
	}

	return services, nil
}

// Health checks backend health
func (b *EtcdBackend) Health(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	_, err := b.client.Status(ctx, b.config.Endpoints[0])
	if err != nil {
		return fmt.Errorf("etcd health check failed: %w", err)
	}
	return nil
}

// Close closes the backend
func (b *EtcdBackend) Close() error {
	close(b.stopCh)
	b.wg.Wait()

	// Revoke lease
	if b.leaseID != 0 {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		b.client.Revoke(ctx, b.leaseID)
	}

	return b.client.Close()
}

// keepAliveLease keeps the lease alive
func (b *EtcdBackend) keepAliveLease() {
	defer b.wg.Done()

	keepAliveChan, err := b.client.KeepAlive(context.Background(), b.leaseID)
	if err != nil {
		return
	}

	for {
		select {
		case <-b.stopCh:
			return
		case _, ok := <-keepAliveChan:
			if !ok {
				// Lease expired, recreate
				lease, err := b.client.Grant(context.Background(), b.leaseTTL)
				if err != nil {
					continue
				}
				b.leaseID = lease.ID

				// Restart keep-alive
				keepAliveChan, err = b.client.KeepAlive(context.Background(), b.leaseID)
				if err != nil {
					continue
				}
			}
		}
	}
}

// notifyWatchers notifies all watchers for a service
func (b *EtcdBackend) notifyWatchers(serviceName string) {
	instances, err := b.Discover(context.Background(), serviceName)
	if err != nil {
		return
	}

	b.mu.RLock()
	watchers := b.watchers[serviceName]
	b.mu.RUnlock()

	for _, watcher := range watchers {
		go watcher(instances)
	}
}

// buildServiceKey builds the etcd key for a service instance
func (b *EtcdBackend) buildServiceKey(serviceName, instanceID string) string {
	return path.Join(b.config.KeyPrefix, serviceName, instanceID)
}

// buildServicePrefix builds the etcd key prefix for a service
func (b *EtcdBackend) buildServicePrefix(serviceName string) string {
	return path.Join(b.config.KeyPrefix, serviceName) + "/"
}

// extractServiceName extracts service name from etcd key
func (b *EtcdBackend) extractServiceName(key string) string {
	// Remove prefix
	key = strings.TrimPrefix(key, b.config.KeyPrefix+"/")

	// Get first path component
	parts := strings.Split(key, "/")
	if len(parts) > 0 {
		return parts[0]
	}

	return ""
}

// GetServiceByID retrieves a specific service instance by ID
func (b *EtcdBackend) GetServiceByID(ctx context.Context, serviceID string) (*ServiceInstance, error) {
	// Search all services for the instance ID
	resp, err := b.client.Get(ctx, b.config.KeyPrefix, clientv3.WithPrefix())
	if err != nil {
		return nil, fmt.Errorf("failed to search for service: %w", err)
	}

	for _, kv := range resp.Kvs {
		var instance ServiceInstance
		if err := json.Unmarshal(kv.Value, &instance); err != nil {
			continue
		}

		if instance.ID == serviceID {
			return &instance, nil
		}
	}

	return nil, fmt.Errorf("service instance not found: %s", serviceID)
}

// UpdateServiceHealth updates the health status of a service instance
func (b *EtcdBackend) UpdateServiceHealth(ctx context.Context, serviceID string, status HealthStatus) error {
	// Get the service instance
	instance, err := b.GetServiceByID(ctx, serviceID)
	if err != nil {
		return err
	}

	// Update status
	instance.Status = status
	instance.LastHeartbeat = time.Now().Unix()

	// Re-register with updated status
	return b.Register(ctx, instance)
}

