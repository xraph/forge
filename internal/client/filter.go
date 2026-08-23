package client

import (
	"fmt"
	"path"
	"sort"
	"strings"
)

// PathFilter selects which endpoints a generated client covers.
//
// It exists because a specification is usually larger than the API any one
// consumer talks to. A service that mounts an auth engine, an admin dashboard
// and its own domain routes publishes all three from one document, and a
// client generated over the whole thing buries the twenty endpoints a caller
// wants under the two hundred it must never touch.
//
// Filtering is a generation-time concern rather than a serving-time one: the
// server is right to publish everything it serves, and the client is right to
// bind only what it consumes.
type PathFilter struct {
	// Include keeps only the endpoints matching at least one pattern. Empty
	// means every endpoint is a candidate.
	Include []string

	// Exclude drops endpoints matching any pattern, and is applied after
	// Include so that a narrow exclusion can carve a hole in a broad include.
	Exclude []string
}

// FilterResult reports what a filter did, so a caller can say so rather than
// silently generating a smaller client than the operator expected.
type FilterResult struct {
	// KeptEndpoints and DroppedEndpoints count operations, not paths: one path
	// with a GET and a DELETE is two endpoints and they filter together.
	KeptEndpoints    int
	DroppedEndpoints int

	// KeptStreams and DroppedStreams count channels across all three stream
	// families together. They are separate from the endpoint counts because a
	// filter selecting only channels is a real client: callers reject a filter
	// that matched nothing, and endpoints alone are not the whole of what a
	// filter can match.
	KeptStreams    int
	DroppedStreams int

	// KeptSchemas and DroppedSchemas count component schemas after pruning.
	KeptSchemas    int
	DroppedSchemas int

	// KeptEntities and DroppedEntities count rows of the cache metadata
	// table, entities and routing types together, because that is how they
	// reach the generated client: one table, one row per typename.
	KeptEntities    int
	DroppedEntities int

	// KeptTags and DroppedTags count document-level tag declarations, kept
	// when some surviving operation or channel carries the name.
	KeptTags    int
	DroppedTags int

	// DroppedPaths lists the distinct paths removed, sorted, for reporting.
	DroppedPaths []string
}

// Empty reports whether the filter would do anything at all.
func (f PathFilter) Empty() bool {
	return len(f.Include) == 0 && len(f.Exclude) == 0
}

// Apply filters the spec in place -- endpoints, streams and the tag list --
// then prunes everything keyed by a typename that no survivor can reach: the
// component schemas, the entity table and the routing table.
//
// Pruning matters as much as the endpoint filter, and see pruneUnreachable for
// why it matters twice -- the second half is cache metadata that ships to a
// browser rather than types that stop at a compiler.
func (s *APISpec) Apply(f PathFilter) FilterResult {
	result := FilterResult{}

	if f.Empty() {
		result.KeptEndpoints = len(s.Endpoints)
		result.KeptStreams = len(s.WebSockets) + len(s.SSEs) + len(s.WebTransports)
		result.KeptSchemas = len(s.Schemas)
		result.KeptEntities = len(s.Entities) + len(s.RoutingTypes)
		result.KeptTags = len(s.Tags)

		return result
	}

	kept := make([]Endpoint, 0, len(s.Endpoints))
	dropped := make(map[string]struct{})

	for _, endpoint := range s.Endpoints {
		if f.allows(endpoint.Path) {
			kept = append(kept, endpoint)

			continue
		}

		dropped[endpoint.Path] = struct{}{}
		result.DroppedEndpoints++
	}

	s.Endpoints = kept
	result.KeptEndpoints = len(kept)

	s.filterStreams(f, &result, dropped)

	tagsBefore := len(s.Tags)
	s.filterTags()
	result.KeptTags = len(s.Tags)
	result.DroppedTags = tagsBefore - result.KeptTags

	for p := range dropped {
		result.DroppedPaths = append(result.DroppedPaths, p)
	}

	sort.Strings(result.DroppedPaths)

	schemasBefore := len(s.Schemas)
	entitiesBefore := len(s.Entities) + len(s.RoutingTypes)

	s.pruneUnreachable()

	result.KeptSchemas = len(s.Schemas)
	result.DroppedSchemas = schemasBefore - result.KeptSchemas
	result.KeptEntities = len(s.Entities) + len(s.RoutingTypes)
	result.DroppedEntities = entitiesBefore - result.KeptEntities

	return result
}

