package ai

import (
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/ai/internal"
)

// DI container keys for AI extension services.
const (
	// AgentManagerKey is the DI key for the agent manager.
	AgentManagerKey = "forge.ai.agentManager"
	// AgentFactoryKey is the DI key for the agent factory.
	AgentFactoryKey = "forge.ai.agentFactory"
	// LLMManagerKey is the DI key for the LLM manager.
	LLMManagerKey = "forge.ai.llmManager"
	// SDKLLMManagerKey is the DI key for the SDK LLM manager interface.
	SDKLLMManagerKey = "forge.ai.sdk.llmManager"
	// StateStoreKey is the DI key for the AI SDK state store.
	StateStoreKey = "forge.ai.sdk.stateStore"
	// VectorStoreKey is the DI key for the AI SDK vector store.
	VectorStoreKey = "forge.ai.sdk.vectorStore"
	// VectorStoreKeyLegacy is the legacy DI key for the AI SDK vector store.
	VectorStoreKeyLegacy = "vectorStore"
	
	// Training service keys
	// ModelTrainerKey is the DI key for the model trainer.
	ModelTrainerKey = "forge.ai.training.modelTrainer"
	// DataManagerKey is the DI key for the data manager.
	DataManagerKey = "forge.ai.training.dataManager"
	// PipelineManagerKey is the DI key for the pipeline manager.
	PipelineManagerKey = "forge.ai.training.pipelineManager"
)

// Config is the public configuration for the AI extension.
type Config struct {
	// Core features
	EnableLLM          bool          `json:"enable_llm"          yaml:"enable_llm"`
	EnableAgents       bool          `json:"enable_agents"       yaml:"enable_agents"`
	EnableTraining     bool          `json:"enable_training"     yaml:"enable_training"`
	EnableInference    bool          `json:"enable_inference"    yaml:"enable_inference"`
	EnableCoordination bool          `json:"enable_coordination" yaml:"enable_coordination"`
	MaxConcurrency     int           `json:"max_concurrency"     yaml:"max_concurrency"`
	RequestTimeout     time.Duration `json:"request_timeout"     yaml:"request_timeout"`
	CacheSize          int           `json:"cache_size"          yaml:"cache_size"`

	// LLM configuration
	LLM LLMConfiguration `json:"llm" yaml:"llm"`

	// Inference configuration
	Inference InferenceConfiguration `json:"inference" yaml:"inference"`

	// Agent configuration
	Agents AgentConfiguration `json:"agents" yaml:"agents"`

	// Middleware configuration
	Middleware MiddlewareConfiguration `json:"middleware" yaml:"middleware"`

	// Store configurations
	StateStore  StateStoreConfig  `json:"state_store"  yaml:"state_store"`
	VectorStore VectorStoreConfig `json:"vector_store" yaml:"vector_store"`

	// Training configuration
	Training TrainingConfiguration `json:"training" yaml:"training"`
}

// LLMConfiguration contains LLM-specific settings.
type LLMConfiguration struct {
	DefaultProvider string                    `json:"default_provider" yaml:"default_provider"`
	Providers       map[string]ProviderConfig `json:"providers"        yaml:"providers"`
	MaxRetries      int                       `json:"max_retries"      yaml:"max_retries"`
	RetryDelay      time.Duration             `json:"retry_delay"      yaml:"retry_delay"`
	Timeout         time.Duration             `json:"timeout"          yaml:"timeout"`
}

// ProviderConfig defines configuration for an LLM provider.
type ProviderConfig struct {
	Type    string         `json:"type"     yaml:"type"`
	APIKey  string         `json:"api_key"  yaml:"api_key"`
	BaseURL string         `json:"base_url" yaml:"base_url"`
	Models  []string       `json:"models"   yaml:"models"`
	Options map[string]any `json:"options"  yaml:"options"`
}

