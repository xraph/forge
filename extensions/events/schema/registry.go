package schema

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/internal/errors"
	"github.com/xraph/forge/internal/logger"
)

// SchemaRegistry manages event schemas for validation and evolution.
type SchemaRegistry struct {
	schemas     map[string]map[int]*Schema // event_type -> version -> schema
	schemasByID map[string]*Schema         // schema_id -> schema
	store       SchemaStore
	cache       SchemaCache
	config      *RegistryConfig
	logger      forge.Logger
	metrics     forge.Metrics
	mu          sync.RWMutex
	validator   *EventSchemaValidator
}

// RegistryConfig defines configuration for the schema registry.
type RegistryConfig struct {
	EnableCache        bool          `json:"enable_cache"        yaml:"enable_cache"`
	CacheTTL           time.Duration `json:"cache_ttl"           yaml:"cache_ttl"`
	AutoRegister       bool          `json:"auto_register"       yaml:"auto_register"`
	VersioningStrategy string        `json:"versioning_strategy" yaml:"versioning_strategy"` // "strict", "compatible", "none"
	ValidationLevel    string        `json:"validation_level"    yaml:"validation_level"`    // "strict", "lenient", "disabled"
	AllowEvolution     bool          `json:"allow_evolution"     yaml:"allow_evolution"`
	MaxVersions        int           `json:"max_versions"        yaml:"max_versions"`
}

// DefaultRegistryConfig returns default configuration.
func DefaultRegistryConfig() *RegistryConfig {
	return &RegistryConfig{
		EnableCache:        true,
		CacheTTL:           time.Hour,
		AutoRegister:       false,
		VersioningStrategy: "compatible",
		ValidationLevel:    "lenient",
		AllowEvolution:     true,
		MaxVersions:        10,
	}
}

// SchemaStore defines interface for persisting schemas.
type SchemaStore interface {
	// SaveSchema saves a schema to persistent storage
	SaveSchema(ctx context.Context, schema *Schema) error

	// GetSchema retrieves a schema by type and version
	GetSchema(ctx context.Context, eventType string, version int) (*Schema, error)

	// GetSchemaByID retrieves a schema by ID
	GetSchemaByID(ctx context.Context, schemaID string) (*Schema, error)

	// GetLatestSchema retrieves the latest version of a schema
	GetLatestSchema(ctx context.Context, eventType string) (*Schema, error)

	// GetSchemaVersions retrieves all versions of a schema
	GetSchemaVersions(ctx context.Context, eventType string) ([]*Schema, error)

	// DeleteSchema deletes a schema
	DeleteSchema(ctx context.Context, schemaID string) error

	// ListEventTypes lists all registered event types
	ListEventTypes(ctx context.Context) ([]string, error)

	// Close closes the schema store
	Close(ctx context.Context) error
}

// SchemaCache defines interface for caching schemas.
type SchemaCache interface {
	// Get retrieves a schema from cache
	Get(key string) (*Schema, bool)

	// Set stores a schema in cache
	Set(key string, schema *Schema, ttl time.Duration)

	// Delete removes a schema from cache
	Delete(key string)

	// Clear clears all cached schemas
	Clear()

	// Stats returns cache statistics
	Stats() map[string]any
}

// SchemaEvolution defines how schemas can evolve.
type SchemaEvolution struct {
	EventType      string              `json:"event_type"`
	FromVersion    int                 `json:"from_version"`
	ToVersion      int                 `json:"to_version"`
	Changes        []SchemaChange      `json:"changes"`
	Compatible     bool                `json:"compatible"`
	Transformation *TransformationRule `json:"transformation,omitempty"`
	Metadata       map[string]any      `json:"metadata,omitempty"`
}

// SchemaChange represents a change in schema.
type SchemaChange struct {
	Type        string `json:"type"` // "add", "remove", "modify", "rename"
	Path        string `json:"path"` // JSON path to the changed property
	OldValue    any    `json:"old_value,omitempty"`
	NewValue    any    `json:"new_value,omitempty"`
	Description string `json:"description,omitempty"`
}

