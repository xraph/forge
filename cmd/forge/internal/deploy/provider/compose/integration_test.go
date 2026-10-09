//go:build integration

package compose_test

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/xraph/forge/cmd/forge/config"
	"github.com/xraph/forge/cmd/forge/internal/deploy/engine"
	"github.com/xraph/forge/cmd/forge/internal/deploy/execx"
	"github.com/xraph/forge/cmd/forge/internal/deploy/output"
	"github.com/xraph/forge/cmd/forge/internal/deploy/provider"
	"github.com/xraph/forge/cmd/forge/internal/deploy/spec"
	"github.com/xraph/forge/cmd/forge/internal/deploy/state"
	"github.com/xraph/forge/cmd/forge/internal/deploy/testdata"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestComposeStackLifecycle(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Fatal("Docker required for integration qualification")
	}
	root := testdata.Copy(t, "atlas-v2")
	project := fmt.Sprintf("forge-deploy-it-%d", time.Now().UnixNano())
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	doc, ds, err := spec.Parse(filepath.Join(root, ".forge.yml"))
	if err != nil || ds.HasErrors() {
		t.Fatal(err, ds)
	}
	ops := []spec.Op{{Path: "deploy.targets.local.project", Value: project}, {Path: "deploy.services.gateway.ports.http.port", Value: port}}
	if builder := os.Getenv("FORGE_DEPLOY_TEST_BUILDER"); builder != "" {
		ops = append(ops, spec.Op{Path: "deploy.targets.local.build.builder", Value: builder})
	}
	files, err := doc.Patch(ops)
	if err != nil {
		t.Fatal(err)
	}
	if err := spec.Write(doc.Path, doc.Hash, files[doc.Path]); err != nil {
		t.Fatal(err)
	}
	if os.Getenv("FORGE_DEPLOY_TEST_BUILDER") == "host" {
		result, err := execx.System().Run(context.Background(), execx.Command{Name: "go", Args: []string{"mod", "tidy"}, Dir: root, Env: []string{"GOWORK=off"}})
		if err != nil {
			t.Fatalf("fixture dependencies: %s", result.Stderr)
		}
	}
	cfg, err := config.LoadForgeConfigFrom(root)
	if err != nil {
		t.Fatal(err)
	}
	e, err := engine.New(engine.Options{Config: cfg, Runner: execx.System(), Mode: output.Mode{NonInteractive: true}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 18*time.Minute)
	defer cancel()
	p, b, err := e.Plan(ctx, "local", "dev")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.Export(ctx, p, b, "", false); err != nil {
		t.Fatal(err)
	}
	bundle := filepath.Join(root, "deployments", "local", "dev", "compose.yaml")
	env := filepath.Join(root, ".forge", "state", "local", "dev", "generated.env")
	docker := func(args ...string) string {
		t.Helper()
		result, err := execx.System().Run(ctx, execx.Command{Name: "docker", Args: args, Dir: root})
		if err != nil {
			t.Fatalf("Docker verification %v failed", args)
		}
		return result.Stdout
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		_, _ = execx.System().Run(cleanup, execx.Command{Name: "docker", Args: []string{"compose", "-p", project, "-f", bundle, "--env-file", env, "down", "-v", "--remove-orphans"}, Dir: root})
		for _, service := range p.Deployment.Services {
			image := service.Image.Repository + ":" + service.Image.Tag
			_, _ = execx.System().Run(cleanup, execx.Command{Name: "docker", Args: []string{"image", "rm", image}, Dir: root})
		}
	})
	events := make(chan provider.Event, 128)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for ev := range events {
			t.Log(ev.Op, ev.Status, ev.Message)
		}
	}()
	err = e.Apply(ctx, p, p.Hash, false, events)
	close(events)
	<-done
	if err != nil {
		t.Fatal(err)
	}
	status, err := e.Status(ctx, "local", "dev")
	if err != nil || status.Overall != state.StatusHealthy {
		t.Fatal(err, status)
	}
	version := docker("version", "--format", "{{.Server.Version}}")
	compose := docker("compose", "version", "--short")
	t.Log("Docker", strings.TrimSpace(version), "Compose", strings.TrimSpace(compose))
	if got := docker("exec", project+"-cache-1", "redis-cli", "JSON.SET", "forge:probe", "$", `{"value":7}`); !strings.Contains(got, "OK") {
		t.Fatal("Redis JSON unavailable")
	}
	if got := docker("exec", project+"-cache-1", "redis-cli", "FT.CREATE", "forge:search", "ON", "JSON", "PREFIX", "1", "forge:", "SCHEMA", "$.value", "AS", "value", "NUMERIC"); !strings.Contains(got, "OK") {
		t.Fatal("Redis search unavailable")
	}
	docker("exec", project+"-primary-1", "psql", "-U", "forge", "-d", "atlas", "-c", "CREATE TABLE forge_deploy_probe(value text); INSERT INTO forge_deploy_probe VALUES ('persisted-db');")
	if raw := docker("exec", project+"-gateway-1", "wget", "-qO-", fmt.Sprintf("http://127.0.0.1:%d/probe", port)); !strings.Contains(raw, "http://api:8080") {
		t.Fatal("gateway service address did not resolve")
	}
	docker("exec", project+"-api-1", "wget", "-qO-", "http://127.0.0.1:8080/storage/probe")
	docker("compose", "-p", project, "-f", bundle, "--env-file", env, "restart", "primary", "cache", "uploads")
	docker("compose", "-p", project, "-f", bundle, "--env-file", env, "up", "-d", "--wait", "--no-build", "api", "worker", "gateway")
	if raw := docker("exec", project+"-primary-1", "psql", "-U", "forge", "-d", "atlas", "-Atc", "SELECT value FROM forge_deploy_probe"); !strings.Contains(raw, "persisted-db") {
		t.Fatal("database data lost")
	}
	if raw := docker("exec", project+"-cache-1", "redis-cli", "JSON.GET", "forge:probe"); !strings.Contains(raw, "7") {
		t.Fatal("Redis data lost")
	}
	if raw := docker("exec", project+"-api-1", "wget", "-qO-", "http://127.0.0.1:8080/storage/read"); !strings.Contains(raw, "persisted-object") {
		t.Fatal("object data lost")
	}
	logs, err := e.Logs(ctx, "local", "dev", "api", provider.LogOptions{Tail: 10})
	if err != nil {
		t.Fatal(err)
	}
	_ = logs.Close()
	if err := e.Destroy(ctx, "local", "dev", false); err != nil {
		t.Fatal(err)
	}
	var volume struct {
		Name string `json:"Name"`
	}
	raw := docker("volume", "inspect", project+"_primary-data")
	var volumes []json.RawMessage
	if json.Unmarshal([]byte(raw), &volumes) != nil || len(volumes) != 1 {
		t.Fatal("data volume missing")
	}
	_ = json.Unmarshal(volumes[0], &volume)
	if volume.Name == "" {
		t.Fatal("data removed by default")
	}
}
