package images

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"hash"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/xraph/forge/cmd/forge/internal/deploy/model"
	"github.com/xraph/forge/cmd/forge/internal/deploy/plan"
	"github.com/xraph/forge/cmd/forge/internal/deploy/state"
)

var moduleTokens = regexp.MustCompile(`"(?:\\.|[^"\\])*"|[^\s()]+`)

// IncludedSource is shared with planning so nested source directories remain hashed.
func IncludedSource(relative string, isDir bool) bool {
	slash := filepath.ToSlash(relative)
	base := filepath.Base(relative)

	if isDir {
		if slash == ".forge" || slash == ".git" || slash == "deployments" {
			return false
		}

		switch base {
		case "node_modules", ".next", ".turbo":
			return false
		}

		return true
	}

	return base != ".DS_Store" && base != ".env" && !strings.HasPrefix(base, ".env.") && filepath.Ext(base) != ".pem"
}
func safeModulePath(root *os.Root, base, path string) error {
	unquoted, err := strconv.Unquote(path)
	if err == nil {
		path = unquoted
	}

	if filepath.IsAbs(path) {
		return errors.New("local Go module paths must stay inside the project")
	}

	relative := filepath.Clean(filepath.Join(base, path))
	if !filepath.IsLocal(relative) {
		return errors.New("local Go module paths must stay inside the project")
	}

	module, err := root.OpenRoot(relative)
	if err != nil {
		return errors.New("local Go module path is unavailable inside the project")
	}
	defer module.Close()

	if _, err := module.ReadFile("go.mod"); err != nil {
		return errors.New("local Go module has no go.mod")
	}

	return nil
}
func validateModuleFile(root *os.Root, relative string, raw []byte) error {
	scan := bufio.NewScanner(strings.NewReader(string(raw)))
	inUse := false

	for scan.Scan() {
		line := strings.TrimSpace(scan.Text())
		if strings.HasPrefix(line, "//") {
			continue
		}

		tokens := moduleTokens.FindAllString(line, -1)
		if len(tokens) == 0 {
			if strings.Contains(line, ")") {
				inUse = false
			}

			continue
		}

		if filepath.Base(relative) == "go.work" {
			if tokens[0] == "use" {
				if strings.Contains(line, "(") {
					inUse = true
				}

				tokens = tokens[1:]
			} else if !inUse {
				tokens = nil
			}

			if len(tokens) > 0 {
				if err := safeModulePath(root, filepath.Dir(relative), tokens[0]); err != nil {
					return err
				}
			}
		}

		tokens = moduleTokens.FindAllString(line, -1)
		for i, token := range tokens {
			if token != "=>" || i+1 >= len(tokens) {
				continue
			}

			path := tokens[i+1]

			unquoted, err := strconv.Unquote(path)
			if err == nil {
				path = unquoted
			}

			if strings.HasPrefix(path, ".") || filepath.IsAbs(path) || strings.Contains(path, "\\") {
				if err := safeModulePath(root, filepath.Dir(relative), path); err != nil {
					return err
				}
			}
		}
	}

	return scan.Err()
}
func moduleFor(rootPath, main string) (string, string, error) {
	if !filepath.IsLocal(main) || strings.ContainsAny(main, "\r\n") {
		return "", "", errors.New("invalid service main path")
	}

	root, err := os.OpenRoot(rootPath)
	if err != nil {
		return "", "", err
	}
	defer root.Close()

	dir := filepath.Clean(main)
	for {
		if _, err := root.ReadFile(filepath.Join(dir, "go.mod")); err == nil {
			pkg, err := filepath.Rel(dir, main)

			return dir, "./" + filepath.ToSlash(pkg), err
		}

		if dir == "." {
			return "", "", errors.New("service has no go.mod inside the project")
		}

		dir = filepath.Dir(dir)
	}
}

func buildContext(ctx context.Context, rootPath string, d *model.Deployment, st *state.Store, p *plan.Plan) (string, error) {
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		return "", err
	}
	defer root.Close()

	hash := p.Hash
	if !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(hash) {
		return "", errors.New("invalid build plan identity")
	}

	relative := filepath.Join("build", "source-"+hash)
	if err := st.MkdirAll(relative); err != nil {
		return "", err
	}

	destination := filepath.Join(st.Dir(), relative)

	dest, err := os.OpenRoot(destination)
	if err != nil {
		return "", err
	}
	defer dest.Close()

	excludes := map[string]bool{".forge.yml": true, ".forge.yaml": true}

	for _, path := range d.BuildExcludes {
		if !filepath.IsLocal(path) || strings.ContainsAny(path, "\r\n") {
			return "", errors.New("invalid runtime config path")
		}

		excludes[filepath.Clean(path)] = true
	}

	err = filepath.WalkDir(rootPath, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if err := ctx.Err(); err != nil {
			return err
		}

		rel, err := filepath.Rel(rootPath, path)
		if err != nil {
			return err
		}

		if rel == "." {
			return nil
		}

		if !IncludedSource(rel, entry.IsDir()) || excludes[rel] {
			if entry.IsDir() {
				return filepath.SkipDir
			}

			return nil
		}

		if entry.Type()&os.ModeSymlink != 0 {
			return errors.New("build context contains a symbolic link; use files inside the project")
		}

		if entry.IsDir() {
			return dest.MkdirAll(rel, 0700)
		}

		if !entry.Type().IsRegular() {
			return errors.New("build context contains a non-regular file")
		}

		if strings.HasPrefix(filepath.Base(rel), "config") && (filepath.Ext(rel) == ".yaml" || filepath.Ext(rel) == ".yml") {
			return nil
		}

		source, err := root.Open(rel)
		if err != nil {
			return err
		}
		defer source.Close()

		if filepath.Base(rel) == "go.mod" || filepath.Base(rel) == "go.work" {
			raw, err := root.ReadFile(rel)
			if err != nil {
				return err
			}

			if err := validateModuleFile(root, rel, raw); err != nil {
				return err
			}
		}

		out, err := dest.OpenFile(rel, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}

		info, err := entry.Info()
		if err != nil {
			out.Close()

			return err
		}

		if err := dest.Chmod(rel, info.Mode().Perm()); err != nil {
			out.Close()

			return err
		}

		sum := SourceHasher(info.Mode())

		_, copyErr := io.Copy(io.MultiWriter(out, sum), source)
		if copyErr == nil && len(p.Inputs) > 0 {
			if p.Inputs[filepath.ToSlash(rel)] != hex.EncodeToString(sum.Sum(nil)) {
				copyErr = errors.New("build source changed after plan approval")
			}
		}

		closeErr := out.Close()

		return errors.Join(copyErr, closeErr)
	})
	if err != nil {
		return "", err
	}

	return destination, nil
}

// SourceHasher includes permissions because Docker COPY preserves executable assets.
func SourceHasher(mode fs.FileMode) hash.Hash {
	sum := sha256.New()
	_, _ = sum.Write([]byte(strconv.FormatUint(uint64(mode.Perm()), 8) + "\x00"))

	return sum
}
func SourceDigest(raw []byte, mode fs.FileMode) string {
	sum := SourceHasher(mode)
	_, _ = sum.Write(raw)

	return hex.EncodeToString(sum.Sum(nil))
}