// InferenceConfiguration contains inference engine settings.
type InferenceConfiguration struct {
	Workers        int           `json:"workers"         yaml:"workers"`
	BatchSize      int           `json:"batch_size"      yaml:"batch_size"`
	BatchTimeout   time.Duration `json:"batch_timeout"   yaml:"batch_timeout"`
	CacheSize      int           `json:"cache_size"      yaml:"cache_size"`
	CacheTTL       time.Duration `json:"cache_ttl"       yaml:"cache_ttl"`
	EnableBatching bool          `json:"enable_batching" yaml:"enable_batching"`
	EnableCaching  bool          `json:"enable_caching"  yaml:"enable_caching"`
	EnableScaling  bool          `json:"enable_scaling"  yaml:"enable_scaling"`
}

// AgentConfiguration contains agent settings.
type AgentConfiguration struct {
	EnabledAgents []string               `json:"enabled_agents" yaml:"enabled_agents"`
	AgentConfigs  map[string]interface{} `json:"agent_configs"  yaml:"agent_configs"`
}

// MiddlewareConfiguration contains middleware settings.
type MiddlewareConfiguration struct {
	EnabledMiddleware []string                               `json:"enabled_middleware" yaml:"enabled_middleware"`
	MiddlewareConfigs map[string]internal.AIMiddlewareConfig `json:"middleware_configs" yaml:"middleware_configs"`
}

// StateStoreConfig configures conversation state persistence.
type StateStoreConfig struct {
	Type     string               `json:"type"     yaml:"type"` // memory, postgres, redis
	Memory   *MemoryStateConfig   `json:"memory"   yaml:"memory"`
	Postgres *PostgresStateConfig `json:"postgres" yaml:"postgres"`
	Redis    *RedisStateConfig    `json:"redis"    yaml:"redis"`
}

// MemoryStateConfig for in-memory state store.
type MemoryStateConfig struct {
	TTL time.Duration `json:"ttl" yaml:"ttl"` // Auto-cleanup TTL (0 = no cleanup)
}

// PostgresStateConfig for PostgreSQL state store.
type PostgresStateConfig struct {
	ConnString string `json:"connection_string" yaml:"connection_string"`
	TableName  string `json:"table_name"        yaml:"table_name"`
}

// RedisStateConfig for Redis state store.
type RedisStateConfig struct {
	Addrs    []string `json:"addrs"    yaml:"addrs"`
	Password string   `json:"password" yaml:"password"`
	DB       int      `json:"db"       yaml:"db"`
}

// VectorStoreConfig configures vector embeddings storage.
type VectorStoreConfig struct {
	Type     string                 `json:"type"     yaml:"type"` // memory, postgres, pinecone, weaviate
	Memory   *MemoryVectorConfig    `json:"memory"   yaml:"memory"`
	Postgres *PostgresVectorConfig  `json:"postgres" yaml:"postgres"`
	Pinecone *PineconeVectorConfig  `json:"pinecone" yaml:"pinecone"`
	Weaviate *WeaviateVectorConfig  `json:"weaviate" yaml:"weaviate"`
}

// MemoryVectorConfig for in-memory vector store.
type MemoryVectorConfig struct {
	// No additional config needed - purely in-memory
}

// PostgresVectorConfig for pgvector.
type PostgresVectorConfig struct {
	ConnString string `json:"connection_string" yaml:"connection_string"`
	TableName  string `json:"table_name"        yaml:"table_name"`
	Dimensions int    `json:"dimensions"        yaml:"dimensions"`
}

// PineconeVectorConfig for Pinecone cloud.
type PineconeVectorConfig struct {
	APIKey      string `json:"api_key"      yaml:"api_key"`
	Environment string `json:"environment"  yaml:"environment"`
	IndexName   string `json:"index_name"   yaml:"index_name"`
}

// WeaviateVectorConfig for Weaviate.
type WeaviateVectorConfig struct {
	Scheme    string `json:"scheme"     yaml:"scheme"`
	Host      string `json:"host"       yaml:"host"`
	APIKey    string `json:"api_key"    yaml:"api_key"`
	ClassName string `json:"class_name" yaml:"class_name"`
}

