package client

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/xraph/forge/internal/shared"
)

func syncExt(role, path string) (string, map[string]any) {
	ext := map[string]any{
		"x-forge-sync": map[string]any{
			"protocol": "grove-crdt",
			"entity":   "Document",
			"table":    "documents",
			"dataset":  "{id}",
			"role":     role,
		},
	}

	return path, ext
}

func TestResolveEndpointIdempotent(t *testing.T) {
	cases := []struct {
		name     string
		ext      map[string]any
		want     bool
		warnings int
	}{
		{"declared true", map[string]any{"x-forge-idempotent": true}, true, 0},
		{"declared false", map[string]any{"x-forge-idempotent": false}, false, 0},
		{"absent", map[string]any{}, false, 0},
		{"a string is not a bool", map[string]any{"x-forge-idempotent": "true"}, false, 1},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			spec := &APISpec{}
			ep := &Endpoint{Method: "POST", Path: "/orders"}

			resolveEndpointIdempotent(spec, ep, c.ext)

			if ep.Idempotent != c.want {
				t.Errorf("Idempotent = %v, want %v", ep.Idempotent, c.want)
			}

			if len(spec.Warnings) != c.warnings {
				t.Errorf("warnings = %v, want %d", spec.Warnings, c.warnings)
			}
		})
	}
}

func TestCollectSyncRouteGroupsRolesByEntity(t *testing.T) {
	spec := &APISpec{}

	for _, role := range []string{"push", "pull", "socket", "stream"} {
		path, ext := syncExt(role, "/datasets/{id}/sync/"+role)
		collectSyncRoute(spec, "route "+path, path, "", ext)
	}

	other := map[string]any{"x-forge-sync": map[string]any{
		"protocol": "grove-crdt", "entity": "Annotation", "table": "annotations", "role": "pull",
	}}
	collectSyncRoute(spec, "route /notes/pull", "/notes/pull", "", other)

	want := []SyncDecl{
		{Protocol: "grove-crdt", Entity: "Annotation", Table: "annotations", Pull: "/notes/pull"},
		{
			Protocol: "grove-crdt", Entity: "Document", Table: "documents", Dataset: "{id}",
			Pull: "/datasets/{id}/sync/pull", Push: "/datasets/{id}/sync/push",
			Stream: "/datasets/{id}/sync/stream", Socket: "/datasets/{id}/sync/socket",
		},
	}

	if !reflect.DeepEqual(spec.Sync, want) {
		t.Fatalf("Sync =\n%+v\nwant\n%+v", spec.Sync, want)
	}

	if len(spec.Warnings) != 0 {
		t.Errorf("unexpected warnings: %v", spec.Warnings)
	}
}

func TestCollectSyncRouteTakesStreamAndSocketRolesFromTheRouteKind(t *testing.T) {
	spec := &APISpec{}
	decl := map[string]any{"x-forge-sync": map[string]any{"protocol": "grove-crdt", "entity": "Document", "dataset": "{id}"}}

	collectSyncRoute(spec, "channel /d/{id}/sync/stream", "/d/{id}/sync/stream", "stream", decl)
	collectSyncRoute(spec, "channel /d/{id}/sync/ws", "/d/{id}/sync/ws", "socket", decl)

	if len(spec.Sync) != 1 || spec.Sync[0].Stream != "/d/{id}/sync/stream" || spec.Sync[0].Socket != "/d/{id}/sync/ws" {
		t.Fatalf("Sync = %+v, want stream and socket taken from the route kind", spec.Sync)
	}

	if len(spec.Warnings) != 0 {
		t.Errorf("unexpected warnings: %v", spec.Warnings)
	}
}

// One Grove route serves several tables, so the extension may be a list.
func TestCollectSyncRouteAcceptsAListOfDeclarations(t *testing.T) {
	spec := &APISpec{}

	collectSyncRoute(spec, "GET /sync/pull", "/sync/pull", "", map[string]any{"x-forge-sync": []any{
		map[string]any{"protocol": "grove-crdt", "entity": "Document", "table": "documents", "role": "pull"},
		map[string]any{"protocol": "grove-crdt", "entity": "Annotation", "table": "annotations", "role": "pull"},
	}})

	if len(spec.Sync) != 2 || spec.Sync[0].Entity != "Annotation" || spec.Sync[1].Entity != "Document" ||
		spec.Sync[0].Pull != "/sync/pull" || spec.Sync[1].Pull != "/sync/pull" {
		t.Fatalf("Sync = %+v, want one row per listed entity, both pulling from /sync/pull", spec.Sync)
	}
}

