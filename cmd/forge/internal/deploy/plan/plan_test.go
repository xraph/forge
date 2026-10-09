package plan

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/xraph/forge/cmd/forge/internal/deploy/model"
	"github.com/xraph/forge/cmd/forge/internal/deploy/render"
	"github.com/xraph/forge/cmd/forge/internal/deploy/state"
)

func sample() (*model.Deployment, *render.Bundle) {
	d := &model.Deployment{Project: "atlas", Environment: "dev", TargetName: "local",
		Services:  []model.Service{{Name: "api", Image: model.Image{Repository: "atlas/atlas-api", Tag: "dev"}}},
		Resources: []model.Resource{{Name: "primary", Type: model.Postgres, Lifecycle: "container", Secret: model.SecretRef{Name: "primary-generated", EnvVar: "ATLAS_PRIMARY_DSN", Resolver: "generated", Where: "generated at apply", Resolved: true}}}}
	b := render.New("local", "dev")
	b.Add("compose.yaml", []byte("services: {}\n"))

	return d, b
}

func TestHashIgnoresTimeAndSecretDetail(t *testing.T) {
	d, b := sample()
	ops := []Operation{{ID: "create:primary", Kind: OpCreate, Resource: "primary"}}
	p1, _ := Build(d, b, state.Snapshot{}, map[string]string{".forge.yml": "h1"}, ops)

	time.Sleep(2 * time.Millisecond)

	d2, b2 := sample()
	d2.Resources[0].Secret.Where = "somewhere else"

	p2, _ := Build(d2, b2, state.Snapshot{}, map[string]string{".forge.yml": "h1"}, ops)
	if p1.Hash != p2.Hash {
		t.Fatal("hash must not depend on time or secret location")
	}

	d3, b3 := sample()
	d3.Services[0].Image.Tag = "other"

	p3, _ := Build(d3, b3, state.Snapshot{}, map[string]string{".forge.yml": "h1"}, ops)
	if p3.Hash == p1.Hash {
		t.Fatal("hash must change with the model")
	}
}

func TestSaveLoadVerify(t *testing.T) {
	d, b := sample()
	p, _ := Build(d, b, state.Snapshot{}, map[string]string{".forge.yml": "h1"}, nil)
	dir := t.TempDir()

	path, err := Save(dir, p)
	if err != nil {
		t.Fatal(err)
	}

	if filepath.Base(path) != "dev-local-"+p.Hash[:12]+".json" {
		t.Fatal(path)
	}

	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", info.Mode())
	}

	back, err := Load(path)
	if err != nil || back.Hash != p.Hash {
		t.Fatalf("%v %v", back, err)
	}

	if diags := Verify(back, map[string]string{".forge.yml": "h1"}); len(diags) != 0 {
		t.Fatalf("%v", diags)
	}

	diags := Verify(back, map[string]string{".forge.yml": "changed"})
	if len(diags) != 1 || diags[0].Code != "DEPLOY_PLAN_STALE" || diags[0].File != ".forge.yml" {
		t.Fatalf("%v", diags)
	}

	back.Operations = append(back.Operations, Operation{ID: "x"})
	if diags := Verify(back, map[string]string{".forge.yml": "h1"}); len(diags) != 1 || diags[0].Code != "DEPLOY_PLAN_HASH_MISMATCH" {
		t.Fatalf("tampered plan: %v", diags)
	}
}

func TestSecretVariableIsApproved(t *testing.T) {
	d, b := sample()
	p, _ := Build(d, b, state.Snapshot{}, nil, nil)

	p.Deployment.Resources[0].Secret.EnvVar = "FORGED_ENV"
	if !Verify(p, nil).HasErrors() {
		t.Fatal("secret delivery changed without approval")
	}
}

func TestLoadRejectsChangedOperation(t *testing.T) {
	d, b := sample()
	p, _ := Build(d, b, state.Snapshot{}, nil, nil)

	path, err := Save(t.TempDir(), p)
	if err != nil {
		t.Fatal(err)
	}

	raw, _ := os.ReadFile(path)

	var edited Plan
	if err := json.Unmarshal(raw, &edited); err != nil {
		t.Fatal(err)
	}

	edited.Operations = append(edited.Operations, Operation{ID: "delete:primary", Kind: OpDelete, Destructive: true})

	raw, err = json.Marshal(edited)
	if err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}

	if _, err := Load(path); err == nil {
		t.Fatal("edited stored plan was accepted")
	}
}
