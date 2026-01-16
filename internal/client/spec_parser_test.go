package client_test

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/xraph/forge/internal/client"
)

func TestSpecParserOpenAPI(t *testing.T) {
	openAPISpec := `
openapi: 3.1.0
info:
  title: Test API
  version: 1.0.0
  description: Test API for spec parser
servers:
  - url: https://api.example.com
    description: Production
paths:
  /users:
    get:
      summary: List all users
      operationId: listUsers
      tags:
        - users
      parameters:
        - name: limit
          in: query
          schema:
            type: integer
            default: 10
      responses:
        '200':
          description: Successful response
          content:
            application/json:
              schema:
                type: array
                items:
                  $ref: '#/components/schemas/User'
    post:
      summary: Create user
      operationId: createUser
      tags:
        - users
      requestBody:
        required: true
        content:
          application/json:
            schema:
              $ref: '#/components/schemas/CreateUserRequest'
      responses:
        '201':
          description: User created
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/User'
      security:
        - bearerAuth: []
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
          format: uuid
        name:
          type: string
        email:
          type: string
          format: email
    CreateUserRequest:
      type: object
      required:
        - name
      properties:
        name:
          type: string
        email:
          type: string
  securitySchemes:
    bearerAuth:
      type: http
      scheme: bearer
      bearerFormat: JWT
`

	tmpDir := t.TempDir()

	specFile := filepath.Join(tmpDir, "openapi.yaml")

	err := os.WriteFile(specFile, []byte(openAPISpec), 0644)
	if err != nil {
		t.Fatalf("Failed to write spec file: %v", err)
	}

	parser := client.NewSpecParser()

	spec, err := parser.ParseFile(context.Background(), specFile)
	if err != nil {
		t.Fatalf("ParseFile failed: %v", err)
	}

	// Test API info
	if spec.Info.Title != "Test API" {
		t.Errorf("Expected title 'Test API', got '%s'", spec.Info.Title)
	}

	if spec.Info.Version != "1.0.0" {
		t.Errorf("Expected version '1.0.0', got '%s'", spec.Info.Version)
	}

	// Test servers
	if len(spec.Servers) != 1 {
		t.Errorf("Expected 1 server, got %d", len(spec.Servers))
	}

	if spec.Servers[0].URL != "https://api.example.com" {
		t.Errorf("Expected server URL 'https://api.example.com', got '%s'", spec.Servers[0].URL)
	}

	// Test endpoints
	if len(spec.Endpoints) != 2 {
		t.Errorf("Expected 2 endpoints, got %d", len(spec.Endpoints))
	}

	// Test GET endpoint
	var getEndpoint *client.Endpoint

	for i := range spec.Endpoints {
		if spec.Endpoints[i].OperationID == "listUsers" {
			getEndpoint = &spec.Endpoints[i]

			break
		}
	}

	if getEndpoint == nil {
		t.Fatal("listUsers endpoint not found")
	}

	if getEndpoint.Method != http.MethodGet {
		t.Errorf("Expected method GET, got %s", getEndpoint.Method)
	}

	if getEndpoint.Path != "/users" {
		t.Errorf("Expected path '/users', got '%s'", getEndpoint.Path)
	}

	if len(getEndpoint.Tags) != 1 || getEndpoint.Tags[0] != "users" {
		t.Errorf("Expected tag 'users', got %v", getEndpoint.Tags)
	}

	// Test query parameter
	if len(getEndpoint.QueryParams) != 1 {
		t.Errorf("Expected 1 query parameter, got %d", len(getEndpoint.QueryParams))
	} else {
		if getEndpoint.QueryParams[0].Name != "limit" {
			t.Errorf("Expected parameter name 'limit', got '%s'", getEndpoint.QueryParams[0].Name)
		}

		if getEndpoint.QueryParams[0].Schema.Type != "integer" {
			t.Errorf("Expected parameter type 'integer', got '%s'", getEndpoint.QueryParams[0].Schema.Type)
		}
	}

	// Test POST endpoint with auth
	var postEndpoint *client.Endpoint

	for i := range spec.Endpoints {
		if spec.Endpoints[i].OperationID == "createUser" {
			postEndpoint = &spec.Endpoints[i]

			break
		}
	}

	if postEndpoint == nil {
		t.Fatal("createUser endpoint not found")
	}

	if postEndpoint.Method != http.MethodPost {
		t.Errorf("Expected method POST, got %s", postEndpoint.Method)
	}

	if postEndpoint.RequestBody == nil {
		t.Error("Expected request body, got nil")
	}

	if len(postEndpoint.Security) == 0 {
		t.Error("Expected security requirements, got none")
	}

	// Test security schemes
	if len(spec.Security) != 1 {
		t.Errorf("Expected 1 security scheme, got %d", len(spec.Security))
	}

	if spec.Security[0].Type != "http" {
		t.Errorf("Expected auth type 'http', got '%s'", spec.Security[0].Type)
	}

	if spec.Security[0].Scheme != "bearer" {
		t.Errorf("Expected scheme 'bearer', got '%s'", spec.Security[0].Scheme)
	}

	// Test schemas
	if len(spec.Schemas) != 2 {
		t.Errorf("Expected 2 schemas, got %d", len(spec.Schemas))
	}

	userSchema, ok := spec.Schemas["User"]
	if !ok {
		t.Error("User schema not found")
	} else {
		if userSchema.Type != "object" {
			t.Errorf("Expected User schema type 'object', got '%s'", userSchema.Type)
		}

		if len(userSchema.Required) != 2 {
			t.Errorf("Expected 2 required fields, got %d", len(userSchema.Required))
		}

		if len(userSchema.Properties) != 3 {
			t.Errorf("Expected 3 properties, got %d", len(userSchema.Properties))
		}
	}
}

