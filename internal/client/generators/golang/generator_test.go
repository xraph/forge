package golang_test

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/xraph/forge/internal/client"
	"github.com/xraph/forge/internal/client/generators/golang"
)

func TestGoGenerator(t *testing.T) {
	gen := golang.NewGenerator()

	if gen.Name() != "go" {
		t.Errorf("Expected name 'go', got '%s'", gen.Name())
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
		"typed-errors",
	}

	for _, expected := range expectedFeatures {
		found := slices.Contains(features, expected)

		if !found {
			t.Errorf("Expected feature '%s' not found", expected)
		}
	}
}

func TestGoGeneratorRESTEndpoints(t *testing.T) {
	spec := &client.APISpec{
		Info: client.APIInfo{
			Title:       "Test API",
			Version:     "1.0.0",
			Description: "Test",
		},
		Servers: []client.Server{
			{URL: "https://api.example.com"},
		},
		Endpoints: []client.Endpoint{
			{
				ID:          "listUsers",
				OperationID: "listUsers",
				Method:      "GET",
				Path:        "/users",
				Summary:     "List users",
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
				Security: []client.SecurityRequirement{
					{SchemeName: "bearerAuth"},
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
		Security: []client.SecurityScheme{
			{
				Key:    "bearerAuth",
				Type:   "http",
				Scheme: "bearer",
			},
		},
	}

	config := client.GeneratorConfig{
		Language:    "go",
		OutputDir:   "./testclient",
		PackageName: "testclient",
		Module:      "github.com/example/testclient",
		APIName:     "Client",
		BaseURL:     "https://api.example.com",
		Version:     "1.0.0",
		IncludeAuth: true,
		Features: client.Features{
			TypedErrors: true,
		},
	}

	gen := golang.NewGenerator()

	result, err := gen.Generate(context.Background(), spec, config)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	// Check expected files
	expectedFiles := []string{"client.go", "types.go", "rest.go", "errors.go"}
	for _, file := range expectedFiles {
		if _, ok := result.Files[file]; !ok {
			t.Errorf("Expected file '%s' not found", file)
		}
	}

	// Check client.go contains auth config
	clientCode := result.Files["client.go"]
	if !strings.Contains(clientCode, "AuthConfig") {
		t.Error("client.go should contain AuthConfig")
	}

	// The scheme is keyed "bearerAuth", so the emitted field is named after
	// it (BearerAuth) rather than the old fixed "BearerToken" name every
	// bearer scheme used to share regardless of its key.
	if !strings.Contains(clientCode, "BearerAuth string") {
		t.Error("client.go should contain BearerAuth field")
	}

	// Check types.go contains generated structs
	typesCode := result.Files["types.go"]
	if !strings.Contains(typesCode, "type User struct") {
		t.Error("types.go should contain User struct")
	}

	if !strings.Contains(typesCode, "type CreateUserRequest struct") {
		t.Error("types.go should contain CreateUserRequest struct")
	}

	// Check rest.go contains endpoint methods
	restCode := result.Files["rest.go"]
	if !strings.Contains(restCode, "ListUsers") {
		t.Error("rest.go should contain ListUsers method")
	}

	if !strings.Contains(restCode, "CreateUser") {
		t.Error("rest.go should contain CreateUser method")
	}

	if !strings.Contains(restCode, "context.Context") {
		t.Error("rest.go should use context.Context")
	}

	// Check errors.go
	errorsCode := result.Files["errors.go"]
	if !strings.Contains(errorsCode, "APIError") {
		t.Error("errors.go should contain APIError type")
	}
}

func TestGoGeneratorWebSocket(t *testing.T) {
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
		Language:         "go",
		OutputDir:        "./chatclient",
		PackageName:      "chatclient",
		Module:           "github.com/example/chatclient",
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

	gen := golang.NewGenerator()

	result, err := gen.Generate(context.Background(), spec, config)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	// Check WebSocket file
	wsCode, ok := result.Files["websocket.go"]
	if !ok {
		t.Fatal("websocket.go not found")
	}

	// Check for expected content
	expectedStrings := []string{
		"WebSocket",
		"Connect",
		"Send",
		"OnMessage",
		"Close",
		"ConnectionState",
	}

	for _, expected := range expectedStrings {
		if !strings.Contains(wsCode, expected) {
			t.Errorf("websocket.go should contain '%s'", expected)
		}
	}

	// Check for reconnection logic
	if config.Features.Reconnection {
		if !strings.Contains(wsCode, "reconnect") {
			t.Error("websocket.go should contain reconnection logic")
		}
	}

	// Check for heartbeat
	if config.Features.Heartbeat {
		if !strings.Contains(wsCode, "heartbeat") || !strings.Contains(wsCode, "PingMessage") {
			t.Error("websocket.go should contain heartbeat/ping logic")
		}
	}
}

func TestGoGeneratorSSE(t *testing.T) {
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
		Language:         "go",
		OutputDir:        "./notifclient",
		PackageName:      "notifclient",
		Module:           "github.com/example/notifclient",
		APIName:          "NotificationClient",
		BaseURL:          "https://api.example.com",
		Version:          "1.0.0",
		IncludeStreaming: true,
		Features: client.Features{
			Reconnection:    true,
			StateManagement: true,
		},
	}

	gen := golang.NewGenerator()

	result, err := gen.Generate(context.Background(), spec, config)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	// Check SSE file
	sseCode, ok := result.Files["sse.go"]
	if !ok {
		t.Fatal("sse.go not found")
	}

	// Check for expected content
	expectedStrings := []string{
		"SSE",
		"Connect",
		"OnAlert",
		"OnUpdate",
		"Close",
	}

	for _, expected := range expectedStrings {
		if !strings.Contains(sseCode, expected) {
			t.Errorf("sse.go should contain '%s'", expected)
		}
	}
}

func TestGoGeneratorValidation(t *testing.T) {
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

	gen := golang.NewGenerator()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := gen.Validate(tt.spec)
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr = %v", err, tt.wantErr)
			}
		})
	}
}

