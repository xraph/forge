package internal

import (
	"fmt"
	"slices"
)

// EncryptionKeyConfig is one encryption at rest key. Exactly one of File, Env
// and Value is set. Keys are 32 bytes (AES-256): a file holds 32 raw bytes,
// 64 hex characters or base64 text; an environment variable or inline value
// holds hex or base64 text.
//
// To rotate, add a key with a new ID and make it active. Values written
// earlier stay readable with the older key until they are re-encrypted
// (Service.ReencryptStorage) or replaced, after which the old key can be
// removed.
type EncryptionKeyConfig struct {
	// ID is stored in every value the key encrypts. IDs start at 1 and must
	// never be reused for a different key.
	ID    uint32 `json:"id"    yaml:"id"`
	File  string `json:"file"  yaml:"file"`
	Env   string `json:"env"   yaml:"env"`
	Value string `json:"value" yaml:"value"`
}

// EncryptionKeySources returns the configured keys as a list: EncryptionKeys
// when set, otherwise the single key settings as key ID 1.
func (s SecurityConfig) EncryptionKeySources() []EncryptionKeyConfig {
	if len(s.EncryptionKeys) > 0 {
		return slices.Clone(s.EncryptionKeys)
	}

	single := EncryptionKeyConfig{ID: 1, File: s.EncryptionKeyFile, Env: s.EncryptionKeyEnv, Value: s.EncryptionKey}
	if single.File == "" && single.Env == "" && single.Value == "" {
		return nil
	}

	return []EncryptionKeyConfig{single}
}

// validateEncryption checks the shape of the encryption settings. Reading the
// keys and checking their length happens when the service starts.
func (s SecurityConfig) validateEncryption() error {
	if !s.EnableEncryption {
		return nil
	}

	single := 0

	for _, v := range []string{s.EncryptionKeyFile, s.EncryptionKeyEnv, s.EncryptionKey} {
		if v != "" {
			single++
		}
	}

	if len(s.EncryptionKeys) > 0 && single > 0 {
		return fmt.Errorf("%w: set either security.encryption_keys or one of encryption_key_file, "+
			"encryption_key_env and encryption_key, not both", ErrInvalidConfig)
	}

	keys := s.EncryptionKeySources()
	if len(keys) == 0 {
		return fmt.Errorf("%w: security.enable_encryption needs a key: set security.encryption_key_file or "+
			"encryption_key_env, or encryption_keys to rotate keys", ErrInvalidConfig)
	}

	seen := make(map[uint32]bool, len(keys))

	for _, k := range keys {
		if k.ID == 0 {
			return fmt.Errorf("%w: security.encryption_keys: key IDs start at 1", ErrInvalidConfig)
		}

		if seen[k.ID] {
			return fmt.Errorf("%w: security.encryption_keys: key ID %d is listed twice", ErrInvalidConfig, k.ID)
		}

		seen[k.ID] = true

		sources := 0

		for _, v := range []string{k.File, k.Env, k.Value} {
			if v != "" {
				sources++
			}
		}

		if sources != 1 {
			return fmt.Errorf("%w: encryption key %d needs exactly one of a file, an environment variable "+
				"or an inline value", ErrInvalidConfig, k.ID)
		}

		if k.Value != "" && !s.AllowInlineEncryptionKey {
			return fmt.Errorf("%w: encryption key %d is written inline in the configuration; use a key file "+
				"or an environment variable, or set security.allow_inline_encryption_key", ErrInvalidConfig, k.ID)
		}
	}

	if s.EncryptionActiveKeyID != 0 && !seen[s.EncryptionActiveKeyID] {
		return fmt.Errorf("%w: security.encryption_active_key_id %d is not a configured key",
			ErrInvalidConfig, s.EncryptionActiveKeyID)
	}

	return nil
}
