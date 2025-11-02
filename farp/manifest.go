package farp

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"time"
)

// NewManifest creates a new schema manifest with default values
func NewManifest(serviceName, serviceVersion, instanceID string) *SchemaManifest {
	return &SchemaManifest{
		Version:        ProtocolVersion,
		ServiceName:    serviceName,
		ServiceVersion: serviceVersion,
		InstanceID:     instanceID,
		Schemas:        []SchemaDescriptor{},
		Capabilities:   []string{},
		Endpoints:      SchemaEndpoints{},
		UpdatedAt:      time.Now().Unix(),
	}
}

// AddSchema adds a schema descriptor to the manifest
func (m *SchemaManifest) AddSchema(descriptor SchemaDescriptor) {
	m.Schemas = append(m.Schemas, descriptor)
}

// AddCapability adds a capability to the manifest
func (m *SchemaManifest) AddCapability(capability string) {
	// Check if capability already exists
	for _, c := range m.Capabilities {
		if c == capability {
			return
		}
	}
	m.Capabilities = append(m.Capabilities, capability)
}

// UpdateChecksum recalculates the manifest checksum based on all schema hashes
func (m *SchemaManifest) UpdateChecksum() error {
	checksum, err := CalculateManifestChecksum(m)
	if err != nil {
		return fmt.Errorf("failed to calculate manifest checksum: %w", err)
	}
	m.Checksum = checksum
	m.UpdatedAt = time.Now().Unix()
	return nil
}

// Validate validates the manifest for correctness
func (m *SchemaManifest) Validate() error {
	// Check protocol version compatibility
	if !IsCompatible(m.Version) {
		return fmt.Errorf("%w: manifest version %s, protocol version %s",
			ErrIncompatibleVersion, m.Version, ProtocolVersion)
	}

	// Check required fields
	if m.ServiceName == "" {
		return &ValidationError{Field: "service_name", Message: "service name is required"}
	}
	if m.InstanceID == "" {
		return &ValidationError{Field: "instance_id", Message: "instance ID is required"}
	}

	// Validate health endpoint
	if m.Endpoints.Health == "" {
		return &ValidationError{Field: "endpoints.health", Message: "health endpoint is required"}
	}

	// Validate each schema descriptor
	for i, schema := range m.Schemas {
		if err := ValidateSchemaDescriptor(&schema); err != nil {
			return fmt.Errorf("invalid schema at index %d: %w", i, err)
		}
	}

	// Verify checksum if present
	if m.Checksum != "" {
		expectedChecksum, err := CalculateManifestChecksum(m)
		if err != nil {
			return fmt.Errorf("failed to verify checksum: %w", err)
		}
		if m.Checksum != expectedChecksum {
			return fmt.Errorf("%w: expected %s, got %s", ErrChecksumMismatch, expectedChecksum, m.Checksum)
		}
	}

	return nil
}

// GetSchema retrieves a schema descriptor by type
func (m *SchemaManifest) GetSchema(schemaType SchemaType) (*SchemaDescriptor, bool) {
	for i := range m.Schemas {
		if m.Schemas[i].Type == schemaType {
			return &m.Schemas[i], true
		}
	}
	return nil, false
}

// HasCapability checks if the manifest includes a specific capability
func (m *SchemaManifest) HasCapability(capability string) bool {
	for _, c := range m.Capabilities {
		if c == capability {
			return true
		}
	}
	return false
}

// Clone creates a deep copy of the manifest
func (m *SchemaManifest) Clone() *SchemaManifest {
	clone := &SchemaManifest{
		Version:        m.Version,
		ServiceName:    m.ServiceName,
		ServiceVersion: m.ServiceVersion,
		InstanceID:     m.InstanceID,
		Schemas:        make([]SchemaDescriptor, len(m.Schemas)),
		Capabilities:   make([]string, len(m.Capabilities)),
		Endpoints:      m.Endpoints,
		UpdatedAt:      m.UpdatedAt,
		Checksum:       m.Checksum,
	}

	copy(clone.Schemas, m.Schemas)
	copy(clone.Capabilities, m.Capabilities)

	return clone
}

// ToJSON serializes the manifest to JSON
func (m *SchemaManifest) ToJSON() ([]byte, error) {
	return json.Marshal(m)
}

// ToPrettyJSON serializes the manifest to pretty-printed JSON
func (m *SchemaManifest) ToPrettyJSON() ([]byte, error) {
	return json.MarshalIndent(m, "", "  ")
}

// FromJSON deserializes a manifest from JSON
func FromJSON(data []byte) (*SchemaManifest, error) {
	var manifest SchemaManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidManifest, err)
	}
	return &manifest, nil
}

