package typescript_test

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/xraph/forge/internal/client"
	"github.com/xraph/forge/internal/client/generators/typescript"
)

func TestTypeScriptGenerator(t *testing.T) {
	gen := typescript.NewGenerator()

	if gen.Name() != "typescript" {
		t.Errorf("Expected name 'typescript', got '%s'", gen.Name())
	}

	features := gen.SupportedFeatures()
	expectedFeatures := []string{
		"rest",
		"websocket",
		"sse",
		"auth",
		"reconnection",
		"heartbeat",
		"state-management",
	}

	for _, expected := range expectedFeatures {
		found := slices.Contains(features, expected)

		if !found {
			t.Errorf("Expected feature '%s' not found", expected)
		}
	}
}

func TestTypeScriptGeneratorRESTEndpoints(t *testing.T) {
	spec := &client.APISpec{
		Info: client.APIInfo{
			Title:       "Test API",
			Version:     "1.0.0",
			Description: "Test",
		},
		Servers: []client.Server{
			{URL: "https://api.example.com"},
		},
		Security: []client.SecurityScheme{
			{
				Type:         "http",
				Scheme:       "bearer",
				BearerFormat: "JWT",
			},
		},
		Endpoints: []client.Endpoint{
			{
				ID:          "listUsers",
				OperationID: "listUsers",
				Method:      "GET",
				Path:        "/users",
				Summary:     "List users",
				QueryParams: []client.Parameter{
					{
						Name:     "limit",
						Required: false,
						Schema:   &client.Schema{Type: "integer"},
					},
				},
				Responses: map[int]*client.Response{
					200: {
						Description: "Success",
						Content: map[string]*client.MediaType{
							"application/json": {
								Schema: &client.Schema{
									Type: "array",
									Items: &client.Schema{
										Ref: "#/components/schemas/User",
									},
								},
							},
						},
					},
				},
			},
			{
				ID:          "createUser",
				OperationID: "createUser",
				Method:      "POST",
				Path:        "/users",
				Summary:     "Create user",
				RequestBody: &client.RequestBody{
					Required: true,
					Content: map[string]*client.MediaType{
						"application/json": {
							Schema: &client.Schema{
								Ref: "#/components/schemas/CreateUserRequest",
							},
						},
					},
				},
				Responses: map[int]*client.Response{
					201: {
						Description: "Created",
						Content: map[string]*client.MediaType{
							"application/json": {
								Schema: &client.Schema{
									Ref: "#/components/schemas/User",
								},
							},
						},
					},
				},
			},
		},
		Schemas: map[string]*client.Schema{
			"User": {
				Type: "object",
				Properties: map[string]*client.Schema{
					"id":    {Type: "string"},
					"name":  {Type: "string"},
					"email": {Type: "string"},
				},
				Required: []string{"id", "name"},
			},
			"CreateUserRequest": {
				Type: "object",
				Properties: map[string]*client.Schema{
					"name":  {Type: "string"},
					"email": {Type: "string"},
				},
				Required: []string{"name"},
			},
		},
	}

	config := client.GeneratorConfig{
		Language:    "typescript",
		OutputDir:   "./testclient",
		PackageName: "@example/testclient",
		APIName:     "APIClient",
		BaseURL:     "https://api.example.com",
		Version:     "1.0.0",
		IncludeAuth: true,
	}

	gen := typescript.NewGenerator()

	result, err := gen.Generate(context.Background(), spec, config)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	// Check expected files
	expectedFiles := []string{
		"package.json",
		"tsconfig.json",
		"src/types.ts",
		"src/client.ts",
		"src/rest.ts",
		"src/index.ts",
	}
	for _, file := range expectedFiles {
		if _, ok := result.Files[file]; !ok {
			t.Errorf("Expected file '%s' not found", file)
		}
	}

	// Validate package.json
	packageJSON := result.Files["package.json"]

	var pkg map[string]any
	if err := json.Unmarshal([]byte(packageJSON), &pkg); err != nil {
		t.Errorf("Invalid package.json: %v", err)
	}

	if pkg["name"] != "@example/testclient" {
		t.Errorf("Expected package name '@example/testclient', got '%v'", pkg["name"])
	}

	// Check types.ts contains interfaces
	typesCode := result.Files["src/types.ts"]
	if !strings.Contains(typesCode, "interface User") {
		t.Error("types.ts should contain User interface")
	}

	if !strings.Contains(typesCode, "interface CreateUserRequest") {
		t.Error("types.ts should contain CreateUserRequest interface")
	}

	if !strings.Contains(typesCode, "interface AuthConfig") {
		t.Logf("types.ts content: %s", typesCode)
		t.Error("types.ts should contain AuthConfig interface")
	}

	// Check client.ts
	clientCode := result.Files["src/client.ts"]
	if !strings.Contains(clientCode, "class APIClient") {
		t.Error("client.ts should contain APIClient class")
	}

	if !strings.Contains(clientCode, "HTTPClient") {
		t.Error("client.ts should use HTTPClient from fetch module")
	}

	// Check rest.ts contains methods
	restCode := result.Files["src/rest.ts"]
	if !strings.Contains(restCode, "listUsers") {
		t.Error("rest.ts should contain listUsers method")
	}

	if !strings.Contains(restCode, "createUser") {
		t.Error("rest.ts should contain createUser method")
	}

	if !strings.Contains(restCode, "async") {
		t.Error("rest.ts should use async/await")
	}

	// Check index.ts exports
	indexCode := result.Files["src/index.ts"]
	if !strings.Contains(indexCode, "export") {
		t.Error("index.ts should contain exports")
	}
}

