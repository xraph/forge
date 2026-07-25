package typescript

import (
	"context"
	"strings"
	"testing"
)

// errorsMentioning returns the subset of errs containing needle.
func errorsMentioning(errs []string, needle string) []string {
	var out []string

	for _, e := range errs {
		if strings.Contains(e, needle) {
			out = append(out, e)
		}
	}

	return out
}

func TestNoDanglingAuthConfig(t *testing.T) {
	for _, f := range gateFixtures() {
		t.Run(f.Name, func(t *testing.T) {
			errs := typeCheck(t, generateTo(t, f))

			if bad := errorsMentioning(errs, "AuthConfig"); len(bad) > 0 {
				t.Errorf("AuthConfig is referenced but not exported:\n%s", strings.Join(bad, "\n"))
			}
		})
	}
}

func TestRESTExtendsConfiguredClientClass(t *testing.T) {
	for _, f := range gateFixtures() {
		t.Run(f.Name, func(t *testing.T) {
			errs := typeCheck(t, generateTo(t, f))

			for _, needle := range []string{"has no exported member 'Client'", "Property 'request' does not exist"} {
				if bad := errorsMentioning(errs, needle); len(bad) > 0 {
					t.Errorf("REST client does not extend the configured class:\n%s", strings.Join(bad, "\n"))
				}
			}
		})
	}
}

func TestNoUndeclaredRequire(t *testing.T) {
	for _, f := range gateFixtures() {
		t.Run(f.Name, func(t *testing.T) {
			errs := typeCheck(t, generateTo(t, f))

			if bad := errorsMentioning(errs, "Cannot find name 'require'"); len(bad) > 0 {
				t.Errorf("generated code uses an undeclared require:\n%s", strings.Join(bad, "\n"))
			}
		})
	}
}

func TestTypesQuoteNonIdentifierKeys(t *testing.T) {
	var fixture gateFixture

	for _, f := range gateFixtures() {
		if f.Name == "odd-keys" {
			fixture = f
		}
	}

	out, err := NewGenerator().Generate(context.Background(), fixture.Spec, fixture.Config)
	if err != nil {
		t.Fatal(err)
	}

	types := out.Files["src/types.ts"]

	if !strings.Contains(types, "\"content-type\"?: string;") {
		t.Errorf("expected quoted \"content-type\" key, got:\n%s", types)
	}

	if !strings.Contains(types, "\"3dtiles\"?: string;") {
		t.Errorf("expected quoted \"3dtiles\" key, got:\n%s", types)
	}

	if !strings.Contains(types, "\"it's\"?: string;") {
		t.Errorf("expected properly escaped \"it's\" key, got:\n%s", types)
	}

	if !strings.Contains(types, "\"back\\\\slash\"?: string;") {
		t.Errorf("expected properly escaped \"back\\\\slash\" key, got:\n%s", types)
	}

	errs := typeCheck(t, generateTo(t, fixture))

	// Verify the syntax errors we fixed are gone
	if bad := errorsMentioning(errs, "TS1131"); len(bad) > 0 {
		t.Errorf("should not have TS1131 errors:\n%s", strings.Join(bad, "\n"))
	}
	if bad := errorsMentioning(errs, "TS1351"); len(bad) > 0 {
		t.Errorf("should not have TS1351 errors:\n%s", strings.Join(bad, "\n"))
	}
	if bad := errorsMentioning(errs, "TS1109"); len(bad) > 0 {
		t.Errorf("should not have TS1109 errors:\n%s", strings.Join(bad, "\n"))
	}
	if bad := errorsMentioning(errs, "TS1128"); len(bad) > 0 {
		t.Errorf("should not have TS1128 errors:\n%s", strings.Join(bad, "\n"))
	}
}