// ValidateSchemaDescriptor validates a schema descriptor
func ValidateSchemaDescriptor(sd *SchemaDescriptor) error {
	// Check schema type
	if !sd.Type.IsValid() {
		return fmt.Errorf("%w: %s", ErrUnsupportedType, sd.Type)
	}

	// Check spec version
	if sd.SpecVersion == "" {
		return &ValidationError{Field: "spec_version", Message: "spec version is required"}
	}

	// Validate location
	if err := sd.Location.Validate(); err != nil {
		return err
	}

	// For inline schemas, InlineSchema must be present
	if sd.Location.Type == LocationTypeInline && sd.InlineSchema == nil {
		return &ValidationError{Field: "inline_schema", Message: "inline schema is required for inline location type"}
	}

	// Check hash
	if sd.Hash == "" {
		return &ValidationError{Field: "hash", Message: "schema hash is required"}
	}

	// Validate hash format (should be 64 hex characters for SHA256)
	if len(sd.Hash) != 64 {
		return &ValidationError{Field: "hash", Message: "invalid hash format (expected 64 hex characters)"}
	}

	// Check content type
	if sd.ContentType == "" {
		return &ValidationError{Field: "content_type", Message: "content type is required"}
	}

	return nil
}

// CalculateManifestChecksum calculates the SHA256 checksum of a manifest
// by combining all schema hashes in a deterministic order
func CalculateManifestChecksum(manifest *SchemaManifest) (string, error) {
	if len(manifest.Schemas) == 0 {
		// Empty manifest has empty checksum
		return "", nil
	}

	// Sort schemas by type for deterministic hashing
	sortedSchemas := make([]SchemaDescriptor, len(manifest.Schemas))
	copy(sortedSchemas, manifest.Schemas)
	sort.Slice(sortedSchemas, func(i, j int) bool {
		return sortedSchemas[i].Type < sortedSchemas[j].Type
	})

	// Concatenate all schema hashes
	var combined string
	for _, schema := range sortedSchemas {
		combined += schema.Hash
	}

	// Calculate SHA256 of combined hashes
	hash := sha256.Sum256([]byte(combined))
	return hex.EncodeToString(hash[:]), nil
}

// CalculateSchemaChecksum calculates the SHA256 checksum of a schema
func CalculateSchemaChecksum(schema interface{}) (string, error) {
	// Serialize to canonical JSON (map keys are sorted by json.Marshal)
	data, err := json.Marshal(schema)
	if err != nil {
		return "", fmt.Errorf("failed to serialize schema: %w", err)
	}

	// Calculate SHA256
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:]), nil
}

// ManifestDiff represents the difference between two manifests
type ManifestDiff struct {
	// SchemasAdded are schemas present in new but not in old
	SchemasAdded []SchemaDescriptor

	// SchemasRemoved are schemas present in old but not in new
	SchemasRemoved []SchemaDescriptor

	// SchemasChanged are schemas present in both but with different hashes
	SchemasChanged []SchemaChangeDiff

	// CapabilitiesAdded are new capabilities
	CapabilitiesAdded []string

	// CapabilitiesRemoved are removed capabilities
	CapabilitiesRemoved []string

	// EndpointsChanged indicates if endpoints changed
	EndpointsChanged bool
}

// SchemaChangeDiff represents a changed schema
type SchemaChangeDiff struct {
	Type    SchemaType
	OldHash string
	NewHash string
}

// HasChanges returns true if there are any changes
func (d *ManifestDiff) HasChanges() bool {
	return len(d.SchemasAdded) > 0 ||
		len(d.SchemasRemoved) > 0 ||
		len(d.SchemasChanged) > 0 ||
		len(d.CapabilitiesAdded) > 0 ||
		len(d.CapabilitiesRemoved) > 0 ||
		d.EndpointsChanged
}

// DiffManifests compares two manifests and returns the differences
func DiffManifests(old, new *SchemaManifest) *ManifestDiff {
	diff := &ManifestDiff{}

	// Build maps for easier comparison
	oldSchemas := make(map[SchemaType]SchemaDescriptor)
	for _, s := range old.Schemas {
		oldSchemas[s.Type] = s
	}

	newSchemas := make(map[SchemaType]SchemaDescriptor)
	for _, s := range new.Schemas {
		newSchemas[s.Type] = s
	}

	// Find added and changed schemas
	for schemaType, newSchema := range newSchemas {
		if oldSchema, exists := oldSchemas[schemaType]; exists {
			// Schema exists in both, check if changed
			if oldSchema.Hash != newSchema.Hash {
				diff.SchemasChanged = append(diff.SchemasChanged, SchemaChangeDiff{
					Type:    schemaType,
					OldHash: oldSchema.Hash,
					NewHash: newSchema.Hash,
				})
			}
		} else {
			// Schema is new
			diff.SchemasAdded = append(diff.SchemasAdded, newSchema)
		}
	}

	// Find removed schemas
	for schemaType, oldSchema := range oldSchemas {
		if _, exists := newSchemas[schemaType]; !exists {
			diff.SchemasRemoved = append(diff.SchemasRemoved, oldSchema)
		}
	}

	// Compare capabilities
	oldCaps := make(map[string]bool)
	for _, c := range old.Capabilities {
		oldCaps[c] = true
	}

	newCaps := make(map[string]bool)
	for _, c := range new.Capabilities {
		newCaps[c] = true
	}

	for cap := range newCaps {
		if !oldCaps[cap] {
			diff.CapabilitiesAdded = append(diff.CapabilitiesAdded, cap)
		}
	}

	for cap := range oldCaps {
		if !newCaps[cap] {
			diff.CapabilitiesRemoved = append(diff.CapabilitiesRemoved, cap)
		}
	}

	// Compare endpoints (simple comparison)
	if old.Endpoints != new.Endpoints {
		diff.EndpointsChanged = true
	}

	return diff
}

