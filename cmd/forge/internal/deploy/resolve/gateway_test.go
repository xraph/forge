package resolve

import (
	"context"
	"github.com/xraph/forge/cmd/forge/internal/deploy/spec"
	"strings"
	"testing"
)

func gatewayInput(t *testing.T) Input {
	t.Helper()
	in := input(t, "atlas-v2", "local", "dev")
	g := in.Doc.Deploy.Services["gateway"]
	g.Kind = spec.KindGateway
	in.Doc.Deploy.Services["gateway"] = g
	a := in.Doc.Deploy.Services["api"]
	a.Discovery = true
	in.Doc.Deploy.Services["api"] = a

	return in
}
func TestGatewayDiscoveryConnection(t *testing.T) {
	in := gatewayInput(t)

	d, ds, e := Resolve(context.Background(), in)
	if e != nil || ds.HasErrors() {
		t.Fatal(e, ds)
	}

	found := false

	for _, c := range d.Connections {
		if c.From == "api" && c.ConfigKey == "extensions.discovery.farp.gateway_url" {
			found = c.To == "gateway" && c.EnvVar == "FARP_GATEWAY_URL" && c.Timeout == 0
		}
	}

	if !found {
		t.Fatal(d.Connections)
	}

	raw, e := Overlay(d, &d.Services[0])
	if e != nil || !strings.Contains(string(raw), "gateway_url: ${FARP_GATEWAY_URL}") {
		t.Fatal(string(raw), e)
	}
}
func TestGatewayExcludedRequiresEndpoint(t *testing.T) {
	in := gatewayInput(t)
	env := in.Doc.Deploy.Environments["dev"]
	env.Services = []string{"api"}
	in.Doc.Deploy.Environments["dev"] = env

	_, ds, _ := Resolve(t.Context(), in)
	if !ds.HasErrors() {
		t.Fatal("excluded gateway accepted")
	}

	env.ExternalServices = map[string]spec.ExternalService{"gateway": {URL: "https://gateway.example.com"}}
	in.Doc.Deploy.Environments["dev"] = env

	d, ds, _ := Resolve(t.Context(), in)
	if ds.HasErrors() {
		t.Fatal(ds)
	}

	if len(d.Connections) != 1 || d.Connections[0].Address != "https://gateway.example.com" {
		t.Fatal(d.Connections)
	}
}
func TestGatewaySelectionIsExplicit(t *testing.T) {
	in := gatewayInput(t)
	in.Doc.Deploy.Services["another"] = in.Doc.Deploy.Services["gateway"]

	_, ds, _ := Resolve(t.Context(), in)
	if !ds.HasErrors() {
		t.Fatal("ambiguous gateway accepted")
	}

	target := in.Doc.Deploy.Targets["local"]
	target.Gateway = "gateway"
	in.Doc.Deploy.Targets["local"] = target

	_, ds, _ = Resolve(t.Context(), in)
	if ds.HasErrors() {
		t.Fatal(ds)
	}
}

func TestGatewayOverlayEnablesFARP(t *testing.T) {
	in := gatewayInput(t)

	d, ds, _ := Resolve(t.Context(), in)
	if ds.HasErrors() {
		t.Fatal(ds)
	}

	raw, e := Overlay(d, &d.Services[0])
	if e != nil {
		t.Fatal(e)
	}

	if !strings.Contains(string(raw), "farp:\n      enabled: true") {
		t.Fatal("gateway URL did not enable FARP", string(raw))
	}
}
