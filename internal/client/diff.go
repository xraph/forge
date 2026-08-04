package client

import (
	"fmt"
	"sort"
	"strings"
)

// ChangeKind is the classification bucket a single difference falls into.
//
// The three buckets are not severities on one axis. BreakingAPI is an HTTP
// contract break: a request the old client sends is now rejected, or a field it
// reads is gone. BreakingCache is a break in the *identity* contract the
// normalized client cache is built on, and it is invisible to every other
// OpenAPI differ because nothing about the wire format changes. Renaming an
// entity from Order to PurchaseOrder leaves every request and response
// byte-identical while repartitioning the entire cache: a persisted store still
// holding "Order:" keys becomes unreachable, and a client that is mid-session
// normalizes one record under two identities. It surfaces as a rendering defect
// three screens away from the rename, which is why it gets its own column
// rather than a footnote.
//
// Unknown exists so the differ can decline. A schema change this code cannot
// prove is a widening or a narrowing is reported as unknown rather than guessed
// at: a differ that silently misclassifies is worse than one that admits it
// does not know, because the first trains people to trust it.
type ChangeKind string

const (
	ChangeCompatible    ChangeKind = "COMPATIBLE"
	ChangeBreakingAPI   ChangeKind = "BREAKING (API)"
	ChangeBreakingCache ChangeKind = "BREAKING (CACHE)"
	ChangeUnknown       ChangeKind = "UNKNOWN"
)

// rank orders the kinds for display: the things that break come first, the
// things that need a human next, the safe additions last.
func (k ChangeKind) rank() int {
	switch k {
	case ChangeBreakingAPI:
		return 0
	case ChangeBreakingCache:
		return 1
	case ChangeUnknown:
		return 2
	case ChangeCompatible:
		return 3
	default:
		return 4
	}
}

// Change categories. Kept as constants because they are part of the --format
// json contract a CI job parses.
const (
	CategoryEndpoint      = "endpoint"
	CategoryParameter     = "parameter"
	CategoryRequestField  = "request-field"
	CategoryResponse      = "response"
	CategoryResponseField = "response-field"
	CategoryEntity        = "entity"
	CategoryCacheTag      = "cache-tag"
	CategoryStream        = "stream"
	CategoryStreamBinding = "stream-binding"
)

// Change is one classified difference.
type Change struct {
	Kind     ChangeKind `json:"kind"`
	Category string     `json:"category"`
	Subject  string     `json:"subject"`
	Detail   string     `json:"detail"`
	Old      string     `json:"old,omitempty"`
	New      string     `json:"new,omitempty"`
}

// DiffReport is the complete classification of one spec pair.
type DiffReport struct {
	Changes []Change    `json:"changes"`
	Summary DiffSummary `json:"summary"`
}

// DiffSummary counts each bucket, so a CI job can gate without walking the
// change list.
type DiffSummary struct {
	Compatible    int `json:"compatible"`
	BreakingAPI   int `json:"breaking_api"`
	BreakingCache int `json:"breaking_cache"`
	Unknown       int `json:"unknown"`
	Total         int `json:"total"`
}

// HasBreaking reports whether either breaking bucket is non-empty.
func (r DiffReport) HasBreaking() bool {
	return r.Summary.BreakingAPI > 0 || r.Summary.BreakingCache > 0
}

// HasUnknown reports whether anything was left unclassified. Callers gate on
// this separately from HasBreaking: an unknown is not proof of a break, but it
// is proof that a human has to look.
func (r DiffReport) HasUnknown() bool {
	return r.Summary.Unknown > 0
}

// diffBuilder accumulates changes and holds both specs so schema references can
// be resolved against the side they came from.
type diffBuilder struct {
	oldSpec *APISpec
	newSpec *APISpec
	changes []Change
}

func (b *diffBuilder) add(kind ChangeKind, category, subject, detail string) {
	b.changes = append(b.changes, Change{Kind: kind, Category: category, Subject: subject, Detail: detail})
}

