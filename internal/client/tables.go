package client

import "encoding/json"

// TablesFile is the file a generator writes its tables to when
// GeneratorConfig.EmitTablesJSON is set.
const TablesFile = "forge-tables.json"

// GeneratedTables is the language-neutral form of the tables a generated
// client carries: operations, entities, stream bindings, security schemes and
// capabilities.
//
// The TypeScript and Dart generators each fill one from the same values they
// render into their own source, so two generators run over one specification
// can be compared field by field. A difference is a runtime that caches,
// invalidates or authorizes differently depending on the language it was
// generated in.
type GeneratedTables struct {
	Ops             map[string]TableOp       `json:"ops"`
	Entities        map[string]TableEntity   `json:"entities"`
	Streams         []TableStream            `json:"streams"`
	SecuritySchemes map[string]TableSecurity `json:"securitySchemes"`
	Capabilities    TableCapabilities        `json:"capabilities"`
}

// TableOp is one row of the operations table, keyed by operation key.
type TableOp struct {
	Method        string   `json:"method"`
	Path          string   `json:"path"`
	Entity        string   `json:"entity,omitempty"`
	RootType      string   `json:"rootType,omitempty"`
	StaleTime     int64    `json:"staleTime,omitempty"`
	Provides      []string `json:"provides"`
	Invalidates   []string `json:"invalidates"`
	Security      []string `json:"security,omitempty"`
	Idempotent    bool     `json:"idempotent,omitempty"`
	BodyCodec     string   `json:"bodyCodec,omitempty"`
	ResponseCodec string   `json:"responseCodec,omitempty"`
}

// TableEntity is one row of the entities table. An empty IDField marks a
// signpost row: walked for its fields, never stored.
type TableEntity struct {
	IDField string            `json:"idField,omitempty"`
	Fields  map[string]string `json:"fields,omitempty"`
}

// TableStream is one stream binding. Kind is "entity" or "duplex"; the
// fields that do not apply to a kind are empty.
type TableStream struct {
	Kind        string   `json:"kind"`
	Channel     string   `json:"channel"`
	Message     string   `json:"message,omitempty"`
	Entity      string   `json:"entity,omitempty"`
	Intent      string   `json:"intent,omitempty"`
	Invalidates []string `json:"invalidates,omitempty"`
	Decode      string   `json:"decode,omitempty"`
	Send        string   `json:"send,omitempty"`
	Receive     string   `json:"receive,omitempty"`
}

// TableSecurity is one declared security scheme.
type TableSecurity struct {
	Type   string `json:"type"`
	In     string `json:"in,omitempty"`
	Name   string `json:"name,omitempty"`
	Scheme string `json:"scheme,omitempty"`
}

// TableCapabilities is the capability vocabulary and the per-operation
// requirements over it.
type TableCapabilities struct {
	Scopes                []string                      `json:"scopes"`
	Roles                 []string                      `json:"roles"`
	Permissions           []string                      `json:"permissions"`
	Operations            []string                      `json:"operations"`
	RequiredCapabilities  map[string][][]string         `json:"requiredCapabilities"`
	RequiredAuthorization map[string]TableAuthorization `json:"requiredAuthorization"`
}

// TableAuthorization is one operation's role and permission requirement.
type TableAuthorization struct {
	Roles       []string `json:"roles,omitempty"`
	Permissions []string `json:"permissions,omitempty"`
}

// MarshalCanonical renders t as indented JSON with every map key sorted and
// every required list present, so two equal tables produce equal bytes.
func (t GeneratedTables) MarshalCanonical() ([]byte, error) {
	norm := t

	if norm.Ops == nil {
		norm.Ops = map[string]TableOp{}
	}

	ops := make(map[string]TableOp, len(norm.Ops))
	for key, op := range norm.Ops {
		op.Provides = nonNilStrings(op.Provides)
		op.Invalidates = nonNilStrings(op.Invalidates)
		ops[key] = op
	}

	norm.Ops = ops

	if norm.Entities == nil {
		norm.Entities = map[string]TableEntity{}
	}

	if norm.Streams == nil {
		norm.Streams = []TableStream{}
	}

	if norm.SecuritySchemes == nil {
		norm.SecuritySchemes = map[string]TableSecurity{}
	}

	caps := norm.Capabilities
	caps.Scopes = nonNilStrings(caps.Scopes)
	caps.Roles = nonNilStrings(caps.Roles)
	caps.Permissions = nonNilStrings(caps.Permissions)
	caps.Operations = nonNilStrings(caps.Operations)

	if caps.RequiredCapabilities == nil {
		caps.RequiredCapabilities = map[string][][]string{}
	}

	if caps.RequiredAuthorization == nil {
		caps.RequiredAuthorization = map[string]TableAuthorization{}
	}

	norm.Capabilities = caps

	out, err := json.MarshalIndent(norm, "", "  ")
	if err != nil {
		return nil, err
	}

	return append(out, '\n'), nil
}

func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}

	return values
}
