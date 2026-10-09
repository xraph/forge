package spec

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

var ErrConflict = errors.New("file changed since it was read")

type Op struct {
	Path   string
	Value  any
	Delete bool
}

// preferredOrder lists keys in the order a reader expects; everything else
// follows alphabetically.
var preferredOrder = map[string]int{
	"version": 0, "registry": 1, "defaults": 2, "spec": 3, "environment_files": 4,
	"services": 5, "resources": 6, "connections": 7, "environments": 8, "targets": 9, "secrets": 10,
	"app": 20, "kind": 21, "ports": 22, "health": 23, "config": 24, "replicas": 25, "migrate": 26,
	"bindings": 27, "calls": 28, "env": 29, "discovery": 30, "schedule": 31,
	"name": 40, "type": 41, "driver": 42, "dsn": 43, "lifecycle": 44, "secret": 45, "recipe": 46,
	"target": 50, "provider": 51,
}

func rank(k string) int {
	if r, ok := preferredOrder[k]; ok {
		return r
	}

	return 1000
}

// toNode converts a Go value into a yaml.Node with ordered map keys.
func toNode(v any) (*yaml.Node, error) {
	switch x := v.(type) {
	case *yaml.Node:
		return x, nil
	case map[string]any:
		n := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}

		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}

		sort.Slice(keys, func(i, j int) bool {
			if rank(keys[i]) != rank(keys[j]) {
				return rank(keys[i]) < rank(keys[j])
			}

			return keys[i] < keys[j]
		})

		for _, k := range keys {
			val, err := toNode(x[k])
			if err != nil {
				return nil, err
			}

			n.Content = append(n.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: k}, val)
		}

		return n, nil
	case []map[string]any:
		n := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}

		for _, item := range x {
			val, err := toNode(item)
			if err != nil {
				return nil, err
			}

			n.Content = append(n.Content, val)
		}

		return n, nil
	default:
		var n yaml.Node
		if err := n.Encode(v); err != nil {
			return nil, err
		}

		return &n, nil
	}
}

func mappingOf(n *yaml.Node) *yaml.Node {
	if n.Kind == yaml.DocumentNode && len(n.Content) > 0 {
		return n.Content[0]
	}

	return n
}

// cloneNode preserves graph identity between anchor definitions and aliases.
func cloneNode(root *yaml.Node) *yaml.Node {
	nodes := map[*yaml.Node]*yaml.Node{}

	var copyNode func(*yaml.Node) *yaml.Node

	copyNode = func(n *yaml.Node) *yaml.Node {
		if n == nil {
			return nil
		}

		if prior, ok := nodes[n]; ok {
			return prior
		}

		out := *n
		nodes[n] = &out

		out.Content = nil
		for _, child := range n.Content {
			out.Content = append(out.Content, copyNode(child))
		}

		out.Alias = copyNode(n.Alias)

		return &out
	}

	return copyNode(root)
}

func replaceNode(old, value *yaml.Node) {
	replacement := *value
	replacement.HeadComment = old.HeadComment
	replacement.LineComment = old.LineComment
	replacement.FootComment = old.FootComment
	replacement.Anchor = old.Anchor
	*old = replacement
}

func editPath(root *yaml.Node, parts []string, value *yaml.Node, remove bool) error {
	current := mappingOf(root)

	if len(parts) == 0 {
		if remove {
			return errors.New("cannot delete the document root")
		}

		replaceNode(current, value)

		return nil
	}

	if current.Kind == yaml.AliasNode {
		return errors.New("edit the anchor definition directly before changing an alias")
	}

	part := parts[0]
	if part == "" {
		return errors.New("empty configuration path segment")
	}

	if current.Kind == yaml.SequenceNode {
		index, err := strconv.Atoi(part)
		if err != nil || index < 0 || index >= len(current.Content) {
			return fmt.Errorf("invalid sequence index %q", part)
		}

		if len(parts) > 1 {
			return editPath(current.Content[index], parts[1:], value, remove)
		}

		if remove {
			current.Content = append(current.Content[:index], current.Content[index+1:]...)
		} else {
			replaceNode(current.Content[index], value)
		}

		return nil
	}

	if current.Kind != yaml.MappingNode {
		return fmt.Errorf("configuration path %q crosses a scalar", part)
	}

	index := -1

	for i := 0; i+1 < len(current.Content); i += 2 {
		if current.Content[i].Value == part {
			index = i

			break
		}
	}

	if index < 0 {
		if remove {
			return nil
		}

		next := value
		if len(parts) > 1 {
			next = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		}

		current.Content = append(current.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: part}, next)
		if len(parts) > 1 {
			return editPath(next, parts[1:], value, false)
		}

		return nil
	}

	if len(parts) > 1 {
		return editPath(current.Content[index+1], parts[1:], value, remove)
	}

	if remove {
		current.Content = append(current.Content[:index], current.Content[index+2:]...)
	} else {
		replaceNode(current.Content[index+1], value)
	}

	return nil
}

