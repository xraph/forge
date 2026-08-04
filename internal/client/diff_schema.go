package client

import (
	"fmt"
	"sort"
	"strings"
)

const (
	// maxFieldDepth bounds how deep flattenFields descends into nested
	// objects. Six levels is deeper than any hand-written payload and keeps a
	// pathological (or accidentally recursive) schema from producing an
	// unbounded field list.
	maxFieldDepth = 6

	// maxTypeDepth bounds classifyTypeChange's recursion through arrays and
	// references. Past it the answer is UNKNOWN rather than a guess.
	maxTypeDepth = 4
)

// fieldEntry is one leaf or branch in a flattened body schema: the schema at
// that path and whether its immediate parent declared it required.
type fieldEntry struct {
	Schema   *Schema
	Required bool
}

// flattenFields turns a body schema into a path -> schema map, so two bodies
// can be compared field by field at every depth rather than only at the top
// level.
//
// Paths read the way a developer would write them: "customer.address.city" for
// nested objects, "items[].sku" for a field inside an array element. An array
// body flattens as "[].field", which is what a list response looks like.
//
// References are resolved, with the set of references already open on the
// current path tracked so a self-referential schema (Order -> Customer ->
// Orders[] -> Order, which any ORM with eager loading produces) terminates
// instead of expanding forever.
func flattenFields(spec *APISpec, schema *Schema) map[string]fieldEntry {
	out := make(map[string]fieldEntry)
	if schema == nil {
		return out
	}

	flattenInto(spec, schema, "", 0, map[string]bool{}, out)

	return out
}

func flattenInto(spec *APISpec, schema *Schema, prefix string, depth int, open map[string]bool, out map[string]fieldEntry) {
	if schema == nil || depth > maxFieldDepth {
		return
	}

	if schema.Ref != "" {
		if open[schema.Ref] {
			return
		}

		open[schema.Ref] = true
		defer delete(open, schema.Ref)

		resolved := resolveRef(spec, schema)
		if resolved == nil {
			return
		}

		schema = resolved
	}

	// allOf composes into the same object, so its members flatten at the same
	// prefix rather than under a synthetic one.
	for _, member := range schema.AllOf {
		flattenInto(spec, member, prefix, depth+1, open, out)
	}

	if schema.Items != nil {
		flattenInto(spec, schema.Items, prefix+"[]", depth+1, open, out)
	}

	if len(schema.Properties) == 0 {
		return
	}

	required := make(map[string]bool, len(schema.Required))
	for _, name := range schema.Required {
		required[name] = true
	}

	for _, name := range sortedKeys(schema.Properties) {
		property := schema.Properties[name]

		path := name
		if prefix != "" {
			path = prefix + "." + name
		}

		out[path] = fieldEntry{Schema: property, Required: required[name]}

		flattenInto(spec, property, path, depth+1, open, out)
	}
}

// resolveRef follows a reference to the schema it names, one spec at a time.
// Returns nil when the reference cannot be resolved -- an unresolvable
// reference is a spec defect, and guessing past it would produce a diff that
// claims fields vanished when they were only unreachable.
func resolveRef(spec *APISpec, schema *Schema) *Schema {
	if spec == nil || schema == nil || schema.Ref == "" {
		return schema
	}

	seen := map[string]bool{}
	current := schema

	for current != nil && current.Ref != "" {
		if seen[current.Ref] {
			return nil
		}

		seen[current.Ref] = true

		current = spec.ResolveSchemaRef(current.Ref)
	}

	return current
}

// refDisplayName extracts the component name from a reference for display and
// for comparing two references by target.
//
// Deliberately lenient where filter.go's refName is strict: that one returns ""
// for anything that is not a local component reference, which is right for
// pruning but wrong here -- two different remote references would both collapse
// to "" and compare equal, reporting "no change" for a target that moved.
func refDisplayName(ref string) string {
	if idx := strings.LastIndex(ref, "/"); idx >= 0 {
		return ref[idx+1:]
	}

	return ref
}

type typeResult int

const (
	typeSame typeResult = iota
	typeWidened
	typeNarrowed
	typeUnknown
)

// typeVerdict is one type comparison's outcome plus the sentence that explains
// it. The reason is not decoration: an UNKNOWN with no explanation is
// indistinguishable from a bug in the differ.
type typeVerdict struct {
	result             typeResult
	reason             string
	oldValue, newValue string
}

