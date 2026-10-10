package persistence

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const bootstrapPath = ".forge/store.json"

func Hash(raw []byte) string {
	sum := sha256.Sum256(raw)

	return hex.EncodeToString(sum[:])
}
func Load(root string) (Options, error) {
	r, err := os.OpenRoot(root)
	if err != nil {
		return Options{}, err
	}
	defer r.Close()

	raw, err := r.ReadFile(bootstrapPath)
	if os.IsNotExist(err) {
		return Options{Backend: "files"}, nil
	}

	if err != nil {
		return Options{}, err
	}

	var options Options

	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(&options); err != nil {
		return Options{}, errors.New("invalid deployment store bootstrap")
	}

	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return Options{}, errors.New("invalid deployment store bootstrap")
	}

	if options.Backend != "files" && options.Backend != "sqlite" && options.Backend != "postgres" {
		return Options{}, errors.New("invalid deployment store backend")
	}

	if options.Backend != "files" && options.Project == "" {
		return Options{}, errors.New("deployment store project identity is missing")
	}

	return options, nil
}
func documentFile(root string) (string, []byte, error) {
	r, err := os.OpenRoot(root)
	if err != nil {
		return "", nil, err
	}
	defer r.Close()

	var (
		selected string
		raw      []byte
	)

	for _, name := range []string{".forge.yml", ".forge.yaml"} {
		value, err := r.ReadFile(name)
		if os.IsNotExist(err) {
			continue
		}

		if err != nil {
			return "", nil, err
		}

		if selected != "" {
			return "", nil, errors.New("both .forge.yml and .forge.yaml exist")
		}

		selected, raw = name, value
	}

	if selected == "" {
		return "", nil, os.ErrNotExist
	}

	return filepath.Join(root, selected), raw, nil
}
func OpenSelected(ctx context.Context, root string) (*DB, error) {
	options, err := Load(root)
	if err != nil {
		return nil, err
	}

	if options.Backend == "files" {
		return nil, nil //nolint:nilnil // Files are selected explicitly and do not open a database.
	}

	if options.Backend == "sqlite" {
		r, err := os.OpenRoot(root)
		if err != nil {
			return nil, err
		}
		defer r.Close()

		if _, err := r.Stat(options.Reference); err != nil {
			return nil, ErrUnavailable
		}
	}

	db, err := Open(ctx, root, options)
	if err != nil {
		return nil, err
	}

	if err := db.Active(ctx); err != nil {
		_ = db.Close()

		return nil, err
	}

	revision, _, err := db.Settings(ctx)
	if err != nil || revision == 0 {
		_ = db.Close()

		return nil, ErrUnavailable
	}

	return db, nil
}
func Document(ctx context.Context, root string) (string, uint64, []byte, error) {
	path, file, err := documentFile(root)
	if err != nil {
		return "", 0, nil, err
	}

	db, err := OpenSelected(ctx, root)
	if err != nil {
		return "", 0, nil, err
	}

	if db == nil {
		return path, 0, file, nil
	}
	defer db.Close()

	revision, raw, err := db.Settings(ctx)

	return path, revision, raw, err
}
func stableProject(raw []byte) (string, error) {
	var config struct {
		Project struct {
			Name   string `yaml:"name"`
			Module string `yaml:"module"`
		} `yaml:"project"`
	}
	if yaml.Unmarshal(raw, &config) != nil || config.Project.Name == "" {
		return "", errors.New("deployment store needs a project name")
	}

	return Hash([]byte(config.Project.Module + ":" + config.Project.Name)), nil
}
func atomic(r *os.Root, path string, raw []byte) error {
	if !filepath.IsLocal(path) {
		return errors.New("invalid deployment store path")
	}

	if err := r.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}

	token := make([]byte, 16)
	if _, err := rand.Read(token); err != nil {
		return err
	}

	temp := filepath.Join(filepath.Dir(path), ".store-"+hex.EncodeToString(token))

	f, err := r.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer func() { _ = r.Remove(temp) }()

	if _, err := f.Write(raw); err != nil {
		_ = f.Close()

		return err
	}

	if err := f.Sync(); err != nil {
		_ = f.Close()

		return err
	}

	if err := f.Close(); err != nil {
		return err
	}

	return r.Rename(temp, path)
}
func Authority(root string, exclusive bool) (func(), error) {
	r, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer r.Close()

	if err := r.MkdirAll(".forge", 0700); err != nil {
		return nil, err
	}

	f, err := r.OpenFile(".forge/authority.lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}

	return authorityLock(f, exclusive)
}

type Blob struct {
	Scope   string
	Name    string
	Content []byte
}

func MetadataName(path string) bool {
	if path == "image-rollout.json" {
		return false
	}

	return strings.HasSuffix(path, ".json") || strings.HasSuffix(path, ".jsonl")
}
func fileBlobs(root string) ([]Blob, error) {
	r, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer r.Close()

	out := []Blob{}

	for _, dir := range []string{".forge/state", ".forge/plans"} {
		err := fs.WalkDir(r.FS(), dir, func(path string, entry fs.DirEntry, walkErr error) error {
			if os.IsNotExist(walkErr) && path == dir {
				return nil
			}

			if walkErr != nil {
				return walkErr
			}

			if entry.Type()&os.ModeSymlink != 0 {
				return errors.New("deployment metadata cannot contain symlinks")
			}

			if entry.IsDir() || !MetadataName(path) {
				return nil
			}

			scope, name := "plans", filepath.Base(path)
			if dir == ".forge/state" {
				rel, err := filepath.Rel(dir, path)
				if err != nil {
					return err
				}

				parts := strings.Split(filepath.ToSlash(rel), "/")
				if len(parts) != 3 {
					return nil
				}

				scope = parts[0] + "/" + parts[1]
				name = parts[2]
			}

			raw, err := r.ReadFile(path)
			if err != nil {
				return err
			}

			out = append(out, Blob{Scope: scope, Name: name, Content: raw})

			return nil
		})
		if err != nil {
			return nil, err
		}
	}

	return out, nil
}
func (d *DB) Blobs(ctx context.Context) ([]Blob, error) {
	rows, err := d.sql.QueryContext(ctx, "SELECT scope,name,content FROM forge_deploy_blobs WHERE project=$1 ORDER BY scope,name", d.options.Project)
	if err != nil {
		return nil, ErrUnavailable
	}
	defer rows.Close()

	out := []Blob{}

	for rows.Next() {
		var blob Blob
		if err := rows.Scan(&blob.Scope, &blob.Name, &blob.Content); err != nil {
			return nil, ErrUnavailable
		}

		out = append(out, blob)
	}

	if err := rows.Err(); err != nil {
		return nil, ErrUnavailable
	}

	return out, nil
}
func Configure(ctx context.Context, root string, options Options, expectedHash string) error {
	unlock, err := Authority(root, true)
	if err != nil {
		return err
	}
	defer unlock()

	old, err := Load(root)
	if err != nil {
		return err
	}

	path, _, raw, err := Document(ctx, root)
	if err != nil {
		return err
	}

	if expectedHash == "" || Hash(raw) != expectedHash {
		return ErrConflict
	}

	if options.Backend == "" {
		options.Backend = "files"
	}

	if options.Project == "" {
		options.Project, err = stableProject(raw)
		if err != nil {
			return err
		}
	}

	if old.Backend == options.Backend && old.Reference == options.Reference {
		return nil
	}

	var (
		source      *DB
		sourceLease *Lease
	)

	if old.Backend != "files" {
		source, err = OpenSelected(ctx, root)
		if err != nil {
			return err
		}
		defer source.Close()

		sourceLease, err = source.Acquire(ctx, "authority")
		if err != nil {
			return err
		}

		defer func() { _ = sourceLease.Release() }()

		_, fresh, err := source.Settings(ctx)
		if err != nil {
			return err
		}

		if Hash(fresh) != expectedHash {
			return ErrConflict
		}
	}

	blobs, err := fileBlobs(root)
	if source != nil {
		blobs, err = source.Blobs(ctx)
	}

	if err != nil {
		return err
	}

	if options.Backend == "files" {
		r, err := os.OpenRoot(root)
		if err != nil {
			return err
		}
		defer r.Close()
		// Database authority remains selected until every exported file is durable.
		for _, blob := range blobs {
			if !filepath.IsLocal(blob.Name) || filepath.Base(blob.Name) != blob.Name || !MetadataName(blob.Name) {
				return errors.New("invalid stored metadata path")
			}

			dir := filepath.Join(".forge/state", blob.Scope)
			if blob.Scope == "plans" {
				dir = ".forge/plans"
			} else if !validScope(blob.Scope) {
				return errors.New("invalid stored environment scope")
			}

			if err := atomic(r, filepath.Join(dir, blob.Name), blob.Content); err != nil {
				return err
			}
		}

		if err := atomic(r, filepath.Base(path), raw); err != nil {
			return err
		}
	} else {
		destination, err := Open(ctx, root, options)
		if err != nil {
			return err
		}
		defer destination.Close()

		gate, err := destination.Acquire(ctx, "authority")
		if err != nil {
			return err
		}

		defer func() { _ = gate.Release() }()

		revision, existing, err := destination.Settings(ctx)
		if err != nil {
			return err
		}

		if revision > 0 && Hash(existing) != expectedHash {
			return ErrConflict
		}

		existingBlobs, err := destination.Blobs(ctx)
		if err != nil {
			return err
		}

		wanted := map[string][]byte{}
		for _, blob := range blobs {
			wanted[blob.Scope+"/"+blob.Name] = blob.Content
		}

		for _, blob := range existingBlobs {
			if !bytes.Equal(wanted[blob.Scope+"/"+blob.Name], blob.Content) {
				return ErrConflict
			}
		}

		if revision == 0 {
			if err := destination.SaveSettings(ctx, 0, raw); err != nil {
				return err
			}
		}

		scopes := map[string][]Blob{}
		for _, blob := range blobs {
			scopes[blob.Scope] = append(scopes[blob.Scope], blob)
		}

		keys := []string{}
		for scope := range scopes {
			keys = append(keys, scope)
		}

		sort.Strings(keys)

		for _, scope := range keys {
			lease, err := destination.Acquire(ctx, scope)
			if err != nil {
				return err
			}

			if err := lease.GuardedBy(gate); err != nil {
				_ = lease.Release()

				return err
			}

			for _, blob := range scopes[scope] {
				if err := lease.Write(ctx, blob.Name, blob.Content); err != nil {
					_ = lease.Release()

					return err
				}
			}

			if err := lease.Release(); err != nil {
				return err
			}
		}

		_, verified, err := destination.Settings(ctx)
		if err != nil || Hash(verified) != expectedHash {
			return ErrConflict
		}

		copied, err := destination.Blobs(ctx)
		if err != nil {
			return err
		}

		if len(copied) != len(blobs) {
			return errors.New("deployment store copy verification failed")
		}

		for _, blob := range copied {
			if !bytes.Equal(wanted[blob.Scope+"/"+blob.Name], blob.Content) {
				return errors.New("deployment store copy verification failed")
			}
		}
	}

	if source == nil {
		if err := verifyFileSource(root, expectedHash, blobs); err != nil {
			return err
		}
	} else {
		if err := sourceLease.Renew(ctx); err != nil {
			return err
		}

		_, fresh, err := source.Settings(ctx)
		if err != nil {
			return err
		}

		current, err := source.Blobs(ctx)
		if err != nil {
			return err
		}

		if Hash(fresh) != expectedHash || !sameBlobs(blobs, current) {
			return ErrConflict
		}
	}

	r, err := os.OpenRoot(root)
	if err != nil {
		return err
	}
	defer r.Close()

	payload, err := json.MarshalIndent(options, "", "  ")
	if err != nil {
		return err
	}

	if err := r.Chmod(".forge", 0700); err != nil {
		return err
	}

	if err := atomic(r, bootstrapPath, append(payload, '\n')); err != nil {
		return err
	}

	if sourceLease != nil {
		if err := sourceLease.retire(ctx); err != nil {
			previous, marshalErr := json.MarshalIndent(old, "", "  ")
			if marshalErr != nil {
				return marshalErr
			}

			if restoreErr := atomic(r, bootstrapPath, append(previous, '\n')); restoreErr != nil {
				return errors.Join(err, restoreErr)
			}

			return err
		}
	}

	return nil
}
func validScope(scope string) bool {
	parts := strings.Split(scope, "/")
	if len(parts) != 2 {
		return false
	}

	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return false
		}

		for _, c := range part {
			if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' {
				return false
			}
		}
	}

	return true
}

func verifyFileSource(root, expectedHash string, expected []Blob) error {
	_, raw, err := documentFile(root)
	if err != nil {
		return err
	}

	if Hash(raw) != expectedHash {
		return ErrConflict
	}

	actual, err := fileBlobs(root)
	if err != nil {
		return err
	}

	if !sameBlobs(expected, actual) {
		return ErrConflict
	}

	return nil
}
func sameBlobs(expected, actual []Blob) bool {
	if len(expected) != len(actual) {
		return false
	}

	hashes := map[string]string{}
	for _, blob := range expected {
		hashes[blob.Scope+"/"+blob.Name] = Hash(blob.Content)
	}

	if len(hashes) != len(expected) {
		return false
	}

	for _, blob := range actual {
		if hashes[blob.Scope+"/"+blob.Name] != Hash(blob.Content) {
			return false
		}
	}

	return true
}
