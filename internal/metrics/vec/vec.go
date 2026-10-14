// Package vec provides label vectors: a metric declared once with label
// names, then fetched per label-value tuple. Children are cached so the hot
// path takes no collector lock.
package vec

import (
	"strings"
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
	cache  sync.Map // joined values -> T
	cached atomic.Int64
	warned atomic.Bool
}

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

	key := strings.Join(values, "\xff")
	if m, ok := v.cache.Load(key); ok {
		return m.(T)
	}

	labels := make(map[string]string, len(values))
	for i, n := range v.labels {
		labels[n] = values[i]
	}

	child := v.build(labels)

	if v.cached.Load() >= maxCached {
		return child
	}

	actual, loaded := v.cache.LoadOrStore(key, child)
	if !loaded {
		v.cached.Add(1)
	}

	return actual.(T)
}
