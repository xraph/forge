package sdk

import (
	"context"
	"errors"
	"testing"
	"time"
)

// Test NewToolRegistry

func TestNewToolRegistry(t *testing.T) {
	registry := NewToolRegistry(nil, nil)

	if registry == nil {
		t.Fatal("expected registry to be created")
	}

	if len(registry.tools) != 0 {
		t.Error("expected empty tools initially")
	}
}

// Test RegisterTool

func TestToolRegistry_RegisterTool(t *testing.T) {
	registry := NewToolRegistry(nil, nil)

	tool := &ToolDefinition{
		Name:        "test_tool",
		Version:     "1.0.0",
		Description: "A test tool",
		Handler: func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
			return "success", nil
		},
	}

	err := registry.RegisterTool(tool)

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if len(registry.tools) != 1 {
		t.Errorf("expected 1 tool, got %d", len(registry.tools))
	}
}

func TestToolRegistry_RegisterTool_NoName(t *testing.T) {
	registry := NewToolRegistry(nil, nil)

	tool := &ToolDefinition{
		Handler: func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
			return nil, nil
		},
	}

	err := registry.RegisterTool(tool)

	if err == nil {
		t.Error("expected error for tool without name")
	}
}

func TestToolRegistry_RegisterTool_NoHandler(t *testing.T) {
	registry := NewToolRegistry(nil, nil)

	tool := &ToolDefinition{
		Name: "test_tool",
	}

	err := registry.RegisterTool(tool)

	if err == nil {
		t.Error("expected error for tool without handler")
	}
}

func TestToolRegistry_RegisterTool_Duplicate(t *testing.T) {
	registry := NewToolRegistry(nil, nil)

	tool := &ToolDefinition{
		Name:    "test_tool",
		Version: "1.0.0",
		Handler: func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
			return nil, nil
		},
	}

	registry.RegisterTool(tool)
	err := registry.RegisterTool(tool)

	if err == nil {
		t.Error("expected error for duplicate tool")
	}
}

func TestToolRegistry_RegisterTool_Defaults(t *testing.T) {
	registry := NewToolRegistry(nil, nil)

	tool := &ToolDefinition{
		Name: "test_tool",
		Handler: func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
			return nil, nil
		},
	}

	registry.RegisterTool(tool)

	if tool.Version != "1.0.0" {
		t.Errorf("expected default version 1.0.0, got %s", tool.Version)
	}

	if tool.Timeout != 30*time.Second {
		t.Errorf("expected default timeout 30s, got %v", tool.Timeout)
	}
}

// Test RegisterFunc

func TestToolRegistry_RegisterFunc_Simple(t *testing.T) {
	registry := NewToolRegistry(nil, nil)

	fn := func(ctx context.Context, name string) (string, error) {
		return "Hello, " + name, nil
	}

	err := registry.RegisterFunc("greet", "Greets a person", fn)

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if len(registry.tools) != 1 {
		t.Errorf("expected 1 tool, got %d", len(registry.tools))
	}
}

func TestToolRegistry_RegisterFunc_NoContext(t *testing.T) {
	registry := NewToolRegistry(nil, nil)

	fn := func(a, b int) int {
		return a + b
	}

	err := registry.RegisterFunc("add", "Adds two numbers", fn)

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestToolRegistry_RegisterFunc_NotAFunction(t *testing.T) {
	registry := NewToolRegistry(nil, nil)

	err := registry.RegisterFunc("invalid", "Not a function", "not a function")

	if err == nil {
		t.Error("expected error for non-function")
	}
}

// Test GetTool

func TestToolRegistry_GetTool(t *testing.T) {
	registry := NewToolRegistry(nil, nil)

	original := &ToolDefinition{
		Name:    "test_tool",
		Version: "1.0.0",
		Handler: func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
			return nil, nil
		},
	}

	registry.RegisterTool(original)

	retrieved, err := registry.GetTool("test_tool", "1.0.0")

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if retrieved.Name != "test_tool" {
		t.Errorf("expected tool name 'test_tool', got '%s'", retrieved.Name)
	}
}

func TestToolRegistry_GetTool_DefaultVersion(t *testing.T) {
	registry := NewToolRegistry(nil, nil)

	tool := &ToolDefinition{
		Name:    "test_tool",
		Version: "1.0.0",
		Handler: func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
			return nil, nil
		},
	}

	registry.RegisterTool(tool)

	retrieved, err := registry.GetTool("test_tool", "")

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if retrieved == nil {
		t.Error("expected tool to be retrieved")
	}
}

