package typescript

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/xraph/forge/internal/client"
	"github.com/xraph/forge/internal/client/generators"
	"github.com/xraph/forge/internal/router"
	"github.com/xraph/forge/internal/shared"
)

// gatewayStreamDoc is the shape xraph/forge#106 reported: an OpenAPI document
// with no AsyncAPI beside it, where a log stream is an ordinary-looking GET.
// Written by something other than forge's router, so nothing carries
// x-forge-protocol; the response's media type is the only evidence.
const gatewayStreamDoc = `{
  "openapi": "3.0.3",
  "info": { "title": "Portal", "version": "1.0.0" },
  "paths": {
    "/admin/instances/{instanceId}": {
      "get": {
        "operationId": "getAdminInstance",
        "parameters": [{ "name": "instanceId", "in": "path", "required": true, "schema": { "type": "string" } }],
        "responses": { "200": { "description": "ok", "content": { "application/json": {
          "schema": { "type": "object", "properties": { "id": { "type": "string" } } } } } } }
      }
    },
    "/admin/instances/{instanceId}/logs/stream": {
      "get": {
        "operationId": "streamAdminInstanceLogs",
        "parameters": [{ "name": "instanceId", "in": "path", "required": true, "schema": { "type": "string" } }],
        "responses": { "200": { "description": "log lines", "content": { "text/event-stream": {
          "schema": { "type": "string" } } } } }
      }
    },
    "/admin/events/stream": {
      "get": {
        "operationId": "streamAdminEvents",
        "responses": { "200": { "description": "events", "content": { "text/event-stream": {
          "schema": { "type": "string" } } } } }
      }
    }
  }
}`

// generateThroughEntryPoint drives client.Generator.Generate, the function the
// CLI calls, rather than the language generator directly: separating streams
// from requests happens there.
func generateThroughEntryPoint(t *testing.T, spec *client.APISpec, hooks bool) map[string]string {
	t.Helper()

	return generateWithWarnings(t, spec, hooks).Files
}

func generateWithWarnings(t *testing.T, spec *client.APISpec, hooks bool) *generators.GeneratedClient {
	t.Helper()

	gen := client.NewGenerator()
	if err := gen.Register(NewGenerator()); err != nil {
		t.Fatalf("Register: %v", err)
	}

	cfg := baseConfig()
	cfg.Hooks = hooks

	out, err := gen.Generate(context.Background(), spec, cfg)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	return out
}

func TestStreamEndpointIsNotBoundAsAQuery(t *testing.T) {
	path := writeSpecFile(t, "openapi.json", gatewayStreamDoc)

	parse := func() *client.APISpec {
		spec, err := client.NewSpecParser().ParseFile(context.Background(), path)
		if err != nil {
			t.Fatalf("ParseFile: %v", err)
		}

		return spec
	}

	out := generateWithWarnings(t, parse(), true)
	files := out.Files
	ops, hooks := ClientManifestText(files), ClientHooksText(files)

	if !strings.Contains(hooks, "useGetAdminInstance") {
		t.Fatalf("the ordinary GET lost its hook; hooks:\n%s", hooks)
	}

	for name, text := range map[string]string{"ops": ops, "hooks": hooks} {
		if strings.Contains(text, "/logs/stream") || strings.Contains(text, "/events/stream") ||
			strings.Contains(strings.ToLower(text), "streamadmin") {
			t.Errorf("%s still binds a stream as a request:\n%s", name, text)
		}
	}

	all := joinFiles(files)

	if !strings.Contains(all, "StreamAdminEventsSSEClient") {
		t.Errorf("the stream only OpenAPI describes has no SSE client; files: %v", fileNames(files))
	}

	// The stream clients put a channel's path on the wire as written, so a
	// client for the parameterized stream would connect to a literal
	// `{instanceId}`. It is left out, and the generator says so.
	if strings.Contains(all, "StreamAdminInstanceLogsSSEClient") {
		t.Error("a client was generated for a parameterized stream it cannot address")
	}

	if !slices.ContainsFunc(out.Warnings, func(w string) bool {
		return strings.Contains(w, "GET /admin/instances/{instanceId}/logs/stream")
	}) {
		t.Errorf("no warning names the parameterized stream; warnings: %v", out.Warnings)
	}

	// The stream client built from nothing but an operation has to compile.
	// Without hooks, because they import @forge-go/client-core, which the
	// type-check sandbox does not install.
	dir := t.TempDir()
	writeTree(t, dir, generateThroughEntryPoint(t, parse(), false))

	if errs := typeCheck(t, dir); len(errs) > 0 {
		t.Errorf("%d type error(s):\n%s", len(errs), strings.Join(errs, "\n"))
	}
}

type streamE2ERequest struct{}

type streamE2EOrder struct {
	ID string `json:"id"`
}

// The router writes an SSE route into both documents. Merged, the stream is
// described once, by its channel, and no request binding is left over.
// Unparameterized, so the AsyncAPI channel and the OpenAPI operation are the
// only two descriptions in play.
func TestRouterStreamRouteIsDescribedOnceAcrossBothDocuments(t *testing.T) {
	r := router.NewRouter(
		router.WithOpenAPI(router.OpenAPIConfig{Title: "Orders", Version: "1.0.0"}),
		router.WithAsyncAPI(router.AsyncAPIConfig{Title: "Orders", Version: "1.0.0"}),
	)

	if err := r.GET("/orders/:id", func(ctx shared.Context, req *streamE2ERequest) (*streamE2EOrder, error) {
		return &streamE2EOrder{}, nil
	}); err != nil {
		t.Fatal(err)
	}

	if err := r.SSE("/orders/stream", func(ctx router.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}

	openAPI, err := json.Marshal(r.OpenAPISpec())
	if err != nil {
		t.Fatal(err)
	}

	asyncAPI, err := json.Marshal(r.AsyncAPISpec())
	if err != nil {
		t.Fatal(err)
	}

	parser := client.NewSpecParser()

	var specs []*client.APISpec

	for name, doc := range map[string][]byte{"openapi.json": openAPI, "asyncapi.json": asyncAPI} {
		spec, err := parser.ParseFileUnresolved(context.Background(), writeSpecFile(t, name, string(doc)))
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}

		specs = append(specs, spec)
	}

	merged := client.MergeSpecs(specs...)
	client.ResolveEntityFields(merged)

	files := generateThroughEntryPoint(t, merged, true)

	if hooks := ClientHooksText(files); strings.Contains(hooks, "/stream") ||
		strings.Count(hooks, "= query") != 1 {
		t.Errorf("want exactly one query binding, for GET /orders/{id}; hooks:\n%s", hooks)
	}

	if ops := ClientManifestText(files); strings.Contains(ops, "/stream'") {
		t.Errorf("ops.ts still carries the stream as an operation:\n%s", ops)
	}

	if got := strings.Count(joinFiles(files), "SSEClient extends EventEmitter {"); got != 1 {
		t.Errorf("want one SSE client class, got %d", got)
	}
}
