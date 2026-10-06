package router

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestWithSyncStoresDeclarations(t *testing.T) {
	cfg := applyOpts(
		WithSync(SyncProtocolGroveCRDT, "Document", "documents", SyncRole(SyncRolePull)),
		WithSync(SyncProtocolGroveCRDT, "Comment", "comments", SyncRole(SyncRolePull)),
	)

	defs, _ := cfg.Metadata["forge.client.sync"].([]SyncDef)
	if len(defs) != 2 || defs[0].Entity != "Document" || defs[1].Table != "comments" {
		t.Fatalf("defs = %#v", defs)
	}
}

func TestSyncExtensionIsAnObjectForOneDeclaration(t *testing.T) {
	ext := ClientExtensions("POST", map[string]any{"forge.client.sync": []SyncDef{{
		Protocol: SyncProtocolGroveCRDT, Entity: "DatasetRow", Table: "", Dataset: "{id}", Role: SyncRolePush,
	}}})

	want := map[string]any{"protocol": "grove-crdt", "entity": "DatasetRow", "dataset": "{id}", "role": "push"}
	if !reflect.DeepEqual(ext["x-forge-sync"], want) {
		t.Fatalf("x-forge-sync = %#v, want %#v", ext["x-forge-sync"], want)
	}
}

func TestSyncExtensionIsAnArrayForSeveralDeclarations(t *testing.T) {
	ext := ClientExtensions("POST", map[string]any{"forge.client.sync": []SyncDef{
		{Protocol: SyncProtocolGroveCRDT, Entity: "Document", Table: "documents", Role: SyncRolePull},
		{Protocol: SyncProtocolGroveCRDT, Entity: "Comment", Table: "comments", Role: SyncRolePull},
	}})

	list, ok := ext["x-forge-sync"].([]map[string]any)
	if !ok || len(list) != 2 || list[1]["entity"] != "Comment" {
		t.Fatalf("x-forge-sync = %#v", ext["x-forge-sync"])
	}

	if _, has := list[0]["dataset"]; has {
		t.Fatalf("dataset emitted for a declaration without one: %#v", list[0])
	}
}

func TestSyncDeclarationWithoutRoleIsNotEmitted(t *testing.T) {
	ext := ClientExtensions("POST", map[string]any{"forge.client.sync": []SyncDef{{Protocol: SyncProtocolGroveCRDT, Entity: "Document", Table: "documents"}}})
	if _, ok := ext["x-forge-sync"]; ok {
		t.Fatalf("x-forge-sync emitted without a role: %#v", ext)
	}
}

func TestStreamingRoutesInferTheirRole(t *testing.T) {
	r := NewRouter(WithOpenAPI(OpenAPIConfig{Title: "Sync", Version: "1.0.0"}))

	if err := r.EventStream("/sync/stream", func(ctx Context, s Stream) error { return nil },
		WithSync(SyncProtocolGroveCRDT, "Document", "documents")); err != nil {
		t.Fatal(err)
	}

	if err := r.WebSocket("/sync/ws", func(ctx Context, c Connection) error { return nil },
		WithSync(SyncProtocolGroveCRDT, "Document", "documents")); err != nil {
		t.Fatal(err)
	}

	if err := r.POST("/sync/pull", func(ctx Context) error { return nil },
		WithSync(SyncProtocolGroveCRDT, "Document", "documents", SyncRole(SyncRolePull))); err != nil {
		t.Fatal(err)
	}

	raw, err := json.Marshal(r.OpenAPISpec())
	if err != nil {
		t.Fatal(err)
	}

	var doc struct {
		Paths map[string]map[string]map[string]any `json:"paths"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}

	for path, role := range map[string]string{"/sync/stream": "stream", "/sync/ws": "socket"} {
		ext, _ := doc.Paths[path]["get"]["x-forge-sync"].(map[string]any)
		if ext["role"] != role {
			t.Fatalf("%s role = %#v, want %s (extension %#v)", path, ext["role"], role, ext)
		}
	}

	if ext, _ := doc.Paths["/sync/pull"]["post"]["x-forge-sync"].(map[string]any); ext["role"] != "pull" {
		t.Fatalf("pull extension = %#v", ext)
	}
}
