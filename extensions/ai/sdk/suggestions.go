package sdk

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Suggestion represents a follow-up action or prompt that the user can take.
type Suggestion struct {
	// ID is a unique identifier for the suggestion
	ID string `json:"id"`

	// Type categorizes the suggestion
	Type SuggestionType `json:"type"`

	// Display information
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
	Icon        string `json:"icon,omitempty"`

	// Action details
	Action  string         `json:"action,omitempty"`  // URL, command, or prompt
	Payload map[string]any `json:"payload,omitempty"` // Additional data for the action

	// Metadata
	Category   string   `json:"category,omitempty"`
	Tags       []string `json:"tags,omitempty"`
	Priority   int      `json:"priority,omitempty"`   // 1-10, higher is more relevant
	Confidence float64  `json:"confidence,omitempty"` // 0-1, how confident we are this is useful

	// Display options
	Primary bool   `json:"primary,omitempty"` // Is this a primary/highlighted suggestion
	Variant string `json:"variant,omitempty"` // default, outline, ghost
	Size    string `json:"size,omitempty"`    // sm, md, lg
	Color   string `json:"color,omitempty"`   // brand color or theme color

	// Behavior
	Dismissible bool `json:"dismissible,omitempty"`
	AutoExecute bool `json:"auto_execute,omitempty"` // Execute automatically without user click

	// Metadata
	Metadata  map[string]any `json:"metadata,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
}

// SuggestionType categorizes suggestions.
type SuggestionType string

const (
	SuggestionTypePrompt     SuggestionType = "prompt"     // Follow-up question or prompt
	SuggestionTypeAction     SuggestionType = "action"     // Action button (e.g., run code, export)
	SuggestionTypeLink       SuggestionType = "link"       // External link
	SuggestionTypeNavigation SuggestionType = "navigation" // In-app navigation
	SuggestionTypeRefine     SuggestionType = "refine"     // Refine current response
	SuggestionTypeExpand     SuggestionType = "expand"     // Expand on topic
	SuggestionTypeRelated    SuggestionType = "related"    // Related topics
	SuggestionTypeExample    SuggestionType = "example"    // Show example
	SuggestionTypeExport     SuggestionType = "export"     // Export options
	SuggestionTypeCopy       SuggestionType = "copy"       // Copy content
	SuggestionTypeFeedback   SuggestionType = "feedback"   // Provide feedback
)

// SuggestionBuilder provides a fluent API for building suggestions.
type SuggestionBuilder struct {
	suggestion *Suggestion
}

// NewSuggestion creates a new suggestion builder.
func NewSuggestion(label string, suggestionType SuggestionType) *SuggestionBuilder {
	return &SuggestionBuilder{
		suggestion: &Suggestion{
			ID:        generateSuggestionID(),
			Label:     label,
			Type:      suggestionType,
			Metadata:  make(map[string]any),
			CreatedAt: time.Now(),
			Priority:  5,
			Variant:   "default",
			Size:      "md",
		},
	}
}

// WithID sets the suggestion ID.
func (b *SuggestionBuilder) WithID(id string) *SuggestionBuilder {
	b.suggestion.ID = id

	return b
}

// WithDescription sets the description.
func (b *SuggestionBuilder) WithDescription(description string) *SuggestionBuilder {
	b.suggestion.Description = description

	return b
}

// WithIcon sets the icon.
func (b *SuggestionBuilder) WithIcon(icon string) *SuggestionBuilder {
	b.suggestion.Icon = icon

	return b
}

// WithAction sets the action.
func (b *SuggestionBuilder) WithAction(action string) *SuggestionBuilder {
	b.suggestion.Action = action

	return b
}

// WithPayload sets the payload data.
func (b *SuggestionBuilder) WithPayload(payload map[string]any) *SuggestionBuilder {
	b.suggestion.Payload = payload

	return b
}

// WithPayloadValue adds a single payload value.
func (b *SuggestionBuilder) WithPayloadValue(key string, value any) *SuggestionBuilder {
	if b.suggestion.Payload == nil {
		b.suggestion.Payload = make(map[string]any)
	}

	b.suggestion.Payload[key] = value

	return b
}

// WithCategory sets the category.
func (b *SuggestionBuilder) WithCategory(category string) *SuggestionBuilder {
	b.suggestion.Category = category

	return b
}

// WithTags sets the tags.
func (b *SuggestionBuilder) WithTags(tags ...string) *SuggestionBuilder {
	b.suggestion.Tags = tags

	return b
}

// WithPriority sets the priority (1-10).
func (b *SuggestionBuilder) WithPriority(priority int) *SuggestionBuilder {
	if priority < 1 {
		priority = 1
	} else if priority > 10 {
		priority = 10
	}

	b.suggestion.Priority = priority

	return b
}

// WithConfidence sets the confidence score.
func (b *SuggestionBuilder) WithConfidence(confidence float64) *SuggestionBuilder {
	if confidence < 0 {
		confidence = 0
	} else if confidence > 1 {
		confidence = 1
	}

	b.suggestion.Confidence = confidence

	return b
}

// Primary marks this as a primary suggestion.
func (b *SuggestionBuilder) Primary() *SuggestionBuilder {
	b.suggestion.Primary = true

	return b
}

// WithVariant sets the display variant.
func (b *SuggestionBuilder) WithVariant(variant string) *SuggestionBuilder {
	b.suggestion.Variant = variant

	return b
}

// WithSize sets the display size.
func (b *SuggestionBuilder) WithSize(size string) *SuggestionBuilder {
	b.suggestion.Size = size

	return b
}

// WithColor sets the color.
func (b *SuggestionBuilder) WithColor(color string) *SuggestionBuilder {
	b.suggestion.Color = color

	return b
}

// Dismissible makes the suggestion dismissible.
func (b *SuggestionBuilder) Dismissible() *SuggestionBuilder {
	b.suggestion.Dismissible = true

	return b
}

// AutoExecute sets the suggestion to auto-execute.
func (b *SuggestionBuilder) AutoExecute() *SuggestionBuilder {
	b.suggestion.AutoExecute = true

	return b
}

// WithMetadata adds metadata.
func (b *SuggestionBuilder) WithMetadata(key string, value any) *SuggestionBuilder {
	b.suggestion.Metadata[key] = value

	return b
}

// Build returns the completed suggestion.
func (b *SuggestionBuilder) Build() *Suggestion {
	return b.suggestion
}

// --- Suggestion Generators ---

// SuggestionGenerator generates suggestions based on context.
type SuggestionGenerator interface {
	Generate(ctx context.Context, input SuggestionInput) []Suggestion
}

// SuggestionInput provides context for suggestion generation.
type SuggestionInput struct {
	// The response content
	Content string

	// The original query
	Query string

	// Topics detected in the response
	Topics []string

	// Entities mentioned
	Entities []string

	// Actions available
	AvailableActions []string

	// Current context
	Context map[string]any
}

// FollowUpGenerator generates follow-up prompt suggestions.
type FollowUpGenerator struct {
	templates      []FollowUpTemplate
	maxSuggestions int
}

// FollowUpTemplate defines a follow-up prompt pattern.
type FollowUpTemplate struct {
	Pattern     string
	Label       string
	Description string
	Category    string
	Priority    int
}

// NewFollowUpGenerator creates a new follow-up generator.
func NewFollowUpGenerator() *FollowUpGenerator {
	return &FollowUpGenerator{
		templates:      defaultFollowUpTemplates(),
		maxSuggestions: 5,
	}
}

// WithMaxSuggestions sets the maximum number of suggestions.
func (g *FollowUpGenerator) WithMaxSuggestions(max int) *FollowUpGenerator {
	g.maxSuggestions = max

	return g
}

// AddTemplate adds a custom follow-up template.
func (g *FollowUpGenerator) AddTemplate(template FollowUpTemplate) *FollowUpGenerator {
	g.templates = append(g.templates, template)

	return g
}

// Generate generates follow-up suggestions.
func (g *FollowUpGenerator) Generate(_ context.Context, input SuggestionInput) []Suggestion {
	suggestions := make([]Suggestion, 0)

	// Generate topic-based suggestions
	for _, topic := range input.Topics {
		if len(suggestions) >= g.maxSuggestions {
			break
		}

		suggestions = append(suggestions,
			*NewSuggestion("Tell me more about "+topic, SuggestionTypeExpand).
				WithDescription("Get more details about " + topic).
				WithAction("Tell me more about " + topic).
				WithCategory("expand").
				WithPriority(7).
				Build(),
		)
	}

	// Generate entity-based suggestions
	for _, entity := range input.Entities {
		if len(suggestions) >= g.maxSuggestions {
			break
		}

		suggestions = append(suggestions,
			*NewSuggestion(fmt.Sprintf("What is %s?", entity), SuggestionTypeRelated).
				WithDescription("Learn more about " + entity).
				WithAction(fmt.Sprintf("What is %s?", entity)).
				WithCategory("related").
				WithPriority(6).
				Build(),
		)
	}

	// Add default follow-ups if we have room
	for _, template := range g.templates {
		if len(suggestions) >= g.maxSuggestions {
			break
		}

		// Apply template to current context
		label := applyTemplate(template.Label, input)
		if label == "" {
			continue
		}

		suggestions = append(suggestions,
			*NewSuggestion(label, SuggestionTypePrompt).
				WithDescription(applyTemplate(template.Description, input)).
				WithAction(label).
				WithCategory(template.Category).
				WithPriority(template.Priority).
				Build(),
		)
	}

	return suggestions
}

// defaultFollowUpTemplates returns default follow-up templates.
func defaultFollowUpTemplates() []FollowUpTemplate {
	return []FollowUpTemplate{
		{
			Pattern:     "example",
			Label:       "Can you give me an example?",
			Description: "Request a concrete example",
			Category:    "example",
			Priority:    8,
		},
		{
			Pattern:     "elaborate",
			Label:       "Can you elaborate on this?",
			Description: "Get more detailed explanation",
			Category:    "expand",
			Priority:    7,
		},
		{
			Pattern:     "simplify",
			Label:       "Can you explain this more simply?",
			Description: "Get a simpler explanation",
			Category:    "refine",
			Priority:    6,
		},
		{
			Pattern:     "alternative",
			Label:       "Are there alternatives?",
			Description: "Explore other options",
			Category:    "related",
			Priority:    5,
		},
		{
			Pattern:     "pros_cons",
			Label:       "What are the pros and cons?",
			Description: "Get a balanced view",
			Category:    "expand",
			Priority:    6,
		},
	}
}

// applyTemplate applies a template with input context.
func applyTemplate(template string, input SuggestionInput) string {
	result := template

	// Replace placeholders
	if len(input.Topics) > 0 {
		result = strings.ReplaceAll(result, "{{topic}}", input.Topics[0])
	}

	if len(input.Entities) > 0 {
		result = strings.ReplaceAll(result, "{{entity}}", input.Entities[0])
	}

	if input.Query != "" {
		result = strings.ReplaceAll(result, "{{query}}", input.Query)
	}

	return result
}

// --- Action Suggestion Generator ---

// ActionSuggestionGenerator generates action-based suggestions.
type ActionSuggestionGenerator struct {
	actions map[string]ActionDefinition
}

// ActionDefinition defines an available action.
type ActionDefinition struct {
	ID          string
	Label       string
	Description string
	Icon        string
	Handler     string // Action handler identifier
	Conditions  []ActionCondition
	Priority    int
}

// ActionCondition defines when an action is available.
type ActionCondition struct {
	Type     string // content_type, has_code, has_data, etc.
	Value    any
	Operator string // equals, contains, exists
}

// NewActionSuggestionGenerator creates a new action suggestion generator.
func NewActionSuggestionGenerator() *ActionSuggestionGenerator {
	gen := &ActionSuggestionGenerator{
		actions: make(map[string]ActionDefinition),
	}

	// Register default actions
	gen.RegisterDefaultActions()

	return gen
}

// RegisterAction registers an action.
func (g *ActionSuggestionGenerator) RegisterAction(action ActionDefinition) {
	g.actions[action.ID] = action
}

// RegisterDefaultActions registers default actions.
func (g *ActionSuggestionGenerator) RegisterDefaultActions() {
	g.RegisterAction(ActionDefinition{
		ID:          "copy",
		Label:       "Copy to clipboard",
		Description: "Copy the response to clipboard",
		Icon:        "clipboard",
		Handler:     "copy",
		Priority:    9,
	})

	g.RegisterAction(ActionDefinition{
		ID:          "export_markdown",
		Label:       "Export as Markdown",
		Description: "Download response as markdown file",
		Icon:        "download",
		Handler:     "export",
		Conditions: []ActionCondition{
			{Type: "content_length", Value: 100, Operator: "greater_than"},
		},
		Priority: 7,
	})

	g.RegisterAction(ActionDefinition{
		ID:          "run_code",
		Label:       "Run code",
		Description: "Execute the code snippet",
		Icon:        "play",
		Handler:     "run_code",
		Conditions: []ActionCondition{
			{Type: "has_code", Value: true, Operator: "equals"},
		},
		Priority: 10,
	})

	g.RegisterAction(ActionDefinition{
		ID:          "view_chart",
		Label:       "View as chart",
		Description: "Visualize data as a chart",
		Icon:        "chart",
		Handler:     "visualize",
		Conditions: []ActionCondition{
			{Type: "has_data", Value: true, Operator: "equals"},
		},
		Priority: 8,
	})

	g.RegisterAction(ActionDefinition{
		ID:          "share",
		Label:       "Share response",
		Description: "Share this response",
		Icon:        "share",
		Handler:     "share",
		Priority:    5,
	})

	g.RegisterAction(ActionDefinition{
		ID:          "regenerate",
		Label:       "Regenerate",
		Description: "Generate a new response",
		Icon:        "refresh",
		Handler:     "regenerate",
		Priority:    6,
	})
}

// Generate generates action suggestions based on content.
func (g *ActionSuggestionGenerator) Generate(_ context.Context, input SuggestionInput) []Suggestion {
	suggestions := make([]Suggestion, 0)

	for _, action := range g.actions {
		if !g.checkConditions(action.Conditions, input) {
			continue
		}

		suggestions = append(suggestions,
			*NewSuggestion(action.Label, SuggestionTypeAction).
				WithDescription(action.Description).
				WithIcon(action.Icon).
				WithAction(action.Handler).
				WithPriority(action.Priority).
				Build(),
		)
	}

	return suggestions
}

// checkConditions checks if all conditions are met.
func (g *ActionSuggestionGenerator) checkConditions(conditions []ActionCondition, input SuggestionInput) bool {
	for _, condition := range conditions {
		if !g.checkCondition(condition, input) {
			return false
		}
	}

	return true
}

// checkCondition checks a single condition.
func (g *ActionSuggestionGenerator) checkCondition(condition ActionCondition, input SuggestionInput) bool {
	switch condition.Type {
	case "content_length":
		length := len(input.Content)

		expected, ok := condition.Value.(int)
		if !ok {
			return false
		}

		switch condition.Operator {
		case "greater_than":
			return length > expected
		case "less_than":
			return length < expected
		case "equals":
			return length == expected
		}

	case "has_code":
		hasCode := strings.Contains(input.Content, "```")

		expected, ok := condition.Value.(bool)
		if !ok {
			return hasCode
		}

		return hasCode == expected

	case "has_data":
		hasData := strings.Contains(input.Content, "|") || // table
			strings.Contains(input.Content, "{") // JSON

		expected, ok := condition.Value.(bool)
		if !ok {
			return hasData
		}

		return hasData == expected

	case "content_contains":
		substr, ok := condition.Value.(string)
		if !ok {
			return false
		}

		return strings.Contains(input.Content, substr)
	}

	return true
}