func TestToolRegistry_GetTool_NotFound(t *testing.T) {
	registry := NewToolRegistry(nil, nil)

	_, err := registry.GetTool("nonexistent", "1.0.0")

	if err == nil {
		t.Error("expected error for nonexistent tool")
	}
}

// Test ListTools

func TestToolRegistry_ListTools(t *testing.T) {
	registry := NewToolRegistry(nil, nil)

	for i := 0; i < 3; i++ {
		tool := &ToolDefinition{
			Name:    "tool_" + string(rune('a'+i)),
			Version: "1.0.0",
			Handler: func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
				return nil, nil
			},
		}
		registry.RegisterTool(tool)
	}

	tools := registry.ListTools()

	if len(tools) != 3 {
		t.Errorf("expected 3 tools, got %d", len(tools))
	}
}

// Test ListToolsByCategory

func TestToolRegistry_ListToolsByCategory(t *testing.T) {
	registry := NewToolRegistry(nil, nil)

	categories := []string{"math", "string", "math"}
	for i, cat := range categories {
		tool := &ToolDefinition{
			Name:     "tool_" + string(rune('a'+i)),
			Version:  "1.0.0",
			Category: cat,
			Handler: func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
				return nil, nil
			},
		}
		registry.RegisterTool(tool)
	}

	mathTools := registry.ListToolsByCategory("math")

	if len(mathTools) != 2 {
		t.Errorf("expected 2 math tools, got %d", len(mathTools))
	}
}

// Test SearchTools

func TestToolRegistry_SearchTools_ByName(t *testing.T) {
	registry := NewToolRegistry(nil, nil)

	tool := &ToolDefinition{
		Name:    "calculator",
		Version: "1.0.0",
		Handler: func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
			return nil, nil
		},
	}

	registry.RegisterTool(tool)

	results := registry.SearchTools("calc")

	if len(results) != 1 {
		t.Errorf("expected 1 result, got %d", len(results))
	}
}

func TestToolRegistry_SearchTools_ByDescription(t *testing.T) {
	registry := NewToolRegistry(nil, nil)

	tool := &ToolDefinition{
		Name:        "tool_a",
		Version:     "1.0.0",
		Description: "Performs mathematical operations",
		Handler: func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
			return nil, nil
		},
	}

	registry.RegisterTool(tool)

	results := registry.SearchTools("mathematical")

	if len(results) != 1 {
		t.Errorf("expected 1 result, got %d", len(results))
	}
}

func TestToolRegistry_SearchTools_ByTag(t *testing.T) {
	registry := NewToolRegistry(nil, nil)

	tool := &ToolDefinition{
		Name:    "tool_a",
		Version: "1.0.0",
		Tags:    []string{"utility", "text"},
		Handler: func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
			return nil, nil
		},
	}

	registry.RegisterTool(tool)

	results := registry.SearchTools("utility")

	if len(results) != 1 {
		t.Errorf("expected 1 result, got %d", len(results))
	}
}

// Test ExecuteTool

func TestToolRegistry_ExecuteTool_Success(t *testing.T) {
	registry := NewToolRegistry(nil, nil)

	tool := &ToolDefinition{
		Name:    "echo",
		Version: "1.0.0",
		Parameters: ToolParameterSchema{
			Type: "object",
			Properties: map[string]ToolParameterProperty{
				"message": {Type: "string"},
			},
			Required: []string{"message"},
		},
		Handler: func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
			return params["message"], nil
		},
	}

	registry.RegisterTool(tool)

	result, err := registry.ExecuteTool(context.Background(), "echo", "1.0.0", map[string]interface{}{
		"message": "hello",
	})

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if !result.Success {
		t.Error("expected successful execution")
	}

	if result.Result != "hello" {
		t.Errorf("expected result 'hello', got '%v'", result.Result)
	}
}

func TestToolRegistry_ExecuteTool_Error(t *testing.T) {
	registry := NewToolRegistry(nil, nil)

	testErr := errors.New("tool error")

	tool := &ToolDefinition{
		Name:    "failing_tool",
		Version: "1.0.0",
		Parameters: ToolParameterSchema{
			Type:       "object",
			Properties: map[string]ToolParameterProperty{},
		},
		Handler: func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
			return nil, testErr
		},
	}

	registry.RegisterTool(tool)

	result, err := registry.ExecuteTool(context.Background(), "failing_tool", "1.0.0", map[string]interface{}{})

	if err == nil {
		t.Error("expected error from tool execution")
	}

	if result.Success {
		t.Error("expected unsuccessful execution")
	}
}

