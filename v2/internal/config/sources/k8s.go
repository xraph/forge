package sources

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	json "github.com/json-iterator/go"
	configcore "github.com/xraph/forge/v2/internal/config/core"
	"github.com/xraph/forge/v2/internal/errors"
	"github.com/xraph/forge/v2/internal/logger"
	"github.com/xraph/forge/v2/internal/shared"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

// K8sSource represents a Kubernetes ConfigMap/Secret configuration source
type K8sSource struct {
	name           string
	client         kubernetes.Interface
	namespace      string
	configMapNames []string
	secretNames    []string
	priority       int
	watching       bool
	watchStop      chan struct{}
	logger         logger.Logger
	errorHandler   shared.ErrorHandler
	options        K8sSourceOptions
	mu             sync.RWMutex
}

// K8sSourceOptions contains options for Kubernetes configuration sources
type K8sSourceOptions struct {
	Name           string
	Namespace      string
	ConfigMapNames []string
	SecretNames    []string
	Priority       int
	WatchEnabled   bool
	KubeConfig     string
	InCluster      bool
	LabelSelector  string
	FieldSelector  string
	RetryCount     int
	RetryDelay     time.Duration
	Logger         logger.Logger
	ErrorHandler   shared.ErrorHandler
}

// K8sSourceConfig contains configuration for creating Kubernetes sources
type K8sSourceConfig struct {
	Namespace      string        `yaml:"namespace" json:"namespace"`
	ConfigMapNames []string      `yaml:"configmap_names" json:"configmap_names"`
	SecretNames    []string      `yaml:"secret_names" json:"secret_names"`
	Priority       int           `yaml:"priority" json:"priority"`
	WatchEnabled   bool          `yaml:"watch_enabled" json:"watch_enabled"`
	KubeConfig     string        `yaml:"kubeconfig" json:"kubeconfig"`
	InCluster      bool          `yaml:"in_cluster" json:"in_cluster"`
	LabelSelector  string        `yaml:"label_selector" json:"label_selector"`
	FieldSelector  string        `yaml:"field_selector" json:"field_selector"`
	RetryCount     int           `yaml:"retry_count" json:"retry_count"`
	RetryDelay     time.Duration `yaml:"retry_delay" json:"retry_delay"`
}

// NewK8sSource creates a new Kubernetes configuration source
func NewK8sSource(options K8sSourceOptions) (configcore.ConfigSource, error) {
	if options.Namespace == "" {
		options.Namespace = "default"
	}

	if options.RetryCount == 0 {
		options.RetryCount = 3
	}

	if options.RetryDelay == 0 {
		options.RetryDelay = 5 * time.Second
	}

	name := options.Name
	if name == "" {
		name = fmt.Sprintf("k8s:%s", options.Namespace)
	}

	// Create Kubernetes client
	var config *rest.Config
	var err error

	if options.InCluster {
		// Use in-cluster configuration
		config, err = rest.InClusterConfig()
	} else {
		// Use kubeconfig file
		kubeconfig := options.KubeConfig
		if kubeconfig == "" {
			if home := homedir.HomeDir(); home != "" {
				kubeconfig = filepath.Join(home, ".kube", "config")
			}
		}

		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
	}

	if err != nil {
		return nil, errors.ErrConfigError(fmt.Sprintf("failed to create Kubernetes config: %v", err), err)
	}

	// Create Kubernetes clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, errors.ErrConfigError(fmt.Sprintf("failed to create Kubernetes client: %v", err), err)
	}

	source := &K8sSource{
		name:           name,
		client:         clientset,
		namespace:      options.Namespace,
		configMapNames: options.ConfigMapNames,
		secretNames:    options.SecretNames,
		priority:       options.Priority,
		logger:         options.Logger,
		errorHandler:   options.ErrorHandler,
		options:        options,
	}

	// Test connection
	if err := source.testConnection(context.Background()); err != nil {
		return nil, errors.ErrConfigError(fmt.Sprintf("failed to connect to Kubernetes: %v", err), err)
	}

	return source, nil
}

// Name returns the source name
func (ks *K8sSource) Name() string {
	return ks.name
}

// GetName returns the source name (alias for Name)
func (ks *K8sSource) GetName() string {
	return ks.name
}

// GetType returns the source type
func (ks *K8sSource) GetType() string {
	return "kubernetes"
}

// IsAvailable checks if the source is available
func (ks *K8sSource) IsAvailable(ctx context.Context) bool {
	// TODO: Implement actual Kubernetes availability check
	return true
}

