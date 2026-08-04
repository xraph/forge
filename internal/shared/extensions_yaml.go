package shared

import (
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// The x-* specification extensions carried by Schema, Operation and
// AsyncAPIChannel are hoisted to the top level of their object by custom
// MarshalJSON/UnmarshalJSON methods. gopkg.in/yaml.v3 never consults those: it
// looks for MarshalYAML() (any, error) and UnmarshalYAML(*yaml.Node) error
// instead. The Extensions fields are tagged `yaml:"-"`, so before these helpers
// existed every extension in a YAML document was silently dropped in both
// directions -- and SpecParser.ParseFile accepts .yaml/.yml, so that was a real
// data-loss path, not a theoretical one.
//
// These two functions are the YAML counterparts of the merge/split logic in the
// JSON methods, factored out so the three types share one implementation rather
// than three copies that can drift apart.

// marshalYAMLWithExtensions renders base and hoists the x- prefixed entries of
// extensions onto the resulting mapping.
//
// base must be a value of a local alias type declared inside the calling
// MarshalYAML method: the alias sheds the method set, so encoding it here does
// not recurse back into MarshalYAML forever. Passing the alias rather than an
// enumerated field list also means a field added to the type later is carried
// automatically; a hand-written marshaller would drop it silently.
//
// With no extensions, base is returned untouched and yaml.v3 encodes it exactly
// as it did before this type had a MarshalYAML method at all. That matters:
// merging through a map would reorder every key, so every extension-free object
// in every emitted document would move, and a generated-spec drift check would
// fail on every CI run for no reason.
//
// With extensions, the encoded node keeps its original field order and the
// extensions are appended after it, sorted by key so repeated marshals of the
// same object are byte-identical (Go map iteration order is not stable).
func marshalYAMLWithExtensions(base any, extensions map[string]any) (any, error) {
	if len(extensions) == 0 {
		return base, nil
	}

	keys := make([]string, 0, len(extensions))

	for key := range extensions {
		// Only x- keys are hoisted. Without this guard a caller could put
		// "type" in the map and overwrite a real field of the object.
		if !strings.HasPrefix(key, "x-") {
			continue
		}

		keys = append(keys, key)
	}

	if len(keys) == 0 {
		return base, nil
	}

	sort.Strings(keys)

	node := &yaml.Node{}
	if err := node.Encode(base); err != nil {
		return nil, err
	}

	if node.Kind != yaml.MappingNode {
		return nil, fmt.Errorf(
			"shared: cannot hoist x- extensions onto a non-mapping YAML node (kind %d)", node.Kind)
	}

	for _, key := range keys {
		valueNode := &yaml.Node{}
		if err := valueNode.Encode(extensions[key]); err != nil {
			return nil, err
		}

		if existing := yamlMappingValue(node, key); existing != nil {
			*existing = *valueNode

			continue
		}

		keyNode := &yaml.Node{}
		if err := keyNode.Encode(key); err != nil {
			return nil, err
		}

		node.Content = append(node.Content, keyNode, valueNode)
	}

	return node, nil
}

// yamlMergeKey is YAML's merge key: `<<: *anchor` (or `<<: [*a, *b]`) splices
// another mapping's entries into this one.
const yamlMergeKey = "<<"

// unmarshalYAMLExtensions reads the x- prefixed keys back out of a mapping
// node. It returns nil when the node carries none, so an extension-free object
// keeps a nil Extensions map exactly as the JSON path leaves it.
//
// Merge keys are followed, because yaml.v3 follows them for every ordinary
// struct field and an object whose `type` is inherited but whose `x-forge-id`
// is not would be a trap rather than a limitation. Precedence matches what
// yaml.v3 itself does with the fields beside them, which is YAML's own rule: a
// key written directly on the object beats any merged one, and among the
// entries of a `<<` sequence the earlier one wins. Merge sources are visited
// recursively, so an anchor that itself merges another anchor resolves.
func unmarshalYAMLExtensions(value *yaml.Node) (map[string]any, error) {
	var (
		extensions map[string]any
		merged     []*yaml.Node
	)

	value = resolveYAMLAlias(value)

	if value == nil || value.Kind != yaml.MappingNode {
		return extensions, nil
	}

	for i := 0; i+1 < len(value.Content); i += 2 {
		key := value.Content[i].Value

		if key == yamlMergeKey {
			merged = append(merged, yamlMergeSources(value.Content[i+1])...)

			continue
		}

		if !strings.HasPrefix(key, "x-") {
			continue
		}

		var decoded any
		if err := value.Content[i+1].Decode(&decoded); err != nil {
			return nil, err
		}

		if extensions == nil {
			extensions = make(map[string]any)
		}

		extensions[key] = decoded
	}

	// Direct keys are all in hand, so anything a merge source contributes is by
	// definition not overriding one -- which is exactly the precedence rule.
	for _, source := range merged {
		inherited, err := unmarshalYAMLExtensions(source)
		if err != nil {
			return nil, err
		}

		for key, value := range inherited {
			if _, ok := extensions[key]; ok {
				continue
			}

			if extensions == nil {
				extensions = make(map[string]any)
			}

			extensions[key] = value
		}
	}

	return extensions, nil
}

// yamlMergeSources returns the mapping nodes a `<<` entry splices in, in
// precedence order. The value is normally an alias to a mapping, but YAML also
// allows a sequence of them (earliest wins) and, in hand-written documents, an
// inline mapping.
func yamlMergeSources(node *yaml.Node) []*yaml.Node {
	resolved := resolveYAMLAlias(node)
	if resolved == nil {
		return nil
	}

	if resolved.Kind == yaml.MappingNode {
		return []*yaml.Node{resolved}
	}

	if resolved.Kind != yaml.SequenceNode {
		return nil
	}

	sources := make([]*yaml.Node, 0, len(resolved.Content))

	for _, item := range resolved.Content {
		if mapping := resolveYAMLAlias(item); mapping != nil && mapping.Kind == yaml.MappingNode {
			sources = append(sources, mapping)
		}
	}

	return sources
}

// yamlMappingValue returns the value node stored under key, or nil. It exists so
// hoisting an extension replaces a same-named key rather than emitting a
// duplicate, matching the map-merge semantics of the JSON path. Struct fields
// never carry x- names, so in practice this only fires for a document that
// already spelled the extension out.
func yamlMappingValue(node *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}

	return nil
}

// resolveYAMLAlias follows an anchor reference (`*name`) to the node it points
// at, so a schema written as `order_number: *idprop` is read as the mapping the
// anchor holds. A non-alias node is returned unchanged.
//
// This only resolves a node that IS an alias. A merge key is an ordinary `<<`
// entry inside a mapping whose VALUE is an alias, and is handled separately by
// unmarshalYAMLExtensions; the loop here is for anchor chains.
func resolveYAMLAlias(node *yaml.Node) *yaml.Node {
	for node != nil && node.Kind == yaml.AliasNode {
		node = node.Alias
	}

	return node
}
