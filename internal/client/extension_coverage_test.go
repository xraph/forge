package client

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// Every x-forge extension the generator READS has to answer one question: what
// happens when a document declares it and the value cannot be used?
//
// Three answers are acceptable and one is not. Warning and dropping is the
// default. Being emit-only, never read, needs no answer at all. A known gap is
// tolerable when it is written down with the reason. Dropping in silence is
// the one that keeps costing us: a declaration the generator ignores produces
// a client that compiles, runs, and quietly does not do what the document
// asked, discovered weeks later as cache behaviour nobody connects back to a
// typo in a spec.
//
// So this test reads the source rather than a list someone maintains by hand.
// Add an extension and forget to decide, and the build tells you here instead
// of a user telling you in six months.
type extensionStatus int

const (
	// warnsOnUnusable: read, and a malformed value produces a spec.Warnings
	// entry. Covered by a test in introspector_client_meta_test.go.
	warnsOnUnusable extensionStatus = iota
	// emitOnly: written into documents, never read back by the generator, so
	// there is no failure path to guard.
	emitOnly
	// knownGap: read without warning, deliberately, with the reason recorded.
	knownGap
)

var extensionDecisions = map[string]struct {
	status extensionStatus
	why    string
}{
	"x-forge-stale-time":      {warnsOnUnusable, "introspector.go, warns on a write and on an unusable value"},
	"x-forge-no-entity":       {warnsOnUnusable, "boolExtension"},
	"x-forge-entity":          {warnsOnUnusable, "mapExtension, plus an incomplete-declaration warning"},
	"x-forge-invalidates":     {warnsOnUnusable, "stringSliceExtension, whole value and per element"},
	"x-forge-no-invalidation": {warnsOnUnusable, "stringSliceExtension"},

	"x-forge-authz":    {emitOnly, "read by authz.go into an optional struct; a malformed value degrades to unguarded, which authz.go documents as its deliberate posture"},
	"x-forge-stream":   {emitOnly, "read into stream bindings; a malformed value yields no channels, and the AsyncAPI document that carries it is generated rather than hand-written"},
	"x-forge-envelope": {emitOnly, "read by envelope.go, which distinguishes declared-from-absent itself and has its own refusal cases"},

	"x-forge-id": {knownGap, "read in entity.go's isMarkedIdentityField, which takes no *APISpec and is reached through anyMatch. Threading a spec through that call chain is a wider change than the warning is worth today. A wrong type here comes only from a hand-written document, since the generator emits a bool."},
}

// extensionNamePattern matches the literal names, wherever they are written.
var extensionNamePattern = regexp.MustCompile(`x-forge-[a-z-]+`)

func TestEveryForgeExtensionHasADecisionAboutMalformedInput(t *testing.T) {
	found := map[string]string{}

	for _, dir := range []string{".", "../router"} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("ReadDir(%s): %v", dir, err)
		}

		for _, entry := range entries {
			name := entry.Name()

			if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}

			body, err := os.ReadFile(filepath.Join(dir, name))
			if err != nil {
				t.Fatalf("ReadFile(%s): %v", name, err)
			}

			for _, match := range extensionNamePattern.FindAllString(string(body), -1) {
				found[match] = filepath.Join(dir, name)
			}
		}
	}

	if len(found) == 0 {
		t.Fatal("found no x-forge-* names at all; the scan is broken, not the code")
	}

	undecided := make([]string, 0)

	for name, where := range found {
		if _, ok := extensionDecisions[name]; !ok {
			undecided = append(undecided, name+" (in "+where+")")
		}
	}

	sort.Strings(undecided)

	if len(undecided) > 0 {
		t.Fatalf(
			"these x-forge extensions have no recorded decision about a malformed value:\n  %s\n\n"+
				"Add each to extensionDecisions. If the generator reads it, warn through one of the\n"+
				"typed readers in introspector.go and mark it warnsOnUnusable with a test. If nothing\n"+
				"reads it, mark it emitOnly. If it is read without a warning on purpose, mark it\n"+
				"knownGap and say why. Silently dropping a declaration is the one option that is not\n"+
				"available.",
			strings.Join(undecided, "\n  "))
	}

	// The other direction: a decision left behind for an extension that no
	// longer exists is a comment describing code that is gone.
	stale := make([]string, 0)

	for name := range extensionDecisions {
		if _, ok := found[name]; !ok {
			stale = append(stale, name)
		}
	}

	sort.Strings(stale)

	if len(stale) > 0 {
		t.Fatalf("extensionDecisions names extensions that no longer appear in the source: %s",
			strings.Join(stale, ", "))
	}
}
