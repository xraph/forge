package spec

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/xraph/forge/cmd/forge/internal/deploy/output"
)

type Document struct {
	Path      string
	Root      *yaml.Node
	Deploy    *Deploy
	Hash      string
	Splits    map[string]*Split
	IsV1      bool
	raw       []byte
	lines     map[string]int
	overrides map[string][]byte
	sources   map[string]sourceLocation
}

type sourceLocation struct {
	file string
	line int
}

type Split struct {
	Path string
	Root *yaml.Node
	Hash string
	raw  []byte
}

func Locate(root string) (string, output.Diagnostics, error) {
	yml := filepath.Join(root, ".forge.yml")
	yaml_ := filepath.Join(root, ".forge.yaml")
	_, errA := os.Stat(yml)

	_, errB := os.Stat(yaml_)
	for _, err := range []error{errA, errB} {
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return "", nil, err
		}
	}

	switch {
	case errA == nil && errB == nil:
		d := output.Diagnostic{Code: output.CodeConfigAmbiguous, Severity: output.SeverityError,
			Message: "both .forge.yml and .forge.yaml exist in " + root, Fix: "keep one of them"}

		return "", output.Diagnostics{d}, errors.New(d.Message)
	case errA == nil:
		return yml, nil, nil
	case errB == nil:
		return yaml_, nil, nil
	}

	return "", nil, os.ErrNotExist
}

func hashOf(b []byte) string {
	s := sha256.Sum256(b)

	return hex.EncodeToString(s[:])
}

var yamlLine = regexp.MustCompile(`line (\d+):`)

func parseError(path string, err error) output.Diagnostic {
	d := output.Diagnostic{Code: output.CodeConfigInvalid, Severity: output.SeverityError, Message: err.Error(), File: path}
	if m := yamlLine.FindStringSubmatch(err.Error()); m != nil {
		d.Line, _ = strconv.Atoi(m[1])
	}

	return d
}

func Parse(path string) (*Document, output.Diagnostics, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}

	return ParseData(path, raw)
}

func ParseData(path string, raw []byte) (*Document, output.Diagnostics, error) {
	return ParseProposal(path, raw, nil)
}
func ParseProposal(path string, raw []byte, files map[string][]byte) (*Document, output.Diagnostics, error) {
	doc := &Document{Path: path, Hash: hashOf(raw), raw: raw, Splits: map[string]*Split{}, sources: map[string]sourceLocation{}, overrides: files}

	var root yaml.Node
	if err := yaml.Unmarshal(bytes.TrimPrefix(raw, []byte("\xef\xbb\xbf")), &root); err != nil {
		return doc, output.Diagnostics{parseError(path, err)}, nil
	}

	doc.Root = &root
	// Node decoding alone accepts duplicate keys and recursive aliases. Decode
	// the complete envelope before choosing a section so intent is unambiguous.
	var envelope any
	if err := root.Decode(&envelope); err != nil {
		return doc, output.Diagnostics{parseError(path, err)}, nil
	}

	doc.lines = IndexLines(&root)

	deployNode := childNode(&root, "deploy")
	if deployNode == nil {
		return doc, nil, nil
	}

	if childNode(deployNode, "version") == nil {
		doc.IsV1 = true

		return doc, nil, nil
	}

	var diags output.Diagnostics

	diags = append(diags, unknownKeys(deployNode, reflect.TypeFor[Deploy](), "deploy", path)...)

	var d Deploy
	if err := deployNode.Decode(&d); err != nil {
		return doc, append(diags, parseError(path, err)), nil
	}

	doc.Deploy = &d
	if d.Spec != "" {
		sd := doc.loadSplit(filepath.Join(filepath.Dir(path), d.Spec), []string{"services", "resources", "connections"}, deployNode)
		diags = append(diags, sd...)
	}

	for env, rel := range d.EnvironmentFiles {
		sp := filepath.Join(filepath.Dir(path), rel)
		sd := doc.loadEnvSplit(sp, env, deployNode)
		diags = append(diags, sd...)
	}

	return doc, diags, nil
}

// childNode returns the value node for key in a mapping (or the document root).
func childNode(n *yaml.Node, key string) *yaml.Node {
	if n.Kind == yaml.DocumentNode && len(n.Content) > 0 {
		n = n.Content[0]
	}

	if n.Kind != yaml.MappingNode {
		return nil
	}

	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return n.Content[i+1]
		}
	}

	return nil
}

