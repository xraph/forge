package router

// setMeta writes one client-generation key, allocating the map on first use.
func setMeta(cfg *RouteConfig, key string, value any) {
	if cfg.Metadata == nil {
		cfg.Metadata = make(map[string]any)
	}

	cfg.Metadata[key] = value
}

// appendMeta appends to a []string metadata key so repeated options accumulate
// rather than the last one silently winning.
func appendMeta(cfg *RouteConfig, key string, values []string) {
	if cfg.Metadata == nil {
		cfg.Metadata = make(map[string]any)
	}

	existing, _ := cfg.Metadata[key].([]string)
	cfg.Metadata[key] = append(existing, values...)
}

type entityOpt struct{ def EntityDef }

func (o *entityOpt) Apply(cfg *RouteConfig) { setMeta(cfg, "forge.client.entity", o.def) }

// WithEntity overrides inferred identity for this endpoint's response.
//
// def.IDField is the JSON property name in the response body (see EntityDef),
// so a Go field named ID carrying the json tag "id" is declared as
// IDField: "id".
//
// Prefer implementing ForgeEntity on the type: identity is intrinsic to a type,
// and declaring it per route repeats it on every endpoint returning an Order.
// This option exists for types you cannot add a method to, and for the one
// endpoint whose response is identified differently from the rest.
func WithEntity(def EntityDef) RouteOption { return &entityOpt{def} }

type noEntityOpt struct{}

func (o *noEntityOpt) Apply(cfg *RouteConfig) { setMeta(cfg, "forge.client.noEntity", true) }

// WithoutEntity keeps this endpoint's response out of the normalized store.
// Use it for projections and snapshots that must not merge with the canonical
// record.
func WithoutEntity() RouteOption { return &noEntityOpt{} }

type invalidatesOpt struct{ tags []string }

func (o *invalidatesOpt) Apply(cfg *RouteConfig) {
	appendMeta(cfg, "forge.client.invalidates", o.tags)
}

// WithInvalidates declares cross-entity effects. Same-entity invalidation is
// derived, so this is only for edges a reader would not predict.
func WithInvalidates(tags ...string) RouteOption { return &invalidatesOpt{tags} }

type noInvalidationOpt struct{ tags []string }

func (o *noInvalidationOpt) Apply(cfg *RouteConfig) {
	appendMeta(cfg, "forge.client.noInvalidation", o.tags)
}

// WithoutInvalidation suppresses a derived invalidation for endpoints that
// cannot change list membership.
func WithoutInvalidation(tags ...string) RouteOption { return &noInvalidationOpt{tags} }

type streamBindingOpt struct{ builders []*EmitsBuilder }

func (o *streamBindingOpt) Apply(cfg *RouteConfig) {
	bindings := make([]StreamBinding, 0, len(o.builders))
	for _, b := range o.builders {
		bindings = append(bindings, b.Build())
	}

	setMeta(cfg, "forge.client.streamBindings", bindings)
}

// WithStreamBinding declares which entity updates a channel emits.
func WithStreamBinding(builders ...*EmitsBuilder) RouteOption {
	return &streamBindingOpt{builders}
}
