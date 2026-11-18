package storage

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"math"
	"slices"
	"strconv"
	"sync"

	"github.com/xraph/forge"
	"github.com/xraph/forge/errors"
	"github.com/xraph/forge/extensions/consensus/internal"
)

// Encryption at rest.
//
// EncryptedStorage wraps any internal.Storage and seals every value with
// AES-256-GCM before it reaches the backend. A sealed value (an envelope) is
//
//	version (1 byte) | key ID (4 bytes, big endian) | nonce (12 bytes) | ciphertext and tag
//
// The nonce is 96 random bits from crypto/rand, drawn fresh for every write.
// The additional data is the 5 byte header followed by the storage key, so a
// value cannot be moved to another key, and the header cannot be edited to
// point at a different key, without failing authentication.
//
// Keys are left in plaintext. The raft node stores its state under names such
// as "currentTerm", "votedFor", "log_<index>", "snapshot_<index>" and
// "snapshot_meta", which reveal only indexes. Leaving them readable keeps
// ordered range scans working on every backend. Code that puts sensitive data
// into key names (statemachine.PersistentStateMachine stores "sm/<key>") leaks
// those names; encrypt them before they become keys if that matters.
//
// The wrapper keeps its own metadata under keys that start with a zero byte,
// which no raft key does: one check value per key ID, which proves at start
// that a configured key is the one the data was written with, and a marker
// while a migration runs. GetRange never returns these keys and writes to
// them are refused.

const (
	// KeySize is the size of an encryption key: AES-256.
	KeySize = 32

	envelopeVersion1 byte = 1
	headerSize            = 1 + 4
	nonceSize             = 12
	tagSize               = 16

	// EnvelopeOverhead is the number of bytes encryption adds to each value.
	EnvelopeOverhead = headerSize + nonceSize + tagSize

	// migrationChunk is how many keys a migration or rekey handles per batch.
	migrationChunk = 256
)

var (
	reservedPrefix          = []byte("\x00forge-consensus/encryption/")
	checkValuePrefix        = []byte("\x00forge-consensus/encryption/key/")
	migratingToEncryptedKey = []byte("\x00forge-consensus/encryption/migrating-to-encrypted")
	migratingToPlaintextKey = []byte("\x00forge-consensus/encryption/migrating-to-plaintext")
	checkPlaintext          = []byte("forge-consensus encryption check value v1")

	// lastKey bounds a range scan over every key a backend can hold.
	lastKey = bytes.Repeat([]byte{0xff}, 1024)
)

var (
	// ErrInvalidEncryptionKey means a key could not be loaded or has the
	// wrong size. Its messages never include key material.
	ErrInvalidEncryptionKey = errors.New("consensus storage: invalid encryption key")

	// ErrDecrypt means a value failed authentication: the key is wrong, the
	// value was changed, or it was copied from another key.
	ErrDecrypt = errors.New("consensus storage: cannot decrypt value")

	// ErrUnknownKeyID means a value was written under a key ID that is not
	// configured.
	ErrUnknownKeyID = errors.New("consensus storage: value is encrypted with a key that is not configured")

	// ErrWrongKey means the configured keys do not match the keys the store
	// was written with.
	ErrWrongKey = errors.New("consensus storage: encryption key does not match this store")

	// ErrPlaintextStore means encryption is on but the store already holds
	// data written without it.
	ErrPlaintextStore = errors.New("consensus storage: store holds data written without encryption")

	// ErrEncryptedStore means encryption is off but the store was written
	// with it.
	ErrEncryptedStore = errors.New("consensus storage: store holds encrypted data")

	// ErrMigrationIncomplete means a migration between plaintext and
	// encrypted storage stopped before it finished.
	ErrMigrationIncomplete = errors.New("consensus storage: an encryption migration did not finish")

	// ErrReservedKey means a caller tried to write a key reserved for
	// encryption metadata.
	ErrReservedKey = errors.New("consensus storage: key is reserved for encryption metadata")
)

