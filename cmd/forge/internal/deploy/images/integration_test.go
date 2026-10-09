//go:build integration

package images

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xraph/forge/cmd/forge/internal/deploy/execx"
	"golang.org/x/crypto/bcrypt"
)

func TestPrivateRegistryImageDelivery(t *testing.T) {
	root, p, st := imageFixture(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	runner := execx.System()

	run := func(args ...string) string {
		result, err := runner.Run(ctx, execx.Command{Name: "docker", Args: args, Dir: root})
		if err != nil {
			t.Fatalf("Docker integration command failed: %v", err)
		}

		return strings.TrimSpace(result.Stdout)
	}
	if operatingSystem := run("info", "--format", "{{.OperatingSystem}}"); strings.Contains(operatingSystem, "Docker Desktop") {
		t.Skip("private loopback registry qualification runs on native Linux Docker; the Desktop daemon cannot reach this host-only endpoint")
	}

	token := "forge-integration-private-token"

	hash, err := bcrypt.GenerateFromPassword([]byte(token), bcrypt.DefaultCost)
	if err != nil {
		t.Fatal(err)
	}

	put(t, root, ".forge/registry-auth/htpasswd", "forge:"+string(hash)+"\n")

	name := fmt.Sprintf("forge-registry-it-%d", time.Now().UnixNano())

	binding := "0.0.0.0::5000"
	if port := os.Getenv("FORGE_DEPLOY_TEST_REGISTRY_PORT"); port != "" {
		binding = "0.0.0.0:" + port + ":5000"
	}

	run("create", "--name", name, "--user", "0", "-p", binding, "-e", "REGISTRY_AUTH=htpasswd", "-e", "REGISTRY_AUTH_HTPASSWD_REALM=Forge", "-e", "REGISTRY_AUTH_HTPASSWD_PATH=/auth/htpasswd", "registry:3")
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 30*time.Second)
		defer stop()

		_, _ = runner.Run(cleanup, execx.Command{Name: "docker", Args: []string{"rm", "-f", "-v", name}})
	})

	run("cp", filepath.Join(root, ".forge", "registry-auth"), name+":/auth")
	run("start", name)

	if running := run("inspect", "--format", "{{.State.Running}}", name); running != "true" {
		t.Fatal("registry stopped during startup", run("logs", name))
	}

	port := run("inspect", "--format", `{{(index (index .NetworkSettings.Ports "5000/tcp") 0).HostPort}}`, name)
	host := "127.0.0.1:" + port
	client := &http.Client{Timeout: 2 * time.Second}
	ready := false

	for ctx.Err() == nil {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+host+"/v2/", nil)
		if err != nil {
			t.Fatal(err)
		}

		response, err := client.Do(request)
		if err == nil {
			_ = response.Body.Close()
			if response.StatusCode == http.StatusUnauthorized {
				ready = true

				break
			}
		}

		if run("inspect", "--format", "{{.State.Running}}", name) != "true" {
			t.Fatal("registry stopped", run("logs", name))
		}

		select {
		case <-ctx.Done():
		case <-time.After(100 * time.Millisecond):
		}
	}

	if !ready {
		t.Fatal("authenticated registry did not start")
	}

	if err := Connect(ctx, runner, root, "registry-it", host, "forge", "wrong-token"); err == nil {
		t.Fatal("registry denial ignored")
	}

	loginRunner := fakeRunner(t, func(command execx.Command) (execx.Result, error) {
		result, err := runner.Run(ctx, command)
		if err != nil {
			detail := err.Error()
			for _, secret := range []string{token, "wrong-token", base64.StdEncoding.EncodeToString([]byte("forge:" + token))} {
				detail = strings.ReplaceAll(detail, secret, "[redacted]")
			}

			t.Log("registry command diagnostic", detail)
		}

		return result, err
	})
	if err := Connect(ctx, loginRunner, root, "registry-it", host, "forge", token); err != nil {
		t.Fatal(err)
	}

	put(t, root, "custom.Dockerfile", "FROM scratch\nCOPY cmd/api/main.go /source\n")

	p.Deployment.Services[0].Image.Repository = host + "/forge-api"

	p.Deployment.Services[0].Image.Dockerfile = "custom.Dockerfile"
	if os.Getenv("DOCKER_HOST") == "" {
		p.Deployment.Target.DockerContext = run("context", "show")
	}

	p.Deployment.Target.Build.Delivery = "registry"
	p.Deployment.Target.Build.Registry.Host = host
	p.Deployment.Target.Build.Registry.Auth = "registry-it"
	refs := []string{Ref(p.Deployment.Services[0].Image)}

	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 30*time.Second)
		defer stop()

		_, _ = runner.Run(cleanup, execx.Command{Name: "docker", Args: append([]string{"image", "rm"}, refs...)})
	})

	result, err := Build(ctx, loginRunner, root, p, st, nil)
	if err != nil {
		t.Fatal(err)
	}

	image := result["api"]
	if !immutableDigest.MatchString(image.Digest) {
		t.Fatal("published image has no digest")
	}

	refs = append(refs, Ref(image))

	config, err := ConfigDir(root, p.Deployment)
	if err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(filepath.Join(config, "config.json"))
	if err != nil {
		t.Fatal(err)
	}

	var auth map[string]any
	if err := json.Unmarshal(raw, &auth); err != nil {
		t.Fatal(err)
	}

	events, err := st.ReadFile("journal.jsonl")
	if err != nil || strings.Contains(string(events), token) {
		t.Fatal("credential entered journal", err)
	}

	t.Log("authenticated private registry, denied login, source build, publication, digest verification and target pull passed")
}
