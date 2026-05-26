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

// broadContentIntents is the universal "any layout child" Accepts list used by
// generic layout containers (layout.card.content, layout.section.content,
// layout.stack.children, etc.) and other slots that can host arbitrary
// renderable content. It includes every renderable intent kind so layout
// primitives can host anything a manifest author wants to compose.
var broadContentIntents = []string{
	// Layout containers (recursable)
	"layout.section", "layout.card", "layout.tabs", "layout.accordion",
	"layout.stack", "layout.row", "layout.column", "layout.grid",
	"layout.container", "layout.split", "layout.page", "layout.divider", "layout.spacer",
	// Domain composites
	"resource.list", "resource.detail", "form.edit", "dashboard.grid", "audit.tail",
	// Atom primitives
	"atom.avatar", "atom.badge", "atom.button", "atom.checkbox", "atom.code",
	"atom.heading", "atom.icon", "atom.icon-button", "atom.image",
	"atom.input", "atom.kbd", "atom.label", "atom.link", "atom.progress",
	"atom.radio", "atom.select", "atom.separator", "atom.skeleton",
	"atom.slider", "atom.spinner", "atom.switch", "atom.text", "atom.textarea", "atom.tooltip",
	// Molecule composites
	"molecule.alert", "molecule.breadcrumb", "molecule.chip", "molecule.combobox",
	"molecule.command-palette", "molecule.date-picker", "molecule.dropdown-menu",
	"molecule.empty-state", "molecule.error-state", "molecule.field",
	"molecule.file-upload", "molecule.list-item", "molecule.loading-state",
	"molecule.pagination", "molecule.rating", "molecule.search-bar",
	"molecule.stat-card", "molecule.tag-input",
	// Organism composites
	"organism.calendar", "organism.chart", "organism.code-editor", "organism.comments",
	"organism.data-grid", "organism.dynamic-form", "organism.kanban",
	"organism.notification-center", "organism.stepper", "organism.timeline", "organism.tree-view",
	// Detail composition leaves
	"detail.header", "detail.section",
	// Editor leaves
	"editor.code", "editor.formBuilder",
	// Dashboard widget leaves
	"metric.counter", "metric.gauge", "dashboard.stat", "dashboard.recentlist",
	// Settings UI leaves
	"settings.tabs", "settings.panel",
	// Affordances
	"confirm.dialog",
	// Escape hatches
	"custom", "iframe",
}

// inlineAdornmentIntents is the small subset used by slots that take only
// inline/leaf adornment (layout.card.header, layout.card.footer,
// layout.page.header, layout.page.footer, page.shell.header).
var inlineAdornmentIntents = []string{
	"atom.heading", "atom.text", "atom.badge", "atom.icon", "atom.button",
	"molecule.breadcrumb", "molecule.alert",
	"action.button", "action.menu", "action.divider",
	"layout.section", "layout.row",
	"custom",
}

// formFieldIntents is the Accepts list for form.edit.fields and other slots
// that take field-shaped children (form-state-aware atoms + molecules).
var formFieldIntents = []string{
	"form.field",
	"atom.input", "atom.textarea", "atom.checkbox", "atom.radio",
	"atom.switch", "atom.slider", "atom.select",
	"molecule.field", "molecule.combobox", "molecule.date-picker",
	"molecule.file-upload", "molecule.tag-input",
	"field.json", "field.permissions",
	"custom",
}

// pageMainIntents is the Accepts list for page.shell.main. It's the union of
// domain composites, layout primitives, page-level molecules, organism
// wrappers, auth forms, editors, and escape hatches — every intent kind a
// route can host as top-level content.
var pageMainIntents = []string{
	// Existing core
	"resource.list", "resource.detail", "dashboard.grid", "form.edit",
	"audit.tail", "custom", "iframe",
	// Layout primitives at page level
	"layout.page", "layout.section", "layout.tabs", "layout.accordion", "layout.card",
	"layout.stack", "layout.grid", "layout.row", "layout.column", "layout.container", "layout.split",
	// Page-level molecules
	"molecule.alert", "molecule.empty-state", "molecule.error-state", "molecule.loading-state",
	"molecule.command-palette", "molecule.breadcrumb",
	// Organism-level wrappers usable as top-level content
	"organism.data-grid", "organism.dynamic-form", "organism.calendar", "organism.timeline",
	"organism.kanban", "organism.code-editor", "organism.tree-view", "organism.stepper", "organism.chart",
	"organism.notification-center", "organism.comments",
	// Auth pages — `auth.login.form` is the v1 vocabulary intent; the
	// kebab-case `auth.*-form` variants are composites that can also be
	// used as full pages or inside auth.tabs.
	"auth.login.form",
	"auth.signin-form", "auth.signup-form", "auth.tabs",
	"auth.magic-link-form", "auth.oauth-buttons",
	"auth.forgot-password-form", "auth.reset-password-form",
	"auth.setup-form", "auth.dynamic-signup-form",
	"auth.two-factor-setup", "auth.passkey-prompt", "auth.session-list",
	"auth.user-button", "auth.account-menu", "auth.org-switcher", "auth.org-profile",
	// Editors
	"editor.code", "editor.formBuilder",
	// Settings UI
	"settings.tabs", "settings.panel",
}

