package typescript

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/xraph/forge/internal/client"
)

func TestGateFixturesCoverKnownDefects(t *testing.T) {
	want := []string{"default", "apiname", "odd-keys", "with-auth", "no-streaming", "no-auth-streaming", "ws-sse"}

	got := make(map[string]bool)
	for _, f := range gateFixtures() {
		got[f.Name] = true
	}

	for _, name := range want {
		if !got[name] {
			t.Errorf("fixture %q missing from corpus", name)
		}
	}
}

func TestGenerateToProducesTSConfig(t *testing.T) {
	dir := generateTo(t, gateFixtures()[0])

	if _, err := os.Stat(filepath.Join(dir, "tsconfig.json")); err != nil {
		t.Fatalf("expected generated tsconfig.json: %v", err)
	}
}

type gateFixture struct {
	Name   string
	Spec   *client.APISpec
	Config client.GeneratorConfig
}

// baseSpec returns a spec exercising path params, query params, a request body,
// and a $ref response.
func baseSpec() *client.APISpec {
	user := &client.Schema{
		Type:     "object",
		Required: []string{"id"},
		Properties: map[string]*client.Schema{
			"id":         {Type: "string"},
			"user_id":    {Type: "string"},
			"created_at": {Type: "string", Format: "date-time"},
		},
	}

	return &client.APISpec{
		Info: client.APIInfo{Title: "Probe API", Version: "1.0.0", Description: "probe"},
		Endpoints: []client.Endpoint{
			{
				Method: "GET", Path: "/users/{id}", OperationID: "users.get",
				PathParams:  []client.Parameter{{Name: "id", Schema: &client.Schema{Type: "string"}, Required: true}},
				QueryParams: []client.Parameter{{Name: "include_deleted", Schema: &client.Schema{Type: "boolean"}}},
				Responses: map[int]*client.Response{200: {Content: map[string]*client.MediaType{
					"application/json": {Schema: &client.Schema{Ref: "#/components/schemas/User"}}}}},
			},
			{
				Method: "POST", Path: "/users", OperationID: "users.create",
				RequestBody: &client.RequestBody{Required: true, Content: map[string]*client.MediaType{
					"application/json": {Schema: &client.Schema{Ref: "#/components/schemas/User"}}}},
				Responses: map[int]*client.Response{201: {Content: map[string]*client.MediaType{
					"application/json": {Schema: &client.Schema{Ref: "#/components/schemas/User"}}}}},
			},
		},
		Schemas: map[string]*client.Schema{"User": user},
	}
}

func baseConfig() client.GeneratorConfig {
	cfg := client.DefaultConfig()
	cfg.Language = "typescript"
	cfg.PackageName = "probe"

	return cfg
}

func gateFixtures() []gateFixture {
	oddKeys := baseSpec()
	oddKeys.Schemas["Weird"] = &client.Schema{Type: "object", Properties: map[string]*client.Schema{
		"content-type": {Type: "string"},
		"3dtiles":      {Type: "string"},
		"it's":         {Type: "string"},
		"back\\slash":  {Type: "string"},
	}}

	withAuth := baseSpec()
	withAuth.Security = []client.SecurityScheme{{Type: "http", Name: "bearerAuth", Scheme: "bearer"}}

	apiName := baseConfig()
	apiName.APIName = "APIClient"

	noStreaming := baseConfig()
	noStreaming.IncludeStreaming = false

	noAuthStreaming := baseConfig()
	noAuthStreaming.IncludeAuth = false

	wsSSE := baseSpec()
	wsSSE.WebSockets = []client.WebSocketEndpoint{
		{
			ID:      "chat",
			Path:    "/ws/chat",
			Summary: "Chat room WebSocket",
			SendSchema: &client.Schema{
				Ref: "#/components/schemas/User",
			},
			ReceiveSchema: &client.Schema{
				Ref: "#/components/schemas/User",
			},
		},
	}
	wsSSE.SSEs = []client.SSEEndpoint{
		{
			ID:      "notifications",
			Path:    "/sse/notifications",
			Summary: "Notification stream",
			EventSchemas: map[string]*client.Schema{
				"created": {Ref: "#/components/schemas/User"},
				"updated": {Ref: "#/components/schemas/User"},
			},
		},
	}

	return []gateFixture{
		{Name: "default", Spec: baseSpec(), Config: baseConfig()},
		{Name: "apiname", Spec: baseSpec(), Config: apiName},
		{Name: "odd-keys", Spec: oddKeys, Config: baseConfig()},
		{Name: "with-auth", Spec: withAuth, Config: baseConfig()},
		{Name: "no-streaming", Spec: baseSpec(), Config: noStreaming},
		{Name: "no-auth-streaming", Spec: baseSpec(), Config: noAuthStreaming},
		{Name: "ws-sse", Spec: wsSSE, Config: baseConfig()},
	}
}

func generateTo(t *testing.T, f gateFixture) string {
	t.Helper()

	out, err := NewGenerator().Generate(context.Background(), f.Spec, f.Config)
	if err != nil {
		t.Fatalf("%s: generate: %v", f.Name, err)
	}

	dir := t.TempDir()
	writeTree(t, dir, out.Files)

	return dir
}
