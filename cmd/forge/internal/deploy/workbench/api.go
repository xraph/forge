package workbench

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/xraph/forge/cmd/forge/internal/deploy/engine"
	"github.com/xraph/forge/cmd/forge/internal/deploy/output"
	"github.com/xraph/forge/cmd/forge/internal/deploy/persistence"
	"github.com/xraph/forge/cmd/forge/internal/deploy/provider"
	"github.com/xraph/forge/cmd/forge/internal/deploy/render"
	"github.com/xraph/forge/cmd/forge/internal/deploy/spec"
)

var errBusy = errors.New("another deployment operation is running")

type APIError struct {
	Message     string             `json:"message"`
	Code        int                `json:"code"`
	Diagnostics output.Diagnostics `json:"diagnostics,omitempty"`
}
type reply struct {
	OK    bool      `json:"ok"`
	Data  any       `json:"data,omitempty"`
	Error *APIError `json:"error,omitempty"`
}

func errorData(err error) *APIError {
	data := &APIError{Message: err.Error(), Code: output.ExitInvalidInput}

	var typed *output.Error
	if errors.As(err, &typed) {
		data.Code = typed.Code
		data.Diagnostics = typed.Diagnostics
	}

	if errors.Is(err, persistence.ErrConflict) || errors.Is(err, persistence.ErrLocked) || errors.Is(err, persistence.ErrLeaseLost) || errors.Is(err, errBusy) {
		data.Code = output.ExitConflict
	}

	if errors.Is(err, provider.ErrUnsupported) {
		data.Code = output.ExitUnsupported
	}

	if errors.Is(err, persistence.ErrUnavailable) {
		data.Code = output.ExitAccess
	}

	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		data.Code = output.ExitTimeout
	}

	return data
}
func apiFailure(w http.ResponseWriter, status int, message string, err error) {
	data := &APIError{Message: message, Code: output.ExitInvalidInput}
	if err != nil {
		data = errorData(err)
	}

	writeJSON(w, status, reply{Error: data})
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	raw, err := json.Marshal(value)
	if err != nil {
		http.Error(w, "response encoding failed", http.StatusInternalServerError)

		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(raw)
}
func success(w http.ResponseWriter, data any) { writeJSON(w, 200, reply{OK: true, Data: data}) }
func failure(w http.ResponseWriter, err error) {
	status := 400

	data := errorData(err)
	switch data.Code {
	case output.ExitConflict:
		status = 409
	case output.ExitAccess:
		status = 503
	case output.ExitTimeout:
		status = 408
	case output.ExitUnsupported:
		status = 422
	case output.ExitApplyFailed:
		status = 500
	}

	writeJSON(w, status, reply{Error: data})
}
func decode(w http.ResponseWriter, r *http.Request, value any) bool {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		apiFailure(w, 400, "send application/json", nil)

		return false
	}

	body := http.MaxBytesReader(w, r.Body, 1<<20)
	defer body.Close()

	decoder := json.NewDecoder(body)
	decoder.DisallowUnknownFields()

	err := decoder.Decode(value)
	if err == nil {
		if extra := decoder.Decode(new(any)); !errors.Is(extra, io.EOF) {
			err = errors.New("multiple JSON values")
		}
	}

	if err != nil {
		var limit *http.MaxBytesError
		if errors.As(err, &limit) {
			apiFailure(w, 413, "request body exceeds one MiB", nil)
		} else {
			apiFailure(w, 400, "invalid or unknown request fields", nil)
		}

		return false
	}

	return true
}

type planRequest struct {
	Target      string   `json:"target"`
	Environment string   `json:"env"`
	Services    []string `json:"services,omitempty"`
}

