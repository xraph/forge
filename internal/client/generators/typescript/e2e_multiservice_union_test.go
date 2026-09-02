package typescript

import (
	"context"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/xraph/forge/internal/client"
)

// This file guards the two ways the `entities` table goes wrong that no single
// generated file shows.
//
// Both defects it covers were found in shipped output and neither is visible in
// the file it lives in. One client's entities.ts is well-formed and internally
// consistent whether or not another client contradicts it, and an empty table
// is a syntactically perfect four-line module. They only appear when the tables
// are combined or counted, which is what the consumer does and what no test did.
//
// So the assertions here are deliberately not "this file contains this line".
// They are properties of the SET of clients a gateway produces:
//
//   - No typename is declared with two different field shapes once the tables
//     are unioned, because a consumer unions them so that a record fetched
//     through two clients is one cache entry.
//   - No client carrying entity-bearing operations emits an empty table.

// gatewayFixture is one document fronting four services, prefixed per service
// the way a gateway that merges them does.
//
// Studio and Portal describe the SAME record under their own prefixes --
// identical shapes, which is the merge strip_prefix exists to perform. Identity
// composes its responses with allOf and declares one without `type: "object"`
// at all, which is the shape that used to yield no entity rows whatsoever.
const gatewayFixture = `{
  "openapi": "3.0.3",
  "info": { "title": "Gateway", "version": "1.0.0" },
  "paths": {
    "/twinos/workspaces": { "get": { "operationId": "TwinOS_workspaces.list",
      "responses": { "200": { "description": "ok", "content": { "application/json": {
        "schema": { "$ref": "#/components/schemas/TwinOS_WorkspaceEnvelope" } } } } } } },
    "/studio/workspaces/{id}": { "get": { "operationId": "Studio_workspaces.get",
      "responses": { "200": { "description": "ok", "content": { "application/json": {
        "schema": { "$ref": "#/components/schemas/Studio_WorkspaceResponse" } } } } } } },
    "/portal/workspaces/{id}": { "get": { "operationId": "Portal_workspaces.get",
      "responses": { "200": { "description": "ok", "content": { "application/json": {
        "schema": { "$ref": "#/components/schemas/Portal_WorkspaceResponse" } } } } } } },
    "/identity/orgs": { "get": { "operationId": "Identity_orgs.list",
      "responses": { "200": { "description": "ok", "content": { "application/json": {
        "schema": { "$ref": "#/components/schemas/Identity_OrgListResponse" } } } } } } },
    "/identity/orgs/{id}": { "get": { "operationId": "Identity_orgs.get",
      "responses": { "200": { "description": "ok", "content": { "application/json": {
        "schema": { "$ref": "#/components/schemas/Identity_OrgResponse" } } } } } } },
    "/identity/session": { "get": { "operationId": "Identity_session.get",
      "responses": { "200": { "description": "ok", "content": { "application/json": {
        "schema": { "$ref": "#/components/schemas/Identity_SessionResponse" } } } } } } }
  },
  "components": {
    "schemas": {
      "TwinOS_WorkspaceEnvelope": { "type": "object", "properties": {
        "items": { "type": "array", "items": { "$ref": "#/components/schemas/TwinOS_WorkspaceListItem" } } } },
      "TwinOS_WorkspaceListItem": { "type": "object", "properties": {
        "id": { "type": "string" }, "slug": { "type": "string" } } },

      "Studio_WorkspaceResponse": { "type": "object", "properties": {
        "id": { "type": "string" }, "name": { "type": "string" },
        "owner": { "$ref": "#/components/schemas/Studio_Owner" } } },
      "Studio_Owner": { "type": "object", "properties": {
        "id": { "type": "string" }, "email": { "type": "string" } } },

      "Portal_WorkspaceResponse": { "type": "object", "properties": {
        "id": { "type": "string" }, "name": { "type": "string" },
        "owner": { "$ref": "#/components/schemas/Portal_Owner" } } },
      "Portal_Owner": { "type": "object", "properties": {
        "id": { "type": "string" }, "email": { "type": "string" } } },

      "Identity_OrgListResponse": { "properties": {
        "data": { "type": "array", "items": { "$ref": "#/components/schemas/Identity_OrgResponse" } } } },
      "Identity_OrgBase": { "type": "object", "properties": { "id": { "type": "string" } } },
      "Identity_OrgResponse": { "allOf": [
        { "$ref": "#/components/schemas/Identity_OrgBase" },
        { "type": "object", "properties": { "name": { "type": "string" } } } ] },
      "Identity_SessionResponse": { "allOf": [
        { "$ref": "#/components/schemas/Identity_OrgBase" },
        { "type": "object", "properties": { "token": { "type": "string" } } } ] }
    }
  }
}`