func (b *diffBuilder) addValues(kind ChangeKind, category, subject, detail, oldValue, newValue string) {
	b.changes = append(b.changes, Change{
		Kind:     kind,
		Category: category,
		Subject:  subject,
		Detail:   detail,
		Old:      oldValue,
		New:      newValue,
	})
}

// DiffSpecs classifies every difference between two parsed specifications.
//
// The output is fully sorted: this report gets pasted into pull requests and
// diffed against previous runs, so two runs over the same pair of specs must
// produce byte-identical output regardless of Go's map iteration order.
func DiffSpecs(oldSpec, newSpec *APISpec) DiffReport {
	b := &diffBuilder{oldSpec: oldSpec, newSpec: newSpec}

	b.diffEndpoints()
	b.diffSpecEntities()
	b.diffStreams()

	sort.SliceStable(b.changes, func(i, j int) bool {
		a, c := b.changes[i], b.changes[j]

		if a.Kind.rank() != c.Kind.rank() {
			return a.Kind.rank() < c.Kind.rank()
		}

		if a.Subject != c.Subject {
			return a.Subject < c.Subject
		}

		if a.Category != c.Category {
			return a.Category < c.Category
		}

		return a.Detail < c.Detail
	})

	report := DiffReport{Changes: b.changes}
	for _, c := range b.changes {
		switch c.Kind {
		case ChangeCompatible:
			report.Summary.Compatible++
		case ChangeBreakingAPI:
			report.Summary.BreakingAPI++
		case ChangeBreakingCache:
			report.Summary.BreakingCache++
		case ChangeUnknown:
			report.Summary.Unknown++
		}
	}

	report.Summary.Total = len(b.changes)

	return report
}

// endpointKey identifies an operation across two specs. Method plus path, not
// operationId: an operationId rename is a client-side rename, while a path or
// method change is a different endpoint entirely.
func endpointKey(method, path string) string {
	return strings.ToUpper(method) + " " + path
}

func indexEndpoints(spec *APISpec) map[string]*Endpoint {
	out := make(map[string]*Endpoint, len(spec.Endpoints))

	for i := range spec.Endpoints {
		ep := &spec.Endpoints[i]

		key := endpointKey(ep.Method, ep.Path)
		if _, exists := out[key]; exists {
			continue // first declaration wins; a duplicate is a spec defect, not a diff
		}

		out[key] = ep
	}

	return out
}

func sortedKeys[T any](m map[string]T) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}

	sort.Strings(keys)

	return keys
}

func (b *diffBuilder) diffEndpoints() {
	oldEndpoints := indexEndpoints(b.oldSpec)
	newEndpoints := indexEndpoints(b.newSpec)

	union := make(map[string]struct{}, len(oldEndpoints)+len(newEndpoints))
	for k := range oldEndpoints {
		union[k] = struct{}{}
	}

	for k := range newEndpoints {
		union[k] = struct{}{}
	}

	for _, key := range sortedKeys(union) {
		oldEP, inOld := oldEndpoints[key]
		newEP, inNew := newEndpoints[key]

		switch {
		case !inOld:
			b.add(ChangeCompatible, CategoryEndpoint, key, "added endpoint")
		case !inNew:
			b.add(ChangeBreakingAPI, CategoryEndpoint, key, "removed endpoint")
		default:
			b.diffParameters(key, oldEP, newEP)
			b.diffRequestBody(key, oldEP, newEP)
			b.diffResponses(key, oldEP, newEP)
			b.diffEndpointCache(key, oldEP, newEP)
		}
	}
}

// bodySchema picks the media type a generated client actually builds a type
// from: JSON if it is offered, otherwise the lexicographically first content
// type so the choice is deterministic rather than map-order dependent.
func bodySchema(content map[string]*MediaType) *Schema {
	if len(content) == 0 {
		return nil
	}

	if mt, ok := content["application/json"]; ok && mt != nil {
		return mt.Schema
	}

	for _, ct := range sortedKeys(content) {
		if strings.Contains(ct, "json") && content[ct] != nil {
			return content[ct].Schema
		}
	}

	for _, ct := range sortedKeys(content) {
		if content[ct] != nil {
			return content[ct].Schema
		}
	}

	return nil
}