func TestTypeScriptGeneratorWebSocket(t *testing.T) {
	spec := &client.APISpec{
		Info: client.APIInfo{
			Title:   "Chat API",
			Version: "1.0.0",
		},
		WebSockets: []client.WebSocketEndpoint{
			{
				ID:          "chat",
				Path:        "/chat",
				Description: "Chat WebSocket",
				SendSchema: &client.Schema{
					Type: "object",
					Properties: map[string]*client.Schema{
						"text": {Type: "string"},
					},
				},
				ReceiveSchema: &client.Schema{
					Type: "object",
					Properties: map[string]*client.Schema{
						"text":   {Type: "string"},
						"sender": {Type: "string"},
					},
				},
			},
		},
	}

	config := client.GeneratorConfig{
		Language:         "typescript",
		OutputDir:        "./chatclient",
		PackageName:      "@example/chatclient",
		APIName:          "ChatClient",
		BaseURL:          "https://api.example.com",
		Version:          "1.0.0",
		IncludeStreaming: true,
		Features: client.Features{
			Reconnection:    true,
			Heartbeat:       true,
			StateManagement: true,
		},
	}

	gen := typescript.NewGenerator()

	result, err := gen.Generate(context.Background(), spec, config)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	// Check WebSocket file
	wsCode, ok := result.Files["src/websocket.ts"]
	if !ok {
		t.Fatal("websocket.ts not found")
	}

	// Check for expected content
	expectedStrings := []string{
		"WebSocket",
		"class",
		"connect",
		"send",
		"onMessage",
		"close",
		"ConnectionState",
		"EventEmitter",
	}

	for _, expected := range expectedStrings {
		if !strings.Contains(wsCode, expected) {
			t.Errorf("websocket.ts should contain '%s'", expected)
		}
	}

	// Check for reconnection logic
	if config.Features.Reconnection {
		if !strings.Contains(wsCode, "reconnect") {
			t.Error("websocket.ts should contain reconnection logic")
		}
	}

	// Check for heartbeat
	if config.Features.Heartbeat {
		if !strings.Contains(wsCode, "heartbeat") || !strings.Contains(wsCode, "ping") {
			t.Error("websocket.ts should contain heartbeat/ping logic")
		}
	}

	// Check dependencies include ws
	foundWS := false

	for _, dep := range result.Dependencies {
		if strings.Contains(dep.Name, "ws") {
			foundWS = true

			break
		}
	}

	if !foundWS {
		t.Error("Dependencies should include 'ws' package")
	}
}

