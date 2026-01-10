package sdk

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/xraph/forge/extensions/ai/llm"
)

func TestBaseUITool(t *testing.T) {
	handler := func(ctx context.Context, params map[string]any) (any, error) {
		return map[string]any{"result": "success"}, nil
	}

	tool := NewBaseUITool(BaseUIToolConfig{
		Name:        "test_tool",
		Description: "A test tool",
		Version:     "1.0.0",
		Parameters: ToolParameterSchema{
			Type:       "object",
			Properties: map[string]ToolParameterProperty{},
		},
		Hints: UIToolHints{
			PreferredPartType: PartTypeTable,
			StreamingEnabled:  true,
		},
		Handler: handler,
	})

	if tool.Name() != "test_tool" {
		t.Errorf("Expected name test_tool, got %s", tool.Name())
	}

	if tool.Description() != "A test tool" {
		t.Errorf("Expected description 'A test tool', got %s", tool.Description())
	}

	if !tool.SupportsStreaming() {
		t.Error("Expected streaming to be enabled")
	}

	hints := tool.GetUIHints()
	if hints.PreferredPartType != PartTypeTable {
		t.Errorf("Expected part type table, got %s", hints.PreferredPartType)
	}
}

func TestUIToolBuilder(t *testing.T) {
	tool := NewUITool("query_tool", "Execute queries").
		WithVersion("2.0.0").
		WithParameter("query", "string", "The SQL query", true).
		WithParameter("limit", "integer", "Row limit", false).
		WithPreferredPartType(PartTypeTable).
		WithStreaming(true).
		WithInteractiveFields("query").
		WithActionHandler(ActionHandler{
			ID:          "run",
			Name:        "Run Query",
			Description: "Execute the query",
		}).
		WithRenderOption("sortable", true).
		Collapsible(false).
		WithHandler(func(ctx context.Context, params map[string]any) (any, error) {
			return []map[string]any{{"id": 1, "name": "test"}}, nil
		}).
		Build()

	if tool.Name() != "query_tool" {
		t.Errorf("Expected name query_tool, got %s", tool.Name())
	}

	params := tool.GetParameters()
	if len(params.Properties) != 2 {
		t.Errorf("Expected 2 parameters, got %d", len(params.Properties))
	}

	if len(params.Required) != 1 {
		t.Errorf("Expected 1 required parameter, got %d", len(params.Required))
	}

	hints := tool.GetUIHints()
	if hints.PreferredPartType != PartTypeTable {
		t.Errorf("Expected part type table, got %s", hints.PreferredPartType)
	}

	if !hints.StreamingEnabled {
		t.Error("Expected streaming to be enabled")
	}

	if len(hints.InteractiveFields) != 1 {
		t.Errorf("Expected 1 interactive field, got %d", len(hints.InteractiveFields))
	}

	if len(hints.ActionHandlers) != 1 {
		t.Errorf("Expected 1 action handler, got %d", len(hints.ActionHandlers))
	}

	if hints.RenderOptions["sortable"] != true {
		t.Error("Expected sortable render option")
	}

	if !hints.Collapsible {
		t.Error("Expected collapsible to be true")
	}
}

func TestUIToolExecution(t *testing.T) {
	tool := NewUITool("test_exec", "Test execution").
		WithParameter("input", "string", "Input value", true).
		WithHandler(func(ctx context.Context, params map[string]any) (any, error) {
			input := params["input"].(string)

			return map[string]any{"output": "processed: " + input}, nil
		}).
		Build()

	result, err := tool.Execute(context.Background(), map[string]any{"input": "test"})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	resultMap, ok := result.(map[string]any)
	if !ok {
		t.Fatal("Expected result to be map[string]any")
	}

	if resultMap["output"] != "processed: test" {
		t.Errorf("Expected 'processed: test', got %v", resultMap["output"])
	}
}

func TestTableUITool(t *testing.T) {
	handler := func(ctx context.Context, params map[string]any) (any, error) {
		return []map[string]any{
			{"id": 1, "name": "Alice"},
			{"id": 2, "name": "Bob"},
		}, nil
	}

	tool := NewTableUITool("data_query", "Query data", handler, TableUIConfig{
		BatchSize:  5,
		Sortable:   true,
		Searchable: true,
		Paginated:  true,
		PageSize:   20,
	})

	if tool.Name() != "data_query" {
		t.Errorf("Expected name data_query, got %s", tool.Name())
	}

	hints := tool.GetUIHints()
	if hints.PreferredPartType != PartTypeTable {
		t.Errorf("Expected part type table, got %s", hints.PreferredPartType)
	}

	if !hints.StreamingEnabled {
		t.Error("Expected streaming to be enabled")
	}
}