func TestGoGeneratorWebTransport(t *testing.T) {
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
		Language:         "go",
		OutputDir:        "./wtclient",
		PackageName:      "wtclient",
		Module:           "github.com/example/wtclient",
		APIName:          "WTClient",
		BaseURL:          "https://api.example.com",
		Version:          "1.0.0",
		IncludeStreaming: true,
		Features: client.Features{
			Reconnection:    true,
			StateManagement: true,
		},
	}

	gen := golang.NewGenerator()

	result, err := gen.Generate(context.Background(), spec, config)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	// Check WebTransport file
	wtCode, ok := result.Files["webtransport.go"]
	if !ok {
		t.Fatal("webtransport.go not found")
	}

	// Check for expected content
	expectedStrings := []string{
		"WebTransport",
		"Connect",
		"OpenBidiStream",
		"SendDatagram",
		"ReceiveDatagram",
		"Close",
		"WebTransportState",
	}

	for _, expected := range expectedStrings {
		if !strings.Contains(wtCode, expected) {
			t.Errorf("webtransport.go should contain '%s'", expected)
		}
	}

	// Check for reconnection logic
	if config.Features.Reconnection {
		if !strings.Contains(wtCode, "Reconnect") {
			t.Error("webtransport.go should contain reconnection logic")
		}
	}

	// Check for state management
	if config.Features.StateManagement {
		if !strings.Contains(wtCode, "OnStateChange") {
			t.Error("webtransport.go should contain state management")
		}
	}

	// Check dependencies include webtransport-go
	foundWT := false

	for _, dep := range result.Dependencies {
		if strings.Contains(dep.Name, "webtransport-go") {
			foundWT = true

			break
		}
	}

	if !foundWT {
		t.Error("Dependencies should include 'webtransport-go' package")
	}
}

// Warnings raised while the specification was being built -- a merge that
// dropped a duplicate route, an entity whose id field no schema declares --
// have to survive into the generated result, because the CLI prints only
// what the generator hands back. Go is the DEFAULT language, so a Go
// generator that drops them makes every one of those warnings invisible
// unless the user happens to pass --language typescript.
func TestGoGeneratorCarriesSpecWarnings(t *testing.T) {
	spec := &client.APISpec{
		Info:      client.APIInfo{Title: "Test API", Version: "1.0.0"},
		Endpoints: []client.Endpoint{{ID: "listOrders", Method: "GET", Path: "/orders"}},
		Warnings: []string{
			`route "GET /orders" is declared in more than one source; the first declaration wins`,
		},
	}

	config := client.GeneratorConfig{PackageName: "testclient", Version: "1.0.0"}

	result, err := golang.NewGenerator().Generate(context.Background(), spec, config)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	if !slices.Contains(result.Warnings, spec.Warnings[0]) {
		t.Errorf("generated client Warnings = %v, want it to carry the spec's %q",
			result.Warnings, spec.Warnings[0])
	}
}

