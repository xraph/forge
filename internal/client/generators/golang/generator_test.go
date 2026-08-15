package golang_test

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
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

// webtransportGoVersion is the single version of
// github.com/quic-go/webtransport-go this generator targets. It matches the
// version forge's own go.mod pins, so the API the generator emits against is
// the API that is actually checked out and compiled in this repo.
//
// The choice is load-bearing rather than cosmetic: the package renamed
// Dialer to Transport between v0.11.1 and v0.12.0 (verified against client.go
// and transport.go in the local module cache), so code emitted for one will
// not compile against the other. Dial's signature did not change across that
// rename -- it is (ctx, urlStr, reqHdr) returning (*http.Response, *Session,
// error) in both -- so the type name is the whole of the incompatibility.
const webtransportGoVersion = "v0.12.0"

// TestGoGeneratorWebTransportDependencyIsResolvable is the regression test
// for two defects that only show up when you actually build the generated
// module rather than parse it.
//
// The reported dependency and the emitted go.mod were assembled by two
// separate functions that had drifted apart: getDependencies listed
// webtransport-go, generateGoMod's require block did not, so a generated
// client that imports the package had no way to resolve it. go.mod is now
// derived from getDependencies, which is what keeps the two in step.
//
// The version is asserted here too, because getDependencies pinned v0.6.0
// while webtransport.go emitted against a v0.12.0-era API surface. Pinning
// and emission have to name the same release or the generated client fails
// to compile on a symbol that does not exist in the pinned version.
func TestGoGeneratorWebTransportDependencyIsResolvable(t *testing.T) {
	result, err := golang.NewGenerator().Generate(context.Background(), specSchemalessTransports(), func() client.GeneratorConfig {
		config := schemalessStreamingConfig()
		config.Module = "github.com/example/schemalessclient"

		return config
	}())
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	goMod, ok := result.Files["go.mod"]
	if !ok {
		t.Fatal("go.mod not found")
	}

	// The import webtransport.go emits has to be satisfiable by the go.mod
	// shipped alongside it.
	if !strings.Contains(goMod, "github.com/quic-go/webtransport-go "+webtransportGoVersion) {
		t.Errorf("go.mod does not require webtransport-go %s\n%s", webtransportGoVersion, goMod)
	}

	for _, dep := range result.Dependencies {
		if dep.Name != "github.com/quic-go/webtransport-go" {
			continue
		}

		if dep.Version != webtransportGoVersion {
			t.Errorf("reported webtransport-go version is %s, want %s", dep.Version, webtransportGoVersion)
		}

		if !strings.Contains(goMod, dep.Name+" "+dep.Version) {
			t.Errorf("go.mod and reported dependencies disagree on %s\n%s", dep.Name, goMod)
		}
	}
}

