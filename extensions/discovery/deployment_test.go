package discovery

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/xraph/confy"
	"github.com/xraph/confy/sources"
	"github.com/xraph/forge"
)

func TestDiscoveryLoadsDeploymentYAMLBeforeRegistration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("extensions:\n  discovery:\n    enabled: false\n    backend: kubernetes\n    kubernetes:\n      namespace: deployment-team\n"), 0600); err != nil {
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

	for _, explicit := range []bool{false, true} {
		t.Run(map[bool]string{false: "loaded", true: "options"}[explicit], func(t *testing.T) {
			app := forge.New(forge.WithAppConfigManager(manager), forge.WithAppLogger(forge.NewNoopLogger()))

			var opts []ConfigOption
			if explicit {
				opts = []ConfigOption{WithKubernetes("explicit-team", false), WithBackend("memory"), WithEnabled(true)}
			}

			ext := NewExtension(opts...).(*Extension)
			if err := ext.Register(app); err != nil {
				t.Fatal(err)
			}

			if ext.config.Enabled != explicit {
				t.Fatal("loaded enabled setting or explicit option ignored")
			}

			wantBackend, wantNamespace := "kubernetes", "deployment-team"
			if explicit {
				wantBackend, wantNamespace = "memory", "explicit-team"
			}

			if ext.config.Backend != wantBackend || ext.config.Kubernetes.Namespace != wantNamespace {
				t.Fatalf("deployment config ignored: %+v", ext.config)
			}

			if _, err := forge.InjectType[*Service](app.Container()); !explicit && err == nil {
				t.Fatal("disabled extension registered a service")
			} else if explicit && err != nil {
				t.Fatal("enabled extension failed to register service", err)
			}
		})
	}
}

func TestDiscoveryAdvertisesDeploymentAddress(t *testing.T) {
	t.Setenv("POD_IP", "10.2.3.4")
	t.Setenv("FORGE_ADVERTISE_ADDR", "")

	ext := NewExtension(WithService(ServiceConfig{Port: 8080}), WithAppConfig(forge.AppConfig{HTTPAddress: ":8080"})).(*Extension)
	if got := ext.createServiceInstance().Address; got != "10.2.3.4" {
		t.Fatalf("advertised %s instead of Pod IP", got)
	}

	t.Setenv("FORGE_ADVERTISE_ADDR", "api.internal")

	if got := ext.createServiceInstance().Address; got != "api.internal" {
		t.Fatalf("advertisement override ignored: %s", got)
	}

	ext.config.Service.Address = "explicit.internal"
	if got := ext.createServiceInstance().Address; got != "explicit.internal" {
		t.Fatalf("explicit address lost: %s", got)
	}

	ext.config.Service.Address = "0.0.0.0"
	if got := ext.createServiceInstance().Address; got == "0.0.0.0" {
		t.Fatal("wildcard bind address advertised")
	}
}

func TestDiscoveryIPv6AdvertisementURL(t *testing.T) {
	ext := NewExtension(WithService(ServiceConfig{Name: "api", Address: "::1", Port: 8080}), WithFARPEnabled(true)).(*Extension)
	if got := ext.createServiceInstance().Metadata["farp.manifest"]; got != "http://[::1]:8080/_farp/manifest" {
		t.Fatalf("invalid IPv6 URL: %s", got)
	}
}

func TestDiscoveryRejectsMalformedDeploymentConfig(t *testing.T) {
	manager := confy.NewTestConfyImplWithData(map[string]any{"extensions": map[string]any{"discovery": map[string]any{"enabled": "invalid"}}})

	app := forge.New(forge.WithAppConfigManager(manager), forge.WithAppLogger(forge.NewNoopLogger()))
	if err := NewExtension().Register(app); err == nil {
		t.Fatal("malformed enabled setting ignored")
	}
}

func TestDiscoveryIPv6BindKeepsApplicationPort(t *testing.T) {
	t.Setenv("POD_IP", "fd00::123")
	t.Setenv("FORGE_ADVERTISE_ADDR", "")

	ext := NewExtension(WithAppConfig(forge.AppConfig{HTTPAddress: "[::]:9090"})).(*Extension)

	instance := ext.createServiceInstance()
	if instance.Port != 9090 || instance.Address != "fd00::123" {
		t.Fatalf("wrong IPv6 service endpoint: %s:%d", instance.Address, instance.Port)
	}
}

func expectedDeploymentHostname(t *testing.T) string {
	t.Helper()
	t.Setenv("POD_IP", "")
	t.Setenv("FORGE_ADVERTISE_ADDR", "")

	hostname, err := os.Hostname()
	if err != nil {
		t.Fatal(err)
	}

	return hostname
}
