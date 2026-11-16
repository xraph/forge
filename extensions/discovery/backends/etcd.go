package backends

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
)

// etcdOpTimeout bounds individual etcd calls made from background goroutines,
// so a stalled cluster cannot block lease recovery or Close indefinitely.
const etcdOpTimeout = 5 * time.Second

// ErrEtcdNotInitialized is returned when Register is called before
// Initialize has created the registration lease.
var ErrEtcdNotInitialized = errors.New("etcd backend not initialized: call Initialize before Register")

// EtcdBackend implements service discovery using etcd.
type EtcdBackend struct {
	client   *clientv3.Client
	config   EtcdConfig
	watchers map[string][]func([]*ServiceInstance)
	mu       sync.RWMutex
	stopCh   chan struct{}
	wg       sync.WaitGroup
	leaseTTL int64

	// ctx is cancelled by Close so background etcd calls (keep-alive, lease
	// grants, watches) return promptly instead of holding Close up.
	ctx    context.Context //nolint:containedctx // lifetime context for background goroutines, cancelled by Close
	cancel context.CancelFunc

	// leaseMu guards leaseID and registered. Registrations are remembered so
	// they can be written again under a new lease after the old one expires.
	leaseMu    sync.Mutex
	leaseID    clientv3.LeaseID
	registered map[string]etcdRegistration // key: instance ID

	ops etcdOps
}

type etcdRegistration struct {
	key   string
	value string
}

// etcdOps is the subset of etcd writes the backend performs. It exists so
// lease and registration bookkeeping can be tested without an etcd server.
type etcdOps interface {
	put(ctx context.Context, key, value string, lease clientv3.LeaseID) error
	delete(ctx context.Context, key string) error
}

type clientOps struct{ client *clientv3.Client }

func (c clientOps) put(ctx context.Context, key, value string, lease clientv3.LeaseID) error {
	_, err := c.client.Put(ctx, key, value, clientv3.WithLease(lease))

	return err
}

func (c clientOps) delete(ctx context.Context, key string) error {
	_, err := c.client.Delete(ctx, key)

	return err
}

// NewEtcdBackend creates a new etcd backend.
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

	tlsConfig, err := buildEtcdTLSConfig(config)
	if err != nil {
		return nil, err
	}

	clientConfig.TLS = tlsConfig

	// Create client
	client, err := clientv3.New(clientConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create etcd client: %w", err)
	}

	// Set default key prefix
	if config.KeyPrefix == "" {
		config.KeyPrefix = "/services"
	}

	b := newEtcdBackend(config, clientOps{client: client})
	b.client = client

	return b, nil
}

func newEtcdBackend(config EtcdConfig, ops etcdOps) *EtcdBackend {
	ctx, cancel := context.WithCancel(context.Background())

	return &EtcdBackend{
		config:     config,
		watchers:   make(map[string][]func([]*ServiceInstance)),
		stopCh:     make(chan struct{}),
		leaseTTL:   30, // 30 second lease TTL
		ctx:        ctx,
		cancel:     cancel,
		registered: make(map[string]etcdRegistration),
		ops:        ops,
	}
}

// buildEtcdTLSConfig turns the TLS fields of EtcdConfig into a tls.Config.
//
// It returns nil when TLS is disabled. TLS file settings without TLSEnabled,
// or a certificate without its key, are configuration errors: silently
// falling back to plaintext would send credentials in the clear.
func buildEtcdTLSConfig(config EtcdConfig) (*tls.Config, error) {
	if !config.TLSEnabled {
		if config.TLSCAFile != "" || config.TLSCert != "" || config.TLSKey != "" {
			return nil, errors.New("etcd: tls_ca_file, tls_cert or tls_key is set but tls_enabled is false")
		}

		return nil, nil //nolint:nilnil // nil config means plaintext, which the caller asked for
	}

	cfg := &tls.Config{MinVersion: tls.VersionTLS12}

	if config.TLSCAFile != "" {
		pem, err := os.ReadFile(config.TLSCAFile)
		if err != nil {
			return nil, fmt.Errorf("etcd: read tls_ca_file: %w", err)
		}

		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("etcd: tls_ca_file %s contains no PEM certificates", config.TLSCAFile)
		}

		cfg.RootCAs = pool
	}

	if (config.TLSCert == "") != (config.TLSKey == "") {
		return nil, errors.New("etcd: tls_cert and tls_key must be set together")
	}

	if config.TLSCert != "" {
		cert, err := tls.LoadX509KeyPair(config.TLSCert, config.TLSKey)
		if err != nil {
			return nil, fmt.Errorf("etcd: load client certificate: %w", err)
		}

		cfg.Certificates = []tls.Certificate{cert}
	}

	return cfg, nil
}

