package dart

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/xraph/forge/internal/client"
	"github.com/xraph/forge/internal/client/generators/typescript"
)

// parityCase is one specification and the Dart configuration both generators
// are run over. TypeScript receives the same configuration with its own
// language.
type parityCase struct {
	build  func() *client.APISpec
	config client.GeneratorConfig
}

// parityCorpus is the specification set both generators are run over. Each
// entry builds a fresh spec, since a generator may not be assumed to leave
// its input untouched.
func parityCorpus() map[string]parityCase {
	streaming := streamingFixture()
	pagination := paginationFixture()
	enums := enumsFixture()
	rest := restFixture()

	reserved := baseConfig()
	reserved.PackageName = "reserved_client"

	minimal := baseConfig()
	minimal.PackageName = "minimal_client"

	return map[string]parityCase{
		"orders":   {ordersSpec, baseConfig()},
		"reserved": {reservedSpec, reserved},
		"minimal":  {minimalSpec, minimal},
		"capabilities": {func() *client.APISpec {
			spec := ordersSpec()
			spec.Endpoints[2].Security = []client.SecurityRequirement{
				{SchemeName: "bearerAuth", Scopes: []string{"orders.write", "admin"}},
				{SchemeName: "bearerAuth", Scopes: []string{"orders.admin"}},
			}
			spec.Endpoints[3].Authorization = &client.Authorization{
				Roles: []string{"editor", "admin"}, Permissions: []string{"orders.delete"},
			}

			return spec
		}, baseConfig()},
		"streaming":  {streamingFixtureSpec, streaming.Config},
		"pagination": {paginationFixtureSpec, pagination.Config},
		"enums":      {enumsFixtureSpec, enums.Config},
		"rest":       {restFixtureSpec, rest.Config},
	}
}

func streamingFixtureSpec() *client.APISpec  { return streamingFixture().Spec }
func paginationFixtureSpec() *client.APISpec { return paginationFixture().Spec }
func enumsFixtureSpec() *client.APISpec      { return enumsFixture().Spec }
func restFixtureSpec() *client.APISpec       { return restFixture().Spec }

// TestTablesAgreeWithTypeScript runs one corpus through both generators and
// asserts that the ops, entities, streams, security and capability tables
// each emits are byte-identical as canonical JSON. A difference is a client
// that caches, invalidates or authorizes differently by language.
//
// With FORGE_WRITE_FIXTURES=1 it also writes each table set to
// packages/client-fixtures/ops/<name>.json; otherwise it asserts the tables
// still match that file, and a missing file fails.
func TestTablesAgreeWithTypeScript(t *testing.T) {
	fixtureDir, err := filepath.Abs(filepath.Join("..", "..", "..", "..", "packages", "client-fixtures", "ops"))
	if err != nil {
		t.Fatal(err)
	}

	for name, c := range parityCorpus() {
		t.Run(name, func(t *testing.T) {
			// TypeScript's field naming for a dart config is preserve, so the
			// comparison runs it as a typescript one.
			tsConfig := c.config
			tsConfig.Language = "typescript"
			tsConfig.PackageName = "probe"
			tsConfig.Hooks = true
			tsConfig.EmitTablesJSON = true

			dartConfig := c.config
			dartConfig.EmitTablesJSON = true

			tsOut, err := typescript.NewGenerator().Generate(context.Background(), c.build(), tsConfig)
			if err != nil {
				t.Fatalf("typescript: %v", err)
			}

			dartOut, err := NewGenerator().Generate(context.Background(), c.build(), dartConfig)
			if err != nil {
				t.Fatalf("dart: %v", err)
			}

			tsTables := tsOut.Files[client.TablesFile]
			dartTables := dartOut.Files[client.TablesFile]

			if tsTables == "" || dartTables == "" {
				t.Fatalf("%s missing: typescript %d bytes, dart %d bytes", client.TablesFile, len(tsTables), len(dartTables))
			}

			if tsTables != dartTables {
				t.Fatalf("tables differ\n--- typescript\n%s\n--- dart\n%s", tsTables, dartTables)
			}

			path := filepath.Join(fixtureDir, name+".json")

			if os.Getenv("FORGE_WRITE_FIXTURES") == "1" {
				if err := os.MkdirAll(fixtureDir, 0o755); err != nil {
					t.Fatal(err)
				}

				if err := os.WriteFile(path, []byte(dartTables), 0o644); err != nil {
					t.Fatal(err)
				}

				return
			}

			committed, err := os.ReadFile(path)
			if os.IsNotExist(err) {
				t.Fatalf("%s is missing, so nothing pins this corpus entry; write it with FORGE_WRITE_FIXTURES=1 go test ./internal/client/generators/dart/ -run TestTablesAgreeWithTypeScript", path)
			}

			if err != nil {
				t.Fatal(err)
			}

			if string(committed) != dartTables {
				t.Errorf("%s is stale; regenerate with FORGE_WRITE_FIXTURES=1 go test ./internal/client/generators/dart/ -run TestTablesAgreeWithTypeScript", path)
			}
		})
	}
}

// TestOpsFixtureDirectoryHoldsExactlyTheCorpus fails on a file in ops/ that no
// corpus entry writes: an entry renamed or dropped leaves its old file behind,
// still read by anyone who trusts the directory.
func TestOpsFixtureDirectoryHoldsExactlyTheCorpus(t *testing.T) {
	dir := filepath.Join("..", "..", "..", "..", "packages", "client-fixtures", "ops")

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	want := map[string]bool{}
	for name := range parityCorpus() {
		want[name+".json"] = true
	}

	for _, entry := range entries {
		if !want[entry.Name()] {
			t.Errorf("%s is in ops/ but no corpus entry writes it; delete it", entry.Name())
		}

		delete(want, entry.Name())
	}

	// A missing file is reported by the subtest that owns it, with the
	// command that writes it. With FORGE_WRITE_FIXTURES set the directory is
	// being filled and may be short.
	if os.Getenv("FORGE_WRITE_FIXTURES") != "1" {
		for name := range want {
			t.Errorf("%s is in the corpus but not in ops/", name)
		}
	}
}
