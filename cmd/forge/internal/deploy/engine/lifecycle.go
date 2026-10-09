package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/xraph/forge/cmd/forge/internal/deploy/images"
	"github.com/xraph/forge/cmd/forge/internal/deploy/output"
	"github.com/xraph/forge/cmd/forge/internal/deploy/plan"
	"github.com/xraph/forge/cmd/forge/internal/deploy/provider"
	"github.com/xraph/forge/cmd/forge/internal/deploy/render"
	"github.com/xraph/forge/cmd/forge/internal/deploy/secrets"
	"github.com/xraph/forge/cmd/forge/internal/deploy/spec"
	"github.com/xraph/forge/cmd/forge/internal/deploy/state"
	"io"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"time"
)

type PlanOptions struct{ Services []string }

func (e *Engine) plansDir() string { return filepath.Join(e.cfg.RootDir, ".forge", "plans") }
func (e *Engine) Plan(ctx context.Context, target, env string) (*plan.Plan, *render.Bundle, error) {
	return e.PlanWithOptions(ctx, target, env, PlanOptions{})
}
func (e *Engine) PlanWithOptions(ctx context.Context, target, env string, opts PlanOptions) (*plan.Plan, *render.Bundle, error) {
	res, err := e.load(ctx)
	if err != nil {
		return nil, nil, err
	}

	res.Selection = opts.Services
	e.resolveInto(ctx, res, target, env, true)

	if res.Deployment == nil || res.Diagnostics.HasErrors() {
		return nil, nil, diagnosticError("cannot plan deployment", res.Diagnostics)
	}

	adapter, ok := e.registry.Get(res.Deployment.Target.Provider)
	if !ok {
		return nil, nil, output.Unsupported("forge deploy plan for "+res.Deployment.Target.Provider, "")
	}

	if ds := adapter.Validate(ctx, res.Deployment); ds.HasErrors() {
		return nil, nil, output.Fail(output.ExitInvalidInput, "provider validation failed", ds...)
	}

	inputs, err := e.buildInputHashes(ctx, res)
	if err != nil {
		return nil, nil, err
	}

	raw, err := json.Marshal(inputs)
	if err != nil {
		return nil, nil, err
	}

	sum := sha256.Sum256(raw)
	tag := "forge-" + hex.EncodeToString(sum[:])[:20]

	for i := range res.Deployment.Services {
		if res.Deployment.Services[i].Image.Digest == "" {
			res.Deployment.Services[i].Image.Tag = tag
		}
	}

	bundle, err := adapter.Render(ctx, res.Deployment)
	if err != nil {
		return nil, nil, err
	}

	st, err := state.Open(e.cfg.RootDir, res.Target, res.Environment)
	if err != nil {
		return nil, nil, err
	}
	defer st.Close()

	snap, err := st.Snapshot()
	if err != nil {
		return nil, nil, err
	}

	if _, err := st.Journal().Events(); err != nil {
		return nil, nil, err
	}

	ops, err := adapter.Operations(ctx, res.Deployment, bundle, snap)
	if err != nil {
		return nil, nil, err
	}

	p, err := plan.Build(res.Deployment, bundle, snap, inputs, ops)
	if err != nil {
		return nil, nil, err
	}

	if observer, ok := adapter.(provider.IdentityObserver); ok && (snap.ActivePlanHash != "" || len(snap.Releases) > 0) {
		p.ObservedIDs, err = observer.SnapshotIDs(ctx, res.Deployment)
		if err != nil {
			return nil, nil, err
		}
	}

	p.Diagnostics = res.Diagnostics.Sorted()

	p.Hash, err = p.ComputeHash()
	if err != nil {
		return nil, nil, err
	}

	if _, err := plan.Save(e.plansDir(), p); err != nil {
		return nil, nil, err
	}

	return p, bundle, nil
}
func (e *Engine) Export(ctx context.Context, p *plan.Plan, b *render.Bundle, dir string, force bool) (render.WriteResult, error) {
	if p == nil {
		return render.WriteResult{}, output.Fail(output.ExitInvalidInput, "a plan is required")
	}

	if ds := plan.Verify(p, p.Inputs); ds.HasErrors() {
		return render.WriteResult{}, output.Fail(output.ExitConflict, "invalid plan", ds...)
	}

	adapter, ok := e.registry.Get(p.Target.Provider)
	if !ok {
		return render.WriteResult{}, output.Unsupported("export for "+p.Target.Provider, "")
	}

	if b == nil {
		var err error

		b, err = adapter.Render(ctx, p.Deployment)
		if err != nil {
			return render.WriteResult{}, err
		}
	}

	if !reflect.DeepEqual(b.Hashes(), p.Files) {
		return render.WriteResult{}, output.Fail(output.ExitConflict, "rendered files differ from the approved plan")
	}

	defaultDir := filepath.Join(e.cfg.RootDir, "deployments", p.TargetName, p.Environment)
	if dir == "" {
		dir = defaultDir
	}

	if filepath.Clean(dir) != defaultDir {
		if relocator, ok := adapter.(provider.BundleRelocator); ok {
			var err error

			b, err = relocator.Relocate(p.Deployment, b, dir)
			if err != nil {
				return render.WriteResult{}, err
			}
		}
	}

	st, err := state.Open(e.cfg.RootDir, p.TargetName, p.Environment)
	if err != nil {
		return render.WriteResult{}, err
	}
	defer st.Close()

	snap, err := st.Snapshot()
	if err != nil {
		return render.WriteResult{}, err
	}

	preserve := snap.ActivePlanHash != "" || len(snap.Releases) > 0 || len(snap.Workloads) > 0

	return render.Write(dir, b, render.WriteOptions{Force: force, PreserveMissing: preserve})
}
func (e *Engine) Apply(ctx context.Context, p *plan.Plan, approve string, allowDestructive bool, ev chan<- provider.Event) error {
	if p == nil {
		return output.Fail(output.ExitInvalidInput, "a plan is required")
	}

	if approve == "" {
		return output.Fail(output.ExitInvalidInput, "apply needs approval of the plan hash")
	}

	if approve != p.Hash {
		return output.Fail(output.ExitConflict, "approved hash does not match the plan")
	}

	res, err := e.load(ctx)
	if err != nil {
		return err
	}

	inputs, err := e.buildInputHashes(ctx, res)
	if err != nil {
		return err
	}

	if ds := plan.Verify(p, inputs); ds.HasErrors() {
		return output.Fail(output.ExitConflict, "plan is stale", ds...)
	}

	for _, op := range p.Operations {
		if op.Destructive && !allowDestructive {
			return output.Fail(output.ExitInvalidInput, "destructive operation requires --allow-destructive")
		}
	}

	adapter, ok := e.registry.Get(p.Target.Provider)
	if !ok {
		return output.Unsupported("apply for "+p.Target.Provider, "")
	}

	st, err := state.Open(e.cfg.RootDir, p.TargetName, p.Environment)
	if err != nil {
		return err
	}
	defer st.Close()

	unlock, err := st.Lock(ctx)
	if err != nil {
		return output.Fail(output.ExitConflict, "deployment is locked", output.Diagnostic{Code: output.CodeLocked, Severity: output.SeverityError, Message: err.Error()})
	}
	defer unlock()

	if _, err := st.Journal().Events(); err != nil {
		return output.Fail(output.ExitConflict, "deployment journal is invalid")
	}

	snap, err := st.Snapshot()
	if err != nil {
		return err
	}

	resume := snap.ActivePlanHash == p.Hash && snap.Revision == p.Snapshot.Revision+1 && (snap.Status == state.StatusApplying || snap.Status == state.StatusPartial || snap.Status == state.StatusCancelled)
	if _, began := st.Journal().Completed(p.Hash + ":apply:begin"); !began {
		resume = false
	}

	if !resume && !reflect.DeepEqual(snap, p.Snapshot) {
		return output.Fail(output.ExitConflict, "deployment state changed since approval; create a new plan")
	}

	expected := p.ObservedIDs
	if resume {
		expected = snap.Identities
	}

	if expected != nil {
		observer, ok := adapter.(provider.IdentityObserver)
		if !ok {
			return output.Fail(output.ExitConflict, "provider cannot verify recorded identities")
		}

		observed, err := observer.SnapshotIDs(ctx, p.Deployment)
		if err != nil || !reflect.DeepEqual(expected, observed) {
			return output.Fail(output.ExitConflict, "remote workload identities changed since approval")
		}
	}

	if _, err := plan.Save(e.plansDir(), p); err != nil {
		return err
	}

	if result, err := e.Export(ctx, p, nil, "", false); err != nil {
		return err
	} else if len(result.Skipped) > 0 {
		return output.Fail(output.ExitConflict, "generated files have local edits; review an export before apply")
	}

	if res.Doc.Deploy == nil {
		return output.Fail(output.ExitInvalidInput, "deployment configuration is missing")
	}

	sec, err := secrets.New(res.Doc.Deploy.Secrets, e.cfg.RootDir, e.runner, p.Target, e.cfg.Project.Name)
	if err != nil {
		return err
	}

	if kr, ok := sec.(*secrets.KubernetesResolver); ok {
		kr.Environment = p.Environment
	}

	values := map[string]string{}

	if resolver, ok := sec.(interface {
		ValuesForApply(ctx context.Context) (map[string]string, error)
	}); ok {
		all, err := resolver.ValuesForApply(ctx)
		if err != nil {
			return output.Fail(output.ExitAccess, "secret resolver is unavailable")
		}

		for _, r := range p.Deployment.Resources {
			if r.Lifecycle == "external" {
				if v := all[r.Secret.EnvVar]; v != "" {
					values[r.Secret.EnvVar] = v
				}
			}
		}
	}

	var cancel context.CancelFunc
	if _, ok := ctx.Deadline(); !ok {
		ctx, cancel = context.WithTimeout(ctx, 10*time.Minute)
	} else {
		ctx, cancel = context.WithCancel(ctx)
	}
	defer cancel()

	if err := adapter.Apply(ctx, p, st, values, ev); err != nil {
		if ctx.Err() != nil {
			return output.Fail(output.ExitTimeout, "deployment timed out or was cancelled")
		}

		if errors.Is(err, provider.ErrUnsupported) {
			return output.Unsupported("apply for "+p.Target.Provider, "")
		}

		return output.Fail(output.ExitApplyFailed, err.Error())
	}

	return nil
}
func diagnosticError(message string, ds output.Diagnostics) error {
	code := output.ExitInvalidInput

	for _, d := range ds.Errors() {
		switch d.Code {
		case output.CodeDecisionOpen, output.CodeVersionMissing:
			code = output.ExitUnresolved
		case output.CodeUnsupportedCommand:
			code = output.ExitUnsupported
		}
	}

	return output.Fail(code, message, ds...)
}
func (e *Engine) metadata(ctx context.Context, target, env string) (provider.Provider, *state.Store, string, string, error) {
	res, err := e.load(ctx)
	if err != nil {
		return nil, nil, "", "", err
	}

	if res.Doc.Deploy == nil || res.Doc.IsV1 || res.Diagnostics.HasErrors() {
		return nil, nil, "", "", diagnosticError("invalid deployment configuration", res.Diagnostics)
	}

	sp := res.Doc.Deploy
	if env == "" {
		env = sp.Defaults.Environment
	}

	if target == "" {
		target = sp.Environments[env].Target
	}

	t, ok := sp.Targets[target]
	if !ok {
		return nil, nil, "", "", output.Fail(output.ExitInvalidInput, "target does not exist")
	}

	if _, ok := sp.Environments[env]; !ok {
		return nil, nil, "", "", output.Fail(output.ExitInvalidInput, "environment does not exist")
	}

	adapter, ok := e.registry.Get(t.Provider)
	if !ok {
		return nil, nil, "", "", output.Unsupported("lifecycle for "+t.Provider, "")
	}

	st, err := state.Open(e.cfg.RootDir, target, env)

	return adapter, st, target, env, err
}
func (e *Engine) Status(ctx context.Context, target, env string) (provider.Status, error) {
	adapter, st, target, env, err := e.metadata(ctx, target, env)
	if err != nil {
		return provider.Status{Overall: state.StatusUnknown}, err
	}
	defer st.Close()

	return adapter.Observe(ctx, provider.EnvRef{Target: target, Env: env}, st)
}
func (e *Engine) Logs(ctx context.Context, target, env, service string, opts provider.LogOptions) (io.ReadCloser, error) {
	adapter, st, target, env, err := e.metadata(ctx, target, env)
	if err != nil {
		return nil, err
	}
	defer st.Close()

	return adapter.Logs(ctx, provider.ServiceRef{Target: target, Env: env, Service: service}, opts)
}
func (e *Engine) Rollback(ctx context.Context, target, env, release string) error {
	adapter, st, target, env, err := e.metadata(ctx, target, env)
	if err != nil {
		return err
	}
	defer st.Close()

	unlock, err := st.Lock(ctx)
	if err != nil {
		return output.Fail(output.ExitConflict, err.Error())
	}
	defer unlock()

	if _, err := st.Journal().Events(); err != nil {
		return err
	}

	snap, err := st.Snapshot()
	if err != nil {
		return err
	}

	if release == "" {
		index := len(snap.Releases) - 1
		for i, r := range snap.Releases {
			if r.PlanHash == snap.ActivePlanHash {
				index = i - 1

				break
			}
		}

		if index < 0 {
			return output.Fail(output.ExitInvalidInput, "no previous release")
		}

		release = snap.Releases[index].ID
	}

	return adapter.Rollback(ctx, provider.EnvRef{Target: target, Env: env}, st, release)
}
func (e *Engine) Destroy(ctx context.Context, target, env string, deleteData bool) error {
	adapter, st, _, _, err := e.metadata(ctx, target, env)
	if err != nil {
		return err
	}
	defer st.Close()

	unlock, err := st.Lock(ctx)
	if err != nil {
		return output.Fail(output.ExitConflict, err.Error())
	}
	defer unlock()

	snap, err := st.Snapshot()
	if err != nil {
		return err
	}

	if _, err := st.Journal().Events(); err != nil {
		return err
	}

	hash := snap.ActivePlanHash
	if hash == "" && len(snap.Releases) > 0 {
		hash = snap.Releases[len(snap.Releases)-1].PlanHash
	}

	if hash == "" {
		return output.Fail(output.ExitInvalidInput, "nothing recorded for this environment")
	}

	matches, err := filepath.Glob(filepath.Join(e.plansDir(), "*-"+hash[:min(12, len(hash))]+".json"))
	if err != nil {
		return err
	}

	for _, path := range matches {
		p, err := plan.Load(path)
		if err == nil && p.Hash == hash {
			return adapter.Destroy(ctx, p, st, provider.DestroyOptions{DeleteData: deleteData})
		}
	}

	return output.Fail(output.ExitConflict, "recorded deployment plan is missing")
}