// filterStreams narrows the three stream families by the same path rule the
// endpoints use.
//
// A channel is a per-service surface exactly as a route is. One gateway
// fronting three services publishes all their channels from one document, and
// a client generated for one of them was carrying the other two's: the
// generated streams table listed every channel in the gateway, and the codec
// table carried every message schema behind them.
//
// The three families are counted together and their paths join the endpoints'
// DroppedPaths, because a path is a path -- an operator reading "dropped
// /admin/ws/audit" does not need to be told which transport served it.
func (s *APISpec) filterStreams(f PathFilter, result *FilterResult, dropped map[string]struct{}) {
	drop := func(p string) {
		dropped[p] = struct{}{}
		result.DroppedStreams++
	}

	websockets := make([]WebSocketEndpoint, 0, len(s.WebSockets))

	for _, ws := range s.WebSockets {
		if f.allows(ws.Path) {
			websockets = append(websockets, ws)

			continue
		}

		drop(ws.Path)
	}

	s.WebSockets = websockets

	sses := make([]SSEEndpoint, 0, len(s.SSEs))

	for _, sse := range s.SSEs {
		if f.allows(sse.Path) {
			sses = append(sses, sse)

			continue
		}

		drop(sse.Path)
	}

	s.SSEs = sses

	webtransports := make([]WebTransportEndpoint, 0, len(s.WebTransports))

	for _, wt := range s.WebTransports {
		if f.allows(wt.Path) {
			webtransports = append(webtransports, wt)

			continue
		}

		drop(wt.Path)
	}

	s.WebTransports = webtransports

	result.KeptStreams = len(s.WebSockets) + len(s.SSEs) + len(s.WebTransports)
}

// filterTags drops the document-level tag declarations that nothing surviving
// the filter carries.
//
// The list is joined straight into the README's API overview as a description
// of what the client covers, so it has to describe THIS client. A gateway
// declares one tag per service it fronts; without this every package claims
// all of them, and the twenty-three-endpoint portal client's README says it
// speaks Studio and TwinOS.
//
// Filtering is strict: a tag no surviving operation or channel carries is
// gone, even if that leaves the list empty. There is no reachability to err
// toward here the way there is for an entity row. A missing entity row breaks
// caching silently; a missing tag omits one line of a README, and the overview
// already omits that line when the list is empty.
//
// Declaration order and descriptions survive untouched. They are the
// document's, and the filter's business is which tags appear, not how.
func (s *APISpec) filterTags() {
	if len(s.Tags) == 0 {
		return
	}

	used := make(map[string]struct{}, len(s.Tags))

	mark := func(tags []string) {
		for _, tag := range tags {
			used[tag] = struct{}{}
		}
	}

	for i := range s.Endpoints {
		mark(s.Endpoints[i].Tags)
	}

	for i := range s.WebSockets {
		mark(s.WebSockets[i].Tags)
	}

	for i := range s.SSEs {
		mark(s.SSEs[i].Tags)
	}

	for i := range s.WebTransports {
		mark(s.WebTransports[i].Tags)
	}

	kept := make([]Tag, 0, len(s.Tags))

	for _, tag := range s.Tags {
		if _, ok := used[tag.Name]; ok {
			kept = append(kept, tag)
		}
	}

	s.Tags = kept
}

