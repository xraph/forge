package sdk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/xraph/forge"
)

// ToolRegistry manages available tools for agents.
type ToolRegistry struct {
	logger  forge.Logger
	metrics forge.Metrics

	mu    sync.RWMutex
	tools map[string]*ToolDefinition
}

// ToolDefinition defines a tool that can be used by agents.
type ToolDefinition struct {
	Name        string              `json:"name"`
	Version     string              `json:"version"`
	Description string              `json:"description"`
	Category    string              `json:"category"`
	Tags        []string            `json:"tags"`
	Parameters  ToolParameterSchema `json:"parameters"`
	Handler     ToolHandler         `json:"-"`
	Async       bool                `json:"async"`
	Timeout     time.Duration       `json:"timeout"`
	RetryConfig *RetryConfig        `json:"retry_config,omitempty"`
	Metadata    map[string]any      `json:"metadata,omitempty"`
	CreatedAt   time.Time           `json:"created_at"`
}

// ToolParameterSchema defines the JSON schema for tool parameters.
type ToolParameterSchema struct {
	Type       string                           `json:"type"`
	Properties map[string]ToolParameterProperty `json:"properties"`
	Required   []string                         `json:"required"`
}

// ToolParameterProperty defines a single parameter.
type ToolParameterProperty struct {
	Type        string   `json:"type"`
	Description string   `json:"description"`
	Enum        []string `json:"enum,omitempty"`
	Default     any      `json:"default,omitempty"`
	Minimum     *float64 `json:"minimum,omitempty"`
	Maximum     *float64 `json:"maximum,omitempty"`
}

// ToolHandler is the function signature for tool implementations.
type ToolHandler func(ctx context.Context, params map[string]any) (any, error)

// ToolExecutionResult represents the result of a tool execution.
type ToolExecutionResult struct {
	ToolName  string
	Success   bool
	Result    any
	Error     error
	Duration  time.Duration
	Timestamp time.Time
	Metadata  map[string]any
}

// NewToolRegistry creates a new tool registry.
func NewToolRegistry(logger forge.Logger, metrics forge.Metrics) *ToolRegistry {
	return &ToolRegistry{
		logger:  logger,
		metrics: metrics,
		tools:   make(map[string]*ToolDefinition),
	}
}

// RegisterTool registers a new tool.
func (tr *ToolRegistry) RegisterTool(tool *ToolDefinition) error {
	if tool.Name == "" {
		return errors.New("tool name is required")
	}

	if tool.Handler == nil {
		return errors.New("tool handler is required")
	}

	if tool.Version == "" {
		tool.Version = "1.0.0"
	}

	if tool.Timeout == 0 {
		tool.Timeout = 30 * time.Second
	}

	tool.CreatedAt = time.Now()

	tr.mu.Lock()
	defer tr.mu.Unlock()

	// Check for duplicate
	key := tr.toolKey(tool.Name, tool.Version)
	if _, exists := tr.tools[key]; exists {
		return fmt.Errorf("tool %s@%s already registered", tool.Name, tool.Version)
	}

	tr.tools[key] = tool

	if tr.logger != nil {
		tr.logger.Info("Tool registered",
			F("name", tool.Name),
			F("version", tool.Version),
			F("category", tool.Category),
		)
	}

	if tr.metrics != nil {
		tr.metrics.Counter("forge.ai.sdk.tools.registered",
			"name", tool.Name,
			"version", tool.Version,
		).Inc()
	}

	return nil
}

// RegisterFunc is a convenience method to register a function as a tool.
func (tr *ToolRegistry) RegisterFunc(name, description string, fn any) error {
	handler, schema, err := tr.wrapFunction(fn)
	if err != nil {
		return fmt.Errorf("failed to wrap function: %w", err)
	}

	tool := &ToolDefinition{
		Name:        name,
		Version:     "1.0.0",
		Description: description,
		Parameters:  schema,
		Handler:     handler,
	}

	return tr.RegisterTool(tool)
}

