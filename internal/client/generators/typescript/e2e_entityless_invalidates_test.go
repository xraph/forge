package typescript

import (
	"strings"
	"testing"
)

// entitylessInvalidatesFixture is two writes whose responses carry no entity,
// each declaring what it invalidates elsewhere. Both shapes come from tap: a
// DELETE answering 204, and an approval that answers with a redirect target.
const entitylessInvalidatesFixture = `{
  "openapi": "3.0.3",
  "info": { "title": "Rules", "version": "1.0.0" },
  "paths": {
    "/rules/{id}": {
      "delete": {
        "operationId": "rules.delete",
        "parameters": [ { "name": "id", "in": "path", "required": true, "schema": { "type": "string" } } ],
        "x-forge-invalidates": ["Rule[]"],
        "responses": { "204": { "description": "deleted" } }
      }
    },
    "/oauth/requests/{id}/approve": {
      "post": {
        "operationId": "oauth.approve",
        "parameters": [ { "name": "id", "in": "path", "required": true, "schema": { "type": "string" } } ],
        "x-forge-invalidates": ["Grant[]"],
        "responses": { "200": { "description": "ok", "content": { "application/json": {
          "schema": { "$ref": "#/components/schemas/RedirectTo" } } } } }
      }
    }
  },
  "components": { "schemas": {
    "RedirectTo": { "type": "object", "properties": { "redirect_to": { "type": "string" } } }
  } }
}`

// The manifest half: the IR carrying the tags is not enough if the emitter
// only renders them for an operation with an entity.
func TestGenerateEmitsInvalidatesForEntitylessWrites(t *testing.T) {
	files := generateFromSpecFile(t, writeSpecFile(t, "openapi.json", entitylessInvalidatesFixture))
	ops := ClientManifestText(files)

	cases := []struct{ op, invalidates string }{
		{"op_rules_delete", "invalidates: ['Rule[]'],"},
		{"op_oauth_approve", "invalidates: ['Grant[]'],"},
	}

	for _, tc := range cases {
		block := opBlock(t, ops, tc.op)

		if !strings.Contains(block, tc.invalidates) {
			t.Errorf("%s is missing %q:\n%s", tc.op, tc.invalidates, block)
		}

		if !strings.Contains(block, "provides: [],") {
			t.Errorf("%s should provide nothing:\n%s", tc.op, block)
		}

		// No entity resolved, so none is claimed. A row naming one would send
		// the runtime normalizing a response that is not a record.
		if strings.Contains(block, "entity:") {
			t.Errorf("%s names an entity it does not have:\n%s", tc.op, block)
		}
	}
}

// opBlock returns one operation's module body, from its declaration to the
// `satisfies` that closes it.
func opBlock(t *testing.T, ops, name string) string {
	t.Helper()

	start := strings.Index(ops, "export const "+name+" = {")
	if start < 0 {
		t.Fatalf("no operation %s in the manifest:\n%s", name, ops)
	}

	end := strings.Index(ops[start:], "} as const satisfies OperationMeta;")
	if end < 0 {
		t.Fatalf("operation %s is never closed", name)
	}

	return ops[start : start+end]
}
