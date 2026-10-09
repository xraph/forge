package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/xraph/forge/cmd/forge/internal/deploy/output"
	"github.com/xraph/forge/cmd/forge/internal/deploy/spec"
	"os"
	"path/filepath"
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

	out := map[string]string{}

	for path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}

		out[path] = digest(raw)
	}

	out[res.Doc.Path] = res.Doc.Hash
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

	for p, want := range res.InputHashes {
		raw, err := os.ReadFile(p)
		if err != nil || digest(raw) != want {
			return output.Fail(output.ExitConflict, "configuration changed after preview", output.Diagnostic{Code: output.CodePlanStale, Severity: output.SeverityError, File: p, Message: "review a new proposal before saving"})
		}
	}

	for path, data := range files {
		want, owned := res.InputHashes[path]
		if !owned {
			return errors.New("proposal cannot write an unreviewed path")
		}

		if err := spec.Write(path, want, data); err != nil {
			return err
		}
	}

	return nil
}
