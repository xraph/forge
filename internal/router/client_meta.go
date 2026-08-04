package router

import (
	"reflect"
	"strings"
)

// EntityDef declares how a type is identified in a client-side normalized cache.
//
// IDField is the JSON PROPERTY NAME as it appears in the response body -- `id`,
// `order_number` -- not the Go field name. That is what the browser runtime
// indexes a payload by, and what inference produces when it reads a schema, so
// declaring identity and inferring it agree by construction. A Go field named
// ID carrying the json tag "id" is therefore declared as IDField: "id".
//
// Getting this backwards is silent: an idField that names no property in the
// response produces a cache key that never matches a record, which looks like
// an ineffective cache rather than a bug. Generation warns when it can see the
// response schema and the named property is not in it.
type EntityDef struct {
	Type    string
	IDField string
}

// ForgeEntity is implemented by types that override entity inference. Reach for
// it when a type has no field named `id`, when the field named `id` is not the
// identity, or when two fields are both identity-shaped and inference would
// otherwise refuse to guess.
//
// The schema generator honours it: when a type implementing ForgeEntity is
// rendered into an OpenAPI schema, the property named by the returned
// EntityDef.IDField is marked with x-forge-id, which is what the client
// generator's inference reads. An explicit marker BEATS the `id` name
// heuristic, so declaring identity resolves the ambiguity rather than adding to
// it. Identity then travels with the type, on every endpoint that returns it,
// rather than being repeated per route.
//
// Declare identity ONCE per type. This and the `forge:"id"` struct tag write
// the same marker, so using both on different fields of one type states that
// two different fields are the identity; that contradiction is refused outright
// and the type resolves to no entity at all.
//
// EntityDef.IDField must be the JSON property name (see EntityDef). Type is
// advisory here -- the entity is named after the type's schema component name,
// so return the same name to keep declarations readable.
type ForgeEntity interface {
	ForgeEntity() EntityDef
}

// StreamIntent is what a stream message does to the cache.
type StreamIntent string

const (
	StreamUpsert StreamIntent = "upsert"
	StreamPatch  StreamIntent = "patch"
	StreamEvict  StreamIntent = "evict"
)

// StreamBinding binds one channel message to an entity type.
type StreamBinding struct {
	Message     string
	EntityType  string
	Intent      StreamIntent
	Invalidates []string
}

// EmitsBuilder accumulates one binding. Build resolves the defaults.
type EmitsBuilder struct {
	binding      StreamBinding
	intentSet    bool
	invalidesSet bool
}

// Emits declares that a channel emits `message` carrying entity T.
//
// Intent is inferred from the message-name suffix, so the common three-message
// channel needs no further configuration:
//
//	forge.Emits[Order]("order.created")
//	forge.Emits[Order]("order.updated")
//	forge.Emits[Order]("order.deleted")
func Emits[T any](message string) *EmitsBuilder {
	return &EmitsBuilder{
		binding: StreamBinding{
			Message:    message,
			EntityType: reflect.TypeOf((*T)(nil)).Elem().Name(),
		},
	}
}

// As overrides the inferred intent for messages outside the naming convention.
func (e *EmitsBuilder) As(intent StreamIntent) *EmitsBuilder {
	e.binding.Intent = intent
	e.intentSet = true

	return e
}

// Invalidates overrides the inferred tag invalidations.
func (e *EmitsBuilder) Invalidates(tags ...string) *EmitsBuilder {
	e.binding.Invalidates = tags
	e.invalidesSet = true

	return e
}

// Build resolves defaults and returns the binding.
func (e *EmitsBuilder) Build() StreamBinding {
	out := e.binding

	if !e.intentSet {
		out.Intent = intentFromMessage(out.Message)
	}

	if !e.invalidesSet {
		// A patch reaches every view through the entity store, so only
		// membership changes need the collection refetched.
		if out.Intent != StreamPatch && out.EntityType != "" {
			out.Invalidates = []string{out.EntityType + "[]"}
		}
	}

	return out
}

// intentFromMessage reads intent from the message-name suffix, defaulting to a
// patch because merging a payload is the safe reading of an unrecognised name:
// it updates what is already cached without inventing or destroying membership.
func intentFromMessage(message string) StreamIntent {
	suffix := message
	if i := strings.LastIndex(message, "."); i >= 0 {
		suffix = message[i+1:]
	}

	switch strings.ToLower(suffix) {
	case "created", "added":
		return StreamUpsert
	case "deleted", "removed":
		return StreamEvict
	default:
		return StreamPatch
	}
}
