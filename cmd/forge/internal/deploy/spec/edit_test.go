package spec

import (
	"errors"
	"os"
	"strings"
	"testing"
)

const commented = `# project comment
project:
  name: a   # inline
deploy:
  version: 2
  # keep me
  registry: ghcr.io/x
  services:
    api: &api
      app: api
      kind: web
      ports: {http: {port: 8080}}
    api2: *api
  custom_unknown_kept: {a: 1}
`

func TestPatchKeepsCommentsAnchorsAndUnknownKeys(t *testing.T) {
	dir := t.TempDir()
	p := write(t, dir, ".forge.yml", commented)
	doc, _, _ := Parse(p) // unknown key diag is expected; Patch must still work

	out, err := doc.Patch([]Op{{Path: "deploy.environments.dev.target", Value: "local"}, {Path: "deploy.registry", Value: "ghcr.io/y"}})
	if err != nil {
		t.Fatal(err)
	}

	got := string(out[p])
	for _, want := range []string{"# project comment", "# inline", "# keep me", "&api", "*api", "custom_unknown_kept", "registry: ghcr.io/y", "environments:\n    dev:\n      target: local"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}

	if strings.Contains(got, "ghcr.io/x") {
		t.Fatal("old value survived")
	}
}

func TestPatchDeleteRemovesPair(t *testing.T) {
	dir := t.TempDir()
	p := write(t, dir, ".forge.yml", commented)
	doc, _, _ := Parse(p)

	out, _ := doc.Patch([]Op{{Path: "deploy.registry", Delete: true}})
	if strings.Contains(string(out[p]), "registry") {
		t.Fatal("registry should be gone")
	}
}

func TestPatchPreservesCRLFAndBOM(t *testing.T) {
	dir := t.TempDir()
	body := "\xef\xbb\xbfproject:\r\n  name: a\r\ndeploy:\r\n  version: 2\r\n"
	p := write(t, dir, ".forge.yml", body)
	doc, _, _ := Parse(p)
	out, _ := doc.Patch([]Op{{Path: "deploy.registry", Value: "r"}})

	got := string(out[p])
	if !strings.HasPrefix(got, "\xef\xbb\xbf") || strings.Contains(got, "\n") && !strings.Contains(got, "\r\n") {
		t.Fatalf("BOM or CRLF lost:\n%q", got)
	}
}

func TestWriteAtomicAndConflict(t *testing.T) {
	dir := t.TempDir()
	p := write(t, dir, ".forge.yml", "a: 1\n")

	doc, _, _ := Parse(p)
	if err := Write(p, doc.Hash, []byte("a: 2\n")); err != nil {
		t.Fatal(err)
	}

	if err := Write(p, doc.Hash, []byte("a: 3\n")); !errors.Is(err, ErrConflict) {
		t.Fatalf("expected ErrConflict, got %v", err)
	}

	data, _ := os.ReadFile(p)
	if string(data) != "a: 2\n" {
		t.Fatalf("conflicting write landed: %q", data)
	}

	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("temp file left behind: %v", entries)
	}
}

func TestPatchOrdersDeployKeys(t *testing.T) {
	dir := t.TempDir()
	p := write(t, dir, ".forge.yml", "project: {name: a}\n")
	doc, _, _ := Parse(p)
	block := map[string]any{"services": map[string]any{"api": map[string]any{"kind": "web", "app": "api"}}, "version": 2, "targets": map[string]any{"local": map[string]any{"provider": "compose"}}}
	out, _ := doc.Patch([]Op{{Path: "deploy", Value: block}})

	got := string(out[p])
	if strings.Index(got, "version: 2") > strings.Index(got, "services:") || strings.Index(got, "app: api") > strings.Index(got, "kind: web") {
		t.Fatalf("keys not in preferred order:\n%s", got)
	}
}
