package sdk

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge"
)

// Artifact represents a named, typed piece of content that can be
// exported, saved, or referenced by users. Similar to Claude's artifacts.
type Artifact struct {
	// ID is a unique identifier for the artifact
	ID string `json:"id"`

	// Name is a human-readable name
	Name string `json:"name"`

	// Type categorizes the artifact
	Type ArtifactType `json:"type"`

	// Content holds the artifact data
	Content string `json:"content"`

	// ContentType is the MIME type of the content
	ContentType string `json:"content_type,omitempty"`

	// Language for code artifacts
	Language string `json:"language,omitempty"`

	// Title for display purposes
	Title string `json:"title,omitempty"`

	// Description provides context about the artifact
	Description string `json:"description,omitempty"`

	// Version for tracking changes
	Version int `json:"version"`

	// Metadata holds additional data
	Metadata map[string]any `json:"metadata,omitempty"`

	// Timestamps
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	// Capabilities
	Exportable  bool `json:"exportable"`
	Editable    bool `json:"editable"`
	Previewable bool `json:"previewable"`
	Runnable    bool `json:"runnable"`
}

// ArtifactType categorizes artifacts.
type ArtifactType string

const (
	ArtifactTypeCode     ArtifactType = "code"
	ArtifactTypeDocument ArtifactType = "document"
	ArtifactTypeData     ArtifactType = "data"
	ArtifactTypeImage    ArtifactType = "image"
	ArtifactTypeChart    ArtifactType = "chart"
	ArtifactTypeTable    ArtifactType = "table"
	ArtifactTypeDiagram  ArtifactType = "diagram"
	ArtifactTypeConfig   ArtifactType = "config"
	ArtifactTypeHTML     ArtifactType = "html"
	ArtifactTypeMarkdown ArtifactType = "markdown"
	ArtifactTypeSVG      ArtifactType = "svg"
	ArtifactTypeReact    ArtifactType = "react"
	ArtifactTypeMermaid  ArtifactType = "mermaid"
)

// ArtifactRegistry manages artifacts within a session or context.
type ArtifactRegistry struct {
	mu        sync.RWMutex
	artifacts map[string]*Artifact
	logger    forge.Logger
	metrics   forge.Metrics

	// Storage backend for persistence
	store ArtifactStore

	// Event handlers
	onArtifactCreated func(*Artifact)
	onArtifactUpdated func(*Artifact)
	onArtifactDeleted func(string)
}

// ArtifactStore provides persistence for artifacts.
type ArtifactStore interface {
	Save(ctx context.Context, artifact *Artifact) error
	Load(ctx context.Context, id string) (*Artifact, error)
	Delete(ctx context.Context, id string) error
	List(ctx context.Context, filter ArtifactFilter) ([]*Artifact, error)
}

// ArtifactFilter for querying artifacts.
type ArtifactFilter struct {
	Types    []ArtifactType
	Names    []string
	FromTime *time.Time
	ToTime   *time.Time
	Limit    int
}

// NewArtifactRegistry creates a new artifact registry.
func NewArtifactRegistry(logger forge.Logger, metrics forge.Metrics) *ArtifactRegistry {
	return &ArtifactRegistry{
		artifacts: make(map[string]*Artifact),
		logger:    logger,
		metrics:   metrics,
	}
}

// WithStore sets the persistence backend.
func (r *ArtifactRegistry) WithStore(store ArtifactStore) *ArtifactRegistry {
	r.store = store
	return r
}

// OnArtifactCreated sets a callback for artifact creation.
func (r *ArtifactRegistry) OnArtifactCreated(fn func(*Artifact)) *ArtifactRegistry {
	r.onArtifactCreated = fn
	return r
}

// OnArtifactUpdated sets a callback for artifact updates.
func (r *ArtifactRegistry) OnArtifactUpdated(fn func(*Artifact)) *ArtifactRegistry {
	r.onArtifactUpdated = fn
	return r
}