// wrapFunction wraps a Go function as a ToolHandler.
func (tr *ToolRegistry) wrapFunction(fn any) (ToolHandler, ToolParameterSchema, error) {
	fnValue := reflect.ValueOf(fn)
	fnType := fnValue.Type()

	if fnType.Kind() != reflect.Func {
		return nil, ToolParameterSchema{}, errors.New("not a function")
	}

	// Generate parameter schema from function signature
	schema := ToolParameterSchema{
		Type:       "object",
		Properties: make(map[string]ToolParameterProperty),
		Required:   make([]string, 0),
	}

	// Skip context parameter if present
	startParam := 0

	if fnType.NumIn() > 0 {
		firstParam := fnType.In(0)
		if firstParam.String() == "context.Context" {
			startParam = 1
		}
	}

	// Extract parameters (simplified - in production, use struct tags)
	for i := startParam; i < fnType.NumIn(); i++ {
		paramType := fnType.In(i)
		paramName := fmt.Sprintf("param%d", i-startParam)

		prop := ToolParameterProperty{
			Type:        tr.reflectTypeToJSONType(paramType),
			Description: fmt.Sprintf("Parameter %d", i-startParam),
		}

		schema.Properties[paramName] = prop
		schema.Required = append(schema.Required, paramName)
	}

	// Create handler wrapper
	handler := func(ctx context.Context, params map[string]any) (any, error) {
		// Build function arguments
		args := make([]reflect.Value, 0)

		// Add context if needed
		if startParam == 1 {
			args = append(args, reflect.ValueOf(ctx))
		}

		// Add other parameters
		for i := startParam; i < fnType.NumIn(); i++ {
			paramName := fmt.Sprintf("param%d", i-startParam)

			paramValue, ok := params[paramName]
			if !ok {
				return nil, fmt.Errorf("missing required parameter: %s", paramName)
			}

			// Convert to appropriate type
			argValue := reflect.ValueOf(paramValue)
			expectedType := fnType.In(i)

			if !argValue.Type().AssignableTo(expectedType) {
				// Try to convert
				if argValue.Type().ConvertibleTo(expectedType) {
					argValue = argValue.Convert(expectedType)
				} else {
					return nil, fmt.Errorf("parameter %s type mismatch", paramName)
				}
			}

			args = append(args, argValue)
		}

		// Call function
		results := fnValue.Call(args)

		// Handle return values (result, error) or just (result)
		if len(results) == 0 {
			return nil, nil
		}

		if len(results) == 1 {
			// Single return value
			if results[0].Type().Implements(reflect.TypeFor[error]()) {
				// It's an error
				if results[0].IsNil() {
					return nil, nil
				}

				return nil, results[0].Interface().(error)
			}

			return results[0].Interface(), nil
		}

		// Two return values: (result, error)
		result := results[0].Interface()
		if results[1].IsNil() {
			return result, nil
		}

		return result, results[1].Interface().(error)
	}

	return handler, schema, nil
}

// reflectTypeToJSONType converts Go types to JSON schema types.
func (tr *ToolRegistry) reflectTypeToJSONType(t reflect.Type) string {
	switch t.Kind() {
	case reflect.String:
		return "string"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return "integer"
	case reflect.Float32, reflect.Float64:
		return "number"
	case reflect.Bool:
		return "boolean"
	case reflect.Array, reflect.Slice:
		return "array"
	case reflect.Map, reflect.Struct:
		return "object"
	default:
		return "string"
	}
}

// GetTool retrieves a tool by name and optional version.
func (tr *ToolRegistry) GetTool(name, version string) (*ToolDefinition, error) {
	tr.mu.RLock()
	defer tr.mu.RUnlock()

	if version == "" {
		version = "1.0.0"
	}

	key := tr.toolKey(name, version)

	tool, exists := tr.tools[key]
	if !exists {
		return nil, fmt.Errorf("tool %s@%s not found", name, version)
	}

	return tool, nil
}

// ListTools returns all registered tools.
func (tr *ToolRegistry) ListTools() []*ToolDefinition {
	tr.mu.RLock()
	defer tr.mu.RUnlock()

	tools := make([]*ToolDefinition, 0, len(tr.tools))
	for _, tool := range tr.tools {
		tools = append(tools, tool)
	}

	return tools
}

// ListToolsByCategory returns tools in a specific category.
func (tr *ToolRegistry) ListToolsByCategory(category string) []*ToolDefinition {
	tr.mu.RLock()
	defer tr.mu.RUnlock()

	tools := make([]*ToolDefinition, 0)
	for _, tool := range tr.tools {
		if tool.Category == category {
			tools = append(tools, tool)
		}
	}

	return tools
}

// SearchTools searches for tools by tags or name.
func (tr *ToolRegistry) SearchTools(query string) []*ToolDefinition {
	tr.mu.RLock()
	defer tr.mu.RUnlock()

	tools := make([]*ToolDefinition, 0)
	queryLower := toLower(query)

	for _, tool := range tr.tools {
		// Check name
		if contains(toLower(tool.Name), queryLower) {
			tools = append(tools, tool)

			continue
		}

		// Check description
		if contains(toLower(tool.Description), queryLower) {
			tools = append(tools, tool)

			continue
		}

		// Check tags
		for _, tag := range tool.Tags {
			if contains(toLower(tag), queryLower) {
				tools = append(tools, tool)

				break
			}
		}
	}

	return tools
}

