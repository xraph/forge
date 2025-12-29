package sdk

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/ai/llm"
)

// UIToolHints provides rendering hints for UI tools.
// These hints tell the frontend how to render tool results.
type UIToolHints struct {
	// PreferredPartType is the preferred UI part type for rendering
	PreferredPartType ContentPartType `json:"preferredPartType"`

	// StreamingEnabled indicates if the tool supports progressive streaming
	StreamingEnabled bool `json:"streamingEnabled"`

	// InteractiveFields lists fields that support user interaction
	InteractiveFields []string `json:"interactiveFields,omitempty"`

	// ActionHandlers lists available action handlers for buttons
	ActionHandlers []ActionHandler `json:"actionHandlers,omitempty"`

	// RenderOptions provides additional rendering options
	RenderOptions map[string]any `json:"renderOptions,omitempty"`

	// Priority determines rendering priority (higher = rendered first)
	Priority int `json:"priority,omitempty"`

	// Collapsible indicates if the rendered UI can be collapsed
	Collapsible bool `json:"collapsible,omitempty"`

	// DefaultCollapsed indicates if the UI should start collapsed
	DefaultCollapsed bool `json:"defaultCollapsed,omitempty"`
}

// ActionHandler defines a handler for button actions.
type ActionHandler struct {
	// ID uniquely identifies this action handler
	ID string `json:"id"`

	// Name is the human-readable name
	Name string `json:"name"`

	// Description describes what the action does
	Description string `json:"description,omitempty"`

	// ToolName is the tool to execute (if action calls a tool)
	ToolName string `json:"toolName,omitempty"`

	// PayloadSchema defines the expected payload structure
	PayloadSchema map[string]any `json:"payloadSchema,omitempty"`

	// RequiresConfirmation indicates if action needs confirmation
	RequiresConfirmation bool `json:"requiresConfirmation,omitempty"`

	// ConfirmationMessage is shown before executing
	ConfirmationMessage string `json:"confirmationMessage,omitempty"`
}

// UIToolRenderFunc is a function that renders tool results as UI parts.
type UIToolRenderFunc func(ctx context.Context, result any, streamer *UIPartStreamer) error

// UITool extends the base Tool interface with UI rendering capabilities.
type UITool interface {
	// Name returns the tool name
	Name() string

	// Description returns the tool description
	Description() string

	// GetParameters returns the parameter schema
	GetParameters() ToolParameterSchema

	// Execute runs the tool and returns the result
	Execute(ctx context.Context, params map[string]any) (any, error)

	// GetUIHints returns rendering hints for the frontend
	GetUIHints() UIToolHints

	// RenderUI renders the tool result as a streaming UI part
	// If streaming is not supported, this should stream the complete result at once
	RenderUI(ctx context.Context, result any, streamer *UIPartStreamer) error

	// SupportsStreaming returns true if the tool supports progressive streaming
	SupportsStreaming() bool
}

// BaseUITool provides a base implementation of UITool.
// Embed this in your custom UI tools.
type BaseUITool struct {
	name        string
	description string
	version     string
	params      ToolParameterSchema
	hints       UIToolHints
	handler     ToolHandler
	renderFunc  UIToolRenderFunc
}

// BaseUIToolConfig holds configuration for creating a BaseUITool.
type BaseUIToolConfig struct {
	Name        string
	Description string
	Version     string
	Parameters  ToolParameterSchema
	Hints       UIToolHints
	Handler     ToolHandler
	RenderFunc  UIToolRenderFunc
}

// NewBaseUITool creates a new base UI tool.
func NewBaseUITool(config BaseUIToolConfig) *BaseUITool {
	if config.Version == "" {
		config.Version = "1.0.0"
	}

	return &BaseUITool{
		name:        config.Name,
		description: config.Description,
		version:     config.Version,
		params:      config.Parameters,
		hints:       config.Hints,
		handler:     config.Handler,
		renderFunc:  config.RenderFunc,
	}
}

