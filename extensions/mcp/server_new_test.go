package mcp_test

import (
	"context"
	"testing"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/mcp"
)

func TestExecuteTool(t *testing.T) {
	logger := forge.NewNoopLogger()
	metrics := forge.NewNoOpMetrics()
	config := mcp.DefaultConfig()

	server := mcp.NewServer(config, logger, metrics)

	// Create a fake route
	route := forge.RouteInfo{
		Method:  "GET",
		Path:    "/test/:id",
		Name:    "test-route",
		Summary: "Test route",
	}

	// Generate tool from route
	tool, err := server.GenerateToolFromRoute(route)
	if err != nil {
		t.Fatalf("GenerateToolFromRoute() error = %v", err)
	}

	// Register the tool
	err = server.RegisterTool(tool)
	if err != nil {
		t.Fatalf("RegisterTool() error = %v", err)
	}

	// Execute the tool
	result, err := server.ExecuteTool(context.Background(), tool, map[string]interface{}{
		"id": "123",
		"query": map[string]interface{}{
			"filter": "active",
		},
	})
	if err != nil {
		t.Errorf("ExecuteTool() error = %v", err)
	}

	if result == "" {
		t.Error("ExecuteTool() returned empty result")
	}

	// Should contain the executed path
	expectedPath := "/test/123"
	if len(result) < len(expectedPath) {
		t.Errorf("ExecuteTool() result too short, got %s", result)
	}
}

func TestGenerateInputSchema(t *testing.T) {
	logger := forge.NewNoopLogger()
	metrics := forge.NewNoOpMetrics()
	config := mcp.DefaultConfig()

	server := mcp.NewServer(config, logger, metrics)

	tests := []struct {
		name            string
		route           forge.RouteInfo
		expectedParams  []string
		expectBodyField bool
	}{
		{
			name: "GET with path param",
			route: forge.RouteInfo{
				Method:  "GET",
				Path:    "/users/:id",
				Summary: "Get user",
			},
			expectedParams:  []string{"id"},
			expectBodyField: false,
		},
		{
			name: "POST with body",
			route: forge.RouteInfo{
				Method:  "POST",
				Path:    "/users",
				Summary: "Create user",
			},
			expectedParams:  []string{},
			expectBodyField: true,
		},
		{
			name: "PUT with path param and body",
			route: forge.RouteInfo{
				Method:  "PUT",
				Path:    "/users/:id",
				Summary: "Update user",
			},
			expectedParams:  []string{"id"},
			expectBodyField: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tool, err := server.GenerateToolFromRoute(tt.route)
			if err != nil {
				t.Fatalf("GenerateToolFromRoute() error = %v", err)
			}

			if tool.InputSchema == nil {
				t.Fatal("InputSchema is nil")
			}

			// Check path parameters
			for _, param := range tt.expectedParams {
				if _, ok := tool.InputSchema.Properties[param]; !ok {
					t.Errorf("Expected parameter %s not found in schema", param)
				}

				// Check if required
				found := false

				for _, req := range tool.InputSchema.Required {
					if req == param {
						found = true

						break
					}
				}

				if !found {
					t.Errorf("Parameter %s should be required", param)
				}
			}

			// Check body field for POST/PUT/PATCH
			if tt.expectBodyField {
				if _, ok := tool.InputSchema.Properties["body"]; !ok {
					t.Error("Expected 'body' field in schema for POST/PUT/PATCH")
				}
			}

			// All schemas should have query field
			if _, ok := tool.InputSchema.Properties["query"]; !ok {
				t.Error("Expected 'query' field in schema")
			}
		})
	}
}

