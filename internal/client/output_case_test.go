package client_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xraph/forge/internal/client"
	"github.com/xraph/forge/internal/client/generators"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func dirNames(t *testing.T, dir string) []string {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}

	return names
}

// caseInsensitive reports whether dir's disk folds case, the default on macOS
// and Windows.
func caseInsensitive(t *testing.T, dir string) bool {
	t.Helper()

	writeFile(t, filepath.Join(dir, "probe"), "")
	defer os.Remove(filepath.Join(dir, "probe"))

	_, err := os.Stat(filepath.Join(dir, "PROBE"))

	return err == nil
}

// The exact sequence Wakflo hit on a Mac: a codec module left on disk under
// another spelling, then a run that writes the real one. The write landed in
// the old file under the old name, the prune deleted that name as stale, and
// the run finished with no ShareResponse codec at all. On a case-sensitive
// disk the old code passes this too; on a Mac it does not.
func TestWriteClientReplacesAStaleSpelling(t *testing.T) {
	out := t.TempDir()
	writeFile(t, filepath.Join(out, "src/codecs/shareResponse.ts"), "stale\n")

	gen := &generators.GeneratedClient{
		Files: map[string]string{
			"src/codecs/ShareResponse.ts":   "export const codec_ShareResponse = 1;\n",
			"src/codecs/shareResponse-2.ts": "export const codec_shareResponse = 2;\n",
		},
		ExclusiveDirs: []string{"src/codecs"},
	}

	for run := 1; run <= 2; run++ {
		if err := client.NewOutputManager().WriteClient(gen, out); err != nil {
			t.Fatalf("run %d: WriteClient: %v", run, err)
		}

		got := strings.Join(dirNames(t, filepath.Join(out, "src/codecs")), " ")
		if got != "ShareResponse.ts shareResponse-2.ts" {
			t.Fatalf("run %d: codecs dir holds %q", run, got)
		}

		data, err := os.ReadFile(filepath.Join(out, "src/codecs/ShareResponse.ts"))
		if err != nil {
			t.Fatal(err)
		}

		if string(data) != gen.Files["src/codecs/ShareResponse.ts"] {
			t.Fatalf("run %d: ShareResponse.ts holds %q", run, data)
		}
	}
}

// Clearing a stale spelling must never reach a file that only shares a
// spelling. On a case-sensitive disk `Types.ts` next to a generated
// `types.ts` is a separate file, and outside an exclusive directory it is not
// the generator's to delete.
func TestWriteClientKeepsADistinctFileOnACaseSensitiveDisk(t *testing.T) {
	out := t.TempDir()
	if caseInsensitive(t, out) {
		t.Skip("this disk folds case, so Types.ts and types.ts are one file here")
	}

	writeFile(t, filepath.Join(out, "src/Types.ts"), "mine\n")
	writeFile(t, filepath.Join(out, "src/types.ts"), "old\n")

	gen := &generators.GeneratedClient{Files: map[string]string{"src/types.ts": "new\n"}}
	if err := client.NewOutputManager().WriteClient(gen, out); err != nil {
		t.Fatalf("WriteClient: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(out, "src/Types.ts"))
	if err != nil || string(data) != "mine\n" {
		t.Fatalf("Types.ts was touched: %q, %v", data, err)
	}
}

// Two generated paths that differ only by case are one file on a Mac, so the
// tree would depend on which was written last. The writer refuses before it
// touches the disk.
func TestWriteClientRefusesFilesThatDifferOnlyByCase(t *testing.T) {
	out := filepath.Join(t.TempDir(), "client")

	gen := &generators.GeneratedClient{Files: map[string]string{
		"src/codecs/ShareResponse.ts": "a\n",
		"src/codecs/shareResponse.ts": "b\n",
	}}

	err := client.NewOutputManager().WriteClient(gen, out)
	if err == nil || !strings.Contains(err.Error(), "differ only by case") {
		t.Fatalf("WriteClient error = %v, want a case collision", err)
	}

	if _, statErr := os.Stat(out); !os.IsNotExist(statErr) {
		t.Fatalf("WriteClient created %s before refusing: %v", out, statErr)
	}
}
