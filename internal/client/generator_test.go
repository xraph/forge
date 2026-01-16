package client_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/xraph/forge/internal/client"
	"github.com/xraph/forge/internal/client/generators/golang"
	"github.com/xraph/forge/internal/client/generators/typescript"
)

func TestGeneratorRegistry(t *testing.T) {
	gen := client.NewGenerator()

	// Test registering Go generator
	err := gen.Register(golang.NewGenerator())
	if err != nil {
		t.Fatalf("Failed to register Go generator: %v", err)
	}

	// Test registering TypeScript generator
	err = gen.Register(typescript.NewGenerator())
	if err != nil {
		t.Fatalf("Failed to register TypeScript generator: %v", err)
	}

	// Test duplicate registration
	err = gen.Register(golang.NewGenerator())
	if err == nil {
		t.Error("Expected error for duplicate registration, got nil")
	}

	// Test listing generators
	generators := gen.ListGenerators()
	if len(generators) != 2 {
		t.Errorf("Expected 2 generators, got %d", len(generators))
	}

	// Check generator info
	info, err := gen.GetGeneratorInfo("go")
	if err != nil {
		t.Errorf("Failed to get Go generator info: %v", err)
	}

	if info.Name != "go" {
		t.Errorf("Expected generator name 'go', got '%s'", info.Name)
	}
}

func TestGenerateFromFile(t *testing.T) {
	// Create a temporary test spec file
	testSpec := `
openapi: 3.1.0
info:
  title: Test API
  version: 1.0.0
  description: Test API for client generation
servers:
  - url: https://api.example.com
paths:
  /users:
    get:
      summary: List users
      operationId: listUsers
      responses:
        '200':
          description: Success
          content:
            application/json:
              schema:
                type: array
                items:
                  $ref: '#/components/schemas/User'
  /users/{id}:
    get:
      summary: Get user
      operationId: getUser
      parameters:
        - name: id
          in: path
          required: true
          schema:
            type: string
      responses:
        '200':
          description: Success
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/User'
components:
  schemas:
    User:
      type: object
      required:
        - id
        - name
      properties:
        id:
          type: string
        name:
          type: string
        email:
          type: string
  securitySchemes:
    bearerAuth:
      type: http
      scheme: bearer
`

	// Create temp directory
	tmpDir := t.TempDir()

	specFile := filepath.Join(tmpDir, "openapi.yaml")

	err := os.WriteFile(specFile, []byte(testSpec), 0644)
	if err != nil {
		t.Fatalf("Failed to write spec file: %v", err)
	}

	// Create generator with both language generators
	gen := client.NewGenerator()
	gen.Register(golang.NewGenerator())
	gen.Register(typescript.NewGenerator())

	tests := []struct {
		name      string
		language  string
		wantFiles []string
	}{
		{
			name:     "Go client generation",
			language: "go",
			wantFiles: []string{
				"client.go",
				"types.go",
				"rest.go",
				"errors.go",
			},
		},
		{
			name:     "TypeScript client generation",
			language: "typescript",
			wantFiles: []string{
				"package.json",
				"tsconfig.json",
				"src/types.ts",
				"src/client.ts",
				"src/rest.ts",
				"src/index.ts",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			outputDir := filepath.Join(tmpDir, "output", tt.language)

			config := client.GeneratorConfig{
				Language:    tt.language,
				OutputDir:   outputDir,
				PackageName: "testclient",
				APIName:     "TestClient",
				BaseURL:     "https://api.example.com",
				IncludeAuth: true,
				Version:     "1.0.0",
				Features: client.Features{
					TypedErrors: true,
				},
			}

			// Generate client
			generatedClient, err := gen.GenerateFromFile(context.Background(), specFile, config)
			if err != nil {
				t.Fatalf("GenerateFromFile failed: %v", err)
			}

			// Check generated files
			if len(generatedClient.Files) == 0 {
				t.Error("No files generated")
			}

			// Check expected files exist
			for _, wantFile := range tt.wantFiles {
				if _, ok := generatedClient.Files[wantFile]; !ok {
					t.Errorf("Expected file '%s' not found in generated files", wantFile)
				}
			}

			// Check dependencies
			if len(generatedClient.Dependencies) == 0 && tt.language != "typescript" {
				t.Log("Note: No dependencies listed")
			}

			// Write to disk and verify
			outputMgr := client.NewOutputManager()

			err = outputMgr.WriteClient(generatedClient, outputDir)
			if err != nil {
				t.Errorf("Failed to write client: %v", err)
			}

			// Verify files were written
			for _, wantFile := range tt.wantFiles {
				filePath := filepath.Join(outputDir, wantFile)
				if _, err := os.Stat(filePath); os.IsNotExist(err) {
					t.Errorf("Expected file '%s' was not written to disk", wantFile)
				}
			}

			// Verify README was created
			readmePath := filepath.Join(outputDir, "README.md")
			if _, err := os.Stat(readmePath); os.IsNotExist(err) {
				t.Error("README.md was not created")
			}
		})
	}
}