// TransformationRule defines how to transform data between schema versions.
type TransformationRule struct {
	Rule         string         `json:"rule"` // "copy", "rename", "transform", "default"
	SourcePath   string         `json:"source_path,omitempty"`
	TargetPath   string         `json:"target_path,omitempty"`
	Transform    string         `json:"transform,omitempty"` // transformation expression
	DefaultValue any            `json:"default_value,omitempty"`
	Metadata     map[string]any `json:"metadata,omitempty"`
}

// NewSchemaRegistry creates a new schema registry.
func NewSchemaRegistry(store SchemaStore, cache SchemaCache, config *RegistryConfig, logger forge.Logger, metrics forge.Metrics) *SchemaRegistry {
	if config == nil {
		config = DefaultRegistryConfig()
	}

	registry := &SchemaRegistry{
		schemas:     make(map[string]map[int]*Schema),
		schemasByID: make(map[string]*Schema),
		store:       store,
		cache:       cache,
		config:      config,
		logger:      logger,
		metrics:     metrics,
	}

	// Initialize validator if validation is enabled
	if config.ValidationLevel != "disabled" {
		validationConfig := &ValidationConfig{
			StrictMode:       config.ValidationLevel == "strict",
			AllowExtraFields: config.ValidationLevel == "lenient",
		}
		registry.validator = NewEventSchemaValidator(registry, validationConfig, logger, metrics)
	}

	return registry
}

// RegisterSchema registers a new schema in the registry.
func (sr *SchemaRegistry) RegisterSchema(ctx context.Context, schema *Schema) error {
	if err := sr.validateSchema(schema); err != nil {
		return errors.ErrValidationError("schema", err)
	}

	sr.mu.Lock()
	defer sr.mu.Unlock()

	// Check if schema already exists
	if existing, exists := sr.getSchemaFromMemory(schema.Name, schema.Version); exists {
		if existing.ID == schema.ID {
			return nil // Already registered
		}

		return fmt.Errorf("schema %s version %d already exists with different ID", schema.Name, schema.Version)
	}

	// Check version compatibility if required
	if sr.config.VersioningStrategy != "none" {
		if err := sr.checkVersionCompatibility(ctx, schema); err != nil {
			return fmt.Errorf("version compatibility check failed: %w", err)
		}
	}

	// Ensure schema has timestamps
	now := time.Now().UTC().Format(time.RFC3339)
	if schema.CreatedAt == "" {
		schema.CreatedAt = now
	}

	schema.UpdatedAt = now

	// Save to persistent store
	if sr.store != nil {
		if err := sr.store.SaveSchema(ctx, schema); err != nil {
			return fmt.Errorf("failed to save schema to store: %w", err)
		}
	}

	// Add to memory
	if sr.schemas[schema.Name] == nil {
		sr.schemas[schema.Name] = make(map[int]*Schema)
	}

	sr.schemas[schema.Name][schema.Version] = schema
	sr.schemasByID[schema.ID] = schema

	// Cache if enabled
	if sr.config.EnableCache && sr.cache != nil {
		cacheKey := sr.buildCacheKey(schema.Name, schema.Version)
		sr.cache.Set(cacheKey, schema, sr.config.CacheTTL)
	}

	// Clean up old versions if limit exceeded
	if sr.config.MaxVersions > 0 {
		sr.cleanupOldVersions(ctx, schema.Name)
	}

	if sr.logger != nil {
		sr.logger.Info("schema registered",
			logger.String("event_type", schema.Name),
			logger.Int("version", schema.Version),
			logger.String("schema_id", schema.ID),
		)
	}

	if sr.metrics != nil {
		sr.metrics.Counter("forge.events.schema_registered").Inc()
		sr.metrics.Gauge("forge.events.schemas_total").Set(float64(len(sr.schemasByID)))
	}

	return nil
}