// OnArtifactDeleted sets a callback for artifact deletion.
func (r *ArtifactRegistry) OnArtifactDeleted(fn func(string)) *ArtifactRegistry {
	r.onArtifactDeleted = fn
	return r
}

// Create creates a new artifact.
func (r *ArtifactRegistry) Create(artifact *Artifact) error {
	if artifact.ID == "" {
		artifact.ID = generateArtifactID()
	}

	if artifact.Name == "" {
		return fmt.Errorf("artifact name is required")
	}

	artifact.Version = 1
	artifact.CreatedAt = time.Now()
	artifact.UpdatedAt = artifact.CreatedAt

	// Set defaults based on type
	r.setArtifactDefaults(artifact)

	r.mu.Lock()
	r.artifacts[artifact.ID] = artifact
	r.mu.Unlock()

	// Persist if store is configured
	if r.store != nil {
		if err := r.store.Save(context.Background(), artifact); err != nil {
			if r.logger != nil {
				r.logger.Warn("Failed to persist artifact",
					F("artifact_id", artifact.ID),
					F("error", err.Error()),
				)
			}
		}
	}

	if r.onArtifactCreated != nil {
		r.onArtifactCreated(artifact)
	}

	if r.logger != nil {
		r.logger.Debug("Artifact created",
			F("id", artifact.ID),
			F("name", artifact.Name),
			F("type", artifact.Type),
		)
	}

	if r.metrics != nil {
		r.metrics.Counter("forge.ai.sdk.artifacts.created", "type", string(artifact.Type)).Inc()
	}

	return nil
}

// setArtifactDefaults sets default values based on artifact type.
// It only sets values that are not already explicitly set (zero values).
func (r *ArtifactRegistry) setArtifactDefaults(artifact *Artifact) {
	if artifact.Metadata == nil {
		artifact.Metadata = make(map[string]any)
	}

	// Check if capabilities are explicitly set via metadata marker
	_, capsExplicitlySet := artifact.Metadata["_capabilities_set"]

	switch artifact.Type {
	case ArtifactTypeCode:
		if !capsExplicitlySet {
			artifact.Exportable = true
			artifact.Editable = true
			artifact.Previewable = false
			artifact.Runnable = isRunnableLanguage(artifact.Language)
		}
		if artifact.ContentType == "" {
			artifact.ContentType = "text/plain"
		}

	case ArtifactTypeDocument, ArtifactTypeMarkdown:
		if !capsExplicitlySet {
			artifact.Exportable = true
			artifact.Editable = true
			artifact.Previewable = true
		}
		if artifact.ContentType == "" {
			artifact.ContentType = "text/markdown"
		}

	case ArtifactTypeHTML:
		if !capsExplicitlySet {
			artifact.Exportable = true
			artifact.Editable = true
			artifact.Previewable = true
		}
		if artifact.ContentType == "" {
			artifact.ContentType = "text/html"
		}

	case ArtifactTypeData, ArtifactTypeTable:
		if !capsExplicitlySet {
			artifact.Exportable = true
			artifact.Editable = false
			artifact.Previewable = true
		}
		if artifact.ContentType == "" {
			artifact.ContentType = "application/json"
		}

	case ArtifactTypeImage, ArtifactTypeSVG:
		if !capsExplicitlySet {
			artifact.Exportable = true
			artifact.Editable = false
			artifact.Previewable = true
		}

	case ArtifactTypeChart, ArtifactTypeDiagram, ArtifactTypeMermaid:
		if !capsExplicitlySet {
			artifact.Exportable = true
			artifact.Editable = true
			artifact.Previewable = true
		}

	case ArtifactTypeReact:
		if !capsExplicitlySet {
			artifact.Exportable = true
			artifact.Editable = true
			artifact.Previewable = true
			artifact.Runnable = true
		}
		if artifact.ContentType == "" {
			artifact.ContentType = "text/jsx"
		}

	case ArtifactTypeConfig:
		if !capsExplicitlySet {
			artifact.Exportable = true
			artifact.Editable = true
			artifact.Previewable = false
		}
	}
}

