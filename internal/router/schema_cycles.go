package router

import "reflect"

// Cycle protection for the embedded-struct walks.
//
// Go forbids a struct from embedding itself by value, but not from embedding a
// POINTER to itself, and not from embedding a chain of types that closes back
// on the first:
//
//	type Node struct {
//	    *Node
//	    Name string `json:"name"`
//	}
//
// Every walk in this package that flattens embedded fields -- the OpenAPI
// struct schema, the unified request extractor, the AsyncAPI header walk --
// descends into the embedded type and starts over. On a type like Node that
// never bottoms out: the walk recurses until the goroutine stack is exhausted,
// which in Go is `fatal error: stack overflow`, not a panic. No recover()
// intercepts it and no middleware contains it, so registering a single route
// with such a request or response type killed the process during spec
// generation. Tree nodes, linked lists, comment threads and org charts all
// have this shape.
//
// # Why a visited set rather than a depth bound
//
// A depth bound terminates, but it cannot tell "this type came back" from
// "this type is legitimately deep". It answers the second case by silently
// dropping real fields from the document, with no error and nothing to
// grep for. A set keyed on reflect.Type answers the actual question -- is this
// a back edge -- and lets a deep-but-finite embedding chain through intact.
//
// # Why not a $ref
//
// OpenAPI expresses recursion with a $ref back to the component, and that IS
// what this generator emits for a type that recurses through a NAMED field:
// createOrReuseComponentRef registers a placeholder component before
// descending, so a json-tagged "Parent *Node" field resolves to
// $ref: '#/components/schemas/Node'. That path was never broken.
//
// A $ref is not available here, because embedding is not reference -- it is
// flattening. An embedded field contributes its properties to the enclosing
// object rather than appearing as a property, so the only way to spell it with
// a $ref is `allOf: [$ref '#/components/schemas/Node', {...}]` inside Node's
// own definition, which is self-referential and unresolvable.
//
// The right answer is the one encoding/json already gives. Its field walk
// dedupes on reflect.Type for this exact reason, so a Node marshals as
// {"name": ...}: the embedded *Node promotes nothing the outer struct has not
// already promoted. The schema describes that JSON, so the schema must agree
// with it -- and it does, once the walk stops revisiting.
//
// # Scope of the set
//
// The set holds the types on the CURRENT path, not every type seen anywhere in
// the walk: a type is added on entry and removed on exit. That detects a back
// edge and nothing else, so a type reached twice by two different embedding
// paths (the diamond case) is still promoted exactly as it is today. The
// change is therefore confined to types that are genuinely cyclic -- which is
// to say, to types that crashed.
type visitedTypes map[reflect.Type]struct{}

// newVisitedTypes returns a set seeded with the struct type whose walk is
// beginning. Seeding the root is what makes the first arrival back at it a
// back edge rather than one redundant lap.
func newVisitedTypes(root reflect.Type) visitedTypes {
	if root.Kind() == reflect.Ptr {
		root = root.Elem()
	}

	v := visitedTypes{}
	if root.Kind() == reflect.Struct {
		v[root] = struct{}{}
	}

	return v
}

// enter resolves an embedded field's type to the struct to descend into and
// marks it as being on the current path.
//
// It reports false when there is nothing to descend into -- the field is not a
// struct, or it is already on the path and descending would close a cycle. The
// returned release must be called when the walk of that type finishes, which
// is what keeps the set path-scoped; it is a no-op when ok is false.
func (v visitedTypes) enter(typ reflect.Type) (structType reflect.Type, release func(), ok bool) {
	if typ.Kind() == reflect.Ptr {
		typ = typ.Elem()
	}

	if typ.Kind() != reflect.Struct {
		return typ, func() {}, false
	}

	if _, onPath := v[typ]; onPath {
		return typ, func() {}, false
	}

	v[typ] = struct{}{}

	return typ, func() { delete(v, typ) }, true
}

// seen reports whether typ is already on the current path, without marking it.
// Used by walks that only ask a yes/no question about a type and so have no
// walk to release.
func (v visitedTypes) seen(typ reflect.Type) bool {
	if typ.Kind() == reflect.Ptr {
		typ = typ.Elem()
	}

	_, onPath := v[typ]

	return onPath
}

// mark places typ on the current path and returns the release that takes it
// off again.
func (v visitedTypes) mark(typ reflect.Type) func() {
	if typ.Kind() == reflect.Ptr {
		typ = typ.Elem()
	}

	v[typ] = struct{}{}

	return func() { delete(v, typ) }
}