// GetSchema retrieves a schema by event type and version.
func (sr *SchemaRegistry) GetSchema(eventType string, version int) (*Schema, error) {
	// Try cache first if enabled
	if sr.config.EnableCache && sr.cache != nil {
		cacheKey := sr.buildCacheKey(eventType, version)
		if schema, found := sr.cache.Get(cacheKey); found {
			return schema, nil
		}
	}

	// Try memory
	sr.mu.RLock()
	schema, exists := sr.getSchemaFromMemory(eventType, version)
	sr.mu.RUnlock()

	if exists {
		// Update cache
		if sr.config.EnableCache && sr.cache != nil {
			cacheKey := sr.buildCacheKey(eventType, version)
			sr.cache.Set(cacheKey, schema, sr.config.CacheTTL)
		}

		return schema, nil
	}

	// Try persistent store
	if sr.store != nil {
		schema, err := sr.store.GetSchema(context.Background(), eventType, version)
		if err != nil {
			return nil, fmt.Errorf("failed to get schema from store: %w", err)
		}

		// Add to memory and cache
		sr.mu.Lock()

		if sr.schemas[eventType] == nil {
			sr.schemas[eventType] = make(map[int]*Schema)
		}

		sr.schemas[eventType][version] = schema
		sr.schemasByID[schema.ID] = schema
		sr.mu.Unlock()

		if sr.config.EnableCache && sr.cache != nil {
			cacheKey := sr.buildCacheKey(eventType, version)
			sr.cache.Set(cacheKey, schema, sr.config.CacheTTL)
		}

		return schema, nil
	}

	return nil, fmt.Errorf("schema not found: %s version %d", eventType, version)
}

// GetLatestSchema retrieves the latest version of a schema.
func (sr *SchemaRegistry) GetLatestSchema(eventType string) (*Schema, error) {
	sr.mu.RLock()
	versions, exists := sr.schemas[eventType]
	sr.mu.RUnlock()

	if exists {
		var (
			latestVersion int
			latestSchema  *Schema
		)

		for version, schema := range versions {
			if version > latestVersion {
				latestVersion = version
				latestSchema = schema
			}
		}

		if latestSchema != nil {
			return latestSchema, nil
		}
	}

	// Try persistent store
	if sr.store != nil {
		return sr.store.GetLatestSchema(context.Background(), eventType)
	}

	return nil, fmt.Errorf("no schema found for event type: %s", eventType)
}

// GetSchemaByID retrieves a schema by ID.
func (sr *SchemaRegistry) GetSchemaByID(schemaID string) (*Schema, error) {
	sr.mu.RLock()
	schema, exists := sr.schemasByID[schemaID]
	sr.mu.RUnlock()

	if exists {
		return schema, nil
	}

	// Try persistent store
	if sr.store != nil {
		schema, err := sr.store.GetSchemaByID(context.Background(), schemaID)
		if err != nil {
			return nil, fmt.Errorf("failed to get schema by ID from store: %w", err)
		}

		// Add to memory
		sr.mu.Lock()

		if sr.schemas[schema.Name] == nil {
			sr.schemas[schema.Name] = make(map[int]*Schema)
		}

		sr.schemas[schema.Name][schema.Version] = schema
		sr.schemasByID[schema.ID] = schema
		sr.mu.Unlock()

		return schema, nil
	}

	return nil, fmt.Errorf("schema not found: %s", schemaID)
}

// GetSchemaVersions retrieves all versions of a schema.
func (sr *SchemaRegistry) GetSchemaVersions(eventType string) ([]*Schema, error) {
	sr.mu.RLock()
	versions, exists := sr.schemas[eventType]
	sr.mu.RUnlock()

	var schemas []*Schema

	if exists {
		for _, schema := range versions {
			schemas = append(schemas, schema)
		}
	}

	// Also check persistent store for any missing versions
	if sr.store != nil {
		storeSchemas, err := sr.store.GetSchemaVersions(context.Background(), eventType)
		if err != nil {
			return schemas, nil // Return what we have from memory
		}

		// Merge with memory schemas, avoiding duplicates
		existing := make(map[int]bool)
		for _, schema := range schemas {
			existing[schema.Version] = true
		}

		for _, schema := range storeSchemas {
			if !existing[schema.Version] {
				schemas = append(schemas, schema)
			}
		}
	}

	return schemas, nil
}