// classifyTypeChange decides whether a schema widened, narrowed, stayed the
// same, or changed in a way this differ will not pretend to understand.
//
// What it classifies:
//   - integer <-> number (widened / narrowed)
//   - a type becoming untyped, or an untyped one gaining a type
//   - nullability gained (widened) or lost (narrowed)
//   - enum members added (widened), removed (narrowed), an enum dropped
//     entirely (widened), or one introduced where none existed (narrowed)
//   - a format dropped (widened) or added (narrowed)
//   - numeric and length bounds loosened (widened) or tightened (narrowed)
//   - a pattern dropped (widened) or added (narrowed)
//   - arrays, by recursing into the item schema
//   - oneOf/anyOf member sets, by comparing member signatures
//
// What it refuses to classify, reporting UNKNOWN with the reason:
//   - unrelated type changes (string -> object, object -> array, ...)
//   - one format replaced by another (uuid -> email): neither contains the
//     other, and pretending otherwise would be a guess
//   - one pattern replaced by another: comparing two regular expressions for
//     containment is not something to do casually inside a differ
//   - a union appearing on only one side, or a union whose members both gained
//     and lost entries
//   - a reference that cannot be resolved, or nesting deeper than maxTypeDepth
//
// When several signals disagree, a narrowing wins over an unknown, which wins
// over a widening. The order is deliberate: the report should never be quieter
// than the most alarming thing it found.
func classifyTypeChange(oldSpec *APISpec, oldSchema *Schema, newSpec *APISpec, newSchema *Schema, depth int) typeVerdict {
	if oldSchema == nil && newSchema == nil {
		return typeVerdict{result: typeSame}
	}

	if oldSchema == nil || newSchema == nil {
		return typeVerdict{result: typeUnknown, reason: "schema present on only one side"}
	}

	if depth > maxTypeDepth {
		return typeVerdict{result: typeUnknown, reason: "nested deeper than the differ inspects"}
	}

	// References: identical target names are treated as unchanged here because
	// the referenced object's own fields are compared by flattenFields. Only a
	// change of target needs a decision.
	if oldSchema.Ref != "" || newSchema.Ref != "" {
		if oldSchema.Ref != "" && newSchema.Ref != "" {
			if refDisplayName(oldSchema.Ref) == refDisplayName(newSchema.Ref) {
				return typeVerdict{result: typeSame}
			}

			oldResolved := resolveRef(oldSpec, oldSchema)
			newResolved := resolveRef(newSpec, newSchema)

			if oldResolved == nil || newResolved == nil {
				return typeVerdict{
					result:   typeUnknown,
					reason:   fmt.Sprintf("reference changed %s -> %s and could not be resolved", refDisplayName(oldSchema.Ref), refDisplayName(newSchema.Ref)),
					oldValue: refDisplayName(oldSchema.Ref),
					newValue: refDisplayName(newSchema.Ref),
				}
			}

			inner := classifyTypeChange(oldSpec, oldResolved, newSpec, newResolved, depth+1)
			if inner.result == typeSame {
				// Same underlying shape under a different component name. The
				// wire format is unchanged; if that renamed component is an
				// entity, diffSpecEntities reports the cache consequence.
				return typeVerdict{result: typeSame}
			}

			return inner
		}

		// A reference on one side and an inline schema on the other: resolve
		// and compare the shapes.
		oldResolved := resolveRef(oldSpec, oldSchema)
		newResolved := resolveRef(newSpec, newSchema)

		if oldResolved == nil || newResolved == nil {
			return typeVerdict{result: typeUnknown, reason: "reference could not be resolved"}
		}

		return classifyTypeChange(oldSpec, oldResolved, newSpec, newResolved, depth+1)
	}

	var verdicts []typeVerdict

	verdicts = append(verdicts, classifyBaseType(oldSchema, newSchema))
	verdicts = append(verdicts, classifyNullability(oldSchema, newSchema))
	verdicts = append(verdicts, classifyEnum(oldSchema, newSchema))
	verdicts = append(verdicts, classifyFormat(oldSchema, newSchema))
	verdicts = append(verdicts, classifyBounds(oldSchema, newSchema))
	verdicts = append(verdicts, classifyPattern(oldSchema, newSchema))
	verdicts = append(verdicts, classifyUnion(oldSchema, newSchema))

	if oldSchema.Items != nil && newSchema.Items != nil {
		inner := classifyTypeChange(oldSpec, oldSchema.Items, newSpec, newSchema.Items, depth+1)
		if inner.result != typeSame {
			inner.reason = "array element " + inner.reason
			verdicts = append(verdicts, inner)
		}
	}

	return combineVerdicts(verdicts)
}