// TestGoGeneratorWebTransportDialsThroughTransport pins the emitted code to
// the type name that exists in the version above. webtransport-go renamed
// Dialer to Transport in v0.12.0, so emitting &webtransport.Dialer{} against
// that pin is an undefined-symbol compile error in the generated client --
// invisible to both the parse gate and the unused-import gate, since the
// identifier is syntactically fine and the package import is genuinely used.
func TestGoGeneratorWebTransportDialsThroughTransport(t *testing.T) {
	result, err := golang.NewGenerator().Generate(context.Background(), specWithCookieAuth(t), authConfigForTest())
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	wtCode, ok := result.Files["webtransport.go"]
	if !ok {
		t.Fatal("webtransport.go not found")
	}

	if !strings.Contains(wtCode, "&webtransport.Transport{}") {
		t.Errorf("webtransport.go does not construct a webtransport.Transport\n%s", wtCode)
	}

	if strings.Contains(wtCode, "webtransport.Dialer") {
		t.Error("webtransport.go still constructs the pre-v0.12.0 webtransport.Dialer")
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

// TestGoGeneratorWarnsOnUnemittedCookieParams is the regression test for a
// ruling that was recorded and never implemented: Endpoint.CookieParams
// landed on both IR builders (spec_parser.go, introspector.go), but nothing
// warned that the Go generator does not emit it, so a non-auth `in: cookie`
// parameter still vanished with no trace -- just one layer later than before
// the field existed.
func TestGoGeneratorWarnsOnUnemittedCookieParams(t *testing.T) {
	spec := &client.APISpec{
		Info: client.APIInfo{Title: "Test API", Version: "1.0.0"},
		Endpoints: []client.Endpoint{
			{
				ID:     "getWidget",
				Method: "GET",
				Path:   "/widgets/{id}",
				CookieParams: []client.Parameter{
					{Name: "trackingId", In: "cookie"},
					{Name: "locale", In: "cookie"},
				},
			},
		},
	}

	config := client.GeneratorConfig{PackageName: "testclient", Version: "1.0.0"}

	result, err := golang.NewGenerator().Generate(context.Background(), spec, config)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	var found bool

	for _, w := range result.Warnings {
		if strings.Contains(w, "GET /widgets/{id}") && strings.Contains(w, "trackingId") && strings.Contains(w, "locale") {
			found = true
		}
	}

	if !found {
		t.Errorf("Warnings = %v, want one naming GET /widgets/{id} and its cookie parameters", result.Warnings)
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

// TestGoGeneratorWebSocketConnectCallsApplyWithHandshakeURL is the targeted
// assertion the review flagged as missing for WebSocket: the aggregate
// ".apply(" count in TestGoGeneratorRoutesEveryTransportThroughApply above is
// already satisfied by client.go's addAuth and webtransport.go alone, so
// deleting WebSocket's own apply call would not fail it. webtransport.go got
// a targeted dial-shape assertion after an earlier review flagged this same
// weakness there (see TestGoGeneratorWebTransportDialPassesAuthHeader);
// WebSocket did not, until now.
func TestGoGeneratorWebSocketConnectCallsApplyWithHandshakeURL(t *testing.T) {
	result, err := golang.NewGenerator().Generate(context.Background(), specWithCookieAuth(t), authConfigForTest())
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	wsCode, ok := result.Files["websocket.go"]
	if !ok {
		t.Fatal("websocket.go not found")
	}

	if !strings.Contains(wsCode, "ws.client.auth.apply(header, u)") {
		t.Errorf("Connect does not call apply with the parsed handshake URL\n%s", wsCode)
	}

	if !strings.Contains(wsCode, "dialer.DialContext(ctx, endpoint, header)") {
		t.Errorf("Connect still dials without the auth header\n%s", wsCode)
	}
}

// TestGoGeneratorWebSocketSeedsHeaderFromJarBeforeApply is the regression
// test for the defect finding 2 flagged: gorilla's Dialer applies dialer.Jar
// to the handshake request first and then copies the caller's header over
// it, and "Cookie" hits the default wholesale-replace branch of that copy.
// Without seeding header from the jar before apply runs, a typed cookie
// field on AuthConfig would silently wipe out whatever session cookie the
// jar had just contributed.
func TestGoGeneratorWebSocketSeedsHeaderFromJarBeforeApply(t *testing.T) {
	result, err := golang.NewGenerator().Generate(context.Background(), specWithCookieAuth(t), authConfigForTest())
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	wsCode, ok := result.Files["websocket.go"]
	if !ok {
		t.Fatal("websocket.go not found")
	}

	seedIdx := strings.Index(wsCode, "ws.client.httpClient.Jar; jar != nil && u != nil")
	applyIdx := strings.Index(wsCode, "ws.client.auth.apply(header, u)")

	if seedIdx == -1 {
		t.Fatalf("websocket.go does not seed header from the jar\n%s", wsCode)
	}

	if applyIdx == -1 {
		t.Fatalf("websocket.go does not call apply\n%s", wsCode)
	}

	if seedIdx > applyIdx {
		t.Errorf("jar seeding happens after apply, so apply's merge has nothing to merge into\n%s", wsCode)
	}

	// dialer.Jar must stay set too: that is what lets the handshake response
	// populate the jar in the first place.
	if !strings.Contains(wsCode, "dialer.Jar = ws.client.httpClient.Jar") {
		t.Errorf("websocket.go no longer sets dialer.Jar\n%s", wsCode)
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

// TestGoGeneratorGeneratedModuleBuilds writes a generated client into a
// temporary module and runs the real compiler over it.
//
// The AST gates below catch two specific classes and nothing else: bad syntax
// and dead imports. Three of the six defects this test was added alongside
// were invisible to both -- a doc comment whose spliced type name broke out
// of the comment (that one the parser did catch), a go.mod missing a require
// for a package the code imports, and a reference to webtransport.Dialer
// after the package renamed the type to Transport. An undefined symbol and an
// unresolvable module are perfectly valid syntax with no unused imports, so
// only a compiler finds them. Each has a targeted regression test of its own
// now, but targeted tests only cover the mistakes already made once; this
// covers the class.
//
// The cost is a dependency on an external toolchain, which is why the earlier
// gates deliberately stopped short of it. That cost is contained by skipping
// rather than failing whenever the environment cannot support the build:
// under -short, with no go on PATH, or when a module cannot be resolved from
// the local cache. Compile errors are never skipped -- only environment
// shortfalls are -- so a green run offline still means everything that could
// be checked was.
func TestGoGeneratorGeneratedModuleBuilds(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping go build gate under -short")
	}

	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skipf("no go toolchain on PATH: %v", err)
	}

	withModule := func(config client.GeneratorConfig) client.GeneratorConfig {
		config.Module = "github.com/example/generated"

		return config
	}

	cases := []struct {
		name   string
		spec   *client.APISpec
		config client.GeneratorConfig
	}{
		// Auth present and every transport carrying a $ref schema: the
		// densest shape, and the one most of this file's other tests use.
		{"all transports, cookie auth", specAllTransportsWithAuth(t), withModule(authStreamingConfig())},

		// The opposite corner: no auth, no schemas, no reconnection, so every
		// conditionally-emitted block is off and every conditional import has
		// to have been withheld correctly.
		{"all transports, no schemas", specSchemalessTransports(), withModule(schemalessStreamingConfig())},

		// Anonymous structs spliced into comments, signatures, and return
		// types.
		{"inline streaming schemas", specInlineStreamingSchemas(), withModule(noAuthStreamingConfig())},

		// One streaming transport on its own. The three cases above all carry
		// every transport at once, so a file leaning on a declaration another
		// file happens to emit compiles there and nowhere else.
		{"sse only", specSSEOnly(), withModule(sseOnlyStreamingConfig())},

		// The other side of that gate: needsSharedStreaming leaves
		// WebTransport out, because webtransport.go refers to nothing
		// streaming.go declares. If that ever stops being true, this spec --
		// the only one that gets no streaming.go at all -- is where it shows.
		{"webtransport only", specWebTransportOnly(), withModule(webTransportOnlyStreamingConfig())},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := golang.NewGenerator().Generate(context.Background(), tc.spec, tc.config)
			if err != nil {
				t.Fatalf("Generate failed: %v", err)
			}

			dir := t.TempDir()

			for name, src := range result.Files {
				if !strings.HasSuffix(name, ".go") && name != "go.mod" {
					continue
				}

				if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o600); err != nil {
					t.Fatalf("write %s: %v", name, err)
				}
			}

			cmd := exec.Command(goBin, "build", "./...")
			cmd.Dir = dir
			// GOPROXY=off keeps the test off the network: everything has to
			// come from the local module cache or the build does not happen
			// at all, and is skipped below rather than failed. -mod=mod lets
			// the go command write the go.sum the generator does not emit,
			// computing the hashes from that same cache, and GOSUMDB=off
			// stops it wanting to corroborate them against the checksum
			// database it cannot reach.
			//
			// Deliberately no GOPRIVATE/GONOPROXY here, however tempting as a
			// second way to silence the checksum database: those mark modules
			// as fetch-direct, which overrides GOPROXY=off and puts the test
			// back on the network.
			cmd.Env = append(os.Environ(),
				"GOFLAGS=-mod=mod",
				"GOPROXY=off",
				"GOSUMDB=off",
			)

			out, err := cmd.CombinedOutput()
			if err == nil {
				return
			}

			if isModuleResolutionFailure(string(out)) {
				t.Skipf("module cache cannot resolve the generated client's dependencies offline:\n%s", out)
			}

			t.Errorf("generated module does not build: %v\n%s", err, out)
		})
	}
}

// isModuleResolutionFailure reports whether a failed `go build` failed
// because it could not obtain a module, rather than because the code it was
// handed does not compile. Only the former is a reason to skip.
//
// The strings are the go command's own wording for a dependency it cannot
// reach with the network disabled. Matching them narrowly is deliberate: a
// broad match would quietly turn real compile failures into skips, which is
// the one way this gate could end up worse than not having it.
func isModuleResolutionFailure(output string) bool {
	for _, marker := range []string{
		"module lookup disabled by GOPROXY=off",
		"missing go.sum entry",
		"cannot query module",
		"no required module provides package",
	} {
		if strings.Contains(output, marker) {
			return true
		}
	}

	return false
}

// TestGoGeneratorEmitsSyntacticallyValidGo parses every emitted .go file.
// Generated auth code is a struct plus a chain of conditionals assembled by
// string concatenation, which is the shape most likely to produce invalid
// Go from a bad template. Nothing else here would catch that.
//
// This is a syntax check only: go/parser accepts an unused import, a call
// to an undeclared identifier, or a swapped return-value order (all three
// were real, pre-existing bugs in this package -- see
// TestGoGeneratorHasNoUnusedImports and TestGoGeneratorNoAuthOmitsAuthReferences
// below for the checks that catch those). It does not compile anything
// either -- TestGoGeneratorGeneratedModuleBuilds above does that -- but it
// stays worth keeping separately: it needs no toolchain, so it still runs
// under -short and on a machine whose module cache cannot resolve the
// generated client's dependencies, and a parse error names the offending
// line far more directly than a compiler cascading off it.
func TestGoGeneratorEmitsSyntacticallyValidGo(t *testing.T) {
	cases := []struct {
		name   string
		spec   *client.APISpec
		config client.GeneratorConfig
	}{
		{"cookie auth", specWithCookieAuth(t), authConfigForTest()},

		// A streaming payload declared inline rather than as a $ref renders
		// through schemaToGoType as a multi-line anonymous struct, and
		// webtransport.go splices that name into single-line doc comments
		// ("// SendDatagram sends a %s as an unreliable datagram"). The
		// struct body's newlines escape the comment, so the remainder lands
		// at top level as stray tokens. Every other fixture in this file
		// uses a $ref, which resolves to a bare type name and hides this.
		{"inline streaming schemas", specInlineStreamingSchemas(), noAuthStreamingConfig()},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			files, err := golang.NewGenerator().Generate(context.Background(), tc.spec, tc.config)
			if err != nil {
				t.Fatalf("generate: %v", err)
			}

			for name, src := range files.Files {
				if !strings.HasSuffix(name, ".go") {
					continue
				}

				if _, err := parser.ParseFile(token.NewFileSet(), name, src, parser.AllErrors); err != nil {
					t.Errorf("%s does not parse: %v\n%s", name, err, src)
				}
			}
		})
	}
}

