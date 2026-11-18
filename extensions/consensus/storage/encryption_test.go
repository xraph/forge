package storage

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xraph/forge"
	"github.com/xraph/forge/errors"
	"github.com/xraph/forge/extensions/consensus/internal"
)

// testBackend opens handles on one underlying store. Every call to open
// returns a fresh, unstarted handle on the same data, as a restart would.
type testBackend struct {
	name string
	open func() internal.Storage
}

// reopenableMemory lets the memory backend be stopped and started again with
// its data intact, like a persistent backend.
type reopenableMemory struct {
	*MemoryStorage
}

func (m reopenableMemory) Start(ctx context.Context) error {
	if err := m.MemoryStorage.Start(ctx); err != nil && !errors.Is(err, internal.ErrAlreadyStarted) {
		return err
	}

	return nil
}

func (m reopenableMemory) Stop(context.Context) error { return nil }

func testBackends(t testing.TB) []testBackend {
	t.Helper()

	logger := forge.NewNoopLogger()
	badgerDir := filepath.Join(t.TempDir(), "badger")
	boltPath := filepath.Join(t.TempDir(), "raft.db")
	mem := NewMemoryStorage(MemoryStorageConfig{}, logger)

	return []testBackend{
		{name: "memory", open: func() internal.Storage { return reopenableMemory{mem} }},
		{name: "badger", open: func() internal.Storage {
			s, err := NewBadgerStorage(BadgerStorageConfig{Path: badgerDir}, logger)
			if err != nil {
				t.Fatal(err)
			}

			return s
		}},
		{name: "boltdb", open: func() internal.Storage {
			s, err := NewBoltDBStorage(BoltDBStorageConfig{Path: boltPath}, logger)
			if err != nil {
				t.Fatal(err)
			}

			return s
		}},
	}
}

func newKey(t testing.TB) []byte {
	t.Helper()

	key := make([]byte, KeySize)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}

	return key
}

func keyring(t testing.TB, active uint32, keys map[uint32][]byte) *Keyring {
	t.Helper()

	kr, err := NewKeyring(active, keys)
	if err != nil {
		t.Fatal(err)
	}

	return kr
}

// startEncrypted opens the backend with encryption and starts it.
func startEncrypted(t testing.TB, b testBackend, kr *Keyring, migrate bool) (*EncryptedStorage, error) {
	t.Helper()

	s, err := NewEncryptedStorage(b.open(), EncryptedStorageConfig{Keyring: kr, MigratePlaintext: migrate}, forge.NewNoopLogger())
	if err != nil {
		t.Fatal(err)
	}

	return s, s.Start(context.Background())
}

func mustStartEncrypted(t testing.TB, b testBackend, kr *Keyring) *EncryptedStorage {
	t.Helper()

	s, err := startEncrypted(t, b, kr, false)
	if err != nil {
		t.Fatalf("start encrypted %s: %v", b.name, err)
	}

	return s
}

func stop(t testing.TB, s internal.Storage) {
	t.Helper()

	if err := s.Stop(context.Background()); err != nil {
		t.Fatalf("stop: %v", err)
	}
}

func mustGet(t testing.TB, s internal.Storage, key string) []byte {
	t.Helper()

	v, err := s.Get([]byte(key))
	if err != nil {
		t.Fatalf("get %s: %v", key, err)
	}

	return v
}

func rawKeyID(t testing.TB, s *EncryptedStorage, key string) uint32 {
	t.Helper()

	raw, err := s.inner.Get([]byte(key))
	if err != nil {
		t.Fatal(err)
	}

	id, ok := envelopeKeyID(raw)
	if !ok {
		t.Fatalf("raw value of %s is not an envelope", key)
	}

	return id
}

