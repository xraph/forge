package engine

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"sort"
	"strings"

	"github.com/xraph/forge/cmd/forge/config"
	"github.com/xraph/forge/cmd/forge/internal/deploy/persistence"
	"github.com/xraph/forge/cmd/forge/internal/deploy/plan"
	"github.com/xraph/forge/cmd/forge/internal/deploy/spec"
	"github.com/xraph/forge/cmd/forge/internal/deploy/state"
)

type FileView struct {
	Path    string `json:"path"`
	Hash    string `json:"hash"`
	Content string `json:"content"`
}
type SettingsView struct {
	Hash     string              `json:"hash"`
	Revision uint64              `json:"revision"`
	Store    persistence.Options `json:"store"`
	Files    []FileView          `json:"files"`
	Deploy   *spec.Deploy        `json:"deploy"`
}

func (e *Engine) settings(ctx context.Context) (SettingsView, *spec.Document, error) {
	options, err := persistence.Load(e.cfg.RootDir)
	if err != nil {
		return SettingsView{}, nil, err
	}

	path, revision, raw, err := persistence.Document(ctx, e.cfg.RootDir)
	if err != nil {
		return SettingsView{}, nil, err
	}

	doc, diags, err := spec.ParseData(path, raw)
	if err != nil {
		return SettingsView{}, nil, err
	}

	if diags.HasErrors() {
		return SettingsView{}, nil, diagnosticError("invalid deployment settings", diags)
	}

	view := SettingsView{Revision: revision, Store: options, Deploy: doc.Deploy, Files: []FileView{}}

	paths := []string{doc.Path}
	for path := range doc.Splits {
		paths = append(paths, path)
	}

	sort.Strings(paths)

	hashes := map[string]string{}

	for _, path := range paths {
		content := doc.RawBytes()

		hash := doc.Hash
		if split, ok := doc.Splits[path]; ok {
			content = split.RawBytes()
			hash = split.Hash
		}

		rel, err := filepath.Rel(e.cfg.RootDir, path)
		if err != nil || !filepath.IsLocal(rel) {
			return SettingsView{}, nil, errors.New("configuration outside project")
		}

		view.Files = append(view.Files, FileView{Path: filepath.ToSlash(rel), Hash: hash, Content: string(content)})
		hashes[rel] = hash
	}

	input, err := json.Marshal(struct {
		Store    persistence.Options `json:"store"`
		Revision uint64              `json:"revision"`
		Hashes   map[string]string   `json:"hashes"`
	}{options, revision, hashes})
	if err != nil {
		return SettingsView{}, nil, err
	}

	view.Hash = digest(input)

	return view, doc, nil
}
func (e *Engine) Files(ctx context.Context) (SettingsView, error) {
	view, _, err := e.settings(ctx)

	return view, err
}
func (e *Engine) Save(ctx context.Context, expected string, ops []spec.Op) error {
	unlock, err := persistence.Authority(e.cfg.RootDir, true)
	if err != nil {
		return err
	}
	defer unlock()

	view, doc, err := e.settings(ctx)
	if err != nil {
		return err
	}

	if expected == "" || view.Hash != expected {
		return persistence.ErrConflict
	}

	if len(ops) == 0 || len(ops) > 256 {
		return errors.New("provide between 1 and 256 deployment edits")
	}

	for _, op := range ops {
		if !strings.HasPrefix(op.Path, "deploy.") {
			return errors.New("the deployment editor only owns deploy settings")
		}
	}

	files, err := doc.Patch(ops)
	if err != nil {
		return err
	}

	if len(files) != 1 {
		return errors.New("save edits for one configuration file at a time")
	}

	proposal := doc.RawBytes()
	if raw, ok := files[doc.Path]; ok {
		proposal = raw
	}

	parsed, diags, err := spec.ParseProposal(doc.Path, proposal, files)
	if err != nil {
		return err
	}

	cfg, err := config.LoadForgeConfigFrom(e.cfg.RootDir)
	if err != nil {
		return err
	}

	apps := []string{}
	for _, app := range cfg.Build.Apps {
		apps = append(apps, app.Name)
	}

	if len(apps) == 0 && doc.Deploy != nil {
		for _, service := range doc.Deploy.Services {
			apps = append(apps, service.App)
		}
	}

	diags = append(diags, spec.Validate(parsed, apps)...)
	if diags.HasErrors() {
		return diagnosticError("deployment edit is invalid", diags)
	}

	db, err := persistence.OpenSelected(ctx, e.cfg.RootDir)
	if err != nil {
		return err
	}

	if db != nil {
		defer db.Close()
	}

	var gate *persistence.Lease
	if db != nil {
		gate, err = db.Acquire(ctx, "authority")
		if err != nil {
			return err
		}
		defer func() { _ = gate.Release() }()

		ctx = gate.Context(ctx)
	}

	fresh, _, err := e.settings(ctx)
	if err != nil {
		return err
	}

	if fresh.Hash != expected {
		return persistence.ErrConflict
	}

	for path, data := range files {
		if path == doc.Path && gate != nil {
			return gate.SaveSettings(ctx, view.Revision, data)
		}

		hash := doc.Hash
		if split, ok := doc.Splits[path]; ok {
			hash = split.Hash
		}

		if err := spec.WriteWithin(e.cfg.RootDir, path, hash, data); err != nil {
			if errors.Is(err, spec.ErrConflict) {
				return persistence.ErrConflict
			}

			return err
		}
	}

	return nil
}
func (e *Engine) ConfigureStore(ctx context.Context, options persistence.Options, expected string) error {
	view, doc, err := e.settings(ctx)
	if err != nil {
		return err
	}

	if expected == "" || view.Hash != expected {
		return persistence.ErrConflict
	}

	return persistence.ConfigureVersion(ctx, e.cfg.RootDir, options, doc.Hash, view.Revision)
}

func (e *Engine) LoadPlan(ctx context.Context, hash string) (*plan.Plan, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	return plan.Recorded(e.cfg.RootDir, hash, "", "")
}
func (e *Engine) savePlan(ctx context.Context, p *plan.Plan, st *state.Store) error {
	options, err := persistence.Load(e.cfg.RootDir)
	if err != nil {
		return err
	}

	if options.Backend == "files" {
		_, err := plan.SaveWithin(e.cfg.RootDir, p)

		return err
	}

	if st == nil {
		st, err = state.Open(e.cfg.RootDir, p.TargetName, p.Environment)
		if err != nil {
			return err
		}
		defer st.Close()

		unlock, err := st.Lock(ctx)
		if err != nil {
			return err
		}
		defer unlock()

		ctx = st.Context(ctx)
	}

	raw, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}

	return st.WritePlan(ctx, filepath.Base(e.PlanPath(p)), raw)
}