func TestChartUITool(t *testing.T) {
	handler := func(ctx context.Context, params map[string]any) (any, error) {
		return ChartData{
			Labels:   []string{"Jan", "Feb", "Mar"},
			Datasets: []ChartDataset{{Label: "Sales", Data: []float64{100, 150, 200}}},
		}, nil
	}

	dataBuilder := func(result any) ChartData {
		if data, ok := result.(ChartData); ok {
			return data
		}

		return ChartData{}
	}

	tool := NewChartUITool("sales_chart", "Sales chart", ChartLine, handler, dataBuilder)

	if tool.Name() != "sales_chart" {
		t.Errorf("Expected name sales_chart, got %s", tool.Name())
	}

	hints := tool.GetUIHints()
	if hints.PreferredPartType != PartTypeChart {
		t.Errorf("Expected part type chart, got %s", hints.PreferredPartType)
	}
}

func TestMetricsUITool(t *testing.T) {
	handler := func(ctx context.Context, params map[string]any) (any, error) {
		return []Metric{
			{Label: "Users", Value: 1000},
			{Label: "Revenue", Value: 50000, Unit: "$"},
		}, nil
	}

	metricsBuilder := func(result any) []Metric {
		if metrics, ok := result.([]Metric); ok {
			return metrics
		}

		return nil
	}

	tool := NewMetricsUITool("dashboard", "Dashboard metrics", handler, metricsBuilder)

	if tool.Name() != "dashboard" {
		t.Errorf("Expected name dashboard, got %s", tool.Name())
	}

	hints := tool.GetUIHints()
	if hints.PreferredPartType != PartTypeMetric {
		t.Errorf("Expected part type metric, got %s", hints.PreferredPartType)
	}

	if !hints.StreamingEnabled {
		t.Error("Expected streaming to be enabled")
	}
}

func TestUIToolRegistry(t *testing.T) {
	registry := NewUIToolRegistry(nil, nil)

	tool := NewUITool("test_tool", "Test tool").
		WithPreferredPartType(PartTypeTable).
		WithHandler(func(ctx context.Context, params map[string]any) (any, error) {
			return nil, nil
		}).
		Build()

	// Register UI tool
	if err := registry.RegisterUITool(tool); err != nil {
		t.Fatalf("RegisterUITool failed: %v", err)
	}

	// Get UI tool
	retrieved, err := registry.GetUITool("test_tool")
	if err != nil {
		t.Fatalf("GetUITool failed: %v", err)
	}

	if retrieved.Name() != "test_tool" {
		t.Errorf("Expected name test_tool, got %s", retrieved.Name())
	}

	// Check IsUITool
	if !registry.IsUITool("test_tool") {
		t.Error("Expected tool to be recognized as UI tool")
	}

	// List UI tools
	tools := registry.ListUITools()
	if len(tools) != 1 {
		t.Errorf("Expected 1 UI tool, got %d", len(tools))
	}

	// Duplicate registration should fail
	if err := registry.RegisterUITool(tool); err == nil {
		t.Error("Expected error on duplicate registration")
	}
}

func TestUIToolRegistryExecution(t *testing.T) {
	var (
		events []llm.ClientStreamEvent
		mu     sync.Mutex
	)

	onEvent := func(event llm.ClientStreamEvent) error {
		mu.Lock()

		events = append(events, event)

		mu.Unlock()

		return nil
	}

	registry := NewUIToolRegistry(nil, nil)

	renderFunc := func(ctx context.Context, result any, streamer *UIPartStreamer) error {
		_ = streamer.Start()
		_ = streamer.StreamContent(result)

		return streamer.End()
	}

	tool := NewUITool("render_tool", "Tool with rendering").
		WithPreferredPartType(PartTypeCard).
		WithHandler(func(ctx context.Context, params map[string]any) (any, error) {
			return map[string]any{"title": "Test", "description": "Description"}, nil
		}).
		WithRenderFunc(renderFunc).
		Build()

	_ = registry.RegisterUITool(tool)

	result, err := registry.ExecuteUITool(
		context.Background(),
		"render_tool",
		map[string]any{},
		onEvent,
		"exec-1",
	)
	if err != nil {
		t.Fatalf("ExecuteUITool failed: %v", err)
	}

	if !result.Success {
		t.Error("Expected execution to succeed")
	}

	if result.PartID == "" {
		t.Error("Expected PartID to be set")
	}

	mu.Lock()
	defer mu.Unlock()

	// Should have start, content, end events
	if len(events) != 3 {
		t.Errorf("Expected 3 events, got %d", len(events))
	}
}