// fileFor picks the document or split that owns a path.
func (d *Document) fileFor(parts []string) (string, *yaml.Node, []byte, []string) {
	if d.Deploy != nil && len(parts) > 1 && parts[0] == "deploy" {
		if d.Deploy.Spec != "" && (parts[1] == "services" || parts[1] == "resources" || parts[1] == "connections") {
			p := filepath.Join(filepath.Dir(d.Path), d.Deploy.Spec)
			if s, ok := d.Splits[p]; ok {
				return p, s.Root, s.raw, parts[1:]
			}
		}

		if parts[1] == "environments" && len(parts) > 2 {
			if rel, ok := d.Deploy.EnvironmentFiles[parts[2]]; ok {
				p := filepath.Join(filepath.Dir(d.Path), rel)
				if s, ok := d.Splits[p]; ok {
					return p, s.Root, s.raw, parts[3:]
				}
			}
		}
	}

	return d.Path, d.Root, d.raw, parts
}

func (d *Document) Patch(ops []Op) (map[string][]byte, error) {
	if d.Root == nil {
		return nil, errors.New("document did not parse")
	}

	touched := map[string]*yaml.Node{}
	raws := map[string][]byte{}

	for _, op := range ops {
		parts := strings.Split(op.Path, ".")

		path, root, raw, rel := d.fileFor(parts)
		if _, ok := touched[path]; !ok {
			touched[path] = cloneNode(root)
			raws[path] = raw
		}

		root = touched[path]
		if op.Delete {
			if err := editPath(root, rel, nil, true); err != nil {
				return nil, fmt.Errorf("%s: %w", op.Path, err)
			}

			continue
		}

		node, err := toNode(op.Value)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", op.Path, err)
		}

		if err := editPath(root, rel, cloneNode(node), false); err != nil {
			return nil, fmt.Errorf("%s: %w", op.Path, err)
		}
	}

	out := map[string][]byte{}

	for path, root := range touched {
		var buf bytes.Buffer

		enc := yaml.NewEncoder(&buf)
		enc.SetIndent(2)

		if err := enc.Encode(root); err != nil {
			return nil, err
		}

		_ = enc.Close()
		data := buf.Bytes()

		var checked any
		if err := yaml.Unmarshal(data, &checked); err != nil {
			return nil, fmt.Errorf("edit would produce invalid YAML in %s: %w", path, err)
		}

		raw := raws[path]
		if bytes.HasPrefix(raw, []byte("\xef\xbb\xbf")) {
			data = append([]byte("\xef\xbb\xbf"), data...)
		}

		if bytes.Contains(raw, []byte("\r\n")) {
			data = bytes.ReplaceAll(data, []byte("\n"), []byte("\r\n"))
		}

		out[path] = data
	}

	return out, nil
}

// Write replaces path atomically when its current hash matches expectedHash.
var writeMu sync.Mutex

func Write(path, expectedHash string, data []byte) error {
	writeMu.Lock()
	defer writeMu.Unlock()

	current, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	if (err == nil && hashOf(current) != expectedHash) || (os.IsNotExist(err) && expectedHash != "") {
		return fmt.Errorf("%w: %s", ErrConflict, path)
	}

	mode := os.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), ".forge-write-*")
	if err != nil {
		return err
	}

	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()

		return err
	}

	if err := tmp.Sync(); err != nil {
		tmp.Close()

		return err
	}

	if err := tmp.Close(); err != nil {
		return err
	}

	if err := os.Chmod(tmpName, mode); err != nil {
		return err
	}

	if err := os.Rename(tmpName, path); err != nil {
		return err
	}

	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()

	return dir.Sync()
}