// TrainingConfiguration contains training-specific settings.
type TrainingConfiguration struct {
	Enabled         bool              `json:"enabled"           yaml:"enabled"`
	CheckpointPath  string            `json:"checkpoint_path"   yaml:"checkpoint_path"`
	ModelPath       string            `json:"model_path"        yaml:"model_path"`
	DataPath        string            `json:"data_path"         yaml:"data_path"`
	MaxConcurrentJobs int             `json:"max_concurrent_jobs" yaml:"max_concurrent_jobs"`
	DefaultResources ResourcesConfig  `json:"default_resources" yaml:"default_resources"`
	Storage         StorageConfig     `json:"storage"           yaml:"storage"`
}

// ResourcesConfig defines default resource limits for training jobs.
type ResourcesConfig struct {
	CPU        string        `json:"cpu"         yaml:"cpu"`
	Memory     string        `json:"memory"      yaml:"memory"`
	GPU        int           `json:"gpu"         yaml:"gpu"`
	Timeout    time.Duration `json:"timeout"     yaml:"timeout"`
	Priority   int           `json:"priority"    yaml:"priority"`
}

// StorageConfig defines storage backend configuration.
type StorageConfig struct {
	Type       string                 `json:"type"        yaml:"type"` // local, s3, gcs, azure
	Local      *LocalStorageConfig    `json:"local"       yaml:"local"`
	S3         *S3StorageConfig       `json:"s3"          yaml:"s3"`
	GCS        *GCSStorageConfig      `json:"gcs"         yaml:"gcs"`
	Azure      *AzureStorageConfig    `json:"azure"       yaml:"azure"`
}

// LocalStorageConfig for local filesystem storage.
type LocalStorageConfig struct {
	BasePath string `json:"base_path" yaml:"base_path"`
}

// S3StorageConfig for AWS S3 storage.
type S3StorageConfig struct {
	Bucket    string `json:"bucket"     yaml:"bucket"`
	Region    string `json:"region"     yaml:"region"`
	AccessKey string `json:"access_key" yaml:"access_key"`
	SecretKey string `json:"secret_key" yaml:"secret_key"`
	Prefix    string `json:"prefix"     yaml:"prefix"`
}

// GCSStorageConfig for Google Cloud Storage.
type GCSStorageConfig struct {
	Bucket         string `json:"bucket"           yaml:"bucket"`
	ProjectID      string `json:"project_id"       yaml:"project_id"`
	CredentialsFile string `json:"credentials_file" yaml:"credentials_file"`
	Prefix         string `json:"prefix"           yaml:"prefix"`
}

// AzureStorageConfig for Azure Blob Storage.
type AzureStorageConfig struct {
	Account   string `json:"account"    yaml:"account"`
	Container string `json:"container"  yaml:"container"`
	AccessKey string `json:"access_key" yaml:"access_key"`
	Prefix    string `json:"prefix"     yaml:"prefix"`
}

// DefaultConfig returns the default AI configuration.
func DefaultConfig() Config {
	return Config{
		EnableLLM:          true,
		EnableAgents:       true,
		EnableTraining:     false,
		EnableInference:    true,
		EnableCoordination: true,
		MaxConcurrency:     10,
		RequestTimeout:     30 * time.Second,
		CacheSize:          1000,
		LLM: LLMConfiguration{
			DefaultProvider: "lmstudio",
			Providers: map[string]ProviderConfig{
				"lmstudio": {
					Type:    "lmstudio",
					APIKey:  "",
					BaseURL: "http://localhost:1234/v1",
					Models:  []string{},
				},
			},
			MaxRetries: 3,
			RetryDelay: time.Second,
			Timeout:    30 * time.Second,
		},
		Agents: AgentConfiguration{
			EnabledAgents: []string{},
			AgentConfigs:  make(map[string]interface{}),
		},
		Middleware: MiddlewareConfiguration{
			EnabledMiddleware: []string{},
			MiddlewareConfigs: make(map[string]internal.AIMiddlewareConfig),
		},
		StateStore: StateStoreConfig{
			Type: "memory", // Dev-friendly default
			Memory: &MemoryStateConfig{
				TTL: 24 * time.Hour,
			},
		},
		VectorStore: VectorStoreConfig{
			Type: "memory", // Dev-friendly default
			Memory: &MemoryVectorConfig{},
		},
		Training: TrainingConfiguration{
			Enabled:           false, // Disabled by default
			CheckpointPath:    "./checkpoints",
			ModelPath:         "./models",
			DataPath:          "./data",
			MaxConcurrentJobs: 5,
			DefaultResources: ResourcesConfig{
				CPU:      "4",
				Memory:   "8Gi",
				GPU:      0,
				Timeout:  4 * time.Hour,
				Priority: 1,
			},
			Storage: StorageConfig{
				Type: "local",
				Local: &LocalStorageConfig{
					BasePath: "./training-storage",
				},
			},
		},
	}
}