// Keyring holds the AES-256-GCM keys by ID. The active key encrypts new
// writes; every key in the ring can decrypt.
type Keyring struct {
	active uint32
	aeads  map[uint32]cipher.AEAD
	ids    []uint32
}

// NewKeyring builds a keyring from raw 32 byte keys. activeID selects the key
// used for writes; 0 selects the highest ID. Key ID 0 is not allowed. The
// caller may clear the key slices afterwards: the ring keeps only the
// expanded AES state.
func NewKeyring(activeID uint32, keys map[uint32][]byte) (*Keyring, error) {
	if len(keys) == 0 {
		return nil, fmt.Errorf("%w: no keys configured", ErrInvalidEncryptionKey)
	}

	kr := &Keyring{aeads: make(map[uint32]cipher.AEAD, len(keys))}

	for id, key := range keys {
		if id == 0 {
			return nil, fmt.Errorf("%w: key ID 0 is not allowed, IDs start at 1", ErrInvalidEncryptionKey)
		}

		if len(key) != KeySize {
			return nil, fmt.Errorf("%w: key %d is %d bytes, AES-256 needs exactly %d",
				ErrInvalidEncryptionKey, id, len(key), KeySize)
		}

		block, err := aes.NewCipher(key)
		if err != nil {
			return nil, fmt.Errorf("%w: key %d: %w", ErrInvalidEncryptionKey, id, err)
		}

		aead, err := cipher.NewGCM(block)
		if err != nil {
			return nil, fmt.Errorf("%w: key %d: %w", ErrInvalidEncryptionKey, id, err)
		}

		kr.aeads[id] = aead
		kr.ids = append(kr.ids, id)
	}

	slices.Sort(kr.ids)

	if activeID == 0 {
		activeID = kr.ids[len(kr.ids)-1]
	}

	if _, ok := kr.aeads[activeID]; !ok {
		return nil, fmt.Errorf("%w: active key ID %d is not among the configured keys %v",
			ErrInvalidEncryptionKey, activeID, kr.ids)
	}

	kr.active = activeID

	return kr, nil
}

// ActiveKeyID returns the ID of the key used for new writes.
func (k *Keyring) ActiveKeyID() uint32 { return k.active }

// KeyIDs returns the configured key IDs in ascending order.
func (k *Keyring) KeyIDs() []uint32 { return slices.Clone(k.ids) }

// Seal encrypts plaintext for storage under key with the active key.
func (k *Keyring) Seal(key, plaintext []byte) ([]byte, error) {
	return k.sealWith(k.active, key, plaintext)
}

// Open decrypts an envelope stored under key.
func (k *Keyring) Open(key, envelope []byte) ([]byte, error) {
	if len(envelope) < EnvelopeOverhead {
		return nil, fmt.Errorf("%w: value is %d bytes, shorter than an encrypted value", ErrDecrypt, len(envelope))
	}

	if envelope[0] != envelopeVersion1 {
		return nil, fmt.Errorf("%w: unknown format version %d", ErrDecrypt, envelope[0])
	}

	id := binary.BigEndian.Uint32(envelope[1:headerSize])

	aead, ok := k.aeads[id]
	if !ok {
		return nil, fmt.Errorf("%w: key ID %d (configured: %v)", ErrUnknownKeyID, id, k.ids)
	}

	nonce := envelope[headerSize : headerSize+nonceSize]

	plaintext, err := aead.Open(nil, nonce, envelope[headerSize+nonceSize:], additionalData(envelope[:headerSize], key))
	if err != nil {
		return nil, fmt.Errorf("%w: authentication failed with key ID %d "+
			"(wrong key, modified value, or value copied from another key)", ErrDecrypt, id)
	}

	return plaintext, nil
}

