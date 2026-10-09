package resolve

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/xraph/forge/cmd/forge/internal/deploy/model"
)

// Overlay renders the YAML overlay for one service: its bindings as config
// keys and its outbound connections under services.<to>. Keys of the form
// a.b[name].c address a list item by its name field.
func Overlay(d *model.Deployment, s *model.Service) ([]byte, error) {
	root := map[string]any{}

	if s.RuntimeConfig != nil {
		raw, err := yaml.Marshal(s.RuntimeConfig)
		if err != nil {
			return nil, err
		}

		if err := yaml.Unmarshal(raw, &root); err != nil {
			return nil, err
		}
	}

	keys := []string{}
	flat := map[string]string{}

	for _, b := range s.Bindings {
		for k, v := range b.Keys {
			flat[k] = v
			keys = append(keys, k)
		}
	}

	sort.Strings(keys)

	for _, k := range keys {
		if err := set(root, k, flat[k]); err != nil {
			return nil, err
		}
	}

	for _, c := range d.Connections {
		if c.From != s.Name {
			continue
		}

		entry := map[string]any{"url": c.Address}
		if c.Address == "" {
			entry["url"] = "${" + c.EnvVar + "}"
		}

		if c.Timeout > 0 {
			entry["timeout"] = c.Timeout.String()
		}

		if err := set(root, c.ConfigKey, fmt.Sprint(entry["url"])); err != nil {
			return nil, err
		}

		prefix := strings.TrimSuffix(c.ConfigKey, ".url")
		if c.Timeout > 0 {
			if err := set(root, prefix+".timeout", c.Timeout.String()); err != nil {
				return nil, err
			}
		}

		if c.Retries > 0 {
			if err := set(root, prefix+".retry.attempts", strconv.Itoa(c.Retries)); err != nil {
				return nil, err
			}
		}
	}

	if _, ok := root["services"]; !ok {
		root["services"] = map[string]any{}
	}

	var buf strings.Builder

	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)

	if err := enc.Encode(orderedNode(root)); err != nil {
		return nil, err
	}

	return []byte(buf.String()), nil
}

// set writes value at a dotted path, where a segment "list[name]" selects or
// creates the item of list whose "name" field equals name.
func set(root map[string]any, path, value string) error {
	parts := strings.Split(path, ".")
	cur := root

	for i, part := range parts {
		last := i == len(parts)-1
		if open := strings.Index(part, "["); open > 0 && strings.HasSuffix(part, "]") {
			listKey, name := part[:open], part[open+1:len(part)-1]
			list, _ := cur[listKey].([]any)

			var item map[string]any

			for _, it := range list {
				if m, ok := it.(map[string]any); ok && m["name"] == name {
					item = m
				}
			}

			if item == nil {
				item = map[string]any{"name": name}
				list = append(list, item)
				cur[listKey] = list
			}

			if last {
				return fmt.Errorf("path %s ends on a list selector", path)
			}

			cur = item

			continue
		}

		if last {
			cur[part] = value

			return nil
		}

		next, ok := cur[part].(map[string]any)
		if !ok {
			next = map[string]any{}
			cur[part] = next
		}

		cur = next
	}

	return nil
}

func orderedNode(v any) *yaml.Node {
	n := &yaml.Node{}

	switch x := v.(type) {
	case map[string]any:
		n.Kind = yaml.MappingNode
		n.Tag = "!!map"
		keys := sortedKeys(x)
		sort.SliceStable(keys, func(i, j int) bool {
			rank := func(k string) int {
				switch k {
				case "name":
					return 0
				case "driver":
					return 1
				default:
					return 2
				}
			}
			if rank(keys[i]) != rank(keys[j]) {
				return rank(keys[i]) < rank(keys[j])
			}

			return keys[i] < keys[j]
		})

		for _, k := range keys {
			n.Content = append(n.Content, orderedNode(k), orderedNode(x[k]))
		}
	case []any:
		n.Kind = yaml.SequenceNode

		n.Tag = "!!seq"
		for _, item := range x {
			n.Content = append(n.Content, orderedNode(item))
		}
	default:
		_ = n.Encode(v)
	}

	return n
}