// Get retrieves an artifact by ID.
func (r *ArtifactRegistry) Get(id string) (*Artifact, error) {
	r.mu.RLock()
	artifact, exists := r.artifacts[id]
	r.mu.RUnlock()

	if exists {
		return artifact, nil
	}

	// Try loading from store
	if r.store != nil {
		artifact, err := r.store.Load(context.Background(), id)
		if err != nil {
			return nil, fmt.Errorf("artifact not found: %s", id)
		}

		// Cache it
		r.mu.Lock()
		r.artifacts[id] = artifact
		r.mu.Unlock()

		return artifact, nil
	}

	return nil, fmt.Errorf("artifact not found: %s", id)
}

// GetByName retrieves an artifact by name.
func (r *ArtifactRegistry) GetByName(name string) (*Artifact, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, artifact := range r.artifacts {
		if artifact.Name == name {
			return artifact, nil
		}
	}

	return nil, fmt.Errorf("artifact not found: %s", name)
}

// Update updates an existing artifact.
func (r *ArtifactRegistry) Update(id string, content string) error {
	r.mu.Lock()
	artifact, exists := r.artifacts[id]
	if !exists {
		r.mu.Unlock()
		return fmt.Errorf("artifact not found: %s", id)
	}

	artifact.Content = content
	artifact.Version++
	artifact.UpdatedAt = time.Now()
	r.mu.Unlock()

	// Persist
	if r.store != nil {
		if err := r.store.Save(context.Background(), artifact); err != nil {
			if r.logger != nil {
				r.logger.Warn("Failed to persist artifact update",
					F("artifact_id", id),
					F("error", err.Error()),
				)
			}
		}
	}

	if r.onArtifactUpdated != nil {
		r.onArtifactUpdated(artifact)
	}

	if r.metrics != nil {
		r.metrics.Counter("forge.ai.sdk.artifacts.updated").Inc()
	}

	return nil
}

// Delete removes an artifact.
func (r *ArtifactRegistry) Delete(id string) error {
	r.mu.Lock()
	delete(r.artifacts, id)
	r.mu.Unlock()

	if r.store != nil {
		if err := r.store.Delete(context.Background(), id); err != nil {
			if r.logger != nil {
				r.logger.Warn("Failed to delete artifact from store",
					F("artifact_id", id),
					F("error", err.Error()),
				)
			}
		}
	}

	if r.onArtifactDeleted != nil {
		r.onArtifactDeleted(id)
	}

	if r.metrics != nil {
		r.metrics.Counter("forge.ai.sdk.artifacts.deleted").Inc()
	}

	return nil
}

// List returns all artifacts, optionally filtered.
func (r *ArtifactRegistry) List(filter *ArtifactFilter) []*Artifact {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]*Artifact, 0)

	for _, artifact := range r.artifacts {
		if filter != nil {
			// Type filter
			if len(filter.Types) > 0 {
				found := false
				for _, t := range filter.Types {
					if artifact.Type == t {
						found = true
						break
					}
				}
				if !found {
					continue
				}
			}

			// Name filter
			if len(filter.Names) > 0 {
				found := false
				for _, n := range filter.Names {
					if artifact.Name == n {
						found = true
						break
					}
				}
				if !found {
					continue
				}
			}

			// Time filters
			if filter.FromTime != nil && artifact.CreatedAt.Before(*filter.FromTime) {
				continue
			}
			if filter.ToTime != nil && artifact.CreatedAt.After(*filter.ToTime) {
				continue
			}
		}

		result = append(result, artifact)

		// Limit
		if filter != nil && filter.Limit > 0 && len(result) >= filter.Limit {
			break
		}
	}

	return result
}

// Clear removes all artifacts.
func (r *ArtifactRegistry) Clear() {
	r.mu.Lock()
	r.artifacts = make(map[string]*Artifact)
	r.mu.Unlock()
}