func TestTypeScriptGeneratorSSE(t *testing.T) {
	spec := &client.APISpec{
		Info: client.APIInfo{
			Title:   "Notification API",
			Version: "1.0.0",
		},
		SSEs: []client.SSEEndpoint{
			{
				ID:          "notifications",
				Path:        "/notifications",
				Description: "Notification stream",
				EventSchemas: map[string]*client.Schema{
					"alert": {
						Type: "object",
						Properties: map[string]*client.Schema{
							"message": {Type: "string"},
							"level":   {Type: "string"},
						},
					},
					"update": {
						Type: "object",
						Properties: map[string]*client.Schema{
							"data": {Type: "string"},
						},
					},
				},
			},
		},
	}

	config := client.GeneratorConfig{
		Language:         "typescript",
		OutputDir:        "./notifclient",
		PackageName:      "@example/notifclient",
		APIName:          "NotificationClient",
		BaseURL:          "https://api.example.com",
		Version:          "1.0.0",
		IncludeStreaming: true,
		Features: client.Features{
			Reconnection:    true,
			StateManagement: true,
		},
	}

	gen := typescript.NewGenerator()

	result, err := gen.Generate(context.Background(), spec, config)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	// Check SSE file
	sseCode, ok := result.Files["src/sse.ts"]
	if !ok {
		t.Fatal("sse.ts not found")
	}

	// Check for expected content
	expectedStrings := []string{
		"EventSource",
		"class",
		"connect",
		"onAlert",
		"onUpdate",
		"close",
	}

	for _, expected := range expectedStrings {
		if !strings.Contains(sseCode, expected) {
			t.Errorf("sse.ts should contain '%s'", expected)
		}
	}

	// Check dependencies include eventsource
	foundES := false

	for _, dep := range result.Dependencies {
		if strings.Contains(dep.Name, "eventsource") {
			foundES = true

			break
		}
	}

	if !foundES {
		t.Error("Dependencies should include 'eventsource' package")
	}
}

func TestTypeScriptGeneratorTypeConversion(t *testing.T) {
	spec := &client.APISpec{
		Info: client.APIInfo{
			Title:   "Test API",
			Version: "1.0.0",
		},
		Schemas: map[string]*client.Schema{
			"TestTypes": {
				Type: "object",
				Properties: map[string]*client.Schema{
					"stringField":  {Type: "string"},
					"numberField":  {Type: "number"},
					"integerField": {Type: "integer"},
					"boolField":    {Type: "boolean"},
					"arrayField": {
						Type: "array",
						Items: &client.Schema{
							Type: "string",
						},
					},
					"enumField": {
						Type: "string",
						Enum: []any{"option1", "option2", "option3"},
					},
				},
			},
		},
	}

	config := client.GeneratorConfig{
		Language:    "typescript",
		OutputDir:   "./testclient",
		PackageName: "@example/testclient",
		APIName:     "TestClient",
		BaseURL:     "https://api.example.com",
		Version:     "1.0.0",
	}

	gen := typescript.NewGenerator()

	result, err := gen.Generate(context.Background(), spec, config)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	typesCode := result.Files["src/types.ts"]

	// Check type conversions - just verify fields exist
	expectedFields := []string{
		"stringField",
		"numberField",
		"integerField",
		"boolField",
		"arrayField",
		"enumField",
	}

	for _, fieldName := range expectedFields {
		if !strings.Contains(typesCode, fieldName) {
			t.Errorf("types.ts should contain field '%s'", fieldName)
		}
	}
}