func (doc *Document) loadSplit(path string, keys []string, deployNode *yaml.Node) output.Diagnostics {
	if err := withinProject(filepath.Dir(doc.Path), path); err != nil {
		return output.Diagnostics{{Code: output.CodeConfigInvalid, Severity: output.SeverityError, Message: err.Error(), File: doc.Path, Field: "deploy.spec"}}
	}

	raw, err := doc.readFile(path)
	if err != nil {
		return output.Diagnostics{{Code: output.CodeConfigInvalid, Severity: output.SeverityError, Message: err.Error(), File: doc.Path, Field: "deploy.spec", Line: doc.Line("deploy.spec")}}
	}

	var root yaml.Node
	if err := yaml.Unmarshal(raw, &root); err != nil {
		return output.Diagnostics{parseError(path, err)}
	}

	var diags output.Diagnostics

	for _, k := range keys {
		if childNode(deployNode, k) != nil {
			diags = append(diags, output.Diagnostic{Code: output.CodeSpecKeyConflict, Severity: output.SeverityError,
				Message: fmt.Sprintf("deploy.%s is set inline and in %s", k, path), File: doc.Path, Field: "deploy." + k, Line: doc.Line("deploy." + k),
				Fix: "keep it in one place"})
		}
	}

	type stack struct {
		Services    map[string]Service  `yaml:"services"`
		Resources   map[string]Resource `yaml:"resources"`
		Connections []Connection        `yaml:"connections"`
	}

	diags = append(diags, unknownKeys(&root, reflect.TypeFor[stack](), "", path)...)

	var s stack
	if err := root.Decode(&s); err != nil {
		return append(diags, parseError(path, err))
	}

	if doc.Deploy.Services == nil {
		doc.Deploy.Services = s.Services
	}

	if doc.Deploy.Resources == nil {
		doc.Deploy.Resources = s.Resources
	}

	if doc.Deploy.Connections == nil {
		doc.Deploy.Connections = s.Connections
	}

	doc.Splits[path] = &Split{Path: path, Root: &root, Hash: hashOf(raw), raw: raw}
	for field, line := range IndexLines(&root) {
		doc.sources["deploy."+field] = sourceLocation{file: path, line: line}
	}

	return diags
}

func (doc *Document) loadEnvSplit(path, env string, deployNode *yaml.Node) output.Diagnostics {
	if err := withinProject(filepath.Dir(doc.Path), path); err != nil {
		return output.Diagnostics{{Code: output.CodeConfigInvalid, Severity: output.SeverityError, Message: err.Error(), File: doc.Path, Field: "deploy.environment_files." + env}}
	}

	raw, err := doc.readFile(path)
	if err != nil {
		return output.Diagnostics{{Code: output.CodeConfigInvalid, Severity: output.SeverityError, Message: err.Error(), File: doc.Path, Field: "deploy.environment_files." + env}}
	}

	var root yaml.Node
	if err := yaml.Unmarshal(raw, &root); err != nil {
		return output.Diagnostics{parseError(path, err)}
	}

	var diags output.Diagnostics
	if envs := childNode(deployNode, "environments"); envs != nil && childNode(envs, env) != nil {
		diags = append(diags, output.Diagnostic{Code: output.CodeSpecKeyConflict, Severity: output.SeverityError,
			Message: fmt.Sprintf("environment %s is set inline and in %s", env, path), File: doc.Path, Field: "deploy.environments." + env})
	}

	diags = append(diags, unknownKeys(&root, reflect.TypeFor[Environment](), "", path)...)

	var e Environment
	if err := root.Decode(&e); err != nil {
		return append(diags, parseError(path, err))
	}

	if doc.Deploy.Environments == nil {
		doc.Deploy.Environments = map[string]Environment{}
	}

	doc.Deploy.Environments[env] = e
	doc.Splits[path] = &Split{Path: path, Root: &root, Hash: hashOf(raw), raw: raw}
	prefix := "deploy.environments." + env

	doc.sources[prefix] = sourceLocation{file: path, line: 1}
	for field, line := range IndexLines(&root) {
		doc.sources[prefix+"."+field] = sourceLocation{file: path, line: line}
	}

	return diags
}

// unknownKeys walks a mapping node against a struct type and reports keys
// no yaml tag declares. A struct with an inline map accepts any key.
func unknownKeys(n *yaml.Node, t reflect.Type, prefix, file string) output.Diagnostics {
	return walkUnknownKeys(n, t, prefix, file, map[*yaml.Node]bool{})
}