// ExecuteTool executes a tool with the given parameters.
func (tr *ToolRegistry) ExecuteTool(ctx context.Context, name, version string, params map[string]any) (*ToolExecutionResult, error) {
	startTime := time.Now()

	result := &ToolExecutionResult{
		ToolName:  name,
		Timestamp: startTime,
		Metadata:  make(map[string]any),
	}

	// Get tool
	tool, err := tr.GetTool(name, version)
	if err != nil {
		result.Error = err
		result.Duration = time.Since(startTime)

		return result, err
	}

	// Validate parameters
	if err := tr.validateParameters(tool, params); err != nil {
		result.Error = fmt.Errorf("parameter validation failed: %w", err)
		result.Duration = time.Since(startTime)

		return result, result.Error
	}

	// Create context with timeout
	execCtx, cancel := context.WithTimeout(ctx, tool.Timeout)
	defer cancel()

	// Execute with retry if configured
	var (
		execErr    error
		execResult any
	)

	executeFunc := func(ctx context.Context) error {
		var err error

		execResult, err = tool.Handler(execCtx, params)

		return err
	}

	if tool.RetryConfig != nil {
		execErr = Retry(execCtx, *tool.RetryConfig, tr.logger, executeFunc)
	} else {
		execErr = executeFunc(execCtx)
	}

	result.Duration = time.Since(startTime)
	result.Result = execResult
	result.Error = execErr
	result.Success = execErr == nil

	// Record metrics
	if tr.metrics != nil {
		tr.metrics.Counter("forge.ai.sdk.tools.executions",
			"tool", name,
			"version", version,
			"success", strconv.FormatBool(result.Success),
		).Inc()

		tr.metrics.Histogram("forge.ai.sdk.tools.duration",
			"tool", name,
			"version", version,
		).Observe(result.Duration.Seconds())
	}

	if tr.logger != nil {
		if execErr != nil {
			tr.logger.Warn("Tool execution failed",
				F("tool", name),
				F("version", version),
				F("duration", result.Duration),
				F("error", execErr.Error()),
			)
		} else {
			tr.logger.Debug("Tool executed successfully",
				F("tool", name),
				F("version", version),
				F("duration", result.Duration),
			)
		}
	}

	return result, execErr
}

// validateParameters validates tool parameters against the schema.
func (tr *ToolRegistry) validateParameters(tool *ToolDefinition, params map[string]any) error {
	// Check required parameters
	for _, required := range tool.Parameters.Required {
		if _, exists := params[required]; !exists {
			return fmt.Errorf("missing required parameter: %s", required)
		}
	}

	// Validate parameter types
	for paramName, paramValue := range params {
		prop, exists := tool.Parameters.Properties[paramName]
		if !exists {
			return fmt.Errorf("unknown parameter: %s", paramName)
		}

		// Basic type validation
		if err := tr.validateParameterType(paramValue, prop); err != nil {
			return fmt.Errorf("parameter %s: %w", paramName, err)
		}
	}

	return nil
}

// validateParameterType validates a parameter value against its schema.
func (tr *ToolRegistry) validateParameterType(value any, prop ToolParameterProperty) error {
	if value == nil {
		return nil
	}

	valueType := reflect.TypeOf(value)
	expectedType := prop.Type

	switch expectedType {
	case "string":
		if valueType.Kind() != reflect.String {
			return fmt.Errorf("expected string, got %s", valueType.Kind())
		}

		// Check enum
		if len(prop.Enum) > 0 {
			strValue := value.(string)
			found := slices.Contains(prop.Enum, strValue)

			if !found {
				return fmt.Errorf("value must be one of: %v", prop.Enum)
			}
		}

	case "integer":
		switch valueType.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
			reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			// Valid integer type
		case reflect.Float32, reflect.Float64:
			// Allow floats that are whole numbers
			floatValue := reflect.ValueOf(value).Float()
			if floatValue != float64(int64(floatValue)) {
				return errors.New("expected integer, got float with decimals")
			}
		default:
			return fmt.Errorf("expected integer, got %s", valueType.Kind())
		}

	case "number":
		switch valueType.Kind() {
		case reflect.Float32, reflect.Float64,
			reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
			reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			// Valid numeric type
		default:
			return fmt.Errorf("expected number, got %s", valueType.Kind())
		}

	case "boolean":
		if valueType.Kind() != reflect.Bool {
			return fmt.Errorf("expected boolean, got %s", valueType.Kind())
		}

	case "array":
		if valueType.Kind() != reflect.Array && valueType.Kind() != reflect.Slice {
			return fmt.Errorf("expected array, got %s", valueType.Kind())
		}

	case "object":
		if valueType.Kind() != reflect.Map && valueType.Kind() != reflect.Struct {
			return fmt.Errorf("expected object, got %s", valueType.Kind())
		}
	}

	return nil
}

