package typescript

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/xraph/forge/internal/client"
)

func TestGateFixturesCoverKnownDefects(t *testing.T) {
	want := []string{"default", "apiname", "odd-keys", "with-auth", "no-streaming", "no-auth-streaming", "ws-sse", "no-auth-ws-sse"}

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
				// Two 2xx responses (200 with a body, 202 with none) so every gate
				// fixture — and the 12-run determinism test — exercises
				// generateReturnType's sorted-key, multi-status union path, not just
				// the single standalone unit test that covers it directly.
				Responses: map[int]*client.Response{
					200: {Content: map[string]*client.MediaType{
						"application/json": {Schema: &client.Schema{Ref: "#/components/schemas/User"}}}},
					202: {},
				},
			},
			{
				Method: "POST", Path: "/users", OperationID: "users.create",
				RequestBody: &client.RequestBody{Required: true, Content: map[string]*client.MediaType{
					"application/json": {Schema: &client.Schema{Ref: "#/components/schemas/User"}}}},
				Responses: map[int]*client.Response{201: {Content: map[string]*client.MediaType{
					"application/json": {Schema: &client.Schema{Ref: "#/components/schemas/User"}}}}},
			},
			// The next two endpoints declare exactly one 2xx response each, and it
			// always has content — neither contributes a `void` member to
			// generateReturnType's union. They exist so the 8-fixture tsc gate and
			// the 12-run determinism test cover the "no allowEmptyBody" runtime path
			// (round-1 fix regressed this: an empty text/plain or zero-byte binary
			// body was collapsing to `undefined` even when the spec never declared a
			// no-content response for that endpoint).
			{
				Method: "GET", Path: "/text", OperationID: "texts.get",
				Responses: map[int]*client.Response{200: {Content: map[string]*client.MediaType{
					"text/plain": {Schema: &client.Schema{Type: "string"}}}}},
			},
			{
				Method: "GET", Path: "/download", OperationID: "downloads.get",
				Responses: map[int]*client.Response{200: {Content: map[string]*client.MediaType{
					"application/octet-stream": {Schema: &client.Schema{Type: "string", Format: "binary"}}}}},
			},
			// Task 8 gate coverage: a request body beyond application/json. These
			// two endpoints (a multipart upload and a raw binary upload) exercise
			// hasBodyParam's generalisation and requestBodyParamType's FormData/Blob
			// mapping across every fixture in the corpus — the 8-fixture tsc gate
			// (TestGeneratedClientsTypeCheck) and the 12-run determinism test both
			// derive from baseSpec(), so adding them here is what makes those two
			// tests actually cover the defect this task fixed.
			{
				Method: "POST", Path: "/uploads", OperationID: "uploads.create",
				RequestBody: &client.RequestBody{Required: true, Content: map[string]*client.MediaType{
					"multipart/form-data": {Schema: &client.Schema{Type: "object"}}}},
				Responses: map[int]*client.Response{201: {Content: map[string]*client.MediaType{
					"application/json": {Schema: &client.Schema{Ref: "#/components/schemas/User"}}}}},
			},
			{
				Method: "POST", Path: "/raw", OperationID: "raw.create",
				RequestBody: &client.RequestBody{Required: true, Content: map[string]*client.MediaType{
					"application/octet-stream": {Schema: &client.Schema{Type: "string", Format: "binary"}}}},
				Responses: map[int]*client.Response{204: {Description: "ok"}},
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

	noAuthWsSSE := baseConfig()
	noAuthWsSSE.IncludeAuth = false

	return []gateFixture{
		{Name: "default", Spec: baseSpec(), Config: baseConfig()},
		{Name: "apiname", Spec: baseSpec(), Config: apiName},
		{Name: "odd-keys", Spec: oddKeys, Config: baseConfig()},
		{Name: "with-auth", Spec: withAuth, Config: baseConfig()},
		{Name: "no-streaming", Spec: baseSpec(), Config: noStreaming},
		{Name: "no-auth-streaming", Spec: baseSpec(), Config: noAuthStreaming},
		{Name: "ws-sse", Spec: wsSSESpec(), Config: baseConfig()},
		// Crosses the auth axis with the WS/SSE axis: neither "no-auth-streaming"
		// nor "ws-sse" alone exercises AuthConfig gating in websocket.go/sse.go,
		// which is exactly the gap that let AuthConfig ship ungated there.
		{Name: "no-auth-ws-sse", Spec: wsSSESpec(), Config: noAuthWsSSE},
		// Exercises allOf, which none of the fixtures above touch at all --
		// that gap is exactly what let a pure-allOf property compile to a
		// codec table entry with no `fields` (a tsc-breaking, decode()-
		// throwing shape) ship unnoticed. Covers a three-level $ref
		// inheritance chain (Outer -> Mid -> Leaf, an ordinary OpenAPI
		// pattern) and an inline (non-$ref) nested allOf member
		// (OuterInline), plus two members that declare the SAME wire field
		// with different nested shapes (Dup). A genuinely dangling $ref
		// member is deliberately NOT included here -- see
		// TestCodecTableAllOfEmptyCompositionDegradesToPassthrough and
		// TestCodecRuntimeAllOfChainDoesNotThrowAndIsWalked in
		// codecs_test.go, which cover it directly against the codec table
		// and the bundled runtime; schemaToTSType has no ref-existence
		// validation of its own (a pre-existing, orthogonal gap: the exact
		// same "Cannot find name" tsc error occurs for ANY dangling $ref,
		// allOf or not), so a fixture containing one could never pass this
		// gate's zero-tsc-errors bar regardless of the allOf fix.
		{Name: "allof", Spec: allOfSpec(), Config: baseConfig()},
	}
}