// UpdateSchema updates an existing schema.
func (sr *SchemaRegistry) UpdateSchema(ctx context.Context, schema *Schema) error {
	if err := sr.validateSchema(schema); err != nil {
		return errors.ErrValidationError("schema", err)
	}

	sr.mu.Lock()
	defer sr.mu.Unlock()

	// Check if schema exists
	existing, exists := sr.getSchemaFromMemory(schema.Name, schema.Version)
	if !exists {
		return fmt.Errorf("schema %s version %d does not exist", schema.Name, schema.Version)
	}

	// Update timestamp
	schema.CreatedAt = existing.CreatedAt
	schema.UpdatedAt = time.Now().UTC().Format(time.RFC3339)

	// Save to persistent store
	if sr.store != nil {
		if err := sr.store.SaveSchema(ctx, schema); err != nil {
			return fmt.Errorf("failed to update schema in store: %w", err)
		}
	}

	// Update memory
	sr.schemas[schema.Name][schema.Version] = schema
	sr.schemasByID[schema.ID] = schema

	// Invalidate cache
	if sr.config.EnableCache && sr.cache != nil {
		cacheKey := sr.buildCacheKey(schema.Name, schema.Version)
		sr.cache.Delete(cacheKey)
	}

	if sr.logger != nil {
		sr.logger.Info("schema updated",
			logger.String("event_type", schema.Name),
			logger.Int("version", schema.Version),
			logger.String("schema_id", schema.ID),
		)
	}

	if sr.metrics != nil {
		sr.metrics.Counter("forge.events.schema_updated").Inc()
	}

	return nil
}

// DeleteSchema deletes a schema.
func (sr *SchemaRegistry) DeleteSchema(ctx context.Context, eventType string, version int) error {
	sr.mu.Lock()
	defer sr.mu.Unlock()

	schema, exists := sr.getSchemaFromMemory(eventType, version)
	if !exists {
		return fmt.Errorf("schema %s version %d does not exist", eventType, version)
	}

	// Delete from persistent store
	if sr.store != nil {
		if err := sr.store.DeleteSchema(ctx, schema.ID); err != nil {
			return fmt.Errorf("failed to delete schema from store: %w", err)
		}
	}

	// Delete from memory
	delete(sr.schemas[eventType], version)

	if len(sr.schemas[eventType]) == 0 {
		delete(sr.schemas, eventType)
	}

	delete(sr.schemasByID, schema.ID)

	// Invalidate cache
	if sr.config.EnableCache && sr.cache != nil {
		cacheKey := sr.buildCacheKey(eventType, version)
		sr.cache.Delete(cacheKey)
	}

	if sr.logger != nil {
		sr.logger.Info("schema deleted",
			logger.String("event_type", eventType),
			logger.Int("version", version),
			logger.String("schema_id", schema.ID),
		)
	}

	if sr.metrics != nil {
		sr.metrics.Counter("forge.events.schema_deleted").Inc()
		sr.metrics.Gauge("forge.events.schemas_total").Set(float64(len(sr.schemasByID)))
	}

	return nil
}

// ListEventTypes lists all registered event types.
func (sr *SchemaRegistry) ListEventTypes() []string {
	sr.mu.RLock()
	defer sr.mu.RUnlock()

	var eventTypes []string
	for eventType := range sr.schemas {
		eventTypes = append(eventTypes, eventType)
	}

	return eventTypes
}