func combineVerdicts(verdicts []typeVerdict) typeVerdict {
	out := typeVerdict{result: typeSame}

	var (
		reasons              []string
		oldValues, newValues []string
	)

	worst := typeSame

	for _, v := range verdicts {
		if v.result == typeSame {
			continue
		}

		reasons = append(reasons, v.reason)

		if v.oldValue != "" {
			oldValues = append(oldValues, v.oldValue)
		}

		if v.newValue != "" {
			newValues = append(newValues, v.newValue)
		}

		if severityOf(v.result) > severityOf(worst) {
			worst = v.result
		}
	}

	if worst == typeSame {
		return out
	}

	out.result = worst
	out.reason = strings.Join(reasons, "; ")
	out.oldValue = strings.Join(oldValues, ", ")
	out.newValue = strings.Join(newValues, ", ")

	return out
}

// severityOf orders the results so combineVerdicts can pick the loudest.
func severityOf(r typeResult) int {
	switch r {
	case typeSame:
		return 0
	case typeWidened:
		return 1
	case typeUnknown:
		return 2
	case typeNarrowed:
		return 3
	default:
		return 0
	}
}

func classifyBaseType(oldSchema, newSchema *Schema) typeVerdict {
	oldType, newType := oldSchema.Type, newSchema.Type

	if oldType == newType {
		return typeVerdict{result: typeSame}
	}

	switch {
	case oldType == "integer" && newType == "number":
		return typeVerdict{result: typeWidened, reason: "type widened integer -> number", oldValue: "integer", newValue: "number"}

	case oldType == "number" && newType == "integer":
		return typeVerdict{result: typeNarrowed, reason: "type narrowed number -> integer", oldValue: "number", newValue: "integer"}

	case oldType != "" && newType == "":
		return typeVerdict{result: typeWidened, reason: "type widened " + oldType + " -> any", oldValue: oldType, newValue: "any"}

	case oldType == "" && newType != "":
		return typeVerdict{result: typeNarrowed, reason: "type narrowed any -> " + newType, oldValue: "any", newValue: newType}

	default:
		return typeVerdict{
			result:   typeUnknown,
			reason:   fmt.Sprintf("type changed %s -> %s, which is neither a widening nor a narrowing", oldType, newType),
			oldValue: oldType,
			newValue: newType,
		}
	}
}

func classifyNullability(oldSchema, newSchema *Schema) typeVerdict {
	switch {
	case !oldSchema.Nullable && newSchema.Nullable:
		return typeVerdict{result: typeWidened, reason: "became nullable"}
	case oldSchema.Nullable && !newSchema.Nullable:
		return typeVerdict{result: typeNarrowed, reason: "is no longer nullable"}
	default:
		return typeVerdict{result: typeSame}
	}
}

func enumValues(s *Schema) []string {
	out := make([]string, 0, len(s.Enum))
	for _, v := range s.Enum {
		out = append(out, fmt.Sprintf("%v", v))
	}

	sort.Strings(out)

	return out
}

func classifyEnum(oldSchema, newSchema *Schema) typeVerdict {
	oldEnum, newEnum := enumValues(oldSchema), enumValues(newSchema)

	switch {
	case len(oldEnum) == 0 && len(newEnum) == 0:
		return typeVerdict{result: typeSame}

	case len(oldEnum) > 0 && len(newEnum) == 0:
		return typeVerdict{result: typeWidened, reason: "enum constraint removed", oldValue: strings.Join(oldEnum, "|")}

	case len(oldEnum) == 0 && len(newEnum) > 0:
		return typeVerdict{result: typeNarrowed, reason: "enum constraint added", newValue: strings.Join(newEnum, "|")}
	}

	newSet := make(map[string]bool, len(newEnum))
	for _, v := range newEnum {
		newSet[v] = true
	}

	oldSet := make(map[string]bool, len(oldEnum))
	for _, v := range oldEnum {
		oldSet[v] = true
	}

	var removed, added []string

	for _, v := range oldEnum {
		if !newSet[v] {
			removed = append(removed, v)
		}
	}

	for _, v := range newEnum {
		if !oldSet[v] {
			added = append(added, v)
		}
	}

	switch {
	case len(removed) == 0 && len(added) == 0:
		return typeVerdict{result: typeSame}

	case len(removed) > 0:
		return typeVerdict{
			result:   typeNarrowed,
			reason:   "enum values removed: " + strings.Join(removed, ", "),
			oldValue: strings.Join(oldEnum, "|"),
			newValue: strings.Join(newEnum, "|"),
		}

	default:
		return typeVerdict{
			result:   typeWidened,
			reason:   "enum values added: " + strings.Join(added, ", "),
			oldValue: strings.Join(oldEnum, "|"),
			newValue: strings.Join(newEnum, "|"),
		}
	}
}

