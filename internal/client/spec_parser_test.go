package client_test

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
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

// additionalPropertiesYAML and additionalPropertiesJSON are the same document
// in both formats, each declaring five schemas that exercise every shape
// additionalProperties can legally take, plus a nested case:
//   - BoolTrue / BoolFalse: additionalProperties is a bare bool
//   - TypedString: additionalProperties is a schema object ({type: string})
//   - RefValue: additionalProperties is a schema object that is itself a $ref
//   - NestedArray: additionalProperties is a schema object with its own
//     nested Items, proving the normalisation recurses rather than only
//     handling one level
const additionalPropertiesYAML = `
openapi: 3.1.0
info:
  title: AdditionalProperties Test
  version: 1.0.0
paths:
  /noop:
    get:
      summary: noop
      responses:
        '200':
          description: ok
components:
  schemas:
    User:
      type: object
      properties:
        id:
          type: string
    BoolTrue:
      type: object
      additionalProperties: true
    BoolFalse:
      type: object
      additionalProperties: false
    TypedString:
      type: object
      additionalProperties:
        type: string
    RefValue:
      type: object
      additionalProperties:
        $ref: '#/components/schemas/User'
    NestedArray:
      type: object
      additionalProperties:
        type: array
        items:
          type: string
`

const additionalPropertiesJSON = `{
  "openapi": "3.1.0",
  "info": {
    "title": "AdditionalProperties Test",
    "version": "1.0.0"
  },
  "paths": {
    "/noop": {
      "get": {
        "summary": "noop",
        "responses": {
          "200": { "description": "ok" }
        }
      }
    }
  },
  "components": {
    "schemas": {
      "User": {
        "type": "object",
        "properties": { "id": { "type": "string" } }
      },
      "BoolTrue": {
        "type": "object",
        "additionalProperties": true
      },
      "BoolFalse": {
        "type": "object",
        "additionalProperties": false
      },
      "TypedString": {
        "type": "object",
        "additionalProperties": { "type": "string" }
      },
      "RefValue": {
        "type": "object",
        "additionalProperties": { "$ref": "#/components/schemas/User" }
      },
      "NestedArray": {
        "type": "object",
        "additionalProperties": {
          "type": "array",
          "items": { "type": "string" }
        }
      }
    }
  }
}`

// assertAdditionalPropertiesNormalised is shared by the YAML and JSON variants
// of TestSpecParserAdditionalProperties so both formats are held to the exact
// same expectations.
func assertAdditionalPropertiesNormalised(t *testing.T, spec *client.APISpec) {
	t.Helper()

	boolTrue, ok := spec.Schemas["BoolTrue"]
	if !ok {
		t.Fatal("BoolTrue schema not found")
	}

	if v, ok := boolTrue.AdditionalProperties.(bool); !ok || !v {
		t.Errorf("BoolTrue.AdditionalProperties = %#v (%T), want bool(true)", boolTrue.AdditionalProperties, boolTrue.AdditionalProperties)
	}

	boolFalse, ok := spec.Schemas["BoolFalse"]
	if !ok {
		t.Fatal("BoolFalse schema not found")
	}

	if v, ok := boolFalse.AdditionalProperties.(bool); !ok || v {
		t.Errorf("BoolFalse.AdditionalProperties = %#v (%T), want bool(false)", boolFalse.AdditionalProperties, boolFalse.AdditionalProperties)
	}

	typedString, ok := spec.Schemas["TypedString"]
	if !ok {
		t.Fatal("TypedString schema not found")
	}

	typedStringAP, ok := typedString.AdditionalProperties.(*client.Schema)
	if !ok {
		t.Fatalf("TypedString.AdditionalProperties = %#v (%T), want *client.Schema", typedString.AdditionalProperties, typedString.AdditionalProperties)
	}

	if typedStringAP.Type != "string" {
		t.Errorf("TypedString.AdditionalProperties.Type = %q, want \"string\"", typedStringAP.Type)
	}

	refValue, ok := spec.Schemas["RefValue"]
	if !ok {
		t.Fatal("RefValue schema not found")
	}

	refValueAP, ok := refValue.AdditionalProperties.(*client.Schema)
	if !ok {
		t.Fatalf("RefValue.AdditionalProperties = %#v (%T), want *client.Schema", refValue.AdditionalProperties, refValue.AdditionalProperties)
	}

	if refValueAP.Ref != "#/components/schemas/User" {
		t.Errorf("RefValue.AdditionalProperties.Ref = %q, want \"#/components/schemas/User\"", refValueAP.Ref)
	}

	nestedArray, ok := spec.Schemas["NestedArray"]
	if !ok {
		t.Fatal("NestedArray schema not found")
	}

	nestedArrayAP, ok := nestedArray.AdditionalProperties.(*client.Schema)
	if !ok {
		t.Fatalf("NestedArray.AdditionalProperties = %#v (%T), want *client.Schema", nestedArray.AdditionalProperties, nestedArray.AdditionalProperties)
	}

	if nestedArrayAP.Type != "array" {
		t.Errorf("NestedArray.AdditionalProperties.Type = %q, want \"array\"", nestedArrayAP.Type)
	}

	if nestedArrayAP.Items == nil {
		t.Fatal("NestedArray.AdditionalProperties.Items = nil, want a nested *client.Schema")
	}

	if nestedArrayAP.Items.Type != "string" {
		t.Errorf("NestedArray.AdditionalProperties.Items.Type = %q, want \"string\"", nestedArrayAP.Items.Type)
	}
}

