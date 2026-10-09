package spec

import (
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// IndexLines maps dotted paths to the line of the node that holds the value.
// Sequence items are addressed by index: "deploy.services.api.bindings.0".
func IndexLines(root *yaml.Node) map[string]int {
	out := map[string]int{}

	node := root
	if node.Kind == yaml.DocumentNode && len(node.Content) > 0 {
		node = node.Content[0]
	}

	indexNode(node, "", out, map[*yaml.Node]bool{})

	return out
}

func indexNode(n *yaml.Node, prefix string, out map[string]int, ancestors map[*yaml.Node]bool) {
	if n == nil || ancestors[n] || len(out) > 100000 {
		return
	}

	ancestors[n] = true
	defer delete(ancestors, n)

	switch n.Kind {
	case yaml.MappingNode:
		for i := 0; i+1 < len(n.Content); i += 2 {
			k, v := n.Content[i], n.Content[i+1]

			path := k.Value
			if prefix != "" {
				path = prefix + "." + k.Value
			}

			out[path] = k.Line
			indexNode(v, path, out, ancestors)
		}
	case yaml.SequenceNode:
		for i, item := range n.Content {
			path := prefix + "." + strconv.Itoa(i)
			out[path] = item.Line
			indexNode(item, path, out, ancestors)
		}
	case yaml.AliasNode:
		if n.Alias != nil {
			indexNode(n.Alias, prefix, out, ancestors)
		}
	}
}

// Source returns the location of a field, including fields loaded from split files.
func (d *Document) Source(field string) (string, int) {
	for candidate := field; candidate != ""; {
		if source, ok := d.sources[candidate]; ok {
			return source.file, source.line
		}

		pos := strings.LastIndex(candidate, ".")
		if pos < 0 {
			break
		}

		candidate = candidate[:pos]
	}

	return d.Path, d.Line(field)
}

// Line returns the line for a dotted field in the main document, or 0.
func (d *Document) Line(field string) int {
	if d == nil || d.lines == nil {
		return 0
	}

	for field != "" {
		if line := d.lines[field]; line != 0 {
			return line
		}

		pos := strings.LastIndex(field, ".")
		if pos < 0 {
			break
		}

		field = field[:pos]
	}

	return 0
}