// specInlineStreamingSchemas declares its streaming payloads inline instead
// of by $ref, on all three of WebTransport's schema slots plus WebSocket and
// SSE, so every doc comment that splices a rendered type name is exercised.
func specInlineStreamingSchemas() *client.APISpec {
	inline := func() *client.Schema {
		return &client.Schema{
			Type:       "object",
			Properties: map[string]*client.Schema{"text": {Type: "string"}},
		}
	}

	return &client.APISpec{
		Info:       client.APIInfo{Title: "Inline API", Version: "1.0.0"},
		Servers:    []client.Server{{URL: "https://api.example.com"}},
		Endpoints:  []client.Endpoint{{ID: "listOrders", Method: "GET", Path: "/orders"}},
		WebSockets: []client.WebSocketEndpoint{{ID: "chat", Path: "/chat", SendSchema: inline(), ReceiveSchema: inline()}},
		SSEs:       []client.SSEEndpoint{{ID: "notifications", Path: "/notifications", EventSchemas: map[string]*client.Schema{"alert": inline()}}},
		WebTransports: []client.WebTransportEndpoint{{
			ID: "data", Path: "/wt/data",
			BiStreamSchema:  &client.StreamSchema{SendSchema: inline(), ReceiveSchema: inline()},
			UniStreamSchema: &client.StreamSchema{SendSchema: inline()},
			DatagramSchema:  inline(),
		}},
	}
}