// parameterKey identifies a parameter across two specs.
//
// The location is part of the identity, not decoration: "tenant" in the query
// string and "tenant" in a header are two different parameters that happen to
// share a name, and collapsing them would report a spurious removal-plus-
// addition pair whenever both exist, or -- worse -- silently compare one
// against the other and call a move "no change".
func parameterKey(in, name string) string {
	return in + " " + name
}

// indexParameters flattens an endpoint's three parameter collections into one
// map keyed by location and name.
//
// The collection a parameter arrived in supplies its location when the
// parameter itself does not carry one, so a spec that omits `in` (or a parser
// path that does not fill it) still lands in the right bucket rather than under
// an empty location where it would match nothing on the other side.
func indexParameters(ep *Endpoint) map[string]Parameter {
	out := make(map[string]Parameter, len(ep.PathParams)+len(ep.QueryParams)+len(ep.HeaderParams))

	add := func(params []Parameter, defaultIn string) {
		for _, p := range params {
			in := p.In
			if in == "" {
				in = defaultIn
			}

			key := parameterKey(in, p.Name)
			if _, exists := out[key]; exists {
				continue // first declaration wins; a duplicate is a spec defect
			}

			out[key] = p
		}
	}

	add(ep.PathParams, "path")
	add(ep.QueryParams, "query")
	add(ep.HeaderParams, "header")

	return out
}

// diffParameters classifies path, query and header parameter changes.
//
// These are request inputs exactly as much as body fields are, and they are
// where the most routine breaking change of all lives: adding a required query
// parameter. For a generated client that is a hard signature break -- the
// regenerated method takes an argument the old one did not -- so it is
// classified by the same rules diffFieldSets applies to body fields, including
// the removal rule and its reasoning (see the comment there: the regenerated
// type loses the parameter and every caller that passed it stops compiling).
func (b *diffBuilder) diffParameters(key string, oldEP, newEP *Endpoint) {
	oldParams := indexParameters(oldEP)
	newParams := indexParameters(newEP)

	union := make(map[string]struct{}, len(oldParams)+len(newParams))
	for k := range oldParams {
		union[k] = struct{}{}
	}

	for k := range newParams {
		union[k] = struct{}{}
	}

	for _, paramKey := range sortedKeys(union) {
		oldParam, inOld := oldParams[paramKey]
		newParam, inNew := newParams[paramKey]

		switch {
		case !inOld:
			if newParam.Required {
				b.add(ChangeBreakingAPI, CategoryParameter, key,
					fmt.Sprintf("added required %s parameter %q", locationOf(newParam, paramKey), newParam.Name))
			} else {
				b.add(ChangeCompatible, CategoryParameter, key,
					fmt.Sprintf("added optional %s parameter %q", locationOf(newParam, paramKey), newParam.Name))
			}

		case !inNew:
			b.add(ChangeBreakingAPI, CategoryParameter, key,
				fmt.Sprintf("removed %s parameter %q", locationOf(oldParam, paramKey), oldParam.Name))

		default:
			location := locationOf(newParam, paramKey)

			if !oldParam.Required && newParam.Required {
				b.add(ChangeBreakingAPI, CategoryParameter, key,
					fmt.Sprintf("%s parameter %q became required", location, newParam.Name))
			}

			if oldParam.Required && !newParam.Required {
				b.add(ChangeCompatible, CategoryParameter, key,
					fmt.Sprintf("%s parameter %q became optional", location, newParam.Name))
			}

			verdict := classifyTypeChange(b.oldSpec, oldParam.Schema, b.newSpec, newParam.Schema, 0)
			if verdict.result == typeSame {
				continue
			}

			kind := ChangeUnknown

			switch verdict.result {
			case typeWidened:
				kind = ChangeCompatible
			case typeNarrowed:
				kind = ChangeBreakingAPI
			}

			b.addValues(kind, CategoryParameter, key,
				fmt.Sprintf("%s parameter %q: %s", location, newParam.Name, verdict.reason),
				verdict.oldValue, verdict.newValue)
		}
	}
}