// Name returns the tool name.
func (t *BaseUITool) Name() string {
	return t.name
}

// Description returns the tool description.
func (t *BaseUITool) Description() string {
	return t.description
}

// GetParameters returns the parameter schema.
func (t *BaseUITool) GetParameters() ToolParameterSchema {
	return t.params
}

// Execute runs the tool handler.
func (t *BaseUITool) Execute(ctx context.Context, params map[string]any) (any, error) {
	if t.handler == nil {
		return nil, fmt.Errorf("tool %s has no handler", t.name)
	}
	return t.handler(ctx, params)
}

// GetUIHints returns the UI rendering hints.
func (t *BaseUITool) GetUIHints() UIToolHints {
	return t.hints
}

// RenderUI renders the tool result.
func (t *BaseUITool) RenderUI(ctx context.Context, result any, streamer *UIPartStreamer) error {
	if t.renderFunc != nil {
		return t.renderFunc(ctx, result, streamer)
	}

	// Default rendering: stream result as JSON
	return t.defaultRender(ctx, result, streamer)
}

// SupportsStreaming returns true if streaming is enabled in hints.
func (t *BaseUITool) SupportsStreaming() bool {
	return t.hints.StreamingEnabled
}

// defaultRender provides default rendering as JSON.
func (t *BaseUITool) defaultRender(ctx context.Context, result any, streamer *UIPartStreamer) error {
	if err := streamer.Start(); err != nil {
		return err
	}

	if err := streamer.StreamContent(result); err != nil {
		return err
	}

	return streamer.End()
}

// --- UI Tool Builder ---

// UIToolBuilder provides a fluent API for building UI tools.
type UIToolBuilder struct {
	config BaseUIToolConfig
}

// NewUITool creates a new UI tool builder.
func NewUITool(name, description string) *UIToolBuilder {
	return &UIToolBuilder{
		config: BaseUIToolConfig{
			Name:        name,
			Description: description,
			Version:     "1.0.0",
			Parameters:  ToolParameterSchema{Type: "object", Properties: make(map[string]ToolParameterProperty)},
			Hints:       UIToolHints{},
		},
	}
}

// WithVersion sets the tool version.
func (b *UIToolBuilder) WithVersion(version string) *UIToolBuilder {
	b.config.Version = version
	return b
}

// WithParameter adds a parameter to the tool.
func (b *UIToolBuilder) WithParameter(name, paramType, description string, required bool) *UIToolBuilder {
	b.config.Parameters.Properties[name] = ToolParameterProperty{
		Type:        paramType,
		Description: description,
	}
	if required {
		b.config.Parameters.Required = append(b.config.Parameters.Required, name)
	}
	return b
}

// WithPreferredPartType sets the preferred UI part type.
func (b *UIToolBuilder) WithPreferredPartType(partType ContentPartType) *UIToolBuilder {
	b.config.Hints.PreferredPartType = partType
	return b
}

// WithStreaming enables streaming for the tool.
func (b *UIToolBuilder) WithStreaming(enabled bool) *UIToolBuilder {
	b.config.Hints.StreamingEnabled = enabled
	return b
}

// WithInteractiveFields sets the interactive fields.
func (b *UIToolBuilder) WithInteractiveFields(fields ...string) *UIToolBuilder {
	b.config.Hints.InteractiveFields = fields
	return b
}

// WithActionHandler adds an action handler.
func (b *UIToolBuilder) WithActionHandler(handler ActionHandler) *UIToolBuilder {
	b.config.Hints.ActionHandlers = append(b.config.Hints.ActionHandlers, handler)
	return b
}

// WithRenderOption adds a render option.
func (b *UIToolBuilder) WithRenderOption(key string, value any) *UIToolBuilder {
	if b.config.Hints.RenderOptions == nil {
		b.config.Hints.RenderOptions = make(map[string]any)
	}
	b.config.Hints.RenderOptions[key] = value
	return b
}

