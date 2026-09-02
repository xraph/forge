package client

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/xraph/forge/internal/router"
)

// These tests run the whole identity path a real application takes: a Go type,
// through the router's schema generator (where `forge:"id"` and ForgeEntity are
// honoured), out as an OpenAPI document, back in through SpecParser, and into
// InferEntity. Nothing is hand-built -- the point is to catch the steps that
// only exist between the two packages, notably whether x-forge-id survives
// being marshalled into a spec file and read back out.

// entityMarkedByTag declares identity with the struct tag.
type entityMarkedByTag struct {
	OrderNumber string `forge:"id"   json:"order_number"`
	ID          string `json:"id"`
	Total       int    `json:"total"`
}

// entityMarkedByInterface declares identity with a ForgeEntity method, on a
// type that also has a property named `id`. This is the case the docs promise
// and the case that previously resolved to nothing.
type entityMarkedByInterface struct {
	UUID  string `json:"uuid"`
	ID    string `json:"id"`
	Total int    `json:"total"`
}

func (entityMarkedByInterface) ForgeEntity() router.EntityDef {
	return router.EntityDef{Type: "entityMarkedByInterface", IDField: "uuid"}
}

// entityWithContradictoryDeclarations marks one field with the struct tag and
// names a DIFFERENT field from ForgeEntity.
//
// Both mechanisms write x-forge-id, so the generated schema carries two
// explicit identity declarations that disagree. The decision, asserted below
// rather than left to fall out: this resolves to NOTHING. The developer stated
// twice, deliberately, that two different fields are the one identity; there is
// no heuristic left to break the tie that would not be silently overruling one
// of them, and picking wrong keys two records to a single cache entry.
type entityWithContradictoryDeclarations struct {
	OrderNumber string `forge:"id"   json:"order_number"`
	UUID        string `json:"uuid"`
	Total       int    `json:"total"`
}

func (entityWithContradictoryDeclarations) ForgeEntity() router.EntityDef {
	return router.EntityDef{Type: "entityWithContradictoryDeclarations", IDField: "uuid"}
}

// inferEntityForGoType runs the full pipeline for one Go type and returns what
// the client generator would conclude about its identity.
func inferEntityForGoType(t *testing.T, name string, typ reflect.Type) *EntityRef {
	t.Helper()

	schema := router.GetSchemaFromType(typ)
	if schema == nil {
		t.Fatalf("router produced no schema for %s", name)
	}

	encoded, err := json.Marshal(schema)
	if err != nil {
		t.Fatalf("marshal schema: %v", err)
	}

	doc := fmt.Sprintf(`{
  "openapi": "3.0.3",
  "info": { "title": "Identity", "version": "1.0.0" },
  "paths": {},
  "components": { "schemas": { %q: %s } }
}`, name, encoded)

	path := filepath.Join(t.TempDir(), "openapi.json")
	if err := os.WriteFile(path, []byte(doc), 0o600); err != nil {
		t.Fatalf("write spec: %v", err)
	}

	spec, err := NewSpecParser().ParseFile(context.Background(), path)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	parsed, ok := spec.Schemas[name]
	if !ok {
		t.Fatalf("%s missing from parsed schemas", name)
	}

	return InferEntity(spec, name, parsed)
}

func TestForgeIDTagBeatsAPropertyNamedIDEndToEnd(t *testing.T) {
	got := inferEntityForGoType(t, "Order", reflect.TypeFor[entityMarkedByTag]())

	if got == nil {
		t.Fatal("a type tagged forge:\"id\" alongside an `id` property resolved to no entity")
	}

	if got.IDField != "order_number" {
		t.Fatalf("IDField = %q, want order_number (the tagged field, not the one named id)", got.IDField)
	}
}

func TestForgeEntityBeatsAPropertyNamedIDEndToEnd(t *testing.T) {
	got := inferEntityForGoType(t, "Account", reflect.TypeFor[entityMarkedByInterface]())

	if got == nil {
		t.Fatal("a ForgeEntity type with an `id` property resolved to no entity; " +
			"this is the exact case the interface is documented to handle")
	}

	if got.IDField != "uuid" {
		t.Fatalf("IDField = %q, want uuid (the declared field, not the one named id)", got.IDField)
	}
}

func TestContradictoryIdentityDeclarationsResolveToNothing(t *testing.T) {
	typ := reflect.TypeFor[entityWithContradictoryDeclarations]()

	// First establish the premise: both mechanisms really did mark a field, so
	// this test fails for the right reason rather than because one of them
	// quietly stopped working.
	schema := router.GetSchemaFromType(typ)
	for _, prop := range []string{"order_number", "uuid"} {
		if v, _ := schema.Properties[prop].Extensions["x-forge-id"].(bool); !v {
			t.Fatalf("%s is not marked x-forge-id; this test no longer covers the"+
				" contradictory-declaration case", prop)
		}
	}

	if got := inferEntityForGoType(t, "Contradictory", typ); got != nil {
		t.Fatalf("two contradictory identity declarations resolved to %+v; want nil,"+
			" since choosing between them silently overrules a deliberate declaration", got)
	}
}
