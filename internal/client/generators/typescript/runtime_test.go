package typescript

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"
)

// findESBuild returns the argv prefix used to bundle generated TypeScript
// into a single runnable ESM file, or skips the test when no bundler is
// available. Bundling (rather than running .ts files directly under Node's
// native TS support) is required because the generated tsconfig.json uses
// "moduleResolution": "bundler" and every generated import is intentionally
// extensionless (e.g. `from './fetch'`) — Node's own module resolver cannot
// resolve that without a bundler doing the same resolution tsc does.
func findESBuild(t *testing.T) []string {
	t.Helper()

	if path, err := exec.LookPath("esbuild"); err == nil {
		return []string{path}
	}

	if path, err := exec.LookPath("npx"); err == nil {
		return []string{path, "--no-install", "esbuild"}
	}

	t.Skip("neither esbuild nor npx found on PATH; skipping generated-client runtime test")

	return nil
}

// findNode returns the path to a Node.js binary, or skips the test when none
// is available.
func findNode(t *testing.T) string {
	t.Helper()

	path, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found on PATH; skipping generated-client runtime test")
	}

	return path
}

// runNodeDriver bundles entry (a path relative to dir, e.g. "src/__driver.ts")
// with esbuild into a single Node-runnable ESM file and executes it under
// Node, returning stdout. It fails the test outright on any bundling or
// runtime error — a thrown error or non-zero exit means the driver script
// itself is broken, not that the assertion it encodes failed, so that must
// not be mistaken for a passing (or even a meaningfully failing) assertion.
//
// This exists to verify actual runtime behavior of generated code — e.g.
// that a declared `Promise<T | void>` return type is honored by what the
// generated fetch client actually resolves with for an empty-bodied
// response — which `tsc --noEmit` cannot check, since tsc never executes
// anything.
func runNodeDriver(t *testing.T, dir, entry string) string {
	t.Helper()

	esbuildArgv := findESBuild(t)
	nodePath := findNode(t)

	outFile := filepath.Join(dir, "__bundle.mjs")

	args := append(append([]string{}, esbuildArgv[1:]...),
		entry,
		"--bundle",
		"--platform=node",
		"--format=esm",
		"--outfile="+outFile,
	)

	cmd := exec.CommandContext(context.Background(), esbuildArgv[0], args...)
	cmd.Dir = dir

	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("esbuild failed to bundle %s: %v\n%s", entry, err, out)
	}

	runCmd := exec.CommandContext(context.Background(), nodePath, outFile)
	runCmd.Dir = dir

	out, err := runCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("node execution of bundled %s failed: %v\n%s", entry, err, out)
	}

	return string(out)
}
