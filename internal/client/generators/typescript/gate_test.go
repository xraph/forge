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

func TestWSSSEFixtureEmitsStreamingFiles(t *testing.T) {
	var fixture gateFixture

	for _, f := range gateFixtures() {
		if f.Name == "ws-sse" {
			fixture = f
		}
	}

	if fixture.Name == "" {
		t.Fatal("ws-sse fixture not found in gateFixtures()")
	}

	out, err := NewGenerator().Generate(context.Background(), fixture.Spec, fixture.Config)
	if err != nil {
		t.Fatal(err)
	}

	if _, ok := out.Files["src/websocket.ts"]; !ok {
		t.Error("expected src/websocket.ts to be emitted by the ws-sse fixture")
	}

	if _, ok := out.Files["src/sse.ts"]; !ok {
		t.Error("expected src/sse.ts to be emitted by the ws-sse fixture")
	}
}

func TestFetchClientCombinesSignalsAndThrowsErrors(t *testing.T) {
	code := NewFetchClientGenerator().GenerateBaseClient(baseSpec(), baseConfig())

	if strings.Contains(code, "requestConfig.signal || controller.signal") {
		t.Error("a caller-supplied signal must not replace the timeout signal")
	}

	if !strings.Contains(code, "class HTTPError extends Error") {
		t.Error("error responses must throw a real Error subclass")
	}

	if !strings.Contains(code, "throw new HTTPError(") {
		t.Error("handleErrorResponse must throw HTTPError")
	}

	if strings.Contains(code, ": requestConfig.signal\n") {
		t.Error("the AbortSignal.any-unavailable fallback must not yield the caller's signal alone; that silently disables the timeout")
	}

	if !strings.Contains(code, "forwardAbort") {
		t.Error("expected a manual signal-forwarding fallback (forwardAbort) for runtimes without AbortSignal.any")
	}

	if strings.Count(code, "combineSignals") == 0 {
		t.Error("expected a combineSignals helper")
	}

	if !strings.Contains(code, "dispose: () => void") {
		t.Error("combineSignals must return a disposable pair, not a bare signal, so fallback listeners can be removed")
	}

	if !strings.Contains(code, "combined.dispose()") {
		t.Error("expected the caller to dispose of the combined signal wherever the timeout is cleared")
	}

	if got := strings.Count(code, "combined.dispose()"); got < 2 {
		t.Errorf("expected combined.dispose() on both the success and error exit paths, found %d occurrence(s)", got)
	}

	if strings.Contains(code, "{ once: true }") {
		t.Error("fallback abort listeners must be removed explicitly via dispose, not left to { once: true } which leaks on non-abort exits")
	}
}