func (k *Keyring) sealWith(id uint32, key, plaintext []byte) ([]byte, error) {
	aead, ok := k.aeads[id]
	if !ok {
		return nil, fmt.Errorf("%w: key ID %d", ErrUnknownKeyID, id)
	}

	if len(plaintext) > math.MaxInt-EnvelopeOverhead {
		return nil, fmt.Errorf("consensus storage: value of %d bytes is too large to encrypt", len(plaintext))
	}

	out := make([]byte, headerSize+nonceSize, headerSize+nonceSize+len(plaintext)+tagSize)
	out[0] = envelopeVersion1
	binary.BigEndian.PutUint32(out[1:headerSize], id)

	nonce := out[headerSize : headerSize+nonceSize]
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("consensus storage: cannot read a random nonce: %w", err)
	}

	return aead.Seal(out, nonce, plaintext, additionalData(out[:headerSize], key)), nil
}

// envelopeKeyID returns the key ID in an envelope header.
func envelopeKeyID(envelope []byte) (uint32, bool) {
	if len(envelope) < EnvelopeOverhead || envelope[0] != envelopeVersion1 {
		return 0, false
	}

	return binary.BigEndian.Uint32(envelope[1:headerSize]), true
}

func additionalData(header, key []byte) []byte {
	ad := make([]byte, 0, len(header)+len(key))
	ad = append(ad, header...)

	return append(ad, key...)
}

func isReserved(key []byte) bool {
	return bytes.HasPrefix(key, reservedPrefix)
}

func checkValueKey(id uint32) []byte {
	return strconv.AppendUint(slices.Clone(checkValuePrefix), uint64(id), 10)
}

// EncryptedStorageConfig configures NewEncryptedStorage.
type EncryptedStorageConfig struct {
	// Keyring holds the keys. Required.
	Keyring *Keyring

	// MigratePlaintext lets Start encrypt a store that already holds data
	// written without encryption, in place, once. Without it Start refuses
	// such a store with ErrPlaintextStore.
	MigratePlaintext bool
}

// EncryptedStorage encrypts the values of another storage backend. See the
// comment at the top of this file for the format.
type EncryptedStorage struct {
	inner            internal.Storage
	keys             *Keyring
	logger           forge.Logger
	migratePlaintext bool

	// writeMu lets Rekey read, re-encrypt and write a chunk of values without
	// a concurrent write landing in between. Writes share it; Rekey takes it
	// exclusively for one chunk at a time.
	writeMu sync.RWMutex
}

var _ internal.Storage = (*EncryptedStorage)(nil)

// NewEncryptedStorage wraps inner. Start checks that the store matches: a new
// store is marked as encrypted, an encrypted store must be readable with one
// of the configured keys, and a plaintext store is refused unless
// MigratePlaintext is set.
func NewEncryptedStorage(inner internal.Storage, config EncryptedStorageConfig, logger forge.Logger) (*EncryptedStorage, error) {
	if inner == nil {
		return nil, errors.New("consensus storage: encrypted storage needs a backend")
	}

	if config.Keyring == nil {
		return nil, fmt.Errorf("%w: no keyring", ErrInvalidEncryptionKey)
	}

	return &EncryptedStorage{
		inner:            inner,
		keys:             config.Keyring,
		logger:           logger,
		migratePlaintext: config.MigratePlaintext,
	}, nil
}

// Start starts the backend and checks that it holds data this keyring can
// read. On failure the backend is stopped again.
func (s *EncryptedStorage) Start(ctx context.Context) error {
	if err := s.inner.Start(ctx); err != nil {
		return err
	}

	if err := s.prepare(); err != nil {
		_ = s.inner.Stop(context.WithoutCancel(ctx))

		return err
	}

	return nil
}

