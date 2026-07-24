package typescript

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func writeTree(t *testing.T, dir string, files map[string]string) {
	t.Helper()

	for name, content := range files {
		full := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

const probeTSConfig = `{
  "compilerOptions": {
    "target": "ES2020",
    "module": "ESNext",
    "lib": ["ES2020", "DOM"],
    "strict": true,
    "moduleResolution": "bundler",
    "noEmit": true
  },
  "include": ["src/**/*"]
}
`

func TestTypeCheckAcceptsValidTypeScript(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"tsconfig.json": probeTSConfig,
		"src/a.ts":      "export const n: number = 1;\n",
	})

	if errs := typeCheck(t, dir); len(errs) != 0 {
		t.Fatalf("expected valid TypeScript to compile cleanly, got:\n%v", errs)
	}
}

func TestTypeCheckRejectsInvalidTypeScript(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"tsconfig.json": probeTSConfig,
		"src/a.ts":      "export const n: number = 'not a number';\n",
	})

	errs := typeCheck(t, dir)
	if len(errs) == 0 {
		t.Fatal("expected a type error, got none")
	}
}

// findTSC returns the argv prefix used to invoke the TypeScript compiler, or
// skips the test when no compiler is available. CI installs Node so the gate is
// live there; local runs without Node degrade to a skip rather than a failure.
func findTSC(t *testing.T) []string {
	t.Helper()

	if path, err := exec.LookPath("tsc"); err == nil {
		return []string{path}
	}

	if path, err := exec.LookPath("npx"); err == nil {
		return []string{path, "--no-install", "tsc"}
	}

	t.Skip("neither tsc nor npx found on PATH; skipping TypeScript type check")

	return nil
}

// typeCheck runs tsc against dir and returns one entry per reported error.
func typeCheck(t *testing.T, dir string) []string {
	t.Helper()

	argv := findTSC(t)
	argv = append(argv, "--noEmit", "-p", "tsconfig.json")

	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = dir

	out, err := cmd.CombinedOutput()
	if err == nil {
		return nil
	}

	var errs []string

	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, "error TS") {
			errs = append(errs, strings.TrimSpace(line))
		}
	}

	// tsc exited non-zero but emitted nothing parseable: surface it verbatim so
	// a broken toolchain is not mistaken for a clean run.
	if len(errs) == 0 {
		t.Fatalf("tsc failed with no parseable diagnostics: %v\n%s", err, out)
	}

	return errs
}