// TestSpecParserAdditionalProperties asserts that SpecParser normalises
// Schema.AdditionalProperties for both a document-valued (schema) and a
// bool-valued additionalProperties, in both YAML and JSON. Before this fix,
// SpecParser copied the raw decoder output straight into the IR: a JSON/YAML
// boolean decodes to Go bool (already IR-shaped), but a JSON/YAML object
// decodes to map[string]any/map[string]interface{} — not *client.Schema —
// because shared.Schema.AdditionalProperties has no custom
// UnmarshalJSON/UnmarshalYAML for that field. The generator's
// additionalPropsSchema helper only recognises bool and *client.Schema, so a
// perfectly well-formed `additionalProperties: {type: string}` in a real spec
// file silently downgraded to "not allowed" (a closed interface) purely
// because of how the raw value arrived, not because of anything the spec
// author wrote.
func TestSpecParserAdditionalProperties(t *testing.T) {
	t.Run("YAML", func(t *testing.T) {
		tmpDir := t.TempDir()
		specFile := filepath.Join(tmpDir, "openapi.yaml")

		if err := os.WriteFile(specFile, []byte(additionalPropertiesYAML), 0644); err != nil {
			t.Fatalf("Failed to write spec file: %v", err)
		}

		parser := client.NewSpecParser()

		spec, err := parser.ParseFile(context.Background(), specFile)
		if err != nil {
			t.Fatalf("ParseFile failed: %v", err)
		}

		assertAdditionalPropertiesNormalised(t, spec)
	})

	t.Run("JSON", func(t *testing.T) {
		tmpDir := t.TempDir()
		specFile := filepath.Join(tmpDir, "openapi.json")

		if err := os.WriteFile(specFile, []byte(additionalPropertiesJSON), 0644); err != nil {
			t.Fatalf("Failed to write spec file: %v", err)
		}

		parser := client.NewSpecParser()

		spec, err := parser.ParseFile(context.Background(), specFile)
		if err != nil {
			t.Fatalf("ParseFile failed: %v", err)
		}

		assertAdditionalPropertiesNormalised(t, spec)
	})
}

