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
	SchemaVersion int              `yaml:"schemaVersion" json:"schemaVersion"`
	Contributor   Contributor      `yaml:"contributor"   json:"contributor"`
	Queries       map[string]Query `yaml:"queries,omitempty" json:"queries,omitempty"`
	Intents       []Intent         `yaml:"intents"       json:"intents"`
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

// Query is a named binding of an intent plus its parameters. It is parsed from
// the manifest and validated (loader.Validate checks the intent it names is
// declared by the same contributor), and it is carried in the manifest wire
// shape — but nothing consumes it at runtime today. Plugins never see the Go
// manifest; they call intents through their scoped client. Cache is likewise
// parsed and read by nobody. Retained deliberately so the manifest schema stays
// stable; do not build on it until something actually reads it.
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

// Predicate is the boolean access expression: any of all/any/not, plus an optional
// named Warden delegate. An empty Predicate evaluates to allow.
type Predicate struct {
	All    []string `yaml:"all,omitempty"    json:"all,omitempty"`
	Any    []string `yaml:"any,omitempty"    json:"any,omitempty"`
	Not    []string `yaml:"not,omitempty"    json:"not,omitempty"`
	Warden string   `yaml:"warden,omitempty" json:"warden,omitempty"`
}