// Export exports an artifact to the specified format.
func (r *ArtifactRegistry) Export(id string, format ExportFormat) ([]byte, error) {
	artifact, err := r.Get(id)
	if err != nil {
		return nil, err
	}

	if !artifact.Exportable {
		return nil, fmt.Errorf("artifact is not exportable")
	}

	switch format {
	case ExportFormatRaw:
		return []byte(artifact.Content), nil

	case ExportFormatJSON:
		return json.Marshal(artifact)

	case ExportFormatMarkdown:
		return r.exportAsMarkdown(artifact)

	case ExportFormatHTML:
		return r.exportAsHTML(artifact)

	default:
		return nil, fmt.Errorf("unsupported export format: %s", format)
	}
}

// ExportFormat specifies the export format.
type ExportFormat string

const (
	ExportFormatRaw      ExportFormat = "raw"
	ExportFormatJSON     ExportFormat = "json"
	ExportFormatMarkdown ExportFormat = "markdown"
	ExportFormatHTML     ExportFormat = "html"
)

// exportAsMarkdown exports an artifact as markdown.
func (r *ArtifactRegistry) exportAsMarkdown(artifact *Artifact) ([]byte, error) {
	var content string

	switch artifact.Type {
	case ArtifactTypeCode:
		content = fmt.Sprintf("# %s\n\n```%s\n%s\n```\n",
			artifact.Title, artifact.Language, artifact.Content)

	case ArtifactTypeTable, ArtifactTypeData:
		content = fmt.Sprintf("# %s\n\n%s\n", artifact.Title, artifact.Content)

	default:
		content = fmt.Sprintf("# %s\n\n%s\n", artifact.Title, artifact.Content)
	}

	return []byte(content), nil
}

// exportAsHTML exports an artifact as HTML.
func (r *ArtifactRegistry) exportAsHTML(artifact *Artifact) ([]byte, error) {
	var content string

	switch artifact.Type {
	case ArtifactTypeCode:
		content = fmt.Sprintf(`<!DOCTYPE html>
<html>
<head><title>%s</title></head>
<body>
<h1>%s</h1>
<pre><code class="language-%s">%s</code></pre>
</body>
</html>`, artifact.Title, artifact.Title, artifact.Language, artifact.Content)

	case ArtifactTypeHTML:
		content = artifact.Content

	default:
		content = fmt.Sprintf(`<!DOCTYPE html>
<html>
<head><title>%s</title></head>
<body>
<h1>%s</h1>
<div>%s</div>
</body>
</html>`, artifact.Title, artifact.Title, artifact.Content)
	}

	return []byte(content), nil
}

// --- Artifact Builder ---

// ArtifactBuilder provides a fluent API for creating artifacts.
type ArtifactBuilder struct {
	artifact *Artifact
}

// NewArtifact creates a new artifact builder.
func NewArtifact(name string, artifactType ArtifactType) *ArtifactBuilder {
	return &ArtifactBuilder{
		artifact: &Artifact{
			ID:       generateArtifactID(),
			Name:     name,
			Type:     artifactType,
			Metadata: make(map[string]any),
		},
	}
}

// WithID sets the artifact ID.
func (b *ArtifactBuilder) WithID(id string) *ArtifactBuilder {
	b.artifact.ID = id
	return b
}

// WithTitle sets the artifact title.
func (b *ArtifactBuilder) WithTitle(title string) *ArtifactBuilder {
	b.artifact.Title = title
	return b
}

// WithDescription sets the artifact description.
func (b *ArtifactBuilder) WithDescription(description string) *ArtifactBuilder {
	b.artifact.Description = description
	return b
}

// WithContent sets the artifact content.
func (b *ArtifactBuilder) WithContent(content string) *ArtifactBuilder {
	b.artifact.Content = content
	return b
}

// WithLanguage sets the language (for code artifacts).
func (b *ArtifactBuilder) WithLanguage(language string) *ArtifactBuilder {
	b.artifact.Language = language
	return b
}

