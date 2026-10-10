package persistence

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestConfigureCopiesStateAndMakesDatabaseAuthoritative(t *testing.T) {
	root := t.TempDir()

	raw := []byte("project: {name: atlas, module: example.com/atlas}\ndeploy: {version: 2}\n")
	if err := os.WriteFile(filepath.Join(root, ".forge.yml"), raw, 0600); err != nil {
		t.Fatal(err)
	}

	dir := filepath.Join(root, ".forge/state/local/dev")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(dir, "snapshot.json"), []byte(`{"revision":7}`), 0600); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(dir, "journal.jsonl"), []byte("event\n"), 0600); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(dir, "generated.env"), []byte("TOKEN=private"), 0600); err != nil {
		t.Fatal(err)
	}

	if err := Configure(context.Background(), root, Options{Backend: "sqlite", Reference: ".forge/deploy.db"}, Hash(raw)); err != nil {
		t.Fatal(err)
	}

	options, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}

	db, err := Open(context.Background(), root, options)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	_, actual, err := db.Settings(context.Background())
	if err != nil || string(actual) != string(raw) {
		t.Fatal("settings not copied", err)
	}

	blob, err := db.Read(context.Background(), "local/dev", "snapshot.json")
	if err != nil || string(blob) != `{"revision":7}` {
		t.Fatal("state not copied", err)
	}

	if _, err := db.Read(context.Background(), "local/dev", "generated.env"); !os.IsNotExist(err) {
		t.Fatal("credentials copied into database", err)
	}

	if err := db.SaveSettings(context.Background(), 1, []byte("project: {name: atlas}\ndeploy: {version: 2, registry: ghcr.io/changed}\n")); err != nil {
		t.Fatal(err)
	}

	if err := Configure(context.Background(), root, Options{Backend: "files"}, Hash(raw)); !errors.Is(err, ErrConflict) {
		t.Fatal("stale switch accepted", err)
	}

	options, err = Load(root)
	if err != nil || options.Backend != "sqlite" {
		t.Fatal("failed switch changed authority", options, err)
	}
}
func TestUnavailableDatabaseDoesNotFallBack(t *testing.T) {
	root := t.TempDir()

	raw := []byte("project: {name: atlas}\ndeploy: {version: 2}\n")
	if err := os.WriteFile(filepath.Join(root, ".forge.yml"), raw, 0600); err != nil {
		t.Fatal(err)
	}

	if err := Configure(context.Background(), root, Options{Backend: "sqlite", Reference: ".forge/deploy.db"}, Hash(raw)); err != nil {
		t.Fatal(err)
	}

	if err := os.Remove(filepath.Join(root, ".forge/deploy.db")); err != nil {
		t.Fatal(err)
	}

	if _, _, _, err := Document(context.Background(), root); err == nil {
		t.Fatal("missing database silently became fresh authority")
	}
}
func TestSQLiteSymlinkRejected(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".forge"), 0700); err != nil {
		t.Fatal(err)
	}

	if err := os.Symlink(filepath.Join(t.TempDir(), "outside.db"), filepath.Join(root, ".forge/deploy.db")); err != nil {
		t.Fatal(err)
	}

	if db, err := Open(context.Background(), root, Options{Backend: "sqlite", Reference: ".forge/deploy.db", Project: "atlas"}); err == nil {
		_ = db.Close()

		t.Fatal("symlink SQLite authority accepted")
	}
}

func TestSwitchSourceVerificationRejectsChangedMetadata(t *testing.T) {
	root := t.TempDir()

	raw := []byte("project: {name: atlas}\ndeploy: {version: 2}\n")
	if err := os.WriteFile(filepath.Join(root, ".forge.yml"), raw, 0600); err != nil {
		t.Fatal(err)
	}

	dir := filepath.Join(root, ".forge/state/local/dev")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dir, "snapshot.json")
	if err := os.WriteFile(path, []byte(`{"revision":1}`), 0600); err != nil {
		t.Fatal(err)
	}

	blobs, err := fileBlobs(root)
	if err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(path, []byte(`{"revision":2}`), 0600); err != nil {
		t.Fatal(err)
	}

	if err := verifyFileSource(root, Hash(raw), blobs); !errors.Is(err, ErrConflict) {
		t.Fatal("changed source snapshot accepted", err)
	}
}
func TestSQLiteRejectsURICharactersBeforeCreatingFiles(t *testing.T) {
	root := t.TempDir()
	options := Options{Backend: "sqlite", Reference: ".forge/store?cache=shared.db", Project: "atlas"}

	db, err := Open(context.Background(), root, options)
	if err == nil {
		_ = db.Close()

		t.Fatal("URI parameters accepted as a database filename")
	}

	if _, err := os.Stat(filepath.Join(root, options.Reference)); !os.IsNotExist(err) {
		t.Fatal("invalid filename was created", err)
	}
}

func TestStoreRoundTripPreservesAuthoritativeHistory(t *testing.T) {
	root := t.TempDir()

	raw := []byte("project: {name: atlas}\ndeploy: {version: 2}\n")
	if err := os.WriteFile(filepath.Join(root, ".forge.yml"), raw, 0600); err != nil {
		t.Fatal(err)
	}

	options := Options{Backend: "sqlite", Reference: ".forge/deploy.db"}
	if err := Configure(context.Background(), root, options, Hash(raw)); err != nil {
		t.Fatal(err)
	}

	db, err := OpenSelected(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}

	lease, err := db.Acquire(context.Background(), "local/dev")
	if err != nil {
		t.Fatal(err)
	}

	if err := lease.Write(context.Background(), "snapshot.json", []byte(`{"revision":8}`)); err != nil {
		t.Fatal(err)
	}

	if err := lease.Write(context.Background(), "journal.jsonl", []byte("approved\\n")); err != nil {
		t.Fatal(err)
	}

	if err := lease.Release(); err != nil {
		t.Fatal(err)
	}

	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	if err := Configure(context.Background(), root, Options{Backend: "files"}, Hash(raw)); err != nil {
		t.Fatal(err)
	}

	actual, err := os.ReadFile(filepath.Join(root, ".forge/state/local/dev/snapshot.json"))
	if err != nil || string(actual) != `{"revision":8}` {
		t.Fatal("history was lost on export", string(actual), err)
	}

	selected, err := Load(root)
	if err != nil || selected.Backend != "files" {
		t.Fatal(selected, err)
	}
}

func TestSwitchRetiresPreviousSQLAuthority(t *testing.T) {
	root := t.TempDir()

	raw := []byte("project: {name: atlas}\ndeploy: {version: 2}\n")
	if err := os.WriteFile(filepath.Join(root, ".forge.yml"), raw, 0600); err != nil {
		t.Fatal(err)
	}

	if err := Configure(context.Background(), root, Options{Backend: "sqlite", Reference: ".forge/old.db"}, Hash(raw)); err != nil {
		t.Fatal(err)
	}

	old, err := OpenSelected(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	defer old.Close()

	if err := Configure(context.Background(), root, Options{Backend: "files"}, Hash(raw)); err != nil {
		t.Fatal(err)
	}

	lease, err := old.Acquire(context.Background(), "local/dev")
	if err == nil {
		_ = lease.Release()

		t.Fatal("previous SQL authority accepted a deployment after migration")
	}
}
