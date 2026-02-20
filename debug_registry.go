//go:build forge_debug

package forge

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type debugRegistryFile struct {
	Servers []DebugServerEntry `json:"servers"`
}

var debugRegistryMu sync.Mutex

func debugRegistryPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".forge", "debug-servers.json"), nil
}

func debugRegisterServer(entry DebugServerEntry) error {
	debugRegistryMu.Lock()
	defer debugRegistryMu.Unlock()

	path, err := debugRegistryPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	reg := debugReadRegistry(path)

	// Remove stale entries for this pid or debug address.
	filtered := reg.Servers[:0]
	for _, s := range reg.Servers {
		if s.PID != entry.PID && s.DebugAddr != entry.DebugAddr {
			filtered = append(filtered, s)
		}
	}

	entry.PID = os.Getpid()
	entry.StartedAt = time.Now().Format(time.RFC3339)
	reg.Servers = append(filtered, entry)

	return debugWriteRegistry(path, reg)
}

func debugUnregisterServer(debugAddr string) {
	debugRegistryMu.Lock()
	defer debugRegistryMu.Unlock()

	path, err := debugRegistryPath()
	if err != nil {
		return
	}
	reg := debugReadRegistry(path)
	pid := os.Getpid()
	filtered := reg.Servers[:0]
	for _, s := range reg.Servers {
		if s.PID != pid && s.DebugAddr != debugAddr {
			filtered = append(filtered, s)
		}
	}
	reg.Servers = filtered
	_ = debugWriteRegistry(path, reg)
}

func debugReadRegistry(path string) debugRegistryFile {
	data, err := os.ReadFile(path)
	if err != nil {
		return debugRegistryFile{}
	}
	var reg debugRegistryFile
	_ = json.Unmarshal(data, &reg)
	return reg
}

func debugWriteRegistry(path string, reg debugRegistryFile) error {
	data, err := json.MarshalIndent(reg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
