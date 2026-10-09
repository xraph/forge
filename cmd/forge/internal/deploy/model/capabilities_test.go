package model

import (
	"testing"

	"github.com/xraph/forge/cmd/forge/internal/deploy/spec"
)

func TestCapabilitiesSupports(t *testing.T) {
	c := Capabilities{Resources: map[ResourceType][]Lifecycle{
		Postgres: {spec.LifecycleContainer, spec.LifecycleExternal},
	}}
	if !c.Supports(Postgres, spec.LifecycleContainer) {
		t.Fatal("expected container postgres to be supported")
	}

	if c.Supports(Postgres, spec.LifecycleManaged) {
		t.Fatal("managed postgres must not be supported")
	}

	if c.Supports(Redis, spec.LifecycleContainer) {
		t.Fatal("unknown type must not be supported")
	}
}
