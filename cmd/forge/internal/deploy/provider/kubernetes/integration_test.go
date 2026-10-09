//go:build integration

package kubernetes_test

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
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestKubernetesStackLifecycle(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Fatal("Docker required for integration qualification")
	}

	root := testdata.Copy(t, "atlas-v2")
	project := fmt.Sprintf("forge-deploy-it-%d", time.Now().UnixNano())

	ctx, cancel := context.WithTimeout(context.Background(), 22*time.Minute)
	defer cancel()

	kubeconfig := filepath.Join(t.TempDir(), "kubeconfig")
	t.Setenv("KUBECONFIG", kubeconfig)

	cluster := project

	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()

		_, _ = execx.System().Run(cleanup, execx.Command{Name: "kind", Args: []string{"delete", "cluster", "--name", cluster}, Dir: root})
	})

	if result, err := execx.System().Run(ctx, execx.Command{Name: "kind", Args: []string{"create", "cluster", "--name", cluster, "--kubeconfig", kubeconfig, "--wait", "120s"}, Dir: root}); err != nil {
		t.Fatal("isolated kind cluster", result.Stderr)
	}

	port := 8090

	doc, ds, err := spec.Parse(filepath.Join(root, ".forge.yml"))
	if err != nil || ds.HasErrors() {
		t.Fatal(err, ds)
	}

	ops := []spec.Op{
		{Path: "deploy.targets.local.provider", Value: "kubernetes"},
		{Path: "deploy.targets.local.context", Value: "kind-" + cluster},
		{Path: "deploy.targets.local.local_cluster", Value: cluster},
		{Path: "deploy.targets.local.namespace", Value: "atlas-it"},
		{Path: "deploy.targets.local.network_policy", Value: false},
		{Path: "deploy.targets.local.build.builder", Value: "host"},
		{Path: "deploy.services.gateway.ports.http.port", Value: port},
	}

	files, err := doc.Patch(ops)
	if err != nil {
		t.Fatal(err)
	}

	if err := spec.Write(doc.Path, doc.Hash, files[doc.Path]); err != nil {
		t.Fatal(err)
	}

	{
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

	p, b, err := e.Plan(ctx, "local", "dev")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := e.Export(ctx, p, b, "", false); err != nil {
		t.Fatal(err)
	}

	kubectl := func(args ...string) string {
		t.Helper()

		args = append([]string{"--context", "kind-" + cluster, "--namespace", "atlas-it"}, args...)

		result, err := execx.System().Run(ctx, execx.Command{Name: "kubectl", Args: args, Dir: root})
		if err != nil {
			t.Fatalf("Kubernetes verification %v: %s", args, result.Stderr)
		}

		return result.Stdout
	}
	execPod := func(name string, args ...string) string {
		t.Helper()

		return kubectl(append([]string{"exec", name, "--"}, args...)...)
	}
	appExec := func(name string, args ...string) string { return execPod("deployment/"+name, args...) }
	backendExec := func(name string, args ...string) string { return execPod(name+"-0", args...) }

	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()

		for _, service := range p.Deployment.Services {
			_, _ = execx.System().Run(cleanup, execx.Command{Name: "docker", Args: []string{"image", "rm", service.Image.Repository + ":" + service.Image.Tag}, Dir: root})
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

	t.Log("Kubernetes", kubectl("version", "--client", "-o", "json"))
	t.Log("NetworkPolicy enforcement unverified: default kind CNI does not enforce policy")

	if got := backendExec("cache", "redis-cli", "JSON.SET", "forge:probe", "$", `{"value":7}`); !strings.Contains(got, "OK") {
		t.Fatal("Redis JSON unavailable")
	}

	if got := backendExec("cache", "redis-cli", "FT.CREATE", "forge:search", "ON", "JSON", "PREFIX", "1", "forge:", "SCHEMA", "$.value", "AS", "value", "NUMERIC"); !strings.Contains(got, "OK") {
		t.Fatal("Redis search unavailable")
	}

	backendExec("primary", "psql", "-U", "forge", "-d", "atlas", "-c", "CREATE TABLE forge_deploy_probe(value text); INSERT INTO forge_deploy_probe VALUES ('persisted-db');")

	if raw := appExec("gateway", "wget", "-qO-", fmt.Sprintf("http://127.0.0.1:%d/probe", port)); !strings.Contains(raw, "http://api.atlas-it.svc.cluster.local:8080") {
		t.Fatal("gateway service address did not resolve")
	}

	appExec("api", "wget", "-qO-", "http://127.0.0.1:8080/storage/probe")
	kubectl("rollout", "restart", "statefulset/primary", "statefulset/cache", "statefulset/uploads")

	for _, name := range []string{"primary", "cache", "uploads"} {
		kubectl("rollout", "status", "statefulset/"+name, "--timeout=180s")
	}

	kubectl("rollout", "restart", "deployment/api", "deployment/worker", "deployment/gateway")

	for _, name := range []string{"api", "worker", "gateway"} {
		kubectl("rollout", "status", "deployment/"+name, "--timeout=180s")
	}

	if raw := backendExec("primary", "psql", "-U", "forge", "-d", "atlas", "-Atc", "SELECT value FROM forge_deploy_probe"); !strings.Contains(raw, "persisted-db") {
		t.Fatal("database data lost")
	}

	if raw := backendExec("cache", "redis-cli", "JSON.GET", "forge:probe"); !strings.Contains(raw, "7") {
		t.Fatal("Redis data lost")
	}

	if raw := appExec("api", "wget", "-qO-", "http://127.0.0.1:8080/storage/read"); !strings.Contains(raw, "persisted-object") {
		t.Fatal("object data lost")
	}

	logs, err := e.Logs(ctx, "local", "dev", "api", provider.LogOptions{Tail: 10})
	if err != nil {
		t.Fatal(err)
	}

	rawLogs, readErr := io.ReadAll(logs)
	_ = logs.Close()

	if readErr != nil || len(rawLogs) == 0 {
		t.Fatal("deployment logs unavailable", readErr)
	}
	// A failed migration must remain visible even when the prior app is healthy.
	doc, _, err = spec.Parse(filepath.Join(root, ".forge.yml"))
	if err != nil {
		t.Fatal(err)
	}

	files, err = doc.Patch([]spec.Op{{Path: "deploy.services.api.migrate", Value: "sh -c false"}})
	if err != nil {
		t.Fatal(err)
	}

	if err := spec.Write(doc.Path, doc.Hash, files[doc.Path]); err != nil {
		t.Fatal(err)
	}

	failedPlan, _, err := e.PlanWithOptions(ctx, "local", "dev", engine.PlanOptions{Services: []string{"api"}})
	if err != nil {
		t.Fatal(err)
	}

	failedCtx, failedCancel := context.WithTimeout(ctx, 40*time.Second)
	defer failedCancel()

	if err := e.Apply(failedCtx, failedPlan, failedPlan.Hash, false, nil); err == nil {
		t.Fatal("failing migration succeeded")
	}

	failedStatus, err := e.Status(ctx, "local", "dev")
	if err != nil || (failedStatus.Overall != state.StatusPartial && failedStatus.Overall != state.StatusCancelled) || failedStatus.FailedOperation != "migrate:api" {
		t.Fatal("failed migration hidden by previous containers", err, failedStatus)
	}
	// Restore the migration, deploy API alone, and restart the untouched worker.
	doc, _, err = spec.Parse(filepath.Join(root, ".forge.yml"))
	if err != nil {
		t.Fatal(err)
	}

	files, err = doc.Patch([]spec.Op{{Path: "deploy.services.api.migrate", Value: "auto"}})
	if err != nil {
		t.Fatal(err)
	}

	if err := spec.Write(doc.Path, doc.Hash, files[doc.Path]); err != nil {
		t.Fatal(err)
	}

	partial, _, err := e.PlanWithOptions(ctx, "local", "dev", engine.PlanOptions{Services: []string{"api"}})
	if err != nil {
		t.Fatal(err)
	}

	if err := e.Apply(ctx, partial, partial.Hash, false, nil); err != nil {
		t.Fatal(err)
	}

	kubectl("rollout", "restart", "deployment/worker")
	kubectl("rollout", "status", "deployment/worker", "--timeout=120s")
	appExec("worker", "wget", "-qO-", "http://127.0.0.1:8080/_/health/ready")

	if err := e.Destroy(ctx, "local", "dev", false); err != nil {
		t.Fatal(err)
	}

	var claims struct {
		Items []json.RawMessage `json:"items"`
	}

	raw := kubectl("get", "pvc", "-o", "json")
	if json.Unmarshal([]byte(raw), &claims) != nil || len(claims.Items) < 3 {
		t.Fatal("persistent claims removed by default")
	}
}