// TestSpecParserResponseStatusCodes pins the parsing of the response map's
// keys.
//
// The status code was previously never read: `code` was initialised to zero
// and the branch meant to parse it was empty, so every response — including
// 200 — fell through to DefaultError and Endpoint.Responses was always empty.
// Nothing downstream failed loudly. The TypeScript generator simply found no
// 2xx response, typed every method `Promise<void>`, and produced a client that
// compiled cleanly while discarding every response body.
func TestSpecParserResponseStatusCodes(t *testing.T) {
	spec := `
openapi: 3.1.0
info:
  title: Status Codes
  version: 1.0.0
paths:
  /exact:
    get:
      operationId: exact
      responses:
        '200':
          description: OK
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/Thing'
        '404':
          description: Missing
        default:
          description: Error
  /wildcard:
    get:
      operationId: wildcard
      responses:
        '2XX':
          description: Any success
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/Thing'
  /both:
    get:
      operationId: both
      responses:
        '200':
          description: Specific
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/Thing'
        '2XX':
          description: Class fallback
  /nonsense:
    get:
      operationId: nonsense
      responses:
        'banana':
          description: Not a status
        '999':
          description: Out of range
components:
  schemas:
    Thing:
      type: object
      properties:
        id:
          type: string
`

	dir := t.TempDir()
	path := filepath.Join(dir, "spec.yaml")

	if err := os.WriteFile(path, []byte(spec), 0o600); err != nil {
		t.Fatalf("write spec: %v", err)
	}

	parsed, err := client.NewSpecParser().ParseFile(context.Background(), path)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	byPath := make(map[string]*client.Endpoint, len(parsed.Endpoints))

	for i := range parsed.Endpoints {
		byPath[parsed.Endpoints[i].Path] = &parsed.Endpoints[i]
	}

	t.Run("exact codes reach Responses", func(t *testing.T) {
		ep := byPath["/exact"]
		if ep == nil {
			t.Fatal("endpoint /exact missing")
		}

		ok, found := ep.Responses[http.StatusOK]
		if !found {
			t.Fatalf("no 200 response; got codes %v", codesOf(ep))
		}

		if ok.Content["application/json"] == nil {
			t.Fatal("200 lost its JSON content")
		}

		if _, found := ep.Responses[http.StatusNotFound]; !found {
			t.Errorf("no 404 response; got codes %v", codesOf(ep))
		}

		if ep.DefaultError == nil {
			t.Error("default response should still land in DefaultError")
		}
	})

	t.Run("class wildcards normalise to the base of the class", func(t *testing.T) {
		ep := byPath["/wildcard"]
		if ep == nil {
			t.Fatal("endpoint /wildcard missing")
		}

		if _, found := ep.Responses[http.StatusOK]; !found {
			t.Fatalf("2XX did not normalise to 200; got codes %v", codesOf(ep))
		}
	})

	t.Run("an exact code beats a wildcard landing on it", func(t *testing.T) {
		ep := byPath["/both"]
		if ep == nil {
			t.Fatal("endpoint /both missing")
		}

		got := ep.Responses[http.StatusOK]
		if got == nil {
			t.Fatalf("no 200 response; got codes %v", codesOf(ep))
		}

		// Map iteration order is random, so a single pass would make this flap.
		if got.Description != "Specific" {
			t.Errorf("wildcard overwrote the exact code: description = %q", got.Description)
		}
	})

	t.Run("unparseable keys are dropped, not filed as the error shape", func(t *testing.T) {
		ep := byPath["/nonsense"]
		if ep == nil {
			t.Fatal("endpoint /nonsense missing")
		}

		if len(ep.Responses) != 0 {
			t.Errorf("expected no responses, got codes %v", codesOf(ep))
		}

		if ep.DefaultError != nil {
			t.Error("a malformed status must not become the endpoint's error type")
		}
	})
}

func codesOf(ep *client.Endpoint) []int {
	codes := make([]int, 0, len(ep.Responses))
	for code := range ep.Responses {
		codes = append(codes, code)
	}

	sort.Ints(codes)

	return codes
}

// parseDoc writes doc to a temp file and parses it. The parser's entry point
// takes a file path rather than raw bytes, so every test that wants a parsed
// spec from an inline document goes through this.
func parseDoc(t *testing.T, doc string) *client.APISpec {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "openapi.json")

	if err := os.WriteFile(path, []byte(doc), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	spec, err := client.NewSpecParser().ParseFile(context.Background(), path)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	return spec
}