// Collapsible makes the tool result collapsible.
func (b *UIToolBuilder) Collapsible(defaultCollapsed bool) *UIToolBuilder {
	b.config.Hints.Collapsible = true
	b.config.Hints.DefaultCollapsed = defaultCollapsed
	return b
}

// WithHandler sets the tool handler.
func (b *UIToolBuilder) WithHandler(handler ToolHandler) *UIToolBuilder {
	b.config.Handler = handler
	return b
}

// WithRenderFunc sets the UI render function.
func (b *UIToolBuilder) WithRenderFunc(renderFunc UIToolRenderFunc) *UIToolBuilder {
	b.config.RenderFunc = renderFunc
	return b
}

// Build creates the UI tool.
func (b *UIToolBuilder) Build() *BaseUITool {
	return NewBaseUITool(b.config)
}

// --- Common UI Tool Implementations ---

// TableUITool is a UI tool that renders results as tables.
type TableUITool struct {
	*BaseUITool
	tableConfig TableUIConfig
}

// TableUIConfig holds configuration for table rendering.
type TableUIConfig struct {
	// HeaderExtractor extracts headers from the result
	HeaderExtractor func(result any) []TableHeader

	// RowExtractor extracts rows from the result
	RowExtractor func(result any) [][]TableCell

	// BatchSize is the number of rows to stream at once
	BatchSize int

	// Sortable enables sorting
	Sortable bool

	// Searchable enables searching
	Searchable bool

	// Paginated enables pagination
	Paginated bool

	// PageSize is the number of rows per page
	PageSize int
}

// NewTableUITool creates a new table UI tool.
func NewTableUITool(
	name, description string,
	handler ToolHandler,
	config TableUIConfig,
) *TableUITool {
	if config.BatchSize == 0 {
		config.BatchSize = 10
	}

	tool := &TableUITool{
		tableConfig: config,
	}

	tool.BaseUITool = NewBaseUITool(BaseUIToolConfig{
		Name:        name,
		Description: description,
		Handler:     handler,
		Hints: UIToolHints{
			PreferredPartType: PartTypeTable,
			StreamingEnabled:  true,
		},
		RenderFunc: tool.renderTable,
	})

	return tool
}

// renderTable renders the result as a streaming table.
func (t *TableUITool) renderTable(ctx context.Context, result any, streamer *UIPartStreamer) error {
	if err := streamer.Start(); err != nil {
		return err
	}

	// Extract and stream headers
	var headers []TableHeader
	if t.tableConfig.HeaderExtractor != nil {
		headers = t.tableConfig.HeaderExtractor(result)
	} else {
		headers = t.defaultHeaderExtractor(result)
	}

	if err := streamer.StreamHeader(headers); err != nil {
		return err
	}

	// Extract and stream rows in batches
	var rows [][]TableCell
	if t.tableConfig.RowExtractor != nil {
		rows = t.tableConfig.RowExtractor(result)
	} else {
		rows = t.defaultRowExtractor(result)
	}

	batchSize := t.tableConfig.BatchSize
	for i := 0; i < len(rows); i += batchSize {
		end := i + batchSize
		if end > len(rows) {
			end = len(rows)
		}
		batch := rows[i:end]
		if err := streamer.StreamRows(batch); err != nil {
			return err
		}
	}

	// Stream table config as metadata
	metadata := map[string]any{
		"sortable":   t.tableConfig.Sortable,
		"searchable": t.tableConfig.Searchable,
		"paginated":  t.tableConfig.Paginated,
		"pageSize":   t.tableConfig.PageSize,
		"totalRows":  len(rows),
	}
	if err := streamer.StreamMetadata(metadata); err != nil {
		return err
	}

	return streamer.End()
}

