package spec

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func props(t *testing.T, schema map[string]any, def string) map[string]any {
	t.Helper()

	defs := schema["$defs"].(map[string]any)

	d, ok := defs[def].(map[string]any)
	if !ok {
		t.Fatalf("no $defs.%s", def)
	}

	p, _ := d["properties"].(map[string]any)

	return p
}

func tags(t reflect.Type) []string {
	var out []string

	for field := range t.Fields() {
		tag := field.Tag.Get("yaml")

		name, _, _ := strings.Cut(tag, ",")
		if name != "" && name != "-" && !strings.Contains(tag, ",inline") {
			out = append(out, name)
		}
	}

	return out
}

func TestSchemaCoversEveryField(t *testing.T) {
	raw, err := Schema()
	if err != nil {
		t.Fatal(err)
	}

	var schema map[string]any
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatal(err)
	}

	for def, typ := range map[string]reflect.Type{
		"Build": reflect.TypeFor[Build](), "Registry": reflect.TypeFor[Registry](), "Release": reflect.TypeFor[Release](), "Workbench": reflect.TypeFor[Workbench](), "Persistence": reflect.TypeFor[Persistence](), "ServiceBuild": reflect.TypeFor[ServiceBuild](), "ExternalService": reflect.TypeFor[ExternalService](), "Deploy": reflect.TypeFor[Deploy](), "Service": reflect.TypeFor[Service](), "Port": reflect.TypeFor[Port](),
		"Health": reflect.TypeFor[Health](), "Binding": reflect.TypeFor[Binding](), "Resource": reflect.TypeFor[Resource](),
		"Connection": reflect.TypeFor[Connection](), "Environment": reflect.TypeFor[Environment](),
		"ResourceOverride": reflect.TypeFor[ResourceOverride](), "Target": reflect.TypeFor[Target](), "Secrets": reflect.TypeFor[Secrets](),
	} {
		p := props(t, schema, def)
		for _, name := range tags(typ) {
			if _, ok := p[name]; !ok {
				t.Errorf("$defs.%s lacks property %q", def, name)
			}
		}

		for name := range p {
			found := false

			for _, tag := range tags(typ) {
				if tag == name {
					found = true
				}
			}

			if !found {
				t.Errorf("$defs.%s has property %q with no Go field", def, name)
			}
		}
	}

	if schema["$id"] != "https://raw.githubusercontent.com/xraph/forge/main/schema/forge-deploy.schema.json" {
		t.Fatalf("$id: %v", schema["$id"])
	}
}

func TestPublishedDeploymentSchemaMatchesCLI(t *testing.T) {
	published, e := os.ReadFile(filepath.Join("..", "..", "..", "..", "..", "schema", "forge-deploy.schema.json"))
	if e != nil {
		t.Fatal(e)
	}

	embedded, e := Schema()
	if e != nil {
		t.Fatal(e)
	}

	var left, right map[string]any
	if e = json.Unmarshal(published, &left); e != nil {
		t.Fatal(e)
	}

	if e = json.Unmarshal(embedded, &right); e != nil {
		t.Fatal(e)
	}

	if !reflect.DeepEqual(left, right) {
		t.Fatal("published deployment schema differs from CLI schema")
	}
}