// locationOf reports a parameter's location for display, falling back to the
// location embedded in its key when the parameter itself does not carry one.
func locationOf(param Parameter, key string) string {
	if param.In != "" {
		return param.In
	}

	if in, _, found := strings.Cut(key, " "); found {
		return in
	}

	return "request"
}

func (b *diffBuilder) diffRequestBody(key string, oldEP, newEP *Endpoint) {
	var oldSchema, newSchema *Schema

	if oldEP.RequestBody != nil {
		oldSchema = bodySchema(oldEP.RequestBody.Content)
	}

	if newEP.RequestBody != nil {
		newSchema = bodySchema(newEP.RequestBody.Content)
	}

	b.diffBodyRoot(key, "request", CategoryRequestField, oldSchema, newSchema)

	oldFields := flattenFields(b.oldSpec, oldSchema)
	newFields := flattenFields(b.newSpec, newSchema)

	b.diffFieldSets(key, "request", CategoryRequestField, oldFields, newFields, true)
}

// diffBodyRoot classifies a change to the body schema itself, which no
// field-level comparison sees: a response that was a bare string and is now an
// integer has no fields on either side, so without this the differ would report
// nothing at all for it.
//
// A body that exists on only one side is deliberately NOT skipped here. It used
// to be, and the effect was that deleting a response's entire content block --
// the client stops getting a payload at all -- printed "No changes" and exited
// 0. classifyTypeChange already has a verdict for exactly this shape; the job
// here is to let it be reached.
func (b *diffBuilder) diffBodyRoot(subject, where, category string, oldSchema, newSchema *Schema) {
	verdict := classifyTypeChange(b.oldSpec, oldSchema, b.newSpec, newSchema, 0)
	if verdict.result == typeSame {
		return
	}

	kind := ChangeUnknown

	switch verdict.result {
	case typeWidened:
		kind = ChangeCompatible
	case typeNarrowed:
		kind = ChangeBreakingAPI
	}

	b.addValues(kind, category, subject,
		fmt.Sprintf("%s body: %s", where, verdict.reason), verdict.oldValue, verdict.newValue)
}

func (b *diffBuilder) diffResponses(key string, oldEP, newEP *Endpoint) {
	codes := make(map[int]struct{}, len(oldEP.Responses)+len(newEP.Responses))
	for code := range oldEP.Responses {
		codes[code] = struct{}{}
	}

	for code := range newEP.Responses {
		codes[code] = struct{}{}
	}

	ordered := make([]int, 0, len(codes))
	for code := range codes {
		ordered = append(ordered, code)
	}

	sort.Ints(ordered)

	for _, code := range ordered {
		oldResp, inOld := oldEP.Responses[code]
		newResp, inNew := newEP.Responses[code]

		switch {
		case !inOld:
			b.add(ChangeCompatible, CategoryResponse, key, fmt.Sprintf("added response %d", code))

			continue
		case !inNew:
			b.add(ChangeBreakingAPI, CategoryResponse, key, fmt.Sprintf("removed response %d", code))

			continue
		}

		var oldSchema, newSchema *Schema

		if oldResp != nil {
			oldSchema = bodySchema(oldResp.Content)
		}

		if newResp != nil {
			newSchema = bodySchema(newResp.Content)
		}

		b.diffBodyRoot(key, fmt.Sprintf("response %d", code), CategoryResponseField, oldSchema, newSchema)

		oldFields := flattenFields(b.oldSpec, oldSchema)
		newFields := flattenFields(b.newSpec, newSchema)

		b.diffFieldSets(key, fmt.Sprintf("response %d", code), CategoryResponseField, oldFields, newFields, false)
	}
}