// validateSchema validates a schema before registration.
func (sr *SchemaRegistry) validateSchema(schema *Schema) error {
	if schema == nil {
		return errors.New("schema cannot be nil")
	}

	if schema.ID == "" {
		return errors.New("schema ID is required")
	}

	if schema.Name == "" {
		return errors.New("schema name is required")
	}

	if schema.Version <= 0 {
		return errors.New("schema version must be positive")
	}

	if schema.Type == "" {
		return errors.New("schema type is required")
	}

	if schema.Properties == nil {
		return errors.New("schema properties are required")
	}

	return nil
}

// checkVersionCompatibility checks if a new schema version is compatible.
func (sr *SchemaRegistry) checkVersionCompatibility(ctx context.Context, newSchema *Schema) error {
	if sr.config.VersioningStrategy == "none" {
		return nil
	}

	// Get existing schemas for this event type
	existing, exists := sr.schemas[newSchema.Name]
	if !exists || len(existing) == 0 {
		return nil // First schema, no compatibility check needed
	}

	// Find the latest existing version
	var (
		latestVersion int
		latestSchema  *Schema
	)

	for version, schema := range existing {
		if version > latestVersion {
			latestVersion = version
			latestSchema = schema
		}
	}

	if latestSchema == nil {
		return nil
	}

	// Check version ordering
	if newSchema.Version <= latestVersion {
		return fmt.Errorf("new schema version %d must be greater than latest version %d", newSchema.Version, latestVersion)
	}

	// Perform compatibility check based on strategy
	switch sr.config.VersioningStrategy {
	case "strict":
		return sr.checkStrictCompatibility(latestSchema, newSchema)
	case "compatible":
		return sr.checkBackwardCompatibility(latestSchema, newSchema)
	default:
		return nil
	}
}

// checkStrictCompatibility performs strict compatibility checking.
func (sr *SchemaRegistry) checkStrictCompatibility(oldSchema, newSchema *Schema) error {
	// In strict mode, only additive changes are allowed
	evolution := sr.analyzeSchemaEvolution(oldSchema, newSchema)

	for _, change := range evolution.Changes {
		switch change.Type {
		case "remove", "modify":
			return fmt.Errorf("strict compatibility violation: %s change not allowed at path %s", change.Type, change.Path)
		case "add":
			// Adding fields is allowed
			continue
		default:
			return fmt.Errorf("unknown change type: %s", change.Type)
		}
	}

	return nil
}

// checkBackwardCompatibility performs backward compatibility checking.
func (sr *SchemaRegistry) checkBackwardCompatibility(oldSchema, newSchema *Schema) error {
	// In compatible mode, we allow more changes but still check for breaking changes
	evolution := sr.analyzeSchemaEvolution(oldSchema, newSchema)

	for _, change := range evolution.Changes {
		switch change.Type {
		case "remove":
			// Removing required fields is a breaking change
			if sr.isRequiredField(oldSchema, change.Path) {
				return fmt.Errorf("backward compatibility violation: cannot remove required field %s", change.Path)
			}
		case "modify":
			// Type changes are generally breaking
			if sr.isTypeChange(change) {
				return fmt.Errorf("backward compatibility violation: type change not allowed at path %s", change.Path)
			}
		case "add":
			// Adding fields is generally safe
			continue
		}
	}

	return nil
}

// analyzeSchemaEvolution analyzes differences between two schemas.
func (sr *SchemaRegistry) analyzeSchemaEvolution(oldSchema, newSchema *Schema) *SchemaEvolution {
	evolution := &SchemaEvolution{
		EventType:   newSchema.Name,
		FromVersion: oldSchema.Version,
		ToVersion:   newSchema.Version,
		Changes:     make([]SchemaChange, 0),
		Compatible:  true,
	}

	// Compare properties
	sr.compareProperties("", oldSchema.Properties, newSchema.Properties, evolution)

	// Compare required fields
	sr.compareRequiredFields(oldSchema.Required, newSchema.Required, evolution)

	return evolution
}