func TestResourceReader(t *testing.T) {
	logger := forge.NewNoopLogger()
	metrics := forge.NewNoOpMetrics()
	config := mcp.Config{
		Enabled:         true,
		EnableResources: true,
	}

	server := mcp.NewServer(config, logger, metrics)

	// Register a resource
	resource := &mcp.Resource{
		URI:         "test://resource",
		Name:        "Test Resource",
		Description: "A test resource",
	}

	err := server.RegisterResource(resource)
	if err != nil {
		t.Fatalf("RegisterResource() error = %v", err)
	}

	// Test default reader (should work without custom reader)
	content, err := server.ReadResource(context.Background(), resource)
	if err != nil {
		t.Errorf("ReadResource() error = %v", err)
	}

	if content.Type != "text" {
		t.Errorf("Expected content type 'text', got %s", content.Type)
	}

	// Register a custom reader
	customContent := "Custom resource content"

	err = server.RegisterResourceReader("test://resource", func(ctx context.Context, r *mcp.Resource) (mcp.Content, error) {
		return mcp.Content{
			Type: "text",
			Text: customContent,
		}, nil
	})
	if err != nil {
		t.Fatalf("RegisterResourceReader() error = %v", err)
	}

	// Test custom reader
	content, err = server.ReadResource(context.Background(), resource)
	if err != nil {
		t.Errorf("ReadResource() with custom reader error = %v", err)
	}

	if content.Text != customContent {
		t.Errorf("Expected custom content %s, got %s", customContent, content.Text)
	}
}

func TestPromptGenerator(t *testing.T) {
	logger := forge.NewNoopLogger()
	metrics := forge.NewNoOpMetrics()
	config := mcp.Config{
		Enabled:       true,
		EnablePrompts: true,
	}

	server := mcp.NewServer(config, logger, metrics)

	// Register a prompt
	prompt := &mcp.Prompt{
		Name:        "test-prompt",
		Description: "A test prompt",
		Arguments: []mcp.PromptArgument{
			{Name: "input", Description: "Input text", Required: true},
		},
	}

	err := server.RegisterPrompt(prompt)
	if err != nil {
		t.Fatalf("RegisterPrompt() error = %v", err)
	}

	// Test default generator
	messages, err := server.GeneratePrompt(context.Background(), prompt, map[string]interface{}{
		"input": "test",
	})
	if err != nil {
		t.Errorf("GeneratePrompt() error = %v", err)
	}

	if len(messages) == 0 {
		t.Error("Expected at least one message from default generator")
	}

	// Register a custom generator
	customMessage := "Custom prompt message"

	err = server.RegisterPromptGenerator("test-prompt", func(ctx context.Context, p *mcp.Prompt, args map[string]interface{}) ([]mcp.PromptMessage, error) {
		return []mcp.PromptMessage{
			{
				Role: "assistant",
				Content: []mcp.Content{
					{Type: "text", Text: customMessage},
				},
			},
		}, nil
	})
	if err != nil {
		t.Fatalf("RegisterPromptGenerator() error = %v", err)
	}

	// Test custom generator
	messages, err = server.GeneratePrompt(context.Background(), prompt, map[string]interface{}{
		"input": "test",
	})
	if err != nil {
		t.Errorf("GeneratePrompt() with custom generator error = %v", err)
	}

	if len(messages) == 0 {
		t.Fatal("Expected at least one message from custom generator")
	}

	if messages[0].Content[0].Text != customMessage {
		t.Errorf("Expected custom message %s, got %s", customMessage, messages[0].Content[0].Text)
	}
}

func TestToolNaming(t *testing.T) {
	logger := forge.NewNoopLogger()
	metrics := forge.NewNoOpMetrics()
	config := mcp.Config{
		Enabled:    true,
		ToolPrefix: "prefix_",
	}

	server := mcp.NewServer(config, logger, metrics)

	route := forge.RouteInfo{
		Method:  "GET",
		Path:    "/api/v1/users/:id",
		Summary: "Get user by ID",
	}

	tool, err := server.GenerateToolFromRoute(route)
	if err != nil {
		t.Fatalf("GenerateToolFromRoute() error = %v", err)
	}

	expectedName := "prefix_get_api_v1_users_id"
	if tool.Name != expectedName {
		t.Errorf("Expected tool name %s, got %s", expectedName, tool.Name)
	}

	if tool.Description != route.Summary {
		t.Errorf("Expected description %s, got %s", route.Summary, tool.Description)
	}
}
