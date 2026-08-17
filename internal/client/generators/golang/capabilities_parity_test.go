package golang_test

import (
	"context"
	"regexp"
	"slices"
	"testing"

	"github.com/xraph/forge/internal/client"
	"github.com/xraph/forge/internal/client/generators/golang"
	"github.com/xraph/forge/internal/client/generators/typescript"
)

// specWithUnnormalizedAuthorization is a hand-built spec whose declared roles
// and permissions arrive in the shape no production IR path produces: out of
// order, with a duplicate, and with an empty string among them.
//
// Both production paths that populate Endpoint.Authorization
// (resolveEndpointAuthz in authz.go, routeToEndpoint in introspector.go)
// already sort, deduplicate and drop empties, so nothing in the normal
// pipeline can reach a generator looking like this. An Endpoint built
// directly -- a fixture, a caller wiring an APISpec by hand, a future third
// producer -- can, and when it does the two generators must still describe the
// same requirement. An empty role in Go's table is a role no principal can
// ever hold, which turns CanCall into a permanent false for that operation
// while TypeScript's canCall answers true: one client hides a button the other
// shows, for the identical principal.
func specWithUnnormalizedAuthorization() *client.APISpec {
	return &client.APISpec{
		Info:    client.APIInfo{Title: "Unnormalized Authorization API", Version: "1.0.0"},
		Servers: []client.Server{{URL: "https://api.example.com"}},
		Endpoints: []client.Endpoint{
			{
				ID:     "updateUser",
				Method: "PATCH",
				Path:   "/users",
				Authorization: &client.Authorization{
					Roles:       []string{"editor", "", "admin", "editor"},
					Permissions: []string{"users:write", "", "users:read", "users:write"},
				},
			},
		},
	}
}

// TestAuthorizationTableAgreesAcrossLanguages drives one deliberately
// unnormalized endpoint through both generators and asserts the per-operation
// authorization tables they emit list the same roles and the same permissions.
//
// Asserting equality between the two outputs rather than each against a
// literal is the point: a table hardcoded per language is exactly the thing
// that drifted, and a test that pins Go's expected output would have passed
// just as happily before this fix as after it if the expectation had been
// written from Go's behaviour.
func TestAuthorizationTableAgreesAcrossLanguages(t *testing.T) {
	goSrc := goCapabilitiesSource(t)
	tsSrc := tsCapabilitiesSource(t)

	for _, field := range []struct {
		name      string
		goPattern string
		tsPattern string
	}{
		{"roles", `Roles: \[\]Role\{([^}]*)\}`, `roles: \[([^\]]*)\]`},
		{"permissions", `Permissions: \[\]Permission\{([^}]*)\}`, `permissions: \[([^\]]*)\]`},
	} {
		goValues := tableEntryValues(t, "capabilities.go", goSrc, field.goPattern)
		tsValues := tableEntryValues(t, "capabilities.ts", tsSrc, field.tsPattern)

		if !slices.Equal(goValues, tsValues) {
			t.Errorf("generated %s disagree: Go emitted %q, TypeScript emitted %q\n\n%s\n\n%s",
				field.name, goValues, tsValues, goSrc, tsSrc)
		}
	}
}

func goCapabilitiesSource(t *testing.T) string {
	t.Helper()

	result, err := golang.NewGenerator().Generate(
		context.Background(), specWithUnnormalizedAuthorization(), authStreamingConfig())
	if err != nil {
		t.Fatalf("Go Generate: %v", err)
	}

	src, ok := result.Files["capabilities.go"]
	if !ok {
		t.Fatal("capabilities.go not emitted for a spec declaring roles and permissions")
	}

	return src
}

func tsCapabilitiesSource(t *testing.T) string {
	t.Helper()

	config := client.DefaultConfig()
	config.Language = "typescript"
	config.PackageName = "probe"
	config.IncludeAuth = true

	result, err := typescript.NewGenerator().Generate(
		context.Background(), specWithUnnormalizedAuthorization(), config)
	if err != nil {
		t.Fatalf("TypeScript Generate: %v", err)
	}

	src, ok := result.Files["src/capabilities.ts"]
	if !ok {
		t.Fatal("src/capabilities.ts not emitted for a spec declaring roles and permissions")
	}

	return src
}

// quotedValue matches one string literal in either language's quoting style:
// strconv.Quote's double quotes on the Go side, tsString's single quotes on
// the TypeScript side.
var quotedValue = regexp.MustCompile(`"([^"]*)"|'([^']*)'`)

// tableEntryValues pulls the single authorization list pattern matches out of
// src and returns its members in emitted order.
//
// The fixture declares exactly one gated operation, so more than one match
// means the pattern caught something other than the table entry and the
// comparison that follows would be meaningless.
func tableEntryValues(t *testing.T, name, src, pattern string) []string {
	t.Helper()

	matches := regexp.MustCompile(pattern).FindAllStringSubmatch(src, -1)
	if len(matches) != 1 {
		t.Fatalf("%s: %q matched %d times, want exactly 1\n%s", name, pattern, len(matches), src)
	}

	var values []string

	for _, quoted := range quotedValue.FindAllStringSubmatch(matches[0][1], -1) {
		values = append(values, quoted[1]+quoted[2])
	}

	return values
}
