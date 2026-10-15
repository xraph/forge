package telemetry

import (
	"maps"
	"sync"
	"sync/atomic"
)

// maxSpanNames bounds the span name cache. Routes are declared in code, so a
// real app stays far below it; past it, names are built per request.
const maxSpanNames = 1024

// spanNames caches the "METHOD /route" span names that the tracing middleware
// and the profiler labels both use. Building one per request cost a string
// allocation.
var spanNames nameCache

type spanNameKey struct{ method, pattern string }

// nameCache is copy on write: reads take no lock, an insert copies the map.
type nameCache struct {
	mu sync.Mutex
	m  atomic.Pointer[map[spanNameKey]string]
}

func (n *nameCache) get(method, pattern string) string {
	key := spanNameKey{method, pattern}

	if m := n.m.Load(); m != nil {
		if name, ok := (*m)[key]; ok {
			return name
		}
	}

	name := method + " " + pattern

	n.mu.Lock()
	defer n.mu.Unlock()

	old := n.m.Load()

	size := 0
	if old != nil {
		size = len(*old)
	}

	if size >= maxSpanNames {
		return name
	}

	next := make(map[spanNameKey]string, size+1)
	if old != nil {
		maps.Copy(next, *old)
	}

	next[key] = name
	n.m.Store(&next)

	return name
}
