// manifest.go
package contract

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// IntentKind is the wire-level discriminator declared on every intent.
// It must be consistent with the request envelope Kind at dispatch time.
type IntentKind string

const (
	IntentKindGraph        IntentKind = "graph"
	IntentKindQuery        IntentKind = "query"
	IntentKindCommand      IntentKind = "command"
	IntentKindSubscription IntentKind = "subscription"
)

// Capability is the data-classification of an intent's effects.
// It composes with IntentKind: a command must be capability=write; a query/subscription
// must be capability=read; a graph must be capability=render.
type Capability string

const (
	CapRead   Capability = "read"
	CapWrite  Capability = "write"
	CapRender Capability = "render"
)

// ContractManifest is the top-level YAML each contributor publishes.
type ContractManifest struct {
	SchemaVersion int              `yaml:"schemaVersion" json:"schemaVersion"`
	Contributor   Contributor      `yaml:"contributor"   json:"contributor"`
	Queries       map[string]Query `yaml:"queries,omitempty" json:"queries,omitempty"`
	Intents       []Intent         `yaml:"intents"       json:"intents"`
	Graph         []GraphNode      `yaml:"graph,omitempty" json:"graph,omitempty"`
	Extends       []Extension      `yaml:"extends,omitempty" json:"extends,omitempty"`
}

// Contributor names a single contributor and declares its supported envelope versions.
//
// App, when set, opts this contributor into the dashboard's app switcher.
// A contributor without an App block is a "library" contributor — it may
// declare intents and inject nodes into other contributors' graphs via
// Extends, but its own routes (if any) won't appear as a switchable app
// in the sidebar header. The pilot and authsome both set App so they
// surface as first-class apps; helper contributors (e.g. a future shared
// "design system" contributor) can stay invisible.
type Contributor struct {
	Name         string          `yaml:"name"         json:"name"`
	Envelope     EnvelopeSupport `yaml:"envelope"     json:"envelope"`
	Capabilities []string        `yaml:"capabilities,omitempty" json:"capabilities,omitempty"`
	App          *AppInfo        `yaml:"app,omitempty"          json:"app,omitempty"`
}

// AppInfo describes how a contributor presents itself in the app switcher.
// All fields are display-only — the contract dispatch path is unaffected.
//
//	contributor:
//	  name: core-contract
//	  app:
//	    displayName: Forge
//	    root: true         # this app owns the bare URL; no /@slug prefix
//	    icon: forge
//	    priority: 0
//	    home: /
//
//	contributor:
//	  name: auth
//	  app:
//	    displayName: Authsome
//	    slug: authsome     # routes become /@authsome/...
//	    icon: shield
//	    priority: 10
//	    home: /users
//
// Root marks the platform app: its routes are NOT URL-prefixed, so
// /, /health, etc. stay bare. There must be at most one root app per
// dashboard deployment (the registry doesn't enforce this today;
// behaviour on conflict is "first registered wins" via the natural
// ordering in apps.list).
//
// Slug controls URL namespacing for non-root apps: when set, every
// top-level graph route the contributor declares is projected to
// /@<slug><route> on the wire, and Home is projected the same way.
// Defaults to Contributor.Name when unset. Has no effect on a root
// app — root URLs are always bare regardless of slug.
type AppInfo struct {
	DisplayName string `yaml:"displayName" json:"displayName"`
	Slug        string `yaml:"slug,omitempty" json:"slug,omitempty"`
	Root        bool   `yaml:"root,omitempty" json:"root,omitempty"`
	Icon        string `yaml:"icon,omitempty" json:"icon,omitempty"`
	Priority    int    `yaml:"priority,omitempty" json:"priority,omitempty"`
	Home        string `yaml:"home,omitempty" json:"home,omitempty"`
}

