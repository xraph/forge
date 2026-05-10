package pilot

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/xraph/forge/extensions/dashboard/collector"
	"github.com/xraph/forge/extensions/dashboard/contract"
	"github.com/xraph/forge/extensions/dashboard/contract/dispatcher"
	"github.com/xraph/forge/extensions/dashboard/contributor"
)

func TestPilotRegister_RegistersAllIntents(t *testing.T) {
	d := dispatcher.New(dispatcher.NoopMetricsEmitter{})
	reg := contract.NewRegistry()
	wreg := contract.NewWardenRegistry()

	deps := Deps{
		ExtensionsRegistry: newRegistryWith(t, &contributor.Manifest{Name: "auth"}),
		Services:           &stubServices{},
		Metrics:            &stubMetrics{data: &collector.MetricsData{}},
		MetricsInterval:    time.Millisecond,
	}
	if err := Register(d, reg, wreg, deps); err != nil {
		t.Fatalf("Register: %v", err)
	}

	// Contract registry has the pilot manifest.
	if _, ok := reg.Contributor("core-contract"); !ok {
		t.Error("core-contract not in contract registry")
	}
	// Dispatcher has each intent.
	for _, intentName := range []string{"extensions.list", "services.list", "services.detail"} {
		req := contract.Request{Envelope: "v1", Kind: contract.KindQuery, Contributor: "core-contract", Intent: intentName, IntentVersion: 1, Payload: json.RawMessage(`{}`)}
		_, _, err := d.Dispatch(context.Background(), req, contract.Principal{})
		if err != nil && intentName != "services.detail" {
			t.Errorf("%s dispatch: %v", intentName, err)
		}
	}
}

func TestPilotRegister_DefaultsMetricsInterval(t *testing.T) {
	d := dispatcher.New(dispatcher.NoopMetricsEmitter{})
	reg := contract.NewRegistry()
	wreg := contract.NewWardenRegistry()
	deps := Deps{
		ExtensionsRegistry: newRegistryWith(t),
		Services:           &stubServices{},
		Metrics:            &stubMetrics{data: &collector.MetricsData{}},
		// MetricsInterval intentionally zero
	}
	if err := Register(d, reg, wreg, deps); err != nil {
		t.Fatalf("Register: %v", err)
	}
	// No assertion on the actual interval; verify Register didn't error and
	// the subscription is registered.
	intent := contract.Intent{Name: "metrics.summary", Kind: contract.IntentKindSubscription, Version: 1, Capability: contract.CapRead}
	if _, _, err := d.Subscribe(context.Background(), contract.Principal{}, "core-contract", intent, nil); err != nil {
		t.Errorf("metrics.summary not registered: %v", err)
	}
}