// assertNoUnusedImports parses src and fails t if any imported package is
// never referenced. An unused import is a Go compile error, but not a parse
// error, so go/parser (TestGoGeneratorEmitsSyntacticallyValidGo above) does
// not catch it -- this is what actually caught defects 1-3 of task 6: dead
// "context"/"fmt" in client.go, dead "net/url"/"strings" in rest.go, and a
// dead "time" in types.go, all left over from code that used to live in
// those files and later moved out.
//
// "Used" means referenced as pkg.Ident anywhere in the file -- ast.Inspect
// walks both expressions and type positions (e.g. *websocket.Conn is a
// SelectorExpr just like websocket.Foo()), so a field type counts as a use.
// Blank imports (_) are exempt by design; this generator emits no dot
// imports, but they are skipped too rather than mis-flagged, since "import ."
// usage can't be attributed to a single identifier.
func assertNoUnusedImports(t *testing.T, filename, src string) {
	t.Helper()

	file, err := parser.ParseFile(token.NewFileSet(), filename, src, parser.AllErrors)
	if err != nil {
		t.Fatalf("%s does not parse: %v\n%s", filename, err, src)
	}

	used := map[string]bool{}

	ast.Inspect(file, func(n ast.Node) bool {
		if sel, ok := n.(*ast.SelectorExpr); ok {
			if ident, ok := sel.X.(*ast.Ident); ok {
				used[ident.Name] = true
			}
		}

		return true
	})

	for _, imp := range file.Imports {
		path := strings.Trim(imp.Path.Value, `"`)

		var name string

		switch {
		case imp.Name != nil:
			name = imp.Name.Name
		case importPackageNameOverrides[path] != "":
			name = importPackageNameOverrides[path]
		default:
			name = path[strings.LastIndex(path, "/")+1:]
		}

		if name == "_" || name == "." {
			continue
		}

		if !used[name] {
			t.Errorf("%s: import %q is never referenced", filename, path)
		}
	}
}

