package provider

import (
	"context"
	"errors"
	"github.com/xraph/forge/cmd/forge/internal/deploy/execx"
	"testing"
)

func TestRegistryConstructsExplicitFactories(t *testing.T) {
	r := NewRegistry(execx.NewFake(t), t.TempDir(), func(execx.Runner, string) Provider { return ExportOnly{ProviderName: "compose"} })
	if names := r.Names(); len(names) != 1 || names[0] != "compose" {
		t.Fatal(names)
	}

	if _, ok := r.Get("kubernetes"); ok {
		t.Fatal("unexpected adapter")
	}
}
func TestExportOnlyReturnsUnsupported(t *testing.T) {
	var p Provider = ExportOnly{ProviderName: "test"}
	if err := p.Apply(context.Background(), nil, nil, nil, nil); !errors.Is(err, ErrUnsupported) {
		t.Fatal(err)
	}
}

func TestRegistryRejectsDuplicate(t *testing.T) {
	r := NewRegistry(nil, "", func(execx.Runner, string) Provider { return ExportOnly{ProviderName: "compose"} })
	if e := r.Register(ExportOnly{ProviderName: "compose"}); e == nil {
		t.Fatal("duplicate accepted")
	}

	if e := r.Register(nil); e == nil {
		t.Fatal("nil provider accepted")
	}
}

func TestRegistryRejectsTypedNil(t *testing.T) {
	r := NewRegistry(nil, "")

	var p *ExportOnly
	if e := r.Register(p); e == nil {
		t.Fatal("typed nil accepted")
	}
}