// --- Suggestion Manager ---

// SuggestionManager manages suggestions for responses.
type SuggestionManager struct {
	generators []SuggestionGenerator
	maxTotal   int
}

// NewSuggestionManager creates a new suggestion manager.
func NewSuggestionManager() *SuggestionManager {
	return &SuggestionManager{
		generators: make([]SuggestionGenerator, 0),
		maxTotal:   10,
	}
}

// WithMaxSuggestions sets the maximum total suggestions.
func (m *SuggestionManager) WithMaxSuggestions(max int) *SuggestionManager {
	m.maxTotal = max

	return m
}

// AddGenerator adds a suggestion generator.
func (m *SuggestionManager) AddGenerator(gen SuggestionGenerator) *SuggestionManager {
	m.generators = append(m.generators, gen)

	return m
}

// GenerateSuggestions generates suggestions from all registered generators.
func (m *SuggestionManager) GenerateSuggestions(ctx context.Context, input SuggestionInput) []Suggestion {
	allSuggestions := make([]Suggestion, 0)

	for _, gen := range m.generators {
		suggestions := gen.Generate(ctx, input)
		allSuggestions = append(allSuggestions, suggestions...)
	}

	// Sort by priority (higher first)
	sortSuggestionsByPriority(allSuggestions)

	// Limit total
	if len(allSuggestions) > m.maxTotal {
		allSuggestions = allSuggestions[:m.maxTotal]
	}

	return allSuggestions
}

