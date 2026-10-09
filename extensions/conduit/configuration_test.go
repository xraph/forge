package conduit_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xraph/confy"
	"github.com/xraph/confy/sources"
	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/conduit"
	adapter "github.com/xraph/forge/extensions/conduit/discovery"
	"github.com/xraph/forge/extensions/discovery/backends"
)

func TestExtensionInfersManifestAndBindsBeforeRegistration(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("FORGE_ADVERTISE_ADDR", "billing.internal")

	if err := os.WriteFile(filepath.Join(dir, ".forge.yml"), []byte("app:\n  name: billing\n  version: 2.4.0\n  namespace: production\ndev:\n  port: 8093\n"), 0600); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(`extensions:
  conduit:
    providers:
      events: {type: memory}
    streams:
      orders: {provider: events, subjects: ["orders.>"]}
    subscriptions:
      process:
        stream: orders
        timeout: 2s
`), 0600); err != nil {
		t.Fatal(err)
	}

	manager := confy.New()

	source, err := sources.NewFileSource(path, sources.FileSourceOptions{})
	if err != nil {
		t.Fatal(err)
	}

	if err := manager.LoadFrom(source); err != nil {
		t.Fatal(err)
	}

	app := forge.New(forge.WithAppConfigManager(manager), forge.WithAppLogger(forge.NewNoopLogger()), forge.WithAppMetrics(forge.NewNoOpMetrics()))

	ext, err := conduit.NewExtension()
	if err != nil {
		t.Fatal(err)
	}

	var handled atomic.Int64

	if err := conduit.Subscribe(ext.Runtime(), placed, func(context.Context, conduit.Message[order]) error {
		handled.Add(1)

		return nil
	}, conduit.Consumer("process")); err != nil {
		t.Fatal(err)
	}

	if err := ext.Register(app); err != nil {
		t.Fatal(err)
	}

	identity := ext.Runtime().Identity()
	if identity.ServiceID != "billing" || identity.Namespace != "production" || identity.InstanceID == "" {
		t.Fatalf("identity inference failed: %+v", identity)
	}

	cfg := ext.Runtime().Configuration()
	if cfg.Version != "2.4.0" || len(cfg.Endpoints) != 1 || cfg.Endpoints[0].URL != "http://billing.internal:8093" || cfg.Subscriptions["process"].Timeout != 2*time.Second {
		t.Fatalf("manifest or file configuration ignored: %+v", cfg)
	}

	start(t, ext.Runtime())

	if _, err := conduit.Publish(t.Context(), ext.Runtime(), placed, order{ID: "inferred"}); err != nil {
		t.Fatal(err)
	}

	wait(t, func() bool { return handled.Load() == 1 })
}

func TestInferencePreservesExplicitFieldsAndEnvironment(t *testing.T) {
	t.Setenv("CONDUIT_NAMESPACE", "environment-team")
	t.Setenv("CONDUIT_SERVICE_ID", "environment-service")
	t.Setenv("CONDUIT_VERSION", "environment-version")

	ext, err := conduit.NewExtension(conduit.WithConfig(conduit.Config{Identity: conduit.Identity{ServiceID: "explicit-service"}, Version: "explicit-version", Endpoints: []conduit.Endpoint{{Protocol: "https", URL: "https://explicit.internal:8443"}}}))
	if err != nil {
		t.Fatal(err)
	}

	app := forge.New(forge.WithAppName("application"), forge.WithAppLogger(forge.NewNoopLogger()))
	if err := ext.Register(app); err != nil {
		t.Fatal(err)
	}

	cfg := ext.Runtime().Configuration()
	if cfg.Identity.Namespace != "environment-team" || cfg.Identity.ServiceID != "explicit-service" || cfg.Version != "explicit-version" || cfg.Endpoints[0].URL != "https://explicit.internal:8443" {
		t.Fatalf("explicit or environment config ignored: %+v", cfg)
	}
}

func TestForgeBridgeScopesReplicasAndDeparture(t *testing.T) {
	backend, err := backends.NewMemoryBackend()
	if err != nil {
		t.Fatal(err)
	}

	bridge := adapter.NewForge(func() (backends.Backend, error) { return backend, nil })

	for _, id := range []string{"first", "second"} {
		instance := conduit.Instance{Identity: conduit.Identity{Namespace: "production", ServiceID: "billing", InstanceID: id}, Ready: true, Endpoints: []conduit.Endpoint{{Protocol: "http", URL: "http://" + id + ".internal:8080"}}}
		if err := bridge.Register(t.Context(), instance); err != nil {
			t.Fatal(err)
		}
	}

	if err := backend.Register(t.Context(), &backends.ServiceInstance{ID: "unscoped", Name: "billing", Status: backends.HealthStatusPassing}); err != nil {
		t.Fatal(err)
	}

	members, err := bridge.Resolve(t.Context(), "production", "billing")
	if err != nil || len(members) != 2 {
		t.Fatalf("replica resolution failed: %+v %v", members, err)
	}

	if err := bridge.Deregister(t.Context(), members[0].Identity); err != nil {
		t.Fatal(err)
	}

	members, err = bridge.Resolve(t.Context(), "production", "billing")
	if err != nil || len(members) != 1 {
		t.Fatalf("departure affected another replica: %+v %v", members, err)
	}

	other, err := bridge.List(t.Context(), "other")
	if err != nil || len(other) != 0 {
		t.Fatal("bridge leaked an unscoped or other namespace member")
	}

	if strings.Contains(members[0].Endpoints[0].URL, "0.0.0.0") {
		t.Fatal("wildcard advertised")
	}
}

func TestPartialExplicitIdentityPreservesLoadedNamespace(t *testing.T) {
	manager := confy.New()
	manager.Set("extensions.conduit.identity.namespace", "file-team")

	ext, err := conduit.NewExtension(conduit.WithConfig(conduit.Config{Identity: conduit.Identity{ServiceID: "explicit"}}))
	if err != nil {
		t.Fatal(err)
	}

	app := forge.New(forge.WithAppConfigManager(manager), forge.WithAppLogger(forge.NewNoopLogger()))
	if err := ext.Register(app); err != nil {
		t.Fatal(err)
	}

	if got := ext.Runtime().Identity(); got.ServiceID != "explicit" || got.Namespace != "file-team" {
		t.Fatalf("partial identity lost loaded fields: %+v", got)
	}
}