// Summary renders what the filter did as one line, or "" when it did nothing
// worth saying.
//
// This exists because narrowing a client is the kind of change that is only
// obvious to whoever wrote the pattern. An operator who fat-fingers an include
// gets a package that builds, publishes and calls a quarter of what they
// meant, and the only signal today is the absence of a hook they were not
// looking for yet. The entity count earns its place here more than the rest:
// dropping a hundred and thirty-six rows of cache metadata is invisible in
// every other output this command produces.
func (r FilterResult) Summary() string {
	if r.DroppedEndpoints == 0 && r.DroppedStreams == 0 &&
		r.DroppedSchemas == 0 && r.DroppedEntities == 0 && r.DroppedTags == 0 {
		return ""
	}

	parts := make([]string, 0, 5)

	// A category appears when the document had any of it, not when the filter
	// took some away. Reporting only what shrank is how a client of channels
	// and no routes came out as "kept 0/1 endpoints", which reads like the
	// filter matched nothing at all -- while the channel it was written for
	// sat there, kept, and unmentioned.
	add := func(kept, dropped int, noun string) {
		if kept+dropped > 0 {
			parts = append(parts, fmt.Sprintf("%d/%d %s", kept, kept+dropped, noun))
		}
	}

	add(r.KeptEndpoints, r.DroppedEndpoints, "endpoints")
	add(r.KeptStreams, r.DroppedStreams, "streams")
	add(r.KeptSchemas, r.DroppedSchemas, "schemas")
	add(r.KeptEntities, r.DroppedEntities, "entity rows")
	add(r.KeptTags, r.DroppedTags, "tags")

	return "Path filter kept " + strings.Join(parts, ", ")
}

// allows reports whether a path survives the filter.
func (f PathFilter) allows(p string) bool {
	if len(f.Include) > 0 && !matchesAny(f.Include, p) {
		return false
	}

	return !matchesAny(f.Exclude, p)
}

func matchesAny(patterns []string, p string) bool {
	for _, pattern := range patterns {
		if matchPath(pattern, p) {
			return true
		}
	}

	return false
}

// matchPath matches a path against one pattern.
//
// Two forms are accepted, because operators reach for both and guessing wrong
// is a silently empty client:
//
//   - a path prefix: "/identity" matches "/identity" and "/identity/login" but
//     not "/identity-provider", since the boundary is a path separator rather
//     than a character count;
//   - a glob: "/api/*/health" matches through one segment, and a trailing
//     "/**" matches any depth. Plain path.Match is not enough on its own — its
//     "*" never crosses a separator, so "/api/*" would miss "/api/v1/models",
//     which is the pattern everyone writes first.
func matchPath(pattern, p string) bool {
	if pattern == "" {
		return false
	}

	pattern = strings.TrimSuffix(pattern, "/")
	if pattern == "" {
		// "/" alone: the root prefix, which is every path.
		return true
	}

	if pattern == p {
		return true
	}

	// Recursive glob: "/api/**" is the prefix form written explicitly.
	if base, ok := strings.CutSuffix(pattern, "/**"); ok {
		return p == base || strings.HasPrefix(p, base+"/")
	}

	// Prefix, on a segment boundary.
	if strings.HasPrefix(p, pattern+"/") {
		return true
	}

	if ok, err := path.Match(pattern, p); err == nil && ok {
		return true
	}

	return false
}