// defaultHeaderExtractor tries to extract headers from the result.
func (t *TableUITool) defaultHeaderExtractor(result any) []TableHeader {
	// Try to infer headers from result structure
	switch v := result.(type) {
	case []map[string]any:
		if len(v) > 0 {
			headers := make([]TableHeader, 0)
			for key := range v[0] {
				headers = append(headers, TableHeader{
					Label: key,
					Key:   key,
				})
			}
			return headers
		}
	}
	return []TableHeader{}
}

// defaultRowExtractor tries to extract rows from the result.
func (t *TableUITool) defaultRowExtractor(result any) [][]TableCell {
	switch v := result.(type) {
	case []map[string]any:
		rows := make([][]TableCell, len(v))
		for i, row := range v {
			cells := make([]TableCell, 0)
			for _, value := range row {
				cells = append(cells, TableCell{
					Value:   value,
					Display: fmt.Sprintf("%v", value),
				})
			}
			rows[i] = cells
		}
		return rows
	case [][]any:
		rows := make([][]TableCell, len(v))
		for i, row := range v {
			cells := make([]TableCell, len(row))
			for j, value := range row {
				cells[j] = TableCell{
					Value:   value,
					Display: fmt.Sprintf("%v", value),
				}
			}
			rows[i] = cells
		}
		return rows
	}
	return [][]TableCell{}
}

// ChartUITool is a UI tool that renders results as charts.
type ChartUITool struct {
	*BaseUITool
	chartType   ChartType
	dataBuilder func(result any) ChartData
}

// NewChartUITool creates a new chart UI tool.
func NewChartUITool(
	name, description string,
	chartType ChartType,
	handler ToolHandler,
	dataBuilder func(result any) ChartData,
) *ChartUITool {
	tool := &ChartUITool{
		chartType:   chartType,
		dataBuilder: dataBuilder,
	}

	tool.BaseUITool = NewBaseUITool(BaseUIToolConfig{
		Name:        name,
		Description: description,
		Handler:     handler,
		Hints: UIToolHints{
			PreferredPartType: PartTypeChart,
			StreamingEnabled:  false, // Charts usually render all at once
		},
		RenderFunc: tool.renderChart,
	})

	return tool
}

// renderChart renders the result as a chart.
func (t *ChartUITool) renderChart(ctx context.Context, result any, streamer *UIPartStreamer) error {
	if err := streamer.Start(); err != nil {
		return err
	}

	// Stream chart type
	if err := streamer.StreamSection("chartType", string(t.chartType)); err != nil {
		return err
	}

	// Build and stream data
	var data ChartData
	if t.dataBuilder != nil {
		data = t.dataBuilder(result)
	}

	if err := streamer.StreamSection("data", data); err != nil {
		return err
	}

	return streamer.End()
}

// MetricsUITool is a UI tool that renders results as metrics.
type MetricsUITool struct {
	*BaseUITool
	metricsBuilder func(result any) []Metric
}

// NewMetricsUITool creates a new metrics UI tool.
func NewMetricsUITool(
	name, description string,
	handler ToolHandler,
	metricsBuilder func(result any) []Metric,
) *MetricsUITool {
	tool := &MetricsUITool{
		metricsBuilder: metricsBuilder,
	}

	tool.BaseUITool = NewBaseUITool(BaseUIToolConfig{
		Name:        name,
		Description: description,
		Handler:     handler,
		Hints: UIToolHints{
			PreferredPartType: PartTypeMetric,
			StreamingEnabled:  true,
		},
		RenderFunc: tool.renderMetrics,
	})

	return tool
}

// renderMetrics renders the result as metrics.
func (t *MetricsUITool) renderMetrics(ctx context.Context, result any, streamer *UIPartStreamer) error {
	if err := streamer.Start(); err != nil {
		return err
	}

	var metrics []Metric
	if t.metricsBuilder != nil {
		metrics = t.metricsBuilder(result)
	}

	// Stream each metric individually for progressive rendering
	for _, metric := range metrics {
		if err := streamer.StreamSection("metrics", metric); err != nil {
			return err
		}
	}

	return streamer.End()
}