// diffFieldSets compares two flattened field maps.
//
// isRequest changes only the requiredness rules, not the type rules. A field
// that becomes required is a break for a request and irrelevant for a response;
// a field that disappears is a break either way -- from a response because the
// client reads it, and from a request because the generated request type loses
// it and every caller that sets it stops compiling. That second case is not in
// the design's table, which only lists response-field removal; it is included
// here because for a *generated* client the compile break is just as real.
//
// The type rules deliberately follow the design table literally: widened is
// compatible, narrowed is breaking, in both directions. Note the asymmetry this
// glosses over -- widening a response type is compatible on the wire but still
// widens the client's generated type, and narrowing a request type only bites
// callers that were sending the wider value. The table is the contract this
// implements; the nuance is recorded here rather than silently re-decided.
func (b *diffBuilder) diffFieldSets(subject, where, category string, oldFields, newFields map[string]fieldEntry, isRequest bool) {
	union := make(map[string]struct{}, len(oldFields)+len(newFields))
	for k := range oldFields {
		union[k] = struct{}{}
	}

	for k := range newFields {
		union[k] = struct{}{}
	}

	for _, path := range sortedKeys(union) {
		oldField, inOld := oldFields[path]
		newField, inNew := newFields[path]

		switch {
		case !inOld:
			if isRequest && newField.Required {
				b.add(ChangeBreakingAPI, category, subject,
					fmt.Sprintf("added required %s field %q", where, path))
			} else if isRequest {
				b.add(ChangeCompatible, category, subject,
					fmt.Sprintf("added optional %s field %q", where, path))
			} else {
				b.add(ChangeCompatible, category, subject,
					fmt.Sprintf("added %s field %q", where, path))
			}

		case !inNew:
			b.add(ChangeBreakingAPI, category, subject,
				fmt.Sprintf("removed %s field %q", where, path))

		default:
			if isRequest && !oldField.Required && newField.Required {
				b.add(ChangeBreakingAPI, category, subject,
					fmt.Sprintf("%s field %q became required", where, path))
			}

			if isRequest && oldField.Required && !newField.Required {
				b.add(ChangeCompatible, category, subject,
					fmt.Sprintf("%s field %q became optional", where, path))
			}

			verdict := classifyTypeChange(b.oldSpec, oldField.Schema, b.newSpec, newField.Schema, 0)
			if verdict.result == typeSame {
				continue
			}

			kind := ChangeUnknown

			switch verdict.result {
			case typeWidened:
				kind = ChangeCompatible
			case typeNarrowed:
				kind = ChangeBreakingAPI
			}

			b.addValues(kind, category, subject,
				fmt.Sprintf("%s field %q: %s", where, path, verdict.reason),
				verdict.oldValue, verdict.newValue)
		}
	}
}

// diffEndpointCache classifies the cache-identity half of an endpoint change.
//
// None of these touch the HTTP contract. Every one of them repartitions the
// client's normalized store, and an API-only differ reports all of them as "no
// change" -- see the ChangeKind doc comment for why that is the expensive kind
// of silence.
func (b *diffBuilder) diffEndpointCache(key string, oldEP, newEP *Endpoint) {
	switch {
	case oldEP.Entity == nil && newEP.Entity != nil:
		b.addValues(ChangeBreakingCache, CategoryEntity, key,
			fmt.Sprintf("operation is now an entity (%s), responses that were cached as documents are now normalized", newEP.Entity.Type),
			"", entityString(newEP.Entity))

	case oldEP.Entity != nil && newEP.Entity == nil:
		b.addValues(ChangeBreakingCache, CategoryEntity, key,
			fmt.Sprintf("entity %s is no longer an entity, its records leave the normalized store", oldEP.Entity.Type),
			entityString(oldEP.Entity), "")

	case oldEP.Entity != nil && newEP.Entity != nil:
		if oldEP.Entity.Type != newEP.Entity.Type {
			b.addValues(ChangeBreakingCache, CategoryEntity, key,
				fmt.Sprintf("entity typename changed %s -> %s, every persisted %s: key becomes unreachable",
					oldEP.Entity.Type, newEP.Entity.Type, oldEP.Entity.Type),
				oldEP.Entity.Type, newEP.Entity.Type)
		}

		if oldEP.Entity.IDField != newEP.Entity.IDField {
			b.addValues(ChangeBreakingCache, CategoryEntity, key,
				fmt.Sprintf("entity %s id field changed %s -> %s, cached records key on a field that no longer identifies them",
					newEP.Entity.Type, oldEP.Entity.IDField, newEP.Entity.IDField),
				oldEP.Entity.IDField, newEP.Entity.IDField)
		}
	}

	b.diffTagList(key, "provides", oldEP.CacheTags.Provides, newEP.CacheTags.Provides)
	b.diffTagList(key, "invalidates", oldEP.CacheTags.Invalidates, newEP.CacheTags.Invalidates)
}

