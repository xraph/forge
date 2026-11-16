package consensus

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/errors"
	"github.com/xraph/forge/extensions/consensus/internal"
	"github.com/xraph/forge/extensions/consensus/storage"
)

func writeKeyFile(t *testing.T) string {
	t.Helper()

	key := make([]byte, storage.KeySize)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(t.TempDir(), "raft.key")
	if err := os.WriteFile(path, []byte(hex.EncodeToString(key)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	return path
}

func encryptedClusterConfigs(t *testing.T, storageType string, size int) []internal.Config {
	t.Helper()

	cfgs := clusterConfigs(t, "tcp", size)

	for i := range cfgs {
		cfgs[i].Storage.Type = storageType
		cfgs[i].Storage.Path = t.TempDir()
		cfgs[i].Security.EnableEncryption = true
		// Each node has its own key: encryption at rest is local to a node.
		cfgs[i].Security.EncryptionKeyFile = writeKeyFile(t)
	}

	return cfgs
}

func startService(t *testing.T, cfg internal.Config) (*Service, error) {
	t.Helper()

	s, err := NewConsensusService(cfg, forge.NewNoopLogger(), nil)
	if err != nil {
		t.Fatal(err)
	}

	if err := s.Start(context.Background()); err != nil {
		return nil, err
	}

	t.Cleanup(func() { _ = s.Stop(context.Background()) })

	return s, nil
}

func stopServices(t *testing.T, services []*Service) {
	t.Helper()

	for _, s := range services {
		if err := s.Stop(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
}

// applySet applies a set command through the current leader. An attempt that
// times out is retried: an entry appended by a leader that loses its term
// right away is dropped, and raft does not fail its Apply call until a later
// entry is applied at that index. The command is idempotent.
func applySet(t *testing.T, services []*Service, key, value string) {
	t.Helper()

	var err error

	for range 5 {
		leader := waitForServiceLeader(t, services)

		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		err = leader.Apply(ctx, Command{Type: "set", Payload: map[string]any{"key": key, "value": value}})

		cancel()

		if err == nil {
			return
		}
	}

	t.Fatalf("apply %s: %v", key, err)
}

func waitForValue(t *testing.T, services []*Service, key, want string) {
	t.Helper()

	for _, s := range services {
		deadline := time.Now().Add(10 * time.Second)

		for {
			got, err := s.Read(context.Background(), map[string]any{"type": "get", "key": key})
			if err == nil && got == want {
				break
			}

			if time.Now().After(deadline) {
				t.Fatalf("%s: read %s = %v, %v; want %q", s.config.NodeID, key, got, err, want)
			}

			time.Sleep(20 * time.Millisecond)
		}
	}
}

// plaintextForms returns the secret and the stable middle of its base64
// encoding at each of the three byte alignments, since the log stores command
// bytes base64 encoded inside JSON.
func plaintextForms(secret string) [][]byte {
	forms := [][]byte{[]byte(secret)}

	for pad := range 3 {
		enc := base64.StdEncoding.EncodeToString(append(make([]byte, pad), secret...))
		forms = append(forms, []byte(enc[4:len(enc)-4]))
	}

	return forms
}

// assertNoPlaintext fails if any file under dir contains secret in any form.
func assertNoPlaintext(t *testing.T, dir, secret string) {
	t.Helper()

	files := 0

	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		files++

		for _, form := range plaintextForms(secret) {
			if bytes.Contains(data, form) {
				t.Errorf("%s contains plaintext %q", path, form)
			}
		}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	if files == 0 {
		t.Fatalf("no storage files under %s", dir)
	}
}

// A three node cluster with encryption on replicates, snapshots, keeps no
// plaintext on disk, and comes back with its data after a full restart.
func TestServiceEncryptedClusterReplicatesAndRestarts(t *testing.T) {
	for _, storageType := range []string{"boltdb", "badger"} {
		t.Run(storageType, func(t *testing.T) {
			const secret = "launch-code-0451-unmistakable"

			cfgs := encryptedClusterConfigs(t, storageType, 3)
			services := startServices(t, cfgs)

			applySet(t, services, "greeting", "hello")
			waitForValue(t, services, "greeting", "hello")
			applySet(t, services, "secret", secret)
			waitForValue(t, services, "secret", secret)

			leader := waitForServiceLeader(t, services)
			if err := leader.Snapshot(context.Background()); err != nil {
				t.Fatalf("snapshot: %v", err)
			}

			applySet(t, services, "after-snapshot", secret+"-2")
			waitForValue(t, services, "after-snapshot", secret+"-2")

			stopServices(t, services)

			for _, cfg := range cfgs {
				assertNoPlaintext(t, cfg.Storage.Path, secret)
			}

			// Restart every node with the same configuration.
			restarted := make([]*Service, len(cfgs))

			for i, cfg := range cfgs {
				s, err := startService(t, cfg)
				if err != nil {
					t.Fatalf("restart %s: %v", cfg.NodeID, err)
				}

				restarted[i] = s
			}

			waitForValue(t, restarted, "greeting", "hello")
			waitForValue(t, restarted, "secret", secret)
			waitForValue(t, restarted, "after-snapshot", secret+"-2")
		})
	}
}

// Encryption cannot be switched on over a plaintext store, or off over an
// encrypted one, without an explicit migration.
func TestServiceEncryptionModeMismatch(t *testing.T) {
	cfg := clusterConfigs(t, "tcp", 1)[0]
	cfg.Storage.Type = "boltdb"
	cfg.Storage.Path = t.TempDir()

	s, err := startService(t, cfg)
	if err != nil {
		t.Fatal(err)
	}

	applySet(t, []*Service{s}, "k", "plain-value-before-migration")
	stopServices(t, []*Service{s})

	enc := cfg
	enc.Security.EnableEncryption = true
	enc.Security.EncryptionKeyFile = writeKeyFile(t)

	if _, err := startService(t, enc); !errors.Is(err, storage.ErrPlaintextStore) {
		t.Fatalf("encryption over a plaintext store: %v, want ErrPlaintextStore", err)
	}

	enc.Security.EncryptionMigratePlaintext = true

	s, err = startService(t, enc)
	if err != nil {
		t.Fatalf("start with migration: %v", err)
	}

	waitForValue(t, []*Service{s}, "k", "plain-value-before-migration")
	stopServices(t, []*Service{s})

	// Not asserted here: the file may still hold the old plaintext in free
	// pages until the backend reuses or compacts them (see the README).

	if _, err := startService(t, cfg); !errors.Is(err, storage.ErrEncryptedStore) {
		t.Fatalf("plaintext start over an encrypted store: %v, want ErrEncryptedStore", err)
	}

	wrong := enc
	wrong.Security.EncryptionKeyFile = writeKeyFile(t)

	if _, err := startService(t, wrong); !errors.Is(err, storage.ErrWrongKey) {
		t.Fatalf("start with another key: %v, want ErrWrongKey", err)
	}
}

// Rotating keys through the service: add key 2, re-encrypt, then drop key 1.
func TestServiceReencryptStorageAfterRotation(t *testing.T) {
	cfg := clusterConfigs(t, "tcp", 1)[0]
	cfg.Storage.Type = "boltdb"
	cfg.Storage.Path = t.TempDir()
	cfg.Security.EnableEncryption = true

	key1, key2 := writeKeyFile(t), writeKeyFile(t)
	cfg.Security.EncryptionKeyFile = key1

	s, err := startService(t, cfg)
	if err != nil {
		t.Fatal(err)
	}

	applySet(t, []*Service{s}, "k", "v1")
	stopServices(t, []*Service{s})

	cfg.Security.EncryptionKeyFile = ""
	cfg.Security.EncryptionKeys = []internal.EncryptionKeyConfig{{ID: 1, File: key1}, {ID: 2, File: key2}}

	s, err = startService(t, cfg)
	if err != nil {
		t.Fatal(err)
	}

	waitForValue(t, []*Service{s}, "k", "v1")

	n, err := s.ReencryptStorage()
	if err != nil || n == 0 {
		t.Fatalf("ReencryptStorage = %d, %v", n, err)
	}

	stopServices(t, []*Service{s})

	cfg.Security.EncryptionKeys = []internal.EncryptionKeyConfig{{ID: 2, File: key2}}

	s, err = startService(t, cfg)
	if err != nil {
		t.Fatalf("start with only key 2: %v", err)
	}

	waitForValue(t, []*Service{s}, "k", "v1")
}

// A key of the wrong size stops the service at start with a clear error that
// does not echo the key.
func TestServiceEncryptionRejectsBadKey(t *testing.T) {
	cfg := clusterConfigs(t, "tcp", 1)[0]
	cfg.Storage.Type = "boltdb"
	cfg.Storage.Path = t.TempDir()
	cfg.Security.EnableEncryption = true
	cfg.Security.EncryptionKeyFile = filepath.Join(t.TempDir(), "short.key")

	if err := os.WriteFile(cfg.Security.EncryptionKeyFile, []byte("0123456789abcdef0123456789abcdef\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := startService(t, cfg)
	if !errors.Is(err, storage.ErrInvalidEncryptionKey) || !strings.Contains(err.Error(), "32") {
		t.Fatalf("short key: %v, want ErrInvalidEncryptionKey", err)
	}

	if strings.Contains(err.Error(), "0123456789abcdef") {
		t.Fatalf("error echoes the key: %v", err)
	}

	if _, err := (&Service{}).ReencryptStorage(); !errors.Is(err, ErrNotStarted) {
		t.Fatalf("ReencryptStorage before start: %v", err)
	}
}