func (s *EncryptedStorage) prepare() error {
	state, err := readEncryptionState(s.inner)
	if err != nil {
		return err
	}

	if state.toPlaintext {
		return fmt.Errorf("%w: a migration to plaintext was interrupted; run storage.MigrateToPlaintext again "+
			"and start without encryption", ErrMigrationIncomplete)
	}

	if state.toEncrypted || len(state.checkValues) == 0 {
		hasData := state.toEncrypted

		if !hasData {
			if hasData, err = hasUserData(s.inner); err != nil {
				return err
			}
		}

		if !hasData {
			// A new store: record a check value for every configured key.
			return writeCheckValues(s.inner, s.keys, nil, nil)
		}

		if !s.migratePlaintext {
			if state.toEncrypted {
				return fmt.Errorf("%w: a migration to encrypted storage was interrupted; "+
					"start with security.encryption_migrate_plaintext to finish it", ErrMigrationIncomplete)
			}

			return fmt.Errorf("%w: refusing to mix plaintext and encrypted values; set "+
				"security.encryption_migrate_plaintext once to encrypt the existing data in place, "+
				"or start from an empty storage path", ErrPlaintextStore)
		}

		n, err := MigrateToEncrypted(s.inner, s.keys)
		if err != nil {
			return err
		}

		s.logger.Info("encrypted existing plaintext raft storage",
			forge.F("values", n),
			forge.F("key_id", s.keys.ActiveKeyID()),
		)

		return nil
	}

	if err := verifyCheckValues(s.keys, state.checkValues); err != nil {
		return err
	}

	return writeCheckValues(s.inner, s.keys, state.checkValues, nil)
}

// Stop stops the backend.
func (s *EncryptedStorage) Stop(ctx context.Context) error {
	return s.inner.Stop(ctx)
}

// Close closes the backend.
func (s *EncryptedStorage) Close() error {
	return s.inner.Close()
}

// Set encrypts value and stores it under key.
func (s *EncryptedStorage) Set(key, value []byte) error {
	if isReserved(key) {
		return fmt.Errorf("%w: %q", ErrReservedKey, key)
	}

	envelope, err := s.keys.Seal(key, value)
	if err != nil {
		return err
	}

	s.writeMu.RLock()
	defer s.writeMu.RUnlock()

	return s.inner.Set(key, envelope)
}

// Get returns the decrypted value stored under key. A missing key returns the
// backend's error unchanged, so callers still see internal.ErrNodeNotFound.
func (s *EncryptedStorage) Get(key []byte) ([]byte, error) {
	if isReserved(key) {
		return nil, internal.ErrNodeNotFound
	}

	envelope, err := s.inner.Get(key)
	if err != nil {
		return nil, err
	}

	value, err := s.keys.Open(key, envelope)
	if err != nil {
		return nil, fmt.Errorf("key %q: %w", key, err)
	}

	return value, nil
}

// Delete deletes key.
func (s *EncryptedStorage) Delete(key []byte) error {
	if isReserved(key) {
		return fmt.Errorf("%w: %q", ErrReservedKey, key)
	}

	s.writeMu.RLock()
	defer s.writeMu.RUnlock()

	return s.inner.Delete(key)
}

// Batch encrypts the values of the set operations and applies the batch
// atomically through the backend.
func (s *EncryptedStorage) Batch(ops []internal.BatchOp) error {
	sealed := make([]internal.BatchOp, len(ops))

	for i, op := range ops {
		if isReserved(op.Key) {
			return fmt.Errorf("batch operation %d: %w: %q", i, ErrReservedKey, op.Key)
		}

		sealed[i] = internal.BatchOp{Type: op.Type, Key: op.Key}

		if op.Type == internal.BatchOpSet {
			envelope, err := s.keys.Seal(op.Key, op.Value)
			if err != nil {
				return err
			}

			sealed[i].Value = envelope
		}
	}

	s.writeMu.RLock()
	defer s.writeMu.RUnlock()

	return s.inner.Batch(sealed)
}

// GetRange returns the decrypted key-value pairs in [start, end]. Encryption
// metadata is left out.
func (s *EncryptedStorage) GetRange(start, end []byte) ([]internal.KeyValue, error) {
	kvs, err := s.inner.GetRange(start, end)
	if err != nil {
		return nil, err
	}

	out := make([]internal.KeyValue, 0, len(kvs))

	for _, kv := range kvs {
		if isReserved(kv.Key) {
			continue
		}

		value, err := s.keys.Open(kv.Key, kv.Value)
		if err != nil {
			return nil, fmt.Errorf("key %q: %w", kv.Key, err)
		}

		out = append(out, internal.KeyValue{Key: kv.Key, Value: value})
	}

	return out, nil
}

