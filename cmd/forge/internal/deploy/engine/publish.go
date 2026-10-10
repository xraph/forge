package engine

import (
	"context"
	"errors"
	"reflect"

	"github.com/xraph/forge/cmd/forge/internal/deploy/images"
	"github.com/xraph/forge/cmd/forge/internal/deploy/model"
	"github.com/xraph/forge/cmd/forge/internal/deploy/output"
	"github.com/xraph/forge/cmd/forge/internal/deploy/persistence"
	"github.com/xraph/forge/cmd/forge/internal/deploy/plan"
	"github.com/xraph/forge/cmd/forge/internal/deploy/provider"
	"github.com/xraph/forge/cmd/forge/internal/deploy/secrets"
	"github.com/xraph/forge/cmd/forge/internal/deploy/state"
)

// PublishImages verifies or publishes immutable images without changing workloads.
func (e *Engine) PublishImages(ctx context.Context, p *plan.Plan, approved string, events chan<- provider.Event) (map[string]model.Image, error) {
	if p == nil || approved == "" {
		return nil, output.Fail(output.ExitInvalidInput, "publication needs a plan and its full approved hash")
	}

	if approved != p.Hash {
		return nil, output.Fail(output.ExitConflict, "approved hash does not match the plan")
	}

	if p.Target.Build.Delivery != "registry" || p.Target.Build.Source == "git" {
		return nil, output.Fail(output.ExitInvalidInput, "publication requires registry image delivery")
	}

	verify := func() (*InspectResult, error) {
		res, err := e.load(ctx)
		if err != nil {
			return nil, err
		}

		inputs, err := e.buildInputHashes(ctx, res)
		if err != nil {
			return nil, err
		}

		if ds := plan.Verify(p, inputs); ds.HasErrors() {
			return nil, output.Fail(output.ExitConflict, "publication plan is stale", ds...)
		}

		return res, nil
	}
	if _, err := verify(); err != nil {
		return nil, err
	}

	st, err := state.Open(e.Root(), p.TargetName, p.Environment)
	if err != nil {
		return nil, err
	}
	defer st.Close()

	unlock, err := st.Lock(ctx)
	if err != nil {
		return nil, output.Fail(output.ExitConflict, "deployment is locked")
	}
	defer unlock()

	ctx = st.Context(ctx)

	res, err := verify()
	if err != nil {
		return nil, err
	}

	snapshot, err := st.Snapshot()
	if err != nil {
		return nil, err
	}

	if !reflect.DeepEqual(snapshot, p.Snapshot) {
		return nil, output.Fail(output.ExitConflict, "deployment state changed since image review; create a new plan")
	}

	if res.Doc.Deploy == nil {
		return nil, output.Fail(output.ExitInvalidInput, "deployment configuration is missing")
	}

	values := map[string]string{}

	if name := p.Target.Build.Registry.SecretRef; name != "" {
		resolver, err := secrets.New(res.Doc.Deploy.Secrets, e.Root(), e.runner, p.Target, p.Project)
		if err != nil {
			return nil, err
		}

		status, value, err := secrets.Reference(ctx, res.Doc.Deploy.Secrets, e.Root(), resolver, name, true)
		if err != nil || !status.Resolved || value == "" {
			return nil, output.Fail(output.ExitAccess, "registry credential is unavailable")
		}

		values[name] = value
	}

	emit := func(status state.Status, message string) error {
		if events == nil {
			return nil
		}

		select {
		case events <- provider.Event{Op: "publish-images", Status: status, Message: message}:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if err := emit(state.StatusApplying, "Publishing reviewed images"); err != nil {
		return nil, err
	}

	result, err := images.Build(ctx, e.runner, e.Root(), p, st, values)
	if errors.Is(context.Cause(ctx), persistence.ErrLeaseLost) {
		return nil, output.Fail(output.ExitConflict, "publication authority was lost; review saved image pins")
	}

	if ctx.Err() != nil {
		return nil, output.Fail(output.ExitTimeout, "image publication timed out or was cancelled")
	}

	if err != nil {
		return nil, output.Fail(output.ExitApplyFailed, "image publication failed: "+err.Error())
	}

	if err := emit(state.StatusAccepted, "Immutable image digests verified; save them and create a new deployment plan"); err != nil {
		return nil, err
	}

	return result, nil
}