// WithContentType sets the MIME type.
func (b *ArtifactBuilder) WithContentType(contentType string) *ArtifactBuilder {
	b.artifact.ContentType = contentType
	return b
}

// WithMetadata adds metadata.
func (b *ArtifactBuilder) WithMetadata(key string, value any) *ArtifactBuilder {
	b.artifact.Metadata[key] = value
	return b
}

// Exportable sets whether the artifact can be exported.
func (b *ArtifactBuilder) Exportable(exportable bool) *ArtifactBuilder {
	b.artifact.Exportable = exportable
	return b
}

// Editable sets whether the artifact can be edited.
func (b *ArtifactBuilder) Editable(editable bool) *ArtifactBuilder {
	b.artifact.Editable = editable
	return b
}

// Previewable sets whether the artifact can be previewed.
func (b *ArtifactBuilder) Previewable(previewable bool) *ArtifactBuilder {
	b.artifact.Previewable = previewable
	return b
}

// Runnable sets whether the artifact can be executed.
func (b *ArtifactBuilder) Runnable(runnable bool) *ArtifactBuilder {
	b.artifact.Runnable = runnable
	return b
}

// Build returns the completed artifact.
func (b *ArtifactBuilder) Build() *Artifact {
	return b.artifact
}

// --- Helper functions ---

func generateArtifactID() string {
	return fmt.Sprintf("artifact_%d", time.Now().UnixNano())
}

func isRunnableLanguage(language string) bool {
	runnableLanguages := map[string]bool{
		"javascript": true,
		"js":         true,
		"typescript": true,
		"ts":         true,
		"python":     true,
		"py":         true,
		"go":         true,
		"rust":       true,
		"ruby":       true,
		"php":        true,
		"shell":      true,
		"bash":       true,
		"sh":         true,
	}
	return runnableLanguages[language]
}

// --- Code Artifact Helpers ---

// NewCodeArtifact creates a code artifact.
func NewCodeArtifact(name, language, code string) *Artifact {
	artifact := NewArtifact(name, ArtifactTypeCode).
		WithLanguage(language).
		WithContent(code).
		WithTitle(name).
		Build()

	// Apply defaults
	artifact.Exportable = true
	artifact.Editable = true
	artifact.Runnable = isRunnableLanguage(language)
	if artifact.ContentType == "" {
		artifact.ContentType = "text/plain"
	}

	return artifact
}

// NewDocumentArtifact creates a markdown document artifact.
func NewDocumentArtifact(name, content string) *Artifact {
	artifact := NewArtifact(name, ArtifactTypeMarkdown).
		WithContent(content).
		WithTitle(name).
		Build()

	// Apply defaults
	artifact.Exportable = true
	artifact.Editable = true
	artifact.Previewable = true
	artifact.ContentType = "text/markdown"

	return artifact
}

// NewTableArtifact creates a table data artifact.
func NewTableArtifact(name string, data any) *Artifact {
	jsonData, _ := json.MarshalIndent(data, "", "  ")
	return NewArtifact(name, ArtifactTypeTable).
		WithContent(string(jsonData)).
		WithTitle(name).
		Build()
}

// NewChartArtifact creates a chart artifact.
func NewChartArtifact(name string, chartData *ChartData) *Artifact {
	jsonData, _ := json.MarshalIndent(chartData, "", "  ")
	return NewArtifact(name, ArtifactTypeChart).
		WithContent(string(jsonData)).
		WithTitle(name).
		Build()
}

// NewMermaidArtifact creates a Mermaid diagram artifact.
func NewMermaidArtifact(name, diagram string) *Artifact {
	return NewArtifact(name, ArtifactTypeMermaid).
		WithContent(diagram).
		WithTitle(name).
		Build()
}

// NewReactArtifact creates a React component artifact.
func NewReactArtifact(name, component string) *Artifact {
	return NewArtifact(name, ArtifactTypeReact).
		WithContent(component).
		WithLanguage("jsx").
		WithTitle(name).
		Build()
}
