package sdk

import (
	"context"
	"strings"
	"testing"

	"github.com/xraph/forge/extensions/ai/llm"
)

func TestPresentationTools(t *testing.T) {
	tools := PresentationTools()

	// Verify all expected tools are present
	expectedTools := []string{
		ToolRenderTable,
		ToolRenderChart,
		ToolRenderMetrics,
		ToolRenderTimeline,
		ToolRenderKanban,
		ToolRenderButtons,
		ToolRenderForm,
		ToolRenderCard,
		ToolRenderStats,
		ToolRenderGallery,
		ToolRenderAlert,
		ToolRenderProgress,
	}

	if len(tools) != len(expectedTools) {
		t.Errorf("expected %d tools, got %d", len(expectedTools), len(tools))
	}

	toolMap := make(map[string]UITool)
	for _, tool := range tools {
		toolMap[tool.Name()] = tool
	}

	for _, name := range expectedTools {
		if _, exists := toolMap[name]; !exists {
			t.Errorf("expected tool %s not found", name)
		}
	}
}

func TestGetPresentationToolSchemas(t *testing.T) {
	schemas := GetPresentationToolSchemas()

	if len(schemas) == 0 {
		t.Error("expected non-empty schemas")
	}

	// Verify schemas have required fields
	for _, schema := range schemas {
		if schema.Type != "function" {
			t.Errorf("expected type 'function', got '%s'", schema.Type)
		}
		if schema.Function == nil {
			t.Error("expected function definition")
			continue
		}
		if schema.Function.Name == "" {
			t.Error("expected function name")
		}
		if schema.Function.Description == "" {
			t.Errorf("expected description for %s", schema.Function.Name)
		}
		if schema.Function.Parameters == nil {
			t.Errorf("expected parameters for %s", schema.Function.Name)
		}
	}
}

func TestIsPresentationTool(t *testing.T) {
	tests := []struct {
		name     string
		expected bool
	}{
		{ToolRenderTable, true},
		{ToolRenderChart, true},
		{ToolRenderMetrics, true},
		{"unknown_tool", false},
		{"some_other_tool", false},
	}

	for _, tt := range tests {
		result := IsPresentationTool(tt.name)
		if result != tt.expected {
			t.Errorf("IsPresentationTool(%s) = %v, expected %v", tt.name, result, tt.expected)
		}
	}
}

func TestRenderTableTool(t *testing.T) {
	ctx := context.Background()

	params := map[string]any{
		"title": "Test Table",
		"headers": []any{
			map[string]any{"label": "Name", "key": "name"},
			map[string]any{"label": "Value", "key": "value"},
		},
		"rows": []any{
			[]any{"Row 1", "100"},
			[]any{"Row 2", "200"},
		},
		"options": map[string]any{
			"sortable": true,
		},
	}

	var events []llm.ClientStreamEvent
	onEvent := func(event llm.ClientStreamEvent) error {
		events = append(events, event)
		return nil
	}

	result, err := ExecutePresentationTool(ctx, ToolRenderTable, params, onEvent, "test-exec-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.Success {
		t.Errorf("expected success, got failure: %v", result.Error)
	}

	if result.ToolName != ToolRenderTable {
		t.Errorf("expected tool name %s, got %s", ToolRenderTable, result.ToolName)
	}

	// Verify events were emitted
	if len(events) == 0 {
		t.Error("expected events to be emitted")
	}
}

func TestRenderChartTool(t *testing.T) {
	ctx := context.Background()

	params := map[string]any{
		"title":  "Sales Chart",
		"type":   "bar",
		"labels": []any{"Jan", "Feb", "Mar"},
		"datasets": []any{
			map[string]any{
				"label": "Revenue",
				"data":  []any{100.0, 200.0, 300.0},
			},
		},
	}

	result, err := ExecutePresentationTool(ctx, ToolRenderChart, params, nil, "test-exec-2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.Success {
		t.Errorf("expected success, got failure: %v", result.Error)
	}
}