// Priority returns the source priority
func (ks *K8sSource) Priority() int {
	return ks.priority
}

// Load loads configuration from Kubernetes ConfigMaps and Secrets
func (ks *K8sSource) Load(ctx context.Context) (map[string]interface{}, error) {
	if ks.logger != nil {
		ks.logger.Debug("loading configuration from Kubernetes",
			logger.String("namespace", ks.namespace),
			logger.String("configmaps", fmt.Sprintf("%v", ks.configMapNames)),
			logger.String("secrets", fmt.Sprintf("%v", ks.secretNames)),
		)
	}

	config := make(map[string]interface{})

	// Load ConfigMaps
	if err := ks.loadConfigMaps(ctx, config); err != nil {
		return nil, err
	}

	// Load Secrets
	if err := ks.loadSecrets(ctx, config); err != nil {
		return nil, err
	}

	if ks.logger != nil {
		ks.logger.Info("configuration loaded from Kubernetes",
			logger.String("namespace", ks.namespace),
			logger.Int("keys", len(config)),
		)
	}

	return config, nil
}

// Watch starts watching Kubernetes ConfigMaps and Secrets for changes
func (ks *K8sSource) Watch(ctx context.Context, callback func(map[string]interface{})) error {
	ks.mu.Lock()
	defer ks.mu.Unlock()

	if ks.watching {
		return errors.ErrConfigError("already watching Kubernetes resources", nil)
	}

	if !ks.IsWatchable() {
		return errors.ErrConfigError("Kubernetes watching is not enabled", nil)
	}

	ks.watchStop = make(chan struct{})
	ks.watching = true

	// Start watching goroutines
	go ks.watchConfigMaps(ctx, callback)
	go ks.watchSecrets(ctx, callback)

	if ks.logger != nil {
		ks.logger.Info("started watching Kubernetes resources",
			logger.String("namespace", ks.namespace),
		)
	}

	return nil
}

// StopWatch stops watching Kubernetes resources
func (ks *K8sSource) StopWatch() error {
	ks.mu.Lock()
	defer ks.mu.Unlock()

	if !ks.watching {
		return nil
	}

	if ks.watchStop != nil {
		close(ks.watchStop)
		ks.watchStop = nil
	}

	ks.watching = false

	if ks.logger != nil {
		ks.logger.Info("stopped watching Kubernetes resources",
			logger.String("namespace", ks.namespace),
		)
	}

	return nil
}

// Reload forces a reload of Kubernetes configuration
func (ks *K8sSource) Reload(ctx context.Context) error {
	if ks.logger != nil {
		ks.logger.Info("reloading Kubernetes configuration",
			logger.String("namespace", ks.namespace),
		)
	}

	// Just load again - the configuration will be updated
	_, err := ks.Load(ctx)
	return err
}

// IsWatchable returns true if Kubernetes watching is enabled
func (ks *K8sSource) IsWatchable() bool {
	return ks.options.WatchEnabled
}

// SupportsSecrets returns true (Kubernetes can store secrets)
func (ks *K8sSource) SupportsSecrets() bool {
	return len(ks.secretNames) > 0
}

// GetSecret retrieves a secret from Kubernetes
func (ks *K8sSource) GetSecret(ctx context.Context, key string) (string, error) {
	// Parse key format: secret-name/key or secret-name.key
	parts := strings.SplitN(key, "/", 2)
	if len(parts) == 1 {
		parts = strings.SplitN(key, ".", 2)
	}

	if len(parts) != 2 {
		return "", errors.ErrConfigError(fmt.Sprintf("invalid secret key format: %s (expected secret-name/key)", key), nil)
	}

	secretName, secretKey := parts[0], parts[1]

	secret, err := ks.client.CoreV1().Secrets(ks.namespace).Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		return "", errors.ErrConfigError(fmt.Sprintf("failed to get secret from Kubernetes: %v", err), err)
	}

	if secret.Data == nil {
		return "", errors.ErrConfigError(fmt.Sprintf("secret data is nil: %s", secretName), nil)
	}

	data, exists := secret.Data[secretKey]
	if !exists {
		return "", errors.ErrConfigError(fmt.Sprintf("secret key not found: %s in %s", secretKey, secretName), nil)
	}

	return string(data), nil
}