// An absent table means one table per dataset, chosen at runtime.
func TestCollectSyncRouteAllowsAMissingTable(t *testing.T) {
	spec := &APISpec{}

	collectSyncRoute(spec, "GET /d/{id}/pull", "/d/{id}/pull", "", map[string]any{"x-forge-sync": map[string]any{
		"protocol": "grove-crdt", "entity": "DatasetRow", "dataset": "{id}", "role": "pull",
	}})

	if len(spec.Sync) != 1 || spec.Sync[0].Table != "" || spec.Sync[0].Pull != "/d/{id}/pull" {
		t.Fatalf("Sync = %+v, want a row with no table", spec.Sync)
	}

	if len(spec.Warnings) != 0 {
		t.Errorf("a missing table is legal, got warnings %v", spec.Warnings)
	}
}

func TestCollectSyncRouteReportsConflictingTables(t *testing.T) {
	spec := &APISpec{}

	path, ext := syncExt("pull", "/datasets/{id}/sync/pull")
	collectSyncRoute(spec, "route "+path, path, "", ext)
	collectSyncRoute(spec, "route /other/push", "/other/push", "", map[string]any{"x-forge-sync": map[string]any{
		"protocol": "grove-crdt", "entity": "Document", "table": "docs", "role": "push",
	}})

	if spec.Sync[0].Table != "documents" {
		t.Errorf("Table = %q, want the first declaration kept", spec.Sync[0].Table)
	}

	if spec.Sync[0].Push != "/other/push" {
		t.Errorf("Push = %q, want the non-conflicting role merged", spec.Sync[0].Push)
	}

	if len(spec.Warnings) != 1 || !strings.Contains(spec.Warnings[0], `"docs"`) {
		t.Errorf("warnings = %v, want one naming the conflicting table", spec.Warnings)
	}
}

// TestOfflineMetaParityBetweenIRBuilders drives the live-router builder and
// the file builder over the same two operations and asserts they agree on
// Idempotent and Sync, for the reason TestAuthzParityBetweenIRBuilders exists.
func TestOfflineMetaParityBetweenIRBuilders(t *testing.T) {
	pullPath, pullExt := syncExt("pull", "/datasets/{id}/sync/pull")
	pushPath, pushExt := syncExt("push", "/datasets/{id}/sync/push")
	pushExt["x-forge-idempotent"] = true

	op := func(id string, ext map[string]any) *shared.Operation {
		return &shared.Operation{
			OperationID: id,
			Responses:   map[string]*shared.Response{"200": {Description: "ok"}},
			Extensions:  ext,
		}
	}

	live := &APISpec{Schemas: map[string]*Schema{}, Security: []SecurityScheme{}}
	err := (&Introspector{}).extractFromOpenAPI(live, &shared.OpenAPISpec{
		OpenAPI: "3.0.0",
		Info:    shared.Info{Title: "Sync Parity", Version: "1.0.0"},
		Paths: map[string]*shared.PathItem{
			pullPath: {Get: op("syncPull", pullExt)},
			pushPath: {Post: op("syncPush", pushExt)},
		},
	})
	if err != nil {
		t.Fatalf("extractFromOpenAPI: %v", err)
	}

	yamlOp := func(id string, ext map[string]any) map[string]any {
		out := map[string]any{"operationId": id, "responses": map[string]any{"200": map[string]any{"description": "ok"}}}
		for k, v := range ext {
			out[k] = v
		}

		return out
	}

	file, err := NewSpecParser().ParseFile(context.Background(), writeYAMLSpec(t, "openapi.yaml", map[string]any{
		"openapi": "3.0.0",
		"info":    map[string]any{"title": "Sync Parity", "version": "1.0.0"},
		"paths": map[string]any{
			pullPath: map[string]any{"get": yamlOp("syncPull", pullExt)},
			pushPath: map[string]any{"post": yamlOp("syncPush", pushExt)},
		},
	}))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	if !reflect.DeepEqual(live.Sync, file.Sync) {
		t.Errorf("Sync differs:\nlive %+v\nfile %+v", live.Sync, file.Sync)
	}

	if len(live.Sync) != 1 || live.Sync[0].Pull != pullPath || live.Sync[0].Push != pushPath {
		t.Errorf("Sync = %+v, want one Document row with both paths", live.Sync)
	}

	idempotent := func(spec *APISpec) map[string]bool {
		out := map[string]bool{}
		for _, ep := range spec.Endpoints {
			out[ep.OperationID] = ep.Idempotent
		}

		return out
	}

	want := map[string]bool{"syncPull": false, "syncPush": true}
	if got := idempotent(live); !reflect.DeepEqual(got, want) {
		t.Errorf("introspector Idempotent = %v, want %v", got, want)
	}

	if got := idempotent(file); !reflect.DeepEqual(got, want) {
		t.Errorf("spec parser Idempotent = %v, want %v", got, want)
	}
}