// ResolvedSlug returns the slug that should be used for URL prefixing,
// falling back to the contributor name when no explicit slug is set.
// Returns "" for root apps so callers can rely on a non-empty slug
// signalling "this app gets URL prefixing" without a separate
// `if app.Root` branch.
func (a *AppInfo) ResolvedSlug(contributorName string) string {
	if a == nil || a.Root {
		return ""
	}
	if a.Slug != "" {
		return a.Slug
	}
	return contributorName
}

// EnvelopeSupport declares which envelope versions this contributor can speak.
type EnvelopeSupport struct {
	Supports  []string `yaml:"supports"  json:"supports"`
	Preferred string   `yaml:"preferred" json:"preferred"`
}

// Intent declares a single named operation and its security/version metadata.
type Intent struct {
	Name        string           `yaml:"name"        json:"name"`
	Kind        IntentKind       `yaml:"kind"        json:"kind"`
	Version     int              `yaml:"version"     json:"version"`
	Capability  Capability       `yaml:"capability"  json:"capability"`
	Requires    Predicate        `yaml:"requires,omitempty" json:"requires,omitempty"`
	Schema      IntentSchema     `yaml:"schema,omitempty" json:"schema,omitempty"`
	Mode        SubscriptionMode `yaml:"mode,omitempty" json:"mode,omitempty"`               // subscription only
	Invalidates []string         `yaml:"invalidates,omitempty" json:"invalidates,omitempty"` // command only
	Audit       *bool            `yaml:"audit,omitempty"       json:"audit,omitempty"`       // default true for commands
	Deprecated  *Deprecation     `yaml:"deprecated,omitempty" json:"deprecated,omitempty"`
}

// IntentSchema is loose by design: contributors describe their input/output shapes;
// validation against this is opt-in (slice (b) wires it).
type IntentSchema struct {
	Input  map[string]any `yaml:"input,omitempty"  json:"input,omitempty"`
	Output any            `yaml:"output,omitempty" json:"output,omitempty"`
}

// Query is a named, reusable, cacheable data binding referenced by graph nodes.
type Query struct {
	Intent string                 `yaml:"intent" json:"intent"`
	Params map[string]ParamSource `yaml:"params,omitempty" json:"params,omitempty"`
	Cache  *QueryCache            `yaml:"cache,omitempty"  json:"cache,omitempty"`
}

// ParamSource describes where a parameter value comes from.
// Exactly one of Value/From is set; YAML uses { from: route.tenant } or a literal.
type ParamSource struct {
	Value any    `yaml:"value,omitempty" json:"value,omitempty"`
	From  string `yaml:"from,omitempty"  json:"from,omitempty"` // route.X | parent.X | state.X | session.X
}

// UnmarshalYAML accepts either a scalar (treated as the From source) or a
// mapping with the explicit {value} or {from} form.
func (p *ParamSource) UnmarshalYAML(value *yaml.Node) error {
	switch value.Kind {
	case yaml.ScalarNode:
		p.From = value.Value
		return nil
	case yaml.MappingNode:
		type alias ParamSource
		var a alias
		if err := value.Decode(&a); err != nil {
			return err
		}
		*p = ParamSource(a)
		return nil
	default:
		return fmt.Errorf("param: expected scalar or mapping, got kind=%d", value.Kind)
	}
}

// QueryCache declares per-query staleness for the client.
type QueryCache struct {
	StaleTime string `yaml:"staleTime,omitempty" json:"staleTime,omitempty"`
}