func TestUIToolExecutionResult_JSON(t *testing.T) {
	result := &UIToolExecutionResult{
		ToolName:  "test_tool",
		Success:   true,
		Result:    map[string]any{"key": "value"},
		PartID:    "part-123",
		Duration:  100 * 1000000, // 100ms
		Timestamp: time.Now(),
	}

	data, err := result.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON failed: %v", err)
	}

	if len(data) == 0 {
		t.Error("Expected non-empty JSON")
	}
}

func TestCreateDataQueryTool(t *testing.T) {
	queryFunc := func(ctx context.Context, query string) ([]map[string]any, error) {
		return []map[string]any{
			{"id": 1, "value": "a"},
			{"id": 2, "value": "b"},
		}, nil
	}

	tool := CreateDataQueryTool("db_query", "Execute database query", queryFunc)

	if tool.Name() != "db_query" {
		t.Errorf("Expected name db_query, got %s", tool.Name())
	}

	hints := tool.GetUIHints()
	if !hints.StreamingEnabled {
		t.Error("Expected streaming to be enabled")
	}
}

func TestCreateMetricsDashboardTool(t *testing.T) {
	metricsFunc := func(ctx context.Context) ([]Metric, error) {
		return []Metric{
			{Label: "Test", Value: 100},
		}, nil
	}

	tool := CreateMetricsDashboardTool("metrics", "Get metrics", metricsFunc)

	if tool.Name() != "metrics" {
		t.Errorf("Expected name metrics, got %s", tool.Name())
	}

	hints := tool.GetUIHints()
	if hints.PreferredPartType != PartTypeMetric {
		t.Errorf("Expected part type metric, got %s", hints.PreferredPartType)
	}
}

func TestCreateChartTool(t *testing.T) {
	dataFunc := func(ctx context.Context, params map[string]any) (ChartData, error) {
		return ChartData{
			Labels: []string{"A", "B", "C"},
			Datasets: []ChartDataset{
				{Label: "Data", Data: []float64{1, 2, 3}},
			},
		}, nil
	}

	tool := CreateChartTool("line_chart", "Line chart", ChartLine, dataFunc)

	if tool.Name() != "line_chart" {
		t.Errorf("Expected name line_chart, got %s", tool.Name())
	}

	hints := tool.GetUIHints()
	if hints.PreferredPartType != PartTypeChart {
		t.Errorf("Expected part type chart, got %s", hints.PreferredPartType)
	}
}

func TestToolRegistryUIToolHelpers(t *testing.T) {
	registry := NewToolRegistry(nil, nil)

	tool := &ToolDefinition{
		Name:        "ui_test_tool",
		Description: "Test tool with UI hints",
		Handler: func(ctx context.Context, params map[string]any) (any, error) {
			return nil, nil
		},
	}

	hints := UIToolHints{
		PreferredPartType: PartTypeTable,
		StreamingEnabled:  true,
	}

	// Register with UI hints
	if err := registry.RegisterToolWithUIHints(tool, hints); err != nil {
		t.Fatalf("RegisterToolWithUIHints failed: %v", err)
	}

	// Check IsUITool
	if !registry.IsUITool("ui_test_tool", "1.0.0") {
		t.Error("Expected tool to be recognized as UI tool")
	}

	// Get UI hints
	retrievedHints, ok := registry.GetUIHints("ui_test_tool", "1.0.0")
	if !ok {
		t.Fatal("Expected to get UI hints")
	}

	if retrievedHints.PreferredPartType != PartTypeTable {
		t.Errorf("Expected part type table, got %s", retrievedHints.PreferredPartType)
	}

	// List UI tools
	uiTools := registry.ListUITools()
	if len(uiTools) != 1 {
		t.Errorf("Expected 1 UI tool, got %d", len(uiTools))
	}

	// Export schema with UI hints
	schemas := registry.ExportSchemaWithUIHints()
	if len(schemas) != 1 {
		t.Errorf("Expected 1 schema, got %d", len(schemas))
	}

	schema := schemas[0]
	if _, hasHints := schema["ui_hints"]; !hasHints {
		t.Error("Expected schema to include ui_hints")
	}
}