// loadConfigMaps loads configuration from ConfigMaps
func (ks *K8sSource) loadConfigMaps(ctx context.Context, config map[string]interface{}) error {
	if len(ks.configMapNames) == 0 {
		// If no specific names, try to discover based on selectors
		return ks.loadConfigMapsWithSelectors(ctx, config)
	}

	// Load specific ConfigMaps
	for _, name := range ks.configMapNames {
		configMap, err := ks.client.CoreV1().ConfigMaps(ks.namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			if ks.logger != nil {
				ks.logger.Warn("failed to get ConfigMap",
					logger.String("name", name),
					logger.String("namespace", ks.namespace),
					logger.Error(err),
				)
			}
			continue
		}

		ks.parseConfigMap(configMap, config)
	}

	return nil
}

// loadConfigMapsWithSelectors loads ConfigMaps using label/field selectors
func (ks *K8sSource) loadConfigMapsWithSelectors(ctx context.Context, config map[string]interface{}) error {
	listOptions := metav1.ListOptions{}

	if ks.options.LabelSelector != "" {
		listOptions.LabelSelector = ks.options.LabelSelector
	}

	if ks.options.FieldSelector != "" {
		listOptions.FieldSelector = ks.options.FieldSelector
	}

	configMaps, err := ks.client.CoreV1().ConfigMaps(ks.namespace).List(ctx, listOptions)
	if err != nil {
		return errors.ErrConfigError(fmt.Sprintf("failed to list ConfigMaps: %v", err), err)
	}

	for _, configMap := range configMaps.Items {
		ks.parseConfigMap(&configMap, config)
	}

	return nil
}

// loadSecrets loads configuration from Secrets
func (ks *K8sSource) loadSecrets(ctx context.Context, config map[string]interface{}) error {
	for _, name := range ks.secretNames {
		secret, err := ks.client.CoreV1().Secrets(ks.namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			if ks.logger != nil {
				ks.logger.Warn("failed to get Secret",
					logger.String("name", name),
					logger.String("namespace", ks.namespace),
					logger.Error(err),
				)
			}
			continue
		}

		ks.parseSecret(secret, config)
	}

	return nil
}

// parseConfigMap parses a ConfigMap into configuration data
func (ks *K8sSource) parseConfigMap(configMap *corev1.ConfigMap, config map[string]interface{}) {
	if configMap.Data == nil {
		return
	}

	// Use ConfigMap name as a namespace in the configuration
	cmConfig := make(map[string]interface{})

	for key, value := range configMap.Data {
		// Try to parse value as different formats
		parsedValue := ks.parseValue([]byte(value))
		ks.setNestedValue(cmConfig, key, parsedValue)
	}

	// Merge into main config under ConfigMap name
	config[configMap.Name] = cmConfig
}

// parseSecret parses a Secret into configuration data
func (ks *K8sSource) parseSecret(secret *corev1.Secret, config map[string]interface{}) {
	if secret.Data == nil {
		return
	}

	// Use Secret name as a namespace in the configuration
	secretConfig := make(map[string]interface{})

	for key, value := range secret.Data {
		// Secrets are binary data, convert to string
		parsedValue := ks.parseValue(value)
		ks.setNestedValue(secretConfig, key, parsedValue)
	}

	// Merge into main config under secrets namespace
	if config["secrets"] == nil {
		config["secrets"] = make(map[string]interface{})
	}

	if secretsConfig, ok := config["secrets"].(map[string]interface{}); ok {
		secretsConfig[secret.Name] = secretConfig
	}
}