// diffTagList reports tag removals as cache-breaking and additions as
// compatible.
//
// A rename shows up here as a removal plus an addition, and the removal is the
// half that matters: a write whose invalidates tag disappeared no longer
// refetches the collection it changed, and a read whose provides tag
// disappeared is never refreshed by the write that invalidates it. Both leave
// the user looking at data the server no longer has. An added tag only causes
// extra refetching, which is a performance question, not a correctness one.
func (b *diffBuilder) diffTagList(subject, which string, oldTags, newTags []string) {
	oldSet := make(map[string]bool, len(oldTags))
	for _, t := range oldTags {
		oldSet[t] = true
	}

	newSet := make(map[string]bool, len(newTags))
	for _, t := range newTags {
		newSet[t] = true
	}

	removed := make([]string, 0)

	for _, t := range oldTags {
		if !newSet[t] {
			removed = append(removed, t)
		}
	}

	added := make([]string, 0)

	for _, t := range newTags {
		if !oldSet[t] {
			added = append(added, t)
		}
	}

	sort.Strings(removed)
	sort.Strings(added)

	for _, t := range removed {
		b.addValues(ChangeBreakingCache, CategoryCacheTag, subject,
			fmt.Sprintf("%s tag %q removed", which, t), t, "")
	}

	for _, t := range added {
		b.addValues(ChangeCompatible, CategoryCacheTag, subject,
			fmt.Sprintf("%s tag %q added", which, t), "", t)
	}
}

func entityString(e *EntityRef) string {
	if e == nil {
		return ""
	}

	return e.Type + ":{" + e.IDField + "}"
}

// diffSpecEntities compares the spec-level entity table.
//
// This catches a rename that no single endpoint reveals -- a type that is
// declared in one spec and simply absent from the other, because every
// operation that returned it moved to the new name at the same time.
func (b *diffBuilder) diffSpecEntities() {
	union := make(map[string]struct{}, len(b.oldSpec.Entities)+len(b.newSpec.Entities))
	for k := range b.oldSpec.Entities {
		union[k] = struct{}{}
	}

	for k := range b.newSpec.Entities {
		union[k] = struct{}{}
	}

	for _, name := range sortedKeys(union) {
		oldEntity, inOld := b.oldSpec.Entities[name]
		newEntity, inNew := b.newSpec.Entities[name]

		switch {
		case !inOld:
			b.addValues(ChangeCompatible, CategoryEntity, "entity "+name,
				"new entity type declared", "", entityString(newEntity))

		case !inNew:
			b.addValues(ChangeBreakingCache, CategoryEntity, "entity "+name,
				fmt.Sprintf("entity type %s is gone; a persisted store still holding %s: keys cannot reach them", name, name),
				entityString(oldEntity), "")

		default:
			if oldEntity == nil || newEntity == nil {
				continue
			}

			if oldEntity.IDField != newEntity.IDField {
				b.addValues(ChangeBreakingCache, CategoryEntity, "entity "+name,
					fmt.Sprintf("id field changed %s -> %s", oldEntity.IDField, newEntity.IDField),
					oldEntity.IDField, newEntity.IDField)
			}
		}
	}
}

// streamEndpoint is the shape both WebSocket and SSE endpoints reduce to for
// diffing purposes: a path and a set of cache bindings.
type streamEndpoint struct {
	kind     string
	path     string
	bindings []StreamBinding
}