func classifyFormat(oldSchema, newSchema *Schema) typeVerdict {
	switch {
	case oldSchema.Format == newSchema.Format:
		return typeVerdict{result: typeSame}

	case oldSchema.Format != "" && newSchema.Format == "":
		return typeVerdict{result: typeWidened, reason: "format " + oldSchema.Format + " removed", oldValue: oldSchema.Format}

	case oldSchema.Format == "" && newSchema.Format != "":
		return typeVerdict{result: typeNarrowed, reason: "format " + newSchema.Format + " added", newValue: newSchema.Format}

	default:
		return typeVerdict{
			result:   typeUnknown,
			reason:   fmt.Sprintf("format changed %s -> %s; neither contains the other", oldSchema.Format, newSchema.Format),
			oldValue: oldSchema.Format,
			newValue: newSchema.Format,
		}
	}
}

// classifyBounds compares numeric and length constraints. A bound that appears
// or tightens narrows the type; one that disappears or loosens widens it.
func classifyBounds(oldSchema, newSchema *Schema) typeVerdict {
	var verdicts []typeVerdict

	verdicts = append(verdicts, classifyLowerBound("minimum", oldSchema.Minimum, newSchema.Minimum))
	verdicts = append(verdicts, classifyUpperBound("maximum", oldSchema.Maximum, newSchema.Maximum))
	verdicts = append(verdicts, classifyLowerBound("minLength", intPtrAsFloat(oldSchema.MinLength), intPtrAsFloat(newSchema.MinLength)))
	verdicts = append(verdicts, classifyUpperBound("maxLength", intPtrAsFloat(oldSchema.MaxLength), intPtrAsFloat(newSchema.MaxLength)))

	return combineVerdicts(verdicts)
}

func intPtrAsFloat(v *int) *float64 {
	if v == nil {
		return nil
	}

	f := float64(*v)

	return &f
}

func formatBound(v *float64) string {
	if v == nil {
		return ""
	}

	return strings.TrimSuffix(strings.TrimRight(fmt.Sprintf("%f", *v), "0"), ".")
}

// classifyLowerBound handles constraints where a HIGHER value accepts less.
func classifyLowerBound(name string, oldBound, newBound *float64) typeVerdict {
	switch {
	case oldBound == nil && newBound == nil:
		return typeVerdict{result: typeSame}

	case oldBound == nil:
		return typeVerdict{result: typeNarrowed, reason: name + " " + formatBound(newBound) + " added", newValue: formatBound(newBound)}

	case newBound == nil:
		return typeVerdict{result: typeWidened, reason: name + " " + formatBound(oldBound) + " removed", oldValue: formatBound(oldBound)}

	case *newBound > *oldBound:
		return typeVerdict{
			result:   typeNarrowed,
			reason:   fmt.Sprintf("%s raised %s -> %s", name, formatBound(oldBound), formatBound(newBound)),
			oldValue: formatBound(oldBound),
			newValue: formatBound(newBound),
		}

	case *newBound < *oldBound:
		return typeVerdict{
			result:   typeWidened,
			reason:   fmt.Sprintf("%s lowered %s -> %s", name, formatBound(oldBound), formatBound(newBound)),
			oldValue: formatBound(oldBound),
			newValue: formatBound(newBound),
		}

	default:
		return typeVerdict{result: typeSame}
	}
}

