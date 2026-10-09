// Package plan builds, hashes, saves and verifies immutable deployment plans.
package plan

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/xraph/forge/cmd/forge/internal/deploy/model"
	"github.com/xraph/forge/cmd/forge/internal/deploy/output"
	"github.com/xraph/forge/cmd/forge/internal/deploy/render"
	"github.com/xraph/forge/cmd/forge/internal/deploy/spec"
	"github.com/xraph/forge/cmd/forge/internal/deploy/state"
)

const Schema = "forge.deploy.plan/v1"

type OpKind string

const (
	OpBuild   OpKind = "build"
	OpPush    OpKind = "push"
	OpCreate  OpKind = "create"
	OpUpdate  OpKind = "update"
	OpDelete  OpKind = "delete"
	OpBind    OpKind = "bind"
	OpDeliver OpKind = "deliver"
	OpMigrate OpKind = "migrate"
	OpRollout OpKind = "rollout"
	OpRoute   OpKind = "route"
	OpCheck   OpKind = "check"
)

type Operation struct {
	ID          string   `json:"id"`
	Kind        OpKind   `json:"kind"`
	Service     string   `json:"service,omitempty"`
	Resource    string   `json:"resource,omitempty"`
	Destructive bool     `json:"destructive"`
	DependsOn   []string `json:"depends_on,omitempty"`
	Detail      string   `json:"detail"`
}

type Plan struct {
	Schema      string             `json:"schema"`
	Hash        string             `json:"hash"`
	CreatedAt   time.Time          `json:"created_at"`
	TTL         time.Duration      `json:"ttl"`
	Project     string             `json:"project"`
	Environment string             `json:"environment"`
	TargetName  string             `json:"target"`
	Target      spec.Target        `json:"target_spec"`
	Deployment  *model.Deployment  `json:"deployment"`
	Files       map[string]string  `json:"files"`
	Inputs      map[string]string  `json:"inputs"`
	Images      []model.Image      `json:"images"`
	Operations  []Operation        `json:"operations"`
	Snapshot    state.Snapshot     `json:"snapshot"`
	Diagnostics output.Diagnostics `json:"diagnostics"`
}

func Build(d *model.Deployment, b *render.Bundle, snap state.Snapshot, inputs map[string]string, ops []Operation) (*Plan, error) {
	p := &Plan{Schema: Schema, CreatedAt: time.Now().UTC(), TTL: 24 * time.Hour,
		Project: d.Project, Environment: d.Environment, TargetName: d.TargetName, Target: d.Target,
		Deployment: d, Files: b.Hashes(), Inputs: inputs, Operations: ops, Snapshot: snap}
	for _, s := range d.Services {
		p.Images = append(p.Images, s.Image)
	}

	h, err := p.ComputeHash()
	if err != nil {
		return nil, err
	}

	p.Hash = h

	return p, nil
}

// Canonical is the plan without Hash and CreatedAt, with every SecretRef
// reduced to name and resolver. encoding/json sorts map keys, and struct
// fields keep declaration order, so the output is stable.
func (p *Plan) Canonical() ([]byte, error) {
	if p == nil {
		return nil, errors.New("missing deployment plan")
	}

	c := *p
	c.Hash, c.CreatedAt = "", time.Time{}

	if p.Deployment != nil {
		d := *p.Deployment

		d.Resources = append([]model.Resource(nil), d.Resources...)
		for i := range d.Resources {
			d.Resources[i].Secret = model.SecretRef{Name: d.Resources[i].Secret.Name, Resolver: d.Resources[i].Secret.Resolver, EnvVar: d.Resources[i].Secret.EnvVar}
		}

		d.Secrets = append([]model.SecretRef(nil), d.Secrets...)
		for i := range d.Secrets {
			d.Secrets[i] = model.SecretRef{Name: d.Secrets[i].Name, Resolver: d.Secrets[i].Resolver, EnvVar: d.Secrets[i].EnvVar}
		}

		c.Deployment = &d
	}

	var buf bytes.Buffer

	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)

	if err := enc.Encode(c); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

