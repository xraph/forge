package plan

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/xraph/forge/cmd/forge/internal/deploy/persistence"
)

func Recorded(root, hash, target, env string) (*Plan, error) {
	if !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(hash) {
		return nil, errors.New("invalid recorded plan hash")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	db, err := persistence.OpenSelected(ctx, root)
	if err != nil {
		return nil, err
	}

	entries := []persistence.Blob{}

	if db != nil {
		defer db.Close()

		entries, err = db.Blobs(ctx)
		if err != nil {
			return nil, err
		}
	} else {
		r, err := os.OpenRoot(root)
		if err != nil {
			return nil, err
		}
		defer r.Close()

		dir, err := r.Open(".forge/plans")
		if err != nil {
			return nil, err
		}

		names, err := dir.Readdirnames(-1)
		_ = dir.Close()

		if err != nil {
			return nil, err
		}

		for _, name := range names {
			if filepath.Ext(name) != ".json" {
				continue
			}

			raw, err := r.ReadFile(filepath.Join(".forge/plans", name))
			if err != nil {
				return nil, err
			}

			entries = append(entries, persistence.Blob{Scope: "plans", Name: name, Content: raw})
		}
	}

	for _, entry := range entries {
		if entry.Scope != "plans" || !strings.HasSuffix(entry.Name, "-"+hash[:12]+".json") {
			continue
		}

		p, err := Decode(entry.Content)
		if err != nil {
			return nil, err
		}

		if p.Hash == hash && (target == "" || p.TargetName == target) && (env == "" || p.Environment == env) {
			return p, nil
		}
	}

	return nil, errors.New("recorded deployment plan is unavailable")
}
