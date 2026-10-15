package vec

import (
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
)

type fake struct{ labels map[string]string }

func TestWithCachesChildren(t *testing.T) {
	var made atomic.Int32

	v := New("orders", []string{"tier", "region"}, func(l map[string]string) *fake {
		made.Add(1)

		return &fake{labels: l}
	}, &fake{})

	a := v.With("pro", "eu")
	b := v.With("pro", "eu")

	if a != b || made.Load() != 1 {
		t.Fatalf("children not cached: made %d", made.Load())
	}

	if a.labels["tier"] != "pro" || a.labels["region"] != "eu" {
		t.Fatalf("labels = %v", a.labels)
	}
}

func TestWrongArityReturnsSinkAndReportsOnce(t *testing.T) {
	sink := &fake{}
	calls := 0

	restore := SetArityHook(func(name string, want, got int) {
		if name != "orders" || want != 2 || got != 1 {
			t.Errorf("hook got %q %d %d", name, want, got)
		}

		calls++
	})
	defer restore()

	v := New("orders", []string{"tier", "region"}, func(map[string]string) *fake { return &fake{} }, sink)

	for range 2 {
		if v.With("pro") != sink {
			t.Fatal("wrong arity must return the sink")
		}
	}

	if calls != 1 {
		t.Fatalf("hook called %d times, want 1 per vector", calls)
	}
}

func TestCacheIsBounded(t *testing.T) {
	v := New("ids", []string{"id"}, func(map[string]string) *fake { return &fake{} }, &fake{})

	for i := range maxCached + 500 {
		v.With(string(rune('a'+i%26)) + string(rune(i)))
	}

	if n := cachedLen(v.cache.Load()); n != maxCached {
		t.Fatalf("cache holds %d children, want the cap of %d", n, maxCached)
	}
}

// The HTTP server metrics call With on every request, so a cache hit must
// not allocate.
func TestWithCacheHitDoesNotAllocate(t *testing.T) {
	v := New("requests", []string{"method", "route", "status", "scheme", "error"}, func(map[string]string) *fake {
		return &fake{}
	}, &fake{})

	v.With("GET", "/users/:id", "200", "http", "")

	allocs := testing.AllocsPerRun(100, func() {
		v.With("GET", "/users/:id", "200", "http", "")
	})

	if allocs != 0 {
		t.Fatalf("With on a cached tuple allocated %.0f times, want 0", allocs)
	}
}

// Values that join to the same bytes under a naive separator must stay
// distinct children.
func TestWithKeepsAdjacentValuesApart(t *testing.T) {
	v := New("pairs", []string{"a", "b"}, func(l map[string]string) *fake {
		return &fake{labels: l}
	}, &fake{})

	x := v.With("ab", "c")
	y := v.With("a", "bc")

	if x == y {
		t.Fatal(`With("ab", "c") and With("a", "bc") returned the same child`)
	}
}

// Goroutines racing on the same new tuples all get the child that was cached.
func TestWithConcurrentInsertsAgree(t *testing.T) {
	v := New("racing", []string{"id"}, func(map[string]string) *fake { return &fake{} }, &fake{})

	const workers, ids = 8, 64

	got := make([][ids]*fake, workers)

	var wg sync.WaitGroup

	for w := range workers {
		wg.Go(func() {
			for i := range ids {
				got[w][i] = v.With(strconv.Itoa(i))
			}
		})
	}

	wg.Wait()

	for i := range ids {
		want := v.With(strconv.Itoa(i))
		for w := range workers {
			if got[w][i] != want {
				t.Fatalf("worker %d got a different child for id %d", w, i)
			}
		}
	}
}
