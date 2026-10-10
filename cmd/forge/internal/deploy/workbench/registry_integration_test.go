//go:build integration

package workbench

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xraph/forge/cmd/forge/internal/deploy/engine"
	"github.com/xraph/forge/cmd/forge/internal/deploy/testdata"
	"golang.org/x/crypto/bcrypt"
)

func TestWorkbenchRegistryConnection(t *testing.T) {
	binary := os.Getenv("FORGE_DEPLOY_CLI")
	if binary == "" {
		t.Fatal("set FORGE_DEPLOY_CLI to the freshly built CLI")
	}

	root := testdata.Copy(t, "atlas-v2")

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()

	run := func(args ...string) string {
		t.Helper()

		raw, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
		if err != nil {
			t.Fatalf("owned registry command failed: %v", err)
		}

		return strings.TrimSpace(string(raw))
	}
	if strings.Contains(run("info", "--format", "{{.OperatingSystem}}"), "Docker Desktop") {
		if os.Getenv("FORGE_DEPLOY_REQUIRE_REGISTRY") == "1" {
			t.Fatal("registry qualification is required and cannot run on this Docker endpoint")
		}

		t.Skip("private loopback registry qualification requires a native Linux Docker endpoint")
	}

	name := fmt.Sprintf("forge-workbench-registry-%d", time.Now().UnixNano())
	token := "workbench-acceptance-private-token"

	hash, err := bcrypt.GenerateFromPassword([]byte(token), bcrypt.DefaultCost)
	if err != nil {
		t.Fatal(err)
	}

	auth := filepath.Join(root, "registry-auth")
	if err := os.Mkdir(auth, 0700); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(auth, "htpasswd"), []byte("forge:"+string(hash)+"\n"), 0600); err != nil {
		t.Fatal(err)
	}

	run("create", "--name", name, "--user", "0", "-p", "127.0.0.1::5000", "-e", "REGISTRY_AUTH=htpasswd", "-e", "REGISTRY_AUTH_HTPASSWD_REALM=Forge", "-e", "REGISTRY_AUTH_HTPASSWD_PATH=/auth/htpasswd", "registry:3")
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 30*time.Second)
		defer stop()

		if err := exec.CommandContext(cleanup, "docker", "rm", "-f", "-v", name).Run(); err != nil {
			t.Errorf("owned registry cleanup: %v", err)
		}
	})
	run("cp", auth, name+":/auth")
	run("start", name)
	host := "127.0.0.1:" + run("inspect", "--format", `{{(index (index .NetworkSettings.Ports "5000/tcp") 0).HostPort}}`, name)
	client := &http.Client{Timeout: 2 * time.Second}

	for {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+host+"/v2/", nil)
		if err != nil {
			t.Fatal(err)
		}

		response, err := client.Do(request)
		if err == nil {
			_ = response.Body.Close()
			if response.StatusCode == http.StatusUnauthorized {
				break
			}
		}

		select {
		case <-ctx.Done():
			t.Fatal("registry did not become ready")
		case <-time.After(100 * time.Millisecond):
		}
	}

	page := startLivePage(t, binary, root)
	wrong := "wrong-acceptance-token"

	raw, err := json.Marshal(map[string]string{"name": "denied", "host": host, "username": "forge", "token": wrong})
	if err != nil {
		t.Fatal(err)
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, page.origin+"/api/connections/registry", bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}

	request.Header.Set("X-Forge-Workbench", "1")
	request.Header.Set("Origin", page.origin)
	request.Header.Set("Content-Type", "application/json")

	response, err := page.client.Do(request)
	if err != nil {
		t.Fatal(err)
	}

	raw, err = io.ReadAll(response.Body)

	_ = response.Body.Close()
	if err != nil || response.StatusCode < 400 || bytes.Contains(raw, []byte(wrong)) {
		t.Fatal("denied registry authentication was accepted or leaked credentials")
	}

	page.call(t, "connections/registry", map[string]string{"name": "registry-it", "host": host, "username": "forge", "token": token}, nil)

	var connections []engine.Connection
	page.call(t, "connections", nil, &connections)

	if len(connections) != 1 || !connections[0].Connected || connections[0].Host != host {
		t.Fatal("authenticated registry metadata missing")
	}

	raw, err = json.Marshal(connections)
	if err != nil {
		t.Fatal(err)
	}

	if bytes.Contains(raw, []byte(token)) || bytes.Contains(raw, []byte(base64.StdEncoding.EncodeToString([]byte("forge:"+token)))) {
		t.Fatal("registry metadata leaked credentials")
	}

	var settings engine.SettingsView
	page.call(t, "files", nil, &settings)

	raw, err = json.Marshal(settings)
	if err != nil {
		t.Fatal(err)
	}

	if bytes.Contains(raw, []byte(token)) {
		t.Fatal("settings export leaked registry token")
	}

	credentials, err := filepath.Glob(filepath.Join(root, ".forge", "connections", "registries", "registry-it", "*", "config.json"))
	if err != nil || len(credentials) != 1 {
		t.Fatal("isolated registry credentials missing")
	}

	info, err := os.Stat(credentials[0])
	if err != nil {
		t.Fatal(err)
	}

	if info.Mode().Perm() != 0600 {
		t.Fatal("registry credentials are not private")
	}

	t.Log("real registry authentication, denial, isolated credential persistence and sanitized workbench metadata qualified")
}
