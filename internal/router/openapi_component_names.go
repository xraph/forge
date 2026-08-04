package router

import (
	"fmt"
	"maps"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// componentRefPrefix is the JSON pointer prefix every component $ref carries.
const componentRefPrefix = "#/components/schemas/"

// componentRegistration records one component the schema generator has named,
// so the final naming pass can tell which types are competing for a name.
//
// suffix is the decoration a call site appends to the type's own name (today
// only "Body", for the body half of a unified response). It participates in
// every name computation, so Invoice+Body and Invoice are independent names
// that collide independently.
//
// pinned marks a name the user chose explicitly -- an EnumNamer implementation
// or a `schema:"..."` struct tag. Those are never rewritten: the user asked for
// that exact string, and honouring it is more important than tidiness.
type componentRegistration struct {
	typ    reflect.Type
	suffix string
	pinned bool
}

// registryKey is the identity of a registration: the fully qualified Go type
// plus its suffix. The NUL separator keeps type `InvoiceBody` distinct from
// type `Invoice` with suffix "Body"; it is omitted when there is no suffix so
// that plain types keep the bare qualified name they have always had as a key.
func registryKey(qualified, suffix string) string {
	if suffix == "" {
		return qualified
	}

	return qualified + "\x00" + suffix
}

// baseName is the name this registration would get if nothing else competed
// for it -- exactly the name the generator produced before collision handling
// existed. A registration whose baseName is uncontested always keeps it.
func (r *componentRegistration) baseName() string {
	return GetTypeName(r.typ) + r.suffix
}

// sortKey orders registrations independently of the order routes were declared
// in, so the resolution of a collision depends only on the set of types.
func (r *componentRegistration) sortKey() string {
	return registryKey(getQualifiedTypeName(r.typ), r.suffix)
}

// candidates lists the progressively more specific names this registration can
// fall back to, in order. The first entries reuse buildNamespacedCandidates --
// the convention this repo already uses for namespaced component names -- and
// the last is the fully qualified path, which is unique by construction
// because no two Go types share a package path and name.
func (r *componentRegistration) candidates() []string {
	out := make([]string, 0, 4)
	for _, c := range buildNamespacedCandidates(r.typ.PkgPath(), cleanGenericTypeName(r.typ.Name())) {
		out = append(out, c+r.suffix)
	}

	return append(out, sanitizeComponentName(getQualifiedTypeName(r.typ))+r.suffix)
}

// sanitizeComponentName makes a fully qualified Go type name usable as an
// OpenAPI component name, which must match ^[a-zA-Z0-9._-]+$. An import path
// contains '/' and may contain '.' and '-', so it cannot be used raw.
//
// It follows cleanGenericTypeName's lead: that function turns the '[' of a
// generic instantiation into '_' rather than dropping it, keeping the parts of
// the name separated and readable. Here everything that is not a letter, digit
// or underscore becomes '_' for the same reason. That is stricter than the
// OpenAPI charset needs -- '.' and '-' would be legal -- because the name goes
// on to become an identifier in a generated client, where they would not be.
func sanitizeComponentName(name string) string {
	var b strings.Builder

	b.Grow(len(name))

	for _, ch := range name {
		switch {
		case ch >= 'a' && ch <= 'z', ch >= 'A' && ch <= 'Z', ch >= '0' && ch <= '9', ch == '_':
			b.WriteRune(ch)
		default:
			b.WriteByte('_')
		}
	}

	return b.String()
}

// noteComponent records that componentName has been handed to typ. Every name
// the generator hands out goes through here, which is what lets the final pass
// see the whole set at once.
func (g *schemaGenerator) noteComponent(componentName string, typ reflect.Type, suffix string, pinned bool) {
	if componentName == "" || typ == nil {
		return
	}

	if typ.Kind() == reflect.Ptr {
		typ = typ.Elem()
	}

	if existing, ok := g.registrations[componentName]; ok {
		if existing.typ != typ {
			// Two types under one name, and at least one of them was named
			// explicitly, so this pass cannot qualify its way out of it. All it
			// can do is refuse to be quiet about it.
			g.reportPinnedConflict(componentName, existing.typ, typ)

			return
		}

		// A pinned claim wins over an inferred one: the user asked for this
		// exact string, so it must not be rewritten out from under them.
		if pinned && !existing.pinned {
			existing.pinned = true
		}

		return
	}

	g.registrations[componentName] = &componentRegistration{typ: typ, suffix: suffix, pinned: pinned}
}

// componentRef builds a $ref to componentName and remembers the schema it put
// it in, so that if the final pass renames the component every reference to it
// moves with it. Refs are plain strings scattered across the finished
// document; tracking the pointers at the moment of creation is cheaper and far
// less error-prone than walking the document afterwards looking for them.
func (g *schemaGenerator) componentRef(componentName string) *Schema {
	ref := &Schema{Ref: componentRefPrefix + componentName}

	g.trackRef(componentName, ref)

	return ref
}

// trackRef registers an externally created $ref schema for rewriting.
func (g *schemaGenerator) trackRef(componentName string, ref *Schema) {
	if ref == nil || componentName == "" {
		return
	}

	g.refSites[componentName] = append(g.refSites[componentName], ref)
}

// registerComponent stores schema under a collision-free name derived from typ
// and returns a $ref to it. This is the single door through which whole-type
// components enter the components map: five call sites used to assign into
// that map directly, and a second type with the same bare name simply
// overwrote the first.
func (g *schemaGenerator) registerComponent(typ reflect.Type, suffix string, schema *Schema) *Schema {
	if typ == nil || schema == nil {
		return schema
	}

	if typ.Kind() == reflect.Ptr {
		typ = typ.Elem()
	}

	componentName := g.resolveComponentNameWithSuffix(typ, suffix)
	if componentName == "" || g.components == nil {
		return schema
	}

	g.components[componentName] = schema

	return g.componentRef(componentName)
}

// registerPinnedComponent stores schema under a name the user chose explicitly.
// The name is taken as given -- it is never renamed by the final pass -- but it
// is still recorded so other types know it is taken.
func (g *schemaGenerator) registerPinnedComponent(componentName string, typ reflect.Type, schema *Schema) *Schema {
	if componentName == "" || schema == nil || g.components == nil {
		return schema
	}

	g.components[componentName] = schema
	g.noteComponent(componentName, typ, "", true)

	return g.componentRef(componentName)
}

// beginSpec resets the per-document state: the $ref sites recorded for the
// previous document, and the schemas built for it.
//
// Clearing the schemas is what keeps a rename honest. A component schema built
// during an earlier call carries $refs this call never saw, so if a route
// registered since then introduces a collision, those references would not
// move with the component they point at and the document would ship a dangling
// $ref. Everything is reachable from the routes, so rebuilding costs one pass.
//
// The type registry deliberately survives: it is what keeps a component's name
// identical from one call to the next.
func (g *schemaGenerator) beginSpec() {
	g.refSites = make(map[string][]*Schema)

	// Cleared in place: the components map is the one the spec points at.
	clear(g.components)
	clear(g.schemas)
}

// componentRename is one name change applied by the final pass.
type componentRename struct {
	from      string
	to        string
	qualified string
}

// finalizeComponentNames assigns every registered component its final name and
// rewrites the components map, the type registries and every recorded $ref.
//
// Naming happens in two phases because a name cannot be judged until the whole
// set is known: a type only learns that its bare name is contested when the
// second claimant shows up, which may be many routes later. During generation
// each type therefore gets a provisional, guaranteed-unique name; here, with
// the full set in hand, the final names are derived from the set alone.
//
// The rules:
//
//   - A bare name claimed by exactly one type stays exactly as it was. This is
//     the compatibility guarantee: component names appear in every generated
//     client and every checked-in openapi.json, so a type that does not collide
//     must never be renamed.
//   - A bare name claimed by two or more types is burned: nobody gets it, and
//     every claimant falls to its first free namespaced candidate. Handing it to
//     whoever registered first would make the document depend on route order and
//     would let an unrelated new type rename an existing component.
//   - Names the user pinned are reserved and never rewritten.
//
// It is idempotent: running it again over already-final names produces the
// same assignment and no renames.
func (g *schemaGenerator) finalizeComponentNames() []componentRename {
	if len(g.registrations) == 0 {
		return nil
	}

	provisionalNames := slices.Sorted(maps.Keys(g.registrations))

	reserved := make(map[string]bool, len(provisionalNames))
	burned := make(map[string]bool)
	groups := make(map[string][]string)

	// Anything already occupying a name that this pass did not hand out -- a
	// schema put into the components map directly, say -- is untouchable, so
	// no qualified name may be assigned on top of it.
	for name := range g.components {
		if _, ours := g.registrations[name]; !ours {
			reserved[name] = true
		}
	}

	for name := range g.typeRegistry {
		if _, ours := g.registrations[name]; !ours {
			reserved[name] = true
		}
	}

	for _, name := range provisionalNames {
		reg := g.registrations[name]
		if reg.pinned {
			reserved[name] = true

			continue
		}

		base := reg.baseName()
		groups[base] = append(groups[base], name)
	}

	bases := slices.Sorted(maps.Keys(groups))
	final := make(map[string]string, len(provisionalNames))

	// Phase 1: uncontested bare names are kept, and reserved before any
	// qualified name is chosen, so qualification can never displace them.
	for _, base := range bases {
		members := groups[base]
		if len(members) > 1 {
			burned[base] = true

			continue
		}

		if reserved[base] {
			continue // A pinned name owns it; this member qualifies in phase 2.
		}

		final[members[0]] = base
		reserved[base] = true
	}

	// Phase 2: everyone left walks their candidate ladder.
	for _, base := range bases {
		members := groups[base]

		sort.Slice(members, func(i, j int) bool {
			return g.registrations[members[i]].sortKey() < g.registrations[members[j]].sortKey()
		})

		for _, name := range members {
			if _, done := final[name]; done {
				continue
			}

			chosen := pickComponentName(g.registrations[name], reserved, burned)
			final[name] = chosen
			reserved[chosen] = true
		}
	}

	renames := make([]componentRename, 0)

	for _, name := range provisionalNames {
		target := final[name]
		if target == "" || target == name {
			continue
		}

		reg := g.registrations[name]

		renames = append(renames, componentRename{
			from:      name,
			to:        target,
			qualified: getQualifiedTypeName(reg.typ) + reg.suffix,
		})
	}

	// Report by contested bare name rather than by rename: a claimant whose
	// provisional name already happened to be its final one is still part of
	// the collision, and a report that omitted it would be a half-truth.
	contests := make([]nameContest, 0)

	for _, base := range bases {
		members := groups[base]
		if len(members) < 2 {
			continue
		}

		contest := nameContest{base: base}

		for _, name := range members {
			reg := g.registrations[name]
			contest.members = append(contest.members, contestMember{
				qualified: getQualifiedTypeName(reg.typ) + reg.suffix,
				final:     final[name],
			})
		}

		contests = append(contests, contest)
	}

	g.applyRenames(renames)
	g.reportContests(contests)

	return renames
}

// nameContest is one bare component name that more than one type wanted.
type nameContest struct {
	base    string
	members []contestMember
}

type contestMember struct {
	qualified string
	final     string
}

// pickComponentName returns the first candidate name that is neither already
// taken nor burned by a collision.
func pickComponentName(reg *componentRegistration, reserved, burned map[string]bool) string {
	candidates := reg.candidates()
	for _, candidate := range candidates {
		if !reserved[candidate] && !burned[candidate] {
			return candidate
		}
	}

	// Every candidate is spoken for, including the fully qualified form. Two
	// distinct types cannot produce the same qualified form, so this can only
	// happen if sanitisation mapped two different paths onto one string.
	base := candidates[len(candidates)-1]
	for i := 2; ; i++ {
		candidate := base + "_" + strconv.Itoa(i)
		if !reserved[candidate] && !burned[candidate] {
			return candidate
		}
	}
}

// applyRenames moves component schemas, registry entries and $refs onto the
// final names. Renames are applied as a batch -- every source key removed
// before any destination key is written -- because one type's final name can
// be another type's provisional name.
func (g *schemaGenerator) applyRenames(renames []componentRename) {
	if len(renames) == 0 {
		return
	}

	movedComponents := make(map[string]*Schema, len(renames))
	movedSchemas := make(map[string]*Schema, len(renames))
	movedRefs := make(map[string][]*Schema, len(renames))
	movedRegs := make(map[string]*componentRegistration, len(renames))

	for _, r := range renames {
		if s, ok := g.components[r.from]; ok {
			movedComponents[r.to] = s
		}

		if s, ok := g.schemas[r.from]; ok {
			movedSchemas[r.to] = s
		}

		if refs, ok := g.refSites[r.from]; ok {
			movedRefs[r.to] = refs
		}

		if reg, ok := g.registrations[r.from]; ok {
			movedRegs[r.to] = reg
		}
	}

	for _, r := range renames {
		delete(g.components, r.from)
		delete(g.schemas, r.from)
		delete(g.refSites, r.from)
		delete(g.registrations, r.from)
		delete(g.typeRegistry, r.from)
	}

	maps.Copy(g.components, movedComponents)
	maps.Copy(g.schemas, movedSchemas)
	maps.Copy(g.refSites, movedRefs)
	maps.Copy(g.registrations, movedRegs)

	for _, r := range renames {
		reg := g.registrations[r.to]
		if reg == nil {
			continue
		}

		key := registryKey(getQualifiedTypeName(reg.typ), reg.suffix)
		g.typeRegistry[r.to] = key
		g.reverseRegistry[key] = r.to

		for _, ref := range g.refSites[r.to] {
			ref.Ref = componentRefPrefix + r.to
		}
	}
}

// reportPinnedConflict warns that an explicitly chosen component name is
// claimed by two types. Qualification cannot resolve this one -- an explicit
// name is honoured as given -- so one schema does overwrite the other, and the
// only useful thing to do is say so with both type names.
func (g *schemaGenerator) reportPinnedConflict(componentName string, existing, incoming reflect.Type) {
	if g.reportedContests[componentName] {
		return
	}

	msg := fmt.Sprintf(
		"openapi: component name %q is claimed explicitly by two types, %s and %s;"+
			" an explicit name is never rewritten, so one schema overwrites the other -- rename one of them",
		componentName, getQualifiedTypeName(existing), getQualifiedTypeName(incoming))

	g.reportedContests[componentName] = true
	g.nameCollisions = append(g.nameCollisions, msg)

	if g.logger != nil {
		g.logger.Warn(msg)
	}
}

// reportContests logs every qualification. A silent rename is how this class of
// bug survives: the document is wrong in a way that only shows up as a type
// mismatch at runtime, far from the cause. Each contested bare name produces
// one line naming every type that wanted it and the name each one received.
//
// The router has no equivalent of the client generator's spec.Warnings, so the
// report goes to the router's logger, and is also kept on the generator in
// nameCollisions so it survives a nil logger and can be asserted on.
//
// Each contest is reported once: the spec is regenerated on every request to
// the spec endpoint, and repeating the warning per request would bury it.
func (g *schemaGenerator) reportContests(contests []nameContest) {
	for _, contest := range contests {
		if g.reportedContests[contest.base] {
			continue
		}

		members := contest.members
		sort.Slice(members, func(i, j int) bool { return members[i].qualified < members[j].qualified })

		parts := make([]string, 0, len(members))
		for _, m := range members {
			parts = append(parts, fmt.Sprintf("%s -> %q", m.qualified, m.final))
		}

		msg := fmt.Sprintf(
			"openapi: component name %q is claimed by %d types; each was qualified so that every schema survives: %s",
			contest.base, len(members), strings.Join(parts, ", "))

		g.reportedContests[contest.base] = true
		g.nameCollisions = append(g.nameCollisions, msg)

		if g.logger != nil {
			g.logger.Warn(msg)
		}
	}
}