type ProviderInfo struct {
	Name  string `json:"name"`
	Level string `json:"level"`
}

func (e *Engine) Providers(ctx context.Context) []ProviderInfo {
	var result []ProviderInfo

	for _, name := range e.registry.Names() {
		adapter, _ := e.registry.Get(name)

		caps, err := adapter.Capabilities(ctx, spec.Target{Provider: name})
		if err == nil {
			result = append(result, ProviderInfo{Name: name, Level: string(caps.Level)})
		}
	}

	return result
}
func (e *Engine) buildInputHashes(ctx context.Context, res *InspectResult) (map[string]string, error) {
	hashes := map[string]string{}

	for path, hash := range res.InputHashes {
		relative, err := filepath.Rel(e.cfg.RootDir, path)
		if err != nil || !filepath.IsLocal(relative) {
			return nil, errors.New("source outside project")
		}

		hashes[filepath.ToSlash(relative)] = hash
	}

	root, err := os.OpenRoot(e.cfg.RootDir)
	if err != nil {
		return nil, err
	}
	defer root.Close()

	err = filepath.WalkDir(e.cfg.RootDir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if err := ctx.Err(); err != nil {
			return err
		}

		relative, err := filepath.Rel(e.cfg.RootDir, path)
		if err != nil {
			return err
		}

		if entry.IsDir() {
			if !images.IncludedSource(relative, true) {
				return filepath.SkipDir
			}

			return nil
		}

		if !images.IncludedSource(relative, false) {
			return nil
		}

		if entry.Type()&os.ModeSymlink != 0 {
			target, err := root.Readlink(relative)
			if err != nil {
				return err
			}

			hashes[filepath.ToSlash(relative)] = digest([]byte(target))

			return nil
		}

		raw, err := root.ReadFile(relative)
		if err != nil {
			return err
		}

		key := filepath.ToSlash(relative)
		if wanted, ok := hashes[key]; ok && wanted != digest(raw) {
			return output.Fail(output.ExitConflict, "source changed during discovery", output.Diagnostic{Code: output.CodePlanStale, Severity: output.SeverityError, File: relative, Message: "review the changed source"})
		}

		info, err := entry.Info()
		if err != nil {
			return err
		}

		hashes[key] = images.SourceDigest(raw, info.Mode())

		return nil
	})
	if err != nil {
		return nil, err
	}

	return maps.Clone(hashes), nil
}

func (e *Engine) PlanPath(p *plan.Plan) string {
	return filepath.Join(e.plansDir(), p.Environment+"-"+p.TargetName+"-"+p.Hash[:min(12, len(p.Hash))]+".json")
}
