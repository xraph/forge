package contract

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// IntentKind is the wire-level discriminator declared on every intent.
// It must be consistent with the request envelope Kind at dispatch time.
type IntentKind string

const (
	IntentKindQuery        IntentKind = "query"
	IntentKindCommand      IntentKind = "command"
	IntentKindSubscription IntentKind = "subscription"
)

// Capability is the data-classification of an intent's effects.
// It composes with IntentKind: a command must be capability=write; a query/subscription
// must be capability=read.
type Capability string

const (
	CapRead  Capability = "read"
	CapWrite Capability = "write"
)

// ContractManifest is the top-level YAML each contributor publishes.
type ContractManifest struct {
	SchemaVersion int              `json:"schemaVersion"     yaml:"schemaVersion"`
	Contributor   Contributor      `json:"contributor"       yaml:"contributor"`
	Queries       map[string]Query `json:"queries,omitempty" yaml:"queries,omitempty"`
	Intents       []Intent         `json:"intents"           yaml:"intents"`
}

// Contributor names a single contributor and declares its supported envelope versions.
//
// App, when set, opts this contributor into the dashboard's app switcher.
// A contributor without an App block is a "library" contributor — it may
// declare intents without appearing as a switchable app in the sidebar
// header. The pilot and authsome both set App so they surface as
// first-class apps; helper contributors (e.g. a future shared "design
// system" contributor) can stay invisible.
type Contributor struct {
	Name         string          `json:"name"                   yaml:"name"`
	Envelope     EnvelopeSupport `json:"envelope"               yaml:"envelope"`
	Capabilities []string        `json:"capabilities,omitempty" yaml:"capabilities,omitempty"`
	App          *AppInfo        `json:"app,omitempty"          yaml:"app,omitempty"`
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
// Slug names a non-root app for URL namespacing. When set, apps.list
// projects Home to /@<slug><home> on the wire (see projectAppHome).
// Defaults to Contributor.Name when unset. Has no effect on a root app —
// root URLs are always bare regardless of slug.
//
// That projection has no consumer. apps.list is not called from any
// TypeScript in forge-dashboard, and definePlugin's own routes are declared
// bare (the spec's example declares /authsome/users, not /@authsome/users).
// So do NOT assume your plugin's React routes must live under /@<slug>/*.
// A later wave has to settle it one way or the other: either definePlugin
// adopts the /@<slug> prefix and this projection becomes real, or the
// projection is dropped.
type AppInfo struct {
	DisplayName string `json:"displayName"        yaml:"displayName"`
	Slug        string `json:"slug,omitempty"     yaml:"slug,omitempty"`
	Root        bool   `json:"root,omitempty"     yaml:"root,omitempty"`
	Icon        string `json:"icon,omitempty"     yaml:"icon,omitempty"`
	Priority    int    `json:"priority,omitempty" yaml:"priority,omitempty"`
	Home        string `json:"home,omitempty"     yaml:"home,omitempty"`
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
	Supports  []string `json:"supports"  yaml:"supports"`
	Preferred string   `json:"preferred" yaml:"preferred"`
}

// Intent declares a single named operation and its security/version metadata.
type Intent struct {
	Name        string           `json:"name"                  yaml:"name"`
	Kind        IntentKind       `json:"kind"                  yaml:"kind"`
	Version     int              `json:"version"               yaml:"version"`
	Capability  Capability       `json:"capability"            yaml:"capability"`
	Requires    Predicate        `json:"requires"              yaml:"requires,omitempty"`
	Schema      IntentSchema     `json:"schema"                yaml:"schema,omitempty"`
	Mode        SubscriptionMode `json:"mode,omitempty"        yaml:"mode,omitempty"`        // subscription only
	Invalidates []string         `json:"invalidates,omitempty" yaml:"invalidates,omitempty"` // command only
	Audit       *bool            `json:"audit,omitempty"       yaml:"audit,omitempty"`       // default true for commands
	Deprecated  *Deprecation     `json:"deprecated,omitempty"  yaml:"deprecated,omitempty"`
}

// IntentSchema is loose by design: contributors describe their input/output shapes;
// validation against this is opt-in (slice (b) wires it).
type IntentSchema struct {
	Input  map[string]any `json:"input,omitempty"  yaml:"input,omitempty"`
	Output any            `json:"output,omitempty" yaml:"output,omitempty"`
}

// Query is a named binding of an intent plus its parameters. It is parsed from
// the manifest and validated (loader.Validate checks the intent it names is
// declared by the same contributor), and it is carried in the manifest wire
// shape — but nothing consumes it at runtime today. Plugins never see the Go
// manifest; they call intents through their scoped client. Cache is likewise
// parsed and read by nobody. Retained deliberately so the manifest schema stays
// stable; do not build on it until something actually reads it.
type Query struct {
	Intent string                 `json:"intent"           yaml:"intent"`
	Params map[string]ParamSource `json:"params,omitempty" yaml:"params,omitempty"`
	Cache  *QueryCache            `json:"cache,omitempty"  yaml:"cache,omitempty"`
}

// ParamSource describes where a parameter value comes from.
// Exactly one of Value/From is set; YAML uses { from: route.tenant } or a literal.
type ParamSource struct {
	Value any    `json:"value,omitempty" yaml:"value,omitempty"`
	From  string `json:"from,omitempty"  yaml:"from,omitempty"` // route.X | parent.X | state.X | session.X
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
	StaleTime string `json:"staleTime,omitempty" yaml:"staleTime,omitempty"`
}

// Predicate is the boolean access expression: any of all/any/not, plus an optional
// named Warden delegate. An empty Predicate evaluates to allow.
type Predicate struct {
	All    []string `json:"all,omitempty"    yaml:"all,omitempty"`
	Any    []string `json:"any,omitempty"    yaml:"any,omitempty"`
	Not    []string `json:"not,omitempty"    yaml:"not,omitempty"`
	Warden string   `json:"warden,omitempty" yaml:"warden,omitempty"`
}