// UnregisterTool removes a tool from the registry.
func (tr *ToolRegistry) UnregisterTool(name, version string) error {
	tr.mu.Lock()
	defer tr.mu.Unlock()

	if version == "" {
		version = "1.0.0"
	}

	key := tr.toolKey(name, version)
	if _, exists := tr.tools[key]; !exists {
		return fmt.Errorf("tool %s@%s not found", name, version)
	}

	delete(tr.tools, key)

	if tr.logger != nil {
		tr.logger.Info("Tool unregistered",
			F("name", name),
			F("version", version),
		)
	}

	return nil
}

// ExportSchema exports all tools as OpenAI function calling format.
func (tr *ToolRegistry) ExportSchema() []map[string]any {
	tr.mu.RLock()
	defer tr.mu.RUnlock()

	schemas := make([]map[string]any, 0, len(tr.tools))

	for _, tool := range tr.tools {
		schema := map[string]any{
			"name":        tool.Name,
			"description": tool.Description,
			"parameters":  tool.Parameters,
		}
		schemas = append(schemas, schema)
	}

	return schemas
}

// toolKey generates a unique key for a tool.
func (tr *ToolRegistry) toolKey(name, version string) string {
	return fmt.Sprintf("%s@%s", name, version)
}

// Helper functions.
func toLower(s string) string {
	return strings.ToLower(s)
}

func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}

// MarshalJSON for ToolDefinition (excludes handler).
func (td *ToolDefinition) MarshalJSON() ([]byte, error) {
	type Alias ToolDefinition

	return json.Marshal(&struct {
		*Alias

		Handler any `json:"handler,omitempty"`
	}{
		Alias:   (*Alias)(td),
		Handler: nil, // Exclude handler from JSON
	})
}

// IsUITool checks if a tool is marked as a UI tool.
func (tr *ToolRegistry) IsUITool(name, version string) bool {
	tool, err := tr.GetTool(name, version)
	if err != nil {
		return false
	}

	if tool.Metadata == nil {
		return false
	}

	uiTool, ok := tool.Metadata["ui_tool"].(bool)
	return ok && uiTool
}

// GetUIHints returns UI hints for a tool if it's a UI tool.
func (tr *ToolRegistry) GetUIHints(name, version string) (UIToolHints, bool) {
	tool, err := tr.GetTool(name, version)
	if err != nil {
		return UIToolHints{}, false
	}

	if tool.Metadata == nil {
		return UIToolHints{}, false
	}

	if hints, ok := tool.Metadata["ui_hints"].(UIToolHints); ok {
		return hints, true
	}

	return UIToolHints{}, false
}

// RegisterToolWithUIHints registers a tool with UI rendering hints.
// This is a convenience method for tools that want UI rendering without
// implementing the full UITool interface.
func (tr *ToolRegistry) RegisterToolWithUIHints(tool *ToolDefinition, hints UIToolHints) error {
	if tool.Metadata == nil {
		tool.Metadata = make(map[string]any)
	}

	tool.Metadata["ui_tool"] = true
	tool.Metadata["ui_hints"] = hints

	return tr.RegisterTool(tool)
}

// ListUITools returns all tools that have UI rendering hints.
func (tr *ToolRegistry) ListUITools() []*ToolDefinition {
	tr.mu.RLock()
	defer tr.mu.RUnlock()

	result := make([]*ToolDefinition, 0)
	for _, tool := range tr.tools {
		if tool.Metadata != nil {
			if uiTool, ok := tool.Metadata["ui_tool"].(bool); ok && uiTool {
				result = append(result, tool)
			}
		}
	}

	return result
}

// ExportSchemaWithUIHints exports tools with UI hints included.
func (tr *ToolRegistry) ExportSchemaWithUIHints() []map[string]any {
	tr.mu.RLock()
	defer tr.mu.RUnlock()

	schemas := make([]map[string]any, 0, len(tr.tools))

	for _, tool := range tr.tools {
		schema := map[string]any{
			"name":        tool.Name,
			"description": tool.Description,
			"parameters":  tool.Parameters,
		}

		// Include UI hints if present
		if tool.Metadata != nil {
			if hints, ok := tool.Metadata["ui_hints"].(UIToolHints); ok {
				schema["ui_hints"] = hints
			}
		}

		schemas = append(schemas, schema)
	}

	return schemas
}
