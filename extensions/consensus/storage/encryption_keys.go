package storage

import (
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/xraph/forge"
)

// KeySource says where one encryption key comes from. Exactly one of File,
// Env and Value is set.
type KeySource struct {
	// ID identifies the key in every value it encrypts. IDs start at 1.
	ID uint32

	// File is a file holding the key: 32 raw bytes, 64 hex characters or
	// base64 text.
	File string

	// Env names an environment variable holding the key as 64 hex
	// characters or base64 text.
	Env string

	// Value is the key itself, as hex or base64 text. It is refused unless
	// KeyringConfig.AllowInline is set, because keys written into
	// configuration tend to end up in version control and logs.
	Value string
}

// KeyringConfig configures LoadKeyring.
type KeyringConfig struct {
	Keys []KeySource

	// ActiveKeyID selects the key for new writes. 0 selects the highest ID.
	ActiveKeyID uint32

	// AllowInline permits KeySource.Value.
	AllowInline bool
}

// LoadKeyring reads every key, checks that it is a 32 byte AES-256 key, and
// builds a keyring. Errors name the key ID and its source but never contain
// key material. A key file that other users can read is logged as a warning.
func LoadKeyring(config KeyringConfig, logger forge.Logger) (*Keyring, error) {
	if len(config.Keys) == 0 {
		return nil, fmt.Errorf("%w: no keys configured", ErrInvalidEncryptionKey)
	}

	keys := make(map[uint32][]byte, len(config.Keys))

	defer func() {
		for _, key := range keys {
			clear(key)
		}
	}()

	for _, src := range config.Keys {
		if _, dup := keys[src.ID]; dup {
			return nil, fmt.Errorf("%w: key ID %d is configured twice", ErrInvalidEncryptionKey, src.ID)
		}

		key, err := readKeySource(src, config.AllowInline, logger)
		if err != nil {
			return nil, err
		}

		keys[src.ID] = key
	}

	return NewKeyring(config.ActiveKeyID, keys)
}

func readKeySource(src KeySource, allowInline bool, logger forge.Logger) ([]byte, error) {
	sources := 0

	for _, s := range []string{src.File, src.Env, src.Value} {
		if s != "" {
			sources++
		}
	}

	if sources != 1 {
		return nil, fmt.Errorf("%w: key %d needs exactly one of a file, an environment variable or an inline value",
			ErrInvalidEncryptionKey, src.ID)
	}

	switch {
	case src.File != "":
		path := filepath.Clean(src.File)

		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("%w: key %d: cannot read key file %s: %w", ErrInvalidEncryptionKey, src.ID, path, err)
		}

		defer clear(raw)

		warnIfShared(path, src.ID, logger)

		key, err := decodeKey(raw, true)
		if err != nil {
			return nil, fmt.Errorf("%w: key %d in file %s: %w", ErrInvalidEncryptionKey, src.ID, path, err)
		}

		return key, nil
	case src.Env != "":
		text, ok := os.LookupEnv(src.Env)
		if !ok || text == "" {
			return nil, fmt.Errorf("%w: key %d: environment variable %s is not set", ErrInvalidEncryptionKey, src.ID, src.Env)
		}

		key, err := decodeKey([]byte(text), false)
		if err != nil {
			return nil, fmt.Errorf("%w: key %d in environment variable %s: %w", ErrInvalidEncryptionKey, src.ID, src.Env, err)
		}

		return key, nil
	default:
		if !allowInline {
			return nil, fmt.Errorf("%w: key %d is written inline in the configuration; use a key file or an "+
				"environment variable, or allow inline keys explicitly", ErrInvalidEncryptionKey, src.ID)
		}

		key, err := decodeKey([]byte(src.Value), false)
		if err != nil {
			return nil, fmt.Errorf("%w: inline key %d: %w", ErrInvalidEncryptionKey, src.ID, err)
		}

		return key, nil
	}
}

// decodeKey accepts 32 raw bytes (files only), 64 hex characters, or base64
// with or without padding. Decoder errors are not wrapped because some of
// them quote the offending character.
func decodeKey(raw []byte, allowRaw bool) ([]byte, error) {
	if allowRaw && len(raw) == KeySize {
		return append([]byte(nil), raw...), nil
	}

	text := strings.TrimSpace(string(raw))

	var (
		key []byte
		err error
	)

	switch len(text) {
	case hex.EncodedLen(KeySize):
		key, err = hex.DecodeString(text)
	case base64.StdEncoding.EncodedLen(KeySize):
		key, err = base64.StdEncoding.DecodeString(text)
		if err != nil {
			key, err = base64.URLEncoding.DecodeString(text)
		}
	case base64.RawStdEncoding.EncodedLen(KeySize):
		key, err = base64.RawStdEncoding.DecodeString(text)
		if err != nil {
			key, err = base64.RawURLEncoding.DecodeString(text)
		}
	default:
		return nil, fmt.Errorf("the key must be %d bytes: %d raw bytes in a file, %d hex characters or %d base64 characters; "+
			"got %d characters", KeySize, KeySize, hex.EncodedLen(KeySize), base64.StdEncoding.EncodedLen(KeySize), len(text))
	}

	if err != nil || len(key) != KeySize {
		return nil, fmt.Errorf("the key is not valid hex or base64 for a %d byte key", KeySize)
	}

	return key, nil
}

// warnIfShared logs when a key file is readable or writable by group or
// others. It does not fail: some deployments mount secrets with wider modes
// and protect them another way.
func warnIfShared(path string, id uint32, logger forge.Logger) {
	if logger == nil || runtime.GOOS == "windows" {
		return
	}

	info, err := os.Stat(path)
	if err != nil {
		return
	}

	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		logger.Warn("encryption key file is accessible to other users; restrict it to mode 0600",
			forge.F("key_id", id),
			forge.F("path", path),
			forge.F("mode", fmt.Sprintf("%#o", perm)),
		)
	}
}