// pruneUnreachable drops the component schemas, entity rows and routing rows
// that nothing surviving the filter can reach.
//
// Pruning matters as much as the endpoint filter, and it matters twice.
//
// Component schemas generate a type each, so a spec whose auth engine
// contributes a hundred and forty of them yields a types file that is mostly
// unreachable from the client's own surface -- the endpoints look filtered
// while the types plainly are not.
//
// The entity and routing tables are the same failure one file over, and a
// worse one, because they ship to a browser rather than to a compiler. They
// are the normalization metadata the runtime reads at runtime: one row per
// typename saying what identifies it and which of its properties lead to
// another row. Left unpruned, a client binding twenty-three endpoints carries
// the rows for every entity in the document -- a hundred and thirty-odd of
// them -- almost none of which any of its endpoints can return. Nothing is
// wrong with the rows; they are simply weight the consumer downloads to never
// read, and they defeat the point of splitting one gateway document into
// per-service clients at all.
//
// REACHABILITY IS TRANSITIVE AND IS COMPUTED ONCE FOR ALL THREE. If an
// endpoint returns Order and Order has a `customer` property referencing
// Customer, both belong in the table: the cache normalizes nested references,
// so a missing Customer row does not shrink the table, it silently stops
// dependency tracking through `order.customer`. That is exactly the graph the
// $ref walk below already follows for schemas, which is why the entity tables
// prune against its result rather than against a second traversal that could
// disagree with it.
//
// The bias throughout is toward keeping a row. A row that survives and is
// never read costs bytes; a row that is dropped and was needed costs a cache
// entry that never matches, with nothing anywhere to say so.
func (s *APISpec) pruneUnreachable() {
	if len(s.Schemas) == 0 && len(s.Entities) == 0 && len(s.RoutingTypes) == 0 {
		return
	}

	reachable := s.reachableNames()

	for name := range s.Schemas {
		if _, ok := reachable[name]; !ok {
			delete(s.Schemas, name)
		}
	}

	pruneEntityTable(s.Entities, reachable)
	pruneEntityTable(s.RoutingTypes, reachable)
}

// pruneEntityTable drops the rows whose typename nothing reaches.
//
// The rows' Fields are deliberately left as they are. An edge can only point
// at a name the walk did not mark if that name was never reachable in the
// first place, and a dangling edge is inert -- the runtime looks the target up
// in this same table, finds no row, and stops descending. Rewriting the edges
// to match would mean recomputing them, and recomputing them is how a filtered
// client and an unfiltered one start disagreeing about a graph neither of them
// changed.
func pruneEntityTable(table map[string]*EntityRef, reachable map[string]struct{}) {
	for name := range table {
		if _, ok := reachable[name]; !ok {
			delete(table, name)
		}
	}
}