// importPackageNameOverrides maps an import path to the identifier code
// actually refers to it by, for the handful of this generator's imports
// where that is not the last path segment. Go requires an unaliased
// import's identifier to match the imported package's own `package NAME`
// clause, not its directory name -- and hyphens, which webtransport-go's
// repository name contains, cannot appear in a Go identifier at all, so
// there the two must differ. Resolving this in general needs the module
// source (via go/packages or a populated module cache), which is exactly
// what this test suite avoids depending on; since the generator only ever
// emits a small, fixed set of external imports, hardcoding the one
// exception is cheaper and just as correct.
var importPackageNameOverrides = map[string]string{
	"github.com/quic-go/webtransport-go": "webtransport",
}

// TestGoGeneratorHasNoUnusedImports is the regression test for defects 1-3
// of task 6. Each conditional import is exercised on both sides: a spec
// shape that needs it (import present) and one that does not (import
// absent) -- a gate that only ever saw the "needs it" case would not have
// caught any of these, since the bug was specifically the import surviving
// into the case that didn't need it.
func TestGoGeneratorHasNoUnusedImports(t *testing.T) {
	bearerAuthSpec := func() *client.APISpec {
		return &client.APISpec{
			Info: client.APIInfo{Title: "Bearer API", Version: "1.0.0"},
			Security: []client.SecurityScheme{
				{Key: "bearerAuth", Type: "http", Scheme: "bearer"},
			},
			Endpoints: []client.Endpoint{
				{ID: "listItems", Method: "GET", Path: "/items", Security: []client.SecurityRequirement{{SchemeName: "bearerAuth"}}},
			},
		}
	}

	basicAuthSpec := func() *client.APISpec {
		return &client.APISpec{
			Info: client.APIInfo{Title: "Basic API", Version: "1.0.0"},
			Security: []client.SecurityScheme{
				{Key: "basicAuth", Type: "http", Scheme: "basic"},
			},
			Endpoints: []client.Endpoint{
				{ID: "listItems", Method: "GET", Path: "/items", Security: []client.SecurityRequirement{{SchemeName: "basicAuth"}}},
			},
		}
	}

	queryParamSpec := func() *client.APISpec {
		return &client.APISpec{
			Info: client.APIInfo{Title: "Query API", Version: "1.0.0"},
			Endpoints: []client.Endpoint{
				{
					ID: "listItems", Method: "GET", Path: "/items",
					QueryParams: []client.Parameter{
						{Name: "limit", Schema: &client.Schema{Type: "integer"}},
					},
				},
			},
		}
	}

	noQueryParamSpec := func() *client.APISpec {
		return &client.APISpec{
			Info:      client.APIInfo{Title: "Plain API", Version: "1.0.0"},
			Endpoints: []client.Endpoint{{ID: "listItems", Method: "GET", Path: "/items"}},
		}
	}

	dateTimeSchemaSpec := func() *client.APISpec {
		return &client.APISpec{
			Info: client.APIInfo{Title: "Timestamps API", Version: "1.0.0"},
			Schemas: map[string]*client.Schema{
				"Event": {
					Type: "object",
					Properties: map[string]*client.Schema{
						"occurredAt": {Type: "string", Format: "date-time"},
					},
				},
			},
		}
	}

	noSchemaSpec := func() *client.APISpec {
		return &client.APISpec{Info: client.APIInfo{Title: "No Schemas API", Version: "1.0.0"}}
	}

	cases := []struct {
		name   string
		spec   *client.APISpec
		config client.GeneratorConfig
	}{
		// client.go's dead "context"/"fmt": both are always unused today
		// (doRequest, which needed them, lives in rest.go now), so every
		// case below doubles as the "does not need it" side. There is no
		// spec shape that makes client.go need them, which is the point --
		// the fix removes two imports client.go never legitimately used.
		{"minimal, no auth, no endpoints", noSchemaSpec(), client.GeneratorConfig{PackageName: "c", Version: "1.0.0"}},
		{"bearer auth, no query params", bearerAuthSpec(), client.GeneratorConfig{PackageName: "c", Version: "1.0.0", IncludeAuth: true}},

		// rest.go's "net/url": needed only when some endpoint declares a
		// query parameter.
		{"query params present", queryParamSpec(), client.GeneratorConfig{PackageName: "c", Version: "1.0.0"}},
		{"no query params", noQueryParamSpec(), client.GeneratorConfig{PackageName: "c", Version: "1.0.0"}},

		// client.go's "encoding/base64": needed only when a basic scheme is
		// declared (pre-existing Task 4 behaviour, re-checked here alongside
		// the new gates so the whole import block is covered in one table).
		{"basic auth present", basicAuthSpec(), client.GeneratorConfig{PackageName: "c", Version: "1.0.0", IncludeAuth: true}},

		// types.go's "time": needed only when some schema field is a
		// string with format date/date-time.
		{"date-time schema present", dateTimeSchemaSpec(), client.GeneratorConfig{PackageName: "c", Version: "1.0.0"}},
		{"no schemas at all", noSchemaSpec(), client.GeneratorConfig{PackageName: "c", Version: "1.0.0"}},

		// websocket.go/webtransport.go's "net/http"/"net/url": needed only
		// when a scheme is declared, since AuthConfig.apply is the only
		// thing in either file that reaches for them. The auth-present case
		// reuses specNoAuthAllTransports's shape with a scheme added, rather
		// than the older specWithCookieAuth fixture other tests in this file
		// share, so that both sides of this pair differ only in whether a
		// scheme is declared. The schema-less shape that fixture happens to
		// have is now covered deliberately, two cases below, instead of
		// arriving here as a confound.
		{"all transports, no auth (defect 4)", specNoAuthAllTransports(t), noAuthStreamingConfig()},
		{"all transports, cookie auth (defect 4)", specAllTransportsWithAuth(t), authStreamingConfig()},

		// websocket.go's/webtransport.go's "encoding/json", webtransport.go's
		// "io" and "time", and sse.go's "time": each has a single class of
		// user (marshalling a payload, copying a stream, sleeping between
		// reconnects) that a schema-less endpoint or a Reconnection-off config
		// removes entirely. Every case above carries a schema on every
		// streaming endpoint, so this is the first case to exercise the side
		// where those imports have nothing left to reference.
		{"all transports, no schemas", specSchemalessTransports(), schemalessStreamingConfig()},

		// The same spec with Reconnection back on, so the two axes are not
		// conflated: this case still has nothing to marshal but does have a
		// reconnect delay to sleep on, which is what separates "json is dead
		// because no schema" from "time is dead because no reconnection".
		{"all transports, no schemas, reconnection on", specSchemalessTransports(), func() client.GeneratorConfig {
			config := schemalessStreamingConfig()
			config.Features.Reconnection = true

			return config
		}()},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := golang.NewGenerator().Generate(context.Background(), tc.spec, tc.config)
			if err != nil {
				t.Fatalf("Generate failed: %v", err)
			}

			for name, src := range result.Files {
				if !strings.HasSuffix(name, ".go") {
					continue
				}

				assertNoUnusedImports(t, name, src)
			}
		})
	}
}

