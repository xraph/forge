package portgen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestApplyFailsLoudlyWhenAnAnchorIsMissingOrAmbiguous(t *testing.T) {
	cases := map[string]edit{
		"missing":   {old: "absent", new: "x", count: 1},
		"ambiguous": {old: "a", new: "x", count: 1},
		"regex":     {old: `\bTS:`, new: "x", regex: true, count: 1},
	}

	for name, e := range cases {
		if _, err := apply("f.go", "a a", e); err == nil {
			t.Errorf("%s: apply must report the anchor mismatch", name)
		}
	}

	got, err := apply("f.go", "one two", edit{old: "two", new: "2", count: 1})
	if err != nil || got != "one 2" {
		t.Errorf("apply = %q, %v", got, err)
	}
}

func TestGenerateFailsWhenADeclarationDisappears(t *testing.T) {
	dir := t.TempDir()

	for _, tg := range targets {
		for _, src := range tg.sources {
			if err := os.WriteFile(filepath.Join(dir, src.file), []byte("package typescript\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}

	_, err := Generate(dir)
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("Generate = %v, want a declaration-not-found error", err)
	}
}

func TestDeclarationsKeepDocCommentsAndRejectDuplicates(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.go")

	src := "package p\n\n// doc line\nfunc one() {}\n\n// T is a type.\ntype T struct{}\n\nconst (\n\tx = 1\n\ty = 2\n)\n"
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := declarations(path)
	if err != nil {
		t.Fatal(err)
	}

	if got["one"] != "// doc line\nfunc one() {}" || got["T"] != "// T is a type.\ntype T struct{}" {
		t.Errorf("declarations = %q", got)
	}

	if !strings.Contains(got["x"], "y = 2") {
		t.Errorf("a grouped const is keyed by its first name and keeps the group: %q", got["x"])
	}

	if err := os.WriteFile(path, []byte("package p\nfunc one() {}\nfunc one2() {}\ntype one struct{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := declarations(path); err == nil {
		t.Error("a name declared twice must be an error")
	}
}