// GraphNode is a single node in the UI graph (an intent invocation with slot fills).
type GraphNode struct {
	Route       string                 `yaml:"route,omitempty"       json:"route,omitempty"` // top-level only
	Intent      string                 `yaml:"intent"                json:"intent"`
	Title       string                 `yaml:"title,omitempty"       json:"title,omitempty"`
	Nav         *NavConfig             `yaml:"nav,omitempty"         json:"nav,omitempty"`
	Root        bool                   `yaml:"root,omitempty"        json:"root,omitempty"`
	Data        *DataBinding           `yaml:"data,omitempty"        json:"data,omitempty"`
	Props       map[string]any         `yaml:"props,omitempty"       json:"props,omitempty"`
	Slots       map[string][]GraphNode `yaml:"slots,omitempty"       json:"slots,omitempty"`
	VisibleWhen *Predicate             `yaml:"visibleWhen,omitempty" json:"visibleWhen,omitempty"`
	EnabledWhen *Predicate             `yaml:"enabledWhen,omitempty" json:"enabledWhen,omitempty"`
	Op          string                 `yaml:"op,omitempty"          json:"op,omitempty"` // for action nodes
	Payload     map[string]ParamSource `yaml:"payload,omitempty"     json:"payload,omitempty"`
	Component   string                 `yaml:"component,omitempty"   json:"component,omitempty"` // intent: custom escape hatch
	Src         string                 `yaml:"src,omitempty"         json:"src,omitempty"`       // intent: iframe escape hatch
	Sandbox     []string               `yaml:"sandbox,omitempty"     json:"sandbox,omitempty"`
	Protocol    string                 `yaml:"protocol,omitempty"    json:"protocol,omitempty"`
}

// NavConfig is per-route nav metadata; mirrors today's contributor.NavItem fields.
type NavConfig struct {
	Group    string `yaml:"group,omitempty"    json:"group,omitempty"`
	Icon     string `yaml:"icon,omitempty"     json:"icon,omitempty"`
	Priority int    `yaml:"priority,omitempty" json:"priority,omitempty"`
	Badge    string `yaml:"badge,omitempty"    json:"badge,omitempty"`
}

// DataBinding is either an inline {intent, params} pair or a named query reference.
// YAML supports both shapes:
//
//	data: queries.userList
//	data: { intent: users.list, params: {...} }
//
// Kind is not authored in YAML; it's stamped at merge time from the
// referenced intent's declared kind so the React shell can pick the right
// hook (useContractQuery for query, useSubscription for subscription)
// without having to chase the manifest's intent table client-side.
type DataBinding struct {
	QueryRef string                 `yaml:"-" json:"queryRef,omitempty"`
	Intent   string                 `yaml:"intent,omitempty"  json:"intent,omitempty"`
	Kind     IntentKind             `yaml:"-"                 json:"kind,omitempty"`
	Params   map[string]ParamSource `yaml:"params,omitempty"  json:"params,omitempty"`
}

// UnmarshalYAML accepts either a scalar (treated as a named query reference) or
// a mapping with the inline {intent, params} form.
func (d *DataBinding) UnmarshalYAML(value *yaml.Node) error {
	switch value.Kind {
	case yaml.ScalarNode:
		d.QueryRef = value.Value
		return nil
	case yaml.MappingNode:
		// Decode into a shadow type to avoid recursion.
		type alias DataBinding
		var a alias
		if err := value.Decode(&a); err != nil {
			return err
		}
		*d = DataBinding(a)
		return nil
	default:
		return fmt.Errorf("data: expected scalar or mapping, got kind=%d", value.Kind)
	}
}

// Predicate is the boolean access expression: any of all/any/not, plus an optional
// named Warden delegate. An empty Predicate evaluates to allow.
type Predicate struct {
	All    []string `yaml:"all,omitempty"    json:"all,omitempty"`
	Any    []string `yaml:"any,omitempty"    json:"any,omitempty"`
	Not    []string `yaml:"not,omitempty"    json:"not,omitempty"`
	Warden string   `yaml:"warden,omitempty" json:"warden,omitempty"`
}

// Extension declares that this contributor wants to add nodes into another contributor's slot.
type Extension struct {
	Target ExtensionTarget `yaml:"target" json:"target"`
	Slot   string          `yaml:"slot"   json:"slot"` // dotted path: "detailDrawer.fields"
	Add    []GraphNode     `yaml:"add"    json:"add"`
}

// ExtensionTarget identifies the host node to extend.
type ExtensionTarget struct {
	Contributor string `yaml:"contributor" json:"contributor"`
	Intent      string `yaml:"intent"      json:"intent"`
	Route       string `yaml:"route,omitempty" json:"route,omitempty"`
}
