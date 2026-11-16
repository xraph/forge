package golang_test

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/xraph/forge/internal/client"
	"github.com/xraph/forge/internal/client/generators/golang"
)

// The fixtures under internal/client/testdata/injection carry specification
// text written to break out of the position each generator prints it into.
// For Go, a description containing "\nfunc init() {...}" became a real init
// function that ran when the generated package was imported, and a property
// name containing a quote and a backtick closed its struct tag.

func injectionFixture(t *testing.T, name string) *client.APISpec {
	t.Helper()

	spec, err := client.NewSpecParser().ParseFile(context.Background(),
		filepath.Join("..", "..", "testdata", "injection", name))
	if err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}

	return spec
}

func injectionConfig() client.GeneratorConfig {
	cfg := client.DefaultConfig()
	cfg.Language = "go"
	cfg.PackageName = "probe"
	cfg.Module = "example.com/probe"

	return cfg
}

func TestGoSpecTextCannotEscapeItsContext(t *testing.T) {
	out, err := golang.NewGenerator().Generate(context.Background(), injectionFixture(t, "evil.json"), injectionConfig())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	for name, src := range out.Files {
		if !strings.HasSuffix(name, ".go") {
			continue
		}

		file, err := parser.ParseFile(token.NewFileSet(), name, src, parser.ParseComments)
		if err != nil {
			t.Fatalf("%s does not parse: %v", name, err)
		}

		for _, decl := range file.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				if d.Name.Name == "init" {
					t.Errorf("%s: a description became an init function", name)
				}
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					if vs, ok := spec.(*ast.ValueSpec); ok {
						for _, n := range vs.Names {
							if strings.HasPrefix(n.Name, "PWN") {
								t.Errorf("%s: a description declared %s", name, n.Name)
							}
						}
					}
				}
			}
		}

		// The only field allowed to mention PWN_TAG is the one whose JSON
		// name is the hostile property name, intact.
		ast.Inspect(file, func(n ast.Node) bool {
			field, ok := n.(*ast.Field)
			if !ok || field.Tag == nil {
				return true
			}

			raw, err := strconv.Unquote(field.Tag.Value)
			if err != nil {
				t.Errorf("%s: tag %s does not unquote: %v", name, field.Tag.Value, err)

				return true
			}

			jsonName, _, _ := strings.Cut(reflect.StructTag(raw).Get("json"), ",")
			if strings.Contains(raw, "PWN_TAG") && jsonName != "x\"`json:\"y\"` PWN_TAG int `z" {
				t.Errorf("%s: property name escaped its tag: %s", name, field.Tag.Value)
			}

			return true
		})
	}

	rest := out.Files["rest.go"]
	if !strings.Contains(rest, "// func init() { panic(\"PWN_GO_DESC\") }\n") {
		t.Errorf("second description line lost its comment prefix:\n%s", rest)
	}

	if !strings.Contains(rest, `q.Set("q'] = (globalThis.PWN_QUERY=1); queryParams['z", fmt.Sprint(`) {
		t.Errorf("query parameter name was not quoted:\n%s", rest)
	}

	if !strings.Contains(rest, "\"/items/\" + url.PathEscape(fmt.Sprint(id)) + \"`+(globalThis.PWN_PATH=1)+`\"") {
		t.Errorf("path was not built from quoted segments:\n%s", rest)
	}
}

func TestGoSchemaNameThatIsNotAnIdentifierIsRejected(t *testing.T) {
	_, err := golang.NewGenerator().Generate(context.Background(), injectionFixture(t, "evil_name.json"), injectionConfig())
	if err == nil {
		t.Fatal("expected a schema name that is not an identifier to be rejected")
	}

	if !strings.Contains(err.Error(), "not valid Go identifiers") {
		t.Errorf("error does not explain the rejection: %v", err)
	}
}

func TestGoStreamingSpecTextCannotEscapeItsContext(t *testing.T) {
	payload := "a\"`\n}\nfunc init() { panic(1) }\n//"

	spec := &client.APISpec{
		Info: client.APIInfo{Title: "T", Version: "1"},
		WebSockets: []client.WebSocketEndpoint{{
			ID: "chat\"){", Path: "/ws/" + payload, Description: payload,
		}},
		SSEs: []client.SSEEndpoint{{
			ID: "feed", Path: "/sse/" + payload, Description: payload,
			EventSchemas: map[string]*client.Schema{payload: {Type: "object"}},
		}},
		WebTransports: []client.WebTransportEndpoint{{
			ID: "wt", Path: "/wt/" + payload, Description: payload,
		}},
	}

	cfg := injectionConfig()
	cfg.IncludeStreaming = true

	out, err := golang.NewGenerator().Generate(context.Background(), spec, cfg)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	for name, src := range out.Files {
		if !strings.HasSuffix(name, ".go") {
			continue
		}

		file, err := parser.ParseFile(token.NewFileSet(), name, src, 0)
		if err != nil {
			t.Fatalf("%s does not parse: %v", name, err)
		}

		for _, decl := range file.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok && fn.Name.Name == "init" {
				t.Errorf("%s: spec text became an init function", name)
			}
		}
	}
}

// TestGoPathParameterValuesAreEscaped builds the generated client and calls
// it against a test server. A path parameter value used to be concatenated
// into the path unescaped, so "../admin/users?role=admin#" moved the request
// to another route and added a query string.
func TestGoPathParameterValuesAreEscaped(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping go run gate under -short")
	}

	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skipf("no go toolchain on PATH: %v", err)
	}

	spec := &client.APISpec{
		Info: client.APIInfo{Title: "T", Version: "1"},
		Endpoints: []client.Endpoint{{
			Method: "GET", Path: "/users/{id}/posts/{post}", OperationID: "getPost",
			PathParams: []client.Parameter{
				{Name: "id", In: "path", Required: true, Schema: &client.Schema{Type: "string"}},
				{Name: "post", In: "path", Required: true, Schema: &client.Schema{Type: "string"}},
			},
			Responses: map[int]*client.Response{204: {Description: "none"}},
		}},
	}

	cfg := injectionConfig()
	cfg.Module = "example.com/generated"
	cfg.PackageName = "generated"

	out, err := golang.NewGenerator().Generate(context.Background(), spec, cfg)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	dir := t.TempDir()

	for name, src := range out.Files {
		if strings.HasSuffix(name, ".go") || name == "go.mod" {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}

	driverDir := filepath.Join(dir, "cmd", "driver")
	if err := os.MkdirAll(driverDir, 0o755); err != nil {
		t.Fatal(err)
	}

	driver := `package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"

	gen "example.com/generated"
)

func main() {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.EscapedPath() + "|" + r.URL.RawQuery
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := gen.NewClient(gen.WithBaseURL(srv.URL))
	if err := c.GetPost(context.Background(), "../admin/users?role=admin#", "a/b c"); err != nil {
		fmt.Println("error:", err)
	}
	fmt.Print(got)
}
`
	if err := os.WriteFile(filepath.Join(driverDir, "main.go"), []byte(driver), 0o600); err != nil {
		t.Fatal(err)
	}

	cmd := exec.CommandContext(t.Context(), goBin, "run", "./cmd/driver")
	cmd.Dir = dir

	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod", "GOPROXY=off", "GOSUMDB=off", "GOWORK=off")

	got, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go run: %v\n%s", err, got)
	}

	want := "/users/..%2Fadmin%2Fusers%3Frole=admin%23/posts/a%2Fb%20c|"
	if string(got) != want {
		t.Errorf("request reached %q, want %q", got, want)
	}
}