func TestRenderMetricsTool(t *testing.T) {
	ctx := context.Background()

	params := map[string]any{
		"title": "Dashboard",
		"metrics": []any{
			map[string]any{
				"label": "Revenue",
				"value": 50000,
				"unit":  "$",
				"trend": map[string]any{
					"direction":  "up",
					"percentage": 15.5,
				},
			},
			map[string]any{
				"label":  "Users",
				"value":  1250,
				"status": "good",
			},
		},
		"layout": "grid",
	}

	result, err := ExecutePresentationTool(ctx, ToolRenderMetrics, params, nil, "test-exec-3")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.Success {
		t.Errorf("expected success, got failure: %v", result.Error)
	}
}

func TestRenderTimelineTool(t *testing.T) {
	ctx := context.Background()

	params := map[string]any{
		"title": "Project History",
		"events": []any{
			map[string]any{
				"title":       "Project Start",
				"description": "Initial kickoff",
				"timestamp":   "2024-01-01T00:00:00Z",
				"status":      "completed",
			},
			map[string]any{
				"title":       "Phase 1",
				"description": "Development",
				"timestamp":   "2024-03-01T00:00:00Z",
				"status":      "in_progress",
			},
		},
	}

	result, err := ExecutePresentationTool(ctx, ToolRenderTimeline, params, nil, "test-exec-4")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.Success {
		t.Errorf("expected success, got failure: %v", result.Error)
	}
}

func TestRenderButtonsTool(t *testing.T) {
	ctx := context.Background()

	params := map[string]any{
		"title": "Actions",
		"buttons": []any{
			map[string]any{
				"id":      "btn1",
				"label":   "Approve",
				"variant": "primary",
				"action": map[string]any{
					"type":  "callback",
					"value": "approve",
				},
			},
			map[string]any{
				"id":      "btn2",
				"label":   "Reject",
				"variant": "danger",
			},
		},
	}

	result, err := ExecutePresentationTool(ctx, ToolRenderButtons, params, nil, "test-exec-5")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.Success {
		t.Errorf("expected success, got failure: %v", result.Error)
	}
}

func TestRenderFormTool(t *testing.T) {
	ctx := context.Background()

	params := map[string]any{
		"id":          "contact-form",
		"title":       "Contact Us",
		"description": "Fill out the form below",
		"fields": []any{
			map[string]any{
				"name":        "name",
				"label":       "Name",
				"type":        "text",
				"required":    true,
				"placeholder": "Enter your name",
			},
			map[string]any{
				"name":  "email",
				"label": "Email",
				"type":  "email",
			},
			map[string]any{
				"name":  "country",
				"label": "Country",
				"type":  "select",
				"options": []any{
					map[string]any{"value": "us", "label": "United States"},
					map[string]any{"value": "uk", "label": "United Kingdom"},
				},
			},
		},
		"submitLabel": "Send Message",
	}

	result, err := ExecutePresentationTool(ctx, ToolRenderForm, params, nil, "test-exec-6")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.Success {
		t.Errorf("expected success, got failure: %v", result.Error)
	}
}

func TestRenderAlertTool(t *testing.T) {
	ctx := context.Background()

	params := map[string]any{
		"title":       "Warning",
		"message":     "Please review your input before continuing.",
		"type":        "warning",
		"dismissible": true,
	}

	result, err := ExecutePresentationTool(ctx, ToolRenderAlert, params, nil, "test-exec-7")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.Success {
		t.Errorf("expected success, got failure: %v", result.Error)
	}
}

func TestExecutePresentationToolNotFound(t *testing.T) {
	ctx := context.Background()

	_, err := ExecutePresentationTool(ctx, "unknown_tool", nil, nil, "test")
	if err == nil {
		t.Error("expected error for unknown tool")
	}
}

func TestGetPresentationToolDescriptions(t *testing.T) {
	desc := GetPresentationToolDescriptions()

	if desc == "" {
		t.Error("expected non-empty descriptions")
	}

	// Verify it contains expected tool names
	for _, tool := range PresentationTools() {
		if !strings.Contains(desc, tool.Name()) {
			t.Errorf("expected description to contain %s", tool.Name())
		}
	}
}