func TestMergeSpecsMergesSyncRows(t *testing.T) {
	a := &APISpec{Kind: SourceOpenAPI, Sync: []SyncDecl{{Protocol: "grove-crdt", Entity: "Document", Table: "documents", Pull: "/d/pull"}}}
	b := &APISpec{Kind: SourceAsyncAPI, Sync: []SyncDecl{{Protocol: "grove-crdt", Entity: "Document", Table: "documents", Socket: "/d/ws"}}}

	out := MergeSpecs(a, b)

	if len(out.Sync) != 1 || out.Sync[0].Pull != "/d/pull" || out.Sync[0].Socket != "/d/ws" {
		t.Fatalf("Sync = %+v, want one row carrying both sources' paths", out.Sync)
	}
}

func TestApplyDropsFilteredSyncPaths(t *testing.T) {
	spec := &APISpec{
		Endpoints: []Endpoint{{Method: "GET", Path: "/keep/pull"}, {Method: "GET", Path: "/drop/pull"}},
		Sync: []SyncDecl{
			{Protocol: "grove-crdt", Entity: "Dropped", Table: "dropped", Pull: "/drop/pull"},
			{Protocol: "grove-crdt", Entity: "Kept", Table: "kept", Pull: "/keep/pull", Socket: "/drop/ws"},
		},
	}

	spec.Apply(PathFilter{Include: []string{"/keep/**"}})

	want := []SyncDecl{{Protocol: "grove-crdt", Entity: "Kept", Table: "kept", Pull: "/keep/pull"}}
	if !reflect.DeepEqual(spec.Sync, want) {
		t.Fatalf("Sync = %+v, want %+v", spec.Sync, want)
	}
}

func TestStripPrefixRenamesSyncEntities(t *testing.T) {
	spec := &APISpec{
		Schemas: map[string]*Schema{"Studio_Document": {Type: "object"}},
		Sync:    []SyncDecl{{Protocol: "grove-crdt", Entity: "Studio_Document", Table: "documents", Pull: "/p"}},
	}

	if err := StripPrefix(spec, []string{"Studio_"}, nil); err != nil {
		t.Fatal(err)
	}

	if spec.Sync[0].Entity != "Document" {
		t.Errorf("Entity = %q, want Document", spec.Sync[0].Entity)
	}
}

func TestCollectSyncRouteWarnsOnAnUnusableRole(t *testing.T) {
	cases := map[string]map[string]any{
		"http route with no role": {"protocol": "grove-crdt", "entity": "Document"},
		"unknown role":            {"protocol": "grove-crdt", "entity": "Document", "role": "mirror"},
		"missing entity":          {"protocol": "grove-crdt", "role": "pull"},
		"missing protocol":        {"entity": "Document", "role": "pull"},
		"role must be a string":   {"protocol": "grove-crdt", "entity": "Document", "role": 3},
	}

	for name, entry := range cases {
		t.Run(name, func(t *testing.T) {
			spec := &APISpec{}

			collectSyncRoute(spec, "GET /d/pull", "/d/pull", "", map[string]any{"x-forge-sync": entry})

			if len(spec.Sync) != 0 {
				t.Errorf("Sync = %+v, want the row dropped", spec.Sync)
			}

			if len(spec.Warnings) != 1 || !strings.Contains(spec.Warnings[0], "GET /d/pull") ||
				!strings.Contains(spec.Warnings[0], "x-forge-sync") {
				t.Errorf("warnings = %v, want one naming the route and the extension", spec.Warnings)
			}
		})
	}
}

func TestCollectSyncRouteWarnsOnAMalformedValue(t *testing.T) {
	for name, raw := range map[string]any{
		"a string":             "grove-crdt",
		"a list with a scalar": []any{"grove-crdt"},
	} {
		t.Run(name, func(t *testing.T) {
			spec := &APISpec{}

			collectSyncRoute(spec, "GET /d/pull", "/d/pull", "", map[string]any{"x-forge-sync": raw})

			if len(spec.Sync) != 0 || len(spec.Warnings) != 1 {
				t.Errorf("Sync = %+v, warnings = %v, want no rows and one warning", spec.Sync, spec.Warnings)
			}
		})
	}
}

