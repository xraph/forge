package forge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"testing"

	"github.com/xraph/forge/internal/logger"
)

type infraTestExtension struct {
	*BaseExtension

	registered, started int
	requirements        []InfraRequirement
	registerErr         error
}

func (e *infraTestExtension) Register(App) error {
	e.registered++

	return e.registerErr
}
func (e *infraTestExtension) Start(context.Context) error {
	e.started++

	return nil
}
func (e *infraTestExtension) InfraRequirements() []InfraRequirement { return e.requirements }
func TestInfraReportRegistersWithoutStarting(t *testing.T) {
	e := &infraTestExtension{BaseExtension: NewBaseExtension("storage", "1.0.0", ""), requirements: []InfraRequirement{{Kind: "redis", Instance: "cache", ConfigKey: "extensions.storage.url"}}}
	cfg := DefaultAppConfig()
	cfg.Name = "api"
	cfg.Logger = logger.NewTestLogger()
	cfg.Extensions = []Extension{e}
	a := NewApp(cfg)

	var b bytes.Buffer
	if err := WriteInfraRequirements(a, &b); err != nil {
		t.Fatal(err)
	}

	if e.registered != 1 || e.started != 0 || a.(*app).started {
		t.Fatal("started during report", e)
	}

	var r InfraReport
	if err := json.Unmarshal(b.Bytes(), &r); err != nil {
		t.Fatal(err)
	}

	if r.Schema != "forge.infra/v1" || r.App != "api" || len(r.Requirements) != 1 || r.Requirements[0].Extension != "storage" {
		t.Fatal(r)
	}
}
func TestInfraReportErrorsWriteNothing(t *testing.T) {
	for _, tc := range []struct {
		name string
		req  InfraRequirement
		err  error
	}{
		{"registration", InfraRequirement{Kind: "redis", ConfigKey: "extensions.x.url"}, errors.New("credential-private")},
		{"metadata", InfraRequirement{Kind: "redis", ConfigKey: "https://credential-private"}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := &infraTestExtension{BaseExtension: NewBaseExtension("x", "1.0.0", ""), requirements: []InfraRequirement{tc.req}, registerErr: tc.err}
			cfg := DefaultAppConfig()
			cfg.Logger = logger.NewTestLogger()
			cfg.Extensions = []Extension{e}

			var b bytes.Buffer
			if err := WriteInfraRequirements(NewApp(cfg), &b); err == nil || b.Len() != 0 {
				t.Fatal("invalid report published", b.String(), err)
			}
		})
	}
}
func TestInfraReportDeterministic(t *testing.T) {
	makeApp := func(order []string) App {
		cfg := DefaultAppConfig()

		cfg.Logger = logger.NewTestLogger()
		for _, name := range order {
			cfg.Extensions = append(cfg.Extensions, &infraTestExtension{BaseExtension: NewBaseExtension(name, "1.0.0", ""), requirements: []InfraRequirement{{Kind: "redis", Instance: name, ConfigKey: "extensions." + name + ".url"}}})
		}

		return NewApp(cfg)
	}

	var a, b bytes.Buffer
	if err := WriteInfraRequirements(makeApp([]string{"b", "a"}), &a); err != nil {
		t.Fatal(err)
	}

	if err := WriteInfraRequirements(makeApp([]string{"a", "b"}), &b); err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(a.Bytes(), b.Bytes()) {
		t.Fatal("report depends on registration order")
	}
}

func TestInfraRunEnvironmentReturnsBeforeStart(t *testing.T) {
	if os.Getenv("FORGE_INFRA_TEST_CHILD") == "1" {
		cfg := DefaultAppConfig()
		cfg.Name = "api"
		cfg.Logger = logger.NewTestLogger()
		e := &infraTestExtension{BaseExtension: NewBaseExtension("cache", "1.0.0", "")}

		cfg.Extensions = []Extension{e}
		if err := NewApp(cfg).Run(); err != nil {
			panic(err)
		}

		if e.started != 0 {
			panic("extension started")
		}

		return
	}

	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	command := exec.CommandContext(t.Context(), binary, "-test.run=^TestInfraRunEnvironmentReturnsBeforeStart$")

	command.Env = append(os.Environ(), "FORGE_INFRA_TEST_CHILD=1", "FORGE_INTROSPECT=1")

	output, err := command.Output()
	if err != nil {
		t.Fatal(err)
	}

	first := bytes.SplitN(output, []byte("\n"), 2)[0]

	var report InfraReport
	if err := json.Unmarshal(first, &report); err != nil || report.Schema != InfraSchemaVersion {
		t.Fatal(string(output), err)
	}
}
