package render

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestHashesAreStable(t *testing.T) {
	a := New("local", "dev")
	a.Add("compose.yaml", []byte("x: 1\n"))

	b := New("local", "dev")
	b.Add("compose.yaml", []byte("x: 1\n"))

	if a.Hashes()["compose.yaml"] != b.Hashes()["compose.yaml"] {
		t.Fatal("hash differs for identical content")
	}
}

func TestWriteRefreshesAndKeepsPatches(t *testing.T) {
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, "patches"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "patches", "mine.yaml"), []byte("keep"), 0o644)
	b := New("local", "dev")
	b.Add("compose.yaml", []byte("v1\n"))
	b.Add("api/Dockerfile", []byte("FROM x\n"))

	res, err := Write(dir, b, WriteOptions{})
	if err != nil || len(res.Written) != 2 {
		t.Fatalf("%+v %v", res, err)
	}

	var m Manifest

	data, _ := os.ReadFile(filepath.Join(dir, "forge-manifest.json"))

	_ = json.Unmarshal(data, &m)
	if len(m.Files) != 2 || m.Target != "local" {
		t.Fatalf("%+v", m)
	}
	// User edits a generated file; the next write without Force must skip it.
	_ = os.WriteFile(filepath.Join(dir, "compose.yaml"), []byte("edited\n"), 0o644)
	b2 := New("local", "dev")
	b2.Add("compose.yaml", []byte("v2\n"))

	res, _ = Write(dir, b2, WriteOptions{})
	if len(res.Skipped) != 1 || res.Skipped[0] != "compose.yaml" {
		t.Fatalf("%+v", res)
	}

	if _, err := os.Stat(filepath.Join(dir, "api", "Dockerfile")); err == nil {
		t.Fatal("file absent from the new bundle must be deleted")
	}

	if data, _ := os.ReadFile(filepath.Join(dir, "patches", "mine.yaml")); string(data) != "keep" {
		t.Fatal("patches must survive")
	}

	res, _ = Write(dir, b2, WriteOptions{Force: true})
	if len(res.Written) != 1 {
		t.Fatalf("force must overwrite: %+v", res)
	}
}

func TestSecretModeFiles(t *testing.T) {
	dir := t.TempDir()
	b := New("local", "dev")
	b.AddMode("generated.env", []byte("X=1\n"), 0o600)

	if _, err := Write(dir, b, WriteOptions{}); err != nil {
		t.Fatal(err)
	}

	info, _ := os.Stat(filepath.Join(dir, "generated.env"))
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", info.Mode())
	}
}

func TestWritePreservesUntrackedAndEditedObsoleteFiles(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "user.yaml"), []byte("user"), 0600)
	b := New("local", "dev")
	b.Add("user.yaml", []byte("generated"))
	b.Add("old.yaml", []byte("first"))

	res, err := Write(dir, b, WriteOptions{})
	if err != nil || len(res.Skipped) != 1 {
		t.Fatalf("untracked: %+v %v", res, err)
	}

	_ = os.WriteFile(filepath.Join(dir, "old.yaml"), []byte("edited"), 0600)
	b2 := New("local", "dev")

	_, err = Write(dir, b2, WriteOptions{})
	if err != nil {
		t.Fatal(err)
	}

	if raw, err := os.ReadFile(filepath.Join(dir, "old.yaml")); err != nil || string(raw) != "edited" {
		t.Fatal("edited obsolete file removed")
	}
}
func TestWriteRejectsTraversalAndSymlinks(t *testing.T) {
	dir := t.TempDir()
	outside := t.TempDir()

	_ = os.WriteFile(filepath.Join(outside, "config"), []byte("user"), 0600)
	if err := os.Symlink(outside, filepath.Join(dir, "linked")); err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{"../escape", "linked/config", "forge-manifest.json"} {
		b := New("local", "dev")
		b.Add(path, []byte("bad"))

		if _, err := Write(dir, b, WriteOptions{Force: true}); err == nil {
			t.Fatalf("accepted %s", path)
		}
	}
}

func TestExportLeavesUnownedTemporaryFileAlone(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "compose.yaml.forge-tmp")
	_ = os.WriteFile(path, []byte("user data"), 0600)
	b := New("local", "dev")
	b.Add("compose.yaml", []byte("services: {}"))

	if _, err := Write(root, b, WriteOptions{}); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(path)
	if err != nil || string(raw) != "user data" {
		t.Fatal("unowned temporary file overwritten", err)
	}
}
