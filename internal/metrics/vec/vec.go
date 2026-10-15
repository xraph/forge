// Package vec provides label vectors: a metric declared once with label
// names, then fetched per label-value tuple. Children are cached so the hot
// path takes no collector lock.
package vec

import (
	"maps"
	"sync"
	"sync/atomic"

	"github.com/xraph/go-utils/log"
)

// maxCached bounds the child cache. Past it, children are still returned
// (the registry caps series), they just aren't cached here.
const maxCached = 4096

type arityHook func(name string, want, got int)

var hook atomic.Pointer[arityHook]

func defaultHook(name string, want, got int) {
	log.GetGlobalLogger().Error("metrics: wrong number of label values, sample dropped",
		log.String("metric", name), log.Int("want", want), log.Int("got", got))
}

// SetArityHook replaces what happens on a wrong-arity With call and returns a
// function that restores the previous hook. Tests use it to fail loudly.
func SetArityHook(f func(name string, want, got int)) (restore func()) {
	h := arityHook(f)
	prev := hook.Swap(&h)

	return func() { hook.Store(prev) }
}

// Vec is a metric with declared label names.
type Vec[T any] struct {
	name   string
	labels []string
	build  func(map[string]string) T
	sink   T
	warned atomic.Bool

	// cache maps the joined label values to the child. It is copy on write:
	// readers load the map and index it with no lock, and an insert copies it
	// under mu. Indexing a plain map with string(bytes) doesn't allocate,
	// where a sync.Map lookup needs the key as a string first.
	cache atomic.Pointer[map[string]T]
	mu    sync.Mutex
}

// keyBufSize covers the usual joined key without a heap allocation. A longer
// key still works; it just allocates.
const keyBufSize = 128

// New declares a vector. build builds the child for one label set; sink is
// returned on a wrong-arity call.
func New[T any](name string, labelNames []string, build func(map[string]string) T, sink T) *Vec[T] {
	return &Vec[T]{name: name, labels: labelNames, build: build, sink: sink}
}

// With returns the child for the given label values, in labelNames order.
func (v *Vec[T]) With(values ...string) T {
	if len(values) != len(v.labels) {
		if v.warned.CompareAndSwap(false, true) {
			if h := hook.Load(); h != nil {
				(*h)(v.name, len(v.labels), len(values))
			} else {
				defaultHook(v.name, len(v.labels), len(values))
			}
		}

		return v.sink
	}

	var buf [keyBufSize]byte

	key := appendKey(buf[:0], values)

	if m := v.cache.Load(); m != nil {
		if child, ok := (*m)[string(key)]; ok {
			return child
		}
	}

	labels := make(map[string]string, len(values))
	for i, n := range v.labels {
		labels[n] = values[i]
	}

	return v.store(string(key), v.build(labels))
}

// store caches child under key unless another goroutine got there first, in
// which case it returns theirs. Past maxCached it returns child uncached.
func (v *Vec[T]) store(key string, child T) T {
	v.mu.Lock()
	defer v.mu.Unlock()

	old := v.cache.Load()
	if old != nil {
		if existing, ok := (*old)[key]; ok {
			return existing
		}

		if len(*old) >= maxCached {
			return child
		}
	}

	next := make(map[string]T, cachedLen(old)+1)
	if old != nil {
		maps.Copy(next, *old)
	}

	next[key] = child
	v.cache.Store(&next)

	return child
}

func cachedLen[T any](m *map[string]T) int {
	if m == nil {
		return 0
	}

	return len(*m)
}

// appendKey joins values with \xff, a byte that never appears in UTF-8, so
// ("ab", "c") and ("a", "bc") stay apart.
func appendKey(dst []byte, values []string) []byte {
	for i, s := range values {
		if i > 0 {
			dst = append(dst, 0xff)
		}

		dst = append(dst, s...)
	}

	return dst
}
