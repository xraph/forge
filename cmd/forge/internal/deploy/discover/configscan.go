package discover

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/xraph/forge/cmd/forge/internal/deploy/catalog"
	"github.com/xraph/forge/cmd/forge/internal/deploy/model"
	"github.com/xraph/forge/cmd/forge/internal/deploy/spec"
)

// instance is one backend an app's config declares.
type instance struct {
	Extension string
	Name      string             // "default" when the extension has no list
	Type      model.ResourceType // "" when embedded
	Driver    string
	Source    string            // file:line
	Fields    map[string]string // raw string fields: grove_database, default_bucket, ...
}

// scanConfig reads one config file and returns the instances it declares,
// using the catalog to know where to look.
func scanConfig(root, path string, cat *catalog.Catalog) ([]instance, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}

	lines := spec.IndexLines(&doc)

	var tree map[string]any
	if err := yaml.Unmarshal(data, &tree); err != nil {
		return nil, err
	}

	rel, _ := filepath.Rel(root, path)
	src := func(dotted string) string {
		if l, ok := lines[dotted]; ok {
			return fmt.Sprintf("%s:%d", rel, l)
		}

		return rel
	}

	var out []instance

	for _, d := range cat.Descriptors {
		node, ok := lookup(tree, d.ConfigKey)
		if !ok {
			continue
		}

		m, _ := node.(map[string]any)
		if d.Instances != nil && d.Instances.Path != "" {
			if list, ok := lookup(m, d.Instances.Path); ok {
				items, valid := list.([]any)
				if !valid {
					return nil, fmt.Errorf("%s: %s.%s must be a list", rel, d.ConfigKey, d.Instances.Path)
				}

				for i, item := range items {
					im, valid := item.(map[string]any)
					if !valid {
						return nil, fmt.Errorf("%s: %s instance %d must be a mapping", rel, d.ConfigKey, i)
					}

					name, _ := im[d.Instances.Name].(string)
					if name == "" {
						return nil, fmt.Errorf("%s: %s instance %d requires name", rel, d.ConfigKey, i)
					}

					drv := firstString(im, "driver", "type", "storage_driver")

					typ, known := d.KindOf(drv)
					if !known {
						return nil, fmt.Errorf("%s: %s instance uses an unknown driver", rel, d.ConfigKey)
					}

					out = append(out, instance{
						Extension: d.Extension, Name: name, Type: typ, Driver: drv,
						Source: src(fmt.Sprintf("%s.%s.%d", d.ConfigKey, d.Instances.Path, i)),
						Fields: stringFields(im),
					})
				}

				continue
			}
		}

		if d.Instances != nil {
			drv := firstString(m, d.Instances.DefaultFrom...)
			if drv == "" {
				continue
			}

			typ, known := d.KindOf(drv)
			if !known {
				return nil, fmt.Errorf("%s: %s instance uses an unknown driver", rel, d.ConfigKey)
			}

			out = append(out, instance{
				Extension: d.Extension, Name: "default", Type: typ, Driver: drv,
				Source: src(d.ConfigKey), Fields: stringFields(m),
			})

			continue
		}
		// No instances: the extension is present, record it so requires can bind.
		out = append(out, instance{Extension: d.Extension, Name: "", Source: src(d.ConfigKey), Fields: stringFields(m)})
	}

	return out, nil
}

func lookup(tree any, dotted string) (any, bool) {
	cur := tree
	for part := range strings.SplitSeq(dotted, ".") {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}

		cur, ok = m[part]
		if !ok {
			return nil, false
		}
	}

	return cur, true
}

func firstString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k].(string); ok && v != "" {
			return v
		}
	}

	return ""
}

func stringFields(m map[string]any) map[string]string {
	out := map[string]string{}

	for k, v := range m {
		if s, ok := v.(string); ok {
			out[k] = s
		}
	}

	return out
}

// ConfigValue reads the highest precedence scalar without exposing an entire secret-bearing config.
func ConfigValue(app App, dotted string) (string, bool) {
	for _, p := range app.ConfigPaths {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}

		var tree map[string]any
		if yaml.Unmarshal(data, &tree) != nil {
			continue
		}

		v, ok := lookup(tree, dotted)
		if value, isString := v.(string); ok && isString {
			return value, true
		}
	}

	return "", false
}

// NamedInstance reports list presence separately from an item named default.
func NamedInstance(app App, key, list, name string) (bool, bool) {
	for _, p := range app.ConfigPaths {
		raw, err := os.ReadFile(p)
		if err != nil {
			continue
		}

		var tree map[string]any
		if yaml.Unmarshal(raw, &tree) != nil {
			continue
		}

		v, ok := lookup(tree, key+"."+list)
		if !ok {
			continue
		}

		items, ok := v.([]any)
		if !ok {
			return false, true
		}

		for _, item := range items {
			if m, ok := item.(map[string]any); ok && m["name"] == name {
				return true, true
			}
		}

		return false, true
	}

	return false, false
}