// reachableNames returns every component name some surviving root reaches,
// following $ref transitively through properties, items, the polymorphic
// combinators and additionalProperties.
//
// The roots are the endpoints that survived the filter AND every stream,
// because Apply narrows Endpoints and nothing else: a websocket or SSE channel
// is not path-filtered, so all of them survive and everything they carry is
// still reachable. Pruning against the endpoints alone would delete the
// message schemas those channels decode and the entity rows their bindings
// name, leaving a streams[] entry that can never normalize.
//
// Names are also pushed directly, not only through refs. An endpoint's
// RootType and Entity.Type are typenames rather than references, and a stream
// binding names its entity the same way -- `Emits[Order]` records the string
// "Order". Some of those name a component and some name a type the document
// only ever describes inline; pushing both kinds keeps the second kind's
// entity row, which has no schema to prove its reachability and would
// otherwise be dropped for want of evidence.
func (s *APISpec) reachableNames() map[string]struct{} {
	reachable := make(map[string]struct{}, len(s.Schemas))

	var (
		walk func(schema *Schema)
		push func(name string)
	)

	// push marks a name and expands the component behind it, if there is one.
	// It is the only writer, so it is also what terminates the walk: a
	// self-referential schema -- a tree node, a linked list -- and an ordinary
	// bidirectional association both come back to a name already marked.
	push = func(name string) {
		if name == "" {
			return
		}

		if _, seen := reachable[name]; seen {
			return
		}

		reachable[name] = struct{}{}

		walk(s.Schemas[name])
	}

	walk = func(schema *Schema) {
		if schema == nil {
			return
		}

		push(refTargetName(schema.Ref))

		for _, prop := range schema.Properties {
			walk(prop)
		}

		walk(schema.Items)

		for _, sub := range schema.OneOf {
			walk(sub)
		}

		for _, sub := range schema.AnyOf {
			walk(sub)
		}

		for _, sub := range schema.AllOf {
			walk(sub)
		}

		if nested, ok := schema.AdditionalProperties.(*Schema); ok {
			walk(nested)
		}

		// A discriminator names schemas that no property references directly;
		// dropping them would leave a union that cannot resolve its variants.
		if schema.Discriminator != nil {
			for _, ref := range schema.Discriminator.Mapping {
				push(refTargetName(ref))
			}
		}
	}

	walkParams := func(params []Parameter) {
		for _, param := range params {
			walk(param.Schema)
		}
	}

	walkBindings := func(bindings []StreamBinding) {
		for _, b := range bindings {
			push(b.EntityType)
		}
	}

	for i := range s.Endpoints {
		endpoint := &s.Endpoints[i]

		walkParams(endpoint.PathParams)
		walkParams(endpoint.QueryParams)
		walkParams(endpoint.HeaderParams)

		if endpoint.RequestBody != nil {
			for _, media := range endpoint.RequestBody.Content {
				walk(media.Schema)
			}
		}

		for _, resp := range endpoint.Responses {
			walkResponse(resp, walk)
		}

		walkResponse(endpoint.DefaultError, walk)

		push(endpoint.RootType)

		if endpoint.Entity != nil {
			push(endpoint.Entity.Type)
		}
	}

	for i := range s.WebSockets {
		ws := &s.WebSockets[i]

		walkParams(ws.Parameters)
		walk(ws.SendSchema)
		walk(ws.ReceiveSchema)

		for _, schema := range ws.MessageTypes {
			walk(schema)
		}

		walkBindings(ws.StreamBindings)
	}

	for i := range s.SSEs {
		sse := &s.SSEs[i]

		for _, schema := range sse.EventSchemas {
			walk(schema)
		}

		walkBindings(sse.StreamBindings)
	}

	for i := range s.WebTransports {
		wt := &s.WebTransports[i]

		walkStream(wt.UniStreamSchema, walk)
		walkStream(wt.BiStreamSchema, walk)
		walk(wt.DatagramSchema)
	}

	s.walkStreamingFeatures(walk, walkParams)

	return reachable
}

// walkStreamingFeatures reaches the schemas the AsyncAPI streaming extensions
// contribute. They hang off StreamingSpec rather than off any endpoint, so no
// other root reaches them.
func (s *APISpec) walkStreamingFeatures(walk func(*Schema), walkParams func([]Parameter)) {
	if s.Streaming == nil {
		return
	}

	if rooms := s.Streaming.Rooms; rooms != nil {
		walkParams(rooms.Parameters)

		for _, schema := range []*Schema{
			rooms.JoinSchema, rooms.LeaveSchema, rooms.SendSchema, rooms.ReceiveSchema,
			rooms.MemberJoinSchema, rooms.MemberLeaveSchema, rooms.HistorySchema,
		} {
			walk(schema)
		}
	}

	if presence := s.Streaming.Presence; presence != nil {
		walk(presence.UpdateSchema)
		walk(presence.EventSchema)
	}

	if typing := s.Streaming.Typing; typing != nil {
		walkParams(typing.Parameters)
		walk(typing.StartSchema)
		walk(typing.StopSchema)
	}

	if channels := s.Streaming.Channels; channels != nil {
		walkParams(channels.Parameters)

		for _, schema := range []*Schema{
			channels.SubscribeSchema, channels.UnsubscribeSchema,
			channels.PublishSchema, channels.MessageSchema,
		} {
			walk(schema)
		}
	}
}

func walkResponse(resp *Response, walk func(*Schema)) {
	if resp == nil {
		return
	}

	for _, media := range resp.Content {
		walk(media.Schema)
	}

	for _, header := range resp.Headers {
		walk(header.Schema)
	}
}

func walkStream(stream *StreamSchema, walk func(*Schema)) {
	if stream == nil {
		return
	}

	walk(stream.SendSchema)
	walk(stream.ReceiveSchema)
}
