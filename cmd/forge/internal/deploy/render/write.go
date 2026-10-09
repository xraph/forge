package render

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type WriteOptions struct {
	Force           bool
	PreserveMissing bool
}
type WriteResult struct {
	Written   []string `json:"written"`
	Unchanged []string `json:"unchanged"`
	Skipped   []string `json:"skipped"`
}

const manifestName = "forge-manifest.json"

// Write only changes owned artifacts. All paths are anchored inside dir.
func Write(dir string, b *Bundle, opts WriteOptions) (WriteResult, error) {
	var res WriteResult
	if err := os.MkdirAll(dir, 0700); err != nil {
		return res, err
	}

	root, err := os.OpenRoot(dir)
	if err != nil {
		return res, err
	}
	defer root.Close()

	old := Manifest{Files: map[string]string{}}
	if data, err := root.ReadFile(manifestName); err == nil {
		if err := json.Unmarshal(data, &old); err != nil {
			return res, fmt.Errorf("invalid ownership manifest: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return res, err
	}

	valid := func(p string) bool {
		return filepath.IsLocal(p) && filepath.Clean(p) == p && p != "." && p != manifestName
	}
	for p := range b.Files {
		if !valid(p) {
			return res, fmt.Errorf("invalid artifact path %q", p)
		}
	}

	for p := range old.Files {
		if !valid(p) {
			return res, fmt.Errorf("invalid owned path %q", p)
		}
	}

	for _, f := range b.Sorted() {
		current, err := root.ReadFile(f.Path)

		exists := err == nil
		if err != nil && !os.IsNotExist(err) {
			return res, err
		}

		if exists && hash(current) == hash(f.Content) {
			res.Unchanged = append(res.Unchanged, f.Path)

			continue
		}

		if exists && !opts.Force {
			oldHash, tracked := old.Files[f.Path]
			if !tracked || hash(current) != oldHash {
				res.Skipped = append(res.Skipped, f.Path)

				continue
			}
		}

		if err := root.MkdirAll(filepath.Dir(f.Path), 0700); err != nil {
			return res, err
		}

		if err := atomicWrite(root, f.Path, f.Content, f.Mode); err != nil {
			return res, err
		}

		res.Written = append(res.Written, f.Path)
	}

	for p, want := range old.Files {
		if _, keep := b.Files[p]; keep || opts.PreserveMissing || strings.HasPrefix(p, "patches/") {
			continue
		}

		current, err := root.ReadFile(p)
		if os.IsNotExist(err) {
			continue
		}

		if err != nil {
			return res, err
		}

		if hash(current) != want {
			res.Skipped = append(res.Skipped, p)

			continue
		}

		if err := root.Remove(p); err != nil {
			return res, err
		}
	}

	m := b.Manifest
	m.Files = b.Hashes()
	m.Generated = time.Now().UTC()

	if opts.PreserveMissing {
		for p, h := range old.Files {
			if _, ok := m.Files[p]; !ok {
				m.Files[p] = h
			}
		}
	}

	for _, p := range res.Skipped {
		if h, ok := old.Files[p]; ok {
			m.Files[p] = h
		} else {
			delete(m.Files, p)
		}
	}

	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return res, err
	}

	return res, atomicWrite(root, manifestName, append(data, '\n'), 0600)
}
func atomicWrite(root *os.Root, path string, data []byte, mode os.FileMode) error {
	token := make([]byte, 16)
	if _, err := rand.Read(token); err != nil {
		return err
	}

	temp := filepath.Join(filepath.Dir(path), ".forge-"+hex.EncodeToString(token))

	f, err := root.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	defer func() { _ = root.Remove(temp) }()

	if _, err := f.Write(data); err != nil {
		f.Close()

		return err
	}

	if err := f.Chmod(mode); err != nil {
		f.Close()

		return err
	}

	if err := f.Sync(); err != nil {
		f.Close()

		return err
	}

	if err := f.Close(); err != nil {
		return err
	}

	return root.Rename(temp, path)
}
