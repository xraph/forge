package backends

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	corev1listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
)

// tagsAnnotation lets a Service publish discovery tags (comma-separated) so
// DiscoverWithTags can filter on them — e.g. "forge-dashboard-contributor".
const tagsAnnotation = "discovery.forge.xraph.io/tags"

// serviceAccountNamespaceFile is where the in-cluster namespace is mounted.
const serviceAccountNamespaceFile = "/var/run/secrets/kubernetes.io/serviceaccount/namespace"

// KubernetesBackend discovers services from the Kubernetes API by listing and
// watching Services in a namespace, filtered by an optional label selector.
//
// Registration is implicit in Kubernetes — a Service object created by Helm/the
// operator IS the registration — so Register/Deregister are no-ops and discovery
// always reflects live cluster state via a shared informer cache. This makes it
// the right backend for multi-replica, in-cluster deployments where the
// in-memory backend (process-local) can't see peer services.
type KubernetesBackend struct {
	cfg KubernetesConfig

	clientset kubernetes.Interface
	factory   informers.SharedInformerFactory
	informer  cache.SharedIndexInformer
	lister    corev1listers.ServiceLister

	stopCh   chan struct{}
	stopOnce sync.Once

	mu       sync.RWMutex
	watchers map[string][]func([]*ServiceInstance)
}

// NewKubernetesBackend creates a Kubernetes-backed discovery backend. The
// informer is wired up in Initialize so construction stays cheap and failure
// surfaces at start time.
func NewKubernetesBackend(cfg KubernetesConfig) (*KubernetesBackend, error) {
	return &KubernetesBackend{
		cfg:      cfg,
		stopCh:   make(chan struct{}),
		watchers: make(map[string][]func([]*ServiceInstance)),
	}, nil
}

// Name returns the backend name.
func (b *KubernetesBackend) Name() string { return "kubernetes" }

// Initialize builds the Kubernetes client and starts a Service informer scoped
// to the configured namespace and label selector, then waits for the cache to
// sync so the first Discover call returns complete data.
func (b *KubernetesBackend) Initialize(ctx context.Context) error {
	restCfg, err := b.restConfig()
	if err != nil {
		return fmt.Errorf("kubernetes discovery: rest config: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		return fmt.Errorf("kubernetes discovery: clientset: %w", err)
	}
	return b.start(ctx, clientset)
}

// start wires the Service informer against an existing clientset. Split out from
// Initialize so tests can inject a fake clientset.
func (b *KubernetesBackend) start(ctx context.Context, clientset kubernetes.Interface) error {
	b.clientset = clientset

	if b.cfg.Namespace == "" {
		b.cfg.Namespace = inClusterNamespace()
	}

	opts := []informers.SharedInformerOption{}
	if b.cfg.Namespace != "" {
		opts = append(opts, informers.WithNamespace(b.cfg.Namespace))
	}
	if sel := b.cfg.LabelSelector; sel != "" {
		opts = append(opts, informers.WithTweakListOptions(func(o *metav1.ListOptions) {
			o.LabelSelector = sel
		}))
	}

	b.factory = informers.NewSharedInformerFactoryWithOptions(clientset, 5*time.Minute, opts...)
	svcInformer := b.factory.Core().V1().Services()
	b.informer = svcInformer.Informer()
	b.lister = svcInformer.Lister()

	onChange := func(obj any) {
		if svc, ok := obj.(*corev1.Service); ok {
			b.notify(svc.Name)
		}
	}
	if _, err := b.informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    onChange,
		UpdateFunc: func(_, newObj any) { onChange(newObj) },
		DeleteFunc: onChange,
	}); err != nil {
		return fmt.Errorf("kubernetes discovery: add event handler: %w", err)
	}

	b.factory.Start(b.stopCh)
	if !cache.WaitForCacheSync(ctx.Done(), b.informer.HasSynced) {
		return errors.New("kubernetes discovery: informer cache sync failed")
	}
	return nil
}

// Register is a no-op: in Kubernetes the Service object is the registration.
func (b *KubernetesBackend) Register(ctx context.Context, instance *ServiceInstance) error {
	return nil
}

// Deregister is a no-op: Service lifecycle is managed by Kubernetes.
func (b *KubernetesBackend) Deregister(ctx context.Context, serviceID string) error {
	return nil
}

// Discover returns one instance per Service whose name matches serviceName.
func (b *KubernetesBackend) Discover(ctx context.Context, serviceName string) ([]*ServiceInstance, error) {
	svcs, err := b.listServices()
	if err != nil {
		return nil, err
	}

	result := make([]*ServiceInstance, 0, 1)
	for _, svc := range svcs {
		if svc.Name == serviceName {
			if inst := serviceToInstance(svc); inst != nil {
				result = append(result, inst)
			}
		}
	}

	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, nil
}

// DiscoverWithTags filters Discover results by the tags published on the Service
// via the discovery.forge.xraph.io/tags annotation.
func (b *KubernetesBackend) DiscoverWithTags(ctx context.Context, serviceName string, tags []string) ([]*ServiceInstance, error) {
	instances, err := b.Discover(ctx, serviceName)
	if err != nil {
		return nil, err
	}
	if len(tags) == 0 {
		return instances, nil
	}

	filtered := make([]*ServiceInstance, 0, len(instances))
	for _, inst := range instances {
		if inst.HasAllTags(tags) {
			filtered = append(filtered, inst)
		}
	}
	return filtered, nil
}

