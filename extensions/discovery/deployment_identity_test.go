package discovery

import (
	"os"
	"strings"
	"testing"

	"github.com/xraph/forge"
)

func TestDeploymentLogicalIdentityUsesContainerInstance(t *testing.T) {
	t.Setenv("FORGE_SERVICE_ID", "billing-api")
	t.Setenv("FORGE_INSTANCE_ID", "")

	app := forge.New(forge.WithAppName("compiled-app"), forge.WithAppLogger(forge.NewNoopLogger()))

	ext := NewExtension(WithBackend("memory"), WithServiceID("shared-configured-id")).(*Extension)
	if err := ext.Register(app); err != nil {
		t.Fatal(err)
	}

	host, err := os.Hostname()
	if err != nil {
		t.Fatal(err)
	}

	got := ext.createServiceInstance()
	if got.Name != "billing-api" || got.ID != "billing-api-"+host {
		t.Fatalf("container identity ignored: %#v", got)
	}
}

func TestDeploymentIdentityOverridesConfiguredIdentity(t *testing.T) {
	t.Setenv("FORGE_SERVICE_ID", "billing-api")
	t.Setenv("FORGE_INSTANCE_ID", "pod-uid-one")

	app := forge.New(forge.WithAppName("compiled-app"), forge.WithAppLogger(forge.NewNoopLogger()))

	ext := NewExtension(WithBackend("memory"), WithServiceName("configured-name"), WithServiceID("shared-configured-id")).(*Extension)
	if err := ext.Register(app); err != nil {
		t.Fatal(err)
	}

	one := ext.createServiceInstance()

	t.Setenv("FORGE_INSTANCE_ID", "pod-uid-two")

	two := ext.createServiceInstance()
	if one.Name != "billing-api" || two.Name != "billing-api" || one.ID != "pod-uid-one" || two.ID != "pod-uid-two" {
		t.Fatalf("deployment identity ignored: %#v %#v", one, two)
	}
}

func TestIdentityWithoutDeploymentVariablesRetainsFallback(t *testing.T) {
	t.Setenv("FORGE_SERVICE_ID", "")
	t.Setenv("FORGE_INSTANCE_ID", "")

	for _, configured := range []bool{false, true} {
		app := forge.New(forge.WithAppName("compiled-app"), forge.WithAppLogger(forge.NewNoopLogger()))

		ext := NewExtension(WithBackend("memory")).(*Extension)
		if configured {
			ext = NewExtension(WithBackend("memory"), WithServiceName("configured-name"), WithServiceID("configured-id")).(*Extension)
		}

		if err := ext.Register(app); err != nil {
			t.Fatal(err)
		}

		got := ext.createServiceInstance()
		if configured && (got.Name != "configured-name" || got.ID != "configured-id") {
			t.Fatal(got)
		}

		if !configured && (got.Name != "compiled-app" || !strings.HasPrefix(got.ID, "compiled-app-")) {
			t.Fatal(got)
		}
	}
}