// compareProperties compares properties between two schemas.
func (sr *SchemaRegistry) compareProperties(basePath string, oldProps, newProps map[string]*Property, evolution *SchemaEvolution) {
	// Check for removed properties
	for name, oldProp := range oldProps {
		path := sr.buildPath(basePath, name)
		if newProp, exists := newProps[name]; !exists {
			evolution.Changes = append(evolution.Changes, SchemaChange{
				Type:        "remove",
				Path:        path,
				OldValue:    oldProp,
				Description: fmt.Sprintf("Property %s was removed", path),
			})
		} else {
			// Check for modifications
			if oldProp.Type != newProp.Type {
				evolution.Changes = append(evolution.Changes, SchemaChange{
					Type:        "modify",
					Path:        path,
					OldValue:    oldProp.Type,
					NewValue:    newProp.Type,
					Description: fmt.Sprintf("Property %s type changed from %s to %s", path, oldProp.Type, newProp.Type),
				})
			}

			// Recursively check nested properties
			if oldProp.Properties != nil && newProp.Properties != nil {
				sr.compareProperties(path, oldProp.Properties, newProp.Properties, evolution)
			}
		}
	}

	// Check for added properties
	for name, newProp := range newProps {
		path := sr.buildPath(basePath, name)
		if _, exists := oldProps[name]; !exists {
			evolution.Changes = append(evolution.Changes, SchemaChange{
				Type:        "add",
				Path:        path,
				NewValue:    newProp,
				Description: fmt.Sprintf("Property %s was added", path),
			})
		}
	}
}

// compareRequiredFields compares required fields between schemas.
func (sr *SchemaRegistry) compareRequiredFields(oldRequired, newRequired []string, evolution *SchemaEvolution) {
	oldReqMap := make(map[string]bool)
	for _, field := range oldRequired {
		oldReqMap[field] = true
	}

	newReqMap := make(map[string]bool)
	for _, field := range newRequired {
		newReqMap[field] = true
	}

	// Check for removed required fields
	for field := range oldReqMap {
		if !newReqMap[field] {
			evolution.Changes = append(evolution.Changes, SchemaChange{
				Type:        "modify",
				Path:        field,
				OldValue:    "required",
				NewValue:    "optional",
				Description: fmt.Sprintf("Field %s is no longer required", field),
			})
		}
	}

	// Check for added required fields
	for field := range newReqMap {
		if !oldReqMap[field] {
			evolution.Changes = append(evolution.Changes, SchemaChange{
				Type:        "modify",
				Path:        field,
				OldValue:    "optional",
				NewValue:    "required",
				Description: fmt.Sprintf("Field %s is now required", field),
			})
		}
	}
}

// Helper methods.
func (sr *SchemaRegistry) getSchemaFromMemory(eventType string, version int) (*Schema, bool) {
	if versions, exists := sr.schemas[eventType]; exists {
		if schema, exists := versions[version]; exists {
			return schema, true
		}
	}

	return nil, false
}

func (sr *SchemaRegistry) buildCacheKey(eventType string, version int) string {
	return fmt.Sprintf("%s:%d", eventType, version)
}

func (sr *SchemaRegistry) buildPath(basePath, name string) string {
	if basePath == "" {
		return name
	}

	return fmt.Sprintf("%s.%s", basePath, name)
}

func (sr *SchemaRegistry) isRequiredField(schema *Schema, path string) bool {

	return slices.Contains(schema.Required, path)
}

func (sr *SchemaRegistry) isTypeChange(change SchemaChange) bool {
	return change.OldValue != change.NewValue
}