// --- UI Tool Registry ---

// UIToolRegistry extends ToolRegistry with UI tool support.
type UIToolRegistry struct {
	*ToolRegistry
	uiTools map[string]UITool
	mu      sync.RWMutex
}

// NewUIToolRegistry creates a new UI tool registry.
func NewUIToolRegistry(logger forge.Logger, metrics forge.Metrics) *UIToolRegistry {
	return &UIToolRegistry{
		ToolRegistry: NewToolRegistry(logger, metrics),
		uiTools:      make(map[string]UITool),
	}
}

// RegisterUITool registers a UI tool.
func (r *UIToolRegistry) RegisterUITool(tool UITool) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	name := tool.Name()
	if _, exists := r.uiTools[name]; exists {
		return fmt.Errorf("UI tool %s already registered", name)
	}

	r.uiTools[name] = tool

	// Also register as a regular tool for LLM compatibility
	toolDef := &ToolDefinition{
		Name:        name,
		Description: tool.Description(),
		Parameters:  tool.GetParameters(),
		Handler: func(ctx context.Context, params map[string]any) (any, error) {
			return tool.Execute(ctx, params)
		},
		Metadata: map[string]any{
			"ui_tool":  true,
			"ui_hints": tool.GetUIHints(),
		},
	}

	return r.ToolRegistry.RegisterTool(toolDef)
}

// GetUITool retrieves a UI tool by name.
func (r *UIToolRegistry) GetUITool(name string) (UITool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	tool, exists := r.uiTools[name]
	if !exists {
		return nil, fmt.Errorf("UI tool %s not found", name)
	}

	return tool, nil
}

// IsUITool checks if a tool is a UI tool.
func (r *UIToolRegistry) IsUITool(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, exists := r.uiTools[name]
	return exists
}

// ListUITools returns all registered UI tools.
func (r *UIToolRegistry) ListUITools() []UITool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	tools := make([]UITool, 0, len(r.uiTools))
	for _, tool := range r.uiTools {
		tools = append(tools, tool)
	}
	return tools
}

// RegisterPresentationTools registers all built-in presentation tools.
// These include render_table, render_chart, render_metrics, render_timeline, etc.
// Call this once to enable AI-callable presentation tools.
func (r *UIToolRegistry) RegisterPresentationTools() error {
	for _, tool := range PresentationTools() {
		if err := r.RegisterUITool(tool); err != nil {
			// Skip if already registered
			if r.IsUITool(tool.Name()) {
				continue
			}
			return fmt.Errorf("failed to register presentation tool %s: %w", tool.Name(), err)
		}
	}
	return nil
}

// GetPresentationToolSchemas returns LLM-ready tool definitions for all
// registered presentation tools.
func (r *UIToolRegistry) GetPresentationToolSchemas() []llm.Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	schemas := make([]llm.Tool, 0)

	for name, tool := range r.uiTools {
		if IsPresentationTool(name) {
			schemas = append(schemas, llm.Tool{
				Type: "function",
				Function: &llm.FunctionDefinition{
					Name:        tool.Name(),
					Description: tool.Description(),
					Parameters:  toolParamsToMap(tool.GetParameters()),
				},
			})
		}
	}

	return schemas
}

