// slots.go
package contract

import (
	"fmt"
	"strings"
)

// MaxSlotDepth is the maximum nesting depth of graph nodes; trees deeper
// than this are rejected at registration.
const MaxSlotDepth = 8

// Cardinality describes how many fills a slot accepts.
type Cardinality string

const (
	CardinalityOne  Cardinality = "one"
	CardinalityMany Cardinality = "many"
)

// SlotDef describes one slot of a parent intent kind.
type SlotDef struct {
	Accepts     []string // intent names accepted in this slot
	Cardinality Cardinality
	Extensible  bool // if true, other contributors may extend via Extends
}

// IntentKindDef declares the slots of a built-in intent kind.
type IntentKindDef struct {
	Slots map[string]SlotDef
}

// DefaultSlotCatalog is the v1 catalog of built-in intent kinds and their slots.
// Adding a new built-in intent kind here is a shell-version bump (adds new
// renderer behavior). Slice (e) defines the full v1 vocabulary; this map starts
// with the kinds used by the spec's example.
var DefaultSlotCatalog = map[string]IntentKindDef{
	"page.shell": {
		Slots: map[string]SlotDef{
			"main": {
				Accepts:     []string{"resource.list", "resource.detail", "dashboard.grid", "form.edit", "custom", "iframe"},
				Cardinality: CardinalityMany,
			},
		},
	},
	"resource.list": {
		Slots: map[string]SlotDef{
			"rowActions":   {Accepts: []string{"action.button", "action.menu", "action.divider"}, Cardinality: CardinalityMany},
			"detailDrawer": {Accepts: []string{"form.edit", "resource.detail", "custom"}, Cardinality: CardinalityOne, Extensible: true},
		},
	},
	"dashboard.grid": {
		Slots: map[string]SlotDef{
			"widgets": {Accepts: []string{"metric.counter", "metric.gauge", "audit.tail", "custom"}, Cardinality: CardinalityMany},
		},
	},
	"form.edit": {
		Slots: map[string]SlotDef{
			"fields": {Accepts: []string{"form.field", "custom"}, Cardinality: CardinalityMany, Extensible: true},
		},
	},
}

// validateSlotAccepts returns an error if child is not in slot's Accepts list.
func validateSlotAccepts(slot SlotDef, child string) error {
	for _, a := range slot.Accepts {
		if a == child {
			return nil
		}
	}
	return fmt.Errorf("slot does not accept intent %q", child)
}

// checkDepth recurses through GraphNodes and fails if any chain exceeds MaxSlotDepth.
func checkDepth(nodes []GraphNode, current int) error {
	if current > MaxSlotDepth {
		return fmt.Errorf("graph depth %d exceeds max %d", current, MaxSlotDepth)
	}
	for _, n := range nodes {
		for _, children := range n.Slots {
			if err := checkDepth(children, current+1); err != nil {
				return err
			}
		}
	}
	return nil
}

// checkCycle walks the graph and detects intent self-reference along a path.
// (Cross-intent cycles via custom escape hatches are bounded by checkDepth.)
func checkCycle(nodes []GraphNode, ancestors map[string]bool) error {
	for _, n := range nodes {
		if n.Intent == "" {
			continue
		}
		if ancestors[n.Intent] {
			return fmt.Errorf("cycle detected: intent %q nests itself", n.Intent)
		}
		ancestors[n.Intent] = true
		for _, children := range n.Slots {
			if err := checkCycle(children, ancestors); err != nil {
				return err
			}
		}
		delete(ancestors, n.Intent)
	}
	return nil
}