// conflictingGatewayFixture is the same document with TwinOS's envelope renamed
// so that it and Portal's workspace record both strip to `WorkspaceResponse`
// while describing entirely different shapes.
//
// The bare name is substituted, not the quoted one: the component key and the
// $ref pointing at it both have to move, and the $ref carries the name inside a
// longer string. Replacing only the quoted key leaves a dangling reference,
// which prunes the whole schema set and makes the document prove nothing.
func conflictingGatewayFixture() string {
	return strings.ReplaceAll(gatewayFixture, "TwinOS_WorkspaceEnvelope", "TwinOS_WorkspaceResponse")
}

// gatewayPrefixes is every prefix the document uses, which is the set each
// client strips: see clientStripPrefixes in cmd/forge/plugins/client_multi.go
// for why a client strips its siblings' prefixes and not only its own.
var gatewayPrefixes = []string{"TwinOS_", "Studio_", "Portal_", "Identity_"}

// servicePlan is one entry of a clients: block -- a name, and the paths it
// covers.
type servicePlan struct {
	name    string
	include []string
	exclude []string
}

// gatewayClients is the four-client split the fixture describes. Identity
// carries an exclude, because the report that prompted these tests blamed one
// for an empty table and that hypothesis has to stay disprovable.
var gatewayClients = []servicePlan{
	{name: "twinos", include: []string{"/twinos"}},
	{name: "studio", include: []string{"/studio"}},
	{name: "portal", include: []string{"/portal"}},
	{name: "identity", include: []string{"/identity"}, exclude: []string{"/identity/session"}},
}

// generateService produces one client the way the CLI does: parse, filter,
// THEN strip, then generate.
//
// The order is copied from applySpecTransforms rather than chosen here, and it
// is the order that made the cross-client collision invisible in the first
// place, so a test that stripped first would be testing a pipeline nobody runs.
// Each client re-parses the document for the same reason generateOne does: both
// steps narrow the specification in place.
func generateService(t *testing.T, doc string, plan servicePlan) (map[string]string, error) {
	t.Helper()

	spec, err := client.NewSpecParser().ParseFile(
		context.Background(), writeSpecFile(t, "openapi.json", doc))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	spec.Apply(client.PathFilter{Include: plan.include, Exclude: plan.exclude})

	if err := client.StripPrefix(spec, gatewayPrefixes, ReservedIdentifiers()); err != nil {
		return nil, err
	}

	// Hooks on, because opsManifestEnabled gates the whole manifest -- and so
	// entities.ts -- on them. A client generated without hooks has no entity
	// table to be wrong about.
	cfg := baseConfig()
	cfg.Hooks = true

	out, err := (&Generator{}).Generate(context.Background(), spec, cfg)
	if err != nil {
		t.Fatalf("Generate %s: %v", plan.name, err)
	}

	return out.Files, nil
}

// entityShape is one row of the generated table, read back out of the emitted
// TypeScript.
//
// Read back rather than taken from the APISpec on purpose. The spec is what the
// generator believes; entities.ts is what the consumer imports and unions, and
// the two have diverged before -- the field names in this table are renamed on
// the way out, through the same tsFieldName the codecs use.
type entityShape struct {
	client  string
	idField string
	fields  map[string]string
}

var (
	entityRowPattern   = regexp.MustCompile(`^ {2}(?:'([^']*)'|([A-Za-z0-9_$]+)): \{(.*)\},$`)
	entityIDPattern    = regexp.MustCompile(`idField: '([^']*)'`)
	entityFieldPattern = regexp.MustCompile(`(?:'([^']*)'|([A-Za-z0-9_$]+)): '([^']*)'`)
)

