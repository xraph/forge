package images

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/xraph/forge/cmd/forge/internal/deploy/execx"
)

var connectionName = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)
var registryName = regexp.MustCompile(`^[a-zA-Z0-9.-]+(?::[0-9]{1,5})?$`)

type registryConnection struct {
	Host      string `json:"host"`
	Username  string `json:"username"`
	Directory string `json:"directory"`
}
type dockerConfig struct {
	Auths map[string]struct {
		Auth          string `json:"auth,omitempty"`
		IdentityToken string `json:"identitytoken,omitempty"`
	} `json:"auths"`
}

func validRegistry(host string) bool {
	if !registryName.MatchString(host) || strings.HasPrefix(host, "-") {
		return false
	}

	if strings.Contains(host, ":") {
		_, port, err := net.SplitHostPort(host)

		return err == nil && port != "0"
	}

	return true
}
func registryHasAuth(raw []byte, host string) bool {
	var cfg dockerConfig
	if json.Unmarshal(raw, &cfg) != nil {
		return false
	}

	keys := []string{host}
	if host == "docker.io" || host == "registry-1.docker.io" {
		keys = append(keys, "https://index.docker.io/v1/")
	}

	for _, key := range keys {
		auth := cfg.Auths[key]
		if auth.Auth != "" || auth.IdentityToken != "" {
			return true
		}
	}

	return false
}
func randomName() (string, error) {
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}

	return hex.EncodeToString(nonce), nil
}

// Connect publishes a private connection only after Docker persists its login.
func Connect(ctx context.Context, runner execx.Runner, projectRoot, name, host, username, token string) error {
	if !connectionName.MatchString(name) || !validRegistry(host) || username == "" || token == "" || strings.ContainsAny(username, "\r\n") {
		return errors.New("registry name, host, username and credential are required")
	}

	root, err := os.OpenRoot(projectRoot)
	if err != nil {
		return err
	}
	defer root.Close()

	nonce, err := randomName()
	if err != nil {
		return err
	}

	dir := filepath.Join(".forge", "connections", "registries", name, nonce)
	if err := root.MkdirAll(dir, 0700); err != nil {
		return err
	}

	_ = root.Chmod(".forge", 0700)

	published := false
	defer func() {
		if !published {
			_ = root.RemoveAll(dir)
		}
	}()
	// An explicit auth entry prevents Docker from selecting a global credential helper.
	initial := []byte(`{"auths":{` + strconv.Quote(host) + `:{}}}`)
	if err := privateWrite(root, filepath.Join(dir, "config.json"), initial); err != nil {
		return err
	}

	command := execx.Command{Name: "docker", Args: []string{"--config", filepath.Join(projectRoot, dir), "login", host, "--username", username, "--password-stdin"}, Dir: projectRoot, Stdin: strings.NewReader(token)}
	if _, err := runner.Run(ctx, command); err != nil {
		return errors.New("registry authentication failed")
	}

	raw, err := root.ReadFile(filepath.Join(dir, "config.json"))
	if err != nil || !registryHasAuth(raw, host) {
		return errors.New("registry login did not persist credentials")
	}

	if err := root.Chmod(filepath.Join(dir, "config.json"), 0600); err != nil {
		return err
	}

	metadata, err := json.Marshal(registryConnection{Host: host, Username: username, Directory: dir})
	if err != nil {
		return err
	}

	if err := privateWrite(root, filepath.Join(".forge", "connections", "registries", name+".json"), metadata); err != nil {
		return err
	}

	published = true

	return nil
}

// ConnectionDir returns a host-bound isolated Docker config, never a credential value.
func ConnectionDir(projectRoot, name, host string) (string, error) {
	if !connectionName.MatchString(name) || !validRegistry(host) {
		return "", errors.New("invalid registry connection")
	}

	root, err := os.OpenRoot(projectRoot)
	if err != nil {
		return "", err
	}
	defer root.Close()

	raw, err := root.ReadFile(filepath.Join(".forge", "connections", "registries", name+".json"))
	if err != nil {
		return "", errors.New("registry connection is unavailable; connect it first")
	}

	var connection registryConnection
	if json.Unmarshal(raw, &connection) != nil || connection.Host != host || !filepath.IsLocal(connection.Directory) || !strings.HasPrefix(filepath.ToSlash(connection.Directory), ".forge/connections/registries/"+name+"/") {
		return "", errors.New("registry connection does not match the requested host")
	}

	raw, err = root.ReadFile(filepath.Join(connection.Directory, "config.json"))
	if err != nil || !registryHasAuth(raw, host) {
		return "", errors.New("registry connection credentials are unavailable")
	}

	info, err := root.Stat(filepath.Join(connection.Directory, "config.json"))
	if err != nil || info.Mode().Perm() != 0600 {
		return "", errors.New("registry connection must have private permissions")
	}

	return filepath.Join(projectRoot, connection.Directory), nil
}
func privateWrite(root *os.Root, path string, raw []byte) error {
	nonce, err := randomName()
	if err != nil {
		return err
	}

	temp := filepath.Join(filepath.Dir(path), ".write-"+nonce)

	file, err := root.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer func() { _ = root.Remove(temp) }()

	if _, err := file.Write(raw); err != nil {
		file.Close()

		return err
	}

	if err := file.Sync(); err != nil {
		file.Close()

		return err
	}

	if err := file.Close(); err != nil {
		return err
	}

	return root.Rename(temp, path)
}
