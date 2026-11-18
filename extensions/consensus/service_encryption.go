package consensus

import (
	"fmt"

	"github.com/xraph/forge"
	"github.com/xraph/forge/errors"
	"github.com/xraph/forge/extensions/consensus/internal"
	"github.com/xraph/forge/extensions/consensus/storage"
)

// ErrStorageNotEncrypted is returned by ReencryptStorage when the service runs
// without security.enable_encryption.
var ErrStorageNotEncrypted = errors.New("consensus: storage encryption is not enabled")

// protectStorage applies security.enable_encryption to the storage backend.
// With encryption the backend is wrapped in storage.EncryptedStorage, built
// from keys read from files or environment variables. Without it the backend
// is wrapped in a guard that refuses a store written with encryption, so
// ciphertext is never read as raft state.
func protectStorage(cfg internal.Config, store internal.Storage, logger forge.Logger) (internal.Storage, error) {
	if !cfg.Security.EnableEncryption {
		return storage.NewPlaintextGuard(store), nil
	}

	sources := cfg.Security.EncryptionKeySources()
	keys := make([]storage.KeySource, len(sources))

	for i, src := range sources {
		keys[i] = storage.KeySource{ID: src.ID, File: src.File, Env: src.Env, Value: src.Value}
	}

	keyring, err := storage.LoadKeyring(storage.KeyringConfig{
		Keys:        keys,
		ActiveKeyID: cfg.Security.EncryptionActiveKeyID,
		AllowInline: cfg.Security.AllowInlineEncryptionKey,
	}, logger)
	if err != nil {
		return nil, err
	}

	logger.Info("raft storage encryption enabled",
		forge.F("cipher", "AES-256-GCM"),
		forge.F("active_key_id", keyring.ActiveKeyID()),
		forge.F("key_ids", keyring.KeyIDs()),
	)

	return storage.NewEncryptedStorage(store, storage.EncryptedStorageConfig{
		Keyring:          keyring,
		MigratePlaintext: cfg.Security.EncryptionMigratePlaintext,
	}, logger)
}

// ReencryptStorage re-encrypts every stored value that is not under the
// active key and returns how many it rewrote. It runs while the node serves
// requests. Afterwards the older keys can be removed from the configuration.
func (s *Service) ReencryptStorage() (int, error) {
	s.mu.RLock()
	started, store := s.started, s.storage
	s.mu.RUnlock()

	if !started {
		return 0, ErrNotStarted
	}

	encrypted, ok := store.(*storage.EncryptedStorage)
	if !ok {
		return 0, ErrStorageNotEncrypted
	}

	n, err := encrypted.Rekey()
	if err != nil {
		return n, fmt.Errorf("failed to re-encrypt storage: %w", err)
	}

	return n, nil
}