func TestParserKeepsTheApiKeyParameterName(t *testing.T) {
	doc := `{
	  "openapi": "3.0.0",
	  "info": {"title": "t", "version": "1"},
	  "paths": {},
	  "components": {"securitySchemes": {
	    "sessionAuth": {"type": "apiKey", "in": "cookie", "name": "session_id"},
	    "tenantKey":   {"type": "apiKey", "in": "header", "name": "X-Tenant-Key"},
	    "bearerAuth":  {"type": "http", "scheme": "bearer"}
	  }}
	}`

	spec := parseDoc(t, doc)

	byKey := map[string]client.SecurityScheme{}
	for _, s := range spec.Security {
		byKey[s.Key] = s
	}

	if got := byKey["sessionAuth"].ParamName; got != "session_id" {
		t.Errorf("cookie scheme ParamName = %q, want session_id", got)
	}

	if got := byKey["sessionAuth"].In; got != "cookie" {
		t.Errorf("cookie scheme In = %q, want cookie", got)
	}

	if got := byKey["tenantKey"].ParamName; got != "X-Tenant-Key" {
		t.Errorf("header scheme ParamName = %q, want X-Tenant-Key", got)
	}

	// http schemes carry no parameter name; the location is the Authorization
	// header by definition.
	if got := byKey["bearerAuth"].ParamName; got != "" {
		t.Errorf("http scheme ParamName = %q, want empty", got)
	}
}

func TestParserSortsSecuritySchemes(t *testing.T) {
	doc := `{
	  "openapi": "3.0.0",
	  "info": {"title": "t", "version": "1"},
	  "paths": {},
	  "components": {"securitySchemes": {
	    "zeta":  {"type": "http", "scheme": "bearer"},
	    "alpha": {"type": "apiKey", "in": "header", "name": "X-A"},
	    "mid":   {"type": "apiKey", "in": "query",  "name": "q"}
	  }}
	}`

	// A map range is unordered, so one parse proves nothing. Repeat it: the
	// emitted AuthConfig's field order is source order, and a generator whose
	// output moves between runs produces a spurious diff in every repository
	// that regenerates.
	for i := 0; i < 20; i++ {
		spec := parseDoc(t, doc)

		var keys []string
		for _, s := range spec.Security {
			keys = append(keys, s.Key)
		}

		want := []string{"alpha", "mid", "zeta"}
		if !slices.Equal(keys, want) {
			t.Fatalf("run %d: keys = %v, want %v", i, keys, want)
		}
	}
}

func TestParserKeepsCookieParameters(t *testing.T) {
	doc := `{
	  "openapi": "3.0.0",
	  "info": {"title": "t", "version": "1"},
	  "paths": {"/me": {"get": {"operationId": "me", "parameters": [
	    {"name": "sid", "in": "cookie", "required": true, "schema": {"type": "string"}}
	  ], "responses": {"200": {"description": "ok"}}}}}
	}`

	spec := parseDoc(t, doc)

	if len(spec.Endpoints) != 1 {
		t.Fatalf("endpoints = %d, want 1", len(spec.Endpoints))
	}

	got := spec.Endpoints[0].CookieParams
	if len(got) != 1 || got[0].Name != "sid" {
		t.Fatalf("CookieParams = %+v, want one named sid", got)
	}
}

func TestParserWarnsOnAnUnknownParameterLocation(t *testing.T) {
	doc := `{
	  "openapi": "3.0.0",
	  "info": {"title": "t", "version": "1"},
	  "paths": {"/me": {"get": {"operationId": "me", "parameters": [
	    {"name": "weird", "in": "telepathy", "schema": {"type": "string"}}
	  ], "responses": {"200": {"description": "ok"}}}}}
	}`

	spec := parseDoc(t, doc)

	// Silently dropping it is how the cookie case went unnoticed for so long.
	var found bool
	for _, w := range spec.Warnings {
		if strings.Contains(w, "telepathy") && strings.Contains(w, "weird") {
			found = true
		}
	}

	if !found {
		t.Fatalf("warnings = %v, want one naming the parameter and its location", spec.Warnings)
	}
}