// parseEntityTable reads the rows of a generated entities.ts.
//
// A regular expression is enough because writeEntities emits exactly one row
// per line in a fixed shape, and it fails loudly if that ever stops being true:
// a table whose text no row matches yields nothing, and every caller here
// asserts on what it found.
func parseEntityTable(t *testing.T, clientName, src string) map[string]entityShape {
	t.Helper()

	rows := make(map[string]entityShape)
	inTable := false

	for line := range strings.SplitSeq(src, "\n") {
		switch {
		case strings.HasPrefix(line, "export const entities = {"):
			inTable = true

			continue
		case strings.HasPrefix(line, "} as const"):
			inTable = false

			continue
		case !inTable:
			continue
		}

		match := entityRowPattern.FindStringSubmatch(line)
		if match == nil {
			t.Fatalf("%s: unparsed line in entities table: %q", clientName, line)
		}

		name := match[1] + match[2]
		body := match[3]

		shape := entityShape{client: clientName, fields: map[string]string{}}
		if id := entityIDPattern.FindStringSubmatch(body); id != nil {
			shape.idField = id[1]
		}

		if start := strings.Index(body, "fields: {"); start >= 0 {
			for _, f := range entityFieldPattern.FindAllStringSubmatch(body[start:], -1) {
				shape.fields[f[1]+f[2]] = f[3]
			}
		}

		rows[name] = shape
	}

	return rows
}

// conflicts reports whether two rows for one typename disagree.
//
// Both halves count, and for the same reason: the runtime reads `idField` to
// decide what to store a record under and `fields` to decide where to descend,
// so a disagreement in either is a record that lands in the wrong place or
// never lands at all.
//
// Equality is exact rather than "compatible". A supersetting rule would let one
// client's table quietly govern another's payloads, which is the failure being
// guarded against wearing a permission slip.
func (a entityShape) conflicts(b entityShape) bool {
	if a.idField != b.idField || len(a.fields) != len(b.fields) {
		return true
	}

	for prop, target := range a.fields {
		if b.fields[prop] != target {
			return true
		}
	}

	return false
}

func (a entityShape) String() string {
	props := make([]string, 0, len(a.fields))
	for prop, target := range a.fields {
		props = append(props, prop+"->"+target)
	}

	sort.Strings(props)

	return a.client + ": idField=" + a.idField + " fields={" + strings.Join(props, " ") + "}"
}

// unionEntityTables generates every client of a document and merges their
// tables the way the consumer does, reporting each typename that arrives
// declared two different ways.
//
// A client that REFUSES to generate contributes nothing and is counted rather
// than failed. Refusing is one valid way to keep the union consistent -- it is
// how the strip collision is handled today -- and a test that demanded a table
// from every client would break the moment anyone changed which of the two
// remedies applies. What must never happen is a full set of tables that
// contradict each other.
func unionEntityTables(t *testing.T, doc string) (union map[string]entityShape, conflicts []string, refused int) {
	t.Helper()

	union = make(map[string]entityShape)

	for _, plan := range gatewayClients {
		files, err := generateService(t, doc, plan)
		if err != nil {
			refused++

			continue
		}

		for name, shape := range parseEntityTable(t, plan.name, files["src/entities.ts"]) {
			existing, seen := union[name]
			if seen && existing.conflicts(shape) {
				conflicts = append(conflicts, name+"\n  "+existing.String()+"\n  "+shape.String())

				continue
			}

			union[name] = shape
		}
	}

	sort.Strings(conflicts)

	return union, conflicts, refused
}

// TestUnionedEntityTablesDeclareOneShapePerTypename is the assertion the
// consumer's dev-mode warning makes at runtime, made at generation time where
// something can gate on it.
//
// The consumer unions the four tables into one so that a record fetched through
// two clients is one cache entry. In a union, spread order decides which
// declaration of a repeated typename survives, so two clients disagreeing about
// one name means one of them is walking its own payloads against a field they
// do not have -- and nothing throws, the list renders from the raw response,
// and the cache is simply empty for those records.
//
// Both documents are checked. The well-formed one must produce four tables that
// agree. The one whose two services describe different records under one name
// must not produce a contradictory union either, by whichever remedy is in
// force -- today a refusal, and the assertion is written so that it stays a
// real assertion if that ever becomes something else.
func TestUnionedEntityTablesDeclareOneShapePerTypename(t *testing.T) {
	union, conflicts, refused := unionEntityTables(t, gatewayFixture)

	for _, conflict := range conflicts {
		t.Errorf("typename %s", conflict)
	}

	if refused != 0 {
		t.Errorf("%d of %d clients refused to generate from a well-formed document",
			refused, len(gatewayClients))
	}

	// Studio and Portal describe one record under two prefixes, so the union
	// must be smaller than the sum of its parts. Without this the test would
	// still pass on a build where stripping stopped working entirely and every
	// typename stayed unique by keeping its prefix.
	if _, merged := union["WorkspaceResponse"]; !merged {
		t.Errorf("Studio_ and Portal_WorkspaceResponse did not merge into one row; union has %v",
			sortedRowNames(union))
	}

	t.Run("conflicting document", func(t *testing.T) {
		_, conflicts, refused := unionEntityTables(t, conflictingGatewayFixture())

		for _, conflict := range conflicts {
			t.Errorf("typename %s", conflict)
		}

		if len(conflicts) == 0 && refused == 0 {
			t.Error("two services describe different records under one name and every client " +
				"generated a table anyway; nothing stopped the collision")
		}
	})
}