func TestResolveEntityFieldsGivesASyncOnlyEntityAnEntitiesRow(t *testing.T) {
	spec := &APISpec{
		Schemas: map[string]*Schema{
			"Document": {Type: "object", Properties: map[string]*Schema{"id": {Type: "string"}, "title": {Type: "string"}}},
		},
		Sync: []SyncDecl{
			{Protocol: "grove-crdt", Entity: "Document", Table: "documents", Pull: "/d/pull"},
			{Protocol: "grove-crdt", Entity: "Keyless", Table: "keyless", Pull: "/k/pull"},
		},
	}

	resolveEntityFields(spec)

	ref := spec.Entities["Document"]
	if ref == nil || ref.IDField != "id" {
		t.Fatalf("Entities[Document] = %+v, want a row keyed by id", ref)
	}

	if spec.Entities["Keyless"] != nil {
		t.Errorf("Entities[Keyless] = %+v, want none: no schema gives it an identity", spec.Entities["Keyless"])
	}

	if len(spec.Warnings) != 1 || !strings.Contains(spec.Warnings[0], `"Keyless"`) {
		t.Errorf("warnings = %v, want one naming Keyless", spec.Warnings)
	}
}

// A socket channel and an SSE channel carry x-forge-sync, and both IR builders
// must read the role from the channel's kind. Channels with messages are read
// as sockets and channels without as SSE, in both builders.
func TestOfflineSyncChannelParityBetweenIRBuilders(t *testing.T) {
	sync := func(entity string) map[string]any {
		return map[string]any{"protocol": "grove-crdt", "entity": entity, "table": "documents", "dataset": "{id}"}
	}

	wsExt := map[string]any{"x-forge-sync": sync("Document")}
	sseExt := map[string]any{"x-forge-sync": sync("Document")}

	live := &APISpec{Schemas: map[string]*Schema{}}
	err := (&Introspector{}).extractFromAsyncAPI(live, &shared.AsyncAPISpec{
		AsyncAPI: "3.0.0",
		Info:     shared.AsyncAPIInfo{Title: "Sync Channels", Version: "1.0.0"},
		Channels: map[string]*shared.AsyncAPIChannel{
			"ws": {
				Address:    "/d/{id}/sync/ws",
				Messages:   map[string]*shared.AsyncAPIMessage{"frame": {Payload: &shared.Schema{Type: "object"}}},
				Extensions: wsExt,
			},
			"sse": {Address: "/d/{id}/sync/stream", Extensions: sseExt},
		},
		Operations: map[string]*shared.AsyncAPIOperation{
			"syncSocket": {Action: "receive", Channel: &shared.AsyncAPIChannelReference{Ref: "#/channels/ws"}},
			"syncStream": {Action: "receive", Channel: &shared.AsyncAPIChannelReference{Ref: "#/channels/sse"}},
		},
	})
	if err != nil {
		t.Fatalf("extractFromAsyncAPI: %v", err)
	}

	file, err := NewSpecParser().ParseFile(context.Background(), writeYAMLSpec(t, "asyncapi.yaml", map[string]any{
		"asyncapi": "3.0.0",
		"info":     map[string]any{"title": "Sync Channels", "version": "1.0.0"},
		"channels": map[string]any{
			"ws": map[string]any{
				"address":      "/d/{id}/sync/ws",
				"messages":     map[string]any{"frame": map[string]any{"payload": map[string]any{"type": "object"}}},
				"x-forge-sync": sync("Document"),
			},
			"sse": map[string]any{"address": "/d/{id}/sync/stream", "x-forge-sync": sync("Document")},
		},
		"operations": map[string]any{
			"syncSocket": map[string]any{"action": "receive", "channel": map[string]any{"$ref": "#/channels/ws"}},
			"syncStream": map[string]any{"action": "receive", "channel": map[string]any{"$ref": "#/channels/sse"}},
		},
	}))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	want := []SyncDecl{{
		Protocol: "grove-crdt", Entity: "Document", Table: "documents", Dataset: "{id}",
		Stream: "/d/{id}/sync/stream", Socket: "/d/{id}/sync/ws",
	}}

	if !reflect.DeepEqual(live.Sync, want) {
		t.Errorf("introspector Sync = %+v, want %+v", live.Sync, want)
	}

	if !reflect.DeepEqual(file.Sync, want) {
		t.Errorf("spec parser Sync = %+v, want %+v", file.Sync, want)
	}
}