func TestEncryptedStorageRoundTrip(t *testing.T) {
	for _, b := range testBackends(t) {
		t.Run(b.name, func(t *testing.T) {
			kr := keyring(t, 0, map[uint32][]byte{1: newKey(t)})
			s := mustStartEncrypted(t, b, kr)

			if err := s.Set([]byte("currentTerm"), []byte{0, 0, 0, 0, 0, 0, 0, 7}); err != nil {
				t.Fatal(err)
			}

			ops := []internal.BatchOp{
				{Type: internal.BatchOpSet, Key: []byte("log_1"), Value: []byte(`{"index":1}`)},
				{Type: internal.BatchOpSet, Key: []byte("log_2"), Value: []byte(`{"index":2}`)},
				{Type: internal.BatchOpSet, Key: []byte("log_3"), Value: []byte{}},
				{Type: internal.BatchOpDelete, Key: []byte("log_3")},
			}
			if err := s.Batch(ops); err != nil {
				t.Fatal(err)
			}

			if got := mustGet(t, s, "log_2"); string(got) != `{"index":2}` {
				t.Fatalf("log_2 = %q", got)
			}

			if _, err := s.Get([]byte("log_3")); !errors.Is(err, internal.ErrNodeNotFound) {
				t.Fatalf("deleted key returned %v, want ErrNodeNotFound", err)
			}

			if err := s.Set([]byte("empty"), nil); err != nil {
				t.Fatal(err)
			}

			if got := mustGet(t, s, "empty"); len(got) != 0 {
				t.Fatalf("empty value came back as %q", got)
			}

			kvs, err := s.GetRange([]byte("log_"), []byte("log_~"))
			if err != nil {
				t.Fatal(err)
			}

			if len(kvs) != 2 || string(kvs[0].Value) != `{"index":1}` || string(kvs[1].Value) != `{"index":2}` {
				t.Fatalf("range = %+v", kvs)
			}

			raw, err := s.inner.Get([]byte("log_1"))
			if err != nil {
				t.Fatal(err)
			}

			if len(raw) != len(`{"index":1}`)+EnvelopeOverhead || bytes.Contains(raw, []byte("index")) {
				t.Fatalf("raw value is not an envelope: %q", raw)
			}

			if err := s.Delete([]byte("log_1")); err != nil {
				t.Fatal(err)
			}

			stop(t, s)

			// Restart with the same key.
			s = mustStartEncrypted(t, b, kr)
			defer stop(t, s)

			if got := mustGet(t, s, "currentTerm"); !bytes.Equal(got, []byte{0, 0, 0, 0, 0, 0, 0, 7}) {
				t.Fatalf("currentTerm after restart = %v", got)
			}

			if _, err := s.Get([]byte("log_1")); !errors.Is(err, internal.ErrNodeNotFound) {
				t.Fatalf("deleted key after restart returned %v", err)
			}
		})
	}
}

func TestEncryptedStorageRejectsTamperedAndMovedValues(t *testing.T) {
	for _, b := range testBackends(t) {
		t.Run(b.name, func(t *testing.T) {
			kr := keyring(t, 1, map[uint32][]byte{1: newKey(t), 2: newKey(t)})
			s := mustStartEncrypted(t, b, kr)

			defer stop(t, s)

			for _, k := range []string{"a", "b", "c", "d", "e"} {
				if err := s.Set([]byte(k), []byte("value of "+k)); err != nil {
					t.Fatal(err)
				}
			}

			rawA, _ := s.inner.Get([]byte("a"))

			tamper := func(key string, mutate func([]byte) []byte) {
				raw, err := s.inner.Get([]byte(key))
				if err != nil {
					t.Fatal(err)
				}

				if err := s.inner.Set([]byte(key), mutate(bytes.Clone(raw))); err != nil {
					t.Fatal(err)
				}
			}

			// A flipped ciphertext bit.
			tamper("b", func(v []byte) []byte {
				v[len(v)-20] ^= 1

				return v
			})
			// The value of a copied under key c.
			tamper("c", func([]byte) []byte { return bytes.Clone(rawA) })
			// The header switched to the other configured key.
			tamper("d", func(v []byte) []byte { return withID(v, 2) })
			// A truncated value.
			tamper("e", func(v []byte) []byte { return v[:EnvelopeOverhead-1] })

			for _, k := range []string{"b", "c", "d", "e"} {
				if _, err := s.Get([]byte(k)); !errors.Is(err, ErrDecrypt) {
					t.Fatalf("tampered %s: Get returned %v, want ErrDecrypt", k, err)
				}
			}

			if _, err := s.GetRange([]byte("a"), []byte("e")); !errors.Is(err, ErrDecrypt) {
				t.Fatalf("GetRange over tampered values returned %v, want ErrDecrypt", err)
			}

			if got := mustGet(t, s, "a"); string(got) != "value of a" {
				t.Fatalf("untouched value = %q", got)
			}

			// A key ID nobody configured.
			tamper("a", func(v []byte) []byte { return withID(v, 9) })

			if _, err := s.Get([]byte("a")); !errors.Is(err, ErrUnknownKeyID) {
				t.Fatalf("unknown key ID: Get returned %v", err)
			}
		})
	}
}