// Keyring returns the keyring in use.
func (s *EncryptedStorage) Keyring() *Keyring { return s.keys }

// Rekey re-encrypts every value that is not under the active key and returns
// how many it rewrote. It is safe while the node runs: each chunk is read and
// rewritten while writes through this wrapper wait, so a concurrent write or
// delete is never overwritten with an older value. Once it returns, keys that
// are no longer active can be removed from the configuration.
func (s *EncryptedStorage) Rekey() (int, error) {
	keys, err := listUserKeys(s.inner)
	if err != nil {
		return 0, err
	}

	total := 0

	for chunk := range slices.Chunk(keys, migrationChunk) {
		n, err := s.rekeyChunk(chunk)
		total += n

		if err != nil {
			return total, err
		}
	}

	if total > 0 {
		s.logger.Info("re-encrypted raft storage under the active key",
			forge.F("values", total),
			forge.F("key_id", s.keys.ActiveKeyID()),
		)
	}

	return total, nil
}

func (s *EncryptedStorage) rekeyChunk(keys [][]byte) (int, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	ops := make([]internal.BatchOp, 0, len(keys))

	for _, key := range keys {
		envelope, err := s.inner.Get(key)
		if errors.Is(err, internal.ErrNodeNotFound) {
			continue
		}

		if err != nil {
			return 0, fmt.Errorf("key %q: %w", key, err)
		}

		if id, ok := envelopeKeyID(envelope); ok && id == s.keys.ActiveKeyID() {
			continue
		}

		value, err := s.keys.Open(key, envelope)
		if err != nil {
			return 0, fmt.Errorf("key %q: %w", key, err)
		}

		resealed, err := s.keys.Seal(key, value)
		if err != nil {
			return 0, err
		}

		ops = append(ops, internal.BatchOp{Type: internal.BatchOpSet, Key: key, Value: resealed})
	}

	if len(ops) == 0 {
		return 0, nil
	}

	if err := s.inner.Batch(ops); err != nil {
		return 0, err
	}

	return len(ops), nil
}

// NewPlaintextGuard wraps a backend that is used without encryption. Its
// Start fails with ErrEncryptedStore when the store was written with
// encryption, instead of letting the raft node read ciphertext as data.
func NewPlaintextGuard(inner internal.Storage) internal.Storage {
	return &plaintextGuard{Storage: inner}
}

type plaintextGuard struct {
	internal.Storage
}

func (g *plaintextGuard) Start(ctx context.Context) error {
	if err := g.Storage.Start(ctx); err != nil {
		return err
	}

	state, err := readEncryptionState(g.Storage)

	switch {
	case err != nil:
	case state.toEncrypted || state.toPlaintext:
		err = fmt.Errorf("%w: finish the migration before starting without encryption", ErrMigrationIncomplete)
	case len(state.checkValues) > 0:
		err = fmt.Errorf("%w: it was written with security.enable_encryption; start with encryption and "+
			"the same keys, or decrypt it once with storage.MigrateToPlaintext", ErrEncryptedStore)
	}

	if err != nil {
		_ = g.Stop(context.WithoutCancel(ctx))

		return err
	}

	return nil
}

