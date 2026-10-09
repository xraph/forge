package kubernetes

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/xraph/forge/cmd/forge/internal/deploy/execx"
	"github.com/xraph/forge/cmd/forge/internal/deploy/plan"
	"github.com/xraph/forge/cmd/forge/internal/deploy/provider"
	"github.com/xraph/forge/cmd/forge/internal/deploy/state"
	"gopkg.in/yaml.v3"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

type kubeRunner struct {
	*execx.Fake

	objects  map[string]object
	fail     string
	applied  []string
	payloads []string
}

func (f *kubeRunner) Run(_ context.Context, c execx.Command) (execx.Result, error) {
	f.Calls = append(f.Calls, c)
	if f.fail != "" && strings.Contains(c.String(), f.fail) {
		return execx.Result{}, errors.New("simulated cluster denial")
	}

	if c.Name == "docker" {
		return execx.Result{Stdout: "sha256:" + strings.Repeat("a", 64)}, nil
	}

	if c.Name == "kind" {
		return execx.Result{Stdout: "test-control-plane"}, nil
	}

	if c.Name != "kubectl" {
		return execx.Result{}, errors.New("unexpected command")
	}

	if slices.Contains(c.Args, "get") {
		if slices.Contains(c.Args, "--raw") {
			return execx.Result{Stdout: "ok"}, nil
		}

		if slices.Contains(c.Args, "namespace") {
			return execx.Result{Stdout: `{"apiVersion":"v1","kind":"Namespace","metadata":{"name":"atlas-dev"}}`}, nil
		}

		if slices.Contains(c.Args, "secret") {
			return execx.Result{}, errors.New("missing pull secret")
		}

		items := []object{}
		for _, o := range f.objects {
			items = append(items, o)
		}

		raw, err := json.Marshal(object{"apiVersion": "v1", "kind": "List", "items": items})

		return execx.Result{Stdout: string(raw)}, err
	}

	if slices.Contains(c.Args, "apply") && c.Stdin != nil {
		raw, err := io.ReadAll(c.Stdin)
		if err != nil {
			return execx.Result{}, err
		}

		f.payloads = append(f.payloads, string(raw))

		var list struct {
			Items []object `yaml:"items"`
		}
		if err := yaml.Unmarshal(raw, &list); err != nil {
			return execx.Result{}, err
		}

		if slices.Contains(c.Args, "--dry-run=server") {
			return execx.Result{}, nil
		}

		for _, o := range list.Items {
			meta := o["metadata"].(map[string]any)
			key := o["kind"].(string) + "/" + meta["name"].(string)
			meta["uid"] = "uid-" + key
			meta["generation"] = 1
			status := object{"observedGeneration": 1, "readyReplicas": 1, "updatedReplicas": 1, "availableReplicas": 1, "currentRevision": "revision", "updateRevision": "revision", "conditions": []any{object{"type": "Complete", "status": "True"}}}
			o["status"] = status
			f.objects[key] = o
			f.applied = append(f.applied, key)
		}
	}

	return execx.Result{}, nil
}
func fixtureApply(t *testing.T) (*Kubernetes, *plan.Plan, *state.Store, *kubeRunner) {
	t.Helper()
	k, d := fixture(t)
	d.Services = d.Services[:1]
	d.Connections = nil
	d.Routes = nil
	d.Revision = 1
	d.Target.Context = "kind-test"
	d.Target.LocalCluster = "test"
	f := &kubeRunner{Fake: execx.NewFake(t), objects: map[string]object{}}
	k.runner = f

	b, err := k.Render(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}

	ops, err := k.Operations(context.Background(), d, b, state.Snapshot{})
	if err != nil {
		t.Fatal(err)
	}

	p, err := plan.Build(d, b, state.Snapshot{}, nil, ops)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := plan.Save(k.root+"/.forge/plans", p); err != nil {
		t.Fatal(err)
	}

	st, err := state.Open(k.root, d.TargetName, d.Environment)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = st.Close() })

	return k, p, st, f
}
func applyDeadline(t *testing.T) context.Context {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	t.Cleanup(cancel)

	return ctx
}
func TestApprovedKubernetesApplyWaitsForMigrationAndPinsImages(t *testing.T) {
	k, p, st, f := fixtureApply(t)
	if err := k.Apply(applyDeadline(t), p, st, nil, nil); err != nil {
		t.Fatal("approved deployment failed", err)
	}

	migrated := false

	for _, call := range f.Calls {
		if call.Name == "kubectl" {
			if !slices.Contains(call.Args, "kind-test") || !slices.Contains(call.Args, "atlas-dev") {
				t.Fatal("implicit context/namespace", call.String())
			}

			if strings.Contains(call.String(), "wait") && strings.Contains(call.String(), "api-migrate-r1") {
				migrated = true
			}
		}
	}

	if !migrated {
		t.Fatal("migration was not awaited")
	}

	found := false

	for _, key := range f.applied {
		if key == "Deployment/api" {
			found = true
		}
	}

	if !found {
		t.Fatal("application never applied")
	}

	if !strings.Contains(textObject(t, f.objects["Deployment/api"]), "forge.local/") {
		t.Fatal("image not pinned/delivered")
	}

	snap, err := st.Snapshot()
	if err != nil {
		t.Fatal(err)
	}

	if snap.Status != state.StatusHealthy || snap.Revision != 1 || len(snap.Releases) != 1 {
		t.Fatal("release not recorded", snap)
	}

	before := len(f.applied)

	if err := k.Apply(applyDeadline(t), p, st, nil, nil); err != nil {
		t.Fatal(err)
	}

	for _, key := range f.applied[before:] {
		if strings.Contains(key, "migrate") {
			t.Fatal("completed migration replayed")
		}
	}
}
func TestFailedKubernetesMigrationStopsApplicationAndPersistsFailure(t *testing.T) {
	k, p, st, f := fixtureApply(t)
	f.fail = "wait --for=condition=complete job/api-migrate"

	if err := k.Apply(applyDeadline(t), p, st, nil, nil); err == nil {
		t.Fatal("migration failure ignored")
	}

	for _, key := range f.applied {
		if key == "Deployment/api" {
			t.Fatal("workload changed after migration failure")
		}
	}

	snap, err := st.Snapshot()
	if err != nil {
		t.Fatal(err)
	}

	if snap.FailedOperation != "migrate:api" || snap.Status != state.StatusPartial {
		t.Fatal("failure hidden", snap)
	}
}
func TestKubernetesDryRunFailureBlocksBackendsAndApplications(t *testing.T) {
	k, p, st, f := fixtureApply(t)
	f.fail = "--dry-run=server"

	if err := k.Apply(applyDeadline(t), p, st, nil, nil); err == nil {
		t.Fatal("dry run denied but apply succeeded")
	}

	for _, key := range f.applied {
		if strings.HasPrefix(key, "StatefulSet/") || strings.HasPrefix(key, "Deployment/") {
			t.Fatal("invalid manifest changed workload")
		}
	}
}
func TestKubernetesMissingPullAccessFailsBeforeNamespace(t *testing.T) {
	k, p, st, f := fixtureApply(t)
	p.Deployment.Target.Build.Delivery = "registry"

	p.Deployment.Target.Build.Registry.Visibility = "private"
	if err := k.Apply(applyDeadline(t), p, st, nil, nil); err == nil {
		t.Fatal("missing pull credentials ignored")
	}

	if len(f.applied) > 0 {
		t.Fatal("cluster changed before pull access check")
	}
}
func TestKubernetesDestroyRetainsClaimsAndNeverDeletesNamespace(t *testing.T) {
	k, p, st, f := fixtureApply(t)
	if err := k.Apply(applyDeadline(t), p, st, nil, nil); err != nil {
		t.Fatal(err)
	}

	f.Calls = nil

	if err := k.Destroy(applyDeadline(t), p, st, provider.DestroyOptions{}); err != nil {
		t.Fatal(err)
	}

	deleted := false

	for _, call := range f.Calls {
		if slices.Contains(call.Args, "delete") {
			deleted = true

			if strings.Contains(call.String(), "/namespaces/atlas-dev") && !strings.Contains(call.String(), "/namespaces/atlas-dev/") {
				t.Fatal("namespace deleted")
			}

			raw, err := io.ReadAll(call.Stdin)
			if err != nil {
				t.Fatal(err)
			}

			if !strings.Contains(string(raw), "preconditions") {
				t.Fatal("delete lacks identity precondition")
			}

			if strings.Contains(call.String(), "persistentvolumeclaims") {
				t.Fatal("data removed")
			}
		}
	}

	if !deleted {
		t.Fatal("no workloads removed")
	}
}
func TestKubernetesRollbackRejectsInterveningMigration(t *testing.T) {
	k, p, st, f := fixtureApply(t)
	if err := st.RecordRelease(state.Release{ID: "before", PlanHash: p.Hash, AppliedAt: time.Now().Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}

	if err := st.RecordRelease(state.Release{ID: "after", PlanHash: strings.Repeat("c", 64), Migrations: map[string]bool{"migrate:api": false}, AppliedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}

	if err := k.Rollback(applyDeadline(t), provider.EnvRef{}, st, "before"); err == nil || !strings.Contains(err.Error(), "migration") {
		t.Fatal("unsafe rollback", err)
	}

	if len(f.Calls) > 0 {
		t.Fatal("cluster changed before compatibility check")
	}
}
func TestKubernetesDeselectedServiceCannotBeLogged(t *testing.T) {
	k, p, _, f := fixtureApply(t)
	_ = p

	if _, err := k.Logs(applyDeadline(t), provider.ServiceRef{Target: "local", Env: "dev", Service: "gateway"}, provider.LogOptions{}); err == nil {
		t.Fatal("unrecorded service logs exposed")
	}

	if len(f.Calls) > 0 {
		t.Fatal("kubectl ran before selection validation")
	}
}

func TestKubernetesOwnershipLossBlocksDestroyBeforeAnyDeletion(t *testing.T) {
	k, p, st, f := fixtureApply(t)
	if err := k.Apply(applyDeadline(t), p, st, nil, nil); err != nil {
		t.Fatal(err)
	}

	f.objects["Deployment/api"]["metadata"].(map[string]any)["labels"] = map[string]any{"app.kubernetes.io/managed-by": "someone-else"}
	f.Calls = nil

	if err := k.Destroy(applyDeadline(t), p, st, provider.DestroyOptions{}); err == nil {
		t.Fatal("lost ownership silently removed state and backends")
	}

	for _, call := range f.Calls {
		if slices.Contains(call.Args, "delete") {
			t.Fatal("backend removed before ownership check")
		}
	}
}
func TestKubernetesForeignNameCollisionStopsBeforeBackendChanges(t *testing.T) {
	k, p, st, f := fixtureApply(t)
	f.objects["Deployment/api"] = object{"apiVersion": "apps/v1", "kind": "Deployment", "metadata": object{"name": "api", "namespace": "atlas-dev", "uid": "foreign", "labels": object{}}}

	if err := k.Apply(applyDeadline(t), p, st, nil, nil); err == nil {
		t.Fatal("foreign workload overwritten")
	}

	for _, key := range f.applied {
		if strings.HasPrefix(key, "Deployment/") || strings.HasPrefix(key, "StatefulSet/") {
			t.Fatal("workload changed before ownership check")
		}
	}
}

func TestKubernetesDestroyRemovesUnusedBackendsAndRetainsTheirClaims(t *testing.T) {
	k, p, st, f := fixtureApply(t)
	if err := k.Apply(applyDeadline(t), p, st, nil, nil); err != nil {
		t.Fatal(err)
	}

	f.objects["PersistentVolumeClaim/data-primary-0"] = makeObject(p.Deployment, "v1", "PersistentVolumeClaim", "data-primary-0", nil)
	meta := f.objects["PersistentVolumeClaim/data-primary-0"]["metadata"].(map[string]any)
	meta["labels"] = labels(p.Deployment, "primary", "resource")
	meta["uid"] = "claim-uid"

	snap, err := st.Snapshot()
	if err != nil {
		t.Fatal(err)
	}

	snap.Identities, err = k.SnapshotIDs(applyDeadline(t), p.Deployment)
	if err != nil {
		t.Fatal(err)
	}

	if err := st.SaveSnapshot(snap); err != nil {
		t.Fatal(err)
	}

	f.Calls = nil

	if err := k.Destroy(applyDeadline(t), p, st, provider.DestroyOptions{}); err != nil {
		t.Fatal(err)
	}

	removed := false

	for _, call := range f.Calls {
		if slices.Contains(call.Args, "delete") {
			if strings.Contains(call.String(), "/statefulsets/primary") {
				removed = true
			}

			if strings.Contains(call.String(), "persistentvolumeclaims") {
				t.Fatal("claim removed by default")
			}
		}
	}

	if !removed {
		t.Fatal("unused backend remains running")
	}
}

func TestKubernetesFailedRolloutPreservesOperation(t *testing.T) {
	k, p, st, f := fixtureApply(t)
	f.fail = "rollout status deployment/api"

	if err := k.Apply(applyDeadline(t), p, st, nil, nil); err == nil {
		t.Fatal("rollout denial ignored")
	}

	snap, err := st.Snapshot()
	if err != nil {
		t.Fatal(err)
	}

	if snap.FailedOperation != "rollout:api" || snap.Status != state.StatusPartial {
		t.Fatal("rollout failure hidden", snap)
	}
}
func TestKubernetesRollbackUsesFrozenImageAfterSourceChanges(t *testing.T) {
	k, p, st, f := fixtureApply(t)
	p.Deployment.Services[0].Migrate = nil

	b, err := k.Render(context.Background(), p.Deployment)
	if err != nil {
		t.Fatal(err)
	}

	ops, err := k.Operations(context.Background(), p.Deployment, b, state.Snapshot{})
	if err != nil {
		t.Fatal(err)
	}

	p, err = plan.Build(p.Deployment, b, state.Snapshot{}, nil, ops)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := plan.Save(filepath.Join(k.root, ".forge/plans"), p); err != nil {
		t.Fatal(err)
	}

	if err := k.Apply(applyDeadline(t), p, st, nil, nil); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(k.root, "changed.go"), []byte("package changed"), 0600); err != nil {
		t.Fatal(err)
	}

	f.Calls = nil

	if err := k.Rollback(applyDeadline(t), provider.EnvRef{}, st, p.Hash[:12]); err != nil {
		t.Fatal(err)
	}

	for _, c := range f.Calls {
		if c.Name == "docker" && slices.Contains(c.Args, "build") {
			t.Fatal("rollback rebuilt changed source")
		}
	}
}
func TestKubernetesDestroyWaitsForDeletion(t *testing.T) {
	k, p, st, f := fixtureApply(t)
	if err := k.Apply(applyDeadline(t), p, st, nil, nil); err != nil {
		t.Fatal(err)
	}

	f.Calls = nil
	f.fail = "wait --for=delete"

	if err := k.Destroy(applyDeadline(t), p, st, provider.DestroyOptions{}); err == nil {
		t.Fatal("asynchronous deletion reported removed")
	}

	snap, err := st.Snapshot()
	if err != nil {
		t.Fatal(err)
	}

	if snap.Status != state.StatusPartial || snap.FailedOperation == "" {
		t.Fatal("cleanup failure hidden")
	}
}