// validateGraphSlots checks that every slot fill in the graph satisfies the
// parent intent's slot catalog rules: known slot name, cardinality, and accept list.
func validateGraphSlots(nodes []GraphNode) error {
	for _, n := range nodes {
		def, ok := DefaultSlotCatalog[n.Intent]
		if !ok {
			// unknown parent intent: cannot validate slots, allow (may be a leaf or custom)
			continue
		}
		for slotName, children := range n.Slots {
			slot, ok := def.Slots[slotName]
			if !ok {
				return fmt.Errorf("intent %q has no slot %q", n.Intent, slotName)
			}
			if slot.Cardinality == CardinalityOne && len(children) > 1 {
				return fmt.Errorf("intent %q slot %q accepts one fill, got %d", n.Intent, slotName, len(children))
			}
			for _, c := range children {
				if err := validateSlotAccepts(slot, c.Intent); err != nil {
					return fmt.Errorf("intent %q slot %q: %w", n.Intent, slotName, err)
				}
			}
			if err := validateGraphSlots(children); err != nil {
				return err
			}
		}
	}
	return nil
}

// applyExtension finds the target node in graph and merges adds into the named slot.
// slotPath is a dotted path through Slots maps (and implicit first-fill traversal),
// e.g. "main.detailDrawer.fields". Returns an error if the target slot is not
// extensible or the path cannot be resolved.
func applyExtension(graph []GraphNode, target ExtensionTarget, slotPath string, adds []GraphNode) error {
	for i := range graph {
		if matchesTarget(graph[i], target) {
			return walkAndAppend(&graph[i], strings.Split(slotPath, "."), adds)
		}
	}
	return fmt.Errorf("extension target not found: contributor=%s intent=%s route=%s", target.Contributor, target.Intent, target.Route)
}

// matchesTarget reports whether a graph node is the host requested by the extension target.
// Intent must match; Route must match only if the target specifies one.
func matchesTarget(n GraphNode, t ExtensionTarget) bool {
	if n.Intent != t.Intent {
		return false
	}
	if t.Route != "" && n.Route != t.Route {
		return false
	}
	return true
}

// walkAndAppend descends the dotted slot path on n. The final segment names the
// slot to receive adds; intermediate segments name slots to descend into,
// always taking the first fill (cardinality:one assumption for traversal —
// indexed extension paths are out of scope for v1).
func walkAndAppend(n *GraphNode, path []string, adds []GraphNode) error {
	if len(path) == 0 {
		return fmt.Errorf("empty slot path")
	}
	slotName := path[0]
	rest := path[1:]
	if n.Slots == nil {
		n.Slots = map[string][]GraphNode{}
	}
	if len(rest) == 0 {
		// We've reached the target slot; check extensibility
		def, ok := DefaultSlotCatalog[n.Intent]
		if !ok {
			return fmt.Errorf("unknown parent intent %q", n.Intent)
		}
		slot, ok := def.Slots[slotName]
		if !ok {
			return fmt.Errorf("intent %q has no slot %q", n.Intent, slotName)
		}
		if !slot.Extensible {
			return fmt.Errorf("intent %q slot %q is not extensible", n.Intent, slotName)
		}
		for _, a := range adds {
			if err := validateSlotAccepts(slot, a.Intent); err != nil {
				return fmt.Errorf("extension into %q.%q: %w", n.Intent, slotName, err)
			}
		}
		n.Slots[slotName] = append(n.Slots[slotName], adds...)
		return nil
	}
	// Recurse: choose first fill in this slot.
	children, ok := n.Slots[slotName]
	if !ok || len(children) == 0 {
		return fmt.Errorf("path %q: no fills in slot %q to descend", strings.Join(path, "."), slotName)
	}
	return walkAndAppend(&children[0], rest, adds)
}

// deepCopyGraph returns a deep copy of a graph slice. Required so applying
// extensions doesn't mutate a contributor's original manifest.
func deepCopyGraph(in []GraphNode) []GraphNode {
	if in == nil {
		return nil
	}
	out := make([]GraphNode, len(in))
	for i, n := range in {
		nc := n
		if n.Slots != nil {
			nc.Slots = map[string][]GraphNode{}
			for k, v := range n.Slots {
				nc.Slots[k] = deepCopyGraph(v)
			}
		}
		out[i] = nc
	}
	return out
}