func TestGenerateWithStreaming(t *testing.T) {
	testSpec := `
asyncapi: 3.0.0
info:
  title: Chat API
  version: 1.0.0
servers:
  ws:
    host: ws.example.com
    protocol: wss
channels:
  chatMessages:
    address: /chat
    messages:
      sendMessage:
        payload:
          type: object
          properties:
            text:
              type: string
      receiveMessage:
        payload:
          type: object
          properties:
            text:
              type: string
            sender:
              type: string
operations:
  sendChatMessage:
    action: send
    channel:
      $ref: '#/channels/chatMessages'
  receiveChatMessage:
    action: receive
    channel:
      $ref: '#/channels/chatMessages'
`

	tmpDir := t.TempDir()

	specFile := filepath.Join(tmpDir, "asyncapi.yaml")

	err := os.WriteFile(specFile, []byte(testSpec), 0644)
	if err != nil {
		t.Fatalf("Failed to write spec file: %v", err)
	}

	gen := client.NewGenerator()
	gen.Register(golang.NewGenerator())
	gen.Register(typescript.NewGenerator())

	tests := []struct {
		name       string
		language   string
		wantWSFile string
	}{
		{
			name:       "Go WebSocket generation",
			language:   "go",
			wantWSFile: "websocket.go",
		},
		{
			name:       "TypeScript WebSocket generation",
			language:   "typescript",
			wantWSFile: "src/websocket.ts",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			outputDir := filepath.Join(tmpDir, "output-ws", tt.language)

			config := client.GeneratorConfig{
				Language:         tt.language,
				OutputDir:        outputDir,
				PackageName:      "chatclient",
				APIName:          "ChatClient",
				BaseURL:          "https://api.example.com",
				IncludeStreaming: true,
				Version:          "1.0.0",
				Features: client.Features{
					Reconnection:    true,
					Heartbeat:       true,
					StateManagement: true,
				},
			}

			generatedClient, err := gen.GenerateFromFile(context.Background(), specFile, config)
			if err != nil {
				t.Fatalf("GenerateFromFile failed: %v", err)
			}

			// Check WebSocket file exists
			if _, ok := generatedClient.Files[tt.wantWSFile]; !ok {
				t.Errorf("Expected WebSocket file '%s' not found", tt.wantWSFile)
			}

			// Check WebSocket file contains expected content
			wsContent := generatedClient.Files[tt.wantWSFile]
			if len(wsContent) == 0 {
				t.Error("WebSocket file is empty")
			}

			// Basic content checks
			expectedStrings := []string{"WebSocket", "connect", "send"}
			for _, expected := range expectedStrings {
				if !contains(wsContent, expected) {
					t.Errorf("WebSocket file should contain '%s'", expected)
				}
			}
		})
	}
}

func TestConfigValidation(t *testing.T) {
	tests := []struct {
		name      string
		config    client.GeneratorConfig
		wantError bool
	}{
		{
			name: "Valid Go config",
			config: client.GeneratorConfig{
				Language:    "go",
				OutputDir:   "./client",
				PackageName: "client",
			},
			wantError: false,
		},
		{
			name: "Valid TypeScript config",
			config: client.GeneratorConfig{
				Language:    "typescript",
				OutputDir:   "./client",
				PackageName: "@myorg/client",
			},
			wantError: false,
		},
		{
			name: "Missing language",
			config: client.GeneratorConfig{
				OutputDir:   "./client",
				PackageName: "client",
			},
			wantError: true,
		},
		{
			name: "Missing output dir",
			config: client.GeneratorConfig{
				Language:    "go",
				PackageName: "client",
			},
			wantError: true,
		},
		{
			name: "Invalid language",
			config: client.GeneratorConfig{
				Language:    "rust",
				OutputDir:   "./client",
				PackageName: "client",
			},
			wantError: true,
		},
		{
			name: "Invalid Go package name",
			config: client.GeneratorConfig{
				Language:    "go",
				OutputDir:   "./client",
				PackageName: "Invalid-Package",
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantError {
				t.Errorf("Validate() error = %v, wantError = %v", err, tt.wantError)
			}
		})
	}
}

// Helper function.
func contains(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 && (s == substr || len(s) > len(substr) && containsSubstring(s, substr))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}

	return false
}
