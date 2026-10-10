package secrets

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"

	"github.com/xraph/forge/cmd/forge/internal/deploy/spec"
)

// Reference resolves an explicitly named credential without serializing its value.
// Inspection returns resolution metadata; only apply asks for the value.
func Reference(ctx context.Context, cfg spec.Secrets, root string, base Resolver, name string, apply bool) (Status, string, error) {
	if err := ctx.Err(); err != nil {
		return Status{}, "", err
	}

	ref := name
	if mapped, ok := cfg.References[name]; ok {
		ref = mapped
	}

	var value string

	switch {
	case strings.HasPrefix(ref, "env:"):
		key := strings.TrimPrefix(ref, "env:")
		if key == "" || strings.ContainsAny(key, "=\x00\r\n") {
			return Status{}, "", errors.New("invalid environment credential reference")
		}

		value = os.Getenv(key)

		status := Status{Resolved: value != "", Where: "environment " + key}
		if !apply {
			value = ""
		}

		return status, value, nil
	case strings.HasPrefix(ref, "file:"):
		path := strings.TrimPrefix(ref, "file:")

		handle, err := os.OpenRoot(root)
		if err != nil {
			return Status{}, "", errors.New("credential file is unavailable")
		}
		defer handle.Close()

		file, err := handle.Open(path)
		if os.IsNotExist(err) {
			return Status{Where: "credential file " + path + " (missing)"}, "", nil
		}

		if err != nil {
			return Status{}, "", errors.New("credential file is unavailable or outside the project")
		}

		defer file.Close()

		raw, err := io.ReadAll(io.LimitReader(file, 65537))
		if err != nil || len(raw) > 65536 {
			return Status{}, "", errors.New("credential file is invalid")
		}

		value = strings.TrimSpace(string(raw))

		status := Status{Resolved: value != "", Where: "credential file " + path}
		if !apply {
			value = ""
		}

		return status, value, nil
	default:
		status, err := base.Check(ctx, ref)
		if err != nil || !apply || !status.Resolved {
			return status, "", err
		}

		resolver, ok := base.(interface {
			ValuesForApply(ctx context.Context) (map[string]string, error)
		})
		if !ok {
			return status, "", errors.New("credential resolver cannot supply apply values")
		}

		values, err := resolver.ValuesForApply(ctx)
		if err != nil {
			return Status{}, "", errors.New("credential resolver is unavailable")
		}

		return status, values[base.EnvVar(ref)], nil
	}
}