// specNoAuthAllTransports returns a spec with no security schemes at all,
// carrying a REST, WebSocket, SSE, and WebTransport endpoint apiece, so
// every file that used to reference c.auth/ws.client.auth unconditionally
// (client.go, websocket.go, webtransport.go) is exercised by one Generate
// call.
//
// Every streaming endpoint carries a schema, and every schema is a $ref.
// That was originally a way of steering around three separate bugs this
// fixture was not meant to be testing; all three are fixed now, and each has
// a fixture of its own that pins it -- specSchemalessTransports for the
// dead imports a payload-less endpoint used to leave behind, and
// specInlineStreamingSchemas for the anonymous struct that used to break out
// of a doc comment. Keeping this one at its densest, most conventional shape
// is what makes it a useful contrast to those two.
func specNoAuthAllTransports(t *testing.T) *client.APISpec {
	t.Helper()

	textMsg := &client.Schema{Ref: "#/components/schemas/TextMessage"}

	return &client.APISpec{
		Info:    client.APIInfo{Title: "Public API", Version: "1.0.0"},
		Servers: []client.Server{{URL: "https://api.example.com"}},
		Schemas: map[string]*client.Schema{
			"TextMessage": {Type: "object", Properties: map[string]*client.Schema{"text": {Type: "string"}}},
		},
		Endpoints: []client.Endpoint{{ID: "listOrders", Method: "GET", Path: "/orders"}},
		WebSockets: []client.WebSocketEndpoint{
			{ID: "chat", Path: "/chat", SendSchema: textMsg, ReceiveSchema: textMsg},
		},
		SSEs: []client.SSEEndpoint{
			{ID: "notifications", Path: "/notifications", EventSchemas: map[string]*client.Schema{"alert": textMsg}},
		},
		WebTransports: []client.WebTransportEndpoint{
			{ID: "data", Path: "/wt/data", DatagramSchema: textMsg},
		},
	}
}

// noAuthStreamingConfig mirrors authConfigForTest but leaves IncludeAuth
// off, so specNoAuthAllTransports (which declares no security schemes
// anyway) exercises the "AuthConfig does not exist" path deliberately
// rather than by omission. Reconnection is on so sse.go's reconnect delay
// -- and therefore its own "time" import -- is exercised too, for the same
// reason the streaming endpoints above all carry a schema.
func noAuthStreamingConfig() client.GeneratorConfig {
	return client.GeneratorConfig{
		Language:         "go",
		PackageName:      "publicclient",
		APIName:          "PublicClient",
		BaseURL:          "https://api.example.com",
		Version:          "1.0.0",
		Features:         client.Features{Reconnection: true},
		IncludeStreaming: true,
	}
}