func TestTypeScriptGeneratorValidation(t *testing.T) {
	tests := []struct {
		name    string
		spec    *client.APISpec
		wantErr bool
	}{
		{
			name: "Valid spec",
			spec: &client.APISpec{
				Info: client.APIInfo{
					Title:   "Test",
					Version: "1.0.0",
				},
				Endpoints: []client.Endpoint{
					{
						ID:     "test",
						Method: "GET",
						Path:   "/test",
					},
				},
			},
			wantErr: false,
		},
		{
			name:    "Nil spec",
			spec:    nil,
			wantErr: true,
		},
		{
			name: "Missing title",
			spec: &client.APISpec{
				Info: client.APIInfo{
					Version: "1.0.0",
				},
			},
			wantErr: true,
		},
	}

	gen := typescript.NewGenerator()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := gen.Validate(tt.spec)
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr = %v", err, tt.wantErr)
			}
		})
	}
}

func TestTypeScriptGeneratorWebTransport(t *testing.T) {
	spec := &client.APISpec{
		Info: client.APIInfo{
			Title:   "WebTransport API",
			Version: "1.0.0",
		},
		WebTransports: []client.WebTransportEndpoint{
			{
				ID:          "data",
				Path:        "/wt/data",
				Description: "Data WebTransport",
				BiStreamSchema: &client.StreamSchema{
					SendSchema: &client.Schema{
						Type: "object",
						Properties: map[string]*client.Schema{
							"action": {Type: "string"},
							"data":   {Type: "string"},
						},
					},
					ReceiveSchema: &client.Schema{
						Type: "object",
						Properties: map[string]*client.Schema{
							"result": {Type: "string"},
							"status": {Type: "string"},
						},
					},
				},
				UniStreamSchema: &client.StreamSchema{
					SendSchema: &client.Schema{
						Type: "object",
						Properties: map[string]*client.Schema{
							"message": {Type: "string"},
						},
					},
				},
				DatagramSchema: &client.Schema{
					Type: "object",
					Properties: map[string]*client.Schema{
						"ping": {Type: "string"},
					},
				},
			},
		},
	}

	config := client.GeneratorConfig{
		Language:         "typescript",
		OutputDir:        "./wtclient",
		PackageName:      "@example/wtclient",
		APIName:          "WTClient",
		BaseURL:          "https://api.example.com",
		Version:          "1.0.0",
		IncludeStreaming: true,
		Features: client.Features{
			Reconnection:    true,
			StateManagement: true,
		},
	}

	gen := typescript.NewGenerator()

	result, err := gen.Generate(context.Background(), spec, config)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	// Check WebTransport file
	wtCode, ok := result.Files["src/webtransport.ts"]
	if !ok {
		t.Fatal("webtransport.ts not found")
	}

	// Check for expected content
	expectedStrings := []string{
		"WebTransport",
		"class",
		"connect",
		"openBidiStream",
		"openUniStream",
		"sendDatagram",
		"receiveDatagram",
		"close",
		"WebTransportState",
		"EventEmitter",
	}

	for _, expected := range expectedStrings {
		if !strings.Contains(wtCode, expected) {
			t.Errorf("webtransport.ts should contain '%s'", expected)
		}
	}

	// Check for reconnection logic
	if config.Features.Reconnection {
		if !strings.Contains(wtCode, "reconnect") {
			t.Error("webtransport.ts should contain reconnection logic")
		}
	}

	// Check for state management
	if config.Features.StateManagement {
		if !strings.Contains(wtCode, "onStateChange") {
			t.Error("webtransport.ts should contain state management")
		}
	}

	// Verify BiDiStream and UniStream classes are generated
	if !strings.Contains(wtCode, "class BiDiStream") {
		t.Error("webtransport.ts should contain BiDiStream class")
	}

	if !strings.Contains(wtCode, "class UniStream") {
		t.Error("webtransport.ts should contain UniStream class")
	}
}
