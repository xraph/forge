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