func indexStreams(spec *APISpec) map[string]streamEndpoint {
	out := make(map[string]streamEndpoint, len(spec.WebSockets)+len(spec.SSEs))

	for _, ws := range spec.WebSockets {
		key := "WS " + ws.Path
		if _, exists := out[key]; !exists {
			out[key] = streamEndpoint{kind: "WebSocket", path: ws.Path, bindings: ws.StreamBindings}
		}
	}

	for _, sse := range spec.SSEs {
		key := "SSE " + sse.Path
		if _, exists := out[key]; !exists {
			out[key] = streamEndpoint{kind: "SSE", path: sse.Path, bindings: sse.StreamBindings}
		}
	}

	return out
}

func (b *diffBuilder) diffStreams() {
	oldStreams := indexStreams(b.oldSpec)
	newStreams := indexStreams(b.newSpec)

	union := make(map[string]struct{}, len(oldStreams)+len(newStreams))
	for k := range oldStreams {
		union[k] = struct{}{}
	}

	for k := range newStreams {
		union[k] = struct{}{}
	}

	for _, key := range sortedKeys(union) {
		oldStream, inOld := oldStreams[key]
		newStream, inNew := newStreams[key]

		switch {
		case !inOld:
			b.add(ChangeCompatible, CategoryStream, key, "added "+newStream.kind+" endpoint")
		case !inNew:
			b.add(ChangeBreakingAPI, CategoryStream, key, "removed "+oldStream.kind+" endpoint")
		default:
			b.diffStreamBindings(key, oldStream.bindings, newStream.bindings)
		}
	}
}

// diffStreamBindings compares the per-message cache bindings of one stream.
//
// A binding is the streaming half of the same identity contract an endpoint's
// Entity carries: it says which entity type a pushed message updates and what
// it invalidates. Changing the entity type on a binding repartitions the cache
// exactly the way renaming an entity on a REST response does, and dropping a
// binding means a live message stops updating the store at all -- the screen
// keeps showing the pre-message value until something else happens to refetch.
func (b *diffBuilder) diffStreamBindings(subject string, oldBindings, newBindings []StreamBinding) {
	oldByMessage := make(map[string]StreamBinding, len(oldBindings))
	for _, sb := range oldBindings {
		if _, exists := oldByMessage[sb.Message]; !exists {
			oldByMessage[sb.Message] = sb
		}
	}

	newByMessage := make(map[string]StreamBinding, len(newBindings))
	for _, sb := range newBindings {
		if _, exists := newByMessage[sb.Message]; !exists {
			newByMessage[sb.Message] = sb
		}
	}

	union := make(map[string]struct{}, len(oldByMessage)+len(newByMessage))
	for k := range oldByMessage {
		union[k] = struct{}{}
	}

	for k := range newByMessage {
		union[k] = struct{}{}
	}

	for _, message := range sortedKeys(union) {
		oldBinding, inOld := oldByMessage[message]
		newBinding, inNew := newByMessage[message]

		switch {
		case !inOld:
			b.add(ChangeCompatible, CategoryStreamBinding, subject,
				fmt.Sprintf("added cache binding for message %q", message))

		case !inNew:
			b.addValues(ChangeBreakingCache, CategoryStreamBinding, subject,
				fmt.Sprintf("removed cache binding for message %q; pushes stop updating the store", message),
				oldBinding.EntityType, "")

		default:
			if oldBinding.EntityType != newBinding.EntityType {
				b.addValues(ChangeBreakingCache, CategoryStreamBinding, subject,
					fmt.Sprintf("message %q entity type changed %s -> %s", message, oldBinding.EntityType, newBinding.EntityType),
					oldBinding.EntityType, newBinding.EntityType)
			}

			if oldBinding.Intent != newBinding.Intent {
				b.addValues(ChangeBreakingCache, CategoryStreamBinding, subject,
					fmt.Sprintf("message %q intent changed %s -> %s", message, oldBinding.Intent, newBinding.Intent),
					string(oldBinding.Intent), string(newBinding.Intent))
			}

			b.diffTagList(subject+" "+message, "invalidates", oldBinding.Invalidates, newBinding.Invalidates)
		}
	}
}