// DefaultSlotCatalog is the v1 catalog of built-in intent kinds and their slots.
// Adding a new built-in intent kind here is a shell-version bump (adds new
// renderer behavior). The catalog mirrors the React renderer registry in
// /shell/src/intents/register.ts — every intent kind exposed to manifest
// authors should appear in both places.
var DefaultSlotCatalog = map[string]IntentKindDef{
	"page.shell": {
		Slots: map[string]SlotDef{
			"header": {
				Accepts:     inlineAdornmentIntents,
				Cardinality: CardinalityOne,
			},
			"main": {
				Accepts:     pageMainIntents,
				Cardinality: CardinalityMany,
			},
		},
	},
	"resource.list": {
		Slots: map[string]SlotDef{
			"rowActions":   {Accepts: []string{"action.button", "action.menu", "action.divider"}, Cardinality: CardinalityMany},
			"detailDrawer": {Accepts: []string{"form.edit", "resource.detail", "custom"}, Cardinality: CardinalityOne, Extensible: true},
			"filters": {
				Accepts: []string{
					"filter.search", "filter.select", "filter.date",
					"molecule.search-bar", "molecule.combobox", "molecule.date-picker",
					"custom",
				},
				Cardinality: CardinalityMany,
				Extensible:  true,
			},
			"bulkActions": {
				Accepts:     []string{"action.button", "action.menu"},
				Cardinality: CardinalityMany,
				Extensible:  true,
			},
			"header": {
				Accepts:     inlineAdornmentIntents,
				Cardinality: CardinalityOne,
			},
			"pagination": {
				Accepts:     []string{"molecule.pagination", "pagination.cursor", "pagination.offset"},
				Cardinality: CardinalityOne,
			},
		},
	},
	// resource.detail was previously unregistered (effectively a leaf). It now
	// composes a rich detail page from four slots: a record-bound header, a
	// vertical list of sections (each typically a detail.section or layout.card
	// containing nested resource.list/form.edit children), an entity-scoped
	// actions row, and a plugin-extensible extensions slot. Manifests that
	// don't set any slots get the renderer's bare definition-list fallback.
	"resource.detail": {
		Slots: map[string]SlotDef{
			"header": {
				Accepts:     []string{"detail.header", "layout.section", "atom.heading", "molecule.breadcrumb", "custom"},
				Cardinality: CardinalityOne,
			},
			"sections": {
				Accepts: []string{
					"detail.section", "layout.section", "layout.card", "layout.tabs", "layout.accordion",
					"resource.list", "resource.detail", "form.edit",
					"organism.data-grid", "organism.dynamic-form", "organism.timeline", "organism.chart",
					"molecule.alert", "molecule.empty-state",
					"audit.tail", "custom",
				},
				Cardinality: CardinalityMany,
				Extensible:  true,
			},
			"actions": {
				Accepts:     []string{"action.button", "action.menu", "action.divider", "atom.button", "confirm.dialog"},
				Cardinality: CardinalityMany,
				Extensible:  true,
			},
			"extensions": {
				Accepts: []string{
					"detail.section", "layout.section", "layout.card",
					"resource.list", "resource.detail", "form.edit",
					"custom",
				},
				Cardinality: CardinalityMany,
				Extensible:  true,
			},
		},
	},
	"dashboard.grid": {
		Slots: map[string]SlotDef{
			"widgets": {
				Accepts: []string{
					"metric.counter", "metric.gauge", "audit.tail",
					"dashboard.stat", "dashboard.recentlist",
					"molecule.stat-card",
					"organism.chart", "organism.timeline", "organism.calendar",
					"custom",
				},
				Cardinality: CardinalityMany,
				Extensible:  true,
			},
		},
	},
	"form.edit": {
		Slots: map[string]SlotDef{
			"fields": {Accepts: formFieldIntents, Cardinality: CardinalityMany, Extensible: true},
			"actions": {
				Accepts:     []string{"action.button", "atom.button"},
				Cardinality: CardinalityMany,
				Extensible:  true,
			},
			"sections": {
				Accepts:     []string{"layout.section", "layout.card"},
				Cardinality: CardinalityMany,
			},
		},
	},

	// Auth pages and composites. `auth.login.form` is the v1 vocabulary leaf
	// intent. The kebab-case `auth.*-form` variants are React composites the
	// user added separately; we register them in the catalog as leaves so
	// the validator knows about them. `auth.forgot-password-form`,
	// `auth.setup-form`, and `auth.dynamic-signup-form` are new for the
	// authsome migration.
	"auth.login.form":           {Slots: map[string]SlotDef{}},
	"auth.signin-form":          {Slots: map[string]SlotDef{}},
	"auth.signup-form":          {Slots: map[string]SlotDef{}},
	"auth.magic-link-form":      {Slots: map[string]SlotDef{}},
	"auth.oauth-buttons":        {Slots: map[string]SlotDef{}},
	"auth.forgot-password-form": {Slots: map[string]SlotDef{}},
	"auth.reset-password-form":  {Slots: map[string]SlotDef{}},
	"auth.setup-form":           {Slots: map[string]SlotDef{}},
	"auth.dynamic-signup-form":  {Slots: map[string]SlotDef{}},
	"auth.two-factor-setup":     {Slots: map[string]SlotDef{}},
	"auth.passkey-prompt":       {Slots: map[string]SlotDef{}},
	"auth.session-list":         {Slots: map[string]SlotDef{}},
	"auth.user-button":          {Slots: map[string]SlotDef{}},
	"auth.account-menu":         {Slots: map[string]SlotDef{}},
	"auth.org-switcher":         {Slots: map[string]SlotDef{}},
	"auth.org-profile":          {Slots: map[string]SlotDef{}},
	// auth.tabs is a composite that contains signin+signup tabs; it accepts
	// child auth forms in its `tabs` slot.
	"auth.tabs": {
		Slots: map[string]SlotDef{
			"tabs": {
				Accepts: []string{
					"auth.signin-form", "auth.signup-form",
					"auth.magic-link-form", "auth.oauth-buttons",
					"auth.passkey-prompt", "custom",
				},
				Cardinality: CardinalityMany,
				Extensible:  true,
			},
		},
	},

	// Layout primitives. The React renderers already exist under
	// /shell/src/intents/layout.*.tsx. These catalog entries declare their
	// child slots so manifest validation can verify nesting at registration.
	"layout.tabs": {
		Slots: map[string]SlotDef{
			"panels": {
				Accepts: []string{
					"layout.section", "layout.card", "layout.stack",
					"resource.list", "resource.detail", "form.edit",
					"organism.data-grid", "organism.dynamic-form", "organism.chart",
					"custom",
				},
				Cardinality: CardinalityMany,
				Extensible:  true,
			},
		},
	},
	"layout.accordion": {
		Slots: map[string]SlotDef{
			"items": {
				Accepts:     []string{"layout.section", "layout.card", "custom"},
				Cardinality: CardinalityMany,
				Extensible:  true,
			},
		},
	},
	"layout.card": {
		Slots: map[string]SlotDef{
			"header":  {Accepts: inlineAdornmentIntents, Cardinality: CardinalityOne},
			"content": {Accepts: broadContentIntents, Cardinality: CardinalityMany, Extensible: true},
			"footer":  {Accepts: inlineAdornmentIntents, Cardinality: CardinalityOne},
		},
	},
	"layout.section": {
		Slots: map[string]SlotDef{
			"content": {Accepts: broadContentIntents, Cardinality: CardinalityMany, Extensible: true},
		},
	},
	"layout.page": {
		Slots: map[string]SlotDef{
			"header": {Accepts: inlineAdornmentIntents, Cardinality: CardinalityOne},
			"body":   {Accepts: broadContentIntents, Cardinality: CardinalityMany, Extensible: true},
			"footer": {Accepts: inlineAdornmentIntents, Cardinality: CardinalityOne},
		},
	},
	"layout.stack":     {Slots: map[string]SlotDef{"children": {Accepts: broadContentIntents, Cardinality: CardinalityMany, Extensible: true}}},
	"layout.row":       {Slots: map[string]SlotDef{"children": {Accepts: broadContentIntents, Cardinality: CardinalityMany, Extensible: true}}},
	"layout.column":    {Slots: map[string]SlotDef{"children": {Accepts: broadContentIntents, Cardinality: CardinalityMany, Extensible: true}}},
	"layout.grid":      {Slots: map[string]SlotDef{"children": {Accepts: broadContentIntents, Cardinality: CardinalityMany, Extensible: true}}},
	"layout.container": {Slots: map[string]SlotDef{"children": {Accepts: broadContentIntents, Cardinality: CardinalityMany, Extensible: true}}},
	"layout.split":     {Slots: map[string]SlotDef{"children": {Accepts: broadContentIntents, Cardinality: CardinalityMany, Extensible: true}}},

	// Detail composition primitives. detail.header is a leaf bound to the
	// parent resource.detail's record; detail.section is a labelled card
	// wrapper hosting arbitrary content.
	"detail.header": {Slots: map[string]SlotDef{}},
	"detail.section": {
		Slots: map[string]SlotDef{
			"content": {Accepts: broadContentIntents, Cardinality: CardinalityMany, Extensible: true},
		},
	},

	// Editors. editor.code wraps organism.code-editor with form-state
	// integration when nested in form.edit; standalone it renders a full-page
	// editable code surface (settings JSON editor). editor.formBuilder wraps
	// organism.dynamic-form in design mode for the signup-form builder.
	"editor.code":        {Slots: map[string]SlotDef{}},
	"editor.formBuilder": {Slots: map[string]SlotDef{}},

	// Confirmation wrapper for destructive actions that need richer
	// confirmation than action.button's confirm:true flag (e.g. ban-with-reason
	// where the dialog body itself is a form.edit).
	"confirm.dialog": {
		Slots: map[string]SlotDef{
			"trigger": {Accepts: []string{"action.button", "atom.button"}, Cardinality: CardinalityOne},
			"body":    {Accepts: []string{"layout.section", "atom.text", "form.edit", "custom"}, Cardinality: CardinalityMany},
		},
	},

	// Dashboard widget leaves. dashboard.stat is an alias over molecule.stat-card
	// that's bound to a query and plucks a single field. dashboard.recentlist
	// is a compact list widget. metric.gauge is the gauge variant of metric.counter.
	"metric.gauge":         {Slots: map[string]SlotDef{}},
	"dashboard.stat":       {Slots: map[string]SlotDef{}},
	"dashboard.recentlist": {Slots: map[string]SlotDef{}},

	// Filter leaves. Each mutates the enclosing resource.list's query params
	// via FilterStateContext (analogous to form.field/FormStateContext).
	"filter.search": {Slots: map[string]SlotDef{}},
	"filter.select": {Slots: map[string]SlotDef{}},
	"filter.date":   {Slots: map[string]SlotDef{}},

	// Pagination leaves bound to molecule.pagination, distinguished by
	// underlying paging strategy.
	"pagination.cursor": {Slots: map[string]SlotDef{}},
	"pagination.offset": {Slots: map[string]SlotDef{}},

	// Form field extension leaves. Each wraps a heavier component
	// (organism.code-editor for JSON, organism.tree-view for permissions)
	// and integrates with FormStateContext when nested in form.edit.fields.
	"field.json":        {Slots: map[string]SlotDef{}},
	"field.permissions": {Slots: map[string]SlotDef{}},

	// Settings UI primitives. settings.tabs is a self-contained composite
	// that fetches the namespace list and renders one tab per plugin
	// namespace, each tab body being a settings.panel. settings.panel is
	// a self-contained composite that fetches one namespace's grouped
	// definitions+values and renders them as category cards with form
	// fields auto-typed from each Definition's UIMetadata (switch for
	// bool, number for int, select for enum, textarea for multiline,
	// etc.). Both leaves — the internal layout is not recomposable from
	// the manifest because the entire point is that the renderer owns
	// the grouping and rendering rules. Manifest authors compose at the
	// `page.shell.main` level by dropping in a single `settings.tabs`
	// node for the global page or a single `settings.panel` node for a
	// per-plugin deep-link page.
	"settings.tabs":  {Slots: map[string]SlotDef{}},
	"settings.panel": {Slots: map[string]SlotDef{}},
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

// resolveDataBindings walks nodes in place and resolves DataBindings into
// the flat shape the React shell consumes:
//
//   - QueryRef ("queries.X") is looked up in the named-queries map and
//     replaced with the query's Intent + Params (params merged: query
//     defaults beneath, node overrides on top).
//   - Kind is stamped on every binding (QueryRef or inline) by looking up
//     the resolved Intent in the intent-kind map. The shell branches on
//     this to pick useContractQuery vs useSubscription — without it, a
//     metric.counter bound to `queries.overview` (a query) would
//     incorrectly subscribe and the upstream would reject with
//     "intent not a subscription".
//
// The original QueryRef is preserved on the resolved binding so the shell
// can still cache by the named-query identity (more stable than the
// underlying intent name when params differ per call site).
//
// Callers MUST pass deep-copied nodes — the helper allocates a new
// DataBinding per resolved node but does not deep-copy other fields, so
// mutating the shared source manifest would still happen via Slots/etc.
// In practice the only caller is registerLocked, which always runs this
// against deepCopyGraph output.
//
// Unknown refs are left untouched. loader.Validate rejects them at
// registration time, so reaching this with an unknown ref means
// validation was skipped (test paths). Failing here would swallow that
// bug behind a different symptom.
//
// nil queries / nil kinds are treated as empty maps (skip the
// corresponding resolution step).
func resolveDataBindings(nodes []GraphNode, queries map[string]Query, kinds map[string]IntentKind) {
	for i := range nodes {
		n := &nodes[i]
		if n.Data != nil {
			// Always allocate a fresh binding so we never mutate the source
			// manifest's *DataBinding pointer (deepCopyGraph leaves the
			// pointer shared). The clone path is cheap; gen-pop graph
			// nodes don't have a Data binding at all.
			fresh := *n.Data
			if fresh.QueryRef != "" {
				key := fresh.QueryRef
				const prefix = "queries."
				if len(key) > len(prefix) && key[:len(prefix)] == prefix {
					key = key[len(prefix):]
				}
				if q, ok := queries[key]; ok {
					fresh.Intent = q.Intent
					fresh.Params = mergeParamSources(q.Params, n.Data.Params)
				}
			}
			if fresh.Intent != "" && fresh.Kind == "" {
				if k, ok := kinds[fresh.Intent]; ok {
					fresh.Kind = k
				}
			}
			n.Data = &fresh
		}
		for slot, children := range n.Slots {
			resolveDataBindings(children, queries, kinds)
			n.Slots[slot] = children
		}
	}
}

// prefixGraphRoutes rewrites every top-level node's Route to
// "/@<slug><route>", in place. The empty slug is a no-op (the caller is
// expected to compute slug via AppInfo.ResolvedSlug, which only returns
// "" when the contributor isn't an app — and library contributors don't
// have top-level routes in practice).
//
// Already-prefixed routes (starting with "/@") are left alone so the
// transform stays idempotent if a manifest is re-merged. Routes that
// don't start with "/" pass through unchanged — the loader normally
// rejects those, but we don't want to corrupt the data here either.
//
// Sub-slot nodes are NOT prefixed: only the top-level GraphNode owns a
// Route field. The slot children identify themselves via Intent and are
// addressed relative to their parent route on the wire.
func prefixGraphRoutes(nodes []GraphNode, slug string) {
	if slug == "" {
		return
	}
	prefix := "/@" + slug
	for i := range nodes {
		r := nodes[i].Route
		if r == "" {
			continue
		}
		if len(r) >= 2 && r[0] == '/' && r[1] == '@' {
			continue
		}
		if r[0] != '/' {
			continue
		}
		// Treat "/" as the app root: produces "/@slug" rather than
		// "/@slug/" so routes are consistent (no trailing slash).
		if r == "/" {
			nodes[i].Route = prefix
		} else {
			nodes[i].Route = prefix + r
		}
	}
}

// prefixAppHome rewrites the AppInfo.Home field with the same /@<slug>
// transform used on graph routes, so the switcher navigates to the
// fully-namespaced home route the React shell actually sees. Unset
// Home stays unset; "/" maps to "/@slug" (the app root).
func prefixAppHome(app *AppInfo, slug string) {
	if app == nil || slug == "" {
		return
	}
	h := app.Home
	if h == "" {
		return
	}
	if len(h) >= 2 && h[0] == '/' && h[1] == '@' {
		return
	}
	if h[0] != '/' {
		return
	}
	prefix := "/@" + slug
	if h == "/" {
		app.Home = prefix
	} else {
		app.Home = prefix + h
	}
}

// mergeParamSources merges two param maps; values in override win over
// values with the same key in base. Returns nil when both inputs are nil.
// Used to layer a graph node's inline params on top of a named query's
// declared params during QueryRef resolution.
func mergeParamSources(base, override map[string]ParamSource) map[string]ParamSource {
	if len(base) == 0 && len(override) == 0 {
		return nil
	}
	out := make(map[string]ParamSource, len(base)+len(override))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range override {
		out[k] = v
	}
	return out
}
