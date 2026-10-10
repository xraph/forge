package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"reflect"

	"github.com/xraph/forge/cmd/forge/internal/deploy/output"
	"github.com/xraph/forge/cmd/forge/internal/deploy/persistence"
	"github.com/xraph/forge/cmd/forge/internal/deploy/spec"
)

func digest(raw []byte) string {
	s := sha256.Sum256(raw)

	return hex.EncodeToString(s[:])
}
func (e *Engine) sourceHashes(res *InspectResult) (map[string]string, error) {
	paths := map[string]bool{res.Doc.Path: true}
	for path := range res.Doc.Splits {
		paths[path] = true
	}

	for _, a := range res.Discovery.Apps {
		for _, p := range a.ConfigPaths {
			paths[p] = true
		}

		for _, p := range []string{filepath.Join(a.Dir, ".forge.yml"), filepath.Join(a.Dir, ".forge.yaml")} {
			if _, err := os.Stat(p); err == nil {
				paths[p] = true
			}
		}
	}

	for _, name := range []string{"go.mod", "go.work"} {
		p := filepath.Join(e.cfg.RootDir, name)
		if _, err := os.Stat(p); err == nil {
			paths[p] = true
		}
	}

	root, err := os.OpenRoot(e.cfg.RootDir)
	if err != nil {
		return nil, err
	}
	defer root.Close()

	out := map[string]string{}

	for path := range paths {
		relative, err := filepath.Rel(e.cfg.RootDir, path)
		if err != nil || !filepath.IsLocal(relative) {
			return nil, errors.New("source outside project")
		}

		raw, err := root.ReadFile(relative)
		if err != nil {
			return nil, err
		}

		out[path] = digest(raw)
	}

	if res.Authority.Backend == "files" {
		out[res.Doc.Path] = res.Doc.Hash
	}

	for path, split := range res.Doc.Splits {
		out[path] = split.Hash
	}

	return out, nil
}

// SaveInit writes the bytes from the reviewed proposal, after rechecking its sources.
func (e *Engine) SaveInit(ctx context.Context, res *InspectResult, files map[string][]byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	unlock, err := persistence.Authority(e.cfg.RootDir, true)
	if err != nil {
		return err
	}
	defer unlock()

	fresh, err := e.load(ctx)
	if err != nil {
		return err
	}

	if !reflect.DeepEqual(fresh.InputHashes, res.InputHashes) || fresh.Revision != res.Revision || fresh.Authority != res.Authority || fresh.Doc.Hash != res.Doc.Hash {
		return output.Fail(output.ExitConflict, "configuration changed after preview", output.Diagnostic{Code: output.CodePlanStale, Severity: output.SeverityError, Message: "review a new proposal before saving"})
	}

	if len(files) != 1 {
		return errors.New("save a proposal for one configuration file at a time")
	}

	db, err := persistence.OpenSelected(ctx, e.cfg.RootDir)
	if err != nil {
		return err
	}

	if db != nil {
		defer db.Close()
	}

	for path, data := range files {
		want, owned := res.InputHashes[path]
		if !owned {
			return errors.New("proposal cannot write an unreviewed path")
		}

		if db != nil && path == res.Doc.Path {
			gate, err := db.Acquire(ctx, "authority")
			if err != nil {
				return err
			}
			defer func() { _ = gate.Release() }()

			revision, _, err := db.Settings(ctx)
			if err != nil {
				return err
			}

			if revision != res.Revision {
				return persistence.ErrConflict
			}

			return gate.SaveSettings(gate.Context(ctx), res.Revision, data)
		}

		if err := spec.WriteWithin(e.cfg.RootDir, path, want, data); err != nil {
			return err
		}
	}

	return nil
}