func TestEncryptedStorageWrongKeyFailsAtStart(t *testing.T) {
	for _, b := range testBackends(t) {
		t.Run(b.name, func(t *testing.T) {
			right := newKey(t)

			s := mustStartEncrypted(t, b, keyring(t, 0, map[uint32][]byte{1: right}))
			if err := s.Set([]byte("log_1"), []byte("entry")); err != nil {
				t.Fatal(err)
			}

			stop(t, s)

			_, err := startEncrypted(t, b, keyring(t, 0, map[uint32][]byte{1: newKey(t)}), false)
			if !errors.Is(err, ErrWrongKey) || !strings.Contains(err.Error(), "ID 1") {
				t.Fatalf("different key under the same ID: %v, want ErrWrongKey", err)
			}

			_, err = startEncrypted(t, b, keyring(t, 0, map[uint32][]byte{2: newKey(t)}), false)
			if !errors.Is(err, ErrWrongKey) || !strings.Contains(err.Error(), "[1]") {
				t.Fatalf("only an unrelated key ID: %v, want ErrWrongKey naming key 1", err)
			}

			// A failed start leaves the backend closed, so the right key can
			// open it again.
			s = mustStartEncrypted(t, b, keyring(t, 0, map[uint32][]byte{1: right}))
			defer stop(t, s)

			if got := mustGet(t, s, "log_1"); string(got) != "entry" {
				t.Fatalf("log_1 = %q", got)
			}
		})
	}
}

func TestEncryptedStorageKeyRotation(t *testing.T) {
	for _, b := range testBackends(t) {
		t.Run(b.name, func(t *testing.T) {
			key1, key2 := newKey(t), newKey(t)

			s := mustStartEncrypted(t, b, keyring(t, 0, map[uint32][]byte{1: key1}))
			for i := range 300 {
				if err := s.Set(fmt.Appendf(nil, "log_%d", i), fmt.Appendf(nil, "old %d", i)); err != nil {
					t.Fatal(err)
				}
			}

			stop(t, s)

			// Rotate: key 2 becomes active, key 1 stays for reads.
			s = mustStartEncrypted(t, b, keyring(t, 0, map[uint32][]byte{1: key1, 2: key2}))

			if got := mustGet(t, s, "log_7"); string(got) != "old 7" {
				t.Fatalf("old value after rotation = %q", got)
			}

			if err := s.Set([]byte("log_new"), []byte("new")); err != nil {
				t.Fatal(err)
			}

			if id := rawKeyID(t, s, "log_new"); id != 2 {
				t.Fatalf("new write used key %d, want 2", id)
			}

			if id := rawKeyID(t, s, "log_7"); id != 1 {
				t.Fatalf("old value is under key %d, want 1", id)
			}

			stop(t, s)

			// Dropping key 1 before re-encrypting: the store opens with key 2,
			// but old values report the missing key.
			s = mustStartEncrypted(t, b, keyring(t, 0, map[uint32][]byte{2: key2}))

			if _, err := s.Get([]byte("log_7")); !errors.Is(err, ErrUnknownKeyID) {
				t.Fatalf("old value without key 1: %v, want ErrUnknownKeyID", err)
			}

			stop(t, s)

			// Re-encrypt with both keys, then key 1 can go.
			s = mustStartEncrypted(t, b, keyring(t, 0, map[uint32][]byte{1: key1, 2: key2}))

			n, err := s.Rekey()
			if err != nil || n != 300 {
				t.Fatalf("Rekey = %d, %v; want 300", n, err)
			}

			if n, err := s.Rekey(); err != nil || n != 0 {
				t.Fatalf("second Rekey = %d, %v; want 0", n, err)
			}

			stop(t, s)

			s = mustStartEncrypted(t, b, keyring(t, 0, map[uint32][]byte{2: key2}))
			defer stop(t, s)

			for i := range 300 {
				if got := mustGet(t, s, fmt.Sprintf("log_%d", i)); string(got) != fmt.Sprintf("old %d", i) {
					t.Fatalf("log_%d = %q", i, got)
				}
			}
		})
	}
}