func TestSpecParserAsyncAPI(t *testing.T) {
	asyncAPISpec := `
asyncapi: 3.0.0
info:
  title: Chat API
  version: 1.0.0
  description: WebSocket chat API
servers:
  production:
    host: ws.example.com:443
    protocol: wss
    description: Production WebSocket server
channels:
  chatMessages:
    address: /chat/{roomId}
    messages:
      sendMessage:
        name: sendMessage
        payload:
          type: object
          required:
            - text
          properties:
            text:
              type: string
            replyTo:
              type: string
      receiveMessage:
        name: receiveMessage
        payload:
          type: object
          properties:
            text:
              type: string
            sender:
              type: string
            timestamp:
              type: string
              format: date-time
  notifications:
    address: /notifications
    messages:
      userJoined:
        payload:
          type: object
          properties:
            userId:
              type: string
            username:
              type: string
      userLeft:
        payload:
          type: object
          properties:
            userId:
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
  receiveNotifications:
    action: receive
    channel:
      $ref: '#/channels/notifications'
`

	tmpDir := t.TempDir()

	specFile := filepath.Join(tmpDir, "asyncapi.yaml")

	err := os.WriteFile(specFile, []byte(asyncAPISpec), 0644)
	if err != nil {
		t.Fatalf("Failed to write spec file: %v", err)
	}

	parser := client.NewSpecParser()

	spec, err := parser.ParseFile(context.Background(), specFile)
	if err != nil {
		t.Fatalf("ParseFile failed: %v", err)
	}

	// Test API info
	if spec.Info.Title != "Chat API" {
		t.Errorf("Expected title 'Chat API', got '%s'", spec.Info.Title)
	}

	// Test servers
	if len(spec.Servers) != 1 {
		t.Errorf("Expected 1 server, got %d", len(spec.Servers))
	}

	// Test WebSocket endpoints
	if len(spec.WebSockets) == 0 {
		t.Fatal("Expected WebSocket endpoints, got none")
	}

	// Find chat endpoint
	var chatWS *client.WebSocketEndpoint

	for i := range spec.WebSockets {
		if spec.WebSockets[i].Path == "/chat/{roomId}" {
			chatWS = &spec.WebSockets[i]

			break
		}
	}

	if chatWS == nil {
		t.Fatal("Chat WebSocket endpoint not found")
	}

	if chatWS.SendSchema == nil {
		t.Error("Expected send schema, got nil")
	}

	if chatWS.ReceiveSchema == nil {
		t.Error("Expected receive schema, got nil")
	}

	// Test SSE endpoint
	if len(spec.SSEs) != 0 {
		t.Errorf("Expected 0 SSE endpoints, got %d", len(spec.SSEs))
	}

	// Test notifications WebSocket endpoint
	var notifWS *client.WebSocketEndpoint

	for i := range spec.WebSockets {
		if spec.WebSockets[i].Path == "/notifications" {
			notifWS = &spec.WebSockets[i]

			break
		}
	}

	if notifWS == nil {
		t.Fatal("Notifications WebSocket endpoint not found")
	}

	// Check that notifications has receive schema
	if notifWS.ReceiveSchema == nil {
		t.Error("Expected receive schema for notifications, got nil")
	}
}

func TestSpecParserInvalidFile(t *testing.T) {
	tmpDir := t.TempDir()

	tests := []struct {
		name    string
		content string
		wantErr bool
	}{
		{
			name:    "Invalid YAML",
			content: "invalid: [yaml content",
			wantErr: true,
		},
		{
			name:    "Empty file",
			content: "",
			wantErr: true,
		},
		{
			name: "Missing_openapi_asyncapi_version",
			content: `
info:
  title: Test
  version: 1.0.0
`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			specFile := filepath.Join(tmpDir, "test-"+tt.name+".yaml")

			err := os.WriteFile(specFile, []byte(tt.content), 0644)
			if err != nil {
				t.Fatalf("Failed to write spec file: %v", err)
			}

			parser := client.NewSpecParser()

			_, err = parser.ParseFile(context.Background(), specFile)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseFile() error = %v, wantErr = %v", err, tt.wantErr)
			}
		})
	}
}

func TestSpecParserJSONFormat(t *testing.T) {
	openAPIJSON := `{
  "openapi": "3.1.0",
  "info": {
    "title": "JSON Test API",
    "version": "1.0.0"
  },
  "paths": {
    "/test": {
      "get": {
        "summary": "Test endpoint",
        "responses": {
          "200": {
            "description": "Success"
          }
        }
      }
    }
  }
}`

	tmpDir := t.TempDir()

	specFile := filepath.Join(tmpDir, "openapi.json")

	err := os.WriteFile(specFile, []byte(openAPIJSON), 0644)
	if err != nil {
		t.Fatalf("Failed to write spec file: %v", err)
	}

	parser := client.NewSpecParser()

	spec, err := parser.ParseFile(context.Background(), specFile)
	if err != nil {
		t.Fatalf("ParseFile failed: %v", err)
	}

	if spec.Info.Title != "JSON Test API" {
		t.Errorf("Expected title 'JSON Test API', got '%s'", spec.Info.Title)
	}

	if len(spec.Endpoints) != 1 {
		t.Errorf("Expected 1 endpoint, got %d", len(spec.Endpoints))
	}
}