// Name returns the backend name.
func (b *EtcdBackend) Name() string {
	return "etcd"
}

// Initialize initializes the backend.
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

	b.setLease(lease.ID)

	// Start lease keep-alive. It runs until Close, on the backend's own
	// context, so it does not inherit the caller's cancellation.
	b.wg.Add(1)

	go b.keepAliveLease()

	return nil
}

func (b *EtcdBackend) currentLease() clientv3.LeaseID {
	b.leaseMu.Lock()
	defer b.leaseMu.Unlock()

	return b.leaseID
}

func (b *EtcdBackend) setLease(id clientv3.LeaseID) {
	b.leaseMu.Lock()
	b.leaseID = id
	b.leaseMu.Unlock()
}

// Register registers a service instance with etcd.
func (b *EtcdBackend) Register(ctx context.Context, instance *ServiceInstance) error {
	if instance.ID == "" {
		return errors.New("service instance ID is required")
	}

	if instance.Name == "" {
		return errors.New("service name is required")
	}

	// Serialize instance to JSON
	data, err := json.Marshal(instance)
	if err != nil {
		return fmt.Errorf("failed to serialize service instance: %w", err)
	}

	// Build key: /services/{service-name}/{instance-id}
	key := b.buildServiceKey(instance.Name, instance.ID)

	// Hold leaseMu across the put so a concurrent lease replacement either
	// sees this registration or happens before it and hands us the new lease.
	b.leaseMu.Lock()

	lease := b.leaseID
	if lease == 0 {
		b.leaseMu.Unlock()

		return ErrEtcdNotInitialized
	}

	if err := b.ops.put(ctx, key, string(data), lease); err != nil {
		b.leaseMu.Unlock()

		return fmt.Errorf("failed to register service with etcd: %w", err)
	}

	b.registered[instance.ID] = etcdRegistration{key: key, value: string(data)}
	b.leaseMu.Unlock()

	// Notify watchers
	b.notifyWatchers(instance.Name)

	return nil
}