func TestToolRegistry_ExecuteTool_MissingParameter(t *testing.T) {
	registry := NewToolRegistry(nil, nil)

	tool := &ToolDefinition{
		Name:    "tool",
		Version: "1.0.0",
		Parameters: ToolParameterSchema{
			Type: "object",
			Properties: map[string]ToolParameterProperty{
				"required_param": {Type: "string"},
			},
			Required: []string{"required_param"},
		},
		Handler: func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
			return nil, nil
		},
	}

	registry.RegisterTool(tool)

	_, err := registry.ExecuteTool(context.Background(), "tool", "1.0.0", map[string]interface{}{})

	if err == nil {
		t.Error("expected error for missing parameter")
	}
}

func TestToolRegistry_ExecuteTool_Timeout(t *testing.T) {
	registry := NewToolRegistry(nil, nil)

	tool := &ToolDefinition{
		Name:    "slow_tool",
		Version: "1.0.0",
		Timeout: 50 * time.Millisecond,
		Parameters: ToolParameterSchema{
			Type:       "object",
			Properties: map[string]ToolParameterProperty{},
		},
		Handler: func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
			// Respect context cancellation
			select {
			case <-time.After(200 * time.Millisecond):
				return nil, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		},
	}

	registry.RegisterTool(tool)

	result, err := registry.ExecuteTool(context.Background(), "slow_tool", "1.0.0", map[string]interface{}{})

	if err == nil {
		t.Error("expected timeout error")
	}

	if result.Success {
		t.Error("expected unsuccessful execution due to timeout")
	}
}

// Test UnregisterTool

func TestToolRegistry_UnregisterTool(t *testing.T) {
	registry := NewToolRegistry(nil, nil)

	tool := &ToolDefinition{
		Name:    "test_tool",
		Version: "1.0.0",
		Handler: func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
			return nil, nil
		},
	}

	registry.RegisterTool(tool)

	err := registry.UnregisterTool("test_tool", "1.0.0")

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if len(registry.tools) != 0 {
		t.Errorf("expected 0 tools after unregister, got %d", len(registry.tools))
	}
}

func TestToolRegistry_UnregisterTool_NotFound(t *testing.T) {
	registry := NewToolRegistry(nil, nil)

	err := registry.UnregisterTool("nonexistent", "1.0.0")

	if err == nil {
		t.Error("expected error for unregistering nonexistent tool")
	}
}

// Test ExportSchema

func TestToolRegistry_ExportSchema(t *testing.T) {
	registry := NewToolRegistry(nil, nil)

	tool := &ToolDefinition{
		Name:        "test_tool",
		Version:     "1.0.0",
		Description: "A test tool",
		Parameters: ToolParameterSchema{
			Type: "object",
			Properties: map[string]ToolParameterProperty{
				"param1": {Type: "string"},
			},
		},
		Handler: func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
			return nil, nil
		},
	}

	registry.RegisterTool(tool)

	schemas := registry.ExportSchema()

	if len(schemas) != 1 {
		t.Errorf("expected 1 schema, got %d", len(schemas))
	}

	if schemas[0]["name"] != "test_tool" {
		t.Errorf("expected name 'test_tool', got '%v'", schemas[0]["name"])
	}
}

// Test Parameter Validation

func TestToolRegistry_ValidateParameters_String(t *testing.T) {
	registry := NewToolRegistry(nil, nil)

	prop := ToolParameterProperty{Type: "string"}

	err := registry.validateParameterType("hello", prop)
	if err != nil {
		t.Errorf("expected no error for valid string, got %v", err)
	}

	err = registry.validateParameterType(123, prop)
	if err == nil {
		t.Error("expected error for integer when string expected")
	}
}

func TestToolRegistry_ValidateParameters_Integer(t *testing.T) {
	registry := NewToolRegistry(nil, nil)

	prop := ToolParameterProperty{Type: "integer"}

	err := registry.validateParameterType(42, prop)
	if err != nil {
		t.Errorf("expected no error for valid integer, got %v", err)
	}

	err = registry.validateParameterType("not an int", prop)
	if err == nil {
		t.Error("expected error for string when integer expected")
	}
}

func TestToolRegistry_ValidateParameters_Number(t *testing.T) {
	registry := NewToolRegistry(nil, nil)

	prop := ToolParameterProperty{Type: "number"}

	err := registry.validateParameterType(3.14, prop)
	if err != nil {
		t.Errorf("expected no error for valid number, got %v", err)
	}

	err = registry.validateParameterType(42, prop)
	if err != nil {
		t.Errorf("expected no error for integer as number, got %v", err)
	}
}

