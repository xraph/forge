package workbench

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/xraph/forge/cmd/forge/internal/deploy/engine"
	"github.com/xraph/forge/cmd/forge/internal/deploy/output"
	"github.com/xraph/forge/cmd/forge/internal/deploy/provider"
)

type lifecycleRequest struct {
	Action      string `json:"action"`
	Target      string `json:"target"`
	Environment string `json:"env"`
	Release     string `json:"release,omitempty"`
	DeleteData  bool   `json:"delete_data,omitempty"`
}
type lifecycleProof struct {
	Request lifecycleRequest      `json:"request"`
	State   engine.LifecycleState `json:"state"`
	Expires time.Time             `json:"expires"`
	Hash    string                `json:"hash"`
}

func (s *Server) reviewLifecycle(w http.ResponseWriter, r *http.Request, ctx context.Context) {
	var body lifecycleRequest
	if !decode(w, r, &body) {
		return
	}

	if body.Action != "rollback" && body.Action != "destroy" {
		apiFailure(w, 400, "choose rollback or destroy", nil)

		return
	}

	if body.Target == "" || body.Environment == "" {
		apiFailure(w, 400, "target and environment are required", nil)

		return
	}

	if body.Action == "rollback" && (body.Release == "" || body.DeleteData) {
		apiFailure(w, 400, "choose a release for rollback", nil)

		return
	}

	if body.Action == "destroy" && body.Release != "" {
		apiFailure(w, 400, "destroy does not accept a release", nil)

		return
	}

	var proof lifecycleProof

	err := s.syncMutation(func() error {
		state, err := s.engine.LifecycleState(ctx, body.Target, body.Environment)
		if err != nil {
			return err
		}

		if state.RecordedPlan == nil {
			return errors.New("nothing recorded for this environment")
		}

		if body.Action == "rollback" {
			found := false

			for _, release := range state.Snapshot.Releases {
				if release.ID == body.Release {
					found = true
				}
			}

			if !found {
				return errors.New("release was not recorded for this environment")
			}
		}

		proof = lifecycleProof{Request: body, State: state, Expires: time.Now().UTC().Add(15 * time.Minute)}

		raw, err := json.Marshal(proof)
		if err != nil {
			return err
		}

		sum := sha256.Sum256(raw)
		proof.Hash = hex.EncodeToString(sum[:])

		s.operations.Lock()
		defer s.operations.Unlock()

		if len(s.proofs) >= 32 {
			for key := range s.proofs {
				delete(s.proofs, key)

				break
			}
		}

		s.proofs[proof.Hash] = proof

		return nil
	})
	if err != nil {
		failure(w, err)

		return
	}

	success(w, proof)
}
func (s *Server) applyLifecycle(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Hash     string `json:"hash"`
		Approval string `json:"approval"`
	}
	if !decode(w, r, &body) {
		return
	}

	if !s.mutations.TryLock() {
		failure(w, errBusy)

		return
	}

	defer s.mutations.Unlock()

	s.operations.Lock()
	proof, ok := s.proofs[body.Hash]
	busy := s.active != nil

	valid := ok && body.Approval == proof.Hash && !time.Now().After(proof.Expires)
	if valid && !busy {
		delete(s.proofs, body.Hash)
	}
	s.operations.Unlock()

	if busy {
		failure(w, errBusy)

		return
	}

	if !valid {
		failure(w, output.Fail(output.ExitConflict, "refresh and approve the exact lifecycle preview"))

		return
	}

	args := proof.Request

	run, err := s.beginRun(args.Action, args.Target, args.Environment, func(ctx context.Context, _ chan<- provider.Event) error {
		if args.Action == "rollback" {
			return s.engine.RollbackApproved(ctx, args.Target, args.Environment, args.Release, proof.State.Hash)
		}

		return s.engine.DestroyApproved(ctx, args.Target, args.Environment, args.DeleteData, proof.State.Hash)
	})
	if err != nil {
		failure(w, err)

		return
	}

	writeJSON(w, 202, reply{OK: true, Data: run})
}