// ExecuteUITool executes a UI tool and renders the result.
func (r *UIToolRegistry) ExecuteUITool(
	ctx context.Context,
	name string,
	params map[string]any,
	onEvent func(llm.ClientStreamEvent) error,
	executionID string,
) (*UIToolExecutionResult, error) {
	startTime := time.Now()

	tool, err := r.GetUITool(name)
	if err != nil {
		return nil, err
	}

	// Execute the tool
	result, err := tool.Execute(ctx, params)
	if err != nil {
		return &UIToolExecutionResult{
			ToolName:  name,
			Success:   false,
			Error:     err,
			Duration:  time.Since(startTime),
			Timestamp: startTime,
		}, err
	}

	// Create streamer for UI rendering
	streamer := NewUIPartStreamer(UIPartStreamerConfig{
		PartType:    tool.GetUIHints().PreferredPartType,
		ExecutionID: executionID,
		OnEvent:     onEvent,
		Context:     ctx,
		Metadata: map[string]any{
			"tool_name": name,
			"ui_hints":  tool.GetUIHints(),
		},
	})

	// Render the result
	if err := tool.RenderUI(ctx, result, streamer); err != nil {
		return &UIToolExecutionResult{
			ToolName:  name,
			Success:   false,
			Result:    result,
			Error:     err,
			Duration:  time.Since(startTime),
			Timestamp: startTime,
		}, err
	}

	// Build final part
	finalPart, _ := streamer.BuildFinalPart()

	return &UIToolExecutionResult{
		ToolName:  name,
		Success:   true,
		Result:    result,
		Part:      finalPart,
		PartID:    streamer.GetPartID(),
		Duration:  time.Since(startTime),
		Timestamp: startTime,
		Metadata:  streamer.GetAccumulatedData(),
	}, nil
}

// UIToolExecutionResult represents the result of executing a UI tool.
type UIToolExecutionResult struct {
	ToolName  string         `json:"toolName"`
	Success   bool           `json:"success"`
	Result    any            `json:"result,omitempty"`
	Part      ContentPart    `json:"part,omitempty"`
	PartID    string         `json:"partId,omitempty"`
	Error     error          `json:"error,omitempty"`
	Duration  time.Duration  `json:"duration"`
	Timestamp time.Time      `json:"timestamp"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

// MarshalJSON implements json.Marshaler for UIToolExecutionResult.
func (r *UIToolExecutionResult) MarshalJSON() ([]byte, error) {
	type Alias UIToolExecutionResult
	var errStr string
	if r.Error != nil {
		errStr = r.Error.Error()
	}

	return json.Marshal(&struct {
		*Alias
		Error string `json:"error,omitempty"`
	}{
		Alias: (*Alias)(r),
		Error: errStr,
	})
}

// --- Predefined UI Tool Factories ---

// CreateDataQueryTool creates a UI tool for database queries that renders as tables.
func CreateDataQueryTool(
	name, description string,
	queryFunc func(ctx context.Context, query string) ([]map[string]any, error),
) *TableUITool {
	handler := func(ctx context.Context, params map[string]any) (any, error) {
		query, ok := params["query"].(string)
		if !ok {
			return nil, fmt.Errorf("query parameter is required")
		}
		return queryFunc(ctx, query)
	}

	return NewTableUITool(name, description, handler, TableUIConfig{
		BatchSize:  20,
		Sortable:   true,
		Searchable: true,
		Paginated:  true,
		PageSize:   50,
	})
}

// CreateMetricsDashboardTool creates a UI tool for displaying metrics.
func CreateMetricsDashboardTool(
	name, description string,
	metricsFunc func(ctx context.Context) ([]Metric, error),
) *MetricsUITool {
	handler := func(ctx context.Context, params map[string]any) (any, error) {
		return metricsFunc(ctx)
	}

	metricsBuilder := func(result any) []Metric {
		if metrics, ok := result.([]Metric); ok {
			return metrics
		}
		return nil
	}

	return NewMetricsUITool(name, description, handler, metricsBuilder)
}

// CreateChartTool creates a UI tool for displaying charts.
func CreateChartTool(
	name, description string,
	chartType ChartType,
	dataFunc func(ctx context.Context, params map[string]any) (ChartData, error),
) *ChartUITool {
	handler := func(ctx context.Context, params map[string]any) (any, error) {
		return dataFunc(ctx, params)
	}

	dataBuilder := func(result any) ChartData {
		if data, ok := result.(ChartData); ok {
			return data
		}
		return ChartData{}
	}

	return NewChartUITool(name, description, chartType, handler, dataBuilder)
}