func (sr *SchemaRegistry) cleanupOldVersions(ctx context.Context, eventType string) {
	versions := sr.schemas[eventType]
	if len(versions) <= sr.config.MaxVersions {
		return
	}

	// Keep only the latest N versions
	var sortedVersions []int
	for version := range versions {
		sortedVersions = append(sortedVersions, version)
	}

	// Sort versions in descending order
	for i := range len(sortedVersions) - 1 {
		for j := i + 1; j < len(sortedVersions); j++ {
			if sortedVersions[i] < sortedVersions[j] {
				sortedVersions[i], sortedVersions[j] = sortedVersions[j], sortedVersions[i]
			}
		}
	}

	// Remove old versions
	for i := sr.config.MaxVersions; i < len(sortedVersions); i++ {
		version := sortedVersions[i]
		schema := versions[version]

		// Delete from store
		if sr.store != nil {
			sr.store.DeleteSchema(ctx, schema.ID)
		}

		// Delete from memory
		delete(versions, version)
		delete(sr.schemasByID, schema.ID)

		if sr.logger != nil {
			sr.logger.Info("old schema version cleaned up",
				logger.String("event_type", eventType),
				logger.Int("version", version),
			)
		}
	}
}

// GetStats returns registry statistics.
func (sr *SchemaRegistry) GetStats() map[string]any {
	sr.mu.RLock()
	defer sr.mu.RUnlock()

	stats := map[string]any{
		"total_schemas":     len(sr.schemasByID),
		"total_event_types": len(sr.schemas),
		"config":            sr.config,
	}

	// Add cache stats if available
	if sr.cache != nil {
		stats["cache"] = sr.cache.Stats()
	}

	// Add per-event-type stats
	eventTypeStats := make(map[string]int)
	for eventType, versions := range sr.schemas {
		eventTypeStats[eventType] = len(versions)
	}

	stats["schemas_per_event_type"] = eventTypeStats

	return stats
}

// MemorySchemaStore implements SchemaStore using in-memory storage.
type MemorySchemaStore struct {
	schemas map[string]*Schema         // schema_id -> schema
	byType  map[string]map[int]*Schema // event_type -> version -> schema
	mu      sync.RWMutex
}

// NewMemorySchemaStore creates a new in-memory schema store.
func NewMemorySchemaStore() *MemorySchemaStore {
	return &MemorySchemaStore{
		schemas: make(map[string]*Schema),
		byType:  make(map[string]map[int]*Schema),
	}
}

// SaveSchema implements SchemaStore.
func (mss *MemorySchemaStore) SaveSchema(ctx context.Context, schema *Schema) error {
	mss.mu.Lock()
	defer mss.mu.Unlock()

	mss.schemas[schema.ID] = schema

	if mss.byType[schema.Name] == nil {
		mss.byType[schema.Name] = make(map[int]*Schema)
	}

	mss.byType[schema.Name][schema.Version] = schema

	return nil
}

// GetSchema implements SchemaStore.
func (mss *MemorySchemaStore) GetSchema(ctx context.Context, eventType string, version int) (*Schema, error) {
	mss.mu.RLock()
	defer mss.mu.RUnlock()

	if versions, exists := mss.byType[eventType]; exists {
		if schema, exists := versions[version]; exists {
			return schema, nil
		}
	}

	return nil, fmt.Errorf("schema not found: %s version %d", eventType, version)
}

// GetSchemaByID implements SchemaStore.
func (mss *MemorySchemaStore) GetSchemaByID(ctx context.Context, schemaID string) (*Schema, error) {
	mss.mu.RLock()
	defer mss.mu.RUnlock()

	if schema, exists := mss.schemas[schemaID]; exists {
		return schema, nil
	}

	return nil, fmt.Errorf("schema not found: %s", schemaID)
}

// GetLatestSchema implements SchemaStore.
func (mss *MemorySchemaStore) GetLatestSchema(ctx context.Context, eventType string) (*Schema, error) {
	mss.mu.RLock()
	defer mss.mu.RUnlock()

	versions, exists := mss.byType[eventType]
	if !exists {
		return nil, fmt.Errorf("no schemas found for event type: %s", eventType)
	}

	var (
		latestVersion int
		latestSchema  *Schema
	)

	for version, schema := range versions {
		if version > latestVersion {
			latestVersion = version
			latestSchema = schema
		}
	}

	if latestSchema == nil {
		return nil, fmt.Errorf("no schemas found for event type: %s", eventType)
	}

	return latestSchema, nil
}