func (s *Server) api(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		apiFailure(w, 405, "method rejected", nil)

		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), s.timeout)
	defer cancel()

	switch r.Method + " " + r.URL.Path {
	case "GET /api/project":
		res, err := s.engine.Inspect(ctx, "", "")
		if err != nil {
			failure(w, err)

			return
		}

		view, err := s.engine.Files(ctx)
		if err != nil {
			failure(w, err)

			return
		}

		success(w, map[string]any{"name": res.Project, "settings": publicSettings(view), "apps": res.Discovery.Apps, "suggestions": res.Discovery.Suggestions, "diagnostics": res.Diagnostics, "providers": s.engine.Providers(ctx)})
	case "GET /api/files":
		view, err := s.engine.Files(ctx)
		if err != nil {
			failure(w, err)

			return
		}

		success(w, publicSettings(view))
	case "POST /api/files":
		var body struct {
			Expected string    `json:"expected"`
			Ops      []spec.Op `json:"ops"`
		}
		if !decode(w, r, &body) {
			return
		}

		if err := s.syncMutation(func() error { return s.engine.Save(ctx, body.Expected, body.Ops) }); err != nil {
			failure(w, err)

			return
		}

		view, err := s.engine.Files(ctx)
		if err != nil {
			failure(w, err)

			return
		}

		success(w, publicSettings(view))
	case "POST /api/init":
		var body struct {
			Answers map[string]string `json:"answers"`
			Force   bool              `json:"force"`
		}
		if !decode(w, r, &body) {
			return
		}

		var (
			res *engine.InspectResult
			err error
		)

		err = s.syncMutation(func() error {
			res, _, err = s.engine.InitWithOptions(ctx, body.Answers, engine.InitOptions{Write: true, Force: body.Force})

			return err
		})
		if err != nil {
			failure(w, err)

			return
		}

		success(w, map[string]any{"diagnostics": res.Diagnostics})
	case "POST /api/store":
		var body struct {
			Expected  string `json:"expected"`
			Backend   string `json:"backend"`
			Reference string `json:"reference"`
		}
		if !decode(w, r, &body) {
			return
		}

		if err := s.syncMutation(func() error {
			return s.engine.ConfigureStore(ctx, persistence.Options{Backend: body.Backend, Reference: body.Reference}, body.Expected)
		}); err != nil {
			failure(w, err)

			return
		}

		view, err := s.engine.Files(ctx)
		if err != nil {
			failure(w, err)

			return
		}

		success(w, publicSettings(view))
	case "POST /api/doctor":
		var body struct {
			Target      string `json:"target"`
			Environment string `json:"env"`
			Offline     bool   `json:"offline"`
		}
		if !decode(w, r, &body) {
			return
		}

		diagnostics, err := s.engine.Doctor(ctx, body.Target, body.Environment, !body.Offline)
		if err != nil {
			failure(w, err)

			return
		}

		success(w, diagnostics)
	case "POST /api/plan":
		var body planRequest
		if !decode(w, r, &body) {
			return
		}

		var data any

		if err := s.syncMutation(func() error {
			p, b, err := s.engine.PlanWithOptions(ctx, body.Target, body.Environment, engine.PlanOptions{Services: body.Services})
			if err == nil {
				data = map[string]any{"plan": p, "artifacts": artifactContents(b)}
			}

			return err
		}); err != nil {
			failure(w, err)

			return
		}

		success(w, data)
	case "POST /api/export":
		var body struct {
			Hash  string `json:"hash"`
			Force bool   `json:"force"`
		}
		if !decode(w, r, &body) {
			return
		}

		p, err := s.engine.LoadPlan(ctx, body.Hash)
		if err != nil {
			failure(w, err)

			return
		}

		var data any

		if err := s.syncMutation(func() error {
			result, err := s.engine.Export(ctx, p, nil, "", body.Force)
			data = result

			return err
		}); err != nil {
			failure(w, err)

			return
		}

		success(w, data)
	case "POST /api/publish":
		var body struct {
			Hash     string `json:"hash"`
			Approval string `json:"approval"`
		}
		if !decode(w, r, &body) {
			return
		}

		if body.Hash == "" || body.Approval != body.Hash {
			apiFailure(w, 409, "approve the exact plan hash", nil)

			return
		}

		p, err := s.engine.LoadPlan(ctx, body.Hash)
		if err != nil {
			failure(w, err)

			return
		}

		if !s.mutations.TryLock() {
			failure(w, errBusy)

			return
		}
		run, err := s.beginResultRun("publish", p.TargetName, p.Environment, func(ctx context.Context, events chan<- provider.Event) (*Publication, error) {
			images, err := s.engine.PublishImages(ctx, p, body.Approval, events)
			if err != nil {
				return nil, err
			}

			return &Publication{PlanHash: p.Hash, Images: images}, nil
		})
		s.mutations.Unlock()

		if err != nil {
			failure(w, err)

			return
		}

		writeJSON(w, 202, reply{OK: true, Data: run})
	case "POST /api/apply":
		var body struct {
			Hash             string `json:"hash"`
			Approval         string `json:"approval"`
			AllowDestructive bool   `json:"allow_destructive"`
		}
		if !decode(w, r, &body) {
			return
		}

		if body.Hash == "" || body.Approval != body.Hash {
			apiFailure(w, 409, "approve the exact plan hash", nil)

			return
		}

		p, err := s.engine.LoadPlan(ctx, body.Hash)
		if err != nil {
			failure(w, err)

			return
		}

		run, err := s.startRun("apply", p.TargetName, p.Environment, func(ctx context.Context, events chan<- provider.Event) error {
			return s.engine.Apply(ctx, p, body.Approval, body.AllowDestructive, events)
		})
		if err != nil {
			failure(w, err)

			return
		}

		writeJSON(w, 202, reply{OK: true, Data: run})
	case "POST /api/lifecycle/plan":
		s.reviewLifecycle(w, r, ctx)
	case "POST /api/lifecycle/apply":
		s.applyLifecycle(w, r)
	case "GET /api/status":
		status, err := s.engine.Status(ctx, r.URL.Query().Get("target"), r.URL.Query().Get("env"))
		if err != nil {
			failure(w, err)

			return
		}

		success(w, status)
	case "GET /api/history":
		history, err := s.engine.History(ctx, r.URL.Query().Get("target"), r.URL.Query().Get("env"))
		if err != nil {
			failure(w, err)

			return
		}

		success(w, history)
	case "GET /api/logs":
		s.logs(w, r, ctx)
	case "GET /api/events":
		s.streamEvents(w, r)
	case "GET /api/runs":
		success(w, s.currentRuns())
	case "POST /api/cancel":
		var body struct {
			Run string `json:"run"`
		}
		if !decode(w, r, &body) {
			return
		}

		if err := s.cancelRun(body.Run); err != nil {
			failure(w, err)

			return
		}

		success(w, nil)
	case "GET /api/connections":
		data, err := s.engine.Connections(ctx)
		if err != nil {
			failure(w, err)

			return
		}

		success(w, data)
	case "POST /api/connections/registry":
		var body struct {
			Name     string `json:"name"`
			Host     string `json:"host"`
			Username string `json:"username"`
			Token    string `json:"token"`
		}
		if !decode(w, r, &body) {
			return
		}

		if err := s.syncMutation(func() error { return s.engine.ConnectRegistry(ctx, body.Name, body.Host, body.Username, body.Token) }); err != nil {
			failure(w, err)

			return
		}

		success(w, map[string]any{"name": body.Name, "host": body.Host, "username": body.Username, "connected": true})
	default:
		apiFailure(w, 404, "deployment API route not found", nil)
	}
}

func artifactContents(bundle *render.Bundle) map[string]string {
	result := map[string]string{}

	if bundle != nil {
		for name, file := range bundle.Files {
			result[name] = string(file.Content)
		}
	}

	return result
}