// Deregister deregisters a service instance from etcd.
func (b *EtcdBackend) Deregister(ctx context.Context, serviceID string) error {
	// Instances registered through this backend have a known key, and must be
	// forgotten so lease recovery does not write them back.
	b.leaseMu.Lock()
	reg, ok := b.registered[serviceID]
	delete(b.registered, serviceID)
	b.leaseMu.Unlock()

	if ok {
		if err := b.ops.delete(ctx, reg.key); err != nil {
			return fmt.Errorf("failed to deregister service from etcd: %w", err)
		}

		return nil
	}

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

// Discover discovers service instances by name from etcd.
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

// DiscoverWithTags discovers service instances by name and tags from etcd.
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

// Watch watches for changes to a service in etcd.
func (b *EtcdBackend) Watch(ctx context.Context, serviceName string, onChange func([]*ServiceInstance)) error {
	b.mu.Lock()
	b.watchers[serviceName] = append(b.watchers[serviceName], onChange)
	b.mu.Unlock()

	// Send initial state
	instances, err := b.Discover(ctx, serviceName)
	if err == nil {
		onChange(instances)
	}

	// Start watch goroutine. It runs until Close on the backend's own
	// context, so it does not inherit the caller's cancellation.
	b.wg.Add(1)

	go b.watchService(serviceName)

	return nil
}

// watchService watches a service for changes using etcd watch.
func (b *EtcdBackend) watchService(serviceName string) {
	defer b.wg.Done()

	ctx := b.ctx
	prefix := b.buildServicePrefix(serviceName)
	watchChan := b.client.Watch(ctx, prefix, clientv3.WithPrefix())

	for {
		select {
		case <-b.stopCh:
			return
		case watchResp, ok := <-watchChan:
			if !ok {
				// The watch ended (compaction, lost connection or Close).
				// Start a new one unless the backend is shutting down.
				if ctx.Err() != nil {
					return
				}

				select {
				case <-b.stopCh:
					return
				case <-time.After(time.Second):
				}

				watchChan = b.client.Watch(ctx, prefix, clientv3.WithPrefix())

				continue
			}

			if watchResp.Err() != nil {
				continue
			}

			// Service changed, fetch current state
			instances, err := b.Discover(ctx, serviceName)
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

// ListServices lists all registered services in etcd.
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

// Health checks backend health.
func (b *EtcdBackend) Health(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	_, err := b.client.Status(ctx, b.config.Endpoints[0])
	if err != nil {
		return fmt.Errorf("etcd health check failed: %w", err)
	}

	return nil
}

// Close closes the backend.
func (b *EtcdBackend) Close() error {
	close(b.stopCh)
	b.cancel()
	b.wg.Wait()

	// Revoke lease. A failed revoke still closes the client; the lease then
	// just expires on its TTL.
	var revokeErr error

	if lease := b.currentLease(); lease != 0 {
		ctx, cancel := context.WithTimeout(context.Background(), etcdOpTimeout)
		defer cancel()

		if _, err := b.client.Revoke(ctx, lease); err != nil {
			revokeErr = fmt.Errorf("failed to revoke etcd lease: %w", err)
		}
	}

	return errors.Join(revokeErr, b.client.Close())
}

// keepAliveLease keeps the lease alive, and when it is lost, grants a new
// lease and writes every registered instance again under it.
func (b *EtcdBackend) keepAliveLease() {
	defer b.wg.Done()

	ctx := b.ctx

	keepAliveChan, err := b.client.KeepAlive(ctx, b.currentLease())
	if err != nil {
		keepAliveChan = nil
	}

	backoff := time.Second

	for {
		if keepAliveChan != nil {
			select {
			case <-b.stopCh:
				return
			case _, ok := <-keepAliveChan:
				if ok {
					continue
				}
			}
		}

		// The keep-alive stream ended, so the lease is gone or about to be.
		if ctx.Err() != nil {
			return
		}

		keepAliveChan, err = b.renewLease()
		if err != nil {
			keepAliveChan = nil

			select {
			case <-b.stopCh:
				return
			case <-time.After(backoff):
			}

			backoff = min(backoff*2, 30*time.Second)

			continue
		}

		backoff = time.Second
	}
}

// renewLease grants a new lease, re-registers every known instance under it,
// and starts a keep-alive stream for it.
func (b *EtcdBackend) renewLease() (<-chan *clientv3.LeaseKeepAliveResponse, error) {
	grantCtx, cancel := context.WithTimeout(b.ctx, etcdOpTimeout)
	lease, err := b.client.Grant(grantCtx, b.leaseTTL)

	cancel()

	if err != nil {
		return nil, fmt.Errorf("grant etcd lease: %w", err)
	}

	if err := b.reregisterAll(b.ctx, lease.ID); err != nil {
		return nil, err
	}

	return b.client.KeepAlive(b.ctx, lease.ID)
}

// reregisterAll switches to lease and writes every registered instance under
// it. Registrations made concurrently wait on leaseMu, so none is written
// under the old lease after the switch.
func (b *EtcdBackend) reregisterAll(ctx context.Context, lease clientv3.LeaseID) error {
	b.leaseMu.Lock()
	defer b.leaseMu.Unlock()

	b.leaseID = lease

	var errs []error

	for id, reg := range b.registered {
		putCtx, cancel := context.WithTimeout(ctx, etcdOpTimeout)
		err := b.ops.put(putCtx, reg.key, reg.value, lease)

		cancel()

		if err != nil {
			errs = append(errs, fmt.Errorf("re-register %s: %w", id, err))
		}
	}

	return errors.Join(errs...)
}

// notifyWatchers notifies all watchers for a service.
func (b *EtcdBackend) notifyWatchers(serviceName string) {
	b.mu.RLock()
	hasWatchers := len(b.watchers[serviceName]) > 0
	b.mu.RUnlock()

	if !hasWatchers {
		return
	}

	ctx, cancel := context.WithTimeout(b.ctx, etcdOpTimeout)
	defer cancel()

	instances, err := b.Discover(ctx, serviceName)
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

// buildServiceKey builds the etcd key for a service instance.
func (b *EtcdBackend) buildServiceKey(serviceName, instanceID string) string {
	return path.Join(b.config.KeyPrefix, serviceName, instanceID)
}

// buildServicePrefix builds the etcd key prefix for a service.
func (b *EtcdBackend) buildServicePrefix(serviceName string) string {
	return path.Join(b.config.KeyPrefix, serviceName) + "/"
}

// extractServiceName extracts service name from etcd key.
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

// GetServiceByID retrieves a specific service instance by ID.
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

// UpdateServiceHealth updates the health status of a service instance.
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
