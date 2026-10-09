// Package secrets reports whether named secrets resolve. Only apply reads values.
package secrets

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/joho/godotenv"
	"os"
	"path/filepath"
	"strings"

	"github.com/xraph/forge/cmd/forge/internal/deploy/execx"
	"github.com/xraph/forge/cmd/forge/internal/deploy/spec"
)

type Status struct {
	Unverified bool
	Resolved   bool
	Where      string
}

type Resolver interface {
	Name() string
	Check(ctx context.Context, name string) (Status, error)
	EnvVar(name string) string
}

// EnvVarName is UPPER(project)_UPPER(name) with hyphens as underscores.
func EnvVarName(project, name string) string {
	norm := func(s string) string { return strings.ToUpper(strings.ReplaceAll(s, "-", "_")) }

	return norm(project) + "_" + norm(name)
}

func New(cfg spec.Secrets, projectRoot string, runner execx.Runner, target spec.Target, project string) (Resolver, error) {
	switch cfg.Resolver {
	case "", "env":
		return &EnvResolver{Project: project}, nil
	case "file":
		p := cfg.File
		if p == "" {
			p = ".forge/secrets.env"
		}

		if filepath.IsAbs(p) || p == ".." || strings.HasPrefix(filepath.Clean(p), ".."+string(filepath.Separator)) {
			return nil, errors.New("secret file must be inside project")
		}

		return &FileResolver{Root: projectRoot, Project: project, Path: filepath.Join(projectRoot, p), Rel: p}, nil
	case "kubernetes":
		if runner == nil {
			runner = execx.System()
		}

		return &KubernetesResolver{Project: project, Runner: runner, Context: target.Context, Namespace: target.Namespace}, nil
	}

	return nil, fmt.Errorf("unknown secrets resolver %q", cfg.Resolver)
}

type EnvResolver struct{ Project string }

func (r *EnvResolver) Name() string              { return "env" }
func (r *EnvResolver) EnvVar(name string) string { return EnvVarName(r.Project, name) }
func (r *EnvResolver) Check(ctx context.Context, name string) (Status, error) {
	if err := ctx.Err(); err != nil {
		return Status{}, err
	}

	v := r.EnvVar(name)
	if value, ok := os.LookupEnv(v); ok && value != "" {
		return Status{Resolved: true, Where: "environment " + v}, nil
	}

	return Status{Where: "environment " + v + " (unset)"}, nil
}
func (r *EnvResolver) ValuesForApply(_ context.Context) (map[string]string, error) {
	out := map[string]string{}

	prefix := strings.ToUpper(strings.ReplaceAll(r.Project, "-", "_")) + "_"
	for _, kv := range os.Environ() {
		if k, v, ok := strings.Cut(kv, "="); ok && strings.HasPrefix(k, prefix) {
			out[k] = v
		}
	}

	return out, nil
}

type FileResolver struct {
	Root    string
	Project string
	Path    string
	Rel     string
}

func (r *FileResolver) Name() string              { return "file" }
func (r *FileResolver) EnvVar(name string) string { return EnvVarName(r.Project, name) }

func (r *FileResolver) parse() (map[string]string, map[string]int, error) {
	root, err := os.OpenRoot(r.Root)
	if err != nil {
		return nil, nil, err
	}
	defer root.Close()

	f, err := root.Open(r.Rel)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()

	values, err := godotenv.Parse(f)
	if err != nil {
		return nil, nil, fmt.Errorf("parse secret file %s: invalid dotenv syntax", r.Rel)
	}

	if _, err := f.Seek(0, 0); err != nil {
		return nil, nil, err
	}

	lines := map[string]int{}
	sc := bufio.NewScanner(f)

	n := 0
	for sc.Scan() {
		n++

		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		k, _, ok := strings.Cut(strings.TrimPrefix(line, "export "), "=")
		if !ok {
			continue
		}

		lines[strings.TrimSpace(k)] = n
	}

	return values, lines, sc.Err()
}

func (r *FileResolver) Check(ctx context.Context, name string) (Status, error) {
	if err := ctx.Err(); err != nil {
		return Status{}, err
	}

	v := r.EnvVar(name)

	values, lines, err := r.parse()
	if err != nil {
		if os.IsNotExist(err) {
			return Status{Where: r.Rel + " (missing)"}, nil
		}

		return Status{}, err
	}

	if l, ok := lines[v]; ok && values[v] != "" {
		return Status{Resolved: true, Where: fmt.Sprintf("%s:%d", r.Rel, l)}, nil
	}

	return Status{Where: r.Rel + " (no " + v + ")"}, nil
}

func (r *FileResolver) ValuesForApply(_ context.Context) (map[string]string, error) {
	values, _, err := r.parse()

	return values, err
}

type KubernetesResolver struct {
	Project     string
	Environment string
	Runner      execx.Runner
	Context     string
	Namespace   string
}

func (r *KubernetesResolver) Name() string              { return "kubernetes" }
func (r *KubernetesResolver) EnvVar(name string) string { return EnvVarName(r.Project, name) }
func (r *KubernetesResolver) secretName() string        { return "forge-" + r.Project + "-" + r.Environment }

func (r *KubernetesResolver) data(ctx context.Context) (map[string]string, error) {
	args := []string{}
	if r.Context != "" {
		args = append(args, "--context", r.Context)
	}

	args = append(args, "get", "secret", r.secretName(), "-n", r.Namespace, "-o", "jsonpath={.data}")

	res, err := r.Runner.Run(ctx, execx.Command{Name: "kubectl", Args: args})
	if err != nil {
		return nil, err
	}

	out := map[string]string{}
	if strings.TrimSpace(res.Stdout) == "" {
		return out, nil
	}

	return out, json.Unmarshal([]byte(res.Stdout), &out)
}

func (r *KubernetesResolver) Check(ctx context.Context, name string) (Status, error) {
	v := r.EnvVar(name)

	d, err := r.data(ctx)
	if err != nil {
		return Status{}, err
	}

	if raw, err := base64.StdEncoding.DecodeString(d[v]); err == nil && len(raw) > 0 {
		return Status{Resolved: true, Where: "secret " + r.secretName() + " key " + v}, nil
	}

	return Status{Where: "secret " + r.secretName() + " (no " + v + ")"}, nil
}

func (r *KubernetesResolver) ValuesForApply(ctx context.Context) (map[string]string, error) {
	d, err := r.data(ctx)
	if err != nil {
		return nil, err
	}

	out := map[string]string{}

	for k, b64 := range d {
		raw, err := base64.StdEncoding.DecodeString(b64)
		if err != nil {
			return nil, fmt.Errorf("secret key %s: %w", k, err)
		}

		out[k] = string(raw)
	}

	return out, nil
}

// Deferred preserves references without performing remote I/O during inspection.
type Deferred struct{ Resolver }

func (r Deferred) Check(ctx context.Context, name string) (Status, error) {
	if err := ctx.Err(); err != nil {
		return Status{}, err
	}

	return Status{Unverified: true, Where: r.Name() + " (unverified offline)"}, nil
}