// Watch registers onChange for serviceName and fires it immediately with the
// current instances; the informer drives subsequent notifications.
func (b *KubernetesBackend) Watch(ctx context.Context, serviceName string, onChange func([]*ServiceInstance)) error {
	b.mu.Lock()
	b.watchers[serviceName] = append(b.watchers[serviceName], onChange)
	b.mu.Unlock()

	instances, err := b.Discover(ctx, serviceName)
	if err != nil {
		return err
	}
	go onChange(instances)
	return nil
}

// ListServices returns the distinct names of all matching Services.
func (b *KubernetesBackend) ListServices(ctx context.Context) ([]string, error) {
	svcs, err := b.listServices()
	if err != nil {
		return nil, err
	}

	seen := make(map[string]struct{}, len(svcs))
	names := make([]string, 0, len(svcs))
	for _, svc := range svcs {
		if _, ok := seen[svc.Name]; ok {
			continue
		}
		seen[svc.Name] = struct{}{}
		names = append(names, svc.Name)
	}
	sort.Strings(names)
	return names, nil
}

// Health verifies the Kubernetes API is reachable.
func (b *KubernetesBackend) Health(ctx context.Context) error {
	if b.clientset == nil {
		return errors.New("kubernetes discovery: not initialized")
	}
	_, err := b.clientset.CoreV1().Services(b.cfg.Namespace).List(ctx, metav1.ListOptions{
		Limit:         1,
		LabelSelector: b.cfg.LabelSelector,
	})
	return err
}

// Close stops the informer.
func (b *KubernetesBackend) Close() error {
	b.stopOnce.Do(func() { close(b.stopCh) })
	return nil
}

// listServices reads matching Services from the informer cache (already filtered
// by namespace + label selector at the informer level).
func (b *KubernetesBackend) listServices() ([]*corev1.Service, error) {
	if b.lister == nil {
		return nil, errors.New("kubernetes discovery: not initialized")
	}
	if b.cfg.Namespace != "" {
		return b.lister.Services(b.cfg.Namespace).List(labels.Everything())
	}
	return b.lister.List(labels.Everything())
}

// notify recomputes instances for serviceName and fans out to its watchers.
func (b *KubernetesBackend) notify(serviceName string) {
	b.mu.RLock()
	ws := append([]func([]*ServiceInstance){}, b.watchers[serviceName]...)
	b.mu.RUnlock()
	if len(ws) == 0 {
		return
	}

	instances, err := b.Discover(context.Background(), serviceName)
	if err != nil {
		return
	}
	for _, w := range ws {
		go w(instances)
	}
}

// restConfig resolves the client config: in-cluster when requested or when no
// kubeconfig is given, otherwise from the kubeconfig path (or default rules).
func (b *KubernetesBackend) restConfig() (*rest.Config, error) {
	if b.cfg.InCluster {
		return rest.InClusterConfig()
	}
	if b.cfg.KubeconfigPath == "" {
		// Try in-cluster first; fall back to the default kubeconfig loading rules.
		if cfg, err := rest.InClusterConfig(); err == nil {
			return cfg, nil
		}
	}
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	if b.cfg.KubeconfigPath != "" {
		rules.ExplicitPath = b.cfg.KubeconfigPath
	}
	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, &clientcmd.ConfigOverrides{}).ClientConfig()
}

// serviceToInstance maps a Kubernetes Service to a discovery ServiceInstance,
// addressed by its in-cluster DNS name and primary port.
func serviceToInstance(svc *corev1.Service) *ServiceInstance {
	if len(svc.Spec.Ports) == 0 {
		return nil
	}

	port := pickServicePort(svc.Spec.Ports)
	metadata := make(map[string]string, len(svc.Labels))
	for k, v := range svc.Labels {
		metadata[k] = v
	}

	var tags []string
	if raw, ok := svc.Annotations[tagsAnnotation]; ok {
		for _, t := range strings.Split(raw, ",") {
			if t = strings.TrimSpace(t); t != "" {
				tags = append(tags, t)
			}
		}
	}

	return &ServiceInstance{
		ID:            svc.Namespace + "/" + svc.Name,
		Name:          svc.Name,
		Version:       svc.Labels["app.kubernetes.io/version"],
		Address:       fmt.Sprintf("%s.%s.svc.cluster.local", svc.Name, svc.Namespace),
		Port:          port,
		Tags:          tags,
		Metadata:      metadata,
		Status:        HealthStatusPassing,
		LastHeartbeat: time.Now().Unix(),
	}
}

// pickServicePort prefers a port named "http", else the first port.
func pickServicePort(ports []corev1.ServicePort) int {
	for _, p := range ports {
		if p.Name == "http" {
			return int(p.Port)
		}
	}
	return int(ports[0].Port)
}

// inClusterNamespace reads the pod's namespace from the mounted service account.
func inClusterNamespace() string {
	if data, err := os.ReadFile(serviceAccountNamespaceFile); err == nil {
		return strings.TrimSpace(string(data))
	}
	return ""
}
