// Package render holds generated deployment files in memory and writes them
// with an ownership manifest so user edits are never overwritten silently.
package render

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"sort"
	"time"
)

type File struct {
	Path    string
	Content []byte
	Mode    fs.FileMode
}

type Manifest struct {
	Schema    int               `json:"schema"`
	Generated time.Time         `json:"generated"`
	Target    string            `json:"target"`
	Env       string            `json:"env"`
	Files     map[string]string `json:"files"`
}

type Bundle struct {
	Files    map[string]File
	Manifest Manifest
}

func New(target, env string) *Bundle {
	return &Bundle{Files: map[string]File{}, Manifest: Manifest{Schema: 1, Target: target, Env: env, Files: map[string]string{}}}
}

func (b *Bundle) Add(path string, content []byte) { b.AddMode(path, content, 0o644) }

func (b *Bundle) AddMode(path string, content []byte, mode fs.FileMode) {
	b.Files[path] = File{Path: path, Content: content, Mode: mode}
	b.Manifest.Files[path] = hash(content)
}

func (b *Bundle) Hashes() map[string]string {
	out := map[string]string{}
	for p, f := range b.Files {
		out[p] = hash(f.Content)
	}

	return out
}

func (b *Bundle) Sorted() []File {
	out := make([]File, 0, len(b.Files))
	for _, f := range b.Files {
		out = append(out, f)
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })

	return out
}

func hash(b []byte) string {
	s := sha256.Sum256(b)

	return hex.EncodeToString(s[:])
}
