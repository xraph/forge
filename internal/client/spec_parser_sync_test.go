package client

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/xraph/forge/internal/router"
)

// parseRouterDocument runs a router's real OpenAPI document through
// SpecParser, the path a generated client takes.
func parseRouterDocument(t *testing.T, r router.Router) *APISpec {
	t.Helper()

	raw, err := json.Marshal(r.OpenAPISpec())
	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(t.TempDir(), "openapi.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}

	spec, err := NewSpecParser().ParseFile(context.Background(), path)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	return spec
}

// registerDatasetSyncRoutes mounts a dataset's four sync routes the way
// grove's extension does, on a path with a `:id` parameter.
func registerDatasetSyncRoutes(t *testing.T, r router.Router) {
	t.Helper()

	decl := func(opts ...router.SyncOption) router.RouteOption {
		return router.WithSync(router.SyncProtocolGroveCRDT, "DatasetRow", "",
			append([]router.SyncOption{router.SyncDataset("{id}")}, opts...)...)
	}
	ok := func(ctx router.Context) error { return nil }

	if err := r.POST("/datasets/:id/sync/pull", ok, router.WithName("grove.sync.pull"), decl(router.SyncRole(router.SyncRolePull))); err != nil {
		t.Fatal(err)
	}

	if err := r.POST("/datasets/:id/sync/push", ok, router.WithName("grove.sync.push"), decl(router.SyncRole(router.SyncRolePush))); err != nil {
		t.Fatal(err)
	}

	if err := r.EventStream("/datasets/:id/sync/stream", func(ctx router.Context, s router.Stream) error { return nil },
		router.WithName("grove.sync.stream"), decl()); err != nil {
		t.Fatal(err)
	}

	if err := r.WebSocket("/datasets/:id/sync/ws", func(ctx router.Context, c router.Connection) error { return nil },
		router.WithName("grove.sync.ws"), decl()); err != nil {
		t.Fatal(err)
	}
}

var datasetSyncRow = SyncDecl{
	Protocol: "grove-crdt", Entity: "DatasetRow", Table: "", Dataset: "{id}",
	Pull: "/datasets/{id}/sync/pull", Push: "/datasets/{id}/sync/push",
	Stream: "/datasets/{id}/sync/stream", Socket: "/datasets/{id}/sync/ws",
}

func TestSpecParserGroupsSyncRoutesByEntity(t *testing.T) {
	r := router.NewRouter(router.WithOpenAPI(router.OpenAPIConfig{Title: "Sync", Version: "1.0.0"}))
	registerDatasetSyncRoutes(t, r)

	spec := parseRouterDocument(t, r)

	if len(spec.Sync) != 1 || spec.Sync[0] != datasetSyncRow {
		t.Fatalf("Sync = %#v, want [%#v]", spec.Sync, datasetSyncRow)
	}
}

func TestSpecParserReadsAnArrayOfSyncDeclarations(t *testing.T) {
	r := router.NewRouter(router.WithOpenAPI(router.OpenAPIConfig{Title: "Sync", Version: "1.0.0"}))
	ok := func(ctx router.Context) error { return nil }
	both := func(role string) []router.RouteOption {
		return []router.RouteOption{
			router.WithSync(router.SyncProtocolGroveCRDT, "Document", "documents", router.SyncRole(role)),
			router.WithSync(router.SyncProtocolGroveCRDT, "Comment", "comments", router.SyncRole(role)),
		}
	}

	if err := r.POST("/sync/pull", ok, append(both(router.SyncRolePull), router.WithName("crdt.pull"))...); err != nil {
		t.Fatal(err)
	}

	if err := r.POST("/sync/push", ok, append(both(router.SyncRolePush), router.WithName("crdt.push"))...); err != nil {
		t.Fatal(err)
	}

	spec := parseRouterDocument(t, r)

	got := map[string]SyncDecl{}
	for _, d := range spec.Sync {
		got[d.Entity] = d
	}

	if len(got) != 2 || got["Document"].Table != "documents" || got["Comment"].Pull != "/sync/pull" || got["Comment"].Push != "/sync/push" {
		t.Fatalf("Sync = %#v", spec.Sync)
	}
}

// A router with no OpenAPI document is introspected from its raw route table,
// which records the path as registered (`/datasets/:id/...`), while the sync
// declaration's dataset is written `{id}`. Both builders must put the same
// placeholder style in the sync row, or a generated client requests a literal
// ":id".
func TestIntrospectorRawRoutesAgreeWithOpenAPIOnSyncRows(t *testing.T) {
	withDoc := router.NewRouter(router.WithOpenAPI(router.OpenAPIConfig{Title: "Sync", Version: "1.0.0"}))
	registerDatasetSyncRoutes(t, withDoc)

	raw := router.NewRouter()
	registerDatasetSyncRoutes(t, raw)

	if raw.OpenAPISpec() != nil {
		t.Fatal("the raw router has an OpenAPI document; the test would not reach the raw-route path")
	}

	fromDoc, err := NewIntrospector(withDoc).Introspect(context.Background())
	if err != nil {
		t.Fatalf("Introspect (OpenAPI): %v", err)
	}

	fromRoutes, err := NewIntrospector(raw).Introspect(context.Background())
	if err != nil {
		t.Fatalf("Introspect (raw routes): %v", err)
	}

	if !reflect.DeepEqual(fromDoc.Sync, []SyncDecl{datasetSyncRow}) {
		t.Errorf("OpenAPI Sync = %#v, want [%#v]", fromDoc.Sync, datasetSyncRow)
	}

	if !reflect.DeepEqual(fromRoutes.Sync, fromDoc.Sync) {
		t.Errorf("raw-route Sync = %#v, want the OpenAPI builder's %#v", fromRoutes.Sync, fromDoc.Sync)
	}
}