// watchConfigMaps watches for ConfigMap changes
func (ks *K8sSource) watchConfigMaps(ctx context.Context, callback func(map[string]interface{})) {
	defer func() {
		if r := recover(); r != nil {
			if ks.logger != nil {
				ks.logger.Error("panic in Kubernetes ConfigMap watch loop",
					logger.String("namespace", ks.namespace),
					logger.Any("panic", r),
				)
			}
		}
	}()

	retryCount := 0

	for {
		select {
		case <-ctx.Done():
			return
		case <-ks.watchStop:
			return
		default:
		}

		var fieldSelector string
		if len(ks.configMapNames) > 0 {
			// Watch specific ConfigMaps
			fieldSelector = fields.OneTermEqualSelector("metadata.name", strings.Join(ks.configMapNames, ",")).String()
		} else if ks.options.FieldSelector != "" {
			fieldSelector = ks.options.FieldSelector
		}

		watchOptions := metav1.ListOptions{
			Watch:         true,
			FieldSelector: fieldSelector,
		}

		if ks.options.LabelSelector != "" {
			watchOptions.LabelSelector = ks.options.LabelSelector
		}

		watcher, err := ks.client.CoreV1().ConfigMaps(ks.namespace).Watch(ctx, watchOptions)
		if err != nil {
			retryCount++
			if retryCount <= ks.options.RetryCount {
				if ks.logger != nil {
					ks.logger.Warn("ConfigMap watch error, retrying",
						logger.String("namespace", ks.namespace),
						logger.Int("attempt", retryCount),
						logger.Error(err),
					)
				}

				select {
				case <-ctx.Done():
					return
				case <-ks.watchStop:
					return
				case <-time.After(ks.options.RetryDelay):
					continue
				}
			} else {
				ks.handleWatchError(err)
				return
			}
		}

		// Reset retry count on successful watch
		retryCount = 0

		// Process watch events
		for event := range watcher.ResultChan() {
			select {
			case <-ctx.Done():
				watcher.Stop()
				return
			case <-ks.watchStop:
				watcher.Stop()
				return
			default:
			}

			if event.Type == watch.Modified || event.Type == watch.Added || event.Type == watch.Deleted {
				if ks.logger != nil {
					ks.logger.Info("Kubernetes ConfigMap change detected",
						logger.String("namespace", ks.namespace),
						logger.String("event_type", string(event.Type)),
					)
				}

				// Reload all configuration and notify callback
				if config, err := ks.Load(ctx); err == nil {
					if callback != nil {
						go func() {
							defer func() {
								if r := recover(); r != nil {
									if ks.logger != nil {
										ks.logger.Error("panic in Kubernetes watch callback",
											logger.Any("panic", r),
										)
									}
								}
							}()

							callback(config)
						}()
					}
				}
			}
		}

		// Watch connection closed, retry
		if ks.logger != nil {
			ks.logger.Warn("ConfigMap watch connection closed, retrying",
				logger.String("namespace", ks.namespace),
			)
		}
	}
}

// watchSecrets watches for Secret changes
func (ks *K8sSource) watchSecrets(ctx context.Context, callback func(map[string]interface{})) {
	if len(ks.secretNames) == 0 {
		return
	}

	defer func() {
		if r := recover(); r != nil {
			if ks.logger != nil {
				ks.logger.Error("panic in Kubernetes Secret watch loop",
					logger.String("namespace", ks.namespace),
					logger.Any("panic", r),
				)
			}
		}
	}()

	retryCount := 0

	for {
		select {
		case <-ctx.Done():
			return
		case <-ks.watchStop:
			return
		default:
		}

		fieldSelector := fields.OneTermEqualSelector("metadata.name", strings.Join(ks.secretNames, ",")).String()

		watchOptions := metav1.ListOptions{
			Watch:         true,
			FieldSelector: fieldSelector,
		}

		watcher, err := ks.client.CoreV1().Secrets(ks.namespace).Watch(ctx, watchOptions)
		if err != nil {
			retryCount++
			if retryCount <= ks.options.RetryCount {
				if ks.logger != nil {
					ks.logger.Warn("Secret watch error, retrying",
						logger.String("namespace", ks.namespace),
						logger.Int("attempt", retryCount),
						logger.Error(err),
					)
				}

				select {
				case <-ctx.Done():
					return
				case <-ks.watchStop:
					return
				case <-time.After(ks.options.RetryDelay):
					continue
				}
			} else {
				ks.handleWatchError(err)
				return
			}
		}

		// Reset retry count on successful watch
		retryCount = 0

		// Process watch events
		for event := range watcher.ResultChan() {
			select {
			case <-ctx.Done():
				watcher.Stop()
				return
			case <-ks.watchStop:
				watcher.Stop()
				return
			default:
			}

			if event.Type == watch.Modified || event.Type == watch.Added || event.Type == watch.Deleted {
				if ks.logger != nil {
					ks.logger.Info("Kubernetes Secret change detected",
						logger.String("namespace", ks.namespace),
						logger.String("event_type", string(event.Type)),
					)
				}

				// Reload all configuration and notify callback
				if config, err := ks.Load(ctx); err == nil {
					if callback != nil {
						go func() {
							defer func() {
								if r := recover(); r != nil {
									if ks.logger != nil {
										ks.logger.Error("panic in Kubernetes watch callback",
											logger.Any("panic", r),
										)
									}
								}
							}()

							callback(config)
						}()
					}
				}
			}
		}

		// Watch connection closed, retry
		if ks.logger != nil {
			ks.logger.Warn("Secret watch connection closed, retrying",
				logger.String("namespace", ks.namespace),
			)
		}
	}
}

