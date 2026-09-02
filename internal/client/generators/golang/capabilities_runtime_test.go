package golang_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xraph/forge/internal/client/generators/golang"
)

// TestGoGeneratorCapabilityRuntimeAnswers actually builds and runs a program
// against the generated CanCall/MissingCapabilities, rather than only
// checking that the source text looks right.
//
// The other capability tests in this package (capabilities_test.go,
// capabilities_operations_test.go) assert on emitted source text and on
// go/parser/go build succeeding, which catches syntax and compile defects
// but nothing about whether CanCall actually implements scopes ALL-of, roles
// ANY-of and permissions ALL-of correctly. F1's own instructions call out
// exactly this: "getting this wrong is a silent authorization defect", so
// this test drives the real generated code with a real Principal and checks
// the answers, the same way capabilities_test.go in the TypeScript generator
// drives its emitted capabilities.ts under node.
func TestGoGeneratorCapabilityRuntimeAnswers(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping go run gate under -short")
	}

	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skipf("no go toolchain on PATH: %v", err)
	}

	config := authStreamingConfig()
	config.Module = "github.com/example/generated"

	result, err := golang.NewGenerator().Generate(context.Background(), specWithAuthorizationMatrix(), config)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	dir := t.TempDir()

	for name, src := range result.Files {
		if !strings.HasSuffix(name, ".go") && name != "go.mod" {
			continue
		}

		if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	// The driver lives in its own subpackage (package main) so `go run` has
	// something to run: the generated files above are package publicclient,
	// a library with no main of its own.
	driverDir := filepath.Join(dir, "cmd", "driver")
	if err := os.MkdirAll(driverDir, 0o755); err != nil {
		t.Fatalf("mkdir driver dir: %v", err)
	}

	// listUsers: one scope alternative, one scope.
	// createUser: one scope alternative needing two scopes, AND a role
	// (any of editor/admin) AND a permission (users:write).
	// uploadFile: two scope alternatives of different sizes.
	// publicPing: gated on nothing, absent from operationRequirements.
	driver := `package main

import (
	"encoding/json"
	"fmt"

	gen "github.com/example/generated"
)

type snapshot struct {
	CallList    bool     ` + "`json:\"callList\"`" + `
	CallCreate  bool     ` + "`json:\"callCreate\"`" + `
	MissingCreate []string ` + "`json:\"missingCreate\"`" + `
	CallUpload  bool     ` + "`json:\"callUpload\"`" + `
	MissingUpload []string ` + "`json:\"missingUpload\"`" + `
	CallPing    bool     ` + "`json:\"callPing\"`" + `
}

func take(c *gen.Client) snapshot {
	missingCreate := c.MissingCapabilities("createUser")
	missingCreateStr := make([]string, len(missingCreate))
	for i, m := range missingCreate {
		missingCreateStr[i] = string(m)
	}

	missingUpload := c.MissingCapabilities("uploadFile")
	missingUploadStr := make([]string, len(missingUpload))
	for i, m := range missingUpload {
		missingUploadStr[i] = string(m)
	}

	return snapshot{
		CallList:      c.CanCall("listUsers"),
		CallCreate:    c.CanCall("createUser"),
		MissingCreate: missingCreateStr,
		CallUpload:    c.CanCall("uploadFile"),
		MissingUpload: missingUploadStr,
		CallPing:      c.CanCall("publicPing"),
	}
}

func main() {
	c := gen.NewClient()

	before := take(c)

	// Holds the scope listUsers needs, and one of uploadFile's two scope
	// alternatives, but no role or permission at all.
	c.SetPrincipal(gen.Principal{
		Capabilities: []gen.Capability{"users:read", "upload:write"},
	})
	partial := take(c)

	// Now holds createUser's two scopes, one of its two roles (editor, not
	// admin -- roles are ANY-of), and its one required permission.
	c.SetPrincipal(gen.Principal{
		Capabilities: []gen.Capability{"users:read", "upload:write", "users:write", "admin"},
		Roles:        []gen.Role{"editor"},
		Permissions:  []gen.Permission{"users:write"},
	})
	full := take(c)

	out, err := json.Marshal(map[string]snapshot{
		"before":  before,
		"partial": partial,
		"full":    full,
	})
	if err != nil {
		panic(err)
	}

	fmt.Println(string(out))
}
`

	if err := os.WriteFile(filepath.Join(driverDir, "main.go"), []byte(driver), 0o600); err != nil {
		t.Fatalf("write driver: %v", err)
	}

	cmd := exec.Command(goBin, "run", "./cmd/driver")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod", "GOPROXY=off", "GOSUMDB=off")

	out, err := cmd.CombinedOutput()
	if err != nil {
		if isModuleResolutionFailure(string(out)) {
			t.Skipf("module cache cannot resolve the generated client's dependencies offline:\n%s", out)
		}

		t.Fatalf("driver failed: %v\n%s", err, out)
	}

	var got map[string]struct {
		CallList      bool     `json:"callList"`
		CallCreate    bool     `json:"callCreate"`
		MissingCreate []string `json:"missingCreate"`
		CallUpload    bool     `json:"callUpload"`
		MissingUpload []string `json:"missingUpload"`
		CallPing      bool     `json:"callPing"`
	}

	// The driver's stdout may carry `go: downloading` chatter ahead of the
	// JSON line even with GOPROXY=off (module graph resolution against the
	// local cache), so decode only the final line.
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")

	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &got); err != nil {
		t.Fatalf("driver output was not the expected JSON: %v\n%s", err, out)
	}

	// Nothing granted: every gated operation refused, the ungated one is
	// callable regardless -- there is nothing for it to be missing.
	before := got["before"]
	if before.CallList || before.CallCreate || before.CallUpload || !before.CallPing {
		t.Errorf("before = %+v, want everything false except callPing", before)
	}

	// listUsers' one scope is held: callable. uploadFile has one alternative
	// satisfied (upload:write) so it is callable too -- OpenAPI's OR-of-ANDs.
	// createUser needs two scopes plus a role plus a permission, none of
	// which are held yet.
	partial := got["partial"]
	if !partial.CallList {
		t.Errorf("partial.callList = false, want true (its one scope is held)")
	}

	if !partial.CallUpload {
		t.Errorf("partial.callUpload = false, want true (one alternative is fully held)")
	}

	if partial.CallCreate {
		t.Errorf("partial.callCreate = true, want false (no role, no permission, only one of two scopes)")
	}

	// Full grant: every scope, editor (one of the two ANY-of roles), and the
	// one required permission. createUser must now be callable -- this is
	// the case that proves roles are ANY-of rather than ALL-of, since editor
	// alone (not admin) is what's held.
	full := got["full"]
	if !full.CallCreate {
		t.Errorf("full.callCreate = false, want true (editor satisfies the roles ANY-of, permission held, both scopes held)")
	}

	if len(full.MissingCreate) != 0 {
		t.Errorf("full.missingCreate = %v, want empty", full.MissingCreate)
	}

	if !full.CallList || !full.CallUpload || !full.CallPing {
		t.Errorf("full = %+v, want everything callable", full)
	}
}