func TestEncryptedStorageRefusesPlaintextStoreAndMigrates(t *testing.T) {
	for _, b := range testBackends(t) {
		t.Run(b.name, func(t *testing.T) {
			plain := NewPlaintextGuard(b.open())
			if err := plain.Start(context.Background()); err != nil {
				t.Fatal(err)
			}

			for i := range 600 {
				if err := plain.Set(fmt.Appendf(nil, "log_%d", i), fmt.Appendf(nil, "command %d", i)); err != nil {
					t.Fatal(err)
				}
			}

			stop(t, plain)

			kr := keyring(t, 0, map[uint32][]byte{1: newKey(t)})

			if _, err := startEncrypted(t, b, kr, false); !errors.Is(err, ErrPlaintextStore) {
				t.Fatalf("encrypted start on a plaintext store: %v, want ErrPlaintextStore", err)
			}

			// One-time migration.
			s, err := startEncrypted(t, b, kr, true)
			if err != nil {
				t.Fatalf("migrating start: %v", err)
			}

			if got := mustGet(t, s, "log_599"); string(got) != "command 599" {
				t.Fatalf("log_599 after migration = %q", got)
			}

			if id := rawKeyID(t, s, "log_0"); id != 1 {
				t.Fatalf("migrated value under key %d", id)
			}

			stop(t, s)

			// The migration option is harmless once the store is encrypted.
			s, err = startEncrypted(t, b, kr, true)
			if err != nil {
				t.Fatal(err)
			}

			stop(t, s)

			// The reverse mismatch: an encrypted store opened without encryption.
			plain = NewPlaintextGuard(b.open())
			if err := plain.Start(context.Background()); !errors.Is(err, ErrEncryptedStore) {
				t.Fatalf("plaintext start on an encrypted store: %v, want ErrEncryptedStore", err)
			}

			// Decrypting once lets it start without encryption again.
			inner := b.open()
			if err := inner.Start(context.Background()); err != nil {
				t.Fatal(err)
			}

			if n, err := MigrateToPlaintext(inner, kr); err != nil || n != 600 {
				t.Fatalf("MigrateToPlaintext = %d, %v", n, err)
			}

			stop(t, inner)

			plain = NewPlaintextGuard(b.open())
			if err := plain.Start(context.Background()); err != nil {
				t.Fatalf("plaintext start after decrypting: %v", err)
			}

			defer stop(t, plain)

			if got := mustGet(t, plain, "log_3"); string(got) != "command 3" {
				t.Fatalf("log_3 after decrypting = %q", got)
			}
		})
	}
}

