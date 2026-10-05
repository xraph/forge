package typescript

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/xraph/forge/internal/client"
)

// entitySpec is baseSpec with users.get resolved to an entity whose id field
// is snake_case, so camel naming renames the derived tag.
func entitySpec() *client.APISpec {
	spec := baseSpec()
	user := &client.EntityRef{Type: "User", IDField: "user_id"}

	spec.Entities = map[string]*client.EntityRef{"User": user}

	for i := range spec.Endpoints {
		switch spec.Endpoints[i].OperationID {
		case "users.get":
			spec.Endpoints[i].Entity = user
			spec.Endpoints[i].RootType = "User"
			spec.Endpoints[i].StaleTime = 30000
			spec.Endpoints[i].CacheTags = client.TagSet{Provides: []string{"User:{user_id}"}}
		case "users.create":
			spec.Endpoints[i].Entity = user
			spec.Endpoints[i].RootType = "User"
			spec.Endpoints[i].Idempotent = true
			spec.Endpoints[i].CacheTags = client.TagSet{
				Provides: []string{"User:{user_id}"}, Invalidates: []string{"User[]"},
			}
		}
	}

	return spec
}

func TestOpsManifestEmitsIdempotentOnlyWhenDeclared(t *testing.T) {
	cfg := baseConfig()
	cfg.Hooks = true

	out, err := NewGenerator().Generate(context.Background(), entitySpec(), cfg)
	if err != nil {
		t.Fatal(err)
	}

	ops := out.Files["src/ops.ts"]
	if got := strings.Count(ops, "    idempotent: true,\n"); got != 1 {
		t.Errorf("ops.ts carries %d idempotent rows, want 1:\n%s", got, ops)
	}

	if !strings.Contains(ops, "readonly idempotent?: boolean;") {
		t.Error("OperationMeta must declare idempotent when an operation carries it")
	}

	if !strings.Contains(out.Files["src/ops/users.create.ts"], "  idempotent: true,\n") {
		t.Error("the per-operation module must carry the flag too")
	}

	plain, err := NewGenerator().Generate(context.Background(), baseSpec(), cfg)
	if err != nil {
		t.Fatal(err)
	}

	if strings.Contains(plain.Files["src/ops.ts"], "idempotent") {
		t.Error("a client with no idempotent route must emit the bytes it always did")
	}
}

func TestTablesJSONIsAbsentUnlessRequested(t *testing.T) {
	out, err := NewGenerator().Generate(context.Background(), entitySpec(), baseConfig())
	if err != nil {
		t.Fatal(err)
	}

	if _, ok := out.Files[client.TablesFile]; ok {
		t.Errorf("%s emitted without EmitTablesJSON", client.TablesFile)
	}
}

func TestTablesJSONCarriesTheRenderedOpsRows(t *testing.T) {
	cfg := baseConfig()
	cfg.Hooks = true
	cfg.EmitTablesJSON = true

	out, err := NewGenerator().Generate(context.Background(), entitySpec(), cfg)
	if err != nil {
		t.Fatal(err)
	}

	var tables client.GeneratedTables
	if err := json.Unmarshal([]byte(out.Files[client.TablesFile]), &tables); err != nil {
		t.Fatalf("decode %s: %v", client.TablesFile, err)
	}

	get := tables.Ops["users.get"]
	if get.Method != "GET" || get.RootType != "User" || get.StaleTime != 30000 {
		t.Errorf("users.get row = %+v", get)
	}

	if len(get.Provides) != 1 || get.Provides[0] != "User:{userId}" {
		t.Errorf("provides = %v, want the camel-renamed derived tag", get.Provides)
	}

	if !strings.Contains(out.Files["src/ops.ts"], "provides: ['User:{userId}'],") {
		t.Error("ops.ts and the tables file disagree about provides")
	}

	if !tables.Ops["users.create"].Idempotent {
		t.Error("users.create must be idempotent in the tables file")
	}

	if tables.Ops["users.create"].BodyCodec != "User" {
		t.Errorf("bodyCodec = %q, want User", tables.Ops["users.create"].BodyCodec)
	}

	if tables.Entities["User"].IDField != "userId" {
		t.Errorf("entities.User.idField = %q, want userId", tables.Entities["User"].IDField)
	}
}