// MigrateToEncrypted encrypts, in place, every value of a store written
// without encryption, and marks the store as encrypted. inner must be
// started and must not be in use by anything else. An interrupted run leaves
// a marker that stops the store from being opened either way, and running
// the migration again finishes it: values that already decrypt are skipped.
// It returns how many values it encrypted.
//
// Backends may keep the old plaintext in free pages or old value log files
// until they compact; see the README.
func MigrateToEncrypted(inner internal.Storage, keys *Keyring) (int, error) {
	state, err := readEncryptionState(inner)
	if err != nil {
		return 0, err
	}

	if state.toPlaintext {
		return 0, fmt.Errorf("%w: a migration to plaintext was interrupted; finish it first", ErrMigrationIncomplete)
	}

	if len(state.checkValues) > 0 && !state.toEncrypted {
		return 0, fmt.Errorf("%w: it is already encrypted", ErrEncryptedStore)
	}

	if err := inner.Set(migratingToEncryptedKey, []byte{1}); err != nil {
		return 0, fmt.Errorf("failed to mark migration start: %w", err)
	}

	userKeys, err := listUserKeys(inner)
	if err != nil {
		return 0, err
	}

	total := 0

	for chunk := range slices.Chunk(userKeys, migrationChunk) {
		ops := make([]internal.BatchOp, 0, len(chunk))

		for _, key := range chunk {
			value, err := inner.Get(key)
			if errors.Is(err, internal.ErrNodeNotFound) {
				continue
			}

			if err != nil {
				return total, fmt.Errorf("key %q: %w", key, err)
			}

			// Encrypted by an earlier, interrupted run.
			if _, err := keys.Open(key, value); err == nil {
				continue
			}

			envelope, err := keys.Seal(key, value)
			if err != nil {
				return total, err
			}

			ops = append(ops, internal.BatchOp{Type: internal.BatchOpSet, Key: key, Value: envelope})
		}

		if len(ops) > 0 {
			if err := inner.Batch(ops); err != nil {
				return total, err
			}

			total += len(ops)
		}
	}

	finish := []internal.BatchOp{{Type: internal.BatchOpDelete, Key: migratingToEncryptedKey}}

	return total, writeCheckValues(inner, keys, state.checkValues, finish)
}

// MigrateToPlaintext decrypts, in place, every value of an encrypted store
// and removes its encryption metadata, so it can be started without
// encryption. inner must be started and must not be in use by anything else.
// Like MigrateToEncrypted it can be run again after an interruption. It
// returns how many values it decrypted.
func MigrateToPlaintext(inner internal.Storage, keys *Keyring) (int, error) {
	state, err := readEncryptionState(inner)
	if err != nil {
		return 0, err
	}

	if state.toEncrypted {
		return 0, fmt.Errorf("%w: a migration to encrypted storage was interrupted; finish it first", ErrMigrationIncomplete)
	}

	resuming := state.toPlaintext

	if !resuming {
		if len(state.checkValues) == 0 {
			return 0, fmt.Errorf("%w: it is not encrypted", ErrPlaintextStore)
		}

		if err := verifyCheckValues(keys, state.checkValues); err != nil {
			return 0, err
		}

		if err := inner.Set(migratingToPlaintextKey, []byte{1}); err != nil {
			return 0, fmt.Errorf("failed to mark migration start: %w", err)
		}
	}

	userKeys, err := listUserKeys(inner)
	if err != nil {
		return 0, err
	}

	total := 0

	for chunk := range slices.Chunk(userKeys, migrationChunk) {
		ops := make([]internal.BatchOp, 0, len(chunk))

		for _, key := range chunk {
			envelope, err := inner.Get(key)
			if errors.Is(err, internal.ErrNodeNotFound) {
				continue
			}

			if err != nil {
				return total, fmt.Errorf("key %q: %w", key, err)
			}

			value, err := keys.Open(key, envelope)
			if err != nil {
				// An interrupted run already wrote this value in plaintext.
				if resuming {
					continue
				}

				return total, fmt.Errorf("key %q: %w", key, err)
			}

			ops = append(ops, internal.BatchOp{Type: internal.BatchOpSet, Key: key, Value: value})
		}

		if len(ops) > 0 {
			if err := inner.Batch(ops); err != nil {
				return total, err
			}

			total += len(ops)
		}
	}

	finish := make([]internal.BatchOp, 0, len(state.checkValues)+1)
	for id := range state.checkValues {
		finish = append(finish, internal.BatchOp{Type: internal.BatchOpDelete, Key: checkValueKey(id)})
	}

	finish = append(finish, internal.BatchOp{Type: internal.BatchOpDelete, Key: migratingToPlaintextKey})

	return total, inner.Batch(finish)
}