// allOfSpec returns baseSpec() plus the allOf shapes gateFixtures' "allof"
// fixture type-checks and TestCodecTableAllOfEmptyCompositionDegradesToPassthrough
// / TestCodecTableAllOfConflictingMembersWarnAndLastWins exercise directly.
func allOfSpec() *client.APISpec {
	spec := baseSpec()

	spec.Schemas["Leaf"] = &client.Schema{
		Type: "object",
		Properties: map[string]*client.Schema{
			"name": {Type: "string"},
			"tags": {Type: "array", Items: &client.Schema{Ref: "#/components/schemas/User"}},
		},
	}
	// Mid has no Properties of its own -- only AllOf -- so Outer, one level
	// down, cannot get its fields by looking at Mid's own Properties; it
	// must flatten through Mid to Leaf.
	spec.Schemas["Mid"] = &client.Schema{AllOf: []*client.Schema{{Ref: "#/components/schemas/Leaf"}}}
	spec.Schemas["Outer"] = &client.Schema{AllOf: []*client.Schema{{Ref: "#/components/schemas/Mid"}}}
	// Same shape as Mid->Outer, but the intermediate composition is INLINE
	// (no $ref) rather than a named schema.
	spec.Schemas["OuterInline"] = &client.Schema{
		AllOf: []*client.Schema{
			{AllOf: []*client.Schema{{Ref: "#/components/schemas/Leaf"}}},
		},
	}

	spec.Schemas["PayloadA"] = &client.Schema{Type: "object", Properties: map[string]*client.Schema{"x": {Type: "string"}}}
	spec.Schemas["PayloadB"] = &client.Schema{Type: "object", Properties: map[string]*client.Schema{"y": {Type: "string"}}}
	spec.Schemas["MemberA"] = &client.Schema{
		Type: "object", Required: []string{"payload"},
		Properties: map[string]*client.Schema{"payload": {Ref: "#/components/schemas/PayloadA"}},
	}
	spec.Schemas["MemberB"] = &client.Schema{
		Type: "object", Required: []string{"payload"},
		Properties: map[string]*client.Schema{"payload": {Ref: "#/components/schemas/PayloadB"}},
	}
	spec.Schemas["Dup"] = &client.Schema{
		AllOf: []*client.Schema{
			{Ref: "#/components/schemas/MemberA"},
			{Ref: "#/components/schemas/MemberB"},
		},
	}

	return spec
}

// wsSSESpec returns a fresh spec with a WebSocket and an SSE endpoint, built
// from baseSpec(). Called once per fixture that needs it so fixtures never
// share (and risk mutating) the same *client.APISpec.
func wsSSESpec() *client.APISpec {
	spec := baseSpec()
	spec.WebSockets = []client.WebSocketEndpoint{
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
	spec.SSEs = []client.SSEEndpoint{
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

	return spec
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