// A migration that stops halfway blocks both modes until it is run again,
// and the rerun skips values that are already encrypted.
func TestEncryptedStorageInterruptedMigration(t *testing.T) {
	for _, b := range testBackends(t) {
		t.Run(b.name, func(t *testing.T) {
			kr := keyring(t, 0, map[uint32][]byte{1: newKey(t)})

			inner := b.open()
			if err := inner.Start(context.Background()); err != nil {
				t.Fatal(err)
			}

			sealed, err := kr.Seal([]byte("log_1"), []byte("first"))
			if err != nil {
				t.Fatal(err)
			}

			// What an interrupted run leaves: the marker, one value encrypted,
			// one still in plaintext.
			for k, v := range map[string][]byte{
				string(migratingToEncryptedKey): {1},
				"log_1":                         sealed,
				"log_2":                         []byte("second"),
			} {
				if err := inner.Set([]byte(k), v); err != nil {
					t.Fatal(err)
				}
			}

			stop(t, inner)

			if err := NewPlaintextGuard(b.open()).Start(context.Background()); !errors.Is(err, ErrMigrationIncomplete) {
				t.Fatalf("plaintext start mid-migration: %v", err)
			}

			if _, err := startEncrypted(t, b, kr, false); !errors.Is(err, ErrMigrationIncomplete) {
				t.Fatalf("encrypted start mid-migration: %v", err)
			}

			s, err := startEncrypted(t, b, kr, true)
			if err != nil {
				t.Fatal(err)
			}

			defer stop(t, s)

			if got := mustGet(t, s, "log_1"); string(got) != "first" {
				t.Fatalf("log_1 = %q", got)
			}

			if got := mustGet(t, s, "log_2"); string(got) != "second" {
				t.Fatalf("log_2 = %q", got)
			}

			if _, err := s.inner.Get(migratingToEncryptedKey); !errors.Is(err, internal.ErrNodeNotFound) {
				t.Fatalf("migration marker still present: %v", err)
			}
		})
	}
}

func TestEncryptedStorageReservedKeys(t *testing.T) {
	b := testBackends(t)[0]
	s := mustStartEncrypted(t, b, keyring(t, 0, map[uint32][]byte{1: newKey(t)}))

	defer stop(t, s)

	if err := s.Set(checkValueKey(1), []byte("x")); !errors.Is(err, ErrReservedKey) {
		t.Fatalf("Set on a reserved key: %v", err)
	}

	if err := s.Delete(checkValueKey(1)); !errors.Is(err, ErrReservedKey) {
		t.Fatalf("Delete on a reserved key: %v", err)
	}

	if err := s.Batch([]internal.BatchOp{{Type: internal.BatchOpDelete, Key: migratingToPlaintextKey}}); !errors.Is(err, ErrReservedKey) {
		t.Fatalf("Batch on a reserved key: %v", err)
	}

	if err := s.Set([]byte("k"), []byte("v")); err != nil {
		t.Fatal(err)
	}

	kvs, err := s.GetRange([]byte{}, lastKey)
	if err != nil {
		t.Fatal(err)
	}

	if len(kvs) != 1 || string(kvs[0].Key) != "k" {
		t.Fatalf("full range = %+v, want only k", kvs)
	}
}

func TestSealUsesAFreshNonceEveryWrite(t *testing.T) {
	kr := keyring(t, 0, map[uint32][]byte{1: newKey(t)})
	seen := make(map[string]bool)

	for range 10000 {
		env, err := kr.Seal([]byte("k"), []byte("same value"))
		if err != nil {
			t.Fatal(err)
		}

		nonce := string(env[headerSize : headerSize+nonceSize])
		if seen[nonce] {
			t.Fatal("nonce repeated")
		}

		seen[nonce] = true
	}
}

func TestNewKeyringValidatesKeys(t *testing.T) {
	cases := map[string]struct {
		active uint32
		keys   map[uint32][]byte
	}{
		"no keys":        {keys: map[uint32][]byte{}},
		"short key":      {keys: map[uint32][]byte{1: make([]byte, 16)}},
		"long key":       {keys: map[uint32][]byte{1: make([]byte, 33)}},
		"key id zero":    {keys: map[uint32][]byte{0: make([]byte, 32)}},
		"missing active": {active: 3, keys: map[uint32][]byte{1: make([]byte, 32)}},
	}

	for name, c := range cases {
		if _, err := NewKeyring(c.active, c.keys); !errors.Is(err, ErrInvalidEncryptionKey) {
			t.Fatalf("%s: %v, want ErrInvalidEncryptionKey", name, err)
		}
	}

	kr := keyring(t, 0, map[uint32][]byte{3: newKey(t), 1: newKey(t)})
	if kr.ActiveKeyID() != 3 {
		t.Fatalf("default active key = %d, want the highest ID", kr.ActiveKeyID())
	}
}

