package engine

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/xraph/forge/cmd/forge/internal/deploy/execx"
	"github.com/xraph/forge/cmd/forge/internal/deploy/model"
	"github.com/xraph/forge/cmd/forge/internal/deploy/output"
	"github.com/xraph/forge/cmd/forge/internal/deploy/plan"
	"github.com/xraph/forge/cmd/forge/internal/deploy/spec"
	"github.com/xraph/forge/cmd/forge/internal/deploy/state"
	"github.com/xraph/forge/cmd/forge/internal/deploy/testdata"
)

func registryPlan(t *testing.T) (*Engine, *execx.Fake, *plan.Plan) {
	t.Helper()
	e, f := composeEngine(t, testdata.Copy(t, "atlas-v2"))
	ctx := context.Background()

	view, err := e.Files(ctx)
	if err != nil {
		t.Fatal(err)
	}

	digest := "sha256:" + strings.Repeat("a", 64)

	images := map[string]string{}
	for _, name := range []string{"api", "gateway", "worker"} {
		images[name] = "ghcr.io/acme/" + name + "@" + digest
	}

	if err := e.Save(ctx, view.Hash, []spec.Op{{Path: "deploy.targets.local.build", Value: spec.Build{Source: "existing", Delivery: "registry", Images: images}}}); err != nil {
		t.Fatal(err)
	}

	f.Script("docker buildx imagetools inspect", execx.Result{Stdout: `{"digest":"` + digest + `"}`})
	f.Script("docker pull", execx.Result{})
	f.Script("docker image inspect", execx.Result{Stdout: digest})

	p, _, err := e.Plan(ctx, "local", "dev")
	if err != nil {
		t.Fatal(err)
	}

	return e, f, p
}
func TestPublishPreservesWorkloadState(t *testing.T) {
	e, _, p := registryPlan(t)

	st, err := state.Open(e.Root(), "local", "dev")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	before, err := st.Snapshot()
	if err != nil {
		t.Fatal(err)
	}

	images, err := e.PublishImages(context.Background(), p, p.Hash, nil)
	if err != nil {
		t.Fatal(err)
	}

	if len(images) != 3 {
		t.Fatal(images)
	}

	after, err := st.Snapshot()
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("publication changed workload state", err)
	}

	for name, image := range images {
		if image.Digest != p.Deployment.Services[0].Image.Digest {
			t.Fatal("digest missing", image)
		}

		raw, err := st.ReadFile("image-" + name + "-" + p.Hash + ".json")
		if err != nil {
			t.Fatal(err)
		}

		var saved model.Image
		if json.Unmarshal(raw, &saved) != nil || saved.Digest != image.Digest {
			t.Fatal("pin not persisted")
		}
	}
}
func TestPublishRequiresFreshExactApproval(t *testing.T) {
	for _, name := range []string{"wrong", "missing", "changed-input", "changed-state", "local-delivery"} {
		t.Run(name, func(t *testing.T) {
			e, f, p := registryPlan(t)
			approved := p.Hash
			want := output.ExitConflict

			switch name {
			case "wrong":
				approved = "wrong"
			case "missing":
				approved = ""
				want = output.ExitInvalidInput
			case "changed-input":
				path := filepath.Join(e.Root(), ".forge.yml")

				raw, _ := os.ReadFile(path)
				if err := os.WriteFile(path, append(raw, '\n'), 0600); err != nil {
					t.Fatal(err)
				}
			case "changed-state":
				st, err := state.Open(e.Root(), "local", "dev")
				if err != nil {
					t.Fatal(err)
				}

				unlock, err := st.Lock(context.Background())
				if err != nil {
					t.Fatal(err)
				}

				snapshot, err := st.Snapshot()
				if err != nil {
					t.Fatal(err)
				}

				snapshot.Revision++
				if err := st.SaveSnapshot(snapshot); err != nil {
					t.Fatal(err)
				}

				unlock()
				st.Close()
			case "local-delivery":
				p.Target.Build.Delivery = "local"
				p.Deployment.Target.Build.Delivery = "local"
				p.Hash, _ = p.ComputeHash()
				approved = p.Hash
				want = output.ExitInvalidInput
			}

			before := len(f.Calls)
			_, err := e.PublishImages(context.Background(), p, approved, nil)

			var typed *output.Error
			if !errors.As(err, &typed) || typed.Code != want {
				t.Fatalf("want exit %d, got %v", want, err)
			}

			for _, call := range f.Calls[before:] {
				if call.Name == "docker" {
					t.Fatal("rejected publication invoked Docker", call)
				}
			}
		})
	}
}
