package forgemux

import "sort"

type result uint8

const (
	resultMatched result = iota
	resultNotFound
	resultMethodNotAllowed
)

// captureInline is how many parameter positions are recorded without touching
// the heap. Routes with more parameters spill, which is rare enough not to
// optimize.
const captureInline = 8

// capture records the byte offsets of matched parameter segments.
//
// It holds offsets rather than strings on purpose: nothing is turned into a
// value until a route is confirmed, so an abandoned branch costs nothing, and
// the walk allocates zero times.
type capture struct {
	starts, ends [captureInline]uint16
	n            int
	spill        [][2]uint16
}

func (c *capture) push(start, end int) {
	if c.n < captureInline {
		c.starts[c.n] = uint16(start)
		c.ends[c.n] = uint16(end)
		c.n++

		return
	}

	c.spill = append(c.spill, [2]uint16{uint16(start), uint16(end)})
	c.n++
}

// pop undoes the most recent push. Every abandoned branch must pop, or the
// next branch binds against stale offsets.
func (c *capture) pop() {
	if c.n > captureInline {
		c.spill = c.spill[:len(c.spill)-1]
	}

	if c.n > 0 {
		c.n--
	}
}

func (c *capture) len() int { return c.n }

func (c *capture) at(i int) (start, end int) {
	if i < captureInline {
		return int(c.starts[i]), int(c.ends[i])
	}

	pair := c.spill[i-captureInline]

	return int(pair[0]), int(pair[1])
}

// lookup walks the trie for one request.
//
// On a match the caller binds names from ref.pattern.Params against c, in
// order. That is the whole reason names never appear on a node.
func (t *tree) lookup(method, path string, c *capture) (*routeRef, result, []string) {
	return walk(t.root, path, 0, method, c, 0)
}

// walk descends one segment per call, trying children in fixed specificity
// order: static, then parameter edges by descending constraint rank, then
// wildcard. It backtracks, because a static branch that matches this segment
// may dead-end deeper while a parameter branch would have succeeded.
func walk(n *node, path string, pos int, method string, c *capture, depth int) (*routeRef, result, []string) {
	if depth > maxSegments {
		return nil, resultNotFound, nil
	}

	// The path is consumed when nothing is left, and also when only a trailing
	// slash remains: "/" has no segments at all, and "/users/" has none after
	// "users". Without the second clause the walk invents an empty segment and
	// the root route never matches.
	if pos >= len(path) || (pos == len(path)-1 && path[pos] == '/') {
		return terminal(n, method)
	}

	// path[pos] is the '/' that opens this segment.
	start := pos + 1
	end := start

	for end < len(path) && path[end] != '/' {
		end++
	}

	segment := path[start:end]

	// An empty segment is a repeated slash. Collapse it: "/users//42" is
	// "/users/42". The BunRouter adapter cleans these too, but for an interior
	// double slash it does so with a 301, which is the redirect this design
	// removed because it lets a client rewrite POST as GET.
	if segment == "" {
		return walk(n, path, end, method, c, depth+1)
	}

	var (
		sawMethodMismatch bool
		allowed           []string
	)

	if child, ok := n.static[segment]; ok {
		ref, res, a := walk(child, path, end, method, c, depth+1)
		if res == resultMatched {
			return ref, res, nil
		}

		if res == resultMethodNotAllowed {
			sawMethodMismatch, allowed = true, a
		}
	}

	for _, edge := range n.params {
		if !edge.constraint.Match(segment, edge.enum) {
			continue
		}

		c.push(start, end)

		ref, res, a := walk(edge.node, path, end, method, c, depth+1)
		if res == resultMatched {
			return ref, res, nil
		}

		c.pop()

		if res == resultMethodNotAllowed {
			sawMethodMismatch, allowed = true, a
		}
	}

	if n.wildcard != nil {
		// A wildcard consumes the rest of the path, so there is nothing left
		// to walk and the node is resolved directly.
		c.push(start, len(path))

		ref, res, a := terminal(n.wildcard, method)
		if res == resultMatched {
			return ref, res, nil
		}

		c.pop()

		if res == resultMethodNotAllowed {
			sawMethodMismatch, allowed = true, a
		}
	}

	if sawMethodMismatch {
		return nil, resultMethodNotAllowed, allowed
	}

	return nil, resultNotFound, nil
}

// terminal resolves a node that the path has fully consumed.
func terminal(n *node, method string) (*routeRef, result, []string) {
	if ref, ok := n.methods[method]; ok {
		return ref, resultMatched, nil
	}

	if n.any != nil {
		return n.any, resultMatched, nil
	}

	if len(n.methods) > 0 {
		allowed := make([]string, 0, len(n.methods))
		for m := range n.methods {
			allowed = append(allowed, m)
		}

		// Sorted so the Allow header is deterministic across runs; Go's map
		// iteration order is not.
		sort.Strings(allowed)

		return nil, resultMethodNotAllowed, allowed
	}

	return nil, resultNotFound, nil
}
