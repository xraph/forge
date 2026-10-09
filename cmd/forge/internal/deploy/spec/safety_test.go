package spec

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseRejectsSplitOutsideProject(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "project")
	write(t, parent, "outside.yml", "services: {}\n")
	p := write(t, root, ".forge.yml", "deploy: {version: 2, spec: ../outside.yml}\n")

	_, diagnostics, err := Parse(p)
	if err != nil || !diagnostics.HasErrors() {
		t.Fatalf("outside project split accepted: %v %v", diagnostics, err)
	}
}

func TestPatchPreviewDoesNotMutateDocument(t *testing.T) {
	p := write(t, t.TempDir(), ".forge.yml", "project: {name: atlas}\ndeploy: {version: 2, registry: original}\n")

	doc, _, _ := Parse(p)
	if _, err := doc.Patch([]Op{{Path: "deploy.registry", Value: "preview"}}); err != nil {
		t.Fatal(err)
	}

	out, err := doc.Patch([]Op{{Path: "deploy.defaults.target", Value: "local"}})
	if err != nil || strings.Contains(string(out[p]), "preview") {
		t.Fatalf("preview contaminated later edits: %s %v", out[p], err)
	}
}

func TestPatchMissingDeleteDoesNotRemoveSibling(t *testing.T) {
	p := write(t, t.TempDir(), ".forge.yml", "project: {name: atlas}\n")
	doc, _, _ := Parse(p)

	out, err := doc.Patch([]Op{{Path: "missing.project", Delete: true}})
	if err != nil || !strings.Contains(string(out[p]), "name: atlas") {
		t.Fatalf("missing delete removed sibling: %s %v", out[p], err)
	}
}

func TestPatchSequenceItem(t *testing.T) {
	p := write(t, t.TempDir(), ".forge.yml", "deploy: {version: 2, services: {api: {config: [first.yaml, second.yaml]}}}\n")
	doc, _, _ := Parse(p)

	out, err := doc.Patch([]Op{{Path: "deploy.services.api.config.1", Value: "third.yaml"}})
	if err != nil || !strings.Contains(string(out[p]), "third.yaml") || strings.Contains(string(out[p]), "!!seq") {
		t.Fatalf("sequence edit failed: %s %v", out[p], err)
	}

	if err := os.WriteFile(p, out[p], 0o600); err != nil {
		t.Fatal(err)
	}

	next, diagnostics, err := Parse(p)
	if err != nil || diagnostics.HasErrors() || len(next.Deploy.Services["api"].Config) != 2 {
		t.Fatalf("sequence corrupted: %v %v", diagnostics, err)
	}
}

func TestWriteDeletedInputConflicts(t *testing.T) {
	p := write(t, t.TempDir(), ".forge.yml", "project: {name: atlas}\n")

	doc, _, _ := Parse(p)
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}

	if err := Write(p, doc.Hash, []byte("project: {name: other}\n")); !errors.Is(err, ErrConflict) {
		t.Fatalf("deleted input recreated: %v", err)
	}
}

func TestWriteInvalidExpectedHashDoesNotPanic(t *testing.T) {
	p := write(t, t.TempDir(), ".forge.yml", "project: {name: atlas}\n")
	if err := Write(p, "bad", []byte("project: {}\n")); !errors.Is(err, ErrConflict) {
		t.Fatalf("invalid expected hash accepted: %v", err)
	}
}

func TestValidateSplitUsesSourceLocation(t *testing.T) {
	root := t.TempDir()
	p := write(t, root, ".forge.yml", "deploy: {version: 2, spec: stack.yml}\n")
	split := write(t, root, "stack.yml", "services:\n  api:\n    app: missing\n    kind: web\n    ports: {http: {port: 8080}}\n")

	doc, diagnostics, err := Parse(p)
	if err != nil || diagnostics.HasErrors() {
		t.Fatalf("parse: %v %v", diagnostics, err)
	}

	for _, d := range Validate(doc, []string{"gateway"}) {
		if d.Field == "deploy.services.api.app" {
			if d.File != split || d.Line != 3 {
				t.Fatalf("source = %s:%d", d.File, d.Line)
			}

			return
		}
	}

	t.Fatal("missing app diagnostic")
}
