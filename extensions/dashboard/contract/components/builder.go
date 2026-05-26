package components

import (
	"encoding/json"

	"github.com/xraph/forge/extensions/dashboard/contract"
)

// Common carries the metadata fields every component shares — title, op,
// children, slots, predicates, data binding, etc. — and the fluent setters
// for them.
//
// The Self type parameter is a self-reference: each component builder
// embeds *Common[XBuilder] and wires up the Self pointer at construction
// time, so common setters return the concrete *XBuilder and chains stay
// typed across both common and component-specific methods.
type Common[Self any] struct {
	// Self is the concrete builder pointer. Common setters return it so
	// fluent chains preserve the typed builder.
	Self *Self

	intent      string
	title       string
	op          string
	route       string
	component   string
	children    []contract.GraphNode
	slots       map[string][]contract.GraphNode
	visible     *contract.Predicate
	enabled     *contract.Predicate
	data        *contract.DataBinding
	payload     map[string]contract.ParamSource
	nav         *contract.NavConfig
	src         string
	sandbox     []string
	protocol    string
	root        bool
	childrenKey string // slot name children are routed to; defaults to "children"
}

func newCommon[Self any](intent string, self *Self) *Common[Self] {
	return &Common[Self]{
		intent:      intent,
		Self:        self,
		childrenKey: "children",
	}
}

// Title sets the human-readable label used by layout chrome (page titles,
// card headers, sidebar nav).
func (c *Common[Self]) Title(t string) *Self { c.title = t; return c.Self }

// Op sets the command intent invoked when the node is a button or form
// submit. Mirrors GraphNode.Op.
func (c *Common[Self]) Op(op string) *Self { c.op = op; return c.Self }

// Route declares a top-level route. Only meaningful for nodes registered
// at the root of a contributor's graph.
func (c *Common[Self]) Route(r string) *Self { c.route = r; return c.Self }

// Component sets the escape-hatch React component name for nodes the
// contract can't express via typed props. Use sparingly.
func (c *Common[Self]) Component(name string) *Self { c.component = name; return c.Self }

// Src points an iframe-rendered node at an external URL.
func (c *Common[Self]) Src(s string) *Self { c.src = s; return c.Self }

// Sandbox sets iframe sandbox flags.
func (c *Common[Self]) Sandbox(flags ...string) *Self { c.sandbox = flags; return c.Self }

// Protocol declares an iframe message protocol.
func (c *Common[Self]) Protocol(p string) *Self { c.protocol = p; return c.Self }

// Root marks this node as the contributor's root page.
func (c *Common[Self]) Root(r bool) *Self { c.root = r; return c.Self }

// Children appends to the default children slot. The slot name depends on
// the component — for most layout containers it is "children"; for
// specialized components like Card it is "content".
func (c *Common[Self]) Children(nodes ...contract.GraphNode) *Self {
	c.children = append(c.children, nodes...)
	return c.Self
}

// Child appends a single child to the default children slot.
func (c *Common[Self]) Child(node contract.GraphNode) *Self {
	c.children = append(c.children, node)
	return c.Self
}

// Slot appends nodes to a named slot. Use this for slots that are not the
// default children slot (e.g., `actions`, `header`, `footer`, `trigger`).
func (c *Common[Self]) Slot(name string, nodes ...contract.GraphNode) *Self {
	if c.slots == nil {
		c.slots = map[string][]contract.GraphNode{}
	}
	c.slots[name] = append(c.slots[name], nodes...)
	return c.Self
}

// VisibleWhen sets the access predicate controlling node visibility.
func (c *Common[Self]) VisibleWhen(p *contract.Predicate) *Self { c.visible = p; return c.Self }

// EnabledWhen sets the access predicate controlling whether the node is
// interactive.
func (c *Common[Self]) EnabledWhen(p *contract.Predicate) *Self { c.enabled = p; return c.Self }

// Data binds the node to a data source — either a named query reference
// or an inline {intent, params} pair.
func (c *Common[Self]) Data(d *contract.DataBinding) *Self { c.data = d; return c.Self }

// Payload sets the parameter sources used when invoking Op.
func (c *Common[Self]) Payload(p map[string]contract.ParamSource) *Self {
	c.payload = p
	return c.Self
}

// Nav configures the per-route nav metadata.
func (c *Common[Self]) Nav(n *contract.NavConfig) *Self { c.nav = n; return c.Self }

// setChildrenKey overrides the slot name children are routed to. Used by
// components like Card where the default child slot is "content" rather
// than "children".
func (c *Common[Self]) setChildrenKey(key string) { c.childrenKey = key }

// finalize assembles the final slots map by merging children into the
// configured children slot.
func (c *Common[Self]) finalize() map[string][]contract.GraphNode {
	if len(c.children) == 0 && len(c.slots) == 0 {
		return nil
	}
	out := map[string][]contract.GraphNode{}
	for k, v := range c.slots {
		out[k] = v
	}
	if len(c.children) > 0 {
		key := c.childrenKey
		if key == "" {
			key = "children"
		}
		out[key] = append(out[key], c.children...)
	}
	return out
}

// toNode constructs the final GraphNode from common metadata plus
// JSON-encoded typed props.
func (c *Common[Self]) toNode(props any) contract.GraphNode {
	return contract.GraphNode{
		Route:       c.route,
		Intent:      c.intent,
		Title:       c.title,
		Nav:         c.nav,
		Root:        c.root,
		Data:        c.data,
		Props:       structToMap(props),
		Slots:       c.finalize(),
		VisibleWhen: c.visible,
		EnabledWhen: c.enabled,
		Op:          c.op,
		Payload:     c.payload,
		Component:   c.component,
		Src:         c.src,
		Sandbox:     c.sandbox,
		Protocol:    c.protocol,
	}
}

// structToMap converts a typed props struct to map[string]any via a JSON
// round-trip so the result honors json tags (omitempty, custom names) and
// matches what the React renderer receives.
func structToMap(v any) map[string]any {
	if v == nil {
		return nil
	}
	raw, err := json.Marshal(v)
	if err != nil || string(raw) == "null" {
		return nil
	}
	out := map[string]any{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
