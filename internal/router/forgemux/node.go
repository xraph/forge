// Package forgemux is forge's in-house route matcher.
//
// It is a segment-level trie rather than a compressed radix tree. Prefix
// compression saves memory at route counts forge will not reach, and every
// bug it introduces is one that has to be found here rather than upstream.
// Segment nodes map one to one onto pathspec.Segment, so insertion is close
// to transcription.
//
// This package may import internal/pathspec and internal/shared, and nothing
// else in this repository. internal/router imports it, so any dependency
// back the other way is a cycle.
package forgemux

import (
	"fmt"
	"net/http"
	"slices"
	"sort"
	"strconv"

	"github.com/xraph/forge/internal/pathspec"
	"github.com/xraph/forge/internal/shared"
)

// maxSegments bounds both insertion and the recursive walk. A path carrying
// tens of thousands of slashes would otherwise exhaust the stack.
const maxSegments = 256

// routeRef is what a terminal node holds. Pattern is kept because the walk
// binds parameter names from Pattern.Params at the leaf.
type routeRef struct {
	pattern pathspec.Pattern
	handler http.Handler
	kind    shared.RouteKind
}

// paramEdge is one parameter branch out of a node. Edges are kept sorted by
// descending constraint rank, so the walk tries the most specific first.
//
// The edge carries no name. Names live on routeRef.pattern and are bound at
// the leaf, which is what lets /users/{id}/posts and /users/{uid}/comments
// share this edge without conflicting.
type paramEdge struct {
	constraint pathspec.Constraint
	enum       []string
	node       *node
}

type node struct {
	static   map[string]*node
	params   []*paramEdge
	wildcard *node

	// methods holds per-method terminals. any holds a terminal registered
	// with an empty method, which matches every verb including custom ones.
	methods map[string]*routeRef
	any     *routeRef
}

func newNode() *node { return &node{static: make(map[string]*node)} }

type tree struct{ root *node }

func newTree() *tree { return &tree{root: newNode()} }

// shapeOf normalizes a pattern to its matching shape, discarding parameter
// names but keeping kind and constraint. Two routes with the same shape on
// the same method are ambiguous and cannot both be registered.
func shapeOf(p pathspec.Pattern) string {
	out := ""

	for _, seg := range p.Segments {
		switch seg.Kind {
		case pathspec.KindStatic:
			out += "/" + seg.Literal
		case pathspec.KindParam:
			out += "/:" + strconv.Itoa(int(seg.Constraint))
		case pathspec.KindWildcard:
			out += "/*"
		}
	}

	if out == "" {
		return "/"
	}

	return out
}

// insert adds a route. An empty method means every method.
func (t *tree) insert(method string, p pathspec.Pattern, h http.Handler, kind shared.RouteKind) error {
	if len(p.Segments) > maxSegments {
		return fmt.Errorf("forgemux: path %q has %d segments, more than the limit of %d",
			p.Raw, len(p.Segments), maxSegments)
	}

	cur := t.root

	for _, seg := range p.Segments {
		switch seg.Kind {
		case pathspec.KindStatic:
			next, ok := cur.static[seg.Literal]
			if !ok {
				next = newNode()
				cur.static[seg.Literal] = next
			}

			cur = next

		case pathspec.KindParam:
			cur = cur.paramChild(seg)

		case pathspec.KindWildcard:
			if cur.wildcard == nil {
				cur.wildcard = newNode()
			}

			cur = cur.wildcard
		}
	}

	ref := &routeRef{pattern: p, handler: h, kind: kind}

	if method == "" {
		if cur.any != nil {
			return fmt.Errorf("forgemux: route %q conflicts with %q; both claim every method",
				p.Raw, cur.any.pattern.Raw)
		}

		cur.any = ref

		return nil
	}

	if cur.methods == nil {
		cur.methods = make(map[string]*routeRef, 2)
	}

	if prev, ok := cur.methods[method]; ok {
		return fmt.Errorf("forgemux: route %s %q conflicts with %q; both have the shape %s",
			method, p.Raw, prev.pattern.Raw, shapeOf(p))
	}

	cur.methods[method] = ref

	return nil
}

// paramChild finds or creates the edge for a parameter segment. Edges are
// keyed by (constraint, enum), never by name.
func (n *node) paramChild(seg pathspec.Segment) *node {
	for _, e := range n.params {
		if e.constraint == seg.Constraint && slices.Equal(e.enum, seg.Enum) {
			return e.node
		}
	}

	edge := &paramEdge{constraint: seg.Constraint, enum: seg.Enum, node: newNode()}
	n.params = append(n.params, edge)

	// Descending rank, stable so equal-rank edges keep insertion order.
	sort.SliceStable(n.params, func(i, j int) bool {
		return n.params[i].constraint.Rank() > n.params[j].constraint.Rank()
	})

	return edge.node
}