func walkUnknownKeys(n *yaml.Node, t reflect.Type, prefix, file string, ancestors map[*yaml.Node]bool) output.Diagnostics {
	if n == nil {
		return nil
	}

	if ancestors[n] {
		return output.Diagnostics{{Code: output.CodeConfigInvalid, Severity: output.SeverityError, Message: "recursive YAML alias", File: file, Line: n.Line, Field: prefix}}
	}

	ancestors[n] = true
	defer delete(ancestors, n)

	if n.Kind == yaml.AliasNode {
		return walkUnknownKeys(n.Alias, t, prefix, file, ancestors)
	}

	if n.Kind == yaml.DocumentNode && len(n.Content) > 0 {
		n = n.Content[0]
	}

	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}

	var diags output.Diagnostics

	switch t.Kind() {
	case reflect.Struct:
		if n.Kind != yaml.MappingNode {
			return nil
		}

		fields := map[string]reflect.Type{}
		inline := false

		for f := range t.Fields() {
			tag := f.Tag.Get("yaml")

			name, _, _ := strings.Cut(tag, ",")
			if strings.Contains(tag, ",inline") {
				inline = true

				continue
			}

			if name == "" {
				name = strings.ToLower(f.Name)
			}

			if name == "-" {
				continue
			}

			fields[name] = f.Type
		}

		for i := 0; i+1 < len(n.Content); i += 2 {
			k, v := n.Content[i], n.Content[i+1]
			if k.Tag == "!!merge" {
				if v.Kind == yaml.SequenceNode {
					for _, item := range v.Content {
						diags = append(diags, walkUnknownKeys(item, t, prefix, file, ancestors)...)
					}
				} else {
					diags = append(diags, walkUnknownKeys(v, t, prefix, file, ancestors)...)
				}

				continue
			}

			path := k.Value
			if prefix != "" {
				path = prefix + "." + k.Value
			}

			ft, ok := fields[k.Value]
			if !ok {
				if !inline {
					diags = append(diags, output.Diagnostic{Code: output.CodeUnknownKey, Severity: output.SeverityError,
						Message: fmt.Sprintf("unknown key %q", k.Value), File: file, Line: k.Line, Field: path, Fix: "remove it or check the spelling against forge deploy schema"})
				}

				continue
			}

			diags = append(diags, walkUnknownKeys(v, ft, path, file, ancestors)...)
		}
	case reflect.Map:
		if n.Kind != yaml.MappingNode {
			return nil
		}

		for i := 0; i+1 < len(n.Content); i += 2 {
			k, v := n.Content[i], n.Content[i+1]
			if k.Tag == "!!merge" {
				diags = append(diags, walkUnknownKeys(v, t, prefix, file, ancestors)...)

				continue
			}

			diags = append(diags, walkUnknownKeys(v, t.Elem(), prefix+"."+k.Value, file, ancestors)...)
		}
	case reflect.Slice:
		if n.Kind != yaml.SequenceNode {
			return nil
		}

		for i, item := range n.Content {
			diags = append(diags, walkUnknownKeys(item, t.Elem(), fmt.Sprintf("%s.%d", prefix, i), file, ancestors)...)
		}
	}

	return diags
}

// withinProject checks both the declared path and its symlink destination.
func withinProject(root, path string) error {
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}

	realPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		return err
	}

	rel, err := filepath.Rel(realRoot, realPath)
	if err != nil {
		return err
	}

	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return fmt.Errorf("configuration path is outside project: %s", path)
	}

	return nil
}

// RawBytes returns a copy of the original file for diffs and backups.
func (d *Document) RawBytes() []byte { return bytes.Clone(d.raw) }

func (doc *Document) readFile(path string) ([]byte, error) {
	if raw, ok := doc.overrides[path]; ok {
		return bytes.Clone(raw), nil
	}

	r, err := os.OpenRoot(filepath.Dir(doc.Path))
	if err != nil {
		return nil, err
	}
	defer r.Close()

	rel, err := filepath.Rel(filepath.Dir(doc.Path), path)
	if err != nil || !filepath.IsLocal(rel) {
		return nil, errors.New("configuration path is outside project")
	}

	return r.ReadFile(rel)
}

func (s *Split) RawBytes() []byte { return bytes.Clone(s.raw) }