// specAllTransportsWithAuth is specNoAuthAllTransports's mirror image: same
// four endpoints, same schemas, but with a declared scheme required
// everywhere, so this is the "needs net/http and net/url" side of the same
// gate specNoAuthAllTransports exercises the "does not need them" side of.
func specAllTransportsWithAuth(t *testing.T) *client.APISpec {
	t.Helper()

	spec := specNoAuthAllTransports(t)
	spec.Security = []client.SecurityScheme{
		{Key: "sessionAuth", Type: "apiKey", In: "cookie", ParamName: "session_id"},
	}
	requirement := []client.SecurityRequirement{{SchemeName: "sessionAuth"}}
	spec.Endpoints[0].Security = requirement
	spec.WebSockets[0].Security = requirement
	spec.SSEs[0].Security = requirement

	return spec
}

// authStreamingConfig is noAuthStreamingConfig with auth turned on.
func authStreamingConfig() client.GeneratorConfig {
	config := noAuthStreamingConfig()
	config.IncludeAuth = true

	return config
}

// specSchemalessTransports is specNoAuthAllTransports with every send,
// receive, event, and datagram schema stripped off. A streaming endpoint
// declared without a payload schema is a legitimate shape -- the manifest
// makes all four fields optional -- and it is the side of each streaming
// file's import block that nothing exercised until now: with no schema
// there is nothing to marshal, so websocket.go's and webtransport.go's
// "encoding/json" have no use left, and webtransport.go loses its "io" and
// "time" users along with the stream and reconnect helpers those schemas
// gate.
func specSchemalessTransports() *client.APISpec {
	return &client.APISpec{
		Info:          client.APIInfo{Title: "Schemaless API", Version: "1.0.0"},
		Servers:       []client.Server{{URL: "https://api.example.com"}},
		Endpoints:     []client.Endpoint{{ID: "listOrders", Method: "GET", Path: "/orders"}},
		WebSockets:    []client.WebSocketEndpoint{{ID: "chat", Path: "/chat"}},
		SSEs:          []client.SSEEndpoint{{ID: "notifications", Path: "/notifications"}},
		WebTransports: []client.WebTransportEndpoint{{ID: "data", Path: "/wt/data"}},
	}
}

// schemalessStreamingConfig turns Reconnection off as well, so sse.go's
// "time" -- whose only user is the reconnect delay -- is exercised on its
// unused side too. noAuthStreamingConfig deliberately leaves it on.
func schemalessStreamingConfig() client.GeneratorConfig {
	return client.GeneratorConfig{
		Language:         "go",
		PackageName:      "schemalessclient",
		APIName:          "SchemalessClient",
		BaseURL:          "https://api.example.com",
		Version:          "1.0.0",
		IncludeStreaming: true,
	}
}

// specSSEOnly declares SSE alongside REST and nothing else. Every other
// streaming fixture in this file carries all three transports at once, which
// hid a whole class of defect: the shared streaming declarations used to live
// in websocket.go, so anything sse.go referenced from them resolved only
// because a WebSocket endpoint happened to be present in the same spec. An
// API that streams over SSE alone is an ordinary shape, and it is the one
// that has nothing to borrow those declarations from.
func specSSEOnly() *client.APISpec {
	return &client.APISpec{
		Info:      client.APIInfo{Title: "SSE Only API", Version: "1.0.0"},
		Servers:   []client.Server{{URL: "https://api.example.com"}},
		Endpoints: []client.Endpoint{{ID: "listOrders", Method: "GET", Path: "/orders"}},
		SSEs: []client.SSEEndpoint{{
			ID:   "notifications",
			Path: "/notifications",
			EventSchemas: map[string]*client.Schema{
				"alert": {Ref: "#/components/schemas/Alert"},
			},
		}},
		Schemas: map[string]*client.Schema{
			"Alert": {
				Type:       "object",
				Properties: map[string]*client.Schema{"message": {Type: "string"}},
			},
		},
	}
}

// sseOnlyStreamingConfig turns on both features sse.go reaches into the
// shared streaming declarations for: StateManagement pulls in ConnectionState
// and its constants, Reconnection pulls in reconnectConfig,
// defaultReconnectConfig, and calculateBackoff.
func sseOnlyStreamingConfig() client.GeneratorConfig {
	return client.GeneratorConfig{
		Language:         "go",
		PackageName:      "sseclient",
		APIName:          "SSEClient",
		BaseURL:          "https://api.example.com",
		Version:          "1.0.0",
		Features:         client.Features{Reconnection: true, StateManagement: true},
		IncludeStreaming: true,
	}
}

