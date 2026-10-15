package observability

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// ErrInvalidTarget reports a --target value that isn't name=host:port.
var ErrInvalidTarget = errors.New("observability: invalid target")

// Target is one app Prometheus scrapes.
type Target struct {
	Name    string // becomes the job label
	Address string // host:port as seen from inside the container
	// Rewritten is true when ParseTarget replaced a loopback host with
	// host.docker.internal.
	Rewritten bool
}

var unsafeFileChars = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

// ParseTarget reads name=host:port. A loopback host means the developer's
// machine, which the container reaches as host.docker.internal.
func ParseTarget(s string) (Target, error) {
	name, addr, ok := strings.Cut(s, "=")
	if !ok || name == "" {
		return Target{}, fmt.Errorf("%w %q: want name=host:port", ErrInvalidTarget, s)
	}

	host, port, err := net.SplitHostPort(addr)
	if err != nil || host == "" {
		return Target{}, fmt.Errorf("%w %q: want name=host:port", ErrInvalidTarget, s)
	}

	if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
		return Target{}, fmt.Errorf("%w %q: port must be 1 to 65535", ErrInvalidTarget, s)
	}

	t := Target{Name: name, Address: addr}
	if host == "localhost" || net.ParseIP(host).IsLoopback() {
		t.Address = net.JoinHostPort("host.docker.internal", port)
		t.Rewritten = true
	}

	return t, nil
}

func targetPath(dir, name string) string {
	return filepath.Join(dir, "targets", strings.Trim(unsafeFileChars.ReplaceAllString(name, "-"), "-")+".json")
}

// WriteTarget writes a file_sd file for t. It writes a temporary file and
// renames it, so Prometheus never reads half a file.
func WriteTarget(dir string, t Target) error {
	raw, err := json.Marshal([]map[string]any{{"targets": []string{t.Address}, "labels": map[string]string{"job": t.Name}}})
	if err != nil {
		return err
	}

	path := targetPath(dir, t.Name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("observability: create targets dir: %w", err)
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return fmt.Errorf("observability: write target %s: %w", t.Name, err)
	}

	return os.Rename(tmp, path)
}

// RemoveTarget deletes a target file. A missing file is not an error.
func RemoveTarget(dir, name string) error {
	if err := os.Remove(targetPath(dir, name)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("observability: remove target %s: %w", name, err)
	}

	return nil
}