// testConnection tests the connection to Kubernetes
func (ks *K8sSource) testConnection(ctx context.Context) error {
	_, err := ks.client.CoreV1().Namespaces().Get(ctx, ks.namespace, metav1.GetOptions{})
	return err
}

// parseValue parses a Kubernetes value, attempting JSON first, then treating as string
func (ks *K8sSource) parseValue(data []byte) interface{} {
	if len(data) == 0 {
		return ""
	}

	// Convert to string first
	str := string(data)

	// Try to parse as JSON if it looks like JSON
	if (strings.HasPrefix(str, "{") && strings.HasSuffix(str, "}")) ||
		(strings.HasPrefix(str, "[") && strings.HasSuffix(str, "]")) {

		var jsonValue interface{}
		if err := json.Unmarshal(data, &jsonValue); err == nil {
			return jsonValue
		}
	}

	// Check for YAML format (simple detection)
	if strings.Contains(str, ":\n") || strings.Contains(str, ": ") {
		// Could be YAML, but for now just return as string
		// You could integrate a YAML parser here
		return str
	}

	// Return as string
	return str
}

// setNestedValue sets a nested configuration value using dot notation
func (ks *K8sSource) setNestedValue(config map[string]interface{}, key string, value interface{}) {
	keys := strings.Split(key, ".")
	current := config

	for i, k := range keys {
		if i == len(keys)-1 {
			// Last key - set the value
			current[k] = value
		} else {
			// Intermediate key - ensure map exists
			if _, exists := current[k]; !exists {
				current[k] = make(map[string]interface{})
			}

			if nextMap, ok := current[k].(map[string]interface{}); ok {
				current = nextMap
			} else {
				// Type conflict - create new map
				current[k] = make(map[string]interface{})
				current = current[k].(map[string]interface{})
			}
		}
	}
}

// handleWatchError handles errors during watching
func (ks *K8sSource) handleWatchError(err error) {
	if ks.logger != nil {
		ks.logger.Error("Kubernetes watch error",
			logger.String("namespace", ks.namespace),
			logger.Error(err),
		)
	}

	if ks.errorHandler != nil {
		ks.errorHandler.HandleError(nil, errors.ErrConfigError(fmt.Sprintf("Kubernetes watch error for namespace %s", ks.namespace), err))
	}

	// Stop watching on persistent errors
	ks.StopWatch()
}

// K8sSourceFactory creates Kubernetes configuration sources
type K8sSourceFactory struct {
	logger       logger.Logger
	errorHandler shared.ErrorHandler
}

// NewK8sSourceFactory creates a new Kubernetes source factory
func NewK8sSourceFactory(logger logger.Logger, errorHandler shared.ErrorHandler) *K8sSourceFactory {
	return &K8sSourceFactory{
		logger:       logger,
		errorHandler: errorHandler,
	}
}

// CreateFromConfig creates a Kubernetes source from configuration
func (factory *K8sSourceFactory) CreateFromConfig(config K8sSourceConfig) (configcore.ConfigSource, error) {
	options := K8sSourceOptions{
		Name:           fmt.Sprintf("k8s:%s", config.Namespace),
		Namespace:      config.Namespace,
		ConfigMapNames: config.ConfigMapNames,
		SecretNames:    config.SecretNames,
		Priority:       config.Priority,
		WatchEnabled:   config.WatchEnabled,
		KubeConfig:     config.KubeConfig,
		InCluster:      config.InCluster,
		LabelSelector:  config.LabelSelector,
		FieldSelector:  config.FieldSelector,
		RetryCount:     config.RetryCount,
		RetryDelay:     config.RetryDelay,
		Logger:         factory.logger,
		ErrorHandler:   factory.errorHandler,
	}

	return NewK8sSource(options)
}

// CreateWithDefaults creates a Kubernetes source with default settings
func (factory *K8sSourceFactory) CreateWithDefaults(namespace string) (configcore.ConfigSource, error) {
	options := K8sSourceOptions{
		Namespace:    namespace,
		Priority:     150, // Between files and consul
		WatchEnabled: true,
		InCluster:    true, // Default to in-cluster when running in K8s
		RetryCount:   3,
		RetryDelay:   5 * time.Second,
		Logger:       factory.logger,
		ErrorHandler: factory.errorHandler,
	}

	return NewK8sSource(options)
}
