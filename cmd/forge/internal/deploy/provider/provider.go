// Package provider defines deployment adapters shared by the CLI and workbench.
package provider

import (
	"context"
	"errors"
	"github.com/xraph/forge/cmd/forge/internal/deploy/model"
	"github.com/xraph/forge/cmd/forge/internal/deploy/output"
	"github.com/xraph/forge/cmd/forge/internal/deploy/plan"
	"github.com/xraph/forge/cmd/forge/internal/deploy/render"
	"github.com/xraph/forge/cmd/forge/internal/deploy/spec"
	"github.com/xraph/forge/cmd/forge/internal/deploy/state"
	"io"
)

type Event struct {
	Op      string       `json:"op"`
	Status  state.Status `json:"status"`
	Message string       `json:"message"`
}
type ServiceStatus struct {
	Ready   int         `json:"ready"`
	Desired int         `json:"desired"`
	Image   model.Image `json:"image"`
	Message string      `json:"message"`
}
type Status struct {
	Overall   state.Status             `json:"overall"`
	Services  map[string]ServiceStatus `json:"services"`
	Resources map[string]state.Status  `json:"resources"`
	Routes    []string                 `json:"routes"`
}
type ServiceRef struct{ Target, Env, Service string }
type EnvRef struct{ Target, Env string }
type LogOptions struct {
	Follow bool
	Tail   int
}
type DestroyOptions struct{ DeleteData bool }

var ErrUnsupported = errors.New("not supported by this provider")

type Provider interface {
	Name() string
	Capabilities(ctx context.Context, target spec.Target) (model.Capabilities, error)
	Validate(ctx context.Context, deployment *model.Deployment) output.Diagnostics
	Render(ctx context.Context, deployment *model.Deployment) (*render.Bundle, error)
	Operations(ctx context.Context, deployment *model.Deployment, bundle *render.Bundle, snapshot state.Snapshot) ([]plan.Operation, error)
	Apply(ctx context.Context, p *plan.Plan, store *state.Store, values map[string]string, events chan<- Event) error
	Observe(ctx context.Context, ref EnvRef, store *state.Store) (Status, error)
	Logs(ctx context.Context, ref ServiceRef, options LogOptions) (io.ReadCloser, error)
	Rollback(ctx context.Context, ref EnvRef, store *state.Store, release string) error
	Destroy(ctx context.Context, p *plan.Plan, store *state.Store, options DestroyOptions) error
}

// ExportOnly gives adapters explicit unsupported defaults for unimplemented operations.
type ExportOnly struct{ ProviderName string }

func (e ExportOnly) Name() string { return e.ProviderName }
func (ExportOnly) Capabilities(context.Context, spec.Target) (model.Capabilities, error) {
	return model.Capabilities{Level: model.LevelRenderable}, nil
}
func (ExportOnly) Validate(context.Context, *model.Deployment) output.Diagnostics {
	return output.Diagnostics{{Code: output.CodeUnsupportedCommand, Severity: output.SeverityError, Message: "adapter cannot generate this deployment"}}
}
func (ExportOnly) Render(context.Context, *model.Deployment) (*render.Bundle, error) {
	return nil, ErrUnsupported
}
func (ExportOnly) Operations(context.Context, *model.Deployment, *render.Bundle, state.Snapshot) ([]plan.Operation, error) {
	return nil, ErrUnsupported
}
func (ExportOnly) Apply(context.Context, *plan.Plan, *state.Store, map[string]string, chan<- Event) error {
	return ErrUnsupported
}
func (ExportOnly) Observe(context.Context, EnvRef, *state.Store) (Status, error) {
	return Status{Overall: state.StatusUnknown}, ErrUnsupported
}
func (ExportOnly) Logs(context.Context, ServiceRef, LogOptions) (io.ReadCloser, error) {
	return nil, ErrUnsupported
}
func (ExportOnly) Rollback(context.Context, EnvRef, *state.Store, string) error {
	return ErrUnsupported
}
func (ExportOnly) Destroy(context.Context, *plan.Plan, *state.Store, DestroyOptions) error {
	return ErrUnsupported
}
