package storage_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/consensus/internal"
	"github.com/xraph/forge/extensions/consensus/raft"
	"github.com/xraph/forge/extensions/consensus/storage"
)

func encryptedBolt(tb testing.TB, path string, key []byte) *storage.EncryptedStorage {
	tb.Helper()

	inner, err := storage.NewBoltDBStorage(storage.BoltDBStorageConfig{Path: path}, forge.NewNoopLogger())
	if err != nil {
		tb.Fatal(err)
	}

	kr, err := storage.NewKeyring(0, map[uint32][]byte{1: key})
	if err != nil {
		tb.Fatal(err)
	}

	s, err := storage.NewEncryptedStorage(inner, storage.EncryptedStorageConfig{Keyring: kr}, forge.NewNoopLogger())
	if err != nil {
		tb.Fatal(err)
	}

	if err := s.Start(context.Background()); err != nil {
		tb.Fatal(err)
	}

	return s
}

func randomKey(tb testing.TB) []byte {
	tb.Helper()

	key := make([]byte, storage.KeySize)
	if _, err := rand.Read(key); err != nil {
		tb.Fatal(err)
	}

	return key
}

// The raft log persisted through encrypted storage leaves no plaintext of a
// known command in the database file, and reloads intact.
func TestRaftLogOnEncryptedStorageHasNoPlaintextOnDisk(t *testing.T) {
	path := filepath.Join(t.TempDir(), "raft.db")
	key := randomKey(t)
	command := []byte(`{"type":"set","payload":{"key":"card","value":"4111-1111-1111-1111"}}`)

	s := encryptedBolt(t, path, key)

	log, err := raft.NewLog(forge.NewNoopLogger(), s, 0)
	if err != nil {
		t.Fatal(err)
	}

	for i := uint64(1); i <= 50; i++ {
		if err := log.Append(internal.LogEntry{Index: i, Term: 1, Data: command}); err != nil {
			t.Fatal(err)
		}
	}

	if err := s.Set([]byte("votedFor"), []byte("node-with-a-recognisable-name")); err != nil {
		t.Fatal(err)
	}

	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	for _, needle := range [][]byte{[]byte("4111-1111"), []byte(`"payload"`), []byte("recognisable")} {
		if bytes.Contains(raw, needle) {
			t.Fatalf("database file contains plaintext %q", needle)
		}
	}

	// LogEntry.Data is base64 in the JSON encoding, so check that form too.
	if bytes.Contains(raw, []byte(base64.StdEncoding.EncodeToString(command)[:24])) {
		t.Fatal("database file contains the base64 encoded command")
	}

	s = encryptedBolt(t, path, key)
	defer s.Close()

	log, err = raft.NewLog(forge.NewNoopLogger(), s, 0)
	if err != nil {
		t.Fatal(err)
	}

	entry, err := log.Get(50)
	if err != nil || !bytes.Equal(entry.Data, command) || log.LastIndex() != 50 {
		t.Fatalf("reloaded log: last %d, entry %+v, %v", log.LastIndex(), entry, err)
	}
}

// BenchmarkLogAppend measures one raft log append (JSON encoding plus one
// storage write) with and without encryption. Run with -benchmem.
func BenchmarkLogAppend(b *testing.B) {
	for _, size := range []int{128, 1024, 16384} {
		data := make([]byte, size)

		backends := map[string]func(b *testing.B) internal.Storage{
			"memory/plain": func(*testing.B) internal.Storage {
				return storage.NewMemoryStorage(storage.MemoryStorageConfig{}, forge.NewNoopLogger())
			},
			"memory/encrypted": func(b *testing.B) internal.Storage {
				kr, _ := storage.NewKeyring(0, map[uint32][]byte{1: randomKey(b)})
				s, _ := storage.NewEncryptedStorage(
					storage.NewMemoryStorage(storage.MemoryStorageConfig{}, forge.NewNoopLogger()),
					storage.EncryptedStorageConfig{Keyring: kr}, forge.NewNoopLogger())

				return s
			},
			"boltdb/plain": func(b *testing.B) internal.Storage {
				s, _ := storage.NewBoltDBStorage(storage.BoltDBStorageConfig{
					Path: filepath.Join(b.TempDir(), "raft.db"),
				}, forge.NewNoopLogger())

				return s
			},
			"boltdb/encrypted": func(b *testing.B) internal.Storage {
				inner, _ := storage.NewBoltDBStorage(storage.BoltDBStorageConfig{
					Path: filepath.Join(b.TempDir(), "raft.db"),
				}, forge.NewNoopLogger())
				kr, _ := storage.NewKeyring(0, map[uint32][]byte{1: randomKey(b)})
				s, _ := storage.NewEncryptedStorage(inner, storage.EncryptedStorageConfig{Keyring: kr}, forge.NewNoopLogger())

				return s
			},
		}

		for _, name := range []string{"memory/plain", "memory/encrypted", "boltdb/plain", "boltdb/encrypted"} {
			b.Run(name+"/"+sizeName(size), func(b *testing.B) {
				s := backends[name](b)
				if err := s.Start(context.Background()); err != nil {
					b.Fatal(err)
				}

				defer s.Close()

				log, err := raft.NewLog(forge.NewNoopLogger(), s, 0)
				if err != nil {
					b.Fatal(err)
				}

				b.SetBytes(int64(size))
				b.ReportAllocs()

				index := uint64(0)

				for b.Loop() {
					index++

					if err := log.Append(internal.LogEntry{Index: index, Term: 1, Data: data}); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}

// BenchmarkSeal measures the encryption step alone.
func BenchmarkSeal(b *testing.B) {
	kr, err := storage.NewKeyring(0, map[uint32][]byte{1: randomKey(b)})
	if err != nil {
		b.Fatal(err)
	}

	for _, size := range []int{128, 1024, 16384} {
		b.Run(sizeName(size), func(b *testing.B) {
			value := make([]byte, size)

			b.SetBytes(int64(size))
			b.ReportAllocs()

			for b.Loop() {
				if _, err := kr.Seal([]byte("log_123456"), value); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func sizeName(size int) string {
	return strconv.Itoa(size) + "B"
}