// GetSchemaVersions implements SchemaStore.
func (mss *MemorySchemaStore) GetSchemaVersions(ctx context.Context, eventType string) ([]*Schema, error) {
	mss.mu.RLock()
	defer mss.mu.RUnlock()

	versions, exists := mss.byType[eventType]
	if !exists {
		return nil, fmt.Errorf("no schemas found for event type: %s", eventType)
	}

	var schemas []*Schema
	for _, schema := range versions {
		schemas = append(schemas, schema)
	}

	return schemas, nil
}

// DeleteSchema implements SchemaStore.
func (mss *MemorySchemaStore) DeleteSchema(ctx context.Context, schemaID string) error {
	mss.mu.Lock()
	defer mss.mu.Unlock()

	schema, exists := mss.schemas[schemaID]
	if !exists {
		return fmt.Errorf("schema not found: %s", schemaID)
	}

	delete(mss.schemas, schemaID)

	if versions, exists := mss.byType[schema.Name]; exists {
		delete(versions, schema.Version)

		if len(versions) == 0 {
			delete(mss.byType, schema.Name)
		}
	}

	return nil
}

// ListEventTypes implements SchemaStore.
func (mss *MemorySchemaStore) ListEventTypes(ctx context.Context) ([]string, error) {
	mss.mu.RLock()
	defer mss.mu.RUnlock()

	var eventTypes []string
	for eventType := range mss.byType {
		eventTypes = append(eventTypes, eventType)
	}

	return eventTypes, nil
}

// Close implements SchemaStore.
func (mss *MemorySchemaStore) Close(ctx context.Context) error {
	return nil
}

// MemorySchemaCache implements SchemaCache using in-memory storage.
type MemorySchemaCache struct {
	cache  map[string]*cachedSchema
	mu     sync.RWMutex
	hits   int64
	misses int64
}

type cachedSchema struct {
	schema    *Schema
	expiresAt time.Time
}

// NewMemorySchemaCache creates a new in-memory schema cache.
func NewMemorySchemaCache() *MemorySchemaCache {
	return &MemorySchemaCache{
		cache: make(map[string]*cachedSchema),
	}
}

// Get implements SchemaCache.
func (msc *MemorySchemaCache) Get(key string) (*Schema, bool) {
	msc.mu.RLock()
	defer msc.mu.RUnlock()

	cached, exists := msc.cache[key]
	if !exists {
		msc.misses++

		return nil, false
	}

	if time.Now().After(cached.expiresAt) {
		delete(msc.cache, key)
		msc.misses++

		return nil, false
	}

	msc.hits++

	return cached.schema, true
}

// Set implements SchemaCache.
func (msc *MemorySchemaCache) Set(key string, schema *Schema, ttl time.Duration) {
	msc.mu.Lock()
	defer msc.mu.Unlock()

	msc.cache[key] = &cachedSchema{
		schema:    schema,
		expiresAt: time.Now().Add(ttl),
	}
}

// Delete implements SchemaCache.
func (msc *MemorySchemaCache) Delete(key string) {
	msc.mu.Lock()
	defer msc.mu.Unlock()

	delete(msc.cache, key)
}

// Clear implements SchemaCache.
func (msc *MemorySchemaCache) Clear() {
	msc.mu.Lock()
	defer msc.mu.Unlock()

	msc.cache = make(map[string]*cachedSchema)
}

// Stats implements SchemaCache.
func (msc *MemorySchemaCache) Stats() map[string]any {
	msc.mu.RLock()
	defer msc.mu.RUnlock()

	total := msc.hits + msc.misses

	hitRate := float64(0)
	if total > 0 {
		hitRate = float64(msc.hits) / float64(total)
	}

	return map[string]any{
		"entries":  len(msc.cache),
		"hits":     msc.hits,
		"misses":   msc.misses,
		"hit_rate": hitRate,
	}
}