// specWebTransportOnly is specSSEOnly's counterpart for the transport that
// shares nothing: webtransport.go declares its own state handling, so this is
// the shape for which no streaming.go is emitted at all.
func specWebTransportOnly() *client.APISpec {
	return &client.APISpec{
		Info:      client.APIInfo{Title: "WebTransport Only API", Version: "1.0.0"},
		Servers:   []client.Server{{URL: "https://api.example.com"}},
		Endpoints: []client.Endpoint{{ID: "listOrders", Method: "GET", Path: "/orders"}},
		WebTransports: []client.WebTransportEndpoint{{
			ID:             "data",
			Path:           "/wt/data",
			BiStreamSchema: &client.StreamSchema{SendSchema: &client.Schema{Ref: "#/components/schemas/Alert"}},
			DatagramSchema: &client.Schema{Ref: "#/components/schemas/Alert"},
		}},
		Schemas: map[string]*client.Schema{
			"Alert": {
				Type:       "object",
				Properties: map[string]*client.Schema{"message": {Type: "string"}},
			},
		},
	}
}

// webTransportOnlyStreamingConfig turns on the same two features
// sseOnlyStreamingConfig does, so the absence of streaming.go here is down to
// the transport rather than to the features being off.
func webTransportOnlyStreamingConfig() client.GeneratorConfig {
	config := sseOnlyStreamingConfig()
	config.PackageName = "wtclient"
	config.APIName = "WTClient"

	return config
}

// TestGoGeneratorNoAuthOmitsAuthReferences is the regression test for
// defect 4: client.go and websocket.go referenced c.auth/ws.client.auth
// unconditionally, even though AuthConfig is only declared when
// needsAuthConfig is true. A spec with no security schemes at all used to
// produce files with an undefined-identifier compile error. webtransport.go
// carried the identical bug (a c.auth.apply call and an unconditional
// *AuthConfig field/constructor parameter) even though the brief's defect 4
// only named the first two files, so this spec includes a WebTransport
// endpoint too rather than leaving that file's copy of the same bug
// unverified.
func TestGoGeneratorNoAuthOmitsAuthReferences(t *testing.T) {
	spec := specNoAuthAllTransports(t)

	result, err := golang.NewGenerator().Generate(context.Background(), spec, noAuthStreamingConfig())
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	all := strings.Join(valuesOf(result.Files), "\n")

	for _, unwanted := range []string{"c.auth", "ws.client.auth", "AuthConfig"} {
		if strings.Contains(all, unwanted) {
			t.Errorf("no-auth spec still emits %q\n%s", unwanted, all)
		}
	}

	// And the syntax/unused-import gates should both be clean for this
	// no-auth shape specifically, not just the auth-present specs the other
	// tests in this file use.
	for name, src := range result.Files {
		if !strings.HasSuffix(name, ".go") {
			continue
		}

		assertNoUnusedImports(t, name, src)
	}
}

// TestGoGeneratorWebTransportDialPassesAuthHeader is the regression test for
// defect 5 and the assertion the task-5 review flagged as missing: the old
// code destructured dialer.Dial as (*Session, *http.Response, error), but
// every version of github.com/quic-go/webtransport-go -- v0.6.0 (what this
// generator's getDependencies pins) through v0.12.0 (what this repo's own
// go.mod pins, checked directly against transport.go in the local module
// cache) -- returns (*http.Response, *Session, error). The previous
// aggregate ".apply(" count guarded auth routing but would not have noticed
// this file's dial line regressing back to a header-dropping (or
// order-swapped) call, so this asserts the exact call shape.
func TestGoGeneratorWebTransportDialPassesAuthHeader(t *testing.T) {
	result, err := golang.NewGenerator().Generate(context.Background(), specWithCookieAuth(t), authConfigForTest())
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	wtCode, ok := result.Files["webtransport.go"]
	if !ok {
		t.Fatal("webtransport.go not found")
	}

	// The receiver is spelled transport, not dialer, since v0.12.0 renamed
	// the type -- see TestGoGeneratorWebTransportDialsThroughTransport. The
	// argument list is what this test is actually about and did not change.
	if !strings.Contains(wtCode, "transport.Dial(ctx, wtURL, header)") {
		t.Errorf("webtransport.go does not dial with the non-nil auth header\n%s", wtCode)
	}

	// The swapped-order bug this replaces: session bound from the first
	// (response) position instead of the second.
	if strings.Contains(wtCode, "session, _, err := transport.Dial(") {
		t.Error("webtransport.go still destructures Dial as (*Session, *http.Response, error)")
	}
}
