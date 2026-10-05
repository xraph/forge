package dart

import (
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

var (
	dartTypeDecl   = regexp.MustCompile(`(?m)^(?:(?:abstract|sealed|final|base|interface|mixin) )*(?:class|mixin class|enum|typedef|extension type(?: const)?|mixin) +([A-Z][A-Za-z0-9_]*)`)
	dartExportStmt = regexp.MustCompile(`(?s)\bexport\s+'([^']+)'([^;]*);`)
	dartQuoted     = regexp.MustCompile(`'([^']+)'`)
	dartCombinator = regexp.MustCompile(`\b(show|hide)\s+([A-Za-z0-9_,\s]+)$`)
)

// forgeClientExports lists the type names package:forge_client exports,
// found by walking the barrel's export graph the way the analyzer does: every
// export (both arms of a conditional export) is followed, and a show or hide
// combinator narrows what a file contributes.
func forgeClientExports(t *testing.T) map[string]bool {
	t.Helper()

	lib := filepath.Join(forgeClientDir(t), "lib")
	out := map[string]bool{}
	visiting := map[string]bool{}

	var exported func(rel string) map[string]bool

	exported = func(rel string) map[string]bool {
		names := map[string]bool{}
		if visiting[rel] {
			return names
		}

		visiting[rel] = true
		defer delete(visiting, rel)

		raw, err := os.ReadFile(filepath.Join(lib, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}

		text := string(raw)

		for _, m := range dartTypeDecl.FindAllStringSubmatch(text, -1) {
			names[m[1]] = true
		}

		for _, m := range dartExportStmt.FindAllStringSubmatch(text, -1) {
			if strings.HasPrefix(m[1], "package:") || strings.HasPrefix(m[1], "dart:") {
				continue
			}

			tail := m[2]
			targets := []string{m[1]}

			for _, q := range dartQuoted.FindAllStringSubmatch(tail, -1) {
				targets = append(targets, q[1])
			}

			tail = dartQuoted.ReplaceAllString(tail, "")

			var shown map[string]bool

			hidden := map[string]bool{}

			if c := dartCombinator.FindStringSubmatch(strings.TrimSpace(tail)); c != nil {
				list := map[string]bool{}
				for n := range strings.SplitSeq(c[2], ",") {
					list[strings.TrimSpace(n)] = true
				}

				if c[1] == "show" {
					shown = list
				} else {
					hidden = list
				}
			}

			for _, target := range targets {
				for n := range exported(path.Join(path.Dir(rel), target)) {
					if hidden[n] || (shown != nil && !shown[n]) {
						continue
					}

					names[n] = true
				}
			}
		}

		return names
	}

	for n := range exported("forge_client.dart") {
		out[n] = true
	}

	return out
}

// TestReservedIdentifiersCoverEveryForgeClientExport keeps the reserved list
// in step with the real runtime. A schema named after an export would make
// that name ambiguous in any file importing both packages, so each export is
// reserved by name, and every name reserved for it is a real export.
func TestReservedIdentifiersCoverEveryForgeClientExport(t *testing.T) {
	exports := forgeClientExports(t)
	if len(exports) < 100 {
		t.Fatalf("found only %d forge_client exports; the barrel walk is broken", len(exports))
	}

	reserved := ReservedIdentifiers()

	var missing []string

	for name := range exports {
		if !reserved[name] {
			missing = append(missing, name)
		}
	}

	sort.Strings(missing)

	if len(missing) > 0 {
		t.Errorf("forge_client exports %d types ReservedIdentifiers does not reserve: %s", len(missing), strings.Join(missing, ", "))
	}

	// The reverse: a name the list reserves for forge_client that the runtime
	// does not export is stale, and renames a schema for nothing.
	var stale []string

	for _, name := range forgeClientTypeNames {
		if !exports[name] {
			stale = append(stale, name)
		}
	}

	sort.Strings(stale)

	if len(stale) > 0 {
		t.Errorf("forgeClientTypeNames lists %d names forge_client does not export: %s", len(stale), strings.Join(stale, ", "))
	}
}

// TestEveryCollidingFixtureSchemaIsRenamedAndReported covers each schema in
// the gate fixtures that is named after a reserved identifier: it must be
// generated under a Model-suffixed name in its own file, and the rename must
// be reported.
func TestEveryCollidingFixtureSchemaIsRenamedAndReported(t *testing.T) {
	reserved := ReservedIdentifiers()
	covered := map[string]bool{}

	for _, f := range gateFixtures() {
		out := generate(t, f)
		warnings := strings.Join(out.Warnings, "\n")

		for name := range f.Spec.Schemas {
			base := typeIdent(name)
			if !reserved[base] {
				continue
			}

			covered[name] = true

			renamed := base + "Model"
			rel := "lib/src/models/" + fileStem(renamed) + ".dart"
			content := file(t, out, rel)

			if !strings.Contains(content, renamed) {
				t.Errorf("%s: schema %q: %s does not declare %s", f.Name, name, rel, renamed)
			}

			assertContains(t, f.Name+" warnings", warnings, `schema "`+name+`" is generated as `+renamed)

			if _, ok := out.Files["lib/src/models/"+fileStem(base)+".dart"]; ok {
				t.Errorf("%s: schema %q still generated an unsuffixed model file", f.Name, name)
			}
		}
	}

	// ordersSpec names Value; reservedSpec names String, NotFound, QueryState
	// and Assign. If a fixture changes, this fails rather than quietly
	// covering less.
	want := []string{"Assign", "NotFound", "QueryState", "String", "Value"}
	if got := sortedKeys(covered); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("colliding fixture schemas = %v, want %v", got, want)
	}
}

func TestNonCollidingSchemasKeepTheirNames(t *testing.T) {
	out := generate(t, fixture(t, "default"))
	assertContains(t, "order.dart", file(t, out, "lib/src/models/order.dart"), "final class Order {")

	for _, w := range out.Warnings {
		if strings.Contains(w, `schema "Order"`) {
			t.Errorf("Order does not collide with anything but was reported: %s", w)
		}
	}
}