func (p *Plan) ComputeHash() (string, error) {
	data, err := p.Canonical()
	if err != nil {
		return "", err
	}

	sum := sha256.Sum256(data)

	return hex.EncodeToString(sum[:]), nil
}

func Save(dir string, p *Plan) (string, error) {
	if err := shape(p); err != nil {
		return "", err
	}

	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}

	root, err := os.OpenRoot(dir)
	if err != nil {
		return "", err
	}
	defer root.Close()

	name := fmt.Sprintf("%s-%s-%s.json", p.Environment, p.TargetName, p.Hash[:12])

	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return "", err
	}

	token := make([]byte, 16)
	if _, err := rand.Read(token); err != nil {
		return "", err
	}

	tmp := ".plan-" + hex.EncodeToString(token)

	f, err := root.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return "", err
	}
	defer func() { _ = root.Remove(tmp) }()

	if _, err = f.Write(data); err != nil {
		f.Close()

		return "", err
	}

	if err = f.Sync(); err != nil {
		f.Close()

		return "", err
	}

	if err = f.Close(); err != nil {
		return "", err
	}

	if err = root.Rename(tmp, name); err != nil {
		return "", err
	}

	return filepath.Join(dir, name), nil
}

func Load(path string) (*Plan, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var p Plan
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}

	if p.Schema != Schema {
		return nil, fmt.Errorf("%s: plan schema %q is not %q", path, p.Schema, Schema)
	}

	if err := shape(&p); err != nil {
		return nil, err
	}

	hash, err := p.ComputeHash()
	if err != nil {
		return nil, err
	}

	if hash != p.Hash {
		return nil, errors.New("saved plan hash mismatch")
	}

	return &p, nil
}

// Verify recomputes the hash and compares inputs with the current hashes.
func Verify(p *Plan, inputs map[string]string) output.Diagnostics {
	var diags output.Diagnostics

	if err := shape(p); err != nil {
		return output.Diagnostics{{Code: output.CodePlanHashMismatch, Severity: output.SeverityError, Message: err.Error()}}
	}

	h, err := p.ComputeHash()
	if err != nil || h != p.Hash {
		diags = append(diags, output.Diagnostic{Code: output.CodePlanHashMismatch, Severity: output.SeverityError,
			Message: "plan contents do not match its hash", Fix: "run forge deploy plan again"})

		return diags
	}

	if len(inputs) != len(p.Inputs) {
		diags = append(diags, output.Diagnostic{Code: output.CodePlanStale, Severity: output.SeverityError, Message: "project input files changed"})
	}

	for file, want := range p.Inputs {
		if got := inputs[file]; got != want {
			diags = append(diags, output.Diagnostic{Code: output.CodePlanStale, Severity: output.SeverityError,
				Message: file + " changed since the plan was written", File: file, Fix: "run forge deploy plan again"})
		}
	}

	if p.TTL <= 0 || p.TTL > 24*time.Hour || p.CreatedAt.After(time.Now().Add(5*time.Minute)) || time.Since(p.CreatedAt) > p.TTL {
		diags = append(diags, output.Diagnostic{Code: output.CodePlanStale, Severity: output.SeverityError,
			Message: "plan is older than its TTL", Fix: "run forge deploy plan again"})
	}

	return diags
}

func shape(p *Plan) error {
	if p == nil || p.Deployment == nil || p.Schema != Schema {
		return errors.New("invalid deployment plan")
	}

	name := regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)
	if !name.MatchString(p.TargetName) || !name.MatchString(p.Environment) || p.Project == "" || !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(p.Hash) {
		return errors.New("invalid plan identity")
	}

	if p.Project != p.Deployment.Project || p.TargetName != p.Deployment.TargetName || p.Environment != p.Deployment.Environment {
		return errors.New("plan and deployment identity differ")
	}

	return nil
}