// encryptionState is the encryption metadata found in a store.
type encryptionState struct {
	checkValues map[uint32][]byte
	toEncrypted bool
	toPlaintext bool
}

func readEncryptionState(st internal.Storage) (encryptionState, error) {
	state := encryptionState{checkValues: make(map[uint32][]byte)}

	end := append(slices.Clone(reservedPrefix), 0xff)

	kvs, err := st.GetRange(reservedPrefix, end)
	if err != nil {
		return state, fmt.Errorf("failed to read encryption metadata: %w", err)
	}

	for _, kv := range kvs {
		switch {
		case bytes.Equal(kv.Key, migratingToEncryptedKey):
			state.toEncrypted = true
		case bytes.Equal(kv.Key, migratingToPlaintextKey):
			state.toPlaintext = true
		case bytes.HasPrefix(kv.Key, checkValuePrefix):
			id, err := strconv.ParseUint(string(kv.Key[len(checkValuePrefix):]), 10, 32)
			if err != nil || id == 0 {
				return state, fmt.Errorf("%w: malformed encryption metadata key %q", ErrWrongKey, kv.Key)
			}

			state.checkValues[uint32(id)] = kv.Value
		}
	}

	return state, nil
}

// verifyCheckValues confirms that every configured key with a check value in
// the store decrypts it, and that at least one configured key does.
func verifyCheckValues(keys *Keyring, checkValues map[uint32][]byte) error {
	verified := 0

	for id, envelope := range checkValues {
		if _, ok := keys.aeads[id]; !ok {
			continue
		}

		plaintext, err := keys.Open(checkValueKey(id), envelope)
		if err != nil || !bytes.Equal(plaintext, checkPlaintext) {
			return fmt.Errorf("%w: the key configured as ID %d is not the key this store was written with under that ID", ErrWrongKey, id)
		}

		verified++
	}

	if verified == 0 {
		stored := make([]uint32, 0, len(checkValues))
		for id := range checkValues {
			stored = append(stored, id)
		}

		slices.Sort(stored)

		return fmt.Errorf("%w: the store was written with key IDs %v and none of them is configured (configured: %v)",
			ErrWrongKey, stored, keys.ids)
	}

	return nil
}

// writeCheckValues stores a check value for every configured key that does
// not have one yet, together with extra, in one batch.
func writeCheckValues(st internal.Storage, keys *Keyring, existing map[uint32][]byte, extra []internal.BatchOp) error {
	ops := slices.Clone(extra)

	for _, id := range keys.ids {
		if _, ok := existing[id]; ok {
			continue
		}

		key := checkValueKey(id)

		envelope, err := keys.sealWith(id, key, checkPlaintext)
		if err != nil {
			return err
		}

		ops = append(ops, internal.BatchOp{Type: internal.BatchOpSet, Key: key, Value: envelope})
	}

	if len(ops) == 0 {
		return nil
	}

	if err := st.Batch(ops); err != nil {
		return fmt.Errorf("failed to write encryption metadata: %w", err)
	}

	return nil
}

type keyLister interface {
	ListKeys(prefix []byte) ([][]byte, error)
}

// listUserKeys returns every key in the store except encryption metadata.
func listUserKeys(st internal.Storage) ([][]byte, error) {
	var all [][]byte

	if lister, ok := st.(keyLister); ok {
		keys, err := lister.ListKeys(nil)
		if err != nil {
			return nil, fmt.Errorf("failed to list keys: %w", err)
		}

		all = keys
	} else {
		kvs, err := st.GetRange([]byte{}, lastKey)
		if err != nil {
			return nil, fmt.Errorf("failed to list keys: %w", err)
		}

		for _, kv := range kvs {
			all = append(all, kv.Key)
		}
	}

	return slices.DeleteFunc(all, isReserved), nil
}

func hasUserData(st internal.Storage) (bool, error) {
	keys, err := listUserKeys(st)

	return len(keys) > 0, err
}