// sortSuggestionsByPriority sorts suggestions by priority (descending).
func sortSuggestionsByPriority(suggestions []Suggestion) {
	for i := range len(suggestions) - 1 {
		for j := i + 1; j < len(suggestions); j++ {
			if suggestions[j].Priority > suggestions[i].Priority {
				suggestions[i], suggestions[j] = suggestions[j], suggestions[i]
			}
		}
	}
}

// --- Quick Suggestion Helpers ---

// NewPromptSuggestion creates a follow-up prompt suggestion.
func NewPromptSuggestion(label string) *Suggestion {
	return NewSuggestion(label, SuggestionTypePrompt).
		WithAction(label).
		Build()
}

// NewActionSuggestion creates an action suggestion.
func NewActionSuggestion(label, action string) *Suggestion {
	return NewSuggestion(label, SuggestionTypeAction).
		WithAction(action).
		Build()
}

// NewLinkSuggestion creates a link suggestion.
func NewLinkSuggestion(label, url string) *Suggestion {
	return NewSuggestion(label, SuggestionTypeLink).
		WithAction(url).
		WithIcon("external-link").
		Build()
}

// NewCopySuggestion creates a copy action suggestion.
func NewCopySuggestion(content string) *Suggestion {
	return NewSuggestion("Copy to clipboard", SuggestionTypeCopy).
		WithAction("copy").
		WithPayloadValue("content", content).
		WithIcon("clipboard").
		Build()
}

// NewExportSuggestion creates an export suggestion.
func NewExportSuggestion(format string) *Suggestion {
	return NewSuggestion("Export as "+strings.ToUpper(format), SuggestionTypeExport).
		WithAction("export").
		WithPayloadValue("format", format).
		WithIcon("download").
		Build()
}

// --- Helper functions ---

func generateSuggestionID() string {
	return fmt.Sprintf("sug_%d", time.Now().UnixNano())
}

// DefaultSuggestionManager creates a suggestion manager with default generators.
func DefaultSuggestionManager() *SuggestionManager {
	return NewSuggestionManager().
		AddGenerator(NewFollowUpGenerator()).
		AddGenerator(NewActionSuggestionGenerator())
}