// classifyUpperBound handles constraints where a LOWER value accepts less.
func classifyUpperBound(name string, oldBound, newBound *float64) typeVerdict {
	switch {
	case oldBound == nil && newBound == nil:
		return typeVerdict{result: typeSame}

	case oldBound == nil:
		return typeVerdict{result: typeNarrowed, reason: name + " " + formatBound(newBound) + " added", newValue: formatBound(newBound)}

	case newBound == nil:
		return typeVerdict{result: typeWidened, reason: name + " " + formatBound(oldBound) + " removed", oldValue: formatBound(oldBound)}

	case *newBound < *oldBound:
		return typeVerdict{
			result:   typeNarrowed,
			reason:   fmt.Sprintf("%s lowered %s -> %s", name, formatBound(oldBound), formatBound(newBound)),
			oldValue: formatBound(oldBound),
			newValue: formatBound(newBound),
		}

	case *newBound > *oldBound:
		return typeVerdict{
			result:   typeWidened,
			reason:   fmt.Sprintf("%s raised %s -> %s", name, formatBound(oldBound), formatBound(newBound)),
			oldValue: formatBound(oldBound),
			newValue: formatBound(newBound),
		}

	default:
		return typeVerdict{result: typeSame}
	}
}

func classifyPattern(oldSchema, newSchema *Schema) typeVerdict {
	switch {
	case oldSchema.Pattern == newSchema.Pattern:
		return typeVerdict{result: typeSame}

	case oldSchema.Pattern != "" && newSchema.Pattern == "":
		return typeVerdict{result: typeWidened, reason: "pattern removed", oldValue: oldSchema.Pattern}

	case oldSchema.Pattern == "" && newSchema.Pattern != "":
		return typeVerdict{result: typeNarrowed, reason: "pattern added", newValue: newSchema.Pattern}

	default:
		return typeVerdict{
			result:   typeUnknown,
			reason:   "pattern changed; comparing two regular expressions for containment is not something this differ will guess at",
			oldValue: oldSchema.Pattern,
			newValue: newSchema.Pattern,
		}
	}
}

// unionMembers returns a sorted signature of a oneOf/anyOf member list.
func unionMembers(s *Schema) []string {
	members := s.OneOf
	if len(members) == 0 {
		members = s.AnyOf
	}

	out := make([]string, 0, len(members))

	for _, m := range members {
		if m == nil {
			continue
		}

		switch {
		case m.Ref != "":
			out = append(out, refDisplayName(m.Ref))
		case m.Type != "":
			out = append(out, m.Type)
		default:
			out = append(out, "any")
		}
	}

	sort.Strings(out)

	return out
}

func classifyUnion(oldSchema, newSchema *Schema) typeVerdict {
	oldMembers, newMembers := unionMembers(oldSchema), unionMembers(newSchema)

	switch {
	case len(oldMembers) == 0 && len(newMembers) == 0:
		return typeVerdict{result: typeSame}

	case len(oldMembers) == 0 || len(newMembers) == 0:
		return typeVerdict{
			result:   typeUnknown,
			reason:   "union appears on only one side; the two shapes are not comparable member by member",
			oldValue: strings.Join(oldMembers, "|"),
			newValue: strings.Join(newMembers, "|"),
		}
	}

	oldSet := make(map[string]bool, len(oldMembers))
	for _, m := range oldMembers {
		oldSet[m] = true
	}

	newSet := make(map[string]bool, len(newMembers))
	for _, m := range newMembers {
		newSet[m] = true
	}

	var removed, added []string

	for _, m := range oldMembers {
		if !newSet[m] {
			removed = append(removed, m)
		}
	}

	for _, m := range newMembers {
		if !oldSet[m] {
			added = append(added, m)
		}
	}

	switch {
	case len(removed) == 0 && len(added) == 0:
		return typeVerdict{result: typeSame}

	case len(removed) > 0 && len(added) > 0:
		return typeVerdict{
			result:   typeUnknown,
			reason:   "union members both added and removed: " + strings.Join(added, ", ") + " in, " + strings.Join(removed, ", ") + " out",
			oldValue: strings.Join(oldMembers, "|"),
			newValue: strings.Join(newMembers, "|"),
		}

	case len(removed) > 0:
		return typeVerdict{
			result:   typeNarrowed,
			reason:   "union members removed: " + strings.Join(removed, ", "),
			oldValue: strings.Join(oldMembers, "|"),
			newValue: strings.Join(newMembers, "|"),
		}

	default:
		return typeVerdict{
			result:   typeWidened,
			reason:   "union members added: " + strings.Join(added, ", "),
			oldValue: strings.Join(oldMembers, "|"),
			newValue: strings.Join(newMembers, "|"),
		}
	}
}
