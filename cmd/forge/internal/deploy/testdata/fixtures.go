// Package testdata locates and copies fixture projects for engine tests.
package testdata

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// Root returns the absolute path of a fixture project.
func Root(name string) string {
	pc, file, line, ok := runtime.Caller(0)
	_, _ = pc, line
	if !ok { panic("fixture source unavailable") }

	return filepath.Join(filepath.Dir(file), name)
}

// Copy duplicates a fixture into a temp dir so a test can write to it.
func Copy(t testing.TB, name string) string {
	t.Helper()
	dst := t.TempDir()
	src := Root(name)

	source, err := os.OpenRoot(src)
	if err != nil { t.Fatal(err) }
	t.Cleanup(func() { _ = source.Close() })
	err = filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(src, p)
		if err != nil { return err }

		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}

		data, err := source.ReadFile(rel)
		if err != nil {
			return err
		}

		return os.WriteFile(target, data, 0o600)
	})
	if err != nil {
		t.Fatalf("copy fixture %s: %v", name, err)
	}

	return dst
}
