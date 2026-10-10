package engine

import (
	"context"
	"encoding/json"

	"github.com/xraph/forge/cmd/forge/internal/deploy/output"
	"github.com/xraph/forge/cmd/forge/internal/deploy/plan"
	"github.com/xraph/forge/cmd/forge/internal/deploy/state"
)

type LifecycleState struct {
	Hash         string         `json:"hash"`
	Snapshot     state.Snapshot `json:"snapshot"`
	RecordedPlan *plan.Plan     `json:"recorded_plan,omitempty"`
}

func (e *Engine) LifecycleState(ctx context.Context, target, env string) (LifecycleState, error) {
	_, st, target, env, err := e.metadata(ctx, target, env)
	if err != nil {
		return LifecycleState{}, err
	}
	defer st.Close()

	return e.lifecycleState(ctx, st, target, env)
}
func (e *Engine) lifecycleState(ctx context.Context, st *state.Store, target, env string) (LifecycleState, error) {
	view, err := e.Files(ctx)
	if err != nil {
		return LifecycleState{}, err
	}

	snap, err := st.Snapshot()
	if err != nil {
		return LifecycleState{}, err
	}

	journal, err := st.Journal().Events()
	if err != nil {
		return LifecycleState{}, err
	}

	raw, err := json.Marshal(struct {
		Settings string         `json:"settings"`
		Snapshot state.Snapshot `json:"snapshot"`
		Journal  any            `json:"journal"`
	}{view.Hash, snap, journal})
	if err != nil {
		return LifecycleState{}, err
	}

	result := LifecycleState{Hash: digest(raw), Snapshot: snap}

	hash := snap.ActivePlanHash
	if hash == "" && len(snap.Releases) > 0 {
		hash = snap.Releases[len(snap.Releases)-1].PlanHash
	}

	if hash != "" {
		result.RecordedPlan, err = plan.Recorded(e.cfg.RootDir, hash, target, env)
		if err != nil {
			return LifecycleState{}, err
		}
	}

	return result, nil
}
func (e *Engine) RollbackApproved(ctx context.Context, target, env, release, approval string) error {
	if approval == "" {
		return output.Fail(output.ExitConflict, "lifecycle approval is required")
	}

	return e.rollback(ctx, target, env, release, approval)
}
func (e *Engine) DestroyApproved(ctx context.Context, target, env string, deleteData bool, approval string) error {
	if approval == "" {
		return output.Fail(output.ExitConflict, "lifecycle approval is required")
	}

	return e.destroy(ctx, target, env, deleteData, approval)
}

func (e *Engine) verifyLifecycle(ctx context.Context, st *state.Store, target, env, expected string) error {
	current, err := e.lifecycleState(ctx, st, target, env)
	if err != nil {
		return err
	}

	if current.Hash != expected {
		return output.Fail(output.ExitConflict, "deployment changed after lifecycle review; refresh approval")
	}

	return nil
}
