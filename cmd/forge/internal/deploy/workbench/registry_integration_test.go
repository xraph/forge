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
	"github.com/xraph/forge/cmd/forge/internal/deploy/spec"
	"github.com/xraph/forge/cmd/forge/internal/deploy/state"
	"github.com/xraph/forge/cmd/forge/internal/deploy/testdata"
	"golang.org/x/crypto/bcrypt"
)

func TestWorkbenchRegistryConnection(t *testing.T) {
	binary := os.Getenv("FORGE_DEPLOY_CLI")
	if binary == "" {
		t.Fatal("set FORGE_DEPLOY_CLI to the freshly built CLI")
	}

	root := testdata.Copy(t, "atlas-v2")

	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Minute)
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

	tidy := exec.CommandContext(ctx, "go", "mod", "tidy")
	tidy.Dir = root

	tidy.Env = append(os.Environ(), "GOWORK=off")
	if _, err := tidy.CombinedOutput(); err != nil {
		t.Fatal("publication fixture dependency resolution", err)
	}

	page.call(t, "files", nil, &settings)

	namespace := fmt.Sprintf("forge-publish-%d", time.Now().UnixNano())
	build := spec.Build{Source: "local", Builder: "host", Delivery: "registry", Registry: spec.Registry{Host: host, Namespace: namespace, Auth: "registry-it", Visibility: "private"}}
	page.call(t, "files", map[string]any{"expected": settings.Hash, "ops": []spec.Op{{Path: "deploy.targets.local.build", Value: build}, {Path: "deploy.environments.dev.services", Value: []string{"api"}}}}, &settings)
	p := livePlan(t, page, []string{"api"})
	localTag := p.Deployment.Services[0].Image.Repository + ":" + p.Deployment.Services[0].Image.Tag

	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 30*time.Second)
		defer stop()

		_ = exec.CommandContext(cleanup, "docker", "image", "rm", localTag).Run()
	})

	var started Run
	page.call(t, "publish", map[string]string{"hash": p.Hash, "approval": p.Hash}, &started)

	deadline := time.Now().Add(6 * time.Minute)

	var publication *Publication

	for time.Now().Before(deadline) {
		var runs []Run
		page.call(t, "runs", nil, &runs)

		for _, current := range runs {
			if current.ID != started.ID {
				continue
			}

			if current.Status == "failed" || current.Status == "cancelled" {
				t.Fatalf("registry publication failed: %v", current.Error)
			}

			if current.Status == "completed" {
				publication = current.Publication
			}
		}

		if publication != nil {
			break
		}

		time.Sleep(250 * time.Millisecond)
	}

	if publication == nil || publication.PlanHash != p.Hash || len(publication.Images) != 1 || !strings.HasPrefix(publication.Images["api"].Digest, "sha256:") {
		t.Fatal("publication omitted immutable image result")
	}

	var snapshot state.Snapshot
	page.call(t, "history?target=local&env=dev", nil, &snapshot)

	if snapshot.Status == state.StatusHealthy || len(snapshot.Releases) != 0 || len(snapshot.Workloads) != 0 {
		t.Fatal("image publication claimed a workload deployment")
	}

	cli := exec.CommandContext(ctx, binary, "deploy", "publish", "--plan", p.Hash, "--approve-plan", p.Hash, "--output", "json", "--non-interactive")
	cli.Dir = root

	cli.Env = append(os.Environ(), "GOWORK=off")

	output, err := cli.CombinedOutput()
	if err != nil {
		t.Fatalf("CLI publication replay failed: %v (%d output bytes)", err, len(output))
	}

	var envelope struct {
		OK   bool        `json:"ok"`
		Data Publication `json:"data"`
	}
	if json.Unmarshal(output, &envelope) != nil || !envelope.OK || envelope.Data.Images["api"].Digest != publication.Images["api"].Digest || bytes.Contains(output, []byte(token)) {
		t.Fatal("CLI omitted immutable result or leaked registry credentials")
	}

	t.Log("real registry authentication, denial, isolated credentials, workbench publication and CLI digest replay qualified")
}