// TestEveryServiceWithEntityBearingOperationsEmitsRows is the counted
// assertion: a client whose operations return identifiable records must not
// ship an empty table.
//
// An empty entities.ts is four syntactically perfect lines and reads as a
// service that simply has nothing cacheable, which is why one shipped for a
// client carrying a hundred and forty-six operations. The identity service here
// is written the way that one was: responses composed with allOf, and an
// envelope declaring `properties` with no `type: "object"` beside it. Both are
// legal OpenAPI, both are fully resolved by the TypeScript type and codec
// emitters, and both used to contribute nothing here.
func TestEveryServiceWithEntityBearingOperationsEmitsRows(t *testing.T) {
	for _, plan := range gatewayClients {
		t.Run(plan.name, func(t *testing.T) {
			files, err := generateService(t, gatewayFixture, plan)
			if err != nil {
				t.Fatalf("generate %s: %v", plan.name, err)
			}

			rows := parseEntityTable(t, plan.name, files["src/entities.ts"])
			if len(rows) == 0 {
				t.Fatalf("%s carries entity-bearing operations but its entities table is empty:\n%s",
					plan.name, files["src/entities.ts"])
			}
		})
	}

	// Named rather than only counted, so the identity client cannot pass on
	// rows for some unrelated type while the composed ones stay missing.
	files, err := generateService(t, gatewayFixture, gatewayClients[3])
	if err != nil {
		t.Fatalf("generate identity: %v", err)
	}

	rows := parseEntityTable(t, "identity", files["src/entities.ts"])

	if got := rows["OrgResponse"]; got.idField != "id" {
		t.Errorf("OrgResponse composes its id through allOf; want idField 'id', got %q (row %v)",
			got.idField, sortedRowNames(rows))
	}

	if got := rows["OrgListResponse"]; got.fields["data"] != "OrgResponse" {
		t.Errorf("OrgListResponse declares properties without `type: object`; "+
			"want fields.data -> OrgResponse, got %v", got.fields)
	}
}

// TestStrippingRefusesTwoShapesUnderOneName covers the case the union test
// cannot reach, because generation now stops before producing a table to union.
//
// The collision check inside planRenames only ever saw one client's surviving
// schemas, and the path filter runs first, so the sibling that would have
// clashed was already gone. This asserts the pair is caught anyway -- and that
// the benign case, two services describing one record, still generates.
func TestStrippingRefusesTwoShapesUnderOneName(t *testing.T) {
	_, err := generateService(t, conflictingGatewayFixture(), gatewayClients[0])
	if err == nil {
		t.Fatal("TwinOS_ and Portal_WorkspaceResponse have different shapes and both strip to " +
			"WorkspaceResponse, but generation succeeded")
	}

	for _, want := range []string{"WorkspaceResponse", "different shapes", "Portal_WorkspaceResponse"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error does not mention %q: %v", want, err)
		}
	}

	// The same document unmodified: Studio_ and Portal_WorkspaceResponse also
	// strip to one name, and are identical. That merge is the whole reason a
	// client strips its siblings' prefixes, so it must stay silent.
	if _, err := generateService(t, gatewayFixture, gatewayClients[1]); err != nil {
		t.Fatalf("two services describing one record must merge, not fail: %v", err)
	}
}

func sortedRowNames(rows map[string]entityShape) []string {
	names := make([]string, 0, len(rows))
	for name := range rows {
		names = append(names, name)
	}

	sort.Strings(names)

	return names
}