// ToInternal converts public Config to internal AIConfig.
func (c Config) ToInternal(logger forge.Logger, metrics forge.Metrics) internal.AIConfig {
	return internal.AIConfig{
		EnableLLM:          c.EnableLLM,
		EnableAgents:       c.EnableAgents,
		EnableTraining:     c.EnableTraining,
		EnableInference:    c.EnableInference,
		EnableCoordination: c.EnableCoordination,
		MaxConcurrency:     c.MaxConcurrency,
		RequestTimeout:     c.RequestTimeout,
		CacheSize:          c.CacheSize,
		Logger:             logger,
		Metrics:            metrics,
	}
}

// ConfigOption is a functional option for Config.
type ConfigOption func(*Config)

// WithConfig replaces the entire config.
func WithConfig(config Config) ConfigOption {
	return func(c *Config) { *c = config }
}

// WithEnableLLM sets whether LLM is enabled.
func WithEnableLLM(enabled bool) ConfigOption {
	return func(c *Config) { c.EnableLLM = enabled }
}

// WithEnableAgents sets whether agents are enabled.
func WithEnableAgents(enabled bool) ConfigOption {
	return func(c *Config) { c.EnableAgents = enabled }
}

// WithEnableTraining sets whether training is enabled.
func WithEnableTraining(enabled bool) ConfigOption {
	return func(c *Config) { c.EnableTraining = enabled }
}

// WithEnableInference sets whether inference is enabled.
func WithEnableInference(enabled bool) ConfigOption {
	return func(c *Config) { c.EnableInference = enabled }
}

// WithEnableCoordination sets whether coordination is enabled.
func WithEnableCoordination(enabled bool) ConfigOption {
	return func(c *Config) { c.EnableCoordination = enabled }
}

// WithMaxConcurrency sets the maximum concurrency.
func WithMaxConcurrency(max int) ConfigOption {
	return func(c *Config) { c.MaxConcurrency = max }
}

// WithRequestTimeout sets the request timeout.
func WithRequestTimeout(timeout time.Duration) ConfigOption {
	return func(c *Config) { c.RequestTimeout = timeout }
}

// WithCacheSize sets the cache size.
func WithCacheSize(size int) ConfigOption {
	return func(c *Config) { c.CacheSize = size }
}

// WithLLMConfig sets the LLM configuration.
func WithLLMConfig(llm LLMConfiguration) ConfigOption {
	return func(c *Config) { c.LLM = llm }
}

// WithInferenceConfig sets the inference configuration.
func WithInferenceConfig(inference InferenceConfiguration) ConfigOption {
	return func(c *Config) { c.Inference = inference }
}

// WithAgentsConfig sets the agents configuration.
func WithAgentsConfig(agents AgentConfiguration) ConfigOption {
	return func(c *Config) { c.Agents = agents }
}

// WithMiddlewareConfig sets the middleware configuration.
func WithMiddlewareConfig(middleware MiddlewareConfiguration) ConfigOption {
	return func(c *Config) { c.Middleware = middleware }
}

// WithTrainingConfig sets the training configuration.
func WithTrainingConfig(training TrainingConfiguration) ConfigOption {
	return func(c *Config) { c.Training = training }
}

// WithStateStore sets the state store configuration.
func WithStateStore(store StateStoreConfig) ConfigOption {
	return func(c *Config) { c.StateStore = store }
}

// WithVectorStore sets the vector store configuration.
func WithVectorStore(store VectorStoreConfig) ConfigOption {
	return func(c *Config) { c.VectorStore = store }
}