func TestLoadKeyring(t *testing.T) {
	key := newKey(t)
	dir := t.TempDir()

	write := func(name string, data []byte) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}

		return path
	}

	rawFile := write("raw.key", key)
	hexFile := write("hex.key", []byte(hex.EncodeToString(key)+"\n"))
	b64File := write("b64.key", []byte(base64.StdEncoding.EncodeToString(key)+"\n"))
	shortFile := write("short.key", []byte("c2hvcnQta2V5LW1hdGVyaWFs"))

	t.Setenv("CONSENSUS_TEST_KEY", hex.EncodeToString(key))
	t.Setenv("CONSENSUS_TEST_BAD_KEY", "zz"+hex.EncodeToString(key)[2:])

	kr, err := LoadKeyring(KeyringConfig{Keys: []KeySource{
		{ID: 1, File: rawFile}, {ID: 2, File: hexFile}, {ID: 3, File: b64File}, {ID: 4, Env: "CONSENSUS_TEST_KEY"},
	}}, forge.NewNoopLogger())
	if err != nil {
		t.Fatal(err)
	}

	// Every source decodes to the same key: a value sealed under each ID
	// opens with a keyring built from the raw key under that ID.
	for _, id := range []uint32{1, 2, 3, 4} {
		env, err := kr.sealWith(id, []byte("k"), []byte("v"))
		if err != nil {
			t.Fatal(err)
		}

		if got, err := keyring(t, 0, map[uint32][]byte{id: key}).Open([]byte("k"), env); err != nil || string(got) != "v" {
			t.Fatalf("key %d did not decode to the raw key: %q, %v", id, got, err)
		}

		// The header is authenticated: the same key material under another
		// ID does not open it.
		if _, err := kr.Open([]byte("k"), withID(env, id%4+1)); !errors.Is(err, ErrDecrypt) {
			t.Fatalf("header rewritten from key %d: %v, want ErrDecrypt", id, err)
		}
	}

	if kr.ActiveKeyID() != 4 {
		t.Fatalf("active key = %d", kr.ActiveKeyID())
	}

	failures := map[string]KeyringConfig{
		"short key file":      {Keys: []KeySource{{ID: 1, File: shortFile}}},
		"missing file":        {Keys: []KeySource{{ID: 1, File: filepath.Join(dir, "absent")}}},
		"unset env":           {Keys: []KeySource{{ID: 1, Env: "CONSENSUS_TEST_UNSET_KEY"}}},
		"bad hex env":         {Keys: []KeySource{{ID: 1, Env: "CONSENSUS_TEST_BAD_KEY"}}},
		"inline not allowed":  {Keys: []KeySource{{ID: 1, Value: hex.EncodeToString(key)}}},
		"two sources":         {Keys: []KeySource{{ID: 1, File: rawFile, Env: "CONSENSUS_TEST_KEY"}}},
		"duplicate ids":       {Keys: []KeySource{{ID: 1, File: rawFile}, {ID: 1, File: hexFile}}},
		"no keys":             {},
		"active not in ring":  {Keys: []KeySource{{ID: 1, File: rawFile}}, ActiveKeyID: 2},
		"inline wrong length": {Keys: []KeySource{{ID: 1, Value: "abcd"}}, AllowInline: true},
	}

	for name, cfg := range failures {
		_, err := LoadKeyring(cfg, forge.NewNoopLogger())
		if !errors.Is(err, ErrInvalidEncryptionKey) {
			t.Fatalf("%s: %v, want ErrInvalidEncryptionKey", name, err)
		}

		// Errors name sources, never key material.
		for _, secret := range []string{hex.EncodeToString(key), "zz" + hex.EncodeToString(key)[2:12], "c2hvcnQ"} {
			if strings.Contains(err.Error(), secret) {
				t.Fatalf("%s: error leaks key material: %v", name, err)
			}
		}
	}

	if _, err := LoadKeyring(KeyringConfig{Keys: []KeySource{{ID: 1, Value: hex.EncodeToString(key)}}, AllowInline: true},
		forge.NewNoopLogger()); err != nil {
		t.Fatalf("allowed inline key: %v", err)
	}
}

func withID(envelope []byte, id uint32) []byte {
	e := bytes.Clone(envelope)
	binary.BigEndian.PutUint32(e[1:5], id)

	return e
}