func TestToolRegistry_ValidateParameters_Boolean(t *testing.T) {
	registry := NewToolRegistry(nil, nil)

	prop := ToolParameterProperty{Type: "boolean"}

	err := registry.validateParameterType(true, prop)
	if err != nil {
		t.Errorf("expected no error for valid boolean, got %v", err)
	}

	err = registry.validateParameterType("true", prop)
	if err == nil {
		t.Error("expected error for string when boolean expected")
	}
}

func TestToolRegistry_ValidateParameters_Enum(t *testing.T) {
	registry := NewToolRegistry(nil, nil)

	prop := ToolParameterProperty{
		Type: "string",
		Enum: []string{"red", "green", "blue"},
	}

	err := registry.validateParameterType("red", prop)
	if err != nil {
		t.Errorf("expected no error for valid enum value, got %v", err)
	}

	err = registry.validateParameterType("yellow", prop)
	if err == nil {
		t.Error("expected error for invalid enum value")
	}
}

// Test Function Wrapping

func TestToolRegistry_WrapFunction_WithContext(t *testing.T) {
	registry := NewToolRegistry(nil, nil)

	called := false
	fn := func(ctx context.Context, name string) (string, error) {
		called = true
		return "Hello, " + name, nil
	}

	handler, schema, err := registry.wrapFunction(fn)
	if err != nil {
		t.Fatalf("expected no error wrapping function, got %v", err)
	}

	if schema.Type != "object" {
		t.Errorf("expected object schema, got %s", schema.Type)
	}

	// Execute wrapped function
	result, err := handler(context.Background(), map[string]interface{}{
		"param0": "World",
	})

	if err != nil {
		t.Fatalf("expected no error executing wrapped function, got %v", err)
	}

	if !called {
		t.Error("expected function to be called")
	}

	if result != "Hello, World" {
		t.Errorf("expected 'Hello, World', got '%v'", result)
	}
}

func TestToolRegistry_WrapFunction_NoContext(t *testing.T) {
	registry := NewToolRegistry(nil, nil)

	fn := func(a, b int) int {
		return a + b
	}

	handler, _, err := registry.wrapFunction(fn)
	if err != nil {
		t.Fatalf("expected no error wrapping function, got %v", err)
	}

	result, err := handler(context.Background(), map[string]interface{}{
		"param0": 5,
		"param1": 3,
	})

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if result != 8 {
		t.Errorf("expected 8, got %v", result)
	}
}

func TestToolRegistry_WrapFunction_ErrorReturn(t *testing.T) {
	registry := NewToolRegistry(nil, nil)

	testErr := errors.New("test error")
	fn := func() error {
		return testErr
	}

	handler, _, err := registry.wrapFunction(fn)
	if err != nil {
		t.Fatalf("expected no error wrapping function, got %v", err)
	}

	_, err = handler(context.Background(), map[string]interface{}{})

	if !errors.Is(err, testErr) {
		t.Errorf("expected test error, got %v", err)
	}
}

// Test MarshalJSON

func TestToolDefinition_MarshalJSON(t *testing.T) {
	tool := &ToolDefinition{
		Name:        "test_tool",
		Version:     "1.0.0",
		Description: "A test tool",
		Handler: func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
			return nil, nil
		},
	}

	data, err := tool.MarshalJSON()

	if err != nil {
		t.Fatalf("expected no error marshaling, got %v", err)
	}

	if len(data) == 0 {
		t.Error("expected non-empty JSON data")
	}

	// Handler should not be in JSON
	if contains(string(data), "handler") {
		t.Error("expected handler to be excluded from JSON")
	}
}

// Test Thread Safety

func TestToolRegistry_ThreadSafety(t *testing.T) {
	registry := NewToolRegistry(nil, nil)

	done := make(chan bool)

	// Concurrent registrations
	for i := 0; i < 5; i++ {
		go func(index int) {
			tool := &ToolDefinition{
				Name:    "tool_" + string(rune('a'+index)),
				Version: "1.0.0",
				Handler: func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
					return nil, nil
				},
			}
			registry.RegisterTool(tool)
			done <- true
		}(i)
	}

	// Concurrent reads
	for i := 0; i < 5; i++ {
		go func() {
			registry.ListTools()
			registry.SearchTools("tool")
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// Should have 5 tools registered
	if len(registry.tools) != 5 {
		t.Errorf("expected 5 tools, got %d", len(registry.tools))
	}
}