// ...and carrying them must not mean sharing the spec's backing array: a
// generator that later appends its own warning would otherwise write into
// the specification it was handed.
func TestGoGeneratorDoesNotAliasTheSpecWarningSlice(t *testing.T) {
	spec := &client.APISpec{
		Info:      client.APIInfo{Title: "Test API", Version: "1.0.0"},
		Endpoints: []client.Endpoint{{ID: "listOrders", Method: "GET", Path: "/orders"}},
		Warnings:  []string{"first"},
	}

	config := client.GeneratorConfig{PackageName: "testclient", Version: "1.0.0"}

	result, err := golang.NewGenerator().Generate(context.Background(), spec, config)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	result.Warnings[0] = "mutated"

	if spec.Warnings[0] != "first" {
		t.Errorf("spec.Warnings[0] = %q after mutating the generated result; the slice must be copied",
			spec.Warnings[0])
	}
}

// specWithCookieAuth returns a spec carrying one cookie-located security
// scheme, required by REST, WebSocket, SSE, and WebTransport endpoints
// alike. Cookie, specifically: a browser cannot set headers on a WebSocket
// handshake, so cookies are frequently the only option there, which made
// WebSocket's old bearer-only check the weakest spot of all the transports.
func specWithCookieAuth(t *testing.T) *client.APISpec {
	t.Helper()

	return &client.APISpec{
		Info:    client.APIInfo{Title: "Session API", Version: "1.0.0"},
		Servers: []client.Server{{URL: "https://api.example.com"}},
		Security: []client.SecurityScheme{
			{Key: "sessionAuth", Type: "apiKey", In: "cookie", ParamName: "session_id"},
		},
		Endpoints: []client.Endpoint{
			{
				ID:       "listOrders",
				Method:   "GET",
				Path:     "/orders",
				Security: []client.SecurityRequirement{{SchemeName: "sessionAuth"}},
			},
		},
		WebSockets: []client.WebSocketEndpoint{
			{
				ID:       "chat",
				Path:     "/chat",
				Security: []client.SecurityRequirement{{SchemeName: "sessionAuth"}},
			},
		},
		SSEs: []client.SSEEndpoint{
			{
				ID:       "notifications",
				Path:     "/notifications",
				Security: []client.SecurityRequirement{{SchemeName: "sessionAuth"}},
			},
		},
		WebTransports: []client.WebTransportEndpoint{
			{ID: "data", Path: "/wt/data"},
		},
	}
}

// authConfigForTest turns on auth and streaming, the minimum needed for
// client.go, websocket.go, sse.go, and webtransport.go to all be generated
// from specWithCookieAuth.
func authConfigForTest() client.GeneratorConfig {
	return client.GeneratorConfig{
		Language:         "go",
		PackageName:      "sessionclient",
		APIName:          "SessionClient",
		BaseURL:          "https://api.example.com",
		Version:          "1.0.0",
		IncludeAuth:      true,
		IncludeStreaming: true,
	}
}

// valuesOf flattens a GeneratedClient.Files map for tests that only care
// whether some emitted file contains a substring, not which one.
func valuesOf(files map[string]string) []string {
	values := make([]string, 0, len(files))

	for _, v := range files {
		values = append(values, v)
	}

	return values
}

// TestGoGeneratorRoutesEveryTransportThroughApply is the regression test for
// the gap Task 4 left behind: WebSocket carried its own bearer-only check,
// hand-rolled separately from AuthConfig.apply, so a cookie- or query-located
// scheme silently never reached the handshake even though REST already
// carried it correctly.
func TestGoGeneratorRoutesEveryTransportThroughApply(t *testing.T) {
	spec := specWithCookieAuth(t)

	result, err := golang.NewGenerator().Generate(context.Background(), spec, authConfigForTest())
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	all := strings.Join(valuesOf(result.Files), "\n")

	// A browser cannot set headers on a WebSocket handshake, so cookies are
	// frequently the only option there. Bearer-only was the weakest spot.
	if strings.Count(all, ".apply(") < 2 {
		t.Errorf("transports do not route through apply\n%s", all)
	}

	if strings.Contains(all, `header.Set("Authorization", "Bearer "+ws.client.auth.BearerToken)`) {
		t.Error("websocket still carries its own bearer-only copy")
	}
}

// TestGoGeneratorEmitsSessionOptions checks the opt-in jar support that lets
// a generated client hold and replay a session cookie, since the endpoint
// that sets one is frequently absent from securitySchemes and so would
// otherwise never get an option to use.
func TestGoGeneratorEmitsSessionOptions(t *testing.T) {
	result, err := golang.NewGenerator().Generate(context.Background(), specWithCookieAuth(t), authConfigForTest())
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	all := strings.Join(valuesOf(result.Files), "\n")

	for _, want := range []string{
		"func WithCookieJar(jar http.CookieJar) ClientOption",
		"func WithSessionJar() ClientOption",
		"cookiejar.New(nil)",
	} {
		if !strings.Contains(all, want) {
			t.Errorf("missing %q", want)
		}
	}
}
